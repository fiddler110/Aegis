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
