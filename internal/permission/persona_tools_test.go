package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// spyGate records every Check call it receives and returns a fixed decision.
type spyGate struct {
	calls  int
	allow  bool
	reason string
}

func (g *spyGate) Check(context.Context, tool.Tool, json.RawMessage) (bool, string) {
	g.calls++
	return g.allow, g.reason
}

// fixedApprover always returns approve, and records whether it was called.
type fixedApprover struct {
	approve bool
	calls   int
}

func (a *fixedApprover) Approve(context.Context, string, string, json.RawMessage) bool {
	a.calls++
	return a.approve
}

func TestPersonaToolGate_EmptyListPassesThrough(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: false} // should never be consulted
	gate := NewPersonaToolGate(base, "general", nil, approver, nil, nil)

	ok, _ := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if !ok {
		t.Error("empty tools list should defer entirely to base gate")
	}
	if base.calls != 1 {
		t.Errorf("base.Check called %d times, want 1", base.calls)
	}
	if approver.calls != 0 {
		t.Errorf("approver should not be consulted when Tools is empty, got %d calls", approver.calls)
	}
}

func TestPersonaToolGate_ListedToolSkipsApproval(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: false}
	gate := NewPersonaToolGate(base, "developer", []string{"read_file", "shell"}, approver, nil, nil)

	ok, _ := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if !ok {
		t.Error("listed tool should be allowed without consulting the approver")
	}
	if approver.calls != 0 {
		t.Errorf("approver should not be consulted for a listed tool, got %d calls", approver.calls)
	}
	if base.calls != 1 {
		t.Errorf("base.Check called %d times, want 1", base.calls)
	}
}

func TestPersonaToolGate_ListedToolIsCaseInsensitive(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: false}
	gate := NewPersonaToolGate(base, "developer", []string{"Shell"}, approver, nil, nil)

	if ok, _ := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil); !ok {
		t.Error("tool-name matching should be case-insensitive")
	}
	if approver.calls != 0 {
		t.Error("approver should not be consulted when the tool matches case-insensitively")
	}
}

func TestPersonaToolGate_UnlistedToolAsksApproverThenDefersToBase(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: true}
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, approver, nil, nil)

	ok, _ := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if !ok {
		t.Error("approved out-of-list tool should still be allowed")
	}
	if approver.calls != 1 {
		t.Errorf("approver called %d times, want 1", approver.calls)
	}
	if base.calls != 1 {
		t.Errorf("base gate must still run the real decision after approval, calls=%d", base.calls)
	}
}

func TestPersonaToolGate_DeclinedApprovalBlocksWithoutConsultingBase(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: false}
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, approver, nil, nil)

	ok, reason := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if ok {
		t.Error("declined confirmation should block the call")
	}
	if reason == "" {
		t.Error("expected a non-empty denial reason")
	}
	if base.calls != 0 {
		t.Errorf("base gate should not run when the user declined, calls=%d", base.calls)
	}
}

func TestPersonaToolGate_NilApproverDefaultsToWarnAndAllow(t *testing.T) {
	base := &spyGate{allow: true}
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, nil, nil, nil)

	ok, _ := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if !ok {
		t.Error("nil approver should default to warn-and-allow (AutoApprove), never a hard block")
	}
	if base.calls != 1 {
		t.Errorf("base gate should still run, calls=%d", base.calls)
	}
}

func TestPersonaToolGate_BaseDenialStillWins(t *testing.T) {
	// Even when the tool is on the persona's list (no advisory prompt at
	// all), the persona-tool gate must never override the real gate's deny.
	base := &spyGate{allow: false, reason: "denied by mode policy"}
	approver := &fixedApprover{approve: true}
	gate := NewPersonaToolGate(base, "developer", []string{"shell"}, approver, nil, nil)

	ok, reason := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if ok {
		t.Error("persona tool list must never grant access the base gate denies")
	}
	if reason != "denied by mode policy" {
		t.Errorf("reason = %q, want the base gate's reason", reason)
	}
}

func TestPersonaToolGate_OnDecisionCallback(t *testing.T) {
	base := &spyGate{allow: true}
	var got ContextualDecision
	approver := &fixedApprover{approve: true}
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, approver, nil, func(d ContextualDecision) {
		got = d
	})

	gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if got.Tool != "shell" || got.Rule != "persona_tools" || got.Decision != Allow {
		t.Errorf("onDecision callback = %+v", got)
	}
}
