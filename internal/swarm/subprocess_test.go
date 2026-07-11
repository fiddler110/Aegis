package swarm

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sync"
	"testing"
)

// TestMain lets this test binary double as a fake worker process when the
// SubprocessBackend re-executes it. We detect the marker env var before the test
// framework parses flags and act as the worker, then exit.
func TestMain(m *testing.M) {
	// Detect when the test binary is re-executed as a fake worker by the
	// SubprocessBackend. We check os.Args for the "__worker" subcommand
	// because filteredEnv() strips SWARM_TEST_WORKER from the child's
	// environment — without this, the child falls through to m.Run() and
	// recursively spawns more children (exponential fork bomb / OOM).
	if slices.Contains(os.Args, "__worker") {
		fakeWorkerMain()
		return
	}
	os.Exit(m.Run())
}

// fakeWorkerMain simulates the headless worker: it reads the spec and records a
// result in the mailbox, varying behavior by prompt to exercise each path.
func fakeWorkerMain() {
	var specPath string
	for i, a := range os.Args {
		if a == "--spec" && i+1 < len(os.Args) {
			specPath = os.Args[i+1]
		}
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		os.Exit(3)
	}
	var spec WorkerSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		os.Exit(3)
	}

	switch spec.Config.Prompt {
	case "crash":
		// Exit without recording a result -> parent must synthesize a failure.
		os.Exit(2)
	case "fail":
		mb, _ := OpenMailbox(spec.MailboxRoot, spec.Identity)
		_ = mb.Send(Message{Type: MsgResult, Sender: spec.Identity.AgentID, Payload: map[string]any{"error": "deliberate failure"}})
		os.Exit(1)
	case "report-remaining":
		// Echoes back what Spawn computed for this worker (P10.3), so the
		// parent test can assert on the exact remaining-budget arithmetic
		// without needing a real engine run.
		mb, _ := OpenMailbox(spec.MailboxRoot, spec.Identity)
		out, _ := json.Marshal(map[string]any{
			"remaining_budget_usd": spec.RemainingBudgetUSD,
			"remaining_tokens":     spec.RemainingTokens,
		})
		_ = mb.Send(Message{Type: MsgResult, Sender: spec.Identity.AgentID, Text: string(out), Payload: map[string]any{"error": ""}})
		os.Exit(0)
	default:
		mb, _ := OpenMailbox(spec.MailboxRoot, spec.Identity)
		_ = mb.Send(Message{
			Type:   MsgResult,
			Sender: spec.Identity.AgentID,
			Text:   "worker handled: " + spec.Config.Prompt,
			// Simulates a worker self-reporting its own spend (P10.3) so
			// SubprocessBackend can fold it back into the parent's shared
			// tracker once this process exits.
			Payload: map[string]any{"error": "", "cost_usd": 2.5, "tokens": 100},
		})
		os.Exit(0)
	}
}

func newTestSubprocessBackend(t *testing.T) (*SubprocessBackend, *Registry) {
	t.Helper()
	t.Setenv("SWARM_TEST_WORKER", "1") // children inherit; marks them as workers
	reg := NewRegistry()
	b := NewSubprocessBackend(os.Args[0], "__worker", reg, MailboxRoot(t.TempDir()), "", 0, 0)
	return b, reg
}

func TestSubprocessSpawnSuccess(t *testing.T) {
	b, reg := newTestSubprocessBackend(t)
	h, err := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed() || res.Output != "worker handled: do it" {
		t.Errorf("result = %+v", res)
	}
	if m, ok := reg.Get(res.AgentID); !ok || m.Status != StatusDone {
		t.Errorf("registry status = %+v", m)
	}
}

func TestSubprocessSpawnReportedFailure(t *testing.T) {
	b, _ := newTestSubprocessBackend(t)
	h, _ := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "fail"})
	res, _ := h.Wait(context.Background())
	if !res.Failed() || res.Err != "deliberate failure" {
		t.Errorf("expected reported failure, got %+v", res)
	}
}

func TestSubprocessSpawnCrashSynthesizesError(t *testing.T) {
	b, _ := newTestSubprocessBackend(t)
	h, _ := b.Spawn(context.Background(), SpawnConfig{Name: "w", Prompt: "crash"})
	res, _ := h.Wait(context.Background())
	if !res.Failed() {
		t.Fatalf("expected failure for a crashing worker, got %+v", res)
	}
	if res.Output != "" {
		t.Errorf("crash should yield no output, got %q", res.Output)
	}
}

// fakeTracker is a minimal costTracker double for tests: NewSubprocessBackend
// deliberately doesn't import internal/cost, so nothing here needs a real
// *cost.Tracker either.
type fakeTracker struct {
	mu             sync.Mutex
	usd            float64
	tokens         int
	addedCostUSD   float64
	addedTokens    int
	addWorkerCosts int
}

func (f *fakeTracker) TotalUSD() float64 { f.mu.Lock(); defer f.mu.Unlock(); return f.usd }
func (f *fakeTracker) TotalTokens() int  { f.mu.Lock(); defer f.mu.Unlock(); return f.tokens }
func (f *fakeTracker) AddWorkerCost(costUSD float64, tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addedCostUSD = costUSD
	f.addedTokens = tokens
	f.addWorkerCosts++
}

// TestSubprocessSpawnComputesRemainingBudget is the P10.3 regression: each
// subprocess worker used to get a fresh cfg.Cost.BudgetUSD/MaxTokensPerRun
// allowance regardless of what the fan-out tree had already spent, so N
// spawns enforced N times the intended ceiling. Spawn must instead pass down
// what's left of the shared tracker's budget.
func TestSubprocessSpawnComputesRemainingBudget(t *testing.T) {
	t.Setenv("SWARM_TEST_WORKER", "1")
	reg := NewRegistry()
	b := NewSubprocessBackend(os.Args[0], "__worker", reg, MailboxRoot(t.TempDir()), "", 1.0, 1000)

	tracker := &fakeTracker{usd: 0.4, tokens: 300}
	ctx := WithCostTracker(context.Background(), tracker)

	h, err := b.Spawn(ctx, SpawnConfig{Name: "w", Prompt: "report-remaining"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		RemainingBudgetUSD float64 `json:"remaining_budget_usd"`
		RemainingTokens    int     `json:"remaining_tokens"`
	}
	if err := json.Unmarshal([]byte(res.Output), &got); err != nil {
		t.Fatalf("unmarshal worker report: %v (output=%q)", err, res.Output)
	}
	if diff := got.RemainingBudgetUSD - 0.6; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("RemainingBudgetUSD = %v, want 0.6 (1.0 cap - 0.4 spent)", got.RemainingBudgetUSD)
	}
	if got.RemainingTokens != 700 {
		t.Errorf("RemainingTokens = %d, want 700 (1000 cap - 300 spent)", got.RemainingTokens)
	}
}

// TestRemainingBudgetFairShareFloor is the FIND-14/P24.15 regression: once
// the shared tracker's spend has consumed the whole cap, a worker must still
// get fairShareFraction of the *original* cap, not the near-zero epsilon that
// used to be the only floor.
func TestRemainingBudgetFairShareFloor(t *testing.T) {
	got := remainingBudget(1.0, 1.0) // fully exhausted
	want := 0.2                      // 1.0 * fairShareFraction
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("remainingBudget(1.0, 1.0) = %v, want %v (fair-share floor)", got, want)
	}

	got = remainingBudget(1.0, 5.0) // spent well past the cap
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("remainingBudget(1.0, 5.0) = %v, want %v (fair-share floor still applies)", got, want)
	}
}

// TestRemainingBudgetFairShareFloorFallsBackToEpsilon covers the degenerate
// case the fair-share floor doesn't help with: a cap so small that even
// fairShareFraction of it rounds to (near) zero. minRemainingBudgetUSD must
// still apply so the override stays distinguishable from "no cap configured".
func TestRemainingBudgetFairShareFloorFallsBackToEpsilon(t *testing.T) {
	got := remainingBudget(0.0000001, 0.0000001)
	if got != minRemainingBudgetUSD {
		t.Errorf("remainingBudget with a sub-epsilon cap = %v, want minRemainingBudgetUSD (%v)", got, minRemainingBudgetUSD)
	}
}

// TestRemainingTokensFairShareFloor mirrors TestRemainingBudgetFairShareFloor
// for the token cap.
func TestRemainingTokensFairShareFloor(t *testing.T) {
	got := remainingTokens(1000, 1000) // fully exhausted
	want := 200                        // 1000 * fairShareFraction
	if got != want {
		t.Errorf("remainingTokens(1000, 1000) = %d, want %d (fair-share floor)", got, want)
	}

	got = remainingTokens(1000, 5000) // spent well past the cap
	if got != want {
		t.Errorf("remainingTokens(1000, 5000) = %d, want %d (fair-share floor still applies)", got, want)
	}
}

// TestSubprocessSpawnLaterSiblingGetsFairShareFloor is an end-to-end version
// of the FIND-14 regression through Spawn itself: an early worker consumes
// nearly the whole shared budget, and a later sibling spawned afterward must
// still see a guaranteed non-trivial floor rather than a near-zero remainder.
func TestSubprocessSpawnLaterSiblingGetsFairShareFloor(t *testing.T) {
	t.Setenv("SWARM_TEST_WORKER", "1")
	reg := NewRegistry()
	b := NewSubprocessBackend(os.Args[0], "__worker", reg, MailboxRoot(t.TempDir()), "", 1.0, 1000)

	// Simulates an early sibling having already spent the entire shared cap
	// (and then some) before this later worker is spawned.
	tracker := &fakeTracker{usd: 1.5, tokens: 1500}
	ctx := WithCostTracker(context.Background(), tracker)

	h, err := b.Spawn(ctx, SpawnConfig{Name: "late", Prompt: "report-remaining"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		RemainingBudgetUSD float64 `json:"remaining_budget_usd"`
		RemainingTokens    int     `json:"remaining_tokens"`
	}
	if err := json.Unmarshal([]byte(res.Output), &got); err != nil {
		t.Fatalf("unmarshal worker report: %v (output=%q)", err, res.Output)
	}
	// Both would be the near-zero epsilon floor (1e-6 / 1) pre-fix; assert
	// they instead land at the fair-share floor (0.2 * cap).
	if got.RemainingBudgetUSD < 0.1 {
		t.Errorf("RemainingBudgetUSD = %v, want a fair-share floor (~0.2), not the near-zero epsilon", got.RemainingBudgetUSD)
	}
	if got.RemainingTokens < 100 {
		t.Errorf("RemainingTokens = %d, want a fair-share floor (~200), not the near-zero epsilon", got.RemainingTokens)
	}
}

// TestSubprocessSpawnFoldsWorkerCostIntoSharedTracker verifies a worker's
// self-reported spend (cost_usd/tokens in its mailbox result) is folded back
// into the ctx-carried tracker once it exits, so a sibling spawned
// afterward sees the updated total (P10.3).
func TestSubprocessSpawnFoldsWorkerCostIntoSharedTracker(t *testing.T) {
	b, _ := newTestSubprocessBackend(t)
	tracker := &fakeTracker{}
	ctx := WithCostTracker(context.Background(), tracker)

	h, err := b.Spawn(ctx, SpawnConfig{Name: "w", Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.CostUSD != 2.5 || res.Tokens != 100 {
		t.Fatalf("Result cost/tokens = %v/%d, want 2.5/100 (from the fake worker's reported payload)", res.CostUSD, res.Tokens)
	}
	if tracker.addWorkerCosts != 1 {
		t.Fatalf("AddWorkerCost calls = %d, want 1", tracker.addWorkerCosts)
	}
	if tracker.addedCostUSD != 2.5 || tracker.addedTokens != 100 {
		t.Errorf("folded cost/tokens = %v/%d, want 2.5/100", tracker.addedCostUSD, tracker.addedTokens)
	}
}
