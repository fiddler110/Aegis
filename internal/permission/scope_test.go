package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func TestTaskScopeAllowed(t *testing.T) {
	sc := NewTaskScope()
	if !sc.Allowed("anything.go") {
		t.Error("inactive scope should allow everything")
	}
	if sc.Active() {
		t.Error("fresh scope should be inactive")
	}

	sc.Set([]string{"src/**", "cmd/main.go"})
	if !sc.Active() {
		t.Error("scope should be active after Set")
	}
	cases := []struct {
		path string
		want bool
	}{
		{"src/a.go", true},
		{"src/nested/deep/b.go", true},
		{"cmd/main.go", true},
		{"cmd/other.go", false},
		{"internal/x.go", false},
		// traversal / separator tricks must not dodge the scope.
		{"./src/a.go", true},
		{"src/../internal/x.go", false},
	}
	for _, tc := range cases {
		if got := sc.Allowed(tc.path); got != tc.want {
			t.Errorf("Allowed(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}

	sc.Clear()
	if sc.Active() {
		t.Error("scope should be inactive after Clear")
	}
	if !sc.Allowed("cmd/other.go") {
		t.Error("cleared scope should allow everything again")
	}
}

func TestScopeGateEnforcesWrites(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewScopeGate(base, nil)

	sc := NewTaskScope()
	sc.Set([]string{"src/**"})
	ctx := WithTaskScope(context.Background(), sc)

	write := fakeTool{name: "write_file", cap: tool.CapWrite,
		schema: json.RawMessage(`{"properties":{"path":{}}}`)}

	// In-scope write is allowed.
	if ok, reason := gate.Check(ctx, write, json.RawMessage(`{"path":"src/a.go"}`)); !ok {
		t.Errorf("in-scope write should be allowed, got reason %q", reason)
	}
	// Out-of-scope write is denied.
	if ok, _ := gate.Check(ctx, write, json.RawMessage(`{"path":"internal/x.go"}`)); ok {
		t.Error("out-of-scope write should be denied")
	}

	// multi_edit-shaped input: every edits[].path must be in scope.
	medit := fakeTool{name: "multi_edit", cap: tool.CapWrite}
	if ok, _ := gate.Check(ctx, medit, json.RawMessage(`{"edits":[{"path":"src/a.go"},{"path":"internal/x.go"}]}`)); ok {
		t.Error("multi_edit touching an out-of-scope path should be denied")
	}
	if ok, reason := gate.Check(ctx, medit, json.RawMessage(`{"edits":[{"path":"src/a.go"},{"path":"src/b.go"}]}`)); !ok {
		t.Errorf("fully in-scope multi_edit should be allowed, got %q", reason)
	}
}

func TestScopeGatePassthroughWhenInactive(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewScopeGate(base, nil)

	write := fakeTool{name: "write_file", cap: tool.CapWrite,
		schema: json.RawMessage(`{"properties":{"path":{}}}`)}

	// No scope on the context at all → defer to base (build+AutoApprove allows).
	if ok, reason := gate.Check(context.Background(), write, json.RawMessage(`{"path":"anywhere.go"}`)); !ok {
		t.Errorf("no scope should defer to base and allow, got %q", reason)
	}

	// A scope object present but inactive (never Set) → also passthrough.
	ctx := WithTaskScope(context.Background(), NewTaskScope())
	if ok, reason := gate.Check(ctx, write, json.RawMessage(`{"path":"anywhere.go"}`)); !ok {
		t.Errorf("inactive scope should defer to base and allow, got %q", reason)
	}
}

func TestScopeGateIgnoresPathlessWrites(t *testing.T) {
	// A write-capability tool with no path/file_path/edits (e.g. git_commit,
	// remember) contributes no paths, so an active scope must not block it.
	base := New(ModeBuild, AutoApprove{})
	gate := NewScopeGate(base, nil)

	sc := NewTaskScope()
	sc.Set([]string{"src/**"})
	ctx := WithTaskScope(context.Background(), sc)

	commit := fakeTool{name: "git_commit", cap: tool.CapWrite}
	if ok, reason := gate.Check(ctx, commit, json.RawMessage(`{"message":"wip"}`)); !ok {
		t.Errorf("pathless write-capability tool should not be scope-blocked, got %q", reason)
	}
}

func TestScopeGateReadsUnrestricted(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewScopeGate(base, nil)

	sc := NewTaskScope()
	sc.Set([]string{"src/**"})
	ctx := WithTaskScope(context.Background(), sc)

	read := fakeTool{name: "read_file", cap: tool.CapRead,
		schema: json.RawMessage(`{"properties":{"path":{}}}`)}
	if ok, reason := gate.Check(ctx, read, json.RawMessage(`{"path":"internal/secret.go"}`)); !ok {
		t.Errorf("reads must never be scope-restricted, got %q", reason)
	}
}

// TestScopeGateUsesEffectiveCapability is the ScopeGate analogue of
// TestNetworkAllowListUsesEffectiveCapability (P32.2), covering P63.3:
// Check must gate on tool.EffectiveCapability, not the tool's static
// Capability(). A tool whose static capability is CapRead but that
// reclassifies to CapWrite for this specific call (the CapabilityOverrider
// seam P25.4c added) must still be confined by the active task scope —
// before the fix, ScopeGate.Check read t.Capability() directly, so the
// `== tool.CapWrite` comparison never matched and the outermost containment
// gate was silently bypassed for that call.
func TestScopeGateUsesEffectiveCapability(t *testing.T) {
	base := New(ModeBuild, AutoApprove{})
	gate := NewScopeGate(base, nil)

	sc := NewTaskScope()
	sc.Set([]string{"src/**"})
	ctx := WithTaskScope(context.Background(), sc)

	writeClassified := fakeOverrideTool{
		fakeTool:    fakeTool{name: "odd_tool", cap: tool.CapRead},
		overrideCap: tool.CapWrite,
	}

	if ok, _ := gate.Check(ctx, writeClassified, json.RawMessage(`{"path":"internal/secret.go"}`)); ok {
		t.Error("an out-of-scope write must be blocked even when the write capability comes from CapabilityFor, not Capability()")
	}

	// In-scope paths still pass, so the widened classification restricts
	// rather than blanket-denies.
	if ok, reason := gate.Check(ctx, writeClassified, json.RawMessage(`{"path":"src/app.go"}`)); !ok {
		t.Errorf("in-scope write should be allowed, got %q", reason)
	}
}
