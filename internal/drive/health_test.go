package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
)

// backendState builds a State wired for the P50.1 backend-recovery
// helpers: a discard errOut/logger and a caller-supplied liveness probe.
func backendState(check func(context.Context) (bool, bool)) *State {
	return &State{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ErrOut:       io.Discard,
		CheckBackend: check,
	}
}

// TestRecoverBackendDown is the P50.1 verdict guard: a backend-unavailable error
// is waited out and, on recovery, resumed; a non-backend error and a backend
// with no liveness probe both fall through to the caller's normal paths; and a
// backend that stays down past the budget stops resumably.
func TestRecoverBackendDown(t *testing.T) {
	down := provider.NewTransportError("ollama", errors.New("connection refused"))
	if !provider.IsBackendUnavailableError(down) {
		t.Fatal("test premise: a connection-refused transport error must be backend-unavailable")
	}
	ctx := context.Background()

	// Not a backend error → not handled here.
	st := backendState(func(context.Context) (bool, bool) { return true, true })
	if got := st.recoverBackendDown(ctx, errors.New("boom"), "phase"); got != backendNotDown {
		t.Errorf("non-backend error = %v, want backendNotDown", got)
	}

	// Backend error but no liveness probe wired → not handled (don't spin).
	st = backendState(nil)
	if got := st.recoverBackendDown(ctx, down, "phase"); got != backendNotDown {
		t.Errorf("nil probe = %v, want backendNotDown", got)
	}

	// Backend error, probe reports unsupported (e.g. cloud adapter) → not handled.
	st = backendState(func(context.Context) (bool, bool) { return false, false })
	if got := st.recoverBackendDown(ctx, down, "phase"); got != backendNotDown {
		t.Errorf("unsupported probe = %v, want backendNotDown", got)
	}

	// Backend error, server already back on the first probe → recovered.
	st = backendState(func(context.Context) (bool, bool) { return true, true })
	if got := st.recoverBackendDown(ctx, down, "phase"); got != backendRecovered {
		t.Errorf("healthy probe = %v, want backendRecovered", got)
	}

	// Backend error, server stays down past a shrunk budget → give up (resumable).
	defer withShrunkBackendBudget(t, 30*time.Millisecond, 2*time.Millisecond)()
	st = backendState(func(context.Context) (bool, bool) { return false, true })
	if got := st.recoverBackendDown(ctx, down, "phase"); got != backendGaveUp {
		t.Errorf("persistently-down backend = %v, want backendGaveUp", got)
	}
}

// TestRecoverBackendDown_CtxCancel: a cancelled context ends the wait promptly
// with a give-up verdict rather than blocking for the whole budget.
func TestRecoverBackendDown_CtxCancel(t *testing.T) {
	defer withShrunkBackendBudget(t, 10*time.Second, 50*time.Millisecond)()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st := backendState(func(context.Context) (bool, bool) { return false, true })
	down := provider.NewStreamError("ollama", "model runner has unexpectedly stopped")
	start := time.Now()
	if got := st.recoverBackendDown(ctx, down, "phase"); got != backendGaveUp {
		t.Errorf("cancelled wait = %v, want backendGaveUp", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a cancelled context must abort the wait promptly, not run the full budget")
	}
}

// TestRecoverPhase6Error folds P50.1 into the phase-6 verdict: backend recovered
// maps to overflowRetry, backend give-up to overflowStop, a plain
// context-overflow still delegates to the P47.7 overflow recovery, and a P52.3
// tool-failure abort delegates to recoverToolFailureStall, and a P57.1
// loop-guard abort delegates to recoverReasoningLoop (returning loopRetry, so
// the caller escalates the retry's prompt as well as resetting it).
func TestRecoverPhase6Error(t *testing.T) {
	ctx := context.Background()
	resets := 0
	tfResets := 0
	loopResets := 0

	// Backend recovered → retry.
	st := backendState(func(context.Context) (bool, bool) { return true, true })
	st.EscalateWindow = func() (int, bool) { return 0, false }
	down := provider.NewTransportError("ollama", errors.New("connection refused"))
	if got := st.recoverPhase6Error(ctx, down, "phase-6 quality pass", &resets, &tfResets, &loopResets); got != overflowRetry {
		t.Errorf("recovered backend = %v, want overflowRetry", got)
	}

	// Backend give-up → stop.
	restore := withShrunkBackendBudget(t, 20*time.Millisecond, 2*time.Millisecond)
	st = backendState(func(context.Context) (bool, bool) { return false, true })
	if got := st.recoverPhase6Error(ctx, down, "phase-6 verify fix", &resets, &tfResets, &loopResets); got != overflowStop {
		t.Errorf("dead backend = %v, want overflowStop", got)
	}
	restore()

	// A context overflow (not a backend error) still flows to overflow recovery.
	st = backendState(func(context.Context) (bool, bool) { return true, true })
	st.EscalateWindow = func() (int, bool) { return 131072, true }
	resets = 0
	overflow := provider.NewContextTruncationError("ollama", "unexpected end of JSON input")
	if got := st.recoverPhase6Error(ctx, overflow, "phase-6 quality pass", &resets, &tfResets, &loopResets); got != overflowRetry {
		t.Errorf("overflow = %v, want overflowRetry (delegated)", got)
	}

	// A P52.3 consecutive-tool-failure abort delegates to the tool-failure
	// recovery and is resumable, spending its own budget rather than the
	// overflow one.
	stall := fmt.Errorf("%w (6 in a row): edit_file keeps failing", engine.ErrToolFailureLimit)
	if got := st.recoverPhase6Error(ctx, stall, "phase-6 verify fix", &resets, &tfResets, &loopResets); got != overflowRetry {
		t.Errorf("tool-failure stall = %v, want overflowRetry (delegated)", got)
	}
	if tfResets != 1 {
		t.Errorf("tool-failure resets = %d, want 1 (must not spend the overflow budget)", tfResets)
	}

	// A P57.1 loop-guard abort delegates to the reasoning-loop recovery, spends
	// its own budget, and asks for the escalated retry rather than a plain one.
	looped := fmt.Errorf("%w: identical tool calls repeated 5 turns", engine.ErrLoopDetected)
	if got := st.recoverPhase6Error(ctx, looped, "phase-6 verify fix", &resets, &tfResets, &loopResets); got != loopRetry {
		t.Errorf("loop abort = %v, want loopRetry (delegated)", got)
	}
	if loopResets != 1 {
		t.Errorf("loop resets = %d, want 1 (must not spend the overflow or tool-failure budget)", loopResets)
	}
	if tfResets != 1 {
		t.Errorf("tool-failure resets = %d after a loop abort, want 1 (unchanged)", tfResets)
	}

	// A plain error is neither backend-down, overflow, a breaker trip, nor a loop → surfaced.
	if got := st.recoverPhase6Error(ctx, errors.New("boom"), "phase-6 verify fix", &resets, &tfResets, &loopResets); got != overflowNotHandled {
		t.Errorf("plain error = %v, want overflowNotHandled", got)
	}
}

// TestRecoverToolFailureStall pins the P52.3 breaker's phase-level treatment:
// the abort is resumable (a fresh context re-read from disk is the right remedy
// for a model re-guessing arguments), bounded by its own budget, and it never
// escalates the serving window — the window is not what failed.
func TestRecoverToolFailureStall(t *testing.T) {
	escalated := false
	st := backendState(func(context.Context) (bool, bool) { return true, true })
	st.EscalateWindow = func() (int, bool) { escalated = true; return 131072, true }
	stall := fmt.Errorf("%w (6 in a row): edit_file keeps failing with: %q",
		engine.ErrToolFailureLimit, "old_string not found")

	resets := 0
	for i := 1; i <= maxToolFailureResets; i++ {
		if got := st.recoverToolFailureStall(stall, "architecture phase", &resets); got != overflowRetry {
			t.Fatalf("reset %d = %v, want overflowRetry", i, got)
		}
	}
	if got := st.recoverToolFailureStall(stall, "architecture phase", &resets); got != overflowStop {
		t.Errorf("past budget = %v, want overflowStop", got)
	}
	if escalated {
		t.Error("recoverToolFailureStall escalated the context window; the window is not the failure")
	}

	// A non-breaker error is left for the caller to surface.
	resets = 0
	if got := st.recoverToolFailureStall(errors.New("boom"), "architecture phase", &resets); got != overflowNotHandled {
		t.Errorf("plain error = %v, want overflowNotHandled", got)
	}
	if resets != 0 {
		t.Errorf("plain error spent %d reset(s), want 0", resets)
	}
}

// TestSuiteSnapshotRoundTrip is the P50.3 rollback primitive: a snapshot of the
// suite report files restores their exact contents after an edit, and it
// ignores the non-suite `.quality-stamp.json` so a rollback never resurrects a
// stale stamp.
func TestSuiteSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"3-findings.md":       "clean findings\n",
		"1.1-model.mmd":       "flowchart LR\n",
		"inventory.yaml":      "threats: []\n",
		".quality-stamp.json": `{"fingerprint":"x"}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := suiteSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap[".quality-stamp.json"]; ok {
		t.Error("snapshot must not capture .quality-stamp.json")
	}
	if len(snap) != 3 {
		t.Errorf("snapshot captured %d files, want 3 (md/mmd/yaml)", len(snap))
	}

	// Regress a file, then restore.
	if err := os.WriteFile(filepath.Join(dir, "3-findings.md"), []byte("REGRESSED duplicate FIND-07\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreSuiteSnapshot(dir, snap); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "3-findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != files["3-findings.md"] {
		t.Errorf("after restore = %q, want the clean pre-edit content", string(got))
	}
}

// TestPhaseProgress exercises the P50.4 heartbeat tracker: enter resets the
// per-phase counters, tick advances the turn and records pending, and snapshot
// reads them back — the state the background heartbeat ticker reports.
func TestPhaseProgress(t *testing.T) {
	p := &Progress{}
	p.enter("findings")
	if turn, _ := p.tick(4); turn != 1 {
		t.Errorf("first tick turn = %d, want 1", turn)
	}
	if turn, _ := p.tick(3); turn != 2 {
		t.Errorf("second tick turn = %d, want 2", turn)
	}
	phase, turn, pending, _ := p.snapshot()
	if phase != "findings" || turn != 2 || pending != 3 {
		t.Errorf("snapshot = (%q, %d, %d), want (findings, 2, 3)", phase, turn, pending)
	}
	p.enter("assessment") // a new phase resets turn
	if _, turn, _, _ := p.snapshot(); turn != 0 {
		t.Errorf("after enter, turn = %d, want 0", turn)
	}
}

// withShrunkBackendBudget shrinks the P50.1 wait-for-recovery budget/interval
// for a test and returns a restore func (also deferred by t.Cleanup as a
// belt-and-braces guard against a forgotten restore).
func withShrunkBackendBudget(t *testing.T, budget, interval time.Duration) func() {
	t.Helper()
	oldBudget, oldInterval := backendRecoverBudget, backendProbeInterval
	backendRecoverBudget, backendProbeInterval = budget, interval
	restore := func() { backendRecoverBudget, backendProbeInterval = oldBudget, oldInterval }
	t.Cleanup(restore)
	return restore
}
