package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// preloadFakeTool is a minimal registrable tool for exercising the
// deferred/exposed transitions without pulling in a real tool's dependencies.
type preloadFakeTool struct{ name string }

func (f *preloadFakeTool) Name() string                 { return f.name }
func (f *preloadFakeTool) Description() string          { return f.name + " description" }
func (f *preloadFakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *preloadFakeTool) Capability() tool.Capability  { return tool.CapRead }
func (f *preloadFakeTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}

func exposedNames(reg *tool.Registry) map[string]bool {
	out := map[string]bool{}
	for _, s := range reg.Schemas() {
		out[s.Name] = true
	}
	return out
}

func deferredNames(reg *tool.Registry) map[string]bool {
	out := map[string]bool{}
	for _, d := range reg.Deferred() {
		out[d.Name] = true
	}
	return out
}

// TestPreloadPersonaToolsExposesDeferredDeclaredTools is the P34.3 regression:
// a persona that declares a deferred tool must get it exposed at activation,
// so the model never reaches for the wrong tool while the right one is
// invisible pending a tool_search call.
func TestPreloadPersonaToolsExposesDeferredDeclaredTools(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&preloadFakeTool{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"recon_scan", "dast_scan", "latex_build"} {
		if err := reg.RegisterDeferred(&preloadFakeTool{name: n}); err != nil {
			t.Fatal(err)
		}
	}

	loaded := preloadPersonaTools(reg, persona.Persona{
		Name:  "red-team",
		Tools: []string{"read_file", "recon_scan", "dast_scan"},
	})

	if got, want := strings.Join(loaded, ","), "recon_scan,dast_scan"; got != want {
		t.Errorf("preloadPersonaTools returned %q, want %q (only the deferred declared tools)", got, want)
	}

	exposed := exposedNames(reg)
	for _, n := range []string{"recon_scan", "dast_scan"} {
		if !exposed[n] {
			t.Errorf("%q declared by the persona but not exposed after preload", n)
		}
	}
	// A deferred tool the persona never declared stays deferred — preload is
	// scoped to the persona's working set, not a blanket "load everything".
	if exposed["latex_build"] {
		t.Error("latex_build is not in the persona's Tools list but was exposed anyway")
	}
	if !deferredNames(reg)["latex_build"] {
		t.Error("latex_build should still be advertised as deferred")
	}
	// Preloaded tools drop out of the deferred advertisement, so the prompt
	// stops telling the model to tool_search for what it can already see.
	if d := deferredNames(reg); d["recon_scan"] || d["dast_scan"] {
		t.Errorf("preloaded tools still advertised as deferred: %v", d)
	}
}

// TestPreloadPersonaToolsIgnoresUnknownAndNonDeferred guards the advisory
// contract (P7.5): a persona's Tools list can name anything, and preload must
// only ever move a *registered, currently-deferred* tool to exposed.
func TestPreloadPersonaToolsIgnoresUnknownAndNonDeferred(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&preloadFakeTool{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterDeferred(&preloadFakeTool{name: "recon_scan"}); err != nil {
		t.Fatal(err)
	}
	// Something else deliberately hid this one; preload must not undo that.
	reg.SetExposed("read_file", false)

	loaded := preloadPersonaTools(reg, persona.Persona{
		Name:  "typo-ridden",
		Tools: []string{"no_such_tool", "read_file", ""},
	})
	if len(loaded) != 0 {
		t.Errorf("preloadPersonaTools loaded %v, want nothing (no deferred tool declared)", loaded)
	}
	if exposedNames(reg)["read_file"] {
		t.Error("preload re-exposed a tool that was deliberately un-exposed")
	}

	// An empty Tools list (e.g. the `general` persona) is a no-op.
	if loaded := preloadPersonaTools(reg, persona.Persona{Name: "general"}); len(loaded) != 0 {
		t.Errorf("empty Tools list loaded %v, want nothing", loaded)
	}
	if loaded := preloadPersonaTools(nil, persona.Persona{Tools: []string{"recon_scan"}}); len(loaded) != 0 {
		t.Errorf("nil registry loaded %v, want nothing", loaded)
	}
}

// TestPreloadPersonaToolsDoesNotLeakAcrossSessions is the P9 invariant applied
// to P34.3: preloading a persona's tools onto one session's registry clone must
// not expose them daemon-wide, or a red-team session would silently widen the
// tools offered to every other session and persona.
func TestPreloadPersonaToolsDoesNotLeakAcrossSessions(t *testing.T) {
	base := tool.NewRegistry()
	if err := base.RegisterDeferred(&preloadFakeTool{name: "recon_scan"}); err != nil {
		t.Fatal(err)
	}
	sessionA, sessionB := base.Clone(), base.Clone()

	preloadPersonaTools(sessionA, persona.Persona{Name: "red-team", Tools: []string{"recon_scan"}})

	if !exposedNames(sessionA)["recon_scan"] {
		t.Fatal("session A did not get its own persona's tool")
	}
	if exposedNames(base)["recon_scan"] {
		t.Error("preload leaked back to the daemon-wide registry")
	}
	if exposedNames(sessionB)["recon_scan"] {
		t.Error("preload leaked sideways into a sibling session")
	}
	if !deferredNames(sessionB)["recon_scan"] {
		t.Error("sibling session should still see recon_scan as deferred")
	}
}

// TestRedTeamPersonaPreloadsItsScanTools is the concrete case P34.3 was filed
// from, run against the real built-in persona and the real built-in registry:
// red-team names recon_scan/dast_scan in its Tools list and its prose, but both
// are registered deferred, so a live qwen3:14b tried security_scan — the
// source-code scanner, wrong for a network target — twice before being told to
// call tool_search.
func TestRedTeamPersonaPreloadsItsScanTools(t *testing.T) {
	p, ok := persona.Get("red-team")
	if !ok {
		t.Fatal("built-in red-team persona not found")
	}
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	// Precondition: these are deferred, which is what makes the fix necessary.
	for _, n := range []string{"recon_scan", "dast_scan"} {
		if !deferredNames(reg)[n] {
			t.Fatalf("precondition failed: %q is not deferred, so P34.3 does not apply to it", n)
		}
	}

	preloadPersonaTools(reg, p)

	exposed := exposedNames(reg)
	for _, n := range []string{"recon_scan", "dast_scan", "security_advise"} {
		if !exposed[n] {
			t.Errorf("red-team declares %q but it is not exposed after persona activation", n)
		}
	}
}

// TestEffectiveSystemDropsPreloadedToolsFromDeferredBlock covers the other half
// of the fix: the advertisement is built from the session's own registry, so a
// preloaded tool is not still described as "not loaded yet". Otherwise the
// prompt would contradict the schema list and re-invite the tool_search
// round-trip P34.3 exists to remove.
func TestEffectiveSystemDropsPreloadedToolsFromDeferredBlock(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	reg := tool.NewRegistry()
	if err := reg.RegisterDeferred(&preloadFakeTool{name: "recon_scan"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterDeferred(&preloadFakeTool{name: "latex_build"}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, reg)
	srv.workspace = t.TempDir()

	const sessionID = "sess-1"
	preloadPersonaTools(srv.sessionToolRegistry(sessionID), persona.Persona{
		Name:  "red-team",
		Tools: []string{"recon_scan"},
	})

	sys := srv.effectiveSystem("base", sessionID)
	if strings.Contains(sys, "recon_scan") {
		t.Errorf("preloaded recon_scan still advertised as deferred in the session's prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "latex_build") {
		t.Errorf("latex_build is still deferred and should still be advertised:\n%s", sys)
	}

	// A session with no preload (and the no-session case) is unchanged.
	other := srv.effectiveSystem("base", "sess-2")
	if !strings.Contains(other, "recon_scan") {
		t.Errorf("another session lost the recon_scan advertisement:\n%s", other)
	}
}
