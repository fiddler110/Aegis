package enginecfg

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
)

// GuardModel picks the model output-guard verdict calls run on. In order:
// an explicit output_guard.model, then — on a *cloud* provider — the configured
// provider.small_model, the same preference session titles and compaction have;
// otherwise the session model itself.
//
// The small-model preference exists because a small non-thinking model makes
// the guard's strict "reply exactly PASS" contract actually satisfiable and
// keeps the extra call cheap; running the verdict on a deep/thinking session
// model tripled turn latency and fail-closed nearly every passing answer in the
// P25.3 live eval. That reasoning holds on Anthropic/OpenAI, where the two
// models are separate remote capacity.
//
// It inverts on a single local Ollama server (P59.5). The guard fires on every
// final answer plus its corrective retries, and each call naming a model other
// than the resident one can evict that resident model and force a full cold
// reload on the next turn — on a 16GB-VRAM box, every post-guard turn. That is
// precisely the churn the bounded keep_alive default
// (providerfactory.defaultOllamaKeepAlive) and the P33.9 load_duration
// telemetry were built to eliminate, and a cold reload costs far more than the
// verdict call saves. So locally the guard runs on the model already loaded,
// and an operator with the VRAM to hold two picks the second one deliberately
// via output_guard.model rather than inheriting it from a key meant for
// compaction and titles.
func GuardModel(cfg *config.Config, sessionModel string) string {
	if cfg == nil {
		return sessionModel
	}
	if m := strings.TrimSpace(cfg.OutputGuard.Model); m != "" {
		return m
	}
	p := cfg.Provider
	if strings.EqualFold(p.Default, "ollama") || strings.Contains(p.BaseURL, ":11434") {
		return sessionModel
	}
	if p.SmallModel != "" {
		return p.SmallModel
	}
	return sessionModel
}

// OutputGuardConfig merges the global output-guard default with a persona's
// override into a guard.Config.
func OutputGuardConfig(cfg *config.Config, p persona.Persona, logger *slog.Logger) guard.Config {
	c := guard.Config{}
	if cfg != nil {
		c = guard.Config{
			Mode:       cfg.OutputGuard.Mode,
			Rubric:     cfg.OutputGuard.Rubric,
			MaxRetries: cfg.OutputGuard.MaxRetries,
		}
	}
	if p.Guard == nil {
		return c
	}
	if p.Guard.Disabled {
		// A loaded (non-built-in) persona is untrusted content (P7.5),
		// the same as its Mode and Rules fields: honoring "output_guard:
		// none" unconditionally would let a project-level persona.md
		// silently switch off the last safety net with no warning
		// surfaced anywhere. Built-in personas are reviewed and shipped
		// with Aegis, so they remain trusted to disable the guard.
		if p.Loaded {
			if logger != nil {
				logger.Warn("ignoring output_guard: none from untrusted (loaded) persona", "persona", p.Name)
			}
		} else {
			return guard.Config{Disabled: true}
		}
	}
	if p.Guard.Mode != "" {
		c.Mode = p.Guard.Mode
	}
	if len(p.Guard.Schema) > 0 {
		c.Schema = p.Guard.Schema
	}
	if p.Guard.Rubric != "" {
		c.Rubric = p.Guard.Rubric
	}
	if p.Guard.MaxRetries > 0 {
		c.MaxRetries = p.Guard.MaxRetries
	}
	return c
}

// GuardOptions are the three engine fields the output guard occupies. They are
// returned together because they are only ever correct together: a guard
// function with the wrong retry count re-asks the wrong number of times, and a
// schema format without the guard that motivated it constrains decoding for no
// reason.
type GuardOptions struct {
	Func       guard.Func
	MaxRetries int
	Format     json.RawMessage
}

// Apply writes the guard fields into an engine.Options under construction.
func (g GuardOptions) Apply(o *engine.Options) {
	o.OutputGuard = g.Func
	o.OutputGuardMaxRetries = g.MaxRetries
	o.OutputGuardFormat = g.Format
}

// OutputGuard resolves the P25.3 second-pass output guard for a run.
//
// adapter must already be wrapped for the guard model's own context window
// where the caller can resolve one (P52.4) — that is a per-turn decision the
// daemon makes and a one-shot CLI process cannot.
//
// P66.13/ARCH-06: `aegis chat` had no output guard at all, so a configured
// rubric or schema — the last check on what the model actually returns — applied
// on the TUI and silently not on the scripted path.
func OutputGuard(cfg *config.Config, p persona.Persona, adapter provider.Adapter, guardModel string, logger *slog.Logger) GuardOptions {
	gc := OutputGuardConfig(cfg, p, logger)
	fn, retries := guard.Resolve(gc, adapter, guardModel)
	var format json.RawMessage
	// P59.8: a schema guard's requirement is expressible to the backend ahead
	// of generation, so the corrective retry is decoded under it instead of
	// being asked in prose and checked afterwards. Only schema mode has a
	// machine-checkable shape — an llm-mode rubric is prose, and there is
	// nothing to compile.
	if fn != nil && gc.Mode == "schema" {
		format = guard.SchemaFormat(gc.Schema)
	}
	return GuardOptions{Func: fn, MaxRetries: retries, Format: format}
}
