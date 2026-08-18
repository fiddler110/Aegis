package enginecfg

import (
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/persona"
)

// PersonaModel resolves the effective model for a persona: a config-level
// override (`personas.<name>.model`) wins, then the persona file's own `model:`
// frontmatter, then the global provider.model.
//
// The precedence is the load-bearing part and it is not new — the daemon has
// resolved persona models this way since P14.7. What is new (P69.1) is that it
// lives here rather than only on *Server, because `aegis debate` is headless
// and resolves the same question with no daemon at all. Two copies of a
// precedence rule is how the CLI and the daemon end up disagreeing about which
// model a persona runs on, which on a single-GPU local backend is not a
// cosmetic difference: it decides which weights are resident.
func PersonaModel(cfg *config.Config, p persona.Persona) string {
	if cfg == nil {
		return p.Model
	}
	if ov, ok := cfg.Personas[p.Name]; ok && ov.Model != "" {
		return ov.Model
	}
	if p.Model != "" {
		return p.Model
	}
	return cfg.Provider.Model
}

// DebateSeatModel resolves the model for one debate seat by persona name.
// A persona that does not resolve (an unknown or removed name) yields a
// zero Persona, which falls through to the global provider.model — the same
// model the seat would have run on before per-seat resolution existed. An
// unresolvable persona must not leave a seat with no model at all.
func DebateSeatModel(cfg *config.Config, personaName string) string {
	p, _ := persona.Get(personaName)
	if p.Name == "" {
		p.Name = personaName
	}
	return PersonaModel(cfg, p)
}
