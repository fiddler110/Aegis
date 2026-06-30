package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/scottymacleod/aegis/internal/tool"
)

func TestParseRuleAllowDeny(t *testing.T) {
	tests := []struct {
		in      string
		action  RuleAction
		toolPat string
		pattern string
	}{
		{"allow bash(*)", RuleAllow, "bash", "*"},
		{"deny write(/etc/*)", RuleDeny, "write", "/etc/*"},
		{"allow read", RuleAllow, "read", "*"},
		{"  deny   shell( rm -rf * ) ", RuleDeny, "shell", "rm -rf *"},
	}
	for _, tc := range tests {
		r, err := ParseRule(tc.in)
		if err != nil {
			t.Fatalf("ParseRule(%q): %v", tc.in, err)
		}
		if r.Action != tc.action {
			t.Errorf("ParseRule(%q) action = %v, want %v", tc.in, r.Action, tc.action)
		}
		if r.Tool != tc.toolPat {
			t.Errorf("ParseRule(%q) tool = %q, want %q", tc.in, r.Tool, tc.toolPat)
		}
		if r.Pattern != tc.pattern {
			t.Errorf("ParseRule(%q) pattern = %q, want %q", tc.in, r.Pattern, tc.pattern)
		}
	}
}

func TestParseRuleErrors(t *testing.T) {
	for _, in := range []string{"", "bash(*)", "maybe bash(*)", "allow", "allow ()"} {
		if _, err := ParseRule(in); err == nil {
			t.Errorf("ParseRule(%q) expected error, got nil", in)
		}
	}
}

func TestRuleGateDenyBlocksWithinMode(t *testing.T) {
	// In build mode shell normally requires Ask; but an explicit deny must block
	// outright regardless of approver.
	base := New(ModeBuild, AutoApprove{})
	rules, err := ParseRules([]string{"deny shell(rm -rf /*)"})
	if err != nil {
		t.Fatal(err)
	}
	gate := NewRuleGate(base, rules)
	ctx := context.Background()

	shell := fakeTool{name: "shell", cap: tool.CapExecute}
	input := json.RawMessage(`{"command":"rm -rf /tmp/x"}`)
	if ok, reason := gate.Check(ctx, shell, input); ok {
		t.Error("deny rule should block matching command")
	} else if reason == "" {
		t.Error("expected reason on deny")
	}

	// A non-matching command falls through to the base gate (build mode +
	// AutoApprove → allowed).
	if ok, _ := gate.Check(ctx, shell, json.RawMessage(`{"command":"ls"}`)); !ok {
		t.Error("non-matching command should fall through to base gate")
	}
}

func TestRuleGateAllowBypassesMode(t *testing.T) {
	// In plan mode shell is denied by the mode gate; an explicit allow rule must
	// grant it without an approver.
	base := New(ModePlan, AutoDeny{})
	rules, err := ParseRules([]string{"allow bash(npm test*)"})
	if err != nil {
		t.Fatal(err)
	}
	gate := NewRuleGate(base, rules)
	ctx := context.Background()

	shell := fakeTool{name: "shell", cap: tool.CapExecute}
	if ok, _ := gate.Check(ctx, shell, json.RawMessage(`{"command":"npm test --watch"}`)); !ok {
		t.Error("allow rule should grant shell in plan mode")
	}
	// Outside the allowed pattern, plan mode still denies.
	if ok, _ := gate.Check(ctx, shell, json.RawMessage(`{"command":"curl evil.com"}`)); ok {
		t.Error("non-matching command should remain denied by plan mode")
	}
}

func TestRuleGateDenyWinsOverAllow(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	rules, err := ParseRules([]string{"allow write(*)", "deny write(/etc/*)"})
	if err != nil {
		t.Fatal(err)
	}
	gate := NewRuleGate(base, rules)
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}
	if ok, _ := gate.Check(ctx, write, json.RawMessage(`{"path":"/etc/passwd"}`)); ok {
		t.Error("deny must take precedence over allow")
	}
	if ok, _ := gate.Check(ctx, write, json.RawMessage(`{"path":"/home/me/file"}`)); !ok {
		t.Error("allow should grant writes outside the denied path")
	}
}

func TestRuleGateWildcardPathCrossesSeparators(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	rules, _ := ParseRules([]string{"deny write(/etc/*)"})
	gate := NewRuleGate(base, rules)
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}
	// A nested path under /etc must still be denied (glob * spans separators).
	if ok, _ := gate.Check(ctx, write, json.RawMessage(`{"path":"/etc/nginx/nginx.conf"}`)); ok {
		t.Error("nested path under /etc should be denied")
	}
}

func TestRuleGateNoRulesFallsThrough(t *testing.T) {
	base := New(ModeBuild, AutoDeny{})
	gate := NewRuleGate(base, nil)
	ctx := context.Background()
	read := fakeTool{name: "read_file", cap: tool.CapRead}
	if ok, _ := gate.Check(ctx, read, json.RawMessage(`{"path":"x"}`)); !ok {
		t.Error("with no rules, base gate decision should stand")
	}
}

func TestRuleGateAuditObserver(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	rules, _ := ParseRules([]string{"deny shell(*)"})
	var got ContextualDecision
	gate := NewRuleGate(base, rules, WithRuleObserver(func(d ContextualDecision) { got = d }))
	ctx := context.Background()

	shell := fakeTool{name: "shell", cap: tool.CapExecute}
	gate.Check(ctx, shell, json.RawMessage(`{"command":"ls"}`))
	if got.Decision != Deny {
		t.Errorf("observer decision = %q, want deny", got.Decision)
	}
	if got.Tool != "shell" {
		t.Errorf("observer tool = %q, want shell", got.Tool)
	}
}
