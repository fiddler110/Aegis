package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/scottymacleod/aegis/internal/tool"
)

type fakeTool struct {
	name string
	cap  tool.Capability
}

func (f fakeTool) Name() string                { return f.name }
func (f fakeTool) Description() string          { return "" }
func (f fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f fakeTool) Capability() tool.Capability  { return f.cap }
func (f fakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

func TestPolicyDecide(t *testing.T) {
	tests := []struct {
		mode Mode
		cap  tool.Capability
		want Decision
	}{
		{ModePlan, tool.CapRead, Allow},
		{ModePlan, tool.CapNetwork, Allow},
		{ModePlan, tool.CapWrite, Deny},
		{ModePlan, tool.CapExecute, Deny},
		{ModeBuild, tool.CapRead, Allow},
		{ModeBuild, tool.CapWrite, Allow},
		{ModeBuild, tool.CapNetwork, Allow},
		{ModeBuild, tool.CapExecute, Ask},
	}
	for _, tt := range tests {
		got := Policy{Mode: tt.mode}.Decide(tt.cap)
		if got != tt.want {
			t.Errorf("Decide(%s,%s) = %s, want %s", tt.mode, tt.cap, got, tt.want)
		}
	}
}

func TestGateCheck(t *testing.T) {
	ctx := context.Background()
	write := fakeTool{name: "write_file", cap: tool.CapWrite}
	exec := fakeTool{name: "shell", cap: tool.CapExecute}

	// Plan mode blocks writes.
	if ok, _ := New(ModePlan, nil).Check(ctx, write, nil); ok {
		t.Error("plan mode should block write")
	}
	// Build mode allows writes.
	if ok, _ := New(ModeBuild, nil).Check(ctx, write, nil); !ok {
		t.Error("build mode should allow write")
	}
	// Build mode asks for execute; AutoDeny -> blocked.
	if ok, _ := New(ModeBuild, AutoDeny{}).Check(ctx, exec, nil); ok {
		t.Error("execute should be denied by AutoDeny approver")
	}
	// Build mode asks for execute; AutoApprove -> allowed.
	if ok, _ := New(ModeBuild, AutoApprove{}).Check(ctx, exec, nil); !ok {
		t.Error("execute should be allowed by AutoApprove approver")
	}
}
