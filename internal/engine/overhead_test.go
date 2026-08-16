package engine

import (
	"testing"

	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestRequestOverheadFollowsAMidRunExposureChange is P66.14's PERF-03 half.
//
// The overhead was snapshotted in the guard's constructor, on the argument that a
// mid-run exposure change moves it by less than the estimate's own error. That is
// wrong by an order of magnitude: `tool_search` exposes a previously-deferred
// tool *during* the run, and a single schema is hundreds of estimated tokens
// against a small window's budget, where the calibrated estimate's residual error
// is a few percent. A snapshot taken before the load undercounts the trigger for
// the rest of the run, in the one direction the headroom check exists to avoid.
//
// Asserted as a difference against tokenest.Tools over the registry's own schemas
// rather than against a fixed count, so it survives any change to the fixture
// tools' descriptions.
func TestRequestOverheadFollowsAMidRunExposureChange(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&namedFakeTool{name: "exposed"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterDeferred(&namedFakeTool{name: "deferred"}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()

	before := g.overhead()
	if want := tokenest.Tools(reg.Schemas()); before != want {
		t.Fatalf("overhead = %d, want %d (the exposed schemas)", before, want)
	}

	// Exactly what tool_search does when the model asks for a deferred tool.
	if loaded := reg.Load("deferred"); len(loaded) != 1 {
		t.Fatalf("Load exposed %d tools, want 1", len(loaded))
	}

	after := g.overhead()
	if after <= before {
		t.Errorf("overhead after loading a deferred tool = %d, not above the %d before it — "+
			"the snapshot is stale and the compaction trigger is undercounting", after, before)
	}
	if want := tokenest.Tools(reg.Schemas()); after != want {
		t.Errorf("overhead = %d, want %d (the schemas as they now stand)", after, want)
	}
}

// TestRequestOverheadIsMemoized: the fix may not turn a per-turn estimate into a
// per-turn *re-render* of every schema. An unchanged exposed set must be answered
// from the memo, which is observable through the registry — Schemas() is itself
// cached, so a re-render is only detectable as the version read that guards it.
func TestRequestOverheadIsMemoized(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&namedFakeTool{name: "exposed"}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()

	first := g.overhead()
	version := reg.SchemaVersion()
	for i := 0; i < 5; i++ {
		if got := g.overhead(); got != first {
			t.Fatalf("overhead moved to %d on read %d with no exposure change", got, i)
		}
	}
	if got := reg.SchemaVersion(); got != version {
		t.Errorf("reading the overhead changed the registry's schema version (%d -> %d)", version, got)
	}
}

// TestNoRegistryHasNoOverhead: a run with no tools attaches nothing to its
// requests beyond conv, and must not panic reaching for a registry it does not
// have.
func TestNoRegistryHasNoOverhead(t *testing.T) {
	eng, err := New(Options{Adapter: endTurnAdapter(), Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := eng.newCompactionGuard().overhead(); got != 0 {
		t.Errorf("overhead with no registry = %d, want 0", got)
	}
}

// TestSchemaVersionMovesOnEveryExposureChange guards the signal the memo above
// depends on: a version that failed to move on one of these paths would leave the
// overhead stale exactly as the constructor snapshot did, and the memo would hide
// it more thoroughly than the snapshot did.
func TestSchemaVersionMovesOnEveryExposureChange(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&namedFakeTool{name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterDeferred(&namedFakeTool{name: "b"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		mut  func()
	}{
		{"Register", func() { _ = reg.Register(&namedFakeTool{name: "c"}) }},
		{"Upsert", func() { reg.Upsert(&namedFakeTool{name: "a"}) }},
		{"SetExposed", func() { reg.SetExposed("a", false) }},
		{"Load", func() { reg.Load("b") }},
		{"ScopeExposed", func() { _ = reg.ScopeExposed([]string{"a"}) }},
		{"RegisterDeferred", func() { _ = reg.RegisterDeferred(&namedFakeTool{name: "d"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := reg.SchemaVersion()
			tc.mut()
			if after := reg.SchemaVersion(); after == before {
				t.Errorf("%s did not move the schema version (still %d)", tc.name, before)
			}
		})
	}

	// And the restore half of a scope, which puts the exposed set back and must
	// be just as visible as the narrowing was.
	restore := reg.ScopeExposed([]string{"a"})
	before := reg.SchemaVersion()
	restore()
	if after := reg.SchemaVersion(); after == before {
		t.Errorf("ScopeExposed's restore did not move the schema version (still %d)", before)
	}
}

// TestOverheadAtCompactionTimeIsWhatTheEstimateUses closes the loop the two tests
// above open separately: the guard's estimate — the number every headroom
// decision in this file reads — has to move with the exposure change too, not
// merely the accessor.
func TestOverheadAtCompactionTimeIsWhatTheEstimateUses(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&namedFakeTool{name: "exposed"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterDeferred(&namedFakeTool{name: "deferred"}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: endTurnAdapter(), Tools: reg, Model: "test",
		MaxTokens: 100, ContextWindowTokens: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()
	conv := bigConversation()

	before := g.estimate(conv)
	reg.Load("deferred")
	if after := g.estimate(conv); after <= before {
		t.Errorf("estimate for an unchanged conversation = %d after loading a deferred tool, "+
			"not above the %d before it — the schemas the model was just handed are not being counted",
			after, before)
	}
}
