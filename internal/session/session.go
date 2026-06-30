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

	"github.com/google/uuid"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/trace"

	_ "modernc.org/sqlite"
)

// Session is a stored conversation.
type Session struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	System       string             `json:"system"`
	Mode         string             `json:"mode"`
	Persona      string             `json:"persona"`
	Messages     []provider.Message `json:"messages"`
	Traces       []trace.TurnTrace  `json:"traces,omitempty"`
	InputTokens  int                `json:"input_tokens"`
	OutputTokens int                `json:"output_tokens"`
	CostUSD      float64            `json:"cost_usd"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// Meta is a session without its message body, for listings.
type Meta struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Mode         string    `json:"mode"`
	Persona      string    `json:"persona"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
	} {
		_, _ = s.db.Exec(col) // "duplicate column name" error expected on fresh schema
	}
	return nil
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
		`SELECT id, title, system, mode, persona, messages, traces, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var (
		sess         Session
		msgBlob      []byte
		traceBlob    []byte
		created, upd int64
	)
	if err := row.Scan(&sess.ID, &sess.Title, &sess.System, &sess.Mode, &sess.Persona, &msgBlob, &traceBlob,
		&sess.InputTokens, &sess.OutputTokens, &sess.CostUSD, &created, &upd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	msgs, err := provider.UnmarshalMessages(msgBlob)
	if err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}
	sess.Messages = msgs
	if len(traceBlob) > 0 {
		if err := json.Unmarshal(traceBlob, &sess.Traces); err != nil {
			return nil, fmt.Errorf("decode traces: %w", err)
		}
	}
	sess.CreatedAt = time.UnixMilli(created)
	sess.UpdatedAt = time.UnixMilli(upd)
	return &sess, nil
}

// AppendTraces appends per-turn trace records to a session's trace log. It is a
// read-modify-write within a transaction so concurrent runs on different
// sessions stay isolated (and the store serializes writes on a single
// connection). A nil/empty slice is a no-op.
func (s *Store) AppendTraces(ctx context.Context, id string, ts []trace.TurnTrace) error {
	if len(ts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var blob []byte
	if err := tx.QueryRowContext(ctx, `SELECT traces FROM sessions WHERE id = ?`, id).Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var existing []trace.TurnTrace
	if len(blob) > 0 {
		if err := json.Unmarshal(blob, &existing); err != nil {
			return fmt.Errorf("decode traces: %w", err)
		}
	}
	existing = append(existing, ts...)
	out, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET traces = ?, updated_at = ? WHERE id = ?`,
		out, time.Now().UnixMilli(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveMessages persists the message list and bumps updated_at.
func (s *Store) SaveMessages(ctx context.Context, id string, msgs []provider.Message) error {
	blob, err := provider.MarshalMessages(msgs)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET messages = ?, updated_at = ? WHERE id = ?`,
		blob, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTitle updates a session's title.
func (s *Store) SetTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, time.Now().UnixMilli(), id)
	return err
}

// List returns session metadata, most recently updated first.
func (s *Store) List(ctx context.Context) ([]Meta, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, mode, persona, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Meta
	for rows.Next() {
		var m Meta
		var created, upd int64
		if err := rows.Scan(&m.ID, &m.Title, &m.Mode, &m.Persona, &m.InputTokens, &m.OutputTokens, &m.CostUSD, &created, &upd); err != nil {
			return nil, err
		}
		m.CreatedAt = time.UnixMilli(created)
		m.UpdatedAt = time.UnixMilli(upd)
		out = append(out, m)
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

// SetSystem updates a session's system prompt.
func (s *Store) SetSystem(ctx context.Context, id, system string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET system = ?, updated_at = ? WHERE id = ?`,
		system, time.Now().UnixMilli(), id)
	return err
}

// SetMode updates a session's permission mode.
func (s *Store) SetMode(ctx context.Context, id, mode string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET mode = ?, updated_at = ? WHERE id = ?`,
		mode, time.Now().UnixMilli(), id)
	return err
}

// Delete removes a session.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}
