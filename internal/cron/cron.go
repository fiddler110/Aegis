package cron

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Job is a scheduled command.
type Job struct {
	ID       string    `json:"id"`
	Schedule string    `json:"schedule"` // cron expression
	Command  string    `json:"command"`  // shell command to run when due
	Title    string    `json:"title"`
	Enabled  bool      `json:"enabled"`
	LastRun  time.Time `json:"last_run"` // zero if never run
	Created  time.Time `json:"created"`
}

// ErrNotFound is returned for an unknown job id.
var ErrNotFound = errors.New("cron job not found")

// Store persists cron jobs in the shared SQLite database.
type Store struct{ db *sql.DB }

// NewStore creates a cron store over db and ensures its table exists.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS cron_jobs (
    id        TEXT PRIMARY KEY,
    schedule  TEXT NOT NULL,
    command   TEXT NOT NULL,
    title     TEXT NOT NULL DEFAULT '',
    enabled   INTEGER NOT NULL DEFAULT 1,
    last_run  INTEGER NOT NULL DEFAULT 0,
    created   INTEGER NOT NULL
);`); err != nil {
		return nil, err
	}
	return s, nil
}

// Save upserts a job.
func (s *Store) Save(ctx context.Context, j *Job) error {
	var last int64
	if !j.LastRun.IsZero() {
		last = j.LastRun.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cron_jobs (id, schedule, command, title, enabled, last_run, created)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    schedule = excluded.schedule,
    command  = excluded.command,
    title    = excluded.title,
    enabled  = excluded.enabled,
    last_run = excluded.last_run`,
		j.ID, j.Schedule, j.Command, j.Title, boolToInt(j.Enabled), last, j.Created.UnixMilli())
	return err
}

// Get loads a job by id.
func (s *Store) Get(ctx context.Context, id string) (*Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, schedule, command, title, enabled, last_run, created FROM cron_jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

// List returns all jobs, newest first.
func (s *Store) List(ctx context.Context) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, schedule, command, title, enabled, last_run, created FROM cron_jobs ORDER BY created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Delete removes a job.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanJob(sc scanner) (*Job, error) {
	var (
		j             Job
		enabled       int
		last, created int64
	)
	if err := sc.Scan(&j.ID, &j.Schedule, &j.Command, &j.Title, &enabled, &last, &created); err != nil {
		return nil, err
	}
	j.Enabled = enabled != 0
	if last != 0 {
		j.LastRun = time.UnixMilli(last)
	}
	j.Created = time.UnixMilli(created)
	return &j, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RunFunc fires a due job. The scheduler ignores its result; a typical
// implementation launches the job's command as a background task.
type RunFunc func(j Job)

// Scheduler periodically checks persisted jobs and fires those that are due.
type Scheduler struct {
	store  *Store
	run    RunFunc
	logger *slog.Logger
	now    func() time.Time // injectable clock for tests
}

// NewScheduler builds a scheduler over store; run fires a due job.
func NewScheduler(store *Store, run RunFunc, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{store: store, run: run, logger: logger, now: time.Now}
}

// Create validates the schedule, persists a new enabled job, and returns it.
func (s *Scheduler) Create(ctx context.Context, schedule, command, title string) (*Job, error) {
	if _, err := Parse(schedule); err != nil {
		return nil, err
	}
	if command == "" {
		return nil, fmt.Errorf("cron: command is required")
	}
	j := &Job{
		ID:       uuid.NewString(),
		Schedule: schedule,
		Command:  command,
		Title:    title,
		Enabled:  true,
		Created:  s.now(),
	}
	if err := s.store.Save(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

// List returns all jobs.
func (s *Scheduler) List(ctx context.Context) ([]*Job, error) { return s.store.List(ctx) }

// Delete removes a job.
func (s *Scheduler) Delete(ctx context.Context, id string) error { return s.store.Delete(ctx, id) }

// Toggle flips a job's enabled flag and returns the new state.
func (s *Scheduler) Toggle(ctx context.Context, id string) (bool, error) {
	j, err := s.store.Get(ctx, id)
	if err != nil {
		return false, err
	}
	j.Enabled = !j.Enabled
	if err := s.store.Save(ctx, j); err != nil {
		return false, err
	}
	return j.Enabled, nil
}

// tick fires every enabled job whose schedule matches the given minute and that
// has not already run in that same minute (idempotent within a minute).
func (s *Scheduler) tick(now time.Time) {
	now = now.Truncate(time.Minute)
	jobs, err := s.store.List(context.Background())
	if err != nil {
		s.logger.Error("cron tick: list", "err", err)
		return
	}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		sched, err := Parse(j.Schedule)
		if err != nil {
			s.logger.Warn("cron: bad schedule", "job", j.ID, "schedule", j.Schedule, "err", err)
			continue
		}
		if !sched.Matches(now) {
			continue
		}
		if !j.LastRun.IsZero() && !j.LastRun.Truncate(time.Minute).Before(now) {
			continue // already fired this minute
		}
		j.LastRun = now
		if err := s.store.Save(context.Background(), j); err != nil {
			s.logger.Error("cron: save last_run", "job", j.ID, "err", err)
		}
		s.logger.Info("cron: firing job", "job", j.ID, "title", j.Title)
		s.run(*j)
	}
}

// Run drives the scheduler until ctx is cancelled, checking due jobs once per
// minute (aligned to the minute boundary).
func (s *Scheduler) Run(ctx context.Context) {
	// Align the first tick to the next minute boundary.
	next := s.now().Truncate(time.Minute).Add(time.Minute)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.tick(s.now())
			next = next.Add(time.Minute)
			timer.Reset(time.Until(next))
		}
	}
}
