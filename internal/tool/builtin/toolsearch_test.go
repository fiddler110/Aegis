package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

type deferredFakeTool struct{ name, desc string }

func (f *deferredFakeTool) Name() string                 { return f.name }
func (f *deferredFakeTool) Description() string          { return f.desc }
func (f *deferredFakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *deferredFakeTool) Capability() tool.Capability  { return tool.CapRead }
func (f *deferredFakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

// TestToolSearchUsesContextRegistryOverConstructorRegistry is the P9
// regression: tool_search must mutate the registry the engine attaches to ctx
// (a per-session clone, when the caller scopes exposure per session) instead
// of the registry it was constructed against — otherwise a session's
// tool_search call would expose the tool process-wide, on the daemon's one
// shared registry, rather than just for that session.
func TestToolSearchUsesContextRegistryOverConstructorRegistry(t *testing.T) {
	base := tool.NewRegistry()
	if err := base.RegisterDeferred(&deferredFakeTool{name: "latex_build", desc: "build a LaTeX document"}); err != nil {
		t.Fatal(err)
	}
	sessionClone := base.Clone()

	ts := &toolSearchTool{reg: base}
	ctx := tool.WithRegistry(context.Background(), sessionClone)

	res, err := ts.Execute(ctx, json.RawMessage(`{"query":"latex"}`))
	if err != nil || res.IsError {
		t.Fatalf("Execute: err=%v res=%+v", err, res)
	}

	if got := len(sessionClone.Schemas()); got != 1 {
		t.Errorf("session clone exposed schemas = %d, want 1 (tool_search should expose on the ctx registry)", got)
	}
	if got := len(base.Schemas()); got != 0 {
		t.Errorf("constructor registry exposed schemas = %d, want 0 (must not leak process-wide)", got)
	}
}

// TestToolSearchFallsBackToConstructorRegistry verifies a call with no
// registry attached to ctx (e.g. a sub-agent context that doesn't scope
// exposure) still works against the tool's own construction-time registry.
func TestToolSearchFallsBackToConstructorRegistry(t *testing.T) {
	base := tool.NewRegistry()
	if err := base.RegisterDeferred(&deferredFakeTool{name: "latex_build", desc: "build a LaTeX document"}); err != nil {
		t.Fatal(err)
	}
	ts := &toolSearchTool{reg: base}

	res, err := ts.Execute(context.Background(), json.RawMessage(`{"query":"latex"}`))
	if err != nil || res.IsError {
		t.Fatalf("Execute: err=%v res=%+v", err, res)
	}
	if got := len(base.Schemas()); got != 1 {
		t.Errorf("constructor registry exposed schemas = %d, want 1", got)
	}
}
