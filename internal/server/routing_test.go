package server

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
)

// TestClassifyTurn is the P9.4 classifier table: deterministic inputs to
// verdicts, covering the cases called out in the roadmap item — a short
// question, a long imperative multi-step request, a code-fence-containing
// message, and a mid-flight session that already made tool calls.
func TestClassifyTurn(t *testing.T) {
	toolHistory := []provider.Message{
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "1", Name: "read_file"},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "1", Content: "file contents"},
		}},
	}

	cases := []struct {
		name        string
		text        string
		history     []provider.Message
		wantComplex bool
	}{
		{
			name:        "short question",
			text:        "What does this function do?",
			wantComplex: false,
		},
		{
			name:        "short greeting",
			text:        "hi",
			wantComplex: false,
		},
		{
			name: "long imperative multi-step request",
			text: "Please refactor the authentication module to use the new session " +
				"store, update every call site across the codebase that still " +
				"references the old API, add regression tests covering the new " +
				"behavior, and make sure the documentation in docs/auth.md is " +
				"updated to reflect the change before you consider this done.",
			wantComplex: true,
		},
		{
			name:        "code fence",
			text:        "Why does this fail?\n```go\nfunc f() { panic(\"x\") }\n```",
			wantComplex: true,
		},
		{
			name:        "explicit numbered plan",
			text:        "Do the following:\n1. Update the schema\n2. Migrate the data\n3. Verify counts",
			wantComplex: true,
		},
		{
			name:        "explicit bulleted plan",
			text:        "Please:\n- fix the bug\n- add a test\n- update the changelog",
			wantComplex: true,
		},
		{
			name:        "mid-flight session with prior tool calls",
			text:        "ok now do the next part",
			history:     toolHistory,
			wantComplex: true,
		},
		{
			name:        "empty text",
			text:        "",
			wantComplex: true,
		},
		{
			name:        "compound multi-sentence request",
			text:        "Look at the file. Then tell me what's wrong. Then fix it.",
			wantComplex: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			complex, reason := classifyTurn(tc.text, tc.history)
			if complex != tc.wantComplex {
				t.Errorf("classifyTurn(%q) = (%v, %q), want complex=%v", tc.text, complex, reason, tc.wantComplex)
			}
			if reason == "" {
				t.Error("classifyTurn should always return a non-empty reason")
			}
		})
	}
}

// TestClassifyTurnLongMessageWithoutOtherSignals confirms sheer length alone
// (no code fence, no list, few sentences) still routes to complex once past
// the word/char thresholds — a detailed single-sentence spec is still not a
// "simple" turn.
func TestClassifyTurnLongMessageWithoutOtherSignals(t *testing.T) {
	longWord := strings.Repeat("a", complexCharThreshold+1)
	complex, reason := classifyTurn(longWord, nil)
	if !complex {
		t.Errorf("expected long single-token message to classify complex, got reason %q", reason)
	}
}

// TestRouteModelRequiresBothTaskRoutingAndSmallModel mirrors
// TestGuardModelPrefersSmallModel's shape: routing must be a no-op unless
// both provider.task_routing is enabled AND provider.small_model is
// configured, so a daemon that never opts in sees zero behavior change.
func TestRouteModelRequiresBothTaskRoutingAndSmallModel(t *testing.T) {
	simpleText := "hi"

	// Neither set.
	s := &Server{cfg: &config.Config{}}
	if model, reason, routed := s.routeModel("base", simpleText, nil); model != "base" || reason != "" || routed {
		t.Errorf("routing disabled = (%q, %q, %v), want (base, \"\", false)", model, reason, routed)
	}

	// SmallModel set but TaskRouting off.
	s = &Server{cfg: &config.Config{Provider: config.ProviderConfig{SmallModel: "small"}}}
	if model, _, routed := s.routeModel("base", simpleText, nil); model != "base" || routed {
		t.Errorf("TaskRouting off should be a no-op, got model=%q routed=%v", model, routed)
	}

	// TaskRouting on but no SmallModel configured.
	s = &Server{cfg: &config.Config{Provider: config.ProviderConfig{TaskRouting: true}}}
	if model, _, routed := s.routeModel("base", simpleText, nil); model != "base" || routed {
		t.Errorf("no SmallModel configured should be a no-op, got model=%q routed=%v", model, routed)
	}

	// Both set, simple text → routes to SmallModel.
	s = &Server{cfg: &config.Config{Provider: config.ProviderConfig{TaskRouting: true, SmallModel: "small-fast"}}}
	if model, reason, routed := s.routeModel("base", simpleText, nil); model != "small-fast" || reason == "" || !routed {
		t.Errorf("simple turn with routing enabled = (%q, %q, %v), want small-fast/non-empty reason/true", model, reason, routed)
	}

	// Both set, complex text → stays on base.
	if model, reason, routed := s.routeModel("base", "```code```", nil); model != "base" || reason == "" || routed {
		t.Errorf("complex turn with routing enabled = (%q, %q, %v), want base/non-empty reason/false", model, reason, routed)
	}
}

// TestTurnModelSessionOverrideWinsOverRouting is the P9.4 sibling of
// TestResolveModelSessionOverrideWins: an explicit per-session /model
// override must always win over task routing, even when TaskRouting is
// enabled, a SmallModel is configured, and the turn would otherwise classify
// as simple.
func TestTurnModelSessionOverrideWinsOverRouting(t *testing.T) {
	s := &Server{cfg: &config.Config{
		Provider: config.ProviderConfig{Model: "global", TaskRouting: true, SmallModel: "small-fast"},
	}}

	// No session override, simple turn → routes to the small model.
	if model, reason, routed := s.turnModel(persona.Persona{Name: "plain"}, "", "hi", nil); model != "small-fast" || reason == "" || !routed {
		t.Errorf("no override, simple turn = (%q, %q, %v), want small-fast routed", model, reason, routed)
	}

	// Explicit session override present → routing must not run at all, even
	// though the turn text alone would classify simple.
	if model, reason, routed := s.turnModel(persona.Persona{Name: "plain"}, "session-override", "hi", nil); model != "session-override" || reason != "" || routed {
		t.Errorf("session override = (%q, %q, %v), want session-override/no routing", model, reason, routed)
	}
}

// TestTurnModelDisabledMatchesResolveModel confirms that with routing off
// (the default), turnModel is exactly resolveModel — no behavior change for
// existing users who never touch provider.task_routing.
func TestTurnModelDisabledMatchesResolveModel(t *testing.T) {
	s := &Server{cfg: &config.Config{Provider: config.ProviderConfig{Model: "global"}}}
	p := persona.Persona{Name: "plain"}
	want := s.resolveModel(p, "")
	if model, reason, routed := s.turnModel(p, "", "any text at all, short or long, doesn't matter here", nil); model != want || reason != "" || routed {
		t.Errorf("routing disabled turnModel = (%q, %q, %v), want (%q, \"\", false)", model, reason, routed, want)
	}
}
