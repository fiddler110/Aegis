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

// Narrowing must never *widen*: a tool hidden for another reason (permission
// mode, deferred registration) stays hidden even when a phase names it.
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
