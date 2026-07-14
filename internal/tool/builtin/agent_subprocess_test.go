package builtin

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/swarm"
)

// TestMain lets this test binary double as a fake headless worker process
// when swarm.SubprocessBackend re-execs it, mirroring the identical pattern
// internal/swarm/subprocess_test.go already uses. This package needs its own
// copy (rather than reusing swarm's) because
// TestAgentToolBackgroundSpawnRespectsSharedBudgetCeiling below exercises a
// real *swarm.SubprocessBackend end to end through the real agentTool —
// previously every test in this package only ever used a stub/fake
// swarm.Backend, so the actual subprocess worker re-exec path was never
// driven from here.
func TestMain(m *testing.M) {
	if slices.Contains(os.Args, "__worker") {
		fakeSubprocessWorkerMain()
		return
	}
	os.Exit(m.Run())
}

// fakeSubprocessWorkerMain stands in for the real headless worker
// (internal/cli/worker.go's runWorker) for this package's SubprocessBackend
// integration test: it reads the spec SubprocessBackend.Spawn wrote and
// reports back exactly the budget-ceiling fields the spec carried, so the
// parent test can assert on what Spawn actually computed and handed down
// without needing a real provider/engine in this package's test binary.
func fakeSubprocessWorkerMain() {
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
	var spec swarm.WorkerSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		os.Exit(3)
	}
	mb, err := swarm.OpenMailbox(spec.MailboxRoot, spec.Identity)
	if err != nil {
		os.Exit(3)
	}
	out, _ := json.Marshal(map[string]any{
		"remaining_budget_usd": spec.RemainingBudgetUSD,
		"remaining_tokens":     spec.RemainingTokens,
	})
	_ = mb.Send(swarm.Message{
		Type:    swarm.MsgResult,
		Sender:  spec.Identity.AgentID,
		Text:    string(out),
		Payload: map[string]any{"error": ""},
	})
	os.Exit(0)
}

// TestAgentToolBackgroundSpawnRespectsSharedBudgetCeiling is the FIND-16/
// P27.17 end-to-end regression. Two pieces of existing coverage each verify
// one half of the detached-spawn budget-propagation chain in isolation:
//   - TestAgentToolBackgroundSpawnCarriesCostTracker (agent_test.go) asserts
//     spawnBackground puts the shared tracker back on jobCtx after
//     task.Manager.Start severs it from the caller's ctx — but only against a
//     stub backend that just records the ctx it received.
//   - internal/swarm/subprocess_test.go's TestSubprocessSpawnComputesRemainingBudget
//     asserts a real SubprocessBackend.Spawn computes a fair-share-reduced
//     WorkerSpec.RemainingBudgetUSD/RemainingTokens from a ctx-carried tracker
//     with prior spend — but calls Spawn directly, never through a detached
//     background job.
//
// Neither test exercises the full production path together: agentTool.Execute
// with background:true -> task.Manager.Start (real detach) -> spawnBackground's
// carry-forward -> a real SubprocessBackend.Spawn -> the WorkerSpec a worker
// process actually receives. This test closes that gap: it spawns a detached
// background sub-agent through the real agentTool, a real task.Manager, and a
// real *swarm.SubprocessBackend, with a shared tracker that already has
// significant prior spend on the caller's (pre-detach) ctx, and asserts the
// detached child's WorkerSpec carries the fair-share-reduced remaining ceiling
// rather than the daemon's full configured cap.
func TestAgentToolBackgroundSpawnRespectsSharedBudgetCeiling(t *testing.T) {
	t.Setenv("SWARM_TEST_WORKER", "1") // marks children as fake workers, mirrors swarm's own test convention

	const capUSD = 1.0
	const capTokens = 1000
	reg := swarm.NewRegistry()
	backend := swarm.NewSubprocessBackend(os.Args[0], "__worker", reg, swarm.MailboxRoot(t.TempDir()), "", capUSD, capTokens)

	// Simulates the fan-out tree having already spent 0.4/1.0 USD and
	// 300/1000 tokens before this detached spawn happens — an earlier
	// sibling (or the parent turn itself) already drew down the shared cap.
	tracker := cost.NewTracker()
	tracker.AddWorkerCost(0.4, 300)

	mgr := newTaskMgr(t)
	at := NewAgentTool(backend, mgr)

	// Attach the tracker to the caller's live ctx, exactly like a real
	// request-scoped session run does, *before* the detach point inside
	// agentTool.Execute -> spawnBackground -> task.Manager.Start.
	ctx := swarm.WithCostTracker(context.Background(), tracker)
	res, err := at.Execute(ctx, json.RawMessage(`{"prompt":"do","subagent_type":"general","background":true}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("background spawn failed: %+v", res)
	}

	_, after, found := strings.Cut(res.Content, "task id ")
	if !found {
		t.Fatalf("could not find task id in response: %q", res.Content)
	}
	id, _, _ := strings.Cut(after, ")")
	tk, ok := mgr.Wait(id)
	if !ok {
		t.Fatalf("background task %q never finished", id)
	}
	if tk.Error != "" || tk.Output == "" {
		t.Fatalf("background task did not report a worker result: state=%v output=%q err=%q", tk.State, tk.Output, tk.Error)
	}

	var got struct {
		RemainingBudgetUSD float64 `json:"remaining_budget_usd"`
		RemainingTokens    int     `json:"remaining_tokens"`
	}
	if err := json.Unmarshal([]byte(tk.Output), &got); err != nil {
		t.Fatalf("unmarshal worker report from task output %q: %v", tk.Output, err)
	}

	wantBudget := capUSD - 0.4 // 0.6
	if diff := got.RemainingBudgetUSD - wantBudget; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("RemainingBudgetUSD = %v, want %v (cap - prior spend)", got.RemainingBudgetUSD, wantBudget)
	}
	if got.RemainingBudgetUSD <= 0 || got.RemainingBudgetUSD >= capUSD {
		t.Errorf("RemainingBudgetUSD = %v; detached spawn escaped the shared ceiling (want strictly between 0 and the full cap %v)", got.RemainingBudgetUSD, capUSD)
	}

	wantTokens := capTokens - 300 // 700
	if got.RemainingTokens != wantTokens {
		t.Errorf("RemainingTokens = %d, want %d (cap - prior spend)", got.RemainingTokens, wantTokens)
	}
	if got.RemainingTokens <= 0 || got.RemainingTokens >= capTokens {
		t.Errorf("RemainingTokens = %d; detached spawn escaped the shared ceiling (want strictly between 0 and the full cap %d)", got.RemainingTokens, capTokens)
	}
}
