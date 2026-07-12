package server

import (
	"fmt"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
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

// guardModel picks the model output-guard verdict calls run on: the configured
// provider.small_model when set — the same preference session titles
// (sessions.go) and compaction (server.go) already have — otherwise the
// session model itself. A small non-thinking model makes the guard's strict
// "reply exactly PASS" contract actually satisfiable and keeps the extra call
// cheap; running the verdict on a deep/thinking session model tripled turn
// latency and fail-closed nearly every passing answer in the P25.3 live eval.
func (s *Server) guardModel(sessionModel string) string {
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

// buildGate assembles the shared permission gate stack — base mode gate →
// contextual egress/network policy → text allow/deny rules → persona tool
// advisory (outermost) — used by every engine run the daemon starts, top-level
// or sub-agent, so a spawned teammate can't bypass an operator's security
// posture just because it took a different code path to get an engine (P10.1).
// Mode clamping happens above this call (resolveSessionMode / clampMode); an
// empty persona.Persona{} skips the persona-specific layers (rules/tools),
// which is what sub-agent runs pass since they have no persona of their own.
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

	// Apply text-based allow/deny rules as the outermost gate so they are
	// evaluated before the contextual and mode gates. An explicit deny always
	// blocks; an explicit allow grants without prompting; otherwise the call
	// falls through to the gate(s) wrapped above.
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

	// A persona's declared Tools list is advisory only (P7.5: never a
	// security boundary) — wrapped outermost so a call outside the list is
	// flagged before the real allow/deny rules below run.
	if len(p.Tools) > 0 {
		gate = permission.NewPersonaToolGate(gate, p.Name, p.Tools, approver, s.logger,
			func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			})
	}

	return gate, engineHooks
}

// newEngine builds an engine for one turn. modelOverride, when non-empty, is
// a per-session model pin (P14.7 /model) that takes precedence over the
// persona's own Model and the global provider.model — same precedence a
// persona-level override already has over the global default.
func (s *Server) newEngine(mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool, tracker *cost.Tracker, tools *tool.Registry, modelOverride, workdir string) (*engine.Engine, error) {
	if s.adapter == nil {
		return nil, s.providerUnconfiguredErr()
	}
	if tools == nil {
		tools = s.tools
	}
	if approver == nil {
		approver = s.approver()
	}
	gate, engineHooks := s.buildGate(mode, approver, p)

	model := s.resolveModel(p, modelOverride)

	var guardFn guard.Func
	var guardRetries int
	if guardEnabled {
		guardFn, guardRetries = guard.Resolve(s.outputGuardConfig(p), s.adapter, s.guardModel(model))
	}

	if tracker == nil {
		tracker = cost.NewTracker()
	}
	// Effective window (config or Ollama-detected, see contextwindow.go)
	// rather than raw config, so proactive compaction fires before a local
	// server silently truncates the prompt.
	ctxWin, _ := s.effectiveContextWindow()
	return engine.New(engine.Options{
		Adapter:               s.adapter,
		Tools:                 tools,
		Gate:                  gate,
		Compactor:             s.compactor,
		Hooks:                 engineHooks,
		Cost:                  tracker,
		BudgetUSD:             s.cfg.Cost.BudgetUSD,
		MaxTokensPerRun:       s.cfg.Cost.MaxTokensPerRun,
		Model:                 model,
		MaxTokens:             s.cfg.Provider.MaxTokens,
		MaxIterations:         s.cfg.Provider.MaxIterations,
		LoopThreshold:         s.cfg.Provider.LoopThreshold,
		ContextWindowTokens:   ctxWin,
		SteerChan:             steerCh,
		OutputGuard:           guardFn,
		OutputGuardMaxRetries: guardRetries,
		RedactSecrets:         s.cfg.Security.RedactSecrets,
		Logger:                s.logger,
		Workdir:               workdir,
	})
}
