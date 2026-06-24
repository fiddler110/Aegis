package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scottymacleod/agentharness/internal/swarm"
)

// fakeBackend records the SpawnConfig it receives and returns a scripted result.
type fakeBackend struct {
	root   string
	gotCfg swarm.SpawnConfig
	output string
	errStr string
	spawns int
}

func (f *fakeBackend) Spawn(ctx context.Context, cfg swarm.SpawnConfig) (*swarm.Handle, error) {
	f.gotCfg = cfg
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
	at := NewAgentTool(b)
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

func TestAgentToolDepthGuard(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), output: "ok"}
	ctx := swarm.WithDepth(context.Background(), maxSpawnDepth)
	at := NewAgentTool(b)
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
	at := NewAgentTool(b)
	res, _ := at.Execute(context.Background(), json.RawMessage(`{"subagent_type":"general"}`))
	if !res.IsError {
		t.Error("expected error when prompt is missing")
	}
}

func TestAgentToolPropagatesFailure(t *testing.T) {
	b := &fakeBackend{root: t.TempDir(), errStr: "sub failed"}
	at := NewAgentTool(b)
	res, _ := at.Execute(context.Background(), json.RawMessage(`{"prompt":"x","subagent_type":"general"}`))
	if !res.IsError || !strings.Contains(res.Content, "sub failed") {
		t.Errorf("expected propagated failure, got %+v", res)
	}
}
