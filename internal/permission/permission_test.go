package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

type fakeTool struct {
	name   string
	cap    tool.Capability
	schema json.RawMessage // optional; defaults to "{}" (no properties) when unset
}

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "" }
func (f fakeTool) InputSchema() json.RawMessage {
	if len(f.schema) == 0 {
		return json.RawMessage(`{}`)
	}
	return f.schema
}
func (f fakeTool) Capability() tool.Capability { return f.cap }
func (f fakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

// fakeOverrideTool implements tool.CapabilityOverrider on top of fakeTool,
// for exercising the P25.4c EffectiveCapability seam without a real shell
// tool: overrideCap is returned for every call regardless of input, while
// Capability() (embedded from fakeTool) keeps reporting the static cap.
type fakeOverrideTool struct {
	fakeTool
	overrideCap tool.Capability
}

func (f fakeOverrideTool) CapabilityFor(json.RawMessage) tool.Capability { return f.overrideCap }

func TestPolicyDecide(t *testing.T) {
	tests := []struct {
		mode Mode
		cap  tool.Capability
		want Decision
	}{
		{ModePlan, tool.CapRead, Allow},
		{ModePlan, tool.CapNetwork, Ask},
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

// TestGateCheckUsesEffectiveCapability covers P25.4c: a call a tool
// reclassifies via CapabilityOverrider (e.g. shell's read-only allowlist) is
// gated on that narrower capability, not the tool's static one — both in
// build mode (no approval prompt) and plan mode (allowed instead of denied).
func TestGateCheckUsesEffectiveCapability(t *testing.T) {
	ctx := context.Background()
	readClassified := fakeOverrideTool{
		fakeTool:    fakeTool{name: "shell", cap: tool.CapExecute},
		overrideCap: tool.CapRead,
	}

	if ok, _ := New(ModeBuild, AutoDeny{}).Check(ctx, readClassified, nil); !ok {
		t.Error("expected a read-classified call to be allowed outright in build mode, no approver consulted")
	}
	if ok, _ := New(ModePlan, AutoDeny{}).Check(ctx, readClassified, nil); !ok {
		t.Error("expected a read-classified call to be allowed under the plan-mode read gate, not denied")
	}

	// Sanity: an unclassified call to the same tool still follows its static
	// CapExecute behavior (Ask in build mode; AutoDeny blocks it).
	plainExec := fakeTool{name: "shell", cap: tool.CapExecute}
	if ok, _ := New(ModeBuild, AutoDeny{}).Check(ctx, plainExec, nil); ok {
		t.Error("expected an unclassified execute call to still require approval")
	}
}
