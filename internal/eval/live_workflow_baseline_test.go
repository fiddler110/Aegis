//go:build live_workflow

package eval

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
)

// TestLiveWorkflowBaseline (P60.4) is the control group TestLiveWorkflow never
// had: it runs the *same* task, in the *same* environment, against the *same*
// model, through Aegis and through a second CLI agent, and attributes an Aegis
// failure to one of them.
//
// The problem it solves is that TestLiveWorkflow measures Aegis and the model
// fused together. A failed assertion there has two readings — "this local model
// is too weak" and "our scaffolding regressed" — and the only thing that has
// ever separated them is having watched the same model pass before, which is no
// help for a model being tried for the first time. Holding the environment
// fixed and swapping the harness is the separation: a task both fail is the
// model, a task only Aegis fails is us.
//
// Only the *outcome* is compared, deliberately. The SSE-shape assertions in
// TestLiveWorkflow (tool-call budget, no `find /` detours, no guard meta-text
// leakage, token accounting) stay Aegis-only, because they read our own event
// stream and have no counterpart in another harness — and because a baseline
// that had to emit them would restrict the comparison to harnesses built like
// ours, which is exactly the bias a control group must not have.
//
// Opt-in on both axes, since a stale baseline is worse than none:
//
//	AEGIS_EVAL_BASELINE_HARNESS='claude -p {prompt}' \
//	  go test -tags live_workflow ./internal/eval/... -run TestLiveWorkflowBaseline -v
//
// Whatever agent is named must be configured to reach the same model server
// this test uses (AEGIS_EVAL_BASE_URL / AEGIS_EVAL_MODEL) — the comparison is
// meaningless against a different model, and Aegis cannot verify that from
// outside the other harness's config. Point them at the same endpoint and say
// so in the run's notes.
func TestLiveWorkflowBaseline(t *testing.T) {
	spec := os.Getenv("AEGIS_EVAL_BASELINE_HARNESS")
	if spec == "" {
		t.Skip("set AEGIS_EVAL_BASELINE_HARNESS (e.g. 'claude -p {prompt}') to run the cross-harness control group")
	}
	baseline, err := NewCLIHarness(spec)
	if err != nil {
		t.Fatalf("baseline harness: %v", err)
	}
	interpreter := findPython(t)

	baseURL := os.Getenv("AEGIS_EVAL_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("AEGIS_EVAL_MODEL")
	if model == "" {
		model = "llama3.2"
	}

	task := SeededBugTask()

	// Each harness gets its own pristine copy of the fixture: they must not be
	// able to observe, or benefit from, each other's work.
	aegisDir := taskDir(t, "aegis")
	baselineDir := taskDir(t, "baseline")

	aegisOutcome := runTaskUnderAegis(t, task, aegisDir, interpreter, baseURL, model)
	t.Log(aegisOutcome)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	baselineOutcome := RunTask(ctx, baseline, task, baselineDir, interpreter)
	t.Log(baselineOutcome)

	verdict, explanation := Compare(aegisOutcome, baselineOutcome)
	t.Logf("verdict: %s — %s", verdict, explanation)

	switch verdict {
	case VerdictOK:
		// Nothing to attribute.
	case VerdictScaffolding:
		t.Error(explanation)
	case VerdictModel, VerdictUnknown:
		// Not a failure of this test: the point of a control group is that it
		// can *decline* to blame the harness. Logged above, and loud enough in
		// -v output to act on, without turning a weak local model into a red
		// build that teaches people to ignore this tier.
		t.Skip(explanation)
	}
}

// taskDir makes a fresh directory for one harness's attempt. Deliberately not
// t.TempDir(): the Aegis run roots a real daemon here, which opens a
// knowledge.db/longmem.db that nothing in this test closes, and t.TempDir()'s
// cleanup turns a locked file into a hard failure (it does on Windows, where an
// open handle blocks deletion). Same reasoning as writeSeededBugFixture.
func taskDir(t *testing.T, label string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aegis-control-"+label+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Logf("cleanup: could not remove %s: %v", dir, rmErr)
		}
	})
	return dir
}

// runTaskUnderAegis runs the task through a real daemon over HTTP+SSE — the
// same seam TestLiveWorkflow uses — and scores it with the task's own portable
// outcome check, so the two harnesses are measured by identical criteria.
func runTaskUnderAegis(t *testing.T, task WorkflowTask, dir, interpreter, baseURL, model string) TaskOutcome {
	t.Helper()
	start := time.Now()
	if err := task.Materialize(dir); err != nil {
		return TaskOutcome{Task: task.Name, Harness: "aegis", Err: err}
	}

	// The daemon roots its workspace at the process cwd, so this test chdirs
	// the same way the harness recipe (`cd <project> && aegis serve`) does.
	// Sequential — no t.Parallel in this file — so a process-wide chdir is safe.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	cl := newLiveWorkflowDaemon(t, baseURL, model, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	outcome := TaskOutcome{Task: task.Name, Harness: "aegis"}
	meta, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		outcome.Err = err
		return outcome
	}
	guardOff := false
	events, err := cl.PostMessageReq(ctx, meta.ID, api.PostMessageRequest{
		Text:         task.Prompt(interpreter),
		GuardEnabled: &guardOff,
	})
	if err != nil {
		outcome.Err = err
		return outcome
	}
	summary := drainWorkflowEvents(t, events)

	outcome = task.Outcome(dir, interpreter)
	outcome.Harness = "aegis"
	outcome.Elapsed = time.Since(start)
	if summary.errText != "" {
		outcome.Failures = append(outcome.Failures, "engine reported an error: "+summary.errText)
		outcome.Passed = false
	}
	return outcome
}
