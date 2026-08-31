package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

type fakeTool struct {
	name string
	desc string
}

func (f *fakeTool) Name() string                 { return f.name }
func (f *fakeTool) Description() string          { return f.desc }
func (f *fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeTool) Capability() Capability       { return CapRead }
func (f *fakeTool) Execute(context.Context, json.RawMessage) (Result, error) {
	return Result{}, nil
}

func TestDeferredNotExposedUntilLoaded(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeTool{name: "read", desc: "read files"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterDeferred(&fakeTool{name: "latex_build", desc: "build a LaTeX document"}); err != nil {
		t.Fatal(err)
	}

	// Only the core tool is exposed.
	if got := len(r.Schemas()); got != 1 {
		t.Fatalf("exposed schemas = %d, want 1", got)
	}
	// The deferred tool is advertised.
	def := r.Deferred()
	if len(def) != 1 || def[0].Name != "latex_build" {
		t.Fatalf("Deferred() = %+v", def)
	}

	// Search finds it by keyword.
	matches := r.SearchDeferred("latex")
	if len(matches) != 1 {
		t.Fatalf("SearchDeferred = %d, want 1", len(matches))
	}

	// Loading exposes it and removes it from the advertisement.
	r.Load("latex_build")
	if got := len(r.Schemas()); got != 2 {
		t.Fatalf("after load exposed schemas = %d, want 2", got)
	}
	if got := len(r.Deferred()); got != 0 {
		t.Fatalf("after load Deferred() = %d, want 0", got)
	}
}

// TestCloneIsolatesExposure is the P9 regression: loading a deferred tool on
// a clone (as a per-session tool_search call would) must not expose it on the
// original registry or on a sibling clone — otherwise one session's
// tool_search call permanently exposes a tool's schema to every other
// concurrent or future session and persona sharing the daemon-wide registry.
func TestCloneIsolatesExposure(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDeferred(&fakeTool{name: "latex_build", desc: "build a LaTeX document"}); err != nil {
		t.Fatal(err)
	}

	sessionA := r.Clone()
	sessionB := r.Clone()
	sessionA.Load("latex_build")

	if got := len(sessionA.Schemas()); got != 1 {
		t.Errorf("session A (which loaded it) exposed schemas = %d, want 1", got)
	}
	if got := len(r.Schemas()); got != 0 {
		t.Errorf("original registry exposed schemas = %d, want 0 (session A's load must not leak back)", got)
	}
	if got := len(sessionB.Schemas()); got != 0 {
		t.Errorf("sibling session B exposed schemas = %d, want 0 (session A's load must not leak sideways)", got)
	}
	if got := len(sessionB.Deferred()); got != 1 {
		t.Errorf("sibling session B should still see the tool as deferred, Deferred() = %d, want 1", got)
	}
}

// TestCloneSharesLaterRegistrations verifies Clone's tools map is shared by
// reference: a tool registered on the original after cloning (e.g. an MCP
// server's dynamic tools/list_changed refresh calling Upsert) must still be
// visible through an existing clone, since tool registration itself is
// legitimately process-global — only exposure state is meant to be scoped.
func TestCloneSharesLaterRegistrations(t *testing.T) {
	r := NewRegistry()
	clone := r.Clone()

	if err := r.Register(&fakeTool{name: "late", desc: "registered after clone"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := clone.Get("late"); !ok {
		t.Error("a tool registered on the original after cloning should still be visible through the clone")
	}
}

// TestCloneUpsertStaysLocal is the other half of the sharing contract, and the
// non-racing half of P66.4/ARCH-01. Registration on the *parent* is shared
// (above); registration on a *clone* is not. Upsert on a clone used to write
// into the process-global map, so session A's session-scoped `skill` instance —
// carrying A's builtinEnabled list — silently became session B's, defeating
// the "dormant by default until named" guarantee across a session boundary.
func TestCloneUpsertStaysLocal(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeTool{name: "skill", desc: "shared skill tool"}); err != nil {
		t.Fatal(err)
	}
	sessionA := r.Clone()
	sessionB := r.Clone()

	sessionScoped := &fakeTool{name: "skill", desc: "session A's skill tool"}
	sessionA.Upsert(sessionScoped)
	sessionA.Upsert(&fakeTool{name: "threat_model_script", desc: "activated by session A"})

	if got, _ := sessionA.Get("skill"); got != sessionScoped {
		t.Error("session A should see its own upserted instance")
	}
	if got, _ := sessionB.Get("skill"); got == sessionScoped {
		t.Error("session A's upserted skill tool leaked into session B")
	}
	if got, _ := r.Get("skill"); got == sessionScoped {
		t.Error("session A's upserted skill tool leaked into the daemon-wide registry")
	}
	for name, reg := range map[string]*Registry{"sibling clone": sessionB, "parent": r} {
		if _, leaked := reg.Get("threat_model_script"); leaked {
			t.Errorf("a tool session A registered on its clone leaked into the %s", name)
		}
	}

	// A clone of a clone inherits the overlay by copy: a sub-agent starts from
	// its parent session's activated tools without being able to mutate them.
	child := sessionA.Clone()
	if got, _ := child.Get("skill"); got != sessionScoped {
		t.Error("a clone of a clone should inherit the parent clone's session-scoped tools")
	}
	child.Upsert(&fakeTool{name: "skill", desc: "child's own"})
	if got, _ := sessionA.Get("skill"); got != sessionScoped {
		t.Error("a sub-agent's Upsert must not reach back into its parent session")
	}
}

// TestCloneExposesToolsRegisteredAfterTheClone is CRIT-5, and the direction
// TestCloneSharesLaterRegistrations left untested: it checked that the clone
// could *look up* a tool the parent registered later, which worked, but not
// that the clone would *offer* it. It would not. The clone's exposed map was a
// copy taken before the tool existed, so Schemas() skipped the tool, and
// tool_search could not recover it either — the clone's deferred map did not
// have it either, so the tool was neither exposed nor deferred, i.e. invisible
// for the rest of the session. The reachable trigger is an MCP server sending
// notifications/tools/list_changed mid-conversation, which Upserts on the
// parent registry every live session shares.
func TestCloneExposesToolsRegisteredAfterTheClone(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeTool{name: "early", desc: "registered before the clone"}); err != nil {
		t.Fatal(err)
	}
	clone := r.Clone()
	if got := len(clone.Schemas()); got != 1 {
		t.Fatalf("clone schemas before the refresh = %d, want 1", got)
	}

	// The MCP refresh: a new tool appears on the parent.
	r.Upsert(&fakeTool{name: "late", desc: "added by an MCP list_changed refresh"})

	names := schemaNames(clone.Schemas())
	if len(names) != 2 || names[1] != "late" {
		t.Errorf("clone schemas after the refresh = %v, want [early late]", names)
	}
}

// TestCloneSeesAReplacedSchema is the stale-cache half of CRIT-5: a clone's
// schemaCache is only invalidated by its own writes, so a tool whose schema the
// refresh *replaced* kept serving the old one to that session forever.
func TestCloneSeesAReplacedSchema(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeTool{name: "search", desc: "original description"}); err != nil {
		t.Fatal(err)
	}
	clone := r.Clone()
	before := clone.Schemas()
	if len(before) != 1 || before[0].Description != "original description" {
		t.Fatalf("clone schemas = %+v", before)
	}
	version := clone.SchemaVersion()

	r.Upsert(&fakeTool{name: "search", desc: "replaced by the refresh"})

	after := clone.Schemas()
	if len(after) != 1 || after[0].Description != "replaced by the refresh" {
		t.Errorf("clone served a stale schema after the parent replaced it: %+v", after)
	}
	if clone.SchemaVersion() == version {
		t.Error("SchemaVersion did not move, so a consumer caching a derivation of Schemas() would never rebuild it")
	}
}

// TestCloneDeferredFallsThroughToTheParent covers the tool_search recovery
// path: a deferred tool the parent registers after the clone must be findable,
// and loading it on the clone must still be a clone-local decision.
func TestCloneDeferredFallsThroughToTheParent(t *testing.T) {
	r := NewRegistry()
	clone := r.Clone()
	sibling := r.Clone()

	if err := r.RegisterDeferred(&fakeTool{name: "late_deferred", desc: "deferred, added after the clone"}); err != nil {
		t.Fatal(err)
	}
	if got := len(clone.Deferred()); got != 1 {
		t.Fatalf("clone Deferred() = %d, want 1", got)
	}
	if got := clone.SearchDeferred("late"); len(got) != 1 {
		t.Fatalf("clone SearchDeferred = %d, want 1", len(got))
	}
	clone.Load("late_deferred")
	if got := len(clone.Schemas()); got != 1 {
		t.Errorf("clone schemas after loading = %d, want 1", got)
	}
	if got := len(sibling.Schemas()); got != 0 {
		t.Errorf("the load leaked to a sibling clone: schemas = %d, want 0", got)
	}
	if got := len(r.Schemas()); got != 0 {
		t.Errorf("the load leaked to the parent: schemas = %d, want 0", got)
	}
}

// TestCloneScopeExposedRestoresFallthrough pins that ScopeExposed on a clone
// narrows inherited exposure and, on restore, goes back to *inheriting* rather
// than freezing the parent's decision at scope-entry time.
func TestCloneScopeExposedRestoresFallthrough(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"read_file", "shell"} {
		if err := r.Register(&fakeTool{name: n, desc: n}); err != nil {
			t.Fatal(err)
		}
	}
	clone := r.Clone()

	restore := clone.ScopeExposed([]string{"read_file"})
	if got := schemaNames(clone.Schemas()); len(got) != 1 || got[0] != "read_file" {
		t.Errorf("scoped clone schemas = %v, want [read_file]", got)
	}
	restore()
	if got := len(clone.Schemas()); got != 2 {
		t.Errorf("clone schemas after restore = %d, want 2", got)
	}

	// After the restore the clone holds no opinion of its own, so a parent
	// change still reaches it.
	r.SetExposed("shell", false)
	if got := schemaNames(clone.Schemas()); len(got) != 1 || got[0] != "read_file" {
		t.Errorf("clone schemas after a parent change = %v, want [read_file]", got)
	}
}

func schemaNames(schemas []provider.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out
}
