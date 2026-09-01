package cron

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/reqorigin"
	"github.com/google/uuid"
)

// Job is a scheduled command.
type Job struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"` // cron expression
	Command  string `json:"command"`  // shell command to run when due
	Title    string `json:"title"`
	Enabled  bool   `json:"enabled"`
	// AutoApprove opts this job into firing unattended even when the daemon's
	// current permission mode would otherwise require interactive approval for
	// shell execution (build mode). No human is present when a scheduled job
	// fires, so without this explicit opt-in the run is blocked rather than
	// silently auto-approved (FIND-03/P24.3) — mirrors mcp_server.auto_approve.
	AutoApprove bool      `json:"auto_approve"`
	LastRun     time.Time `json:"last_run"` // zero if never run
	Created     time.Time `json:"created"`
	// Workdir is the directory the job's command runs in (P25.8). Empty
	// means the daemon's own working directory, matching the pre-P25.8
	// behavior — jobs always fired in the daemon's cwd regardless of which
	// session created them.
	Workdir string `json:"workdir,omitempty"`
	// Notify opts this job into delivering its fire outcome over the
	// configured notification channels (P58.1). Off by default so existing
	// jobs are unchanged and a minute-by-minute job cannot spam the desktop:
	// it is per-job rather than global because "tell me about this one" is a
	// property of the job, not of the scheduler — a digest job wants delivery
	// on success, a monitoring job that fires every minute wants none.
	// Delivery still requires a channel to be configured (notify.desktop or
	// notify.webhook); with neither, this flag is inert.
	Notify bool `json:"notify"`
	// Origin records which surface created this job (reqorigin), stamped at
	// Create time from the calling turn's context (P81.23/FIND-23) — never
	// taken from a field a caller sets on the job itself. "" for a job
	// created before this column existed.
	Origin string `json:"origin,omitempty"`
	// Confirmed gates a job's very first fire (P81.23/FIND-23): a job created
	// from an interactive surface (reqorigin.Interactive — the TUI or a
	// scripted CLI invocation) is confirmed at creation, since an operator
	// was already present to type cron_create. A job created from any other
	// surface (an editor plugin over ACP, an MCP client, the web UI) starts
	// unconfirmed and the scheduler skips it at every tick until an operator
	// confirms it — the same "no one was present when this was scheduled"
	// gap the rest of this finding addresses for auto_approve, applied to
	// registration itself. A job predating this column defaults to true (see
	// the migration below) so an upgrade doesn't strand existing jobs.
	Confirmed bool `json:"confirmed"`
}

// ErrNotFound is returned for an unknown job id.
var ErrNotFound = errors.New("cron job not found")

// RunRecord is a durable, queryable audit record of one cron fire attempt —
// independent of both the ephemeral slog line the scheduler emits and the
// general task-manager view (which is not cron-scoped and not guaranteed to
// stay queryable by job over time). Addresses FIND-34/P24.9.
type RunRecord struct {
	ID      int64     `json:"id"`
	JobID   string    `json:"job_id"`
	FiredAt time.Time `json:"fired_at"`
	Status  string    `json:"status"` // "ok" | "error" | "blocked"
	Output  string    `json:"output"` // combined stdout/stderr, truncated
}

// maxRunOutputBytes caps how much captured output is persisted per run
// record, so a chatty job can't grow the audit table unboundedly. Mirrors
// guard.maxGuardFileBytes's rationale: keep enough to diagnose a failure,
// truncate the rest with a visible marker.
const maxRunOutputBytes = 8000

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
	// Best-effort migration for DBs created before auto_approve/workdir
	// existed; ALTER TABLE fails harmlessly (ignored) if the column is
	// already there.
	_, _ = db.Exec(`ALTER TABLE cron_jobs ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE cron_jobs ADD COLUMN workdir TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE cron_jobs ADD COLUMN notify INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE cron_jobs ADD COLUMN origin TEXT NOT NULL DEFAULT ''`)
	// Default 1: a job predating this column was already reviewed by whatever
	// process created it under the pre-P81.23 rules — grandfathering it in
	// keeps an upgrade from silently stranding every existing job unconfirmed.
	_, _ = db.Exec(`ALTER TABLE cron_jobs ADD COLUMN confirmed INTEGER NOT NULL DEFAULT 1`)
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS cron_runs (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id   TEXT NOT NULL,
    fired_at INTEGER NOT NULL,
    status   TEXT NOT NULL,
    output   TEXT NOT NULL DEFAULT ''
);`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS cron_runs_job_id_idx ON cron_runs(job_id)`); err != nil {
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
INSERT INTO cron_jobs (id, schedule, command, title, enabled, last_run, created, auto_approve, workdir, notify, origin, confirmed)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    schedule     = excluded.schedule,
    command      = excluded.command,
    title        = excluded.title,
    enabled      = excluded.enabled,
    last_run     = excluded.last_run,
    auto_approve = excluded.auto_approve,
    workdir      = excluded.workdir,
    notify       = excluded.notify,
    origin       = excluded.origin,
    confirmed    = excluded.confirmed`,
		j.ID, j.Schedule, j.Command, j.Title, boolToInt(j.Enabled), last, j.Created.UnixMilli(), boolToInt(j.AutoApprove), j.Workdir, boolToInt(j.Notify), j.Origin, boolToInt(j.Confirmed))
	return err
}

// Get loads a job by id.
func (s *Store) Get(ctx context.Context, id string) (*Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, schedule, command, title, enabled, last_run, created, auto_approve, workdir, notify, origin, confirmed FROM cron_jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

// List returns all jobs, newest first.
func (s *Store) List(ctx context.Context) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, schedule, command, title, enabled, last_run, created, auto_approve, workdir, notify, origin, confirmed FROM cron_jobs ORDER BY created DESC`)
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

// RecordRun persists one fire-attempt outcome for a job (FIND-34/P24.9).
// output is truncated to maxRunOutputBytes with a trailing marker when
// longer, so a chatty or runaway job cannot grow the audit table unboundedly.
func (s *Store) RecordRun(ctx context.Context, jobID string, firedAt time.Time, status, output string) error {
	if len(output) > maxRunOutputBytes {
		output = output[:maxRunOutputBytes] + "\n[...truncated]"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_runs (job_id, fired_at, status, output) VALUES (?, ?, ?, ?)`,
		jobID, firedAt.UnixMilli(), status, output)
	return err
}

// ListRuns returns fire-attempt records, most-recent-first. jobID == ""
// returns runs across all jobs; limit <= 0 means unlimited.
func (s *Store) ListRuns(ctx context.Context, jobID string, limit int) ([]*RunRecord, error) {
	query := `SELECT id, job_id, fired_at, status, output FROM cron_runs`
	var args []any
	if jobID != "" {
		query += ` WHERE job_id = ?`
		args = append(args, jobID)
	}
	query += ` ORDER BY fired_at DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RunRecord
	for rows.Next() {
		var r RunRecord
		var fired int64
		if err := rows.Scan(&r.ID, &r.JobID, &fired, &r.Status, &r.Output); err != nil {
			return nil, err
		}
		r.FiredAt = time.UnixMilli(fired)
		out = append(out, &r)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanJob(sc scanner) (*Job, error) {
	var (
		j                                       Job
		enabled, autoApprove, notify, confirmed int
		last, created                           int64
	)
	if err := sc.Scan(&j.ID, &j.Schedule, &j.Command, &j.Title, &enabled, &last, &created, &autoApprove, &j.Workdir, &notify, &j.Origin, &confirmed); err != nil {
		return nil, err
	}
	j.Enabled = enabled != 0
	j.AutoApprove = autoApprove != 0
	j.Notify = notify != 0
	j.Confirmed = confirmed != 0
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
	store       *Store
	run         RunFunc
	logger      *slog.Logger
	now         func() time.Time // injectable clock for tests
	parsedSched sync.Map         // map[string]*Schedule — avoids re-parsing on every tick

	// autoApproveGuard, when non-nil, is consulted by Create whenever a
	// caller sets autoApprove — a non-nil error refuses job creation
	// (P81.23/FIND-23), mirroring the allow_unsandboxed_auto_exec gate the
	// daemon already applies to auto_approve_exec at startup. nil means no
	// gate is wired (e.g. in tests that don't care about sandbox posture),
	// not "always allowed".
	autoApproveGuard func() error
}

// NewScheduler builds a scheduler over store; run fires a due job.
func NewScheduler(store *Store, run RunFunc, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{store: store, run: run, logger: logger, now: time.Now}
}

// SetAutoApproveGuard installs the fire-time posture gate Create consults
// whenever autoApprove is requested (P81.23/FIND-23). Call once, before the
// scheduler starts accepting Create calls.
func (s *Scheduler) SetAutoApproveGuard(guard func() error) { s.autoApproveGuard = guard }

// Create validates the schedule, persists a new enabled job, and returns it.
// autoApprove opts the job into firing unattended even when the daemon's
// current permission mode would otherwise require approval for shell
// execution (see Job.AutoApprove) — refused outright when autoApproveGuard is
// set and rejects the current sandbox posture (P81.23/FIND-23). workdir is
// the directory the job's command runs in (P25.8); "" falls back to the
// daemon's own working directory at fire time. notify opts the job into
// out-of-band delivery of its fire outcome (P58.1; see Job.Notify). origin is
// the reqorigin value of the surface that called this (P81.23/FIND-23) — a
// job created from a non-interactive surface starts unconfirmed and the
// scheduler skips it until an operator confirms it via Confirm. Kept at the
// end of the signature, after the pre-existing booleans, so none of them
// become adjacent and silently swappable at a call site.
func (s *Scheduler) Create(ctx context.Context, schedule, command, title string, autoApprove bool, workdir string, notify bool, origin string) (*Job, error) {
	if _, err := Parse(schedule); err != nil {
		return nil, err
	}
	if command == "" {
		return nil, fmt.Errorf("cron: command is required")
	}
	if autoApprove && s.autoApproveGuard != nil {
		if err := s.autoApproveGuard(); err != nil {
			return nil, err
		}
	}
	j := &Job{
		ID:          uuid.NewString(),
		Schedule:    schedule,
		Command:     command,
		Title:       title,
		Enabled:     true,
		AutoApprove: autoApprove,
		Created:     s.now(),
		Workdir:     workdir,
		Notify:      notify,
		Origin:      origin,
		Confirmed:   reqorigin.Interactive(origin),
	}
	if err := s.store.Save(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

// Confirm marks a job confirmed (P81.23/FIND-23), clearing it to fire for the
// first time. A no-op (returns nil) if the job is already confirmed.
func (s *Scheduler) Confirm(ctx context.Context, id string) error {
	j, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if j.Confirmed {
		return nil
	}
	j.Confirmed = true
	return s.store.Save(ctx, j)
}

// List returns all jobs.
func (s *Scheduler) List(ctx context.Context) ([]*Job, error) { return s.store.List(ctx) }

// History returns cron fire-attempt audit records, most-recent-first.
// jobID == "" returns history across all jobs; limit <= 0 means unlimited.
func (s *Scheduler) History(ctx context.Context, jobID string, limit int) ([]*RunRecord, error) {
	return s.store.ListRuns(ctx, jobID, limit)
}

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
		sched, err := s.cachedParse(j.Schedule)
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
		if !j.Confirmed {
			// P81.23/FIND-23: a job created from a non-interactive surface
			// (ACP, MCP, the web UI) does not get to fire unattended until an
			// operator confirms it — no run record either, since nothing ran.
			s.logger.Warn("cron: skipping unconfirmed job", "job", j.ID, "title", j.Title, "origin", j.Origin)
			continue
		}
		j.LastRun = now
		if err := s.store.Save(context.Background(), j); err != nil {
			s.logger.Error("cron: save last_run", "job", j.ID, "err", err)
		}
		s.logger.Info("cron: firing job", "job", j.ID, "title", j.Title)
		s.run(*j)
	}
}

// cachedParse returns a parsed Schedule for expr, caching the result so the
// expression is only parsed once per distinct value.
func (s *Scheduler) cachedParse(expr string) (*Schedule, error) {
	if v, ok := s.parsedSched.Load(expr); ok {
		return v.(*Schedule), nil
	}
	sched, err := Parse(expr)
	if err != nil {
		return nil, err
	}
	s.parsedSched.Store(expr, sched)
	return sched, nil
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
