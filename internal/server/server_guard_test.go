package server

import (
	"testing"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/persona"
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
