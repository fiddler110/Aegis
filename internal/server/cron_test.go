package server

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/task"

	_ "modernc.org/sqlite"
)

// cronRunFuncTestDeps builds the pieces newCronRunFunc needs, backed by a
// temp-file SQLite DB, without standing up a full daemon.
func cronRunFuncTestDeps(t *testing.T) (*cron.Store, *task.Manager) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	cronStore, err := cron.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	taskStore, err := task.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	taskMgr := task.NewManager(taskStore, logger)
	return cronStore, taskMgr
}

// waitForRun polls cron_runs until at least one record for jobID exists (the
// fire happens on a goroutine started by taskMgr.Start, so there's no
// synchronous handle to wait on directly).
func waitForRun(t *testing.T, store *cron.Store, jobID string) *cron.RunRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := store.ListRuns(context.Background(), jobID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 {
			return runs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a run record for job %s", jobID)
	return nil
}

func TestNewCronRunFuncRecordsSuccess(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	runCronCmd := func(ctx context.Context, command string, emit func(string)) error {
		emit("all good")
		return nil
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, func() permission.Mode { return permission.ModeAuto }, logger)

	job := cron.Job{ID: "job-ok", Title: "t", Command: "echo hi"}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "ok" {
		t.Errorf("status = %q, want ok", rec.Status)
	}
	if rec.Output != "all good" {
		t.Errorf("output = %q, want %q", rec.Output, "all good")
	}
	if rec.JobID != job.ID {
		t.Errorf("job id = %q, want %q", rec.JobID, job.ID)
	}
	if rec.FiredAt.IsZero() {
		t.Error("expected non-zero fired-at")
	}
}

func TestNewCronRunFuncRecordsExecutionError(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	runCronCmd := func(ctx context.Context, command string, emit func(string)) error {
		emit("partial output before failure")
		return errors.New("boom: command exited 1")
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, func() permission.Mode { return permission.ModeAuto }, logger)

	job := cron.Job{ID: "job-fail", Title: "t", Command: "false"}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "error" {
		t.Errorf("status = %q, want error", rec.Status)
	}
	if rec.Output != "partial output before failure" {
		t.Errorf("output = %q, want the captured partial output", rec.Output)
	}
}

func TestNewCronRunFuncRecordsBlockedByPermissionMode(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	runCronCmd := func(ctx context.Context, command string, emit func(string)) error {
		called = true
		return nil
	}
	// Plan mode denies CapExecute outright (FIND-03/P24.3).
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, func() permission.Mode { return permission.ModePlan }, logger)

	job := cron.Job{ID: "job-denied", Title: "t", Command: "echo hi"}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "blocked" {
		t.Errorf("status = %q, want blocked", rec.Status)
	}
	if called {
		t.Error("expected the command not to run when the permission mode denies execution")
	}
}

func TestNewCronRunFuncRecordsBlockedByAskWithoutAutoApprove(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	runCronCmd := func(ctx context.Context, command string, emit func(string)) error {
		called = true
		return nil
	}
	// Build mode asks for CapExecute approval; with no interactive human
	// present at fire time and no auto_approve on the job, this must be
	// blocked rather than silently allowed (FIND-03/P24.3).
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, func() permission.Mode { return permission.ModeBuild }, logger)

	job := cron.Job{ID: "job-ask-denied", Title: "t", Command: "echo hi", AutoApprove: false}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "blocked" {
		t.Errorf("status = %q, want blocked", rec.Status)
	}
	if called {
		t.Error("expected the command not to run without auto_approve in build mode")
	}
}

func TestNewCronRunFuncRunsWhenAskWithAutoApprove(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	runCronCmd := func(ctx context.Context, command string, emit func(string)) error {
		emit("ran despite build mode")
		return nil
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, func() permission.Mode { return permission.ModeBuild }, logger)

	job := cron.Job{ID: "job-ask-approved", Title: "t", Command: "echo hi", AutoApprove: true}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "ok" {
		t.Errorf("status = %q, want ok", rec.Status)
	}
	if rec.Output != "ran despite build mode" {
		t.Errorf("output = %q, want %q", rec.Output, "ran despite build mode")
	}
}
