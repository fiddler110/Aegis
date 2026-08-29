package enginecfg

import (
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/providerfactory"
)

// Backend is the second half of the "every entry point must decide this
// identically" set that Limits opened. Limits answers what a run may *spend*;
// Backend answers how it *talks to the model server* — the sampling knobs, the
// non-native tool-calling fallback, and the backend identification that admits
// a turn's reported prompt count as a calibration sample.
//
// It exists for the same reason and against the same evidence. The `cost:`
// bounds had drifted at three of eight engine.New sites; these four had drifted
// at four of them, and worse, because two of the fields are not bounds an
// operator can afford to have quietly relaxed:
//
//   - ToolCallShim was set at the daemon session, the in-process sub-agent and
//     the subprocess worker with a written reason at each ("a teammate talks to
//     the same model server as its parent"), and omitted at both debate paths.
//     A shimmed deployment is one whose model cannot speak the tool protocol at
//     all, so an unshimmed debate role there is not degraded — it is unable to
//     call a tool, on the one code path whose whole purpose is adversarial
//     review of work that needs tools.
//   - SharedContextWindow gates the P66.14/LLM-03 token-estimate calibration.
//     A sub-agent and a debate role run against the same Ollama server as the
//     turn that spawned them, so their turns are the same class of sample.
//   - Temperature and Seed are the reproducibility pair. `provider.seed` pins
//     the backend RNG so a run can be replayed; a debate whose seats ignored it
//     made every run that contained one unreproducible, silently.
//
// MaxTokens rides here because it is the per-request cap on the same seam, and
// it was already being copied by hand at every site.
type Backend struct {
	MaxTokens   int
	Temperature *float64
	Seed        *int
	// ToolCallShim switches a run to the non-native tool-calling fallback
	// (P53.6). It is never engaged automatically — this reads the operator's
	// explicit config, exactly as each hand-rolled copy did.
	ToolCallShim bool
	// SharedContextWindow declares a backend positively identified as spending
	// one budget on prompt and completion, and truncating an oversized prompt
	// in silence (P66.14/LLM-03). Never a guess: it is
	// providerfactory.CertainlyOllama, not a heuristic.
	SharedContextWindow bool
}

// ModelBackend reads the backend parameters out of config.
func ModelBackend(cfg *config.Config) Backend {
	if cfg == nil {
		return Backend{}
	}
	return Backend{
		MaxTokens:           cfg.Provider.MaxTokens,
		Temperature:         cfg.Provider.Temperature,
		Seed:                cfg.Provider.Seed,
		ToolCallShim:        cfg.Provider.ToolCallShimEnabled(),
		SharedContextWindow: providerfactory.CertainlyOllama(cfg.Provider),
	}
}

// WithoutSharedContextWindow returns a copy with the calibration admission
// cleared, for a caller talking to a *different* server than the one config
// describes.
//
// Nothing in the tree needs it today — every sub-run reaches the same backend
// as its parent — but the field is the one on this struct where inheriting by
// default is an assumption rather than a fact, so declining it is a named
// call rather than a field a future caller has to remember to unset.
func (b Backend) WithoutSharedContextWindow() Backend {
	b.SharedContextWindow = false
	return b
}

// Apply writes the backend parameters into an engine.Options under
// construction. Like Limits.Apply it sets only the fields it owns, so a caller
// can fill the rest of the struct in any order around it.
func (b Backend) Apply(o *engine.Options) {
	o.MaxTokens = b.MaxTokens
	o.Temperature = b.Temperature
	o.Seed = b.Seed
	o.ToolCallShim = b.ToolCallShim
	o.SharedContextWindow = b.SharedContextWindow
}
