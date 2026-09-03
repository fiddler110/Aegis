// Package checkpoint provides per-turn state snapshots so a user can rewind a
// session: restore the files the agent touched, the conversation up to a point,
// or both. It is the "fearless editing" capability standardized across modern
// agent harnesses (Claude Code /rewind, opencode, Cline checkpoints).
//
// A checkpoint is created at the start of every user turn. As the agent's
// write/edit tools modify files during that turn, a Snapshotter lazily captures
// each file's *pre-modification* content exactly once. Rewinding to the
// checkpoint writes those captured contents back (and deletes files that did
// not exist before the turn), and/or truncates the conversation to the message
// count recorded at the checkpoint.
package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// maxSnapshotBytes caps the size of a single file snapshot. Files larger than
// this are not captured (and therefore not restored), keeping the checkpoint
// store from bloating on large binary artifacts.
const maxSnapshotBytes = 16 << 20 // 16 MiB

// ErrNotFound is returned when a checkpoint does not exist.
var ErrNotFound = errors.New("checkpoint not found")

// ErrRestoreRefused wraps every reason RestoreFiles declines to replay a
// checkpoint at all (P70.1). A refusal is *all or nothing*: when it is
// returned, nothing on disk has been touched. Callers can distinguish it from
// the best-effort per-file write errors RestoreFiles still returns after
// having restored what it could.
var ErrRestoreRefused = errors.New("checkpoint restore refused")

// defaultRestoreMode is the permission a restored file gets when the
// checkpoint predates mode capture (mode column 0). It matches the literal
// os.WriteFile mode this package used before P70.1.
const defaultRestoreMode fs.FileMode = 0o644

// Checkpoint is a restore point captured at the start of a user turn.
type Checkpoint struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Seq       int       `json:"seq"`               // message count to truncate the conversation to on rewind
	Label     string    `json:"label"`             // typically the user's prompt text, truncated
	GitSHA    string    `json:"git_sha,omitempty"` // HEAD commit at checkpoint time (P3.4)
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`

	// WorkspaceRoot is the session's workspace directory at the moment the
	// checkpoint was created. Restore refuses any captured path that does not
	// resolve inside it (P70.1). Recorded per checkpoint rather than per Store
	// because one Store is shared by every session on the daemon, and two
	// sessions can have different workspaces. Empty only for rows written
	// before P70.1, which restore refuses outright.
	WorkspaceRoot string `json:"workspace_root,omitempty"`
}

// FileSnapshot is one captured file within a checkpoint.
type FileSnapshot struct {
	Path    string      // absolute path
	Existed bool        // true if the file existed before the turn; false if newly created
	Content []byte      // pre-turn content (nil if Existed is false)
	Mode    fs.FileMode // pre-turn permission bits; 0 when unknown (pre-P70.1 rows, or a path that did not exist)
}

// Store persists checkpoints in SQLite. It shares the daemon's single session
// database connection, preserving the serialized-writes guarantee.
type Store struct {
	db *sql.DB
}

// NewStore creates the checkpoint tables on db and returns a Store.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS checkpoints (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    label       TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session ON checkpoints(session_id, created_at);
CREATE TABLE IF NOT EXISTS checkpoint_files (
    checkpoint_id TEXT NOT NULL,
    path          TEXT NOT NULL,
    existed       INTEGER NOT NULL,
    content       BLOB,
    PRIMARY KEY (checkpoint_id, path)
);`)
	if err != nil {
		return err
	}
	// Idempotent additions for existing databases (P3.4: git SHA column).
	_, _ = s.db.Exec(`ALTER TABLE checkpoints ADD COLUMN git_sha TEXT NOT NULL DEFAULT ''`)
	// P70.1: the workspace root every captured path must resolve inside, and
	// the pre-turn permission bits of a captured file. Both default to the
	// "unknown" value on rows written by an older binary — an empty root makes
	// restore refuse the checkpoint, a zero mode makes it fall back to 0o644.
	_, _ = s.db.Exec(`ALTER TABLE checkpoints ADD COLUMN workspace_root TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE checkpoint_files ADD COLUMN mode INTEGER NOT NULL DEFAULT 0`)
	// P81.31: the SHA-256 of a captured path's content once the owning turn's
	// own edits are done (RecordPostTurnState) — nullable, left NULL for rows
	// this feature predates or never got the chance to run for. A zero-length
	// (non-NULL) value is the "the path should not exist" case, distinct from
	// both NULL ("no baseline recorded") and a real 32-byte digest.
	_, _ = s.db.Exec(`ALTER TABLE checkpoint_files ADD COLUMN post_digest BLOB`)
	return nil
}

// Create records a new checkpoint for sessionID. seq is the conversation
// message count at the moment of capture (rewinding truncates to it). label is
// a short human description, typically the user's prompt. workspaceRoot is the
// session's workspace directory: restore refuses to write any captured path
// that does not resolve inside it (P70.1), so passing "" produces a checkpoint
// whose files can never be restored.
func (s *Store) Create(ctx context.Context, sessionID string, seq int, label, workspaceRoot string) (*Checkpoint, error) {
	if workspaceRoot != "" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
	}
	cp := &Checkpoint{
		ID:            uuid.NewString(),
		SessionID:     sessionID,
		Seq:           seq,
		Label:         truncateLabel(label, 120),
		CreatedAt:     time.Now(),
		WorkspaceRoot: workspaceRoot,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (id, session_id, seq, label, created_at, workspace_root) VALUES (?, ?, ?, ?, ?, ?)`,
		cp.ID, cp.SessionID, cp.Seq, cp.Label, cp.CreatedAt.UnixMilli(), cp.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("insert checkpoint: %w", err)
	}
	return cp, nil
}

// recordFile stores a single captured file. It is best-effort and used by the
// Snapshotter; a primary-key conflict (the file was already captured this turn)
// is ignored.
func (s *Store) recordFile(checkpointID, path string, existed bool, content []byte, mode fs.FileMode) error {
	existedInt := 0
	if existed {
		existedInt = 1
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO checkpoint_files (checkpoint_id, path, existed, content, mode) VALUES (?, ?, ?, ?, ?)`,
		checkpointID, path, existedInt, content, int64(mode.Perm()))
	return err
}

// List returns checkpoints for a session, most recent first, with file counts.
func (s *Store) List(ctx context.Context, sessionID string) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.session_id, c.seq, c.label, c.git_sha, c.workspace_root, c.created_at,
       (SELECT COUNT(*) FROM checkpoint_files f WHERE f.checkpoint_id = c.id)
FROM checkpoints c
WHERE c.session_id = ?
ORDER BY c.created_at DESC, c.id DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		var cp Checkpoint
		var created int64
		if err := rows.Scan(&cp.ID, &cp.SessionID, &cp.Seq, &cp.Label, &cp.GitSHA, &cp.WorkspaceRoot, &created, &cp.FileCount); err != nil {
			return nil, err
		}
		cp.CreatedAt = time.UnixMilli(created)
		out = append(out, cp)
	}
	return out, rows.Err()
}

// Get loads a single checkpoint's metadata.
func (s *Store) Get(ctx context.Context, id string) (*Checkpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT c.id, c.session_id, c.seq, c.label, c.git_sha, c.workspace_root, c.created_at,
       (SELECT COUNT(*) FROM checkpoint_files f WHERE f.checkpoint_id = c.id)
FROM checkpoints c WHERE c.id = ?`, id)
	var cp Checkpoint
	var created int64
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.Seq, &cp.Label, &cp.GitSHA, &cp.WorkspaceRoot, &created, &cp.FileCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	cp.CreatedAt = time.UnixMilli(created)
	return &cp, nil
}

// SetGitSHA records the HEAD commit SHA at checkpoint creation time (P3.4).
// Best-effort: called asynchronously so errors are tolerated.
func (s *Store) SetGitSHA(ctx context.Context, id, sha string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE checkpoints SET git_sha = ? WHERE id = ?`, sha, id)
	return err
}

// files returns the captured file snapshots for a checkpoint.
func (s *Store) files(ctx context.Context, checkpointID string) ([]FileSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, existed, content, mode FROM checkpoint_files WHERE checkpoint_id = ?`, checkpointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileSnapshot
	for rows.Next() {
		var snap FileSnapshot
		var existedInt int
		var mode int64
		if err := rows.Scan(&snap.Path, &existedInt, &snap.Content, &mode); err != nil {
			return nil, err
		}
		snap.Existed = existedInt == 1
		snap.Mode = fs.FileMode(mode).Perm()
		out = append(out, snap)
	}
	return out, rows.Err()
}

// workspaceRoot returns the root recorded on a checkpoint row, or ErrNotFound
// if the checkpoint does not exist. An empty string means the row predates
// P70.1.
func (s *Store) workspaceRoot(ctx context.Context, checkpointID string) (string, error) {
	var root string
	err := s.db.QueryRowContext(ctx,
		`SELECT workspace_root FROM checkpoints WHERE id = ?`, checkpointID).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return root, err
}

// withinRoot reports whether path resolves to a location strictly inside root.
// Both sides are symlink-resolved, so this is a real filesystem containment
// check — `..` segments and symlinked ancestors are resolved — not a
// string-prefix test (which would accept /work-other for a root of /work).
//
// CLN-1: the containment itself is sandbox.StrictlyWithinRootResolved. The two
// guards stay here because they are this caller's preconditions rather than
// properties of containment: an empty root is a checkpoint row with no
// workspace recorded, and a relative path is not something restore may write.
// "Strictly" is the P70.1 posture — a captured path equal to the workspace
// root is not a file that could have been captured, and is refused.
func withinRoot(root, path string) bool {
	if root == "" || !filepath.IsAbs(path) {
		return false
	}
	return sandbox.StrictlyWithinRootResolved(root, path)
}

// RestoreFiles writes each captured file in the checkpoint back to its
// pre-turn content and permission bits. Files that did not exist before the
// turn are deleted. It returns the number of files restored.
//
// P70.1: every captured path is validated against the workspace root recorded
// on the checkpoint row *before* anything is written, and a single path that
// resolves outside that root refuses the whole restore — nothing is written,
// and the returned error (wrapping ErrRestoreRefused) names the offending
// path. A half-rewound tree is precisely the failure mode /rewind exists to
// prevent, so a stale or malformed row must not produce one. This is the layer
// that stops depending on every present and future capture site resolving
// in-workspace.
//
// A checkpoint whose row records no root (written before P70.1, or by a caller
// that passed "") is refused for the same reason: its paths cannot be checked
// against anything, and silently trusting them is the behavior being removed.
// A checkpoint that captured no files is a no-op and needs no root.
//
// Past validation it stays best-effort per file: an error writing one file is
// recorded but does not stop the others.
func (s *Store) RestoreFiles(ctx context.Context, checkpointID string) (int, error) {
	snaps, err := s.files(ctx, checkpointID)
	if err != nil {
		return 0, err
	}
	if len(snaps) == 0 {
		return 0, nil
	}
	root, err := s.workspaceRoot(ctx, checkpointID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, fmt.Errorf("%w: checkpoint %s has captured files but no checkpoint row", ErrRestoreRefused, checkpointID)
		}
		return 0, err
	}
	if root == "" {
		return 0, fmt.Errorf("%w: checkpoint %s records no workspace root, so its captured paths cannot be validated (checkpoint created before P70.1)", ErrRestoreRefused, checkpointID)
	}
	for _, snap := range snaps {
		if !withinRoot(root, snap.Path) {
			return 0, fmt.Errorf("%w: captured path %q resolves outside the checkpoint's workspace root %q; nothing was restored", ErrRestoreRefused, snap.Path, root)
		}
	}

	var restored int
	var firstErr error
	for _, fs := range snaps {
		if fs.Existed {
			mode := fs.Mode.Perm()
			if mode == 0 {
				mode = defaultRestoreMode
			}
			if err := os.WriteFile(fs.Path, fs.Content, mode); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			// WriteFile's mode applies only when it creates the file, so an
			// existing file whose mode the turn changed needs an explicit
			// chmod to come back as it was.
			if err := os.Chmod(fs.Path, mode); err != nil && firstErr == nil {
				firstErr = err
			}
		} else {
			// File was created during the turn: remove it. A missing file is
			// not an error (it may already have been deleted).
			if err := os.Remove(fs.Path); err != nil && !os.IsNotExist(err) {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		restored++
	}
	return restored, firstErr
}

// Delete removes a checkpoint and its captured files.
func (s *Store) Delete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM checkpoint_files WHERE checkpoint_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM checkpoints WHERE id = ?`, id)
	return err
}

// DeleteForSession removes all checkpoints (and files) for a session. Called
// when a session is deleted so snapshots don't leak.
func (s *Store) DeleteForSession(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM checkpoint_files WHERE checkpoint_id IN (SELECT id FROM checkpoints WHERE session_id = ?)`,
		sessionID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM checkpoints WHERE session_id = ?`, sessionID)
	return err
}

// PruneOlderThan deletes every checkpoint created before cutoff, and the file
// snapshots captured under it. It returns the number of checkpoints deleted.
//
// P81.24. Until this existed, a checkpoint row was deleted only when its
// session was (Delete/DeleteForSession, reached from the delete-session
// handler and from session.Store.Prune), and session.Store.Prune deliberately
// skips archived sessions. So a checkpoint outlived its usefulness
// indefinitely — and a checkpoint is not metadata: checkpoint_files holds
// whole verbatim copies of every file the agent was about to modify, up to
// maxSnapshotBytes each. Rewind only ever reaches the current session's
// recent turns, so age is a sound retention key on its own, independent of
// whether the owning session still exists.
//
// It is a time cutoff rather than a "keep the last N per session" rule because
// the value of a checkpoint decays with wall-clock time, not with the number
// of turns since: a session idle for a month has nothing worth rewinding to
// however few checkpoints it made.
func (s *Store) PruneOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	ms := cutoff.UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM checkpoint_files WHERE checkpoint_id IN (SELECT id FROM checkpoints WHERE created_at < ?)`,
		ms); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM checkpoints WHERE created_at < ?`, ms)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// absentDigest is the sentinel post_digest value recorded for a captured path
// that does not exist once the owning turn's own edits are done — distinct
// from both NULL (no baseline recorded at all) and a real 32-byte SHA-256
// digest, and from Go's nil (database/sql writes a zero-length non-nil slice
// as an empty BLOB, not NULL).
var absentDigest = []byte{}

// RecordPostTurnState stores each of checkpointID's captured paths' current
// on-disk content digest as the baseline ExternalChangesSince later compares
// against (P81.31/FIND-31). Call it once, after the turn that owns the
// checkpoint has finished making its own writes — from that point on, any
// difference between a captured path's live content and this digest was not
// produced by this turn's own tool calls: a reviewer's edit in another
// editor, a concurrent Aegis session, a build script, anything external.
//
// Best-effort like Capture/CaptureBytes: a read/stat error for one path is
// swallowed rather than aborting the rest, since this is provenance for a
// confirmation prompt, not the restore path itself. A checkpoint that
// captured no files is a no-op.
func (s *Store) RecordPostTurnState(ctx context.Context, checkpointID string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM checkpoint_files WHERE checkpoint_id = ?`, checkpointID)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, p := range paths {
		digest := absentDigest
		if data, err := os.ReadFile(p); err == nil {
			sum := sha256.Sum256(data)
			digest = sum[:]
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE checkpoint_files SET post_digest = ? WHERE checkpoint_id = ? AND path = ?`,
			digest, checkpointID, p); err != nil {
			return err
		}
	}
	return nil
}

// ExternalChangesSince returns the captured paths in checkpointID whose
// current on-disk content no longer matches the baseline RecordPostTurnState
// recorded (P81.31/FIND-31) — i.e. paths that changed after the owning turn
// finished, from something other than that turn's own tool calls. A rewind
// that silently overwrites such a path would discard an edit the agent never
// made and the user may not know about.
//
// A path with no recorded baseline (RecordPostTurnState was never called for
// this checkpoint — rows from before this feature existed, or a checkpoint
// whose turn never reached the post-run bookkeeping) is skipped rather than
// reported: there is nothing to compare against, and treating "unknown" as
// "changed" would make every pre-existing checkpoint permanently unrewindable
// without a confirm flag.
func (s *Store) ExternalChangesSince(ctx context.Context, checkpointID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, post_digest FROM checkpoint_files WHERE checkpoint_id = ? AND post_digest IS NOT NULL`, checkpointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changed []string
	for rows.Next() {
		var path string
		var digest []byte
		if err := rows.Scan(&path, &digest); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if len(digest) == 0 {
				continue // still absent, as recorded
			}
			changed = append(changed, path)
			continue
		}
		sum := sha256.Sum256(data)
		if len(digest) == 0 || !bytes.Equal(sum[:], digest) {
			changed = append(changed, path)
		}
	}
	return changed, rows.Err()
}

// EvictOldestOverCap deletes sessionID's checkpoints, oldest first, until
// their total captured-file bytes are at or under maxBytes (P81.31/FIND-31).
// It returns the number of checkpoints deleted. maxBytes <= 0 disables the
// check (unbounded, the pre-P81.31 default).
//
// The most recently created checkpoint is never evicted, even if it alone
// exceeds the cap: the caller of this (checkpoint creation) needs at least
// one checkpoint left to rewind to, and rejecting the checkpoint that
// triggered the call would silently break rewind for the very turn this
// protects. That is a size-based ceiling — PruneOlderThan (P81.24) is the
// existing age-based one; they run independently and neither substitutes for
// the other (an old low-traffic session with two large checkpoints trips
// this, not that; a young session with hundreds of tiny ones trips that, not
// this).
func (s *Store) EvictOldestOverCap(ctx context.Context, sessionID string, maxBytes int64) (int, error) {
	if maxBytes <= 0 {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, COALESCE((SELECT SUM(LENGTH(f.content)) FROM checkpoint_files f WHERE f.checkpoint_id = c.id), 0)
FROM checkpoints c WHERE c.session_id = ?
ORDER BY c.created_at ASC, c.id ASC`, sessionID)
	if err != nil {
		return 0, err
	}
	type row struct {
		id   string
		size int64
	}
	var all []row
	var total int64
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.size); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, r)
		total += r.size
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	if total <= maxBytes || len(all) <= 1 {
		return 0, nil
	}
	var evicted int
	for _, r := range all {
		if len(all)-evicted <= 1 || total <= maxBytes {
			break
		}
		if err := s.Delete(ctx, r.id); err != nil {
			return evicted, err
		}
		total -= r.size
		evicted++
	}
	return evicted, nil
}

// NewSnapshotter returns a Snapshotter that captures pre-modification file
// content into the given checkpoint.
func (s *Store) NewSnapshotter(checkpointID string) *Snapshotter {
	return &Snapshotter{store: s, checkpointID: checkpointID, captured: make(map[string]bool)}
}

// CheckpointID returns the checkpoint this snapshotter captures file writes
// into. Exposed so a caller spawning a subprocess-mode sub-agent (which
// cannot see this Snapshotter directly — it lives in this process's ctx,
// and a subprocess starts an entirely separate one) can pass the id across
// that process boundary and have the worker reconstruct an equivalent
// Snapshotter of its own (P9).
func (s *Snapshotter) CheckpointID() string { return s.checkpointID }

// RestoreFiles rolls back every file captured under this snapshotter's
// checkpoint to its pre-turn state (deleting files that did not exist before
// the turn), delegating to Store.RestoreFiles. It returns the number of
// files restored. Used to quarantine a bad write on an exhausted
// output-guard FAIL (P27.16) — the same primitive the user-facing rewind
// feature already uses — including its P70.1 boundary check, so a rollback
// whose checkpoint records a path outside the workspace root (or records no
// root at all) writes nothing and returns an ErrRestoreRefused error rather
// than partially rolling back. Safe to call on a nil receiver (returns 0, nil),
// matching Capture's nil-safety so callers don't need a separate nil check.
func (s *Snapshotter) RestoreFiles(ctx context.Context) (int, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.RestoreFiles(ctx, s.checkpointID)
}

// Snapshotter captures the pre-modification content of files touched during a
// single turn. It is safe for concurrent use (parallel tool calls) and captures
// each path at most once, so the first capture wins — i.e. the pre-turn state.
type Snapshotter struct {
	store        *Store
	checkpointID string
	mu           sync.Mutex
	captured     map[string]bool
}

// Capture records the current content of absPath as the pre-turn state, the
// first time it is called for that path within the turn. It is a no-op on a nil
// receiver, so callers can invoke it unconditionally. Best-effort: storage
// errors are swallowed so a snapshot failure never blocks a tool.
func (s *Snapshotter) Capture(absPath string) {
	if s == nil || !s.claim(absPath) {
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		// File does not exist yet: record it as newly created so a rewind
		// deletes it.
		_ = s.store.recordFile(s.checkpointID, absPath, false, nil, 0)
		return
	}
	if info.Size() > maxSnapshotBytes {
		// Too large to snapshot; leave it untracked so rewind won't touch it.
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return
	}
	// P70.1: the pre-turn permission bits travel with the content, so a file
	// that was deleted during the turn and is recreated by restore comes back
	// with the mode it had rather than a flat 0o644.
	_ = s.store.recordFile(s.checkpointID, absPath, true, data, info.Mode())
}

// CaptureBytes records explicit pre-turn content for absPath, the first time
// it is called for that path within the turn (same "first call wins" rule as
// Capture, and likewise a no-op on a nil receiver). Unlike Capture, which
// reads absPath's *current* on-disk content, this takes the content directly
// — for a caller that can no longer read the pre-mutation bytes off disk (the
// mutation already happened by the time it runs) but recovered them from
// elsewhere, e.g. `git show HEAD:<path>` for a file a shell subprocess
// modified outside the write_file/edit_file tools that call Capture directly
// (see internal/tool/builtin/shell_checkpoint.go). existed=false with a nil
// data records the path as newly created, so rewind deletes it rather than
// restoring content — the same outcome as Capture's own not-found branch.
func (s *Snapshotter) CaptureBytes(absPath string, existed bool, data []byte) {
	if s == nil || !s.claim(absPath) {
		return
	}
	if len(data) > maxSnapshotBytes {
		return
	}
	// The pre-mutation mode is no longer observable (the mutation already
	// happened), so the file's current mode is the best available proxy —
	// better than recording 0 and restoring a flat 0o644. A path that no
	// longer exists records 0, and restore falls back to the default.
	var mode fs.FileMode
	if info, err := os.Stat(absPath); err == nil {
		mode = info.Mode()
	}
	_ = s.store.recordFile(s.checkpointID, absPath, existed, data, mode)
}

// claim reports whether this call is the first for absPath within the turn,
// atomically marking it captured if so. Callers proceed only when claim
// returns true, giving Capture/CaptureBytes their shared "first call wins"
// semantics regardless of which one is called first for a given path.
func (s *Snapshotter) claim(absPath string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.captured[absPath] {
		return false
	}
	s.captured[absPath] = true
	return true
}

type ctxKey struct{}

// WithSnapshotter attaches a Snapshotter to ctx so write/edit tools can capture
// pre-modification file content during a run.
func WithSnapshotter(ctx context.Context, s *Snapshotter) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// SnapshotterFrom returns the Snapshotter attached to ctx, or nil. The nil
// return is safe to call Capture on.
func SnapshotterFrom(ctx context.Context) *Snapshotter {
	s, _ := ctx.Value(ctxKey{}).(*Snapshotter)
	return s
}

func truncateLabel(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "…"
	}
	return s
}
