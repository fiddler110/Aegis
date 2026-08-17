package server

import (
	"context"
	"fmt"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// roundCapFor builds the P67.1 aggregate round bound for an engine rooted at
// workdir.
//
// The root is bound here rather than read off the context inside builtin.CapRound
// because the engine's own toolCtx supplies it in the ordinary case and this is
// the fallback for the paths that do not set one — the same two-source rule every
// workspace-confined tool follows (effectiveRoot).
func roundCapFor(workdir string) engine.RoundCapFunc {
	return func(ctx context.Context, results []string) []string {
		return builtin.CapRound(ctx, workdir, results)
	}
}

func (s *Server) approver() permission.Approver {
	return enginecfg.Approver(s.cfg)
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

// guardModel and outputGuardConfig bind the daemon's config to the shared
// resolvers, where the reasoning behind each lives (enginecfg/guard.go). They
// stay as methods because the call sites below read better for it, not because
// the daemon decides either differently from `aegis chat`.
func (s *Server) guardModel(sessionModel string) string {
	return enginecfg.GuardModel(s.cfg, sessionModel)
}

func (s *Server) outputGuardConfig(p persona.Persona) guard.Config {
	return enginecfg.OutputGuardConfig(s.cfg, p, s.logger)
}

// buildGate binds the daemon's live state to the shared gate stack
// (enginecfg.BuildGate), which is where the layers and their evaluation order
// are documented. Mode clamping happens above this call (resolveSessionMode /
// clampMode); an empty persona.Persona{} skips the persona-specific layers
// (rules/tools), which is what sub-agent and debate runs pass since they have no
// persona of their own.
//
// Only three things here are the daemon's and cannot live in the shared
// constructor: the permission rules are read under permMu because an "allow
// always" approval adds one at runtime, the audit sink is a daemon-owned file,
// and s.hooks already carries the user's configured exec hooks.
func (s *Server) buildGate(mode string, approver permission.Approver, p persona.Persona) (engine.Gate, engine.Hooks) {
	s.permMu.Lock()
	rules := append([]permission.Rule{}, s.permRules...)
	s.permMu.Unlock()

	return enginecfg.BuildGate(enginecfg.GateOptions{
		Mode:       mode,
		Approver:   approver,
		Persona:    p,
		Security:   s.cfg.Security,
		Registry:   s.tools,
		Rules:      rules,
		Hooks:      s.hooks,
		OnDecision: s.recordPolicyDecision,
		Logger:     s.logger,
	})
}

// recordPolicyDecision writes one gate decision to the audit trail. Passed to
// every layer of the stack, so a decision is recorded wherever it was made.
func (s *Server) recordPolicyDecision(d permission.ContextualDecision) {
	if s.audit != nil {
		s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
	}
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

	var guardOpts enginecfg.GuardOptions
	if guardEnabled {
		// The guard verdict runs on its own model (usually provider.small_model),
		// so it gets that model's window too rather than the run model's (P52.4).
		gm := s.guardModel(model)
		guardWin := ctxWin
		if gm != model {
			guardWin, _ = s.effectiveContextWindowFor(context.Background(), gm)
		}
		guardOpts = enginecfg.OutputGuard(s.cfg, p, s.modelAdapter(guardWin), gm, s.logger)
	}

	if tracker == nil {
		tracker = cost.NewTracker()
	}
	opts := engine.Options{
		Adapter: s.modelAdapter(ctxWin),
		Tools:   tools,
		Gate:    gate,
		// P67.3: newEngine builds the engine for a session turn — the one call
		// class with a human watching this exact stream. Everything else the
		// daemon sends (compaction, the guard, a spawn, a title) declares its
		// own purpose at its own call site, so this stays the only foreground
		// tag in the server.
		Purpose:                 provider.PurposeForeground,
		Compactor:               s.compactor,
		Hooks:                   engineHooks,
		Cost:                    tracker,
		Temperature:             s.cfg.Provider.Temperature,
		Seed:                    s.cfg.Provider.Seed,
		Model:                   model,
		MaxTokens:               s.cfg.Provider.MaxTokens,
		ContextWindowTokens:     ctxWin,
		ContextWindowFloor:      func() int { return provider.RaisedContextWindow(s.adapter) },
		SteerChan:               steerCh,
		ZeroToolNudgeMaxRetries: s.cfg.Provider.ZeroToolNudge,
		ToolCallShim:            s.cfg.Provider.ToolCallShimEnabled(),
		// P67.1: the per-call caps in truncate.go are per *call*; this bounds
		// what a parallel round contributes in aggregate.
		RoundResultCap: roundCapFor(workdir),
		// P66.14/LLM-03: the token-estimate calibration is admissible only
		// against a backend positively identified as reporting the full prompt
		// each turn and truncating in silence. Previously gated on a native-only
		// telemetry field, so it was inert on the documented openai + :11434/v1
		// configuration.
		SharedContextWindow: providerfactory.CertainlyOllama(s.cfg.Provider),
		Logger:              s.logger,
		Workdir:             workdir,
		ExtraRoots:          s.workspaceRootsFor(workdir),
	}
	// P66.13/ARCH-06: the run bounds come from one shared reading of config, so
	// a new one reaches the CLI paths too instead of being set here and
	// forgotten there.
	enginecfg.CostLimits(s.cfg).Apply(&opts)
	guardOpts.Apply(&opts)
	// Gate: set above from s.buildGate, which is enginecfg.BuildGate bound to the
	// daemon's live rules and audit sink.
	eng, err := engine.New(opts)
	if err != nil {
		return nil, "", err
	}
	return eng, model, nil
}
