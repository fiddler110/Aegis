// Package session persists conversations so they survive client restarts,
// matching the opencode model where the daemon owns durable sessions.
package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sqlitestore"
	"github.com/fiddler110/aegis/internal/trace"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

// Session is a stored conversation.
type Session struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	System       string             `json:"system"`
	Mode         string             `json:"mode"`
	Persona      string             `json:"persona"`
	Workdir      string             `json:"workdir,omitempty"`    // P25.1: session's working directory; "" = daemon's default workspace
	Model        string             `json:"model,omitempty"`      // P14.7: per-session model override; "" = persona/global default
	Background   bool               `json:"background,omitempty"` // P3.2: runs detached from TUI
	Archived     bool               `json:"archived,omitempty"`   // soft-deleted; hidden from normal listings
	Messages     []provider.Message `json:"messages"`
	Traces       []trace.TurnTrace  `json:"traces,omitempty"`
	InputTokens  int                `json:"input_tokens"`
	OutputTokens int                `json:"output_tokens"`
	CostUSD      float64            `json:"cost_usd"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	ArchivedAt   *time.Time         `json:"archived_at,omitempty"`
}

// Meta is a session without its message body, for listings.
type Meta struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Mode         string     `json:"mode"`
	Persona      string     `json:"persona"`
	Workdir      string     `json:"workdir,omitempty"`    // P25.1
	Model        string     `json:"model,omitempty"`      // P14.7
	Background   bool       `json:"background,omitempty"` // P3.2
	Archived     bool       `json:"archived,omitempty"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	CostUSD      float64    `json:"cost_usd"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
}

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")

// Store persists sessions in SQLite.
type Store struct {
	db          *sql.DB
	checkpoints CheckpointCleaner

	// bg_events retention (P66.9). bgMu guards both fields.
	bgMu         sync.Mutex
	bgRetention  int
	bgSincePrune map[string]int
}

// DefaultBGEventRetention is how many bg_events rows a single session keeps.
// Older rows are dropped as new ones arrive (P66.9).
//
// The bound exists because bg_events had no bound at all: the rows were
// removed only when the whole session was deleted, and the automatic pruner
// that does that is gated on Cleanup.SessionTTLDays, which has no default. On
// a default install the table therefore grew for the life of the install,
// storing text that session_messages already holds whole. So this cap must
// stay independent of any configuration — an unconfigured daemon gets it.
//
// The rows are a catch-up buffer, not a transcript: a detached run's events
// are replayed by a client that reattaches via GET /sessions/{id}/events?since=N
// while the run is in flight. The durable record of what the run produced is
// session_messages. 2,000 events is comfortably more than one long turn emits
// once text deltas are coalesced (see the detached-run send wrapper in
// internal/server/messages.go), so a client that reattaches to a live run
// still catches up in full; what is lost is only the far history of an old
// run, which is exactly the part that duplicates session_messages.
const DefaultBGEventRetention = 2000

// bgPruneInterval is how many appends a session makes between retention
// sweeps. Pruning on every insert would add a second write to every event;
// sweeping periodically keeps the worst-case row count at
// retention+bgPruneInterval-1 while leaving the common append a single INSERT.
//
// The counter is per session and in memory, so a restart forgets it — which is
// why the first append a session makes in a given process always sweeps
// (see shouldPruneBGEvents). Without that, a session appending fewer than
// bgPruneInterval events per daemon lifetime would accumulate across restarts
// and never be swept, which is the unbounded-growth bug in a slower form.
const bgPruneInterval = 128

// bgPruneCounterCap bounds the in-memory sweep-counter map. Sessions come and
// go (and Delete removes its own entry), but a long-lived daemon that runs
// many short background sessions would otherwise keep one int per session ID
// forever. Dropping the whole map costs nothing but an extra sweep on each
// surviving session's next append.
const bgPruneCounterCap = 4096

// SetBGEventRetention overrides the per-session bg_events cap for this store.
// n <= 0 restores DefaultBGEventRetention. There is no config key for this —
// the point of the bound is that it applies to an unconfigured install — so
// this exists for tests and for embedders with an unusual event volume.
func (s *Store) SetBGEventRetention(n int) {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if n <= 0 {
		n = DefaultBGEventRetention
	}
	s.bgRetention = n
}

// CheckpointCleaner deletes checkpoint snapshots for a session. Session
// storage doesn't own checkpoint tables directly (see internal/checkpoint,
// which shares this store's *sql.DB) but must fan out deletion to them so
// Delete/Prune don't leave orphaned snapshots behind (P32.3) — previously
// only the HTTP delete-session handler did this, so the TTL auto-pruner and
// the /sessions/prune endpoint silently leaked checkpoint data forever.
type CheckpointCleaner interface {
	DeleteForSession(ctx context.Context, sessionID string) error
}

// SetCheckpointCleaner wires a checkpoint store's cleanup into this store's
// Delete/Prune paths. Call once during daemon startup, after both stores are
// constructed. Nil (the default, and every existing session.Store used in
// tests that don't construct a checkpoint store) makes cleanup a no-op.
func (s *Store) SetCheckpointCleaner(c CheckpointCleaner) {
	s.checkpoints = c
}

// Open opens (and migrates) the session store at path.
func Open(path string) (*Store, error) {
	// CLN-3: this used to re-implement sqlitestore.Open outright — same DSN
	// constant, same SetMaxOpenConns(1), same WAL pragma — and was the only
	// one of the three stores that did not create its parent directory.
	db, err := sqlitestore.Open(path, "session")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, bgRetention: DefaultBGEventRetention, bgSincePrune: map[string]int{}}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := sqlitestore.HardenPermissions(path, "session"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying database handle so sibling subsystems (background
// tasks, cron) can persist their own tables on the same single SQLite
// connection, preserving the serialized-writes guarantee.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    title        TEXT    NOT NULL DEFAULT '',
    system       TEXT    NOT NULL DEFAULT '',
    mode         TEXT    NOT NULL DEFAULT 'plan',
    persona      TEXT    NOT NULL DEFAULT '',
    messages     BLOB    NOT NULL DEFAULT '[]',
    traces       BLOB    NOT NULL DEFAULT '[]',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd     REAL    NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS bg_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT    NOT NULL,
    data       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_bg_events_session ON bg_events(session_id, id);
CREATE TABLE IF NOT EXISTS session_messages (
    session_id TEXT    NOT NULL,
    seq        INTEGER NOT NULL,
    data       BLOB    NOT NULL,
    PRIMARY KEY (session_id, seq)
);
CREATE TABLE IF NOT EXISTS session_traces (
    session_id TEXT    NOT NULL,
    seq        INTEGER NOT NULL,
    data       BLOB    NOT NULL,
    PRIMARY KEY (session_id, seq)
);
CREATE TABLE IF NOT EXISTS daily_cost (
    day      TEXT PRIMARY KEY, -- UTC date, YYYY-MM-DD
    cost_usd REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS daily_tokens (
    day    TEXT PRIMARY KEY, -- UTC date, YYYY-MM-DD
    tokens INTEGER NOT NULL DEFAULT 0
);`); err != nil {
		return err
	}
	// Idempotent additions for existing databases (errors silently ignored).
	for _, col := range []string{
		`ALTER TABLE sessions ADD COLUMN input_tokens  INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN cost_usd      REAL    NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN traces        BLOB    NOT NULL DEFAULT '[]'`,
		`ALTER TABLE sessions ADD COLUMN persona TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN background INTEGER NOT NULL DEFAULT 0`, // P3.2
		`ALTER TABLE sessions ADD COLUMN archived_at INTEGER`,                   // NULL = active
		`ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`,        // P14.7: per-session override
		`ALTER TABLE sessions ADD COLUMN workdir TEXT NOT NULL DEFAULT ''`,      // P25.1: per-session working directory
	} {
		_, _ = s.db.Exec(col) // "duplicate column name" error expected on fresh schema
	}
	// P8.1: one-time backfill of legacy whole-blob messages/traces into
	// row-per-message/row-per-trace tables, so the hot per-turn path can
	// append instead of read-modify-write. Cheap no-op on repeat startups:
	// migrated sessions have their legacy blob columns reset to '[]'.
	return s.migrateLegacyBlobs()
}

// migrateLegacyBlobs moves any pre-P8.1 whole-blob messages/traces into
// session_messages/session_traces, one row per message/trace. Runs once per
// session (skipped once its legacy blob column reads back as '[]').
func (s *Store) migrateLegacyBlobs() error {
	rows, err := s.db.Query(`SELECT id, messages, traces FROM sessions WHERE messages != '[]' OR traces != '[]'`)
	if err != nil {
		return err
	}
	type legacy struct {
		id           string
		msgs, traces []byte
	}
	var pending []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.id, &l.msgs, &l.traces); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, l := range pending {
		if err := s.migrateSessionBlobs(l.id, l.msgs, l.traces); err != nil {
			return fmt.Errorf("migrate legacy blobs for session %s: %w", l.id, err)
		}
	}
	return nil
}

func (s *Store) migrateSessionBlobs(id string, msgBlob, traceBlob []byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var msgCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, id).Scan(&msgCount); err != nil {
		return err
	}
	if msgCount == 0 {
		msgs, err := provider.UnmarshalMessages(msgBlob)
		if err != nil {
			return fmt.Errorf("decode legacy messages: %w", err)
		}
		for i, m := range msgs {
			data, err := json.Marshal(m)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO session_messages (session_id, seq, data) VALUES (?, ?, ?)`, id, i, data); err != nil {
				return err
			}
		}
	}

	var traceCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM session_traces WHERE session_id = ?`, id).Scan(&traceCount); err != nil {
		return err
	}
	if traceCount == 0 {
		var traces []trace.TurnTrace
		if len(traceBlob) > 0 {
			if err := json.Unmarshal(traceBlob, &traces); err != nil {
				return fmt.Errorf("decode legacy traces: %w", err)
			}
		}
		for i, t := range traces {
			data, err := json.Marshal(t)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO session_traces (session_id, seq, data) VALUES (?, ?, ?)`, id, i, data); err != nil {
				return err
			}
		}
	}

	if _, err := tx.Exec(`UPDATE sessions SET messages = '[]', traces = '[]' WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Create stores a new session and returns it. workdir is the session's own
// working directory (P25.1); "" keeps the daemon's default workspace.
func (s *Store) Create(ctx context.Context, title, system, mode, persona, workdir string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:        uuid.NewString(),
		Title:     title,
		System:    system,
		Mode:      mode,
		Persona:   persona,
		Workdir:   workdir,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, system, mode, persona, workdir, messages, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?)`,
		sess.ID, sess.Title, sess.System, sess.Mode, sess.Persona, sess.Workdir, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// Get loads a full session by id.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, system, mode, persona, workdir, model, background, archived_at, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var (
		sess         Session
		created, upd int64
		background   int
		archivedAtMS sql.NullInt64
	)
	if err := row.Scan(&sess.ID, &sess.Title, &sess.System, &sess.Mode, &sess.Persona, &sess.Workdir, &sess.Model, &background, &archivedAtMS,
		&sess.InputTokens, &sess.OutputTokens, &sess.CostUSD, &created, &upd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sess.Background = background == 1
	if archivedAtMS.Valid {
		t := time.UnixMilli(archivedAtMS.Int64)
		sess.ArchivedAt = &t
		sess.Archived = true
	}
	msgs, err := s.loadMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.Messages = msgs
	traces, err := s.loadTraces(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.Traces = traces
	sess.CreatedAt = time.UnixMilli(created)
	sess.UpdatedAt = time.UnixMilli(upd)
	return &sess, nil
}

func (s *Store) loadMessages(ctx context.Context, id string) ([]provider.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM session_messages WHERE session_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.Message
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var m provider.Message
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("decode message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) loadTraces(ctx context.Context, id string) ([]trace.TurnTrace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM session_traces WHERE session_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []trace.TurnTrace
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var t trace.TurnTrace
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("decode trace: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AppendTraces appends per-turn trace records to a session's trace log as new
// rows (P8.1) — no read-modify-write of prior traces. A nil/empty slice is a
// no-op.
func (s *Store) AppendTraces(ctx context.Context, id string, ts []trace.TurnTrace) error {
	if len(ts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}

	var base int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq) + 1, 0) FROM session_traces WHERE session_id = ?`, id).Scan(&base); err != nil {
		return err
	}
	for i, t := range ts {
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_traces (session_id, seq, data) VALUES (?, ?, ?)`, id, base+i, data); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UnixMilli(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendMessages appends new messages to a session's transcript as new rows
// (P8.1) without rewriting existing ones — the per-turn hot path, so cost is
// O(new messages) rather than O(total messages). Use SaveMessages instead
// when the earlier history itself changed (e.g. rewind/truncation). A
// nil/empty slice still bumps updated_at.
func (s *Store) AppendMessages(ctx context.Context, id string, msgs []provider.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var base int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq) + 1, 0) FROM session_messages WHERE session_id = ?`, id).Scan(&base); err != nil {
		return err
	}
	for i, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_messages (session_id, seq, data) VALUES (?, ?, ?)`, id, base+i, data); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SaveMessages replaces a session's entire message transcript. This is a full
// rewrite — used for rewind/truncation where earlier history changes; the
// per-turn hot path uses AppendMessages instead (P8.1).
func (s *Store) SaveMessages(ctx context.Context, id string, msgs []provider.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	res, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, id); err != nil {
		return err
	}
	for i, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_messages (session_id, seq, data) VALUES (?, ?, ?)`, id, i, data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTitle updates a session's title.
func (s *Store) SetTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, time.Now().UnixMilli(), id)
	return err
}

// List returns active (non-archived) session metadata, most recently updated first.
func (s *Store) List(ctx context.Context) ([]Meta, error) {
	return s.listSessions(ctx, false)
}

// ListAll returns all sessions including archived ones, most recently updated first.
func (s *Store) ListAll(ctx context.Context) ([]Meta, error) {
	return s.listSessions(ctx, true)
}

func (s *Store) listSessions(ctx context.Context, includeArchived bool) ([]Meta, error) {
	q := `SELECT id, title, mode, persona, workdir, model, background, archived_at, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Meta
	for rows.Next() {
		var m Meta
		var created, upd int64
		var background int
		var archivedAtMS sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Title, &m.Mode, &m.Persona, &m.Workdir, &m.Model, &background, &archivedAtMS, &m.InputTokens, &m.OutputTokens, &m.CostUSD, &created, &upd); err != nil {
			return nil, err
		}
		m.Background = background == 1
		if archivedAtMS.Valid {
			t := time.UnixMilli(archivedAtMS.Int64)
			m.ArchivedAt = &t
			m.Archived = true
		}
		m.CreatedAt = time.UnixMilli(created)
		m.UpdatedAt = time.UnixMilli(upd)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetBackground marks a session as a background (detached) session (P3.2).
func (s *Store) SetBackground(ctx context.Context, id string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET background = ?, updated_at = ? WHERE id = ?`,
		v, time.Now().UnixMilli(), id)
	return err
}

// Archive soft-deletes a session; it is hidden from normal listings but preserved.
func (s *Store) Archive(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET archived_at = ? WHERE id = ?`,
		time.Now().UnixMilli(), id)
	return err
}

// Unarchive restores a previously archived session to active status.
func (s *Store) Unarchive(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET archived_at = NULL WHERE id = ?`, id)
	return err
}

// Prune deletes non-archived sessions whose updated_at is older than olderThan,
// along with their bg_events rows and (via SetCheckpointCleaner) checkpoint
// snapshots (P32.3 — this path previously bypassed checkpoint cleanup
// entirely, since only the HTTP delete-session handler called it). Returns
// the number of sessions deleted.
func (s *Store) Prune(ctx context.Context, olderThan time.Duration) (int, error) {
	threshold := time.Now().Add(-olderThan).UnixMilli()
	const pred = `archived_at IS NULL AND updated_at < ?`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var ids []string
	if s.checkpoints != nil {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE `+pred, threshold)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		rows.Close()
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_messages WHERE session_id IN (SELECT id FROM sessions WHERE `+pred+`)`, threshold); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_traces WHERE session_id IN (SELECT id FROM sessions WHERE `+pred+`)`, threshold); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM bg_events WHERE session_id IN (SELECT id FROM sessions WHERE `+pred+`)`, threshold); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE `+pred, threshold)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if s.checkpoints != nil {
		for _, id := range ids {
			if err := s.checkpoints.DeleteForSession(ctx, id); err != nil {
				slog.Default().Warn("prune session checkpoints", "session", id, "err", err)
			}
		}
	}
	return int(n), nil
}

// BGEvent is one buffered engine event for a background session.
type BGEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Data      string `json:"data"`
}

// AppendBGEvent saves an event JSON payload for a background session, and
// periodically drops that session's oldest events beyond the retention bound
// (P66.9 — see DefaultBGEventRetention for why the bound is unconditional).
// A failed sweep is logged, not returned: the append itself succeeded, and the
// caller's event must not be reported as lost because housekeeping missed.
func (s *Store) AppendBGEvent(ctx context.Context, sessionID, data string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO bg_events (session_id, data, created_at) VALUES (?, ?, ?)`,
		sessionID, data, time.Now().UnixMilli()); err != nil {
		return err
	}
	if keep, ok := s.shouldPruneBGEvents(sessionID); ok {
		if err := s.pruneBGEvents(ctx, sessionID, keep); err != nil {
			slog.Default().Warn("prune bg_events", "session", sessionID, "err", err)
		}
	}
	return nil
}

// shouldPruneBGEvents reports whether this append should sweep the session's
// bg_events, and the row count to keep if so. The first append a session makes
// in this process always sweeps; after that, every bgPruneInterval-th one does.
func (s *Store) shouldPruneBGEvents(sessionID string) (int, bool) {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	keep := s.bgRetention
	if keep <= 0 {
		keep = DefaultBGEventRetention
	}
	if s.bgSincePrune == nil {
		s.bgSincePrune = map[string]int{}
	}
	if len(s.bgSincePrune) > bgPruneCounterCap {
		s.bgSincePrune = map[string]int{}
	}
	n, seen := s.bgSincePrune[sessionID]
	if !seen || n+1 >= bgPruneInterval {
		s.bgSincePrune[sessionID] = 0
		return keep, true
	}
	s.bgSincePrune[sessionID] = n + 1
	return keep, false
}

// pruneBGEvents deletes everything but the newest keep events for a session.
// The subquery finds the id of the keep-th newest row and deletes at or below
// it, which the (session_id, id) index serves as a bounded reverse scan — no
// COUNT(*) over the table and no work at all when the session is under the
// bound (the subquery returns NULL and the delete matches nothing).
func (s *Store) pruneBGEvents(ctx context.Context, sessionID string, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bg_events WHERE session_id = ? AND id <= (
		     SELECT id FROM bg_events WHERE session_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?
		 )`,
		sessionID, sessionID, keep)
	return err
}

// ListBGEvents returns buffered events for a session with id > since.
func (s *Store) ListBGEvents(ctx context.Context, sessionID string, since int64) ([]BGEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, data FROM bg_events WHERE session_id = ? AND id > ? ORDER BY id ASC`,
		sessionID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BGEvent
	for rows.Next() {
		var e BGEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Data); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddUsage accumulates token counts and cost for a session (safe for concurrent
// calls from separate goroutines — SQLite write serialization handles it).
func (s *Store) AddUsage(ctx context.Context, id string, inputTokens, outputTokens int, costUSD float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET input_tokens = input_tokens + ?, output_tokens = output_tokens + ?, cost_usd = cost_usd + ?, updated_at = ? WHERE id = ?`,
		inputTokens, outputTokens, costUSD, time.Now().UnixMilli(), id)
	return err
}

// AddDailyCost accumulates cost against the current UTC day, for the
// cross-session daily spend cap (P9.5). Safe for concurrent calls.
func (s *Store) AddDailyCost(ctx context.Context, costUSD float64) error {
	day := time.Now().UTC().Format("2006-01-02")
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO daily_cost (day, cost_usd) VALUES (?, ?)
		 ON CONFLICT(day) DO UPDATE SET cost_usd = cost_usd + excluded.cost_usd`,
		day, costUSD)
	return err
}

// TodayCost returns accumulated spend across all sessions for the current UTC
// day (0 if nothing has been spent yet today).
func (s *Store) TodayCost(ctx context.Context) (float64, error) {
	day := time.Now().UTC().Format("2006-01-02")
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT cost_usd FROM daily_cost WHERE day = ?`, day).Scan(&cost)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return cost, err
}

// AddDailyTokens accumulates token counts against the current UTC day, for
// the cross-session daily token cap (P10.5) — the always-enforceable
// counterpart to AddDailyCost, since token counts are present even when a
// turn's usage was estimated or its model unpriced. Safe for concurrent calls.
func (s *Store) AddDailyTokens(ctx context.Context, tokens int) error {
	day := time.Now().UTC().Format("2006-01-02")
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO daily_tokens (day, tokens) VALUES (?, ?)
		 ON CONFLICT(day) DO UPDATE SET tokens = tokens + excluded.tokens`,
		day, tokens)
	return err
}

// TodayTokens returns accumulated tokens across all sessions for the current
// UTC day (0 if none recorded yet today).
func (s *Store) TodayTokens(ctx context.Context) (int, error) {
	day := time.Now().UTC().Format("2006-01-02")
	var tokens int
	err := s.db.QueryRowContext(ctx, `SELECT tokens FROM daily_tokens WHERE day = ?`, day).Scan(&tokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return tokens, err
}

// SetSystem updates a session's system prompt.
func (s *Store) SetSystem(ctx context.Context, id, system string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET system = ?, updated_at = ? WHERE id = ?`,
		system, time.Now().UnixMilli(), id)
	return err
}

// SetPersona updates a session's persona name. The engine resolves the
// persona's profile (model, rules, guard) from this name on each turn.
func (s *Store) SetPersona(ctx context.Context, id, persona string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET persona = ?, updated_at = ? WHERE id = ?`,
		persona, time.Now().UnixMilli(), id)
	return err
}

// SetModel updates a session's model override (P14.7). An empty string
// clears the override, reverting to the persona/global default on the next
// turn. The engine resolves this ahead of the persona's own Model and the
// global provider.model on each turn — it does not itself validate the model
// belongs to the configured provider; an unrecognized id surfaces as a
// provider error on the next turn, same as a bad persona-level Model override.
func (s *Store) SetModel(ctx context.Context, id, model string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET model = ?, updated_at = ? WHERE id = ?`,
		model, time.Now().UnixMilli(), id)
	return err
}

// SetMode updates a session's permission mode.
func (s *Store) SetMode(ctx context.Context, id, mode string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET mode = ?, updated_at = ? WHERE id = ?`,
		mode, time.Now().UnixMilli(), id)
	return err
}

// Delete removes a session and its message/trace/bg_events rows, plus any
// checkpoint snapshots registered via SetCheckpointCleaner (P32.3).
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_traces WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bg_events WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bgMu.Lock()
	delete(s.bgSincePrune, id) // the session's rows are gone; don't keep its sweep counter
	s.bgMu.Unlock()
	if s.checkpoints != nil {
		if err := s.checkpoints.DeleteForSession(ctx, id); err != nil {
			slog.Default().Warn("delete session checkpoints", "session", id, "err", err)
		}
	}
	return nil
}
