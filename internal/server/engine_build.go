package server

import (
	"time"

	"context"
	"encoding/json"
	"fmt"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
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

// personaModel binds the daemon's config to the shared persona-model resolver,
// where the precedence (config override -> persona file -> global) is
// documented. It stays a method because the call sites below read better for
// it, not because the daemon resolves it differently from `aegis debate`.
func (s *Server) personaModel(p persona.Persona) string {
	return enginecfg.PersonaModel(s.cfg, p)
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
//
// workdir decides the default StrictPlanMode posture when the operator has
// not set `permission.plan_mode_shell_reads` explicitly (P81.20/FIND-20): an
// untrusted workdir (config.WorkspaceTrusted) now defaults to the strict
// posture, so a classifier defect in an unreviewed workspace is not a silent
// plan-mode bypass by default. Pass the root the run's tools will actually
// execute against — the same root callers already thread through for
// RoundResultCap/ExtraRoots.
func (s *Server) buildGate(mode string, approver permission.Approver, p persona.Persona, workdir string) (engine.Gate, engine.Hooks) {
	s.permMu.Lock()
	rules := append([]permission.Rule{}, s.permRules...)
	s.permMu.Unlock()

	return enginecfg.BuildGate(enginecfg.GateOptions{
		Mode:                mode,
		StrictPlanMode:      !s.cfg.Permission.PlanModeShellReadsEnabled(config.WorkspaceTrusted(workdir)),
		Approver:            approver,
		Persona:             p,
		Security:            s.cfg.Security,
		Registry:            s.tools,
		Rules:               rules,
		Hooks:               s.hooks,
		OnDecision:          s.recordPolicyDecision,
		Logger:              s.logger,
		SandboxBackendLabel: s.sandboxBackendLabel(),
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

// networkShapedSkills names built-in skills whose whole premise is reaching
// the web — deep-research today — so activating one should pre-Load
// web_search/web_fetch immediately rather than leaving them deferred behind
// tool_search (P25.6's LocalProfile). Not a general skill-tools mechanism
// (there is no `tools:` skill frontmatter, unlike persona.Tools): a
// single-purpose fix for the one skill this is actually true of, scoped to
// avoid inventing an abstraction nothing else needs yet.
var networkShapedSkills = map[string]bool{
	"deep-research": true,
}

// preloadNetworkToolsForSkill un-defers web_search/web_fetch for reg — the
// session-scoped clone, same requirement as preloadPersonaTools — when name
// is network-shaped. P71.10: a local-profile session that can't see
// web_fetch reaches for `shell` instead the first time a search looks empty,
// which bypasses the SSRF dialer, the untrusted-content wrapper and the
// injection scan those two tools carry. Loading them at skill-activation time
// (rather than waiting for the model to discover tool_search on its own, or
// never) closes that gap without touching the LocalProfile default for every
// other session.
func preloadNetworkToolsForSkill(reg *tool.Registry, name string) []string {
	if reg == nil || !networkShapedSkills[name] {
		return nil
	}
	deferred := make(map[string]bool)
	for _, d := range reg.Deferred() {
		deferred[d.Name] = true
	}
	var want []string
	for _, t := range []string{"web_search", "web_fetch"} {
		if deferred[t] {
			want = append(want, t)
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
func (s *Server) newEngine(sessionID, mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool, tracker *cost.Tracker, tools *tool.Registry, modelOverride, workdir, userText string, priorMessages []provider.Message, lastActivity time.Time) (*engine.Engine, string, error) {
	if s.adapter == nil {
		return nil, "", s.providerUnconfiguredErr()
	}
	if tools == nil {
		tools = s.tools
	}
	if approver == nil {
		approver = s.approver()
	}
	gate, engineHooks := s.buildGate(mode, approver, p, workdir)

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

	// P65.4: seed the engine's in-memory started-tool record from the durable
	// register — the calls a *previous* process started and never finished,
	// which is exactly what a daemon restart mid-turn leaves behind. A lookup
	// error is treated as "nothing pending" (the same conservative default a
	// nil map has always meant) rather than blocking the turn on a store
	// hiccup. s.opRegister is nil only in tests built without full server
	// wiring, in which case durable tracking is simply off, as before P65.4.
	var initialStarted []string
	var onToolStarted func(toolUseID, toolName string, input json.RawMessage)
	var onToolFinished func(toolUseID string)
	if s.opRegister != nil && sessionID != "" {
		if pending, err := s.opRegister.Pending(context.Background(), sessionID); err != nil {
			s.logger.Warn("opregister: pending lookup failed; resuming with no durable record", "session", sessionID, "err", err)
		} else {
			for _, pc := range pending {
				initialStarted = append(initialStarted, pc.ToolUseID)
			}
		}
		reg := s.opRegister
		onToolStarted = func(toolUseID, toolName string, input json.RawMessage) {
			if err := reg.MarkStarted(context.Background(), sessionID, toolUseID, toolName, input); err != nil {
				s.logger.Warn("opregister: mark started failed", "session", sessionID, "tool_use_id", toolUseID, "err", err)
			}
		}
		onToolFinished = func(toolUseID string) {
			if err := reg.MarkFinished(context.Background(), sessionID, toolUseID); err != nil {
				s.logger.Warn("opregister: mark finished failed", "session", sessionID, "tool_use_id", toolUseID, "err", err)
			}
		}
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
		Purpose: provider.PurposeForeground,
		// P67.6: when this conversation last saw a write, read off the session
		// row before the run appends anything. It is what makes the *resume*
		// case visible — the engine's own clock starts here and would measure a
		// gap of zero no matter how long the session had been sitting idle.
		LastActivityAt: lastActivity,
		Compactor:      s.compactor,
		Hooks:          engineHooks,
		Cost:           tracker,
		// P81.1: the same interactive approver the permission gate uses for an
		// Ask decision, reused here for the scan-hit decision point — one
		// approval round trip (KindApprovalRequest/TUI dialog), two callers.
		Approver:                approver,
		Model:                   model,
		ContextWindowTokens:     ctxWin,
		ContextWindowFloor:      func() int { return provider.RaisedContextWindow(s.adapter) },
		SteerChan:               steerCh,
		ZeroToolNudgeMaxRetries: s.cfg.Provider.ZeroToolNudge,
		// P67.1: the per-call caps in truncate.go are per *call*; this bounds
		// what a parallel round contributes in aggregate.
		RoundResultCap: roundCapFor(workdir),
		Logger:         s.logger,
		Workdir:        workdir,
		ExtraRoots:     s.workspaceRootsFor(workdir),
		// P71.6: newEngine's only caller (handlePostMessage) always has a real
		// session id here, so this always creates or reuses that session's own
		// cache — cleared in handleDeleteSession like the registry clone above.
		WebCache:            s.sessionWebCacheFor(sessionID),
		InitialStartedTools: initialStarted,
		OnToolStarted:       onToolStarted,
		OnToolFinished:      onToolFinished,
	}
	// P66.13/ARCH-06: the run bounds and the backend parameters both come from
	// one shared reading of config, so a new one reaches the CLI, sub-agent and
	// debate paths too instead of being set here and forgotten there.
	enginecfg.CostLimits(s.cfg).Apply(&opts)
	enginecfg.ModelBackend(s.cfg).Apply(&opts)
	guardOpts.Apply(&opts)
	// Gate: set above from s.buildGate, which is enginecfg.BuildGate bound to the
	// daemon's live rules and audit sink.
	eng, err := engine.New(opts)
	if err != nil {
		return nil, "", err
	}
	return eng, model, nil
}
