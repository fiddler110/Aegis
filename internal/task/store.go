package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store persists tasks in the shared SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore creates a task store over db and ensures its table exists.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT 'generic',
    title      TEXT NOT NULL DEFAULT '',
    state      TEXT NOT NULL DEFAULT 'running',
    output     TEXT NOT NULL DEFAULT '',
    error      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);`)
	return err
}

// Save upserts a task.
func (s *Store) Save(ctx context.Context, t *Task) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (id, session_id, kind, title, state, output, error, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    session_id = excluded.session_id,
    kind       = excluded.kind,
    title      = excluded.title,
    state      = excluded.state,
    output     = excluded.output,
    error      = excluded.error,
    updated_at = excluded.updated_at`,
		t.ID, t.SessionID, t.Kind, t.Title, string(t.State), t.Output, t.Error,
		t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}
	return nil
}

// Get loads a task by id.
func (s *Store) Get(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, kind, title, state, output, error, created_at, updated_at
FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// List returns tasks for a session (all when sessionID is empty), newest first.
func (s *Store) List(ctx context.Context, sessionID string) ([]*Task, error) {
	var (
		rows *sql.Rows
		err  error
	)
	const cols = `id, session_id, kind, title, state, output, error, created_at, updated_at`
	if sessionID == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT `+cols+` FROM tasks ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT `+cols+` FROM tasks WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scanner abstracts *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(sc scanner) (*Task, error) {
	var (
		t            Task
		state        string
		created, upd int64
	)
	if err := sc.Scan(&t.ID, &t.SessionID, &t.Kind, &t.Title, &state, &t.Output, &t.Error, &created, &upd); err != nil {
		return nil, err
	}
	t.State = State(state)
	t.CreatedAt = time.UnixMilli(created)
	t.UpdatedAt = time.UnixMilli(upd)
	return &t, nil
}
