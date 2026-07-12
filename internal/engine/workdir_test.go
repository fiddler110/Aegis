package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// workdirCapturingTool records the workdir it observed via
// tool.WorkdirFromContext on each Execute call, so a test can assert the
// engine actually threads Options.Workdir through to tool calls (P25.1).
type workdirCapturingTool struct{ seen []string }

func (t *workdirCapturingTool) Name() string        { return "read_file" }
func (t *workdirCapturingTool) Description() string { return "read a file" }
func (t *workdirCapturingTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *workdirCapturingTool) Capability() tool.Capability { return tool.CapRead }
func (t *workdirCapturingTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	wd, _ := tool.WorkdirFromContext(ctx)
	t.seen = append(t.seen, wd)
	return tool.Result{Content: "ok"}, nil
}

// TestExecuteToolAppliesOptionsWorkdir verifies Options.Workdir (P25.1) is
// carried onto every tool call's context, and that leaving it unset carries
// no override — a tool falls back to its own default exactly as before this
// feature existed.
func TestExecuteToolAppliesOptionsWorkdir(t *testing.T) {
	ct := &workdirCapturingTool{}
	reg := tool.NewRegistry()
	if err := reg.Register(ct); err != nil {
		t.Fatal(err)
	}
	adapter, conv := toolRoundTripConv()

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100, Workdir: "/session/root"})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ct.seen) != 1 || ct.seen[0] != "/session/root" {
		t.Errorf("workdir seen by tool = %v, want [/session/root]", ct.seen)
	}
}

func TestExecuteToolLeavesWorkdirUnsetWhenOptionsWorkdirEmpty(t *testing.T) {
	ct := &workdirCapturingTool{}
	reg := tool.NewRegistry()
	if err := reg.Register(ct); err != nil {
		t.Fatal(err)
	}
	adapter, conv := toolRoundTripConv()

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ct.seen) != 1 || ct.seen[0] != "" {
		t.Errorf("workdir seen by tool = %v, want [\"\"] (no override)", ct.seen)
	}
}
