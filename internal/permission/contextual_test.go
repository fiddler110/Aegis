package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/scottymacleod/agentharness/internal/tool"
)

func TestEgressThenWriteBlocksAfterNetwork(t *testing.T) {
	base := New(ModeBuild, AutoDeny{})
	gate := NewContextualGate(base, ContextualOpts{EgressThenWrite: true})
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}
	fetch := fakeTool{name: "fetch", cap: tool.CapNetwork}

	// Before any network call, writes are allowed (base build mode).
	if ok, _ := gate.Check(ctx, write, nil); !ok {
		t.Error("write should be allowed before network egress")
	}

	// Simulate a network call.
	gate.Check(ctx, fetch, nil)
	gate.PostToolUse(ctx, "fetch", nil, "", false)

	// After network egress, writes should require approval → denied by AutoDeny.
	if ok, reason := gate.Check(ctx, write, nil); ok {
		t.Error("write should be denied after network egress with AutoDeny")
	} else if reason == "" {
		t.Error("expected a reason for denial")
	}
}

func TestEgressThenWriteApprovedAfterNetwork(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewContextualGate(base, ContextualOpts{EgressThenWrite: true})
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}

	// Simulate network egress.
	gate.PostToolUse(ctx, "fetch", nil, "", false)

	// With AutoApprove, the Ask decision should pass.
	if ok, _ := gate.Check(ctx, write, nil); !ok {
		t.Error("write should be approved after network egress with AutoApprove")
	}
}

func TestEgressThenWriteIgnoresErrors(t *testing.T) {
	base := New(ModeBuild, AutoDeny{})
	gate := NewContextualGate(base, ContextualOpts{EgressThenWrite: true})
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}

	// A failed network call should NOT trigger the egress flag.
	gate.PostToolUse(ctx, "fetch", nil, "", true)

	if ok, _ := gate.Check(ctx, write, nil); !ok {
		t.Error("write should still be allowed after a failed network call")
	}
}

func TestEgressThenWriteDisabled(t *testing.T) {
	base := New(ModeBuild, AutoDeny{})
	gate := NewContextualGate(base, ContextualOpts{EgressThenWrite: false})
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}

	gate.PostToolUse(ctx, "fetch", nil, "", false)

	// Rule disabled, so writes should still be allowed.
	if ok, _ := gate.Check(ctx, write, nil); !ok {
		t.Error("write should be allowed when egress_then_write is disabled")
	}
}

func TestNetworkAllowList(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewContextualGate(base, ContextualOpts{
		NetworkAllowList: []string{"example.com", ".trusted.io"},
	})
	ctx := context.Background()

	fetch := fakeTool{name: "fetch", cap: tool.CapNetwork}

	// Allowed domain.
	input := json.RawMessage(`{"url":"https://example.com/api"}`)
	if ok, _ := gate.Check(ctx, fetch, input); !ok {
		t.Error("example.com should be allowed")
	}

	// Subdomain match.
	input = json.RawMessage(`{"url":"https://api.trusted.io/v1"}`)
	if ok, _ := gate.Check(ctx, fetch, input); !ok {
		t.Error("api.trusted.io should match .trusted.io")
	}

	// Disallowed domain.
	input = json.RawMessage(`{"url":"https://evil.com/steal"}`)
	if ok, _ := gate.Check(ctx, fetch, input); ok {
		t.Error("evil.com should be blocked by allowlist")
	}

	// No URL in input.
	input = json.RawMessage(`{}`)
	if ok, _ := gate.Check(ctx, fetch, input); ok {
		t.Error("empty URL should be blocked when allowlist is set")
	}
}

func TestNetworkAllowListDisabled(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewContextualGate(base, ContextualOpts{
		NetworkAllowList: nil,
	})
	ctx := context.Background()

	fetch := fakeTool{name: "fetch", cap: tool.CapNetwork}

	// No allowlist = all URLs allowed.
	input := json.RawMessage(`{"url":"https://anywhere.com/ok"}`)
	if ok, _ := gate.Check(ctx, fetch, input); !ok {
		t.Error("should be allowed when no allowlist is configured")
	}
}

func TestNetworkAllowListSearchToolPassthrough(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewContextualGate(base, ContextualOpts{
		NetworkAllowList: []string{"search.api"},
	})
	ctx := context.Background()

	search := fakeTool{name: "search", cap: tool.CapNetwork}

	// Search tools pass through with synthetic "search.api" domain.
	input := json.RawMessage(`{"query":"test"}`)
	if ok, _ := gate.Check(ctx, search, input); !ok {
		t.Error("search tool should pass with search.api in allowlist")
	}
}

func TestReset(t *testing.T) {
	base := New(ModeBuild, AutoDeny{})
	gate := NewContextualGate(base, ContextualOpts{EgressThenWrite: true})
	ctx := context.Background()

	gate.PostToolUse(ctx, "fetch", nil, "", false)
	if !gate.NetworkUsed() {
		t.Error("expected networkUsed after fetch")
	}

	gate.Reset()
	if gate.NetworkUsed() {
		t.Error("expected networkUsed cleared after reset")
	}

	write := fakeTool{name: "write_file", cap: tool.CapWrite}
	if ok, _ := gate.Check(ctx, write, nil); !ok {
		t.Error("write should be allowed after reset")
	}
}

func TestBaseGateDenialTakesPrecedence(t *testing.T) {
	base := New(ModePlan, AutoDeny{})
	gate := NewContextualGate(base, ContextualOpts{EgressThenWrite: true})
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}

	// Plan mode denies writes regardless of contextual state.
	if ok, _ := gate.Check(ctx, write, nil); ok {
		t.Error("plan mode should deny writes regardless of contextual gate")
	}
}

func TestOnDecisionCallback(t *testing.T) {
	var decisions []ContextualDecision
	base := New(ModeBuild, AutoDeny{})
	gate := NewContextualGate(base, ContextualOpts{
		EgressThenWrite: true,
		OnDecision:      func(d ContextualDecision) { decisions = append(decisions, d) },
	})
	ctx := context.Background()

	write := fakeTool{name: "write_file", cap: tool.CapWrite}

	gate.PostToolUse(ctx, "fetch", nil, "", false)
	gate.Check(ctx, write, nil)

	if len(decisions) == 0 {
		t.Error("expected at least one decision callback")
	}
	if decisions[0].Rule != "egress_then_write" {
		t.Errorf("expected egress_then_write rule, got %q", decisions[0].Rule)
	}
}
