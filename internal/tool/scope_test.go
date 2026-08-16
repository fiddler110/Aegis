package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type namedStub struct{ name string }

func (s *namedStub) Name() string                 { return s.name }
func (s *namedStub) Description() string          { return "stub" }
func (s *namedStub) Capability() Capability       { return CapRead }
func (s *namedStub) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *namedStub) Execute(context.Context, json.RawMessage) (Result, error) {
	return Result{}, nil
}

func exposedNames(r *Registry) map[string]bool {
	out := map[string]bool{}
	for _, s := range r.Schemas() {
		out[s.Name] = true
	}
	return out
}

// Narrowing the schema array is the only tool-selection instruction a small
// model cannot ignore, so it has to actually remove schemas — and put every
// one of them back afterwards.
func TestScopeExposedNarrowsAndRestores(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"read_file", "write_file", "shell", "web_search"} {
		if err := r.Register(&namedStub{name: n}); err != nil {
			t.Fatal(err)
		}
	}
	before := exposedNames(r)
	if len(before) != 4 {
		t.Fatalf("expected 4 exposed tools, got %d", len(before))
	}

	restore := r.ScopeExposed([]string{"read_file", "write_file"})
	during := exposedNames(r)
	if len(during) != 2 || !during["read_file"] || !during["write_file"] {
		t.Errorf("scoped set = %v, want just read_file and write_file", during)
	}
	if during["web_search"] {
		t.Error("web_search survived narrowing — the detour stays possible")
	}

	restore()
	if after := exposedNames(r); len(after) != 4 {
		t.Errorf("restore left %v, want the original 4", after)
	}

	// Restore is idempotent: the drive defers it and also calls it explicitly
	// at the next phase boundary.
	restore()
	if after := exposedNames(r); len(after) != 4 {
		t.Errorf("second restore changed the set: %v", after)
	}
}

// Narrowing must never widen past a *permission* decision: a tool hidden by a
// SetExposed call stays hidden even when a phase names it.
func TestScopeExposedOnlyNarrows(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"read_file", "shell"} {
		if err := r.Register(&namedStub{name: n}); err != nil {
			t.Fatal(err)
		}
	}
	r.SetExposed("shell", false)

	restore := r.ScopeExposed([]string{"read_file", "shell"})
	if exposedNames(r)["shell"] {
		t.Error("scoping re-exposed a deliberately hidden tool")
	}
	restore()
	if exposedNames(r)["shell"] {
		t.Error("restore re-exposed a deliberately hidden tool")
	}
}

// A *deferred* tool is the one exception, and it is the case the drive's phase
// lists actually hit (P62.9): render_diagram and yaml_validate have been named
// by a phase and deferred in the registry since both were written, so the phase
// was handed a prompt naming a tool that was not in its array. Loading it for
// the scope is not an escalation — tool_search can load any deferred tool at
// any time — but it must go back to deferred afterwards, or one phase's surface
// leaks into the next and into the <deferred_tools> block.
func TestScopeExposedLoadsNamedDeferredTool(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&namedStub{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterDeferred(&namedStub{name: "render_diagram"}); err != nil {
		t.Fatal(err)
	}
	if exposedNames(r)["render_diagram"] {
		t.Fatal("a deferred tool is exposed before any scope")
	}

	restore := r.ScopeExposed([]string{"read_file", "render_diagram"})
	if !exposedNames(r)["render_diagram"] {
		t.Error("a phase named a deferred tool and did not get it")
	}

	restore()
	if exposedNames(r)["render_diagram"] {
		t.Error("restore left a deferred tool exposed")
	}
	// Still advertised as deferred, i.e. genuinely back where it started.
	var advertised bool
	for _, info := range r.Deferred() {
		if info.Name == "render_diagram" {
			advertised = true
		}
	}
	if !advertised {
		t.Error("after restore the tool is neither exposed nor advertised as deferred")
	}
}

// A deferred tool a phase does NOT name stays deferred: scoping loads what was
// declared, not everything the registry knows.
func TestScopeExposedLeavesUnnamedDeferredAlone(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&namedStub{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterDeferred(&namedStub{name: "web_search"}); err != nil {
		t.Fatal(err)
	}
	restore := r.ScopeExposed([]string{"read_file"})
	defer restore()
	if exposedNames(r)["web_search"] {
		t.Error("scoping loaded a deferred tool the phase never named")
	}
}

// An empty allowlist means "no opinion", not "hide everything" — phases that
// don't opt in must behave exactly as before.
func TestScopeExposedEmptyIsNoop(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&namedStub{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	restore := r.ScopeExposed(nil)
	if !exposedNames(r)["read_file"] {
		t.Error("an empty allowlist hid a tool")
	}
	restore()
	if !exposedNames(r)["read_file"] {
		t.Error("restore after a no-op scope hid a tool")
	}
}
