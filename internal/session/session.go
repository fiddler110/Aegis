// Package session persists conversations so they survive client restarts,
// matching the opencode model where the daemon owns durable sessions.
package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
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
	db *sql.DB
}

// Open opens (and migrates) the session store at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writes
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
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

// Create stores a new session and returns it.
func (s *Store) Create(ctx context.Context, title, system, mode, persona string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:        uuid.NewString(),
		Title:     title,
		System:    system,
		Mode:      mode,
		Persona:   persona,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, system, mode, persona, messages, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '[]', ?, ?)`,
		sess.ID, sess.Title, sess.System, sess.Mode, sess.Persona, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// Get loads a full session by id.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, system, mode, persona, background, archived_at, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var (
		sess         Session
		created, upd int64
		background   int
		archivedAtMS sql.NullInt64
	)
	if err := row.Scan(&sess.ID, &sess.Title, &sess.System, &sess.Mode, &sess.Persona, &background, &archivedAtMS,
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
	q := `SELECT id, title, mode, persona, background, archived_at, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions`
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
		if err := rows.Scan(&m.ID, &m.Title, &m.Mode, &m.Persona, &background, &archivedAtMS, &m.InputTokens, &m.OutputTokens, &m.CostUSD, &created, &upd); err != nil {
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

// Prune deletes non-archived sessions whose updated_at is older than olderThan.
// Returns the number of sessions deleted.
func (s *Store) Prune(ctx context.Context, olderThan time.Duration) (int, error) {
	threshold := time.Now().Add(-olderThan).UnixMilli()
	const pred = `archived_at IS NULL AND updated_at < ?`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_messages WHERE session_id IN (SELECT id FROM sessions WHERE `+pred+`)`, threshold); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_traces WHERE session_id IN (SELECT id FROM sessions WHERE `+pred+`)`, threshold); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE `+pred, threshold)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), tx.Commit()
}

// BGEvent is one buffered engine event for a background session.
type BGEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Data      string `json:"data"`
}

// AppendBGEvent saves an event JSON payload for a background session.
func (s *Store) AppendBGEvent(ctx context.Context, sessionID, data string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bg_events (session_id, data, created_at) VALUES (?, ?, ?)`,
		sessionID, data, time.Now().UnixMilli())
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

// SetMode updates a session's permission mode.
func (s *Store) SetMode(ctx context.Context, id, mode string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET mode = ?, updated_at = ? WHERE id = ?`,
		mode, time.Now().UnixMilli(), id)
	return err
}

// Delete removes a session and its message/trace rows.
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
