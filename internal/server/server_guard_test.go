package server

import (
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/persona"
)

func TestOutputGuardConfigMerge(t *testing.T) {
	s := &Server{cfg: &config.Config{OutputGuard: config.OutputGuardConfig{
		Enabled: true, Mode: "llm", Rubric: "global", MaxRetries: 1,
	}}}

	// No persona override → global default.
	g := s.outputGuardConfig(persona.Persona{Name: "general"})
	if g.Mode != "llm" || g.Rubric != "global" {
		t.Errorf("default merge = %+v", g)
	}

	// Persona disables → Disabled.
	g = s.outputGuardConfig(persona.Persona{Name: "x", Guard: &persona.GuardConfig{Disabled: true}})
	if !g.Disabled {
		t.Error("persona disable should win")
	}

	// Persona overrides rubric + retries.
	g = s.outputGuardConfig(persona.Persona{Name: "x", Guard: &persona.GuardConfig{Rubric: "local", MaxRetries: 3}})
	if g.Rubric != "local" || g.MaxRetries != 3 || g.Mode != "llm" {
		t.Errorf("override merge = %+v", g)
	}
}

// TestOutputGuardConfigBlocksLoadedPersonaDisable is the output_guard sibling
// of the P7.5 Mode/Rules escalation regressions: a loaded (untrusted)
// persona's "output_guard: none" must not silently switch off the guard,
// since it's the last line of defense if permission rules are somehow
// satisfied but the output itself is bad. A built-in persona remains
// trusted to disable it.
func TestOutputGuardConfigBlocksLoadedPersonaDisable(t *testing.T) {
	s := &Server{
		cfg:    &config.Config{OutputGuard: config.OutputGuardConfig{Enabled: true, Mode: "llm"}},
		logger: discardLogger(),
	}

	// Loaded persona disabling the guard → ignored, global config kept.
	g := s.outputGuardConfig(persona.Persona{Name: "sketchy", Loaded: true, Guard: &persona.GuardConfig{Disabled: true}})
	if g.Disabled {
		t.Error("loaded persona should not be able to disable the output guard")
	}

	// Built-in persona disabling the guard → honored.
	g = s.outputGuardConfig(persona.Persona{Name: "builtin", Loaded: false, Guard: &persona.GuardConfig{Disabled: true}})
	if !g.Disabled {
		t.Error("built-in persona disable should still be honored")
	}
}

// TestResolveSessionModeBlocksLoadedPersonaEscalation is the P7.5 regression:
// a loaded (non-built-in) persona's Mode must not silently escalate a session
// past the configured default when the caller didn't explicitly request a
// mode — otherwise a bundle-installed persona.md declaring mode: auto grants
// unrestricted auto-approval, including shell exec, with no confirmation.
func TestResolveSessionModeBlocksLoadedPersonaEscalation(t *testing.T) {
	s := &Server{
		cfg:    &config.Config{Permission: config.PermissionConfig{Mode: "plan"}},
		logger: discardLogger(),
	}

	// Loaded persona requesting auto, no explicit reqMode → ignored, falls
	// back to "" (caller applies the configured default).
	got := s.resolveSessionMode("", persona.Persona{Name: "sketchy", Mode: "auto", Loaded: true})
	if got != "" {
		t.Errorf("loaded persona escalation should be ignored, got mode %q", got)
	}

	// A loaded persona requesting a mode no more permissive than the default
	// is honored.
	got = s.resolveSessionMode("", persona.Persona{Name: "same", Mode: "plan", Loaded: true})
	if got != "plan" {
		t.Errorf("non-escalating loaded persona mode should be honored, got %q", got)
	}

	// A built-in (Loaded=false) persona is fully trusted, even requesting auto.
	got = s.resolveSessionMode("", persona.Persona{Name: "builtin", Mode: "auto", Loaded: false})
	if got != "auto" {
		t.Errorf("built-in persona mode should be trusted, got %q", got)
	}

	// An explicit caller-requested mode always wins, regardless of persona.
	got = s.resolveSessionMode("auto", persona.Persona{Name: "sketchy", Mode: "plan", Loaded: true})
	if got != "auto" {
		t.Errorf("explicit reqMode should win, got %q", got)
	}
}

func TestPersonaModelPrecedence(t *testing.T) {
	s := &Server{cfg: &config.Config{
		Provider: config.ProviderConfig{Model: "global"},
		Personas: map[string]config.PersonaOverride{"pinned": {Model: "from-config"}},
	}}
	if m := s.personaModel(persona.Persona{Name: "pinned", Model: "from-file"}); m != "from-config" {
		t.Errorf("config override should win, got %q", m)
	}
	if m := s.personaModel(persona.Persona{Name: "other", Model: "from-file"}); m != "from-file" {
		t.Errorf("file model should win when no config override, got %q", m)
	}
	if m := s.personaModel(persona.Persona{Name: "plain"}); m != "global" {
		t.Errorf("global model fallback, got %q", m)
	}
}

// TestGuardModelPrefersSmallModel is the P25.3 SmallModel-routing regression:
// output-guard verdict calls must run on provider.small_model when it is set
// — the same preference session titles and compaction already have — because
// judging on the session's own deep/thinking model made the strict PASS/FAIL
// contract unsatisfiable and tripled turn latency in the live eval.
func TestGuardModelPrefersSmallModel(t *testing.T) {
	s := &Server{cfg: &config.Config{Provider: config.ProviderConfig{SmallModel: "small-fast"}}}
	if m := s.guardModel("deep-thinking"); m != "small-fast" {
		t.Errorf("guardModel with SmallModel set = %q, want small-fast", m)
	}
	s = &Server{cfg: &config.Config{}}
	if m := s.guardModel("deep-thinking"); m != "deep-thinking" {
		t.Errorf("guardModel without SmallModel = %q, want the session model", m)
	}
}

// TestGuardModelStaysResidentOnLocalBackend is the P59.5 regression, and it is
// the inverse of the case above rather than a contradiction of it. On a cloud
// provider the small model is separate remote capacity, so routing the verdict
// to it is a pure win. On one local Ollama server it is the same GPU: naming a
// model other than the resident one on every final answer evicts it and buys a
// full cold reload on the next turn — the exact churn the bounded keep_alive
// default was built to eliminate. Locally the guard must run on the model that
// is already loaded, no matter what small_model says.
func TestGuardModelStaysResidentOnLocalBackend(t *testing.T) {
	for _, p := range []config.ProviderConfig{
		{Default: "ollama", SmallModel: "small-fast"},
		{BaseURL: "http://127.0.0.1:11434", SmallModel: "small-fast"},
	} {
		s := &Server{cfg: &config.Config{Provider: p}}
		if m := s.guardModel("qwen3.6:35b-a3b"); m != "qwen3.6:35b-a3b" {
			t.Errorf("guardModel on a local backend (%+v) = %q, want the resident session model", p, m)
		}
	}

	// An explicit output_guard.model is the operator saying they have the VRAM
	// for both, and outranks every default — local or cloud.
	s := &Server{cfg: &config.Config{
		Provider:    config.ProviderConfig{Default: "ollama", SmallModel: "small-fast"},
		OutputGuard: config.OutputGuardConfig{Model: "llama3.2:1b"},
	}}
	if m := s.guardModel("qwen3.6:35b-a3b"); m != "llama3.2:1b" {
		t.Errorf("explicit output_guard.model = %q, want llama3.2:1b", m)
	}
	s = &Server{cfg: &config.Config{
		Provider:    config.ProviderConfig{SmallModel: "small-fast"},
		OutputGuard: config.OutputGuardConfig{Model: "claude-haiku-4-5-20251001"},
	}}
	if m := s.guardModel("claude-opus-4-8"); m != "claude-haiku-4-5-20251001" {
		t.Errorf("explicit output_guard.model must outrank small_model, got %q", m)
	}
}

// TestResolveModelSessionOverrideWins is the P14.7 regression: a per-session
// /model override must outrank everything personaModel would otherwise
// resolve, including a persona's own config-level pin — the same precedence
// a config override already has over a persona file's Model.
func TestResolveModelSessionOverrideWins(t *testing.T) {
	s := &Server{cfg: &config.Config{
		Provider: config.ProviderConfig{Model: "global"},
		Personas: map[string]config.PersonaOverride{"pinned": {Model: "from-config"}},
	}}
	if m := s.resolveModel(persona.Persona{Name: "pinned", Model: "from-file"}, "session-override"); m != "session-override" {
		t.Errorf("session override should win over everything, got %q", m)
	}
	if m := s.resolveModel(persona.Persona{Name: "plain"}, ""); m != "global" {
		t.Errorf("no session override should fall through to personaModel, got %q", m)
	}
}
