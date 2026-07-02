package swarm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TaskStatus is the lifecycle state of a shared team task.
type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"      // available to claim
	TaskClaimed   TaskStatus = "claimed"   // a teammate is working on it
	TaskCompleted TaskStatus = "completed" // finished
)

// TeamTask is one item on a team's shared task list (P5.1). Teammates claim
// open tasks, work them, and mark them completed — the peer-coordination
// primitive that distinguishes agent teams from parent-child subagents.
type TeamTask struct {
	ID          int64      `json:"id"`
	Team        string     `json:"team"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Owner       string     `json:"owner,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskList is a durable, SQLite-backed shared task list keyed by team. It reuses
// the session database handle (like bg_events) so it survives restarts and is
// visible to every in-process teammate.
type TaskList struct {
	db *sql.DB
}

// NewTaskList creates the backing table if needed and returns a TaskList.
func NewTaskList(db *sql.DB) (*TaskList, error) {
	if db == nil {
		return nil, errors.New("swarm: nil db")
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS team_tasks (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	team        TEXT NOT NULL,
	description TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'open',
	owner       TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_team_tasks_team ON team_tasks(team, status, id);`); err != nil {
		return nil, fmt.Errorf("swarm: create team_tasks: %w", err)
	}
	return &TaskList{db: db}, nil
}

// Add appends a task to a team's list and returns its id.
func (t *TaskList) Add(ctx context.Context, team, description string) (int64, error) {
	now := time.Now().UnixMilli()
	res, err := t.db.ExecContext(ctx,
		`INSERT INTO team_tasks (team, description, status, created_at, updated_at) VALUES (?, ?, 'open', ?, ?)`,
		team, description, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// List returns a team's tasks in creation order.
func (t *TaskList) List(ctx context.Context, team string) ([]TeamTask, error) {
	rows, err := t.db.QueryContext(ctx,
		`SELECT id, team, description, status, owner, created_at, updated_at FROM team_tasks WHERE team = ? ORDER BY id`, team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamTask
	for rows.Next() {
		var tk TeamTask
		var created, updated int64
		var status string
		if err := rows.Scan(&tk.ID, &tk.Team, &tk.Description, &status, &tk.Owner, &created, &updated); err != nil {
			return nil, err
		}
		tk.Status = TaskStatus(status)
		tk.CreatedAt = time.UnixMilli(created)
		tk.UpdatedAt = time.UnixMilli(updated)
		out = append(out, tk)
	}
	return out, rows.Err()
}

// Claim atomically assigns the oldest open task in a team to owner and returns
// it. Returns (nil, nil) when no open task is available. The SELECT+UPDATE runs
// in a transaction so two teammates never claim the same task.
func (t *TaskList) Claim(ctx context.Context, team, owner string) (*TeamTask, error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var tk TeamTask
	var created int64
	err = tx.QueryRowContext(ctx,
		`SELECT id, description, created_at FROM team_tasks WHERE team = ? AND status = 'open' ORDER BY id LIMIT 1`,
		team).Scan(&tk.ID, &tk.Description, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'claimed', owner = ?, updated_at = ? WHERE id = ?`,
		owner, now, tk.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tk.Team = team
	tk.Owner = owner
	tk.Status = TaskClaimed
	tk.CreatedAt = time.UnixMilli(created)
	tk.UpdatedAt = time.UnixMilli(now)
	return &tk, nil
}

// Complete marks a task completed. Returns an error if the task does not exist.
func (t *TaskList) Complete(ctx context.Context, id int64) error {
	now := time.Now().UnixMilli()
	res, err := t.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'completed', updated_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("swarm: task %d not found", id)
	}
	return nil
}
