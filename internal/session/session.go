// Package session persists conversations so they survive client restarts,
// matching the opencode model where the daemon owns durable sessions.
package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/scottymacleod/aegis/internal/provider"

	_ "modernc.org/sqlite"
)

// Session is a stored conversation.
type Session struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	System    string             `json:"system"`
	Mode      string             `json:"mode"`
	Messages  []provider.Message `json:"messages"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// Meta is a session without its message body, for listings.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    system     TEXT NOT NULL DEFAULT '',
    mode       TEXT NOT NULL DEFAULT 'plan',
    messages   BLOB NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);`)
	return err
}

// Create stores a new session and returns it.
func (s *Store) Create(ctx context.Context, title, system, mode string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:        uuid.NewString(),
		Title:     title,
		System:    system,
		Mode:      mode,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, system, mode, messages, created_at, updated_at) VALUES (?, ?, ?, ?, '[]', ?, ?)`,
		sess.ID, sess.Title, sess.System, sess.Mode, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// Get loads a full session by id.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, system, mode, messages, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var (
		sess         Session
		msgBlob      []byte
		created, upd int64
	)
	if err := row.Scan(&sess.ID, &sess.Title, &sess.System, &sess.Mode, &msgBlob, &created, &upd); err != nil {
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
	sess.CreatedAt = time.UnixMilli(created)
	sess.UpdatedAt = time.UnixMilli(upd)
	return &sess, nil
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
		`SELECT id, title, mode, created_at, updated_at FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Meta
	for rows.Next() {
		var m Meta
		var created, upd int64
		if err := rows.Scan(&m.ID, &m.Title, &m.Mode, &created, &upd); err != nil {
			return nil, err
		}
		m.CreatedAt = time.UnixMilli(created)
		m.UpdatedAt = time.UnixMilli(upd)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Delete removes a session.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}
