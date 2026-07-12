package builtin

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
	_ "modernc.org/sqlite"
)

// fakeBackend records the SpawnConfig it receives and returns a scripted result.
type fakeBackend struct {
	root   string
	gotCfg swarm.SpawnConfig
	gotCtx context.Context
	output string
	errStr string
	spawns int
}

func (f *fakeBackend) Spawn(ctx context.Context, cfg swarm.SpawnConfig) (*swarm.Handle, error) {
	f.gotCfg = cfg
	f.gotCtx = ctx
	f.spawns++
	// Reuse the in-process backend to produce a real Handle/Result.
	b := swarm.NewInProcessBackend(func(context.Context, swarm.SpawnConfig) (string, error) {
		if f.errStr != "" {
			return "", &stubErr{f.errStr}
		}
		return f.output, nil
	}, swarm.NewRegistry(), swarm.MailboxRoot(f.root))
	return b.Spawn(ctx, cfg)
}
func (f *fakeBackend) Shutdown(context.Context)                  {}
func (f *fakeBackend) OnStop(func(swarm.Identity, swarm.Result)) {}

type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }

func runAgent(t *testing.T, ctx context.Context, b swarm.Backend, input string) string {
	t.Helper()
	at := NewAgentTool(b, nil)
	res, err := at.Execute(ctx, json.RawMessage(input))
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	_ = res.IsError
	return res.Content
}

func TestAgentToolSuccess(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "the answer"}
	out := runAgent(t, context.Background(), b, `{"prompt":"find X","subagent_type":"explore"}`)
	if out != "the answer" {
		t.Errorf("content = %q", out)
	}
	if b.gotCfg.SystemPrompt == "" {
		t.Error("explore agent should carry a system prompt")
	}
	if b.gotCfg.Mode != "plan" {
		t.Errorf("explore mode = %q, want plan", b.gotCfg.Mode)
	}
}

func TestAgentToolClampsToPlanParent(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := swarm.WithParentMode(context.Background(), "plan")
	// Request a build sub-agent from a plan-mode parent -> must clamp to plan.
	runAgent(t, ctx, b, `{"prompt":"do","subagent_type":"build"}`)
	if b.gotCfg.Mode != "plan" {
		t.Errorf("child mode = %q, want plan (clamped)", b.gotCfg.Mode)
	}
}

func TestAgentToolBuildParentKeepsBuild(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := swarm.WithParentMode(context.Background(), "build")
	runAgent(t, ctx, b, `{"prompt":"do","subagent_type":"build"}`)
	if b.gotCfg.Mode != "build" {
		t.Errorf("child mode = %q, want build", b.gotCfg.Mode)
	}
}

// TestAgentToolCapturesSpawningWorkdir is the P25.8 regression for gap (a):
// the agent tool must capture the spawning turn's workdir at spawn time
// (tool.WorkdirFromContext) and set it on SpawnConfig explicitly, rather than
// leaving a spawned teammate to rely on the ctx value surviving whatever
// context the backend actually runs it under (which a detached/background
// spawn's context.Background()-derived job does not).
func TestAgentToolCapturesSpawningWorkdir(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := tool.WithWorkdir(context.Background(), "/session/root")
	runAgent(t, ctx, b, `{"prompt":"do","subagent_type":"explore"}`)
	if b.gotCfg.Workdir != "/session/root" {
		t.Errorf("SpawnConfig.Workdir = %q, want /session/root", b.gotCfg.Workdir)
	}
}

// TestAgentToolWorkflowCapturesSpawningWorkdir covers the sequential/parallel/
// loop workflow spawn path (executeWorkflow), a separate SpawnConfig
// construction site from the single-agent path above.
func TestAgentToolWorkflowCapturesSpawningWorkdir(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := tool.WithWorkdir(context.Background(), "/session/root")
	runAgent(t, ctx, b, `{"mode":"sequential","agents":[{"prompt":"step 1","subagent_type":"explore"}]}`)
	if b.gotCfg.Workdir != "/session/root" {
		t.Errorf("workflow SpawnConfig.Workdir = %q, want /session/root", b.gotCfg.Workdir)
	}
}

func TestAgentToolDepthGuard(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := swarm.WithDepth(context.Background(), maxSpawnDepth)
	at := NewAgentTool(b, nil)
	res, _ := at.Execute(ctx, json.RawMessage(`{"prompt":"x"}`))
	if !res.IsError || !strings.Contains(res.Content, "depth") {
		t.Errorf("expected depth-guard error, got %+v", res)
	}
	if b.spawns != 0 {
		t.Errorf("must not spawn past max depth, spawns=%d", b.spawns)
	}
}

func TestAgentToolRequiresPrompt(t *testing.T) {
	b := &fakeBackend{root: t.TempDir()}
	at := NewAgentTool(b, nil)
	res, _ := at.Execute(context.Background(), json.RawMessage(`{"subagent_type":"general"}`))
	if !res.IsError {
		t.Error("expected error when prompt is missing")
	}
}

func TestAgentToolPropagatesFailure(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), errStr: "sub failed"}
	at := NewAgentTool(b, nil)
	res, _ := at.Execute(context.Background(), json.RawMessage(`{"prompt":"x","subagent_type":"general"}`))
	if !res.IsError || !strings.Contains(res.Content, "sub failed") {
		t.Errorf("expected propagated failure, got %+v", res)
	}
}

// stubCostTracker is a minimal stand-in for *cost.Tracker so this package
// doesn't need to import internal/cost just to prove context propagation.
type stubCostTracker struct{}

// TestAgentToolPropagatesCheckpointIDToSpawn is the P9 regression: a
// subprocess-mode sub-agent can't see the parent's in-ctx Snapshotter (it
// starts a whole separate process with its own ctx tree), so the checkpoint
// id must be threaded through SpawnConfig explicitly, letting the worker
// reconstruct an equivalent Snapshotter of its own. Without this, checkpoint
// capture silently misses any file writes a subprocess-mode sub-agent makes.
func TestAgentToolPropagatesCheckpointIDToSpawn(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := checkpoint.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	snap := store.NewSnapshotter("cp-123")

	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := checkpoint.WithSnapshotter(context.Background(), snap)
	runAgent(t, ctx, b, `{"prompt":"do","subagent_type":"general"}`)

	if b.gotCfg.CheckpointID != "cp-123" {
		t.Errorf("spawn config CheckpointID = %q, want %q", b.gotCfg.CheckpointID, "cp-123")
	}
}

// TestAgentToolOmitsCheckpointIDWithoutSnapshotter verifies a spawn outside
// any checkpointed turn (e.g. no Snapshotter attached to ctx) leaves
// CheckpointID empty rather than propagating a stale or zero-value id.
func TestAgentToolOmitsCheckpointIDWithoutSnapshotter(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	runAgent(t, context.Background(), b, `{"prompt":"do","subagent_type":"general"}`)

	if b.gotCfg.CheckpointID != "" {
		t.Errorf("spawn config CheckpointID = %q, want empty without a Snapshotter on ctx", b.gotCfg.CheckpointID)
	}
}

// TestAgentToolPropagatesCostTrackerToSpawn is the D1 regression: a shared
// spend ledger attached to the caller's ctx (subAgentRunner reads it via
// swarm.CostTrackerFromContext) must reach the backend's Spawn call unchanged,
// so a sub-agent draws against the same BudgetUSD ceiling as its parent
// instead of getting a fresh allowance.
func TestAgentToolPropagatesCostTrackerToSpawn(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	tracker := &stubCostTracker{}
	ctx := swarm.WithCostTracker(context.Background(), tracker)
	runAgent(t, ctx, b, `{"prompt":"do","subagent_type":"general"}`)
	got, _ := swarm.CostTrackerFromContext(b.gotCtx).(*stubCostTracker)
	if got != tracker {
		t.Errorf("spawn ctx cost tracker = %v, want the shared tracker to propagate unchanged", got)
	}
}

// TestAgentToolBackgroundSpawnCarriesCostTracker is the background/detached
// half of the D1 regression: task.Manager.Start derives its RunFunc context
// from context.Background(), which would otherwise silently drop any shared
// cost ledger on the caller's ctx. spawnBackground must carry it forward
// explicitly rather than relying on context propagation across that detach
// point.
func TestAgentToolBackgroundSpawnCarriesCostTracker(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	mgr := newTaskMgr(t)
	tracker := &stubCostTracker{}
	ctx := swarm.WithCostTracker(context.Background(), tracker)
	at := NewAgentTool(b, mgr)
	res, err := at.Execute(ctx, json.RawMessage(`{"prompt":"do","subagent_type":"general","background":true}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("background spawn failed: %+v", res)
	}

	// Extract the task id from the tool's response and wait for it to finish,
	// so the assertion below doesn't race the background goroutine (and the
	// test doesn't tear down its db mid-write).
	_, after, found := strings.Cut(res.Content, "task id ")
	if !found {
		t.Fatalf("could not find task id in response: %q", res.Content)
	}
	id, _, _ := strings.Cut(after, ")")
	if _, ok := mgr.Wait(id); !ok {
		t.Fatalf("background task %q never finished", id)
	}
	if b.gotCtx == nil {
		t.Fatal("background spawn never reached the backend")
	}
	got, _ := swarm.CostTrackerFromContext(b.gotCtx).(*stubCostTracker)
	if got != tracker {
		t.Errorf("background spawn ctx cost tracker = %v, want the shared tracker to survive the detach", got)
	}
}

// TestAgentToolBackgroundSpawnCarriesWorkdir is the background/detached half
// of the P25.8 regression: task.Manager.Start derives its RunFunc context
// from context.Background(), which drops any tool.WithWorkdir ctx value the
// exact same way it drops the cost tracker above. Because the agent tool
// captures the workdir into cfg *before* handing off to spawnBackground
// (not read again from the job's own context), the spawned engine still
// sees it — this is the detached case the roadmap calls out as "the
// regression that matters" since it passes today only by accident for the
// foreground path.
func TestAgentToolBackgroundSpawnCarriesWorkdir(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	mgr := newTaskMgr(t)
	ctx := tool.WithWorkdir(context.Background(), "/session/root")
	at := NewAgentTool(b, mgr)
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
	if _, ok := mgr.Wait(id); !ok {
		t.Fatalf("background task %q never finished", id)
	}
	if b.gotCfg.Workdir != "/session/root" {
		t.Errorf("background SpawnConfig.Workdir = %q, want /session/root", b.gotCfg.Workdir)
	}
}

// TestAgentToolParallelWorkflowBreadthCap is the D1 breadth-limit regression:
// a 'parallel' workflow call's 'agents' array is model-controlled JSON with no
// other limit, so without MaxParallelAgents a single tool call could fan out
// arbitrarily wide, multiplying spend past a session's budget faster than the
// shared-ledger check can catch it.
func TestAgentToolParallelWorkflowBreadthCap(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	at := NewAgentTool(b, nil)

	agents := make([]map[string]string, MaxParallelAgents+1)
	for i := range agents {
		agents[i] = map[string]string{"prompt": "do something"}
	}
	input, err := json.Marshal(map[string]any{"mode": "parallel", "agents": agents})
	if err != nil {
		t.Fatal(err)
	}
	res, execErr := at.Execute(context.Background(), input)
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if !res.IsError || !strings.Contains(res.Content, "max") {
		t.Errorf("expected breadth-cap error, got %+v", res)
	}
	if b.spawns != 0 {
		t.Errorf("must not spawn any agent when over the breadth cap, spawns=%d", b.spawns)
	}
}

// gatingBackend blocks each spawned agent inside enter() until release is
// closed, so a test can observe exactly how many spawns are in flight at once
// without relying on real sleeps.
type gatingBackend struct {
	root  string
	enter func()
}

func (g *gatingBackend) Spawn(ctx context.Context, cfg swarm.SpawnConfig) (*swarm.Handle, error) {
	b := swarm.NewInProcessBackend(func(context.Context, swarm.SpawnConfig) (string, error) {
		g.enter()
		return "ok", nil
	}, swarm.NewRegistry(), swarm.MailboxRoot(g.root))
	return b.Spawn(ctx, cfg)
}
func (g *gatingBackend) Shutdown(context.Context)                  {}
func (g *gatingBackend) OnStop(func(swarm.Identity, swarm.Result)) {}

// TestAgentToolParallelWorkflowRespectsConcurrencyCap is the P17.2
// integration check: a 'parallel' batch of 5 agents must not run more than
// the limiter's starting cap (floor 2) simultaneously, even though nothing
// else in the request limits concurrency below MaxParallelAgents.
func TestAgentToolParallelWorkflowRespectsConcurrencyCap(t *testing.T) {
	const numAgents = 5
	// entered is buffered so every agent's enter() can report in without
	// blocking on a receiver: once release is closed, agents beyond the two
	// this test explicitly drains must not deadlock trying to send here.
	entered := make(chan struct{}, numAgents)
	release := make(chan struct{})
	b := &gatingBackend{root: t.TempDir(), enter: func() {
		entered <- struct{}{}
		<-release
	}}
	at := NewAgentTool(b, nil) // no WithConcurrencyLimiter -> fresh floor-2 limiter

	agents := make([]map[string]string, numAgents)
	for i := range agents {
		agents[i] = map[string]string{"prompt": "do something"}
	}
	input, err := json.Marshal(map[string]any{"mode": "parallel", "agents": agents})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan tool.Result, 1)
	go func() {
		res, execErr := at.Execute(context.Background(), input)
		if execErr != nil {
			t.Errorf("Execute: %v", execErr)
		}
		done <- res
	}()

	<-entered
	<-entered
	select {
	case <-entered:
		t.Fatal("a third spawn started concurrently while the cap was 2")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if res := <-done; res.IsError {
		t.Errorf("unexpected error result: %+v", res)
	}
}
