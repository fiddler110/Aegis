package server

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestConcurrentSkillActivationAcrossSessions is the P66.4/ARCH-01 regression,
// and it fails two independent ways against the unfixed tree.
//
// Registry.Clone copied the struct but shared the `tools` map by reference
// while giving each clone a fresh mutex, so:
//
//   - Deterministically, an Upsert on session A's clone wrote into the
//     process-global map. Session B's `skill` tool became A's instance,
//     carrying A's builtinEnabled list — the "dormant by default until named"
//     guarantee, broken across a session boundary.
//   - Probabilistically, two sessions activating concurrently (plus the
//     daemon-wide writer that MCP's tools/list_changed callback is) wrote one
//     map under two different locks. Go's runtime answer is `fatal error:
//     concurrent map writes`, which takes down the daemon and every session on
//     it — not just the offender.
//
// The race half needs -race (or luck) to surface; the leak half does not. Run
// with `go test -race ./internal/server/ -run TestConcurrentSkillActivation`.
func TestConcurrentSkillActivationAcrossSessions(t *testing.T) {
	cl, srv, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()

	a, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	b, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Materialize both clones before the goroutines start, so the test
	// exercises concurrent writes to the registration table rather than
	// concurrent creation of the clones.
	regA := srv.sessionToolRegistry(a.ID)
	regB := srv.sessionToolRegistry(b.ID)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			srv.activateSessionSkill(a.ID, "threat-modeling")
			srv.sess.skills.Delete(a.ID) // let it activate again next round
			_ = regA.Schemas()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			srv.activateSessionSkill(b.ID, "content-review")
			srv.sess.skills.Delete(b.ID)
			_ = regB.Deferred()
		}
	}()
	// The third writer: MCP's tools/list_changed callback Upserts on the
	// daemon-wide registry from its own goroutine, for the daemon's whole
	// lifetime (internal/mcp/tool.go). Nothing serializes it against the two
	// above.
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			srv.tools.Upsert(&preloadFakeTool{name: "mcp__srv__refresh"})
			_, _ = srv.tools.Get("skill")
		}
	}()
	wg.Wait()

	// The deterministic half: session-scoped tools stay session-scoped.
	if _, ok := srv.tools.Get("mcp__srv__refresh"); !ok {
		t.Error("an Upsert on the daemon-wide registry should be visible there")
	}
	skillA, okA := regA.Get("skill")
	skillB, okB := regB.Get("skill")
	if !okA || !okB {
		t.Fatal("both sessions should have a skill tool")
	}
	if skillA == skillB {
		t.Error("session A's session-scoped skill tool instance leaked into session B")
	}
	// A tool session A activated must not be reachable from session B or from
	// the daemon-wide registry.
	for _, tn := range threatModelToolNames(regA) {
		if _, leaked := regB.Get(tn); leaked {
			t.Errorf("session A's %q leaked into session B", tn)
		}
		if _, leaked := srv.tools.Get(tn); leaked {
			t.Errorf("session A's %q leaked into the daemon-wide registry", tn)
		}
	}
}

// threatModelToolNames returns the session-scoped script tools activation
// added to reg, if any — named by prefix rather than hardcoded, so the test
// keeps working if the skill's script set changes.
func threatModelToolNames(reg *tool.Registry) []string {
	var out []string
	for _, t := range reg.All() {
		if strings.HasPrefix(t.Name(), "threat_model") {
			out = append(out, t.Name())
		}
	}
	return out
}

// TestSubAgentToolSearchDoesNotWidenTheDaemon is the ARCH-02 regression:
// sub-agents ran against s.tools, so a teammate's tool_search call reached
// Registry.Load on the daemon-wide registry and permanently exposed that
// tool's schema to every session created afterwards — undoing the P9 session
// clone at exactly the seam it exists to protect.
func TestSubAgentToolSearchDoesNotWidenTheDaemon(t *testing.T) {
	cl, srv, cleanup := newSkillTestServer(t)
	defer cleanup()
	ctx := context.Background()

	if err := srv.tools.RegisterDeferred(&preloadFakeTool{name: "expensive_tool"}); err != nil {
		t.Fatal(err)
	}
	parent, err := cl.CreateSession(ctx, api.CreateSessionRequest{Mode: "plan"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	before := len(srv.tools.Schemas())
	child := srv.subAgentToolRegistry(parent.ID)
	// The regression itself was `Tools: s.tools` in subAgentRunner's
	// engine.Options — a teammate handed the daemon-wide registry outright.
	// Assert on identity as well as on effect, so reintroducing that line
	// fails here even if the effect assertions below are later weakened.
	if child == srv.tools {
		t.Fatal("a sub-agent must never run against the daemon-wide registry")
	}
	if child == srv.sessionToolRegistry(parent.ID) {
		t.Fatal("a sub-agent must not share its parent session's registry either")
	}
	if loaded := child.Load("expensive_tool"); len(loaded) != 1 {
		t.Fatalf("sub-agent should be able to load a deferred tool, got %d", len(loaded))
	}

	if got := len(srv.tools.Schemas()); got != before {
		t.Errorf("a sub-agent's tool_search widened the daemon-wide exposed set: %d schemas -> %d", before, got)
	}
	if findSchema(srv.sessionToolRegistry(parent.ID).Schemas(), "expensive_tool") {
		t.Error("a sub-agent's tool_search widened its parent session's exposed set")
	}
	if !findSchema(child.Schemas(), "expensive_tool") {
		t.Error("the sub-agent's own registry should see the tool it loaded")
	}
}

func findSchema(schemas []provider.ToolSchema, name string) bool {
	for _, s := range schemas {
		if s.Name == name {
			return true
		}
	}
	return false
}
