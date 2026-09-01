// Package enginecfg holds the engine-construction decisions that more than one
// entry point has to make identically: the permission gate stack, the run cost
// limits, and the built-in tool option set.
//
// It exists because of the dominant defect shape in this codebase — a mechanism
// built for one path that a second path silently bypasses (P66.13). The daemon
// stacks five permission layers; `aegis chat` built a bare mode gate, so
// `permission.rules` deny rules and `security.egress_then_write` were inert on
// the CLI. `aegis debate` did the same. The subprocess worker had its own
// hand-rolled three-layer copy that had drifted two layers behind. Each was
// correct when written and none was revisited when a layer was added — the same
// shape as P10.1 (the gate stack not crossing the worker's process boundary) and
// P62.10 (the local prompt profile passed at one of five Register call sites).
//
// The fix is a constructor, not four patched copies: adding a layer here reaches
// every entry point, and TestEveryEngineCallSiteDecidesItsGate fails the suite
// when a new engine is built without one or without a written reason.
package enginecfg

import (
	"log/slog"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/hooks"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/tool"
)

// GateOptions are the inputs to BuildGate. Everything except Mode and Approver
// is optional: a zero Persona skips the persona-specific layers, a zero Security
// skips the contextual gate, and a nil Logger/OnDecision simply drops the
// diagnostics.
type GateOptions struct {
	// Mode is the permission mode name ("plan", "build", "auto"). Any clamping
	// (resolveSessionMode / clampMode on the daemon) happens above this call.
	Mode string
	// StrictPlanMode refuses per-call capability downgrades in plan mode
	// (DR-2, `permission.plan_mode_shell_reads: false`). It rides on the gate
	// options rather than being read from config at each call site for the
	// usual reason this package exists: a posture knob every entry point must
	// apply identically belongs in the one constructor, not in four.
	StrictPlanMode bool
	// Approver answers a prompt-worthy call. Required — a nil approver would
	// make the mode gate panic rather than fail closed.
	Approver permission.Approver
	// Persona contributes advisory Tools and (deny-only, when loaded) Rules.
	// The zero value skips both layers, which is what sub-agent and debate runs
	// pass since they have no persona of their own.
	Persona persona.Persona
	// Security is the `security:` config block. Passing the struct rather than
	// its two fields is deliberate: a new contextual knob added to the config
	// reaches every entry point through this one call.
	Security config.SecurityConfig
	// Registry is the tool registry the contextual gate resolves capabilities
	// against. Pass the session-scoped clone where one exists.
	Registry *tool.Registry
	// Rules are the already-parsed text allow/deny rules. They are passed
	// parsed rather than as config strings because the daemon's set includes
	// rules added at runtime by an "allow always" approval, which no config
	// read can see. See ConfigRules for the plain-config case.
	Rules []permission.Rule
	// Hooks is the caller's existing hook chain (audit, user exec hooks). When
	// a contextual gate is built it is composed with — not replaced by — this.
	Hooks engine.Hooks
	// OnDecision receives every policy decision the stack makes, for the audit
	// trail. Nil is fine; the layers then simply do not report.
	OnDecision func(permission.ContextualDecision)
	// Logger receives the warnings about ignored persona rules. Nil is fine.
	Logger *slog.Logger
	// SandboxBackendLabel names the effective command-execution sandbox
	// backend (e.g. "container:docker", "os:bwrap", "local (unsandboxed)")
	// for this run (P81.22/FIND-22). Passed straight to the base mode gate so
	// an execute-capability approval prompt says what will actually contain
	// the command, not only a startup log line. Empty (the sub-agent/debate/
	// worker default, where the caller has no sandbox of its own to name)
	// skips the annotation.
	SandboxBackendLabel string
}

// BuildGate assembles the shared permission gate stack, used by every engine run
// Aegis starts — daemon or CLI, top-level or sub-agent — so a spawned teammate
// or a scripted `aegis chat` can't bypass an operator's security posture just
// because it took a different code path to get an engine (P10.1, P66.13).
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
// worse than no claim, so add new layers to the list here and describe only what
// each layer *does* at its own site.
//
// Every layer except Mode is conditional, so a given run may skip some: the
// contextual gate needs egress-then-write or a network allowlist configured, the
// rule and persona-tool layers need rules/tools to exist.
//
// It returns the gate and the hook chain to give the engine. The two are
// returned together because the contextual gate is *both* — it observes tool
// results to learn that egress happened — so a caller that took the gate and
// kept its own hooks would build a policy that never fires.
func BuildGate(o GateOptions) (engine.Gate, engine.Hooks) {
	baseGate := permission.New(permission.ParseMode(o.Mode), o.Approver)
	baseGate.Policy.StrictPlanMode = o.StrictPlanMode
	baseGate.SandboxBackendLabel = o.SandboxBackendLabel
	// M7: every layer above reports its decisions to o.OnDecision; the mode
	// gate reported nothing, so the one decision no layer explained was the
	// silent capability downgrade — the thing that lets an execute-capable tool
	// run with no approval. Same sink, one stream.
	baseGate.OnDecision = o.OnDecision

	var gate engine.Gate = baseGate
	engineHooks := o.Hooks

	// Wrap with contextual security policies if any are enabled.
	if o.Security.EgressThenWrite || len(o.Security.NetworkAllowList) > 0 || o.Security.TaintAfterUntrustedContent {
		ctxGate := permission.NewContextualGate(baseGate, permission.ContextualOpts{
			EgressThenWrite:            o.Security.EgressThenWrite,
			NetworkAllowList:           o.Security.NetworkAllowList,
			TaintAfterUntrustedContent: o.Security.TaintAfterUntrustedContent,
			Registry:                   o.Registry,
			OnDecision:                 o.OnDecision,
		})
		gate = ctxGate
		// Compose rather than replace. The daemon's version of this line built
		// a fresh hooks.NewMulti(s.audit, ctxGate), which silently dropped the
		// user's configured exec hooks (hooks.Exec) for the whole run whenever
		// egress-then-write or a network allowlist was on — a second instance
		// of the same "a second path bypasses a mechanism" shape this package
		// exists to close, found while extracting it.
		if engineHooks != nil {
			engineHooks = hooks.NewMulti(engineHooks, ctxGate)
		} else {
			engineHooks = hooks.NewMulti(ctxGate)
		}
	}

	// Text-based allow/deny rules. An explicit deny always blocks; an explicit
	// allow grants without prompting; otherwise the call falls through to the
	// gate(s) wrapped above.
	rules := append([]permission.Rule{}, o.Rules...)
	if len(o.Persona.Rules) > 0 {
		if pr, err := permission.ParseRules(o.Persona.Rules); err == nil {
			rules = append(rules, filterPersonaRules(pr, o.Persona, o.Logger)...)
		} else if o.Logger != nil {
			o.Logger.Warn("ignoring invalid persona rules", "persona", o.Persona.Name, "err", err)
		}
	}
	if len(rules) > 0 {
		gate = permission.NewRuleGate(gate, rules, permission.WithRuleObserver(o.OnDecision))
	}

	// A persona's declared Tools list is advisory by default (P7.5: never a
	// security boundary on its own) — it warns or prompts, and the real
	// allow/deny rules it wraps still decide. Persona.ToolsEnforced
	// (P81.20/FIND-20 item 4) opts a persona into treating the list as a hard
	// boundary instead, for an operator who wants a persona to double as
	// containment; advisory remains the default either way.
	if len(o.Persona.Tools) > 0 {
		gate = permission.NewPersonaToolGate(gate, o.Persona.Name, o.Persona.Tools, o.Persona.ToolsEnforced, o.Approver, o.Logger, o.OnDecision)
	}

	// Per-task file-write scope (P46.1). It binds hardest: an out-of-scope
	// write is refused even when a text allow-rule would grant it, since the
	// scope is a further restriction the model/skill opted into for one task,
	// not a competing permission. That is why it goes on last — anything added
	// after it would relax a containment the run asked for. A no-op until a
	// `scope` tool call activates a scope on the run's context.
	gate = permission.NewScopeGate(gate, o.OnDecision)

	return gate, engineHooks
}

// ConfigRules parses the persisted `permission.rules` strings, logging and
// dropping the whole set on a parse error rather than failing the run.
//
// This is the entry point for a process that reads config from disk and has no
// live daemon state — `aegis chat`, `aegis debate`, the subprocess worker. It is
// deliberately *not* what the daemon uses: a rule added by an "allow always"
// approval lives only in the daemon's in-memory set until it is persisted, and
// re-reading config would silently drop it.
func ConfigRules(cfg *config.Config, logger *slog.Logger) []permission.Rule {
	if cfg == nil || len(cfg.Permission.Rules) == 0 {
		return nil
	}
	rules, err := permission.ParseRules(cfg.Permission.Rules)
	if err != nil {
		if logger != nil {
			logger.Warn("ignoring invalid permission.rules from config", "err", err)
		}
		return nil
	}
	return rules
}

// Approver returns the configured default approver: AutoApprove only when the
// operator has opted into it, AutoDeny otherwise. Callers with an interactive
// prompt (the TUI) or an explicit --yes flag substitute their own.
func Approver(cfg *config.Config) permission.Approver {
	if cfg != nil && cfg.Permission.AutoApproveExec {
		return permission.AutoApprove{}
	}
	return permission.AutoDeny{}
}

// filterPersonaRules drops *allow* rules originating from a loaded (user- or
// project-authored, therefore untrusted — P7.5) persona file, keeping its deny
// rules. A persona is content, not configuration: a project-level persona.md
// that could grant permissions would be a privilege-escalation channel, while
// one that only removes them is a restriction its author is entitled to make.
// Built-in personas ship with Aegis and stay trusted.
func filterPersonaRules(rules []permission.Rule, p persona.Persona, logger *slog.Logger) []permission.Rule {
	if !p.Loaded {
		return rules
	}
	kept := make([]permission.Rule, 0, len(rules))
	for _, r := range rules {
		if r.Action == permission.RuleDeny {
			kept = append(kept, r)
			continue
		}
		if logger != nil {
			logger.Warn("ignoring persona allow rule from untrusted (loaded) persona", "persona", p.Name)
		}
	}
	return kept
}
