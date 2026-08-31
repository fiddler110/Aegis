package cli

import (
	"context"
	"log/slog"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/sysprompt"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// buildChatGate stacks the full permission gate for a CLI run.
//
// P66.13/QUAL-01: this was a bare `permission.New(ParseMode(mode), approver)` —
// the daemon's five layers reduced to one — so `permission.rules` deny rules and
// `security.egress_then_write` were silently inert under `aegis chat`, and a
// persona's advisory tool list and per-task write scope never applied at all.
// cli/worker.go's own comment already named this exact bypass as the one P10.1
// closed for the in-process path: worker.go was fixed and chat.go was not.
//
// Two behaviors change beyond adding the missing layers, and both are the point
// rather than a side effect. `permission.rules` from config now bind here (they
// are what a deny rule *is*), and `permission.auto_approve_exec` is honored the
// way the daemon and the subprocess worker already honor it — an operator who
// set it meant it everywhere, and --yes remains the per-invocation opt-in for
// everyone else. A rule added by an unsaved "allow always" in a running daemon
// is the one thing a separate process cannot see; that is a property of the
// process boundary, not of this stack.
func buildChatGate(cfg *config.Config, p persona.Persona, reg *tool.Registry, mode string, autoApprove bool, logger *slog.Logger) (engine.Gate, engine.Hooks) {
	approver := enginecfg.Approver(cfg)
	if autoApprove {
		approver = permission.AutoApprove{}
	}
	return enginecfg.BuildGate(enginecfg.GateOptions{
		Mode:           mode,
		StrictPlanMode: !cfg.Permission.PlanModeShellReadsEnabled(),
		Approver:       approver,
		Persona:        p,
		Security:       cfg.Security,
		Registry:       reg,
		Rules:          enginecfg.ConfigRules(cfg, logger),
		Hooks:          enginecfg.EngineHooks(enginecfg.ExecHooks(cfg, logger)),
		Logger:         logger,
	})
}

// chatEngineOptions are the inputs to buildChatEngine, named so the engine build
// is reachable from a test without a live model server.
type chatEngineOptions struct {
	cfg     *config.Config
	adapter provider.Adapter
	reg     *tool.Registry
	gate    engine.Gate
	hooks   engine.Hooks
	// approver is the same approver buildChatGate resolved for the gate above
	// (P81.1) — reused for the scan-hit decision point rather than threading
	// a second one. Nil (an autoApprove-less zero value never reaches here in
	// practice, but a caller that omits it) leaves the check off, same as
	// before this field existed.
	approver engine.Approver
	persona  persona.Persona
	tracker  *cost.Tracker
	logger   *slog.Logger
	cwd      string
	ctxWin   int
	compact  engine.Compactor
}

// buildChatEngine constructs the CLI drive engine.
//
// P66.13/ARCH-06: `aegis chat` ignored max_iterations, loop_threshold,
// redact_secrets, the output guard and hooks — five configured bounds that
// applied on every other path. They are not spelled out field by field here;
// they arrive through enginecfg.CostLimits and enginecfg.OutputGuard, which is
// what stops the sixth one from being forgotten the same way.
func buildChatEngine(o chatEngineOptions) (*engine.Engine, error) {
	cfg := o.cfg
	opts := engine.Options{
		Adapter:  o.adapter,
		Tools:    o.reg,
		Gate:     o.gate,
		Hooks:    o.hooks,
		Approver: o.approver,
		// P67.3: the CLI drive is a person waiting at a terminal, same
		// as a TUI session — the one call class worth retrying harder.
		Purpose:   provider.PurposeForeground,
		Compactor: o.compact,
		// P67.1: the per-call caps in truncate.go bound one result; this
		// bounds what a parallel round contributes in aggregate.
		RoundResultCap:      roundCapFor(o.cwd),
		Cost:                o.tracker,
		Model:               cfg.Provider.Model,
		ContextWindowTokens: o.ctxWin,
		// P59.7: the phased drive is the one caller that actually
		// escalates the serving window mid-run, so it is the caller
		// whose engine most needs to see the escalation.
		ContextWindowFloor: func() int { return provider.RaisedContextWindow(o.adapter) },
		Logger:             o.logger,
		// The CLI drive runs in cwd, so its registry is already rooted
		// there; additional roots are the only thing it can't derive
		// on its own (P52.13). This is the surface the cross-repo
		// research->document workflow actually runs on today.
		ExtraRoots: driveExtraRoots(o.cwd, cfg, o.logger),
	}
	// P59.4: the generation budget rides the same path as the wall-clock one.
	// MaxTokensPerRun is deliberately still not set here — this is the phased
	// drive, whose whole design is a fresh context per phase, and a
	// context-denominated cap is the wrong instrument for it. Saying that as
	// one named call is the difference between a decision and an omission.
	enginecfg.CostLimits(cfg).WithoutContextTokenCap().Apply(&opts)
	// The sampling knobs, the tool-call shim and the P66.14/LLM-03 calibration
	// admission travel together for the same reason the bounds above do.
	enginecfg.ModelBackend(cfg).Apply(&opts)
	if cfg.OutputGuard.Enabled {
		// Unlike the daemon there is no per-turn model resolution here, so the
		// verdict runs on the adapter as configured rather than one re-wrapped
		// for the guard model's own context window (P52.4). That costs an
		// unsized num_ctx on a local backend when output_guard.model names a
		// second model; it does not change the verdict.
		gm := enginecfg.GuardModel(cfg, cfg.Provider.Model)
		enginecfg.OutputGuard(cfg, o.persona, o.adapter, gm, o.logger).Apply(&opts)
	}
	// Gate: the caller passes one built by buildChatGate (enginecfg.BuildGate);
	// it is set in the literal above rather than here.
	return engine.New(opts)
}

// roundCapFor builds the P67.1 aggregate round bound for an engine rooted at
// root. The CLI paths set no engine Workdir — their tools are rooted at the
// process cwd by construction (builtin.Options.Root) — so the root is bound here
// and the spill lands in the workspace the run is actually reading.
func roundCapFor(root string) engine.RoundCapFunc {
	return func(ctx context.Context, results []string) []string {
		return builtin.CapRound(ctx, root, results)
	}
}

// buildChatSystem assembles the one-shot chat system prompt: persona base +
// shared blocks + memory/context + the <skills_available> index + the cached
// repo map + <deferred_tools> + the debate block. Extracted from the command
// closure so the assembly is unit-testable. explicit --system wins, then the
// resolved persona's own system text.
//
// It is a re-derivation of the daemon's effectiveSystem
// (internal/server/helpers.go) rather than a call into it: the daemon's
// promptSections carries the P67.2 stable/volatile split, which is a property of
// a long-lived session and means nothing to a process that assembles the prompt
// once and exits. What both sides must agree on — the block renderers and the
// local-profile byte caps — is shared through internal/sysprompt, so the four
// divergences P66.13/QUAL-02 found are closed:
//
//   - <deferred_tools> was missing entirely, so the 26 deferred tools the whole
//     P62.6 line is about were undiscoverable via `tool_search` on this path — a
//     pure capability loss with the token saving already banked. It is emitted
//     last, after the registry is final, because the block is the complement of
//     what is exposed.
//   - The debate-integration block was missing, so `security.debate.*` was inert
//     here.
//   - Neither local-profile cap was applied — on the path that *is* the
//     local-model path. Context files are now read through
//     sysprompt.ContextFilesBudget and an over-cap repo map is dropped, matching
//     the daemon's asymmetric posture (truncate instructions, drop a generated
//     map).
//
// One divergence remains and is a fact about the CLI rather than an oversight:
// the repo map comes only from the on-disk cache written by `aegis index`, and
// only while fresh, where the daemon rebuilds it per workspace. A one-shot
// process paying a 185 ms full rebuild before every prompt is the wrong trade;
// `aegis index` is the documented way to refresh it. There is likewise no
// session-scoped skill activation to layer on, because there is no session.
func buildChatSystem(cfg *config.Config, cwd string, enabledBuiltins []string, system string, p persona.Persona, reg *tool.Registry) string {
	resolvedSystem := system
	if resolvedSystem == "" {
		resolvedSystem = p.System
	}
	local := cfg.Provider.LocalPromptProfile()
	src := memory.Sources{ProjectRoot: cwd, DataDir: cfg.DataDir}

	parts := []string{
		resolvedSystem,
		persona.ToolUseBlockFor(local),
		persona.CompletingTasksBlockFor(local),
		persona.PlatformBlockFor(local),
		src.LoadContextCapped(sysprompt.ContextFilesBudget(local)),
		src.Load(),
		// Advertise available skills so the model can discover and load them on
		// demand. builtin.Register wires the `skill` tool into the registry, but
		// without this <skills_available> index the model is never told the skills
		// exist and never calls it.
		skills.BuildIndex(cwd, cfg.DataDir, enabledBuiltins),
		// Inject the cached repository map when present (built via `aegis index`).
		// Read with the configured budget (P62.1), not repomap's built-in default:
		// the cached map holds every symbol extracted, so the render size is decided
		// here — a CLI reading with defaults would hand the model a smaller map than
		// the daemon does for the same repo and the same config.
		chatRepoMapBlock(cfg, cwd, local),
		sysprompt.DeferredToolsBlock(reg),
		sysprompt.DebateIntegrationBlock(cfg.Security.Debate),
	}

	var b strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(part)
	}
	return b.String()
}

// chatRepoMapBlock renders the cached <repo_map> for the CLI prompt, or "" when
// there is no fresh cache or the block is over the local profile's cap.
func chatRepoMapBlock(cfg *config.Config, cwd string, local bool) string {
	rmOpts := repomap.Options{MaxBytes: cfg.RepoMap.MaxBytes, MaxSymbolsPerFile: cfg.RepoMap.MaxSymbolsPerFile}
	rm, fresh, _ := repomap.Load(cwd, repoMapCachePath(cwd), rmOpts)
	if rm == "" || !fresh {
		return ""
	}
	block := repomap.Block(rm)
	if !sysprompt.RepoMapFits(block, local) {
		return ""
	}
	return block
}
