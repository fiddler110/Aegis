package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

func (s *Server) approver() permission.Approver {
	if s.cfg.Permission.AutoApproveExec {
		return permission.AutoApprove{}
	}
	return permission.AutoDeny{}
}

// providerUnconfiguredErr returns a helpful error message that names the
// specific environment variable the user needs to set for their configured
// provider, rather than always blaming ANTHROPIC_API_KEY.
func (s *Server) providerUnconfiguredErr() error {
	switch s.cfg.Provider.Default {
	case "openai":
		if s.cfg.Provider.BaseURL != "" {
			return fmt.Errorf("no model provider configured — run /config to reconfigure or restart the daemon after making changes")
		}
		return fmt.Errorf("no model provider configured (set OPENAI_API_KEY and restart the daemon)")
	default:
		return fmt.Errorf("no model provider configured (set ANTHROPIC_API_KEY and restart the daemon)")
	}
}

// personaModel resolves the effective model for a persona: a config override
// wins, then the persona's own Model, then the global provider model.
func (s *Server) personaModel(p persona.Persona) string {
	if ov, ok := s.cfg.Personas[p.Name]; ok && ov.Model != "" {
		return ov.Model
	}
	if p.Model != "" {
		return p.Model
	}
	return s.cfg.Provider.Model
}

// resolveModel is personaModel with the P14.7 per-session /model override
// layered on top: sessionModel, when non-empty, outranks everything —
// including a persona's own config-level pin — the same way a config
// override already outranks a persona file's Model.
func (s *Server) resolveModel(p persona.Persona, sessionModel string) string {
	if sessionModel != "" {
		return sessionModel
	}
	return s.personaModel(p)
}

// guardModel picks the model output-guard verdict calls run on. In order:
// an explicit output_guard.model, then — on a *cloud* provider — the configured
// provider.small_model, the same preference session titles (sessions.go) and
// compaction (server.go) have; otherwise the session model itself.
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
func (s *Server) guardModel(sessionModel string) string {
	if m := strings.TrimSpace(s.cfg.OutputGuard.Model); m != "" {
		return m
	}
	if s.isOllamaProvider() {
		return sessionModel
	}
	if s.cfg.Provider.SmallModel != "" {
		return s.cfg.Provider.SmallModel
	}
	return sessionModel
}

// outputGuardConfig merges the global output-guard default with a persona's
// override into a guard.Config.
func (s *Server) outputGuardConfig(p persona.Persona) guard.Config {
	c := guard.Config{
		Mode:       s.cfg.OutputGuard.Mode,
		Rubric:     s.cfg.OutputGuard.Rubric,
		MaxRetries: s.cfg.OutputGuard.MaxRetries,
	}
	if p.Guard != nil {
		if p.Guard.Disabled {
			// A loaded (non-built-in) persona is untrusted content (P7.5),
			// the same as its Mode and Rules fields: honoring "output_guard:
			// none" unconditionally would let a project-level persona.md
			// silently switch off the last safety net with no warning
			// surfaced anywhere. Built-in personas are reviewed and shipped
			// with Aegis, so they remain trusted to disable the guard.
			if p.Loaded {
				s.logger.Warn("ignoring output_guard: none from untrusted (loaded) persona", "persona", p.Name)
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
	}
	return c
}

// buildGate assembles the shared permission gate stack, used by every engine
// run the daemon starts, top-level or sub-agent, so a spawned teammate can't
// bypass an operator's security posture just because it took a different code
// path to get an engine (P10.1).
//
// The stack is built inside-out but evaluated outermost-first. In evaluation
// order:
//
//	Scope → PersonaTool → Rules → Contextual → Mode
//
// This doc comment is the one place that order is stated (P63.6). The layers
// below deliberately do not restate their own position relative to each other:
// three of them once did, each claiming to be "the outermost", and two were
// wrong — every one had been correct when written and none was updated as a
// layer was added above it. A wrong ordering claim on a permission stack is
// worse than no claim, so add new layers to the list here and describe only
// what each layer *does* at its own site.
//
// Every layer except Mode is conditional, so a given run may skip some: the
// contextual gate needs egress-then-write or a network allowlist configured,
// the rule and persona-tool layers need rules/tools to exist. Mode clamping
// happens above this call (resolveSessionMode / clampMode); an empty
// persona.Persona{} skips the persona-specific layers (rules/tools), which is
// what sub-agent runs pass since they have no persona of their own.
func (s *Server) buildGate(mode string, approver permission.Approver, p persona.Persona) (engine.Gate, engine.Hooks) {
	baseGate := permission.New(permission.ParseMode(mode), approver)

	var gate engine.Gate = baseGate
	engineHooks := s.hooks

	// Wrap with contextual security policies if any are enabled.
	if s.cfg.Security.EgressThenWrite || len(s.cfg.Security.NetworkAllowList) > 0 {
		ctxGate := permission.NewContextualGate(baseGate, permission.ContextualOpts{
			EgressThenWrite:  s.cfg.Security.EgressThenWrite,
			NetworkAllowList: s.cfg.Security.NetworkAllowList,
			Registry:         s.tools,
			OnDecision: func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			},
		})
		gate = ctxGate
		engineHooks = hooks.NewMulti(s.audit, ctxGate)
	}

	// Text-based allow/deny rules. An explicit deny always blocks; an explicit
	// allow grants without prompting; otherwise the call falls through to the
	// gate(s) wrapped above.
	s.permMu.Lock()
	rules := append([]permission.Rule{}, s.permRules...)
	s.permMu.Unlock()
	if len(p.Rules) > 0 {
		if pr, err := permission.ParseRules(p.Rules); err == nil {
			rules = append(rules, filterPersonaRules(pr, p, s.logger)...)
		} else {
			s.logger.Warn("ignoring invalid persona rules", "persona", p.Name, "err", err)
		}
	}
	if len(rules) > 0 {
		gate = permission.NewRuleGate(gate, rules,
			permission.WithRuleObserver(func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			}))
	}

	// A persona's declared Tools list is advisory only (P7.5: never a security
	// boundary) — it warns or prompts, and the real allow/deny rules it wraps
	// still decide.
	if len(p.Tools) > 0 {
		gate = permission.NewPersonaToolGate(gate, p.Name, p.Tools, approver, s.logger,
			func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			})
	}

	// Per-task file-write scope (P46.1). It binds hardest: an out-of-scope
	// write is refused even when a text allow-rule would grant it, since the
	// scope is a further restriction the model/skill opted into for one task,
	// not a competing permission. That is why it goes on last — anything added
	// after it would relax a containment the run asked for. A no-op until a
	// `scope` tool call activates a scope on the run's context.
	gate = permission.NewScopeGate(gate, func(d permission.ContextualDecision) {
		if s.audit != nil {
			s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
		}
	})

	return gate, engineHooks
}

// preloadPersonaTools exposes any deferred tool named in a persona's advisory
// Tools list, so a persona built around a deferred tool doesn't need a
// tool_search round-trip to discover what it was configured to use (P34.3).
// Observed live: red-team names recon_scan in both its Tools list and its
// prose, but recon_scan is deferred, so qwen3:14b reached for security_scan —
// the source-code scanner, wrong for a network target — twice before being
// told to call tool_search.
//
// Only currently-deferred, not-yet-loaded tools are touched, so this stays
// well inside the advisory contract (P7.5): it cannot register a tool the
// registry lacks, cannot re-expose one something else deliberately hid, and
// changes nothing about what the permission gate allows — it only moves a
// tool's schema from "advertised by name" to "offered", exactly what the
// model's own tool_search call would have done a turn later.
//
// reg must be the session-scoped clone (P9): preloading onto the daemon-wide
// registry would leak one persona's working set into every other session.
// Returns the names newly exposed.
func preloadPersonaTools(reg *tool.Registry, p persona.Persona) []string {
	if reg == nil || len(p.Tools) == 0 {
		return nil
	}
	deferred := make(map[string]bool)
	for _, d := range reg.Deferred() {
		deferred[d.Name] = true
	}
	var want []string
	for _, name := range p.Tools {
		if deferred[name] {
			want = append(want, name)
		}
	}
	if len(want) == 0 {
		return nil
	}
	var loaded []string
	for _, t := range reg.Load(want...) {
		loaded = append(loaded, t.Name())
	}
	return loaded
}

// modelAdapter returns the daemon's shared adapter with the serving context
// window this turn's model should be given attached to every request (P52.4).
//
// The adapter is built once at daemon start while the model is resolved per
// turn, so an unadorned s.adapter asks Ollama to serve a routed/pinned model
// with whatever num_ctx the *primary* model was configured with — on
// VRAM-constrained hardware that either over-allocates a KV cache the small
// model doesn't need or evicts the primary model to make room, which is the
// cold-reload churn between turns that load_duration telemetry exists to make
// visible. Wrapping per run keeps the window a property of the request (where
// the model already lives) instead of mutable adapter state. ctxWin <= 0
// returns the adapter untouched, so nothing changes for a caller with no
// detected window, and non-Ollama adapters ignore Request.NumCtx entirely.
func (s *Server) modelAdapter(ctxWin int) provider.Adapter {
	return provider.WithNumCtx(s.adapter, ctxWin)
}

// newEngine builds an engine for one turn, returning it alongside the model the
// turn resolved to (which the caller feeds back to maybeRefreshContextWindowFor
// once the run has loaded that model — P52.1). modelOverride, when non-empty,
// is a per-session model pin (P14.7 /model) that takes precedence over the
// persona's own Model and the global provider.model — same precedence a
// persona-level override already has over the global default. userText and
// priorMessages (the session's history *before* this turn's message is
// appended) feed P9.4 task routing — see turnModel/routeModel in routing.go
// — and are ignored entirely unless routing is opted into.
func (s *Server) newEngine(mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool, tracker *cost.Tracker, tools *tool.Registry, modelOverride, workdir, userText string, priorMessages []provider.Message) (*engine.Engine, string, error) {
	if s.adapter == nil {
		return nil, "", s.providerUnconfiguredErr()
	}
	if tools == nil {
		tools = s.tools
	}
	if approver == nil {
		approver = s.approver()
	}
	gate, engineHooks := s.buildGate(mode, approver, p)

	model, routingReason, routedToSmall := s.turnModel(p, modelOverride, userText, priorMessages)
	if routingReason != "" {
		if routedToSmall {
			s.logger.Debug("task routing: routed turn to small model", "model", model, "reason", routingReason)
		} else {
			s.logger.Debug("task routing: kept primary model", "model", model, "reason", routingReason)
		}
	}

	// Effective window (config or Ollama-detected, see contextwindow.go)
	// rather than raw config, so proactive compaction fires before a local
	// server silently truncates the prompt — resolved for *this turn's* model
	// (P52.1), after turnModel has picked it, since a persona pin or a routed
	// small model can have a very different window from the global default.
	ctxWin, _ := s.effectiveContextWindowFor(context.Background(), model)

	var guardFn guard.Func
	var guardRetries int
	var guardFormat json.RawMessage
	if guardEnabled {
		// The guard verdict runs on its own model (usually provider.small_model),
		// so it gets that model's window too rather than the run model's (P52.4).
		gm := s.guardModel(model)
		guardWin := ctxWin
		if gm != model {
			guardWin, _ = s.effectiveContextWindowFor(context.Background(), gm)
		}
		gc := s.outputGuardConfig(p)
		guardFn, guardRetries = guard.Resolve(gc, s.modelAdapter(guardWin), gm)
		// P59.8: a schema guard's requirement is expressible to the backend
		// ahead of generation, so the corrective retry is decoded under it
		// instead of being asked in prose and checked afterwards. Only schema
		// mode has a machine-checkable shape — an llm-mode rubric is prose, and
		// there is nothing to compile.
		if guardFn != nil && gc.Mode == "schema" {
			guardFormat = guard.SchemaFormat(gc.Schema)
		}
	}

	if tracker == nil {
		tracker = cost.NewTracker()
	}
	eng, err := engine.New(engine.Options{
		Adapter:                  s.modelAdapter(ctxWin),
		Tools:                    tools,
		Gate:                     gate,
		Compactor:                s.compactor,
		Hooks:                    engineHooks,
		Cost:                     tracker,
		BudgetUSD:                s.cfg.Cost.BudgetUSD,
		MaxTokensPerRun:          s.cfg.Cost.MaxTokensPerRun,
		MaxGeneratedTokensPerRun: s.cfg.Cost.MaxGeneratedTokensPerRun,
		MaxWallClockPerRun:       s.cfg.Cost.MaxWallClockPerRun(),
		Model:                    model,
		MaxTokens:                s.cfg.Provider.MaxTokens,
		MaxIterations:            s.cfg.Provider.MaxIterations,
		LoopThreshold:            s.cfg.Provider.LoopThreshold,
		ContextWindowTokens:      ctxWin,
		ContextWindowFloor:       func() int { return provider.RaisedContextWindow(s.adapter) },
		SteerChan:                steerCh,
		OutputGuard:              guardFn,
		OutputGuardMaxRetries:    guardRetries,
		OutputGuardFormat:        guardFormat,
		ZeroToolNudgeMaxRetries:  s.cfg.Provider.ZeroToolNudge,
		ToolCallShim:             s.cfg.Provider.ToolCallShimEnabled(),
		RedactSecrets:            s.cfg.Security.RedactSecrets,
		Logger:                   s.logger,
		Workdir:                  workdir,
		ExtraRoots:               s.workspaceRootsFor(workdir),
	})
	if err != nil {
		return nil, "", err
	}
	return eng, model, nil
}
