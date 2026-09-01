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
	gate := NewPersonaToolGate(base, "general", nil, false, approver, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"read_file", "shell"}, false, approver, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"Shell"}, false, approver, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, false, approver, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, false, approver, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, false, nil, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"shell"}, false, approver, nil, nil)

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
	gate := NewPersonaToolGate(base, "developer", []string{"read_file"}, false, approver, nil, func(d ContextualDecision) {
		got = d
	})

	gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if got.Tool != "shell" || got.Rule != "persona_tools" || got.Decision != Allow {
		t.Errorf("onDecision callback = %+v", got)
	}
}

// TestPersonaToolGate_EnforcingRefusesWithoutConsultingApprover is P81.20/
// FIND-20 item 4: when a persona opts into enforcing mode, an out-of-list
// tool call is refused outright, and the approver — the advisory path's warn/
// prompt seam — is never consulted at all.
func TestPersonaToolGate_EnforcingRefusesWithoutConsultingApprover(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: true} // would approve if asked; must not be asked
	gate := NewPersonaToolGate(base, "locked-down", []string{"read_file"}, true, approver, nil, nil)

	ok, reason := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if ok {
		t.Error("enforcing mode must refuse a tool outside the persona's list")
	}
	if reason == "" {
		t.Error("expected a non-empty refusal reason")
	}
	if approver.calls != 0 {
		t.Errorf("enforcing mode must not consult the approver, got %d calls", approver.calls)
	}
	if base.calls != 0 {
		t.Errorf("enforcing mode must not fall through to the base gate on refusal, calls=%d", base.calls)
	}
}

// TestPersonaToolGate_EnforcingStillAllowsListedTools pins that enforcing
// mode changes nothing about a tool that IS on the list — it still defers to
// the base gate exactly like the advisory posture does.
func TestPersonaToolGate_EnforcingStillAllowsListedTools(t *testing.T) {
	base := &spyGate{allow: true}
	approver := &fixedApprover{approve: false}
	gate := NewPersonaToolGate(base, "locked-down", []string{"read_file", "shell"}, true, approver, nil, nil)

	ok, _ := gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if !ok {
		t.Error("a listed tool must still be allowed under enforcing mode")
	}
	if approver.calls != 0 {
		t.Errorf("approver should not be consulted for a listed tool, got %d calls", approver.calls)
	}
	if base.calls != 1 {
		t.Errorf("base.Check called %d times, want 1", base.calls)
	}
}

// TestPersonaToolGate_EnforcingOnDecisionCallback pins the audit-trail shape
// for an enforced refusal, so an operator reviewing the decision log sees a
// distinct rule name from the advisory "persona_tools" one and can tell the
// two postures apart.
func TestPersonaToolGate_EnforcingOnDecisionCallback(t *testing.T) {
	base := &spyGate{allow: true}
	var got ContextualDecision
	approver := &fixedApprover{approve: true}
	gate := NewPersonaToolGate(base, "locked-down", []string{"read_file"}, true, approver, nil, func(d ContextualDecision) {
		got = d
	})

	gate.Check(context.Background(), fakeTool{name: "shell", cap: tool.CapExecute}, nil)
	if got.Tool != "shell" || got.Rule != "persona_tools_enforced" || got.Decision != Deny {
		t.Errorf("onDecision callback = %+v", got)
	}
}
