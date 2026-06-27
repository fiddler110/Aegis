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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// maxSnapshotBytes caps the size of a single file snapshot. Files larger than
// this are not captured (and therefore not restored), keeping the checkpoint
// store from bloating on large binary artifacts.
const maxSnapshotBytes = 16 << 20 // 16 MiB

// ErrNotFound is returned when a checkpoint does not exist.
var ErrNotFound = errors.New("checkpoint not found")

// Checkpoint is a restore point captured at the start of a user turn.
type Checkpoint struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Seq       int       `json:"seq"`   // message count to truncate the conversation to on rewind
	Label     string    `json:"label"` // typically the user's prompt text, truncated
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`
}

// FileSnapshot is one captured file within a checkpoint.
type FileSnapshot struct {
	Path    string // absolute path
	Existed bool   // true if the file existed before the turn; false if newly created
	Content []byte // pre-turn content (nil if Existed is false)
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
	return err
}

// Create records a new checkpoint for sessionID. seq is the conversation
// message count at the moment of capture (rewinding truncates to it). label is
// a short human description, typically the user's prompt.
func (s *Store) Create(ctx context.Context, sessionID string, seq int, label string) (*Checkpoint, error) {
	cp := &Checkpoint{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Seq:       seq,
		Label:     truncateLabel(label, 120),
		CreatedAt: time.Now(),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (id, session_id, seq, label, created_at) VALUES (?, ?, ?, ?, ?)`,
		cp.ID, cp.SessionID, cp.Seq, cp.Label, cp.CreatedAt.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("insert checkpoint: %w", err)
	}
	return cp, nil
}

// recordFile stores a single captured file. It is best-effort and used by the
// Snapshotter; a primary-key conflict (the file was already captured this turn)
// is ignored.
func (s *Store) recordFile(checkpointID, path string, existed bool, content []byte) error {
	existedInt := 0
	if existed {
		existedInt = 1
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO checkpoint_files (checkpoint_id, path, existed, content) VALUES (?, ?, ?, ?)`,
		checkpointID, path, existedInt, content)
	return err
}

// List returns checkpoints for a session, most recent first, with file counts.
func (s *Store) List(ctx context.Context, sessionID string) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.session_id, c.seq, c.label, c.created_at,
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
		if err := rows.Scan(&cp.ID, &cp.SessionID, &cp.Seq, &cp.Label, &created, &cp.FileCount); err != nil {
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
SELECT c.id, c.session_id, c.seq, c.label, c.created_at,
       (SELECT COUNT(*) FROM checkpoint_files f WHERE f.checkpoint_id = c.id)
FROM checkpoints c WHERE c.id = ?`, id)
	var cp Checkpoint
	var created int64
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.Seq, &cp.Label, &created, &cp.FileCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	cp.CreatedAt = time.UnixMilli(created)
	return &cp, nil
}

// files returns the captured file snapshots for a checkpoint.
func (s *Store) files(ctx context.Context, checkpointID string) ([]FileSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, existed, content FROM checkpoint_files WHERE checkpoint_id = ?`, checkpointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileSnapshot
	for rows.Next() {
		var fs FileSnapshot
		var existedInt int
		if err := rows.Scan(&fs.Path, &existedInt, &fs.Content); err != nil {
			return nil, err
		}
		fs.Existed = existedInt == 1
		out = append(out, fs)
	}
	return out, rows.Err()
}

// RestoreFiles writes each captured file in the checkpoint back to its
// pre-turn content. Files that did not exist before the turn are deleted. It
// returns the number of files restored. Best-effort per file: an error on one
// file is recorded but does not stop the others.
func (s *Store) RestoreFiles(ctx context.Context, checkpointID string) (int, error) {
	snaps, err := s.files(ctx, checkpointID)
	if err != nil {
		return 0, err
	}
	var restored int
	var firstErr error
	for _, fs := range snaps {
		if fs.Existed {
			if err := os.WriteFile(fs.Path, fs.Content, 0o644); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
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

// NewSnapshotter returns a Snapshotter that captures pre-modification file
// content into the given checkpoint.
func (s *Store) NewSnapshotter(checkpointID string) *Snapshotter {
	return &Snapshotter{store: s, checkpointID: checkpointID, captured: make(map[string]bool)}
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
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.captured[absPath] {
		s.mu.Unlock()
		return
	}
	s.captured[absPath] = true
	s.mu.Unlock()

	info, err := os.Stat(absPath)
	if err != nil {
		// File does not exist yet: record it as newly created so a rewind
		// deletes it.
		_ = s.store.recordFile(s.checkpointID, absPath, false, nil)
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
	_ = s.store.recordFile(s.checkpointID, absPath, true, data)
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
