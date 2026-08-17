package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/drive"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/logging"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/sysprompt"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
)

// chatOptions are the `aegis chat` flags, resolved once and passed down the
// call chain rather than captured by a closure.
//
// newChatCmd was a 683-line function wrapping a 615-line RunE closure that held
// two of P66.13's bugs (QUAL-03): nothing inside it could be reached by a test
// without running the whole command against a live model. Splitting it is the
// *enabling* refactor for this item rather than a finding of its own — the
// permission gate and the system prompt are now two named functions a test can
// call directly, which is what makes "chat stacks the same gate as the daemon"
// an assertion instead of a code reading.
type chatOptions struct {
	system       string
	mode         string
	personaName  string
	autoApprove  bool
	outputFormat string
	skillName    string
	maxTurns     int
	renderFlag   string
	skipFitCheck bool
}

func newChatCmd() *cobra.Command {
	var o chatOptions

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Run a one-shot chat turn through the agent engine (no TUI)",
		Long: "Sends a single prompt to the model and streams the response. Reads the prompt from arguments, or from stdin if none are given.\n\n" +
			"With --skill <name>, the named skill's full instructions are preloaded into the prompt (so a small local model never has to discover and fetch them via the `skill` tool) and the run is driven to completion: after each turn, if any file under .aegis/ still contains a `<!-- PENDING -->` marker, chat auto-continues rather than stopping when the model yields. This is what lets a long, multi-phase skill (threat model, deep research) finish non-interactively; a plain one-shot turn would stop at the first yield with a partial suite (P38.2). --max-turns bounds the drive.",
		RunE: func(cmd *cobra.Command, args []string) error { return runChat(cmd, args, &o) },
	}

	cmd.Flags().StringVar(&o.system, "system", "", "system prompt (overrides --persona)")
	cmd.Flags().StringVar(&o.mode, "mode", "", "permission mode: plan (read-only) or build (default from config)")
	cmd.Flags().StringVar(&o.personaName, "persona", "", "persona to use (e.g. security, developer, sre)")
	cmd.Flags().BoolVar(&o.autoApprove, "yes", false, "auto-approve tool calls that would otherwise require confirmation")
	cmd.Flags().StringVar(&o.outputFormat, "output-format", "text", "output format: text, json (final result object), or stream-json (one event per line)")
	cmd.Flags().StringVar(&o.skillName, "skill", "", "preload the named skill's full instructions into the prompt and drive the run to completion (auto-continue while `<!-- PENDING -->` markers remain under .aegis/) — for multi-phase skills like threat-modeling that a one-shot turn would leave half-finished")
	cmd.Flags().IntVar(&o.maxTurns, "max-turns", 40, "with --skill, the maximum number of drive-to-completion turns before stopping with a resumable partial result")
	cmd.Flags().BoolVar(&o.skipFitCheck, "skip-model-check", false, "with --skill, skip the pre-flight structured-fill probe that refuses to start a phased drive on a model which cannot reliably fill scaffolded documents")
	cmd.Flags().StringVar(&o.renderFlag, "render", "auto", "markdown rendering of the text output format: auto (on when stdout is a terminal), on, or off (raw stream)")
	return cmd
}

func runChat(cmd *cobra.Command, args []string, o *chatOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	stopOllama, err := ensureOllamaRunning(cfg)
	if err != nil {
		return err
	}
	defer stopOllama()
	if err := resolveOllamaModel(cfg); err != nil {
		return err
	}

	prompt, err := readChatPrompt(cmd, args)
	if err != nil {
		return err
	}

	// Handle slash commands in CLI mode.
	if parsed := commands.Parse(prompt); parsed != nil {
		return handleCLISlash(cmd, cfg, parsed)
	}

	// P38.6: a --skill run drives a tool-executed, multi-phase task to
	// completion. A reasoning model with think enabled was observed to
	// *simulate* the whole build inside its thinking trace and then report
	// it done while writing nothing (qwen3:14b: recon→scaffold→fill→verify
	// all narrated in `thinking`, zero real tool calls, no files on disk —
	// a silent false success on the shipped default config). Force think
	// off for the drive so the phases execute as real tool calls, warning
	// when we override an explicitly-enabled setting.
	if o.skillName != "" && cfg.Provider.Think != nil && *cfg.Provider.Think {
		off := false
		cfg.Provider.Think = &off
		fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: --skill drives the run to completion via tool calls; disabling provider.think for this run so the model executes the phases instead of simulating them in its reasoning trace (P38.6)]\n")
	}

	// P47.5(a): a phased --skill drive (threat-modeling) builds a large
	// per-phase context; the generic configured window is often too
	// small for it — the 2026-07-24 FirewallRuleAnalyzer run only
	// converged after a manual AEGIS_PROVIDER_CONTEXT_WINDOW=196608
	// bump. When the drive is phased and the provider is Ollama-backed,
	// size the serving window up front to RecommendContextWindow(model
	// max) if that beats the configured value, so both the num_ctx sent
	// to Ollama (WithNumCtx below) and the compaction budget get the room
	// the phased build needs without the manual step. driveModelMax is
	// kept as the P47.5(b) escalation ceiling for the on-overflow retry.
	// Done before Build so the sized window flows into the adapter.
	phasedDrive := o.skillName != "" && drive.PlanFor(o.skillName, skillPhaseSpecs(cfg, o.skillName)) != nil && !drive.LinearForced()
	driveModelMax := 0
	if phasedDrive {
		if win, modelMax, ok := recommendPhasedDriveWindow(context.Background(), cfg); ok {
			driveModelMax = modelMax
			if win > cfg.Provider.ContextWindow {
				fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: sizing the serving context window to %d tokens for the phased %s drive (model max %d) — overrides the configured %d so the build has room without a manual AEGIS_PROVIDER_CONTEXT_WINDOW bump (P47.5)]\n", win, o.skillName, modelMax, cfg.Provider.ContextWindow)
				cfg.Provider.ContextWindow = win
			}
		}
	}

	adapter, err := providerfactory.Build(cfg, nil, providerfactory.WithModelCaps(cfg.OpenModelCaps()))
	if err != nil {
		return err
	}

	logger, logClose, err := openChatLog(cfg)
	if err != nil {
		return err
	}
	defer logClose()

	// P35.8: capture an otherwise-silent panic to aegis.log before the
	// process dies. A live `aegis chat` run once vanished mid-turn
	// leaving nothing on disk — no panic, no signal record, no final
	// answer. Registered AFTER logClose's defer so, by LIFO ordering,
	// this log write runs before the log file is closed; we re-panic so
	// the crash stays visible and the exit code stays non-zero.
	defer func() {
		if r := recover(); r != nil {
			logger.Error("chat: panic", "value", r, "stack", string(debug.Stack()))
			panic(r)
		}
	}()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	enabledBuiltins, err := prepareChatSkills(cmd, cfg, cwd, o.skillName, logger)
	if err != nil {
		return err
	}

	reg := tool.NewRegistry()
	// The last of P62.10's four call sites, and the one that needed live
	// evidence rather than a token count: `aegis chat --skill` is the
	// harness every P38.1 re-test drives, and the phased drive layers
	// its per-phase tool arrays on top of whatever is registered here,
	// so deferring edit_file changes what those arrays resolve to
	// mid-drive. Registering the full profile against a local model was
	// costing 1,318 schema tokens over what the daemon sends for the
	// same model — security_scan alone 818, more than four times the
	// edit_file schema P62.9 spent its item arguing about.
	//
	// Measured on qwen3:14b before turning it on (2026-08-14, the
	// seeded-bug task run end to end through this command, 16,384-token
	// window, three runs per arm). The refutation P62.9 said to watch
	// for — a turn spent on tool_search hunting for a tool the model
	// knows by name — did not occur in any deferred-surface run. The
	// task was solved in every one of them. What the deferred surface
	// did cost is tool calls: 4-6 against a steady 3, because the model
	// re-reads the file before a multi_edit where it would edit_file
	// straight from the traceback. Run-to-run variance on this task is
	// large enough to swamp that (a control arm failed outright twice by
	// explaining the fix instead of applying it), so it is recorded as a
	// caveat rather than a finding — see P62.9 in research/roadmap.md,
	// which owns the edit_file half of this decision.
	regOpts := enginecfg.BuiltinOptions(cfg, cwd)
	regOpts.BuiltinSkills = enabledBuiltins
	// LocalProfile and the rest of the config-derived option set are decided by
	// enginecfg.BuiltinOptions above (P66.13/QUAL-06), which is also what finally
	// gives this path a Commands resolver — without it the whole toolpath/ripgrep
	// contract was inert here.
	if err := builtin.Register(reg, regOpts); err != nil {
		return err
	}

	// The persona is resolved once and used twice — for the gate's advisory
	// tool list and deny rules, and for the base system prompt. It used to be
	// looked up inside buildChatSystem only, so `--persona security`'s rules
	// reached the prompt and never the permission stack.
	p, _ := persona.Get(o.personaName)
	resolvedMode := cfg.Permission.Mode
	if o.mode != "" {
		resolvedMode = o.mode
	}
	gate, engineHooks := buildChatGate(cfg, p, reg, resolvedMode, o.autoApprove, logger)

	tracker := cost.NewTracker()

	// P47.1: wire proactive per-turn compaction into the CLI drive
	// engine, mirroring the daemon (internal/server/engine_build.go).
	// The engine's 85%-fill compaction is gated on
	// ContextWindowTokens > 0 AND a non-nil Compactor; the CLI path set
	// neither, so a multi-turn --skill drive grew context every turn
	// with no defense — until Ollama hard-rejected the request (173,816
	// vs a 131,072 window on the 2026-07-24 FirewallRuleAnalyzer run)
	// and the drive aborted with a terminal context-truncation error.
	// Resolve the effective window the same way the server does (config
	// or Ollama-detected) and build a Summarizer over it. Factored into
	// driveCompaction so a regression test can assert the CLI path keeps
	// compaction enabled and can't silently diverge from the daemon.
	compactor, ctxWin := driveCompaction(context.Background(), cfg, adapter, logger)

	eng, err := buildChatEngine(chatEngineOptions{
		cfg:     cfg,
		adapter: adapter,
		reg:     reg,
		gate:    gate,
		hooks:   engineHooks,
		persona: p,
		tracker: tracker,
		logger:  logger,
		cwd:     cwd,
		ctxWin:  ctxWin,
		compact: compactor,
	})
	if err != nil {
		return err
	}

	// P35.8: derive the run context from an explicit signal handler that
	// LOGS which signal fired before cancelling. The bare
	// signal.NotifyContext(os.Interrupt) it replaces recorded nothing —
	// when a live chat run vanished mid-turn there was no way to tell a
	// signal from a silent death. Also covers SIGTERM (portable in Go's
	// os/signal), which NotifyContext(os.Interrupt) ignored.
	ctx, stop := installSignalCancel(context.Background(), logger)
	defer stop()

	// P38.2: preload the named skill's full body into the first user
	// message. Small local models were observed to skip the `skill`-tool
	// round-trip that progressive disclosure relies on (P36.1); prepending
	// the instructions verbatim removes that dependency for a scripted run.
	driveToCompletion := false
	skillDir := ""                     // P39.6: where the preloaded skill's bundled verify scripts live
	var skillPhases []skills.PhaseSpec // P52.12: the skill's own declared phase plan, if it has one
	skillRunDir := ""                  // P52.12: the skill's declared `run_dir:`, if any — where its phase globs live
	taskPrompt := prompt               // P39.5: the raw task, kept so the first message can be rewritten without the SKILL.md body after the opening turn
	if o.skillName != "" {
		if sk, ok := skills.Load(cwd, cfg.DataDir, enabledBuiltins, o.skillName); ok && strings.TrimSpace(sk.Content) != "" {
			prompt = skillPreamble(o.skillName, sk.Content) + prompt
			driveToCompletion = true
			skillDir = sk.Dir
			skillPhases = sk.Phases
			skillRunDir = sk.RunDir
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "\n[warning: --skill %q not found or empty; running without preloaded skill body]\n", o.skillName)
		}
	}

	// P39.18: expose the skill's bundled scripts as typed tools, and only
	// while such a skill is actually loaded. They are not part of
	// builtin.Register's always-on surface precisely so they cost nothing
	// on the ~50 tools every other run pays schema tokens for; within the
	// phased drive the per-phase lists narrow them further still.
	for _, t := range builtin.ThreatModelScriptTools(cwd, skillDir) {
		reg.Upsert(t)
	}

	// P39.9: a --skill drive on the legacy OpenAI-compat (/v1) Ollama
	// adapter can't send num_ctx, so context_window is ignored and Ollama
	// serves the model's modelfile default; a skill-driven prompt then
	// overflows and is silently truncated from the front. Warn up front
	// with the exact fix rather than letting the drive quietly degrade.
	if driveToCompletion {
		warnCompatDriveWindow(cmd.ErrOrStderr(), cfg)
	}

	// The system prompt is assembled after the registry is final — the
	// <deferred_tools> block is the complement of what is exposed, so building
	// it before ThreatModelScriptTools/Register would advertise a surface that
	// no longer matches.
	conv := &engine.Conversation{System: buildChatSystem(cfg, cwd, enabledBuiltins, o.system, p, reg)}
	conv.Append(provider.Message{
		Role:    provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: prompt}},
	})

	format, err := parseOutputFormat(o.outputFormat)
	if err != nil {
		return err
	}

	rmode, err := parseRenderMode(o.renderFlag)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	// P56.1: the default text format streamed raw model output and raw
	// tool-argument JSON, so a markdown table or a heading reached the
	// terminal as one undifferentiated chunk. The renderer buffers
	// prose to block boundaries and styles it through glamour when
	// stdout is a terminal; piped or under --render off it stays
	// byte-identical to the raw stream, which is what scripts consume.
	rend := newChatRenderer(out, rmode)
	// An error or a cancelled run never reaches KindDone, and buffered
	// prose that is never flushed is prose the user never sees — a
	// strictly worse failure than the unrendered stream this replaces.
	defer rend.Flush()

	d := &chatDrive{
		eng: eng, conv: conv, logger: logger, errOut: cmd.ErrOrStderr(),
		cwd: cwd, skillName: o.skillName, skillDir: skillDir, skillRunDir: skillRunDir,
		taskPrompt: taskPrompt, maxTurns: o.maxTurns, driveToCompletion: driveToCompletion,
		pendingRoot: filepath.Join(cwd, ".aegis"),
	}
	var answer strings.Builder
	d.onEvent = func(ev engine.Event) {
		// Counted for every format: both json and stream-json report
		// this in their trailer, so counting it per-branch left
		// stream-json's summary permanently reading zero.
		if ev.Kind == engine.KindToolCall {
			d.toolCalls++
			d.iterToolCalls++
			// P39.7: a suite file was actually mutated this turn (as
			// opposed to a read/recon-only or narration-only turn). The
			// no-progress guard below treats the absence of this as an
			// "announce then yield" stall and nudges the model to act.
			if mutatingTools[ev.ToolName] {
				d.iterMutations++
			}
		}
		switch format {
		case outputStreamJSON:
			emitStreamEvent(out, ev)
		case outputJSON:
			if ev.Kind == engine.KindText {
				answer.WriteString(ev.Text)
			}
		default: // text
			switch ev.Kind {
			case engine.KindText:
				rend.Text(ev.Text)
			case engine.KindNotice:
				// Advisories (empty answer, cold load, a model that
				// can't call tools) were dropped entirely here, so the
				// default output format was the one surface that never
				// showed them.
				rend.Notice(ev.Text)
			case engine.KindToolCall:
				rend.ToolCall(ev.ToolName, ev.ToolInput)
			case engine.KindToolResult:
				rend.ToolResult(ev.ToolResult, ev.ToolIsError)
			case engine.KindDone:
				rend.Done()
			}
		}
	}

	// P35.8: bracket the engine run with boundary markers. A silent
	// mid-run disappearance then shows up as a "run starting" line with
	// no matching "run finished" — exactly the diagnostic signal that
	// was missing when a live chat turn vanished without a trace.
	logger.Info("chat: run starting", "prompt_bytes", len(prompt), "drive", driveToCompletion)

	// P38.8 in-harness: a skill with a phase plan (threat-modeling) is
	// driven phase-by-phase in a fresh context each phase, which is what
	// keeps peak context bounded on a local model — the single-context
	// generic drive below is what stalled the P38.1 build. Any other
	// PENDING-driven skill, or AEGIS_SKILL_DRIVE=linear, uses the generic
	// drive. Both branches set runErr and leave the tail logic below
	// (the P38.6 floor check, cost trailer) unchanged.
	var runErr error
	if phases := drive.PlanFor(o.skillName, skillPhases); driveToCompletion && phases != nil && !drive.LinearForced() {
		if err := preflightFillCheck(cmd.Context(), cfg, adapter, cmd.ErrOrStderr(), o.skipFitCheck); err != nil {
			return err
		}
		runErr = d.runPhased(ctx, cfg, adapter, reg, phases, driveModelMax)
	} else {
		runErr = d.runLinear(ctx)
	}

	// P38.6 floor check against fabricated completion. A drive that ends
	// with no PENDING markers is normally "finished" — but a model can
	// report the whole build as done without executing it, leaving .aegis/
	// empty. "No markers" then means "never started", not "complete", and
	// the empty result reads as success — the worst shape (a user believing
	// they have a threat model and having nothing). Distinguish the two: if
	// a completed drive produced no suite files at all, say so. Lever (a)
	// above prevents the observed think-mode trigger; this hardens the
	// oracle against any other fabrication path.
	if driveToCompletion && runErr == nil && ctx.Err() == nil && suiteFileCount(d.pendingRoot) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: the run reported completion but wrote no files under .aegis/ — the model may have described the work instead of executing it. Nothing was produced; re-run (with provider.think disabled if it is on) and check the transcript.]\n")
	}

	emitChatSummary(out, cmd.ErrOrStderr(), format, tracker.Snapshot(), answer.String(), d.toolCalls, runErr)
	return runErr
}

// readChatPrompt reads the prompt from the command's arguments, falling back to
// stdin. The 1 MiB limit bounds a piped file that is not what the caller meant
// to send.
func readChatPrompt(cmd *cobra.Command, args []string) (string, error) {
	const maxPromptSize = 1 << 20 // 1 MiB
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		data, _ := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxPromptSize))
		prompt = strings.TrimSpace(string(data))
	}
	if prompt == "" {
		return "", fmt.Errorf("no prompt provided (pass as arguments or via stdin)")
	}
	return prompt, nil
}

// openChatLog wires cfg.LogLevel into a real logger (P35.7 follow-up): without
// this, engine.New falls back to slog.Default() (info level, unstructured
// stderr), so the debug-level prompt_eval instrumentation added in P35.7 is
// invisible from `aegis chat`, the natural non-interactive/scriptable path.
// Mirrors the logging.New usage in serve/acp/mcp-serve.
//
// At debug level it also mirrors logs to stderr so the diagnostics an operator
// explicitly opted into (prompt_eval prefill counts, tool timings) are visible
// immediately without tailing aegis.log. The answer streams to stdout, so stderr
// mirroring never corrupts it. Kept off below debug so a normal `aegis chat` run
// stays quiet on stderr (info-level WARNs about sandbox/MCP would otherwise leak
// into scripted output).
func openChatLog(cfg *config.Config) (*slog.Logger, func(), error) {
	if err := cfg.EnsureDataDir(); err != nil {
		return nil, nil, fmt.Errorf("ensure data dir: %w", err)
	}
	debugToStderr := strings.EqualFold(strings.TrimSpace(cfg.LogLevel), "debug")
	logger, logCloser, err := logging.New(logging.Options{
		Level:    cfg.LogLevel,
		Path:     cfg.LogPath(),
		ToStderr: debugToStderr,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("init logger: %w", err)
	}
	return logger, func() { logCloser.Close() }, nil
}

// prepareChatSkills self-heals stale built-in skills, materializes the enabled
// ones (plus any named by --skill) both to the data dir and into the workspace,
// and installs the bundled-skill scanner. It returns the enabled built-in set
// the registry and the <skills_available> index are built from.
//
// Self-heal runs on every run. Both the per-user <dataDir>/builtin-skills copy
// and a project's <cwd>/.aegis/builtin-skills copy are extracted snapshots of
// the embedded skills; after a binary upgrade those on-disk copies can lag what
// this binary ships (e.g. a fixed verify.py), and nothing refreshed a skill
// unless that exact skill was invoked — so a project that materialized
// threat-modeling under an older binary kept running the stale bundled scripts.
// Reconcile whatever is already materialized against the embedded content
// regardless of which skill (if any) this run uses: overwrite-on-diff, only
// files that drifted, only shipped builtins (a user's own bundled skill is
// untouched), never creating skills that aren't already present.
func prepareChatSkills(cmd *cobra.Command, cfg *config.Config, cwd, skillName string, logger *slog.Logger) ([]string, error) {
	refreshBuiltins := func(where string, changed []string, rerr error) {
		if rerr != nil {
			logger.Warn("chat: refreshing stale built-in skills failed", "where", where, "err", rerr)
			return
		}
		if len(changed) > 0 {
			logger.Info("chat: refreshed stale built-in skill files to match this binary", "where", where, "count", len(changed), "files", changed)
			fmt.Fprintf(cmd.ErrOrStderr(), "[notice: refreshed %d stale built-in skill file(s) in the %s to match this binary's embedded copies]\n", len(changed), where)
		}
	}
	dc, de := skills.RefreshDataDirBuiltins(cfg.DataDir)
	refreshBuiltins("data dir", dc, de)
	pc, pe := skills.RefreshProjectBuiltins(cwd)
	refreshBuiltins("project", pc, pe)

	// P38.2: --skill preloads a specific skill for this run. Enable it on
	// top of config's builtin list (so the `skill` tool and the
	// <skills_available> index both see it) and materialize the embedded
	// built-ins to <dataDir>/builtin-skills — the daemon does this at
	// startup, but `aegis chat` runs in-process and never did, so without
	// it a freshly-installed binary's builtin skill body and its bundled
	// scripts (recon.py, verify.py, …) wouldn't be on disk to read.
	enabledBuiltins := cfg.Skills.BuiltinEnabled
	if skillName != "" && skills.IsBuiltin(skillName) {
		enabledBuiltins = appendUnique(enabledBuiltins, skillName)
	}
	if skillName != "" || len(enabledBuiltins) > 0 {
		if err := skills.MaterializeBuiltins(cfg.DataDir); err != nil {
			return nil, fmt.Errorf("materialize built-in skills: %w", err)
		}
		// P39.10: the <dataDir>/builtin-skills copy above is outside the
		// workspace root, so the sandboxed file tools (confined to cwd)
		// reject reading a builtin skill's bundled scripts/skeletons —
		// the model can't reach recon.py/scaffold.py and bails before the
		// build even starts. Mirror the daemon (server.sessions) and also
		// materialize the enabled builtins into <cwd>/.aegis/builtin-skills
		// so the <skill_assets> manifest resolves to a workspace-relative
		// path the file tools accept. skills.Load then prefers this project
		// copy, so skillDir/verify-script paths point inside the workspace.
		if err := skills.MaterializeBuiltinsToProject(cwd, enabledBuiltins); err != nil {
			return nil, fmt.Errorf("materialize built-in skills into workspace: %w", err)
		}
	}

	// Screen untrusted bundled skill directories through the same scan
	// `aegis security scan` drives (P44.1); silent no-op when no scanner
	// is available.
	chatScanOpts := security.OptionsFromConfig(cfg.Security)
	skills.SetBundleScanner(func(ctx context.Context, dir string) []string {
		return security.ScanBundleWarnings(ctx, dir, chatScanOpts)
	})
	return enabledBuiltins, nil
}

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
		Mode:     mode,
		Approver: approver,
		Persona:  p,
		Security: cfg.Security,
		Registry: reg,
		Rules:    enginecfg.ConfigRules(cfg, logger),
		Hooks:    enginecfg.EngineHooks(enginecfg.ExecHooks(cfg, logger)),
		Logger:   logger,
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
	persona persona.Persona
	tracker *cost.Tracker
	logger  *slog.Logger
	cwd     string
	ctxWin  int
	compact engine.Compactor
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
		Adapter: o.adapter,
		Tools:   o.reg,
		Gate:    o.gate,
		Hooks:   o.hooks,
		// P67.3: the CLI drive is a person waiting at a terminal, same
		// as a TUI session — the one call class worth retrying harder.
		Purpose:   provider.PurposeForeground,
		Compactor: o.compact,
		// P67.1: the per-call caps in truncate.go bound one result; this
		// bounds what a parallel round contributes in aggregate.
		RoundResultCap: roundCapFor(o.cwd),
		// P66.14/LLM-03: admits this backend's reported prompt counts as
		// calibration samples. The compat adapter on an :11434 base_url
		// is the documented Ollama configuration and had never been
		// admitted.
		SharedContextWindow: providerfactory.CertainlyOllama(cfg.Provider),
		Cost:                o.tracker,
		Temperature:         cfg.Provider.Temperature,
		Seed:                cfg.Provider.Seed,
		Model:               cfg.Provider.Model,
		MaxTokens:           cfg.Provider.MaxTokens,
		ContextWindowTokens: o.ctxWin,
		// P59.7: the phased drive is the one caller that actually
		// escalates the serving window mid-run, so it is the caller
		// whose engine most needs to see the escalation.
		ContextWindowFloor: func() int { return provider.RaisedContextWindow(o.adapter) },
		ToolCallShim:       cfg.Provider.ToolCallShimEnabled(),
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

// emitChatSummary writes the run trailer in whichever output format was asked
// for.
func emitChatSummary(out, errOut io.Writer, format outputFormatKind, snap cost.Snapshot, answer string, toolCalls int, runErr error) {
	switch format {
	case outputJSON:
		emitFinalJSON(out, chatResult{
			Answer:       strings.TrimSpace(answer),
			CostUSD:      snap.TotalUSD,
			Turns:        snap.Turns,
			InputTokens:  snap.Usage.InputTokens,
			OutputTokens: snap.Usage.OutputTokens,
			ToolCalls:    toolCalls,
			Error:        errString(runErr),
		})
	case outputStreamJSON:
		// Final summary line so consumers can read cost without tracking usage.
		emitFinalJSON(out, chatResult{
			Type: "result", CostUSD: snap.TotalUSD, Turns: snap.Turns,
			InputTokens: snap.Usage.InputTokens, OutputTokens: snap.Usage.OutputTokens,
			ToolCalls: toolCalls, Error: errString(runErr),
		})
	default:
		// P35.13: the "in" figure is the summed per-turn input-token
		// count — total input *processed*, which is the billable-input
		// basis a cloud provider charges (each turn re-sends the whole
		// conversation; cache reads are priced separately in cost). This
		// trailer is gated on TotalUSD > 0, so it only prints for a
		// priced/cloud run, where that is exactly the number to show;
		// local runs (cost $0, and where the count is Ollama's full
		// per-turn context size, not prefill work — P35.13) never reach
		// this branch.
		if snap.TotalUSD > 0 {
			fmt.Fprintf(errOut, "\n[cost: $%.4f over %d turn(s), %d in / %d out tokens]\n",
				snap.TotalUSD, snap.Turns, snap.Usage.InputTokens, snap.Usage.OutputTokens)
		}
	}
}

// chatDrive is the state the two drive-to-completion loops share. It exists so
// each loop is a method with a name rather than a branch two hundred lines deep
// inside a command closure.
type chatDrive struct {
	eng     *engine.Engine
	conv    *engine.Conversation
	onEvent func(engine.Event)
	logger  *slog.Logger
	errOut  io.Writer

	cwd         string
	pendingRoot string
	skillName   string
	skillDir    string
	skillRunDir string
	taskPrompt  string
	maxTurns    int

	driveToCompletion bool

	toolCalls     int
	iterToolCalls int
	iterMutations int
}

// runPhased drives a skill with a declared phase plan phase-by-phase in a fresh
// context each phase (P38.8 in-harness), which is what keeps peak context
// bounded on a local model — the single-context generic drive is what stalled
// the P38.1 build.
func (d *chatDrive) runPhased(ctx context.Context, cfg *config.Config, adapter provider.Adapter, reg *tool.Registry, phases []drive.Phase, driveModelMax int) error {
	fmt.Fprintf(d.errOut, "\n[notice: driving %s in phased mode — one bounded fresh context per phase (%d content phases + verify), the in-harness form of P38.8]\n", d.skillName, len(phases))
	d.logger.Info("chat: using phased skill drive (P38.8 in-harness)", "skill", d.skillName, "phases", len(phases))

	// P47.5(b): let a phase escalate the serving window toward the
	// model max on a context overflow. Each call doubles num_ctx
	// (bounded by the model max) by mutating the live Ollama adapter,
	// which the next Stream picks up — no engine rebuild. Nil/no-op
	// when the provider can't escalate (non-Ollama, or already at the
	// ceiling); the drive then falls back to the P47.2/P47.7
	// fresh-context reset alone.
	//
	// P59.7 connects the escalation to the engine's compaction
	// *trigger* (via ContextWindowFloor), so a phase that just gained room
	// stops compacting as if it hadn't. The summarizer's own *budget* still
	// stays at the sized window, which is a deliberate choice rather than the
	// same oversight: a larger num_ctx only buys physical headroom against a
	// transient overshoot, and sizing the summary request to it would spend the
	// new room on the recovery rather than on the work.
	var escalateWindow func() (int, bool)
	if driveModelMax > 0 {
		curWin := cfg.Provider.ContextWindow
		escalateWindow = func() (int, bool) {
			next, grew := drive.NextWindow(curWin, driveModelMax)
			if !grew {
				return curWin, false
			}
			if provider.RaiseContextWindow(adapter, next) {
				curWin = next
				return next, true
			}
			return curWin, false
		}
	}
	// P50.1: a liveness probe so the drive can wait out a
	// crashed/restarting local model server and resume from disk
	// instead of aborting. Unwraps the retry/failover decorators to
	// the base adapter; returns supported==false for a backend with
	// no probe (a cloud adapter), and the drive then never waits on it.
	checkBackend := func(hctx context.Context) (bool, bool) {
		return provider.CheckBackendHealth(hctx, adapter)
	}
	return drive.Run(ctx, &drive.State{
		Engine: d.eng, System: d.conv.System, OnEvent: d.onEvent, Logger: d.logger,
		ErrOut: d.errOut, Cwd: d.cwd, SkillName: d.skillName, SkillDir: d.skillDir,
		TaskPrompt: d.taskPrompt, MaxTurns: d.maxTurns, EscalateWindow: escalateWindow,
		RunDir:       drive.RunDirResolver(d.skillName, d.skillRunDir),
		CheckBackend: checkBackend, Progress: &drive.Progress{},
		IterToolCalls: &d.iterToolCalls, IterMutations: &d.iterMutations,
		// Safe here specifically because the CLI drive owns this
		// registry outright — narrowing is registry-wide, so a host
		// sharing one registry across concurrent sessions must not
		// wire this.
		ScopeTools: reg.ScopeExposed,
	}, phases)
}

// runLinear is the generic single-context drive (P38.2). A multi-phase skill
// (threat model, deep research) is many turns in one context; a plain one-shot
// chat stops at the first yield, so a model that pauses to ask "shall I
// proceed?" mid-build (the motivating failure) leaves a partial suite behind its
// unresolved `<!-- PENDING -->` stubs. When --skill preloaded such a skill, keep
// running while any file under .aegis/ still carries a PENDING marker: append a
// continuation turn and run again — reusing the SAME conversation so context
// threads (and pruning/compaction apply) across the whole drive. Bounded by
// --max-turns and the P39.7 no-progress guard: a turn that mutates no suite file
// and leaves the PENDING set unchanged is an "announce then yield" stall, so
// re-prompt with an explicit "act now" nudge (bounded) rather than burning
// tokens on narration.
//
// The oracle is the stub-first pattern the skill uses (SKILL.md §4.1): a PENDING
// marker is unambiguous unfinished work. If a model instead writes full file
// content without ever stubbing (observed on qwen3:14b, which skips the setup
// step), no markers appear and the drive simply ends when the model yields —
// correct when it finished, and a known limitation when it didn't, but never a
// wrong forced continuation.
//
// Without --skill this is a plain one-shot turn: the loop breaks after the first
// iteration.
func (d *chatDrive) runLinear(ctx context.Context) error {
	var runErr error
	noProgress := 0
	verifyRounds := 0
	qualityReviewed := false // P38.1: run the final quality pass at most once
	preambleCompacted := false
	var prevPending []string
	for iter := 0; ; iter++ {
		d.iterToolCalls = 0
		d.iterMutations = 0
		runErr = d.eng.Run(ctx, d.conv, d.onEvent)
		d.logger.Info("chat: run finished", "iter", iter, "err", errString(runErr), "tool_calls", d.toolCalls, "iter_tool_calls", d.iterToolCalls, "iter_mutations", d.iterMutations)
		if runErr != nil || !d.driveToCompletion || ctx.Err() != nil {
			break
		}
		// P39.5: after the opening turn the model has seen the full
		// SKILL.md. Re-sending its ~9K-token body in the first user
		// message every turn is the drive's context-bounding failure — on
		// a 32K local window (prompt_bytes≈31534 at turn 0) the recon
		// digest plus a few file reads then leave no room to edit_file (a
		// scaffolded resume made 86 tool calls across 3 iterations and
		// cleared 0 of 23 markers). Rewrite the first message once,
		// swapping the skill body for a compact pointer the model can
		// re-read on demand — the same disposable-skill-reference logic
		// P36.2 applies to skill-reference reads. Guarded so it only
		// touches the message while it still carries the preamble (engine
		// compaction may have already rewritten it).
		if !preambleCompacted {
			preambleCompacted = true
			if compactFirstSkillMessage(d.conv, d.skillName, d.taskPrompt, d.skillDir) {
				d.logger.Info("chat: compacted SKILL.md preamble out of first message (P39.5)", "skill", d.skillName)
			}
		}
		pending := scanPendingMarkers(d.pendingRoot)
		if len(pending) == 0 {
			// P39.6: "all markers filled" is not the real done-condition —
			// "verifies clean" is. Run the skill's bundled phase-6 checks
			// (verify.py, lint_dfd.py, inventory.py --check); on failure,
			// feed the failure text back for an in-place fix and re-run,
			// bounded. When the skill ships no verifier / there's nothing to
			// check, verifySkillOutputs reports ran=false and the drive ends
			// as before. This is the autonomous analogue of SKILL.md §5's
			// fix-and-re-run round.
			failures, ran := drive.VerifySkillOutputs(d.skillName, d.skillDir, d.cwd)
			if !ran {
				break // nothing to verify (skill has no verifier / no run dir) — done
			}
			if failures == "" {
				// Mechanical checks are clean. P38.1: run one substantive
				// quality-and-sanity pass the scripts can't do (groundedness,
				// filler, internal coherence), then re-verify. Bounded to a
				// single pass so it terminates; a regression it introduces is
				// caught by the mechanical fix loop above on the next iteration.
				if !qualityReviewed {
					qualityReviewed = true
					d.logger.Info("chat: mechanical checks clean, running final quality pass (P38.1)")
					d.conv.Append(provider.Message{
						Role:    provider.RoleUser,
						Content: []provider.Block{provider.TextBlock{Text: drive.QualityReviewPrompt()}},
					})
					continue
				}
				break // verified clean and quality-reviewed — done
			}
			if verifyRounds++; verifyRounds > drive.MaxVerifyRounds {
				d.logger.Warn("chat: verification still failing after max rounds", "rounds", drive.MaxVerifyRounds)
				fmt.Fprintf(d.errOut, "\n[notice: phase-6 verification still failing after %d fix round(s); stopping with an unverified suite — inspect the run directory and the failures above]\n", drive.MaxVerifyRounds)
				fmt.Fprintf(d.errOut, "%s\n", failures)
				break
			}
			d.logger.Info("chat: verification failed, feeding back for fix", "round", verifyRounds)
			d.conv.Append(provider.Message{
				Role:    provider.RoleUser,
				Content: []provider.Block{provider.TextBlock{Text: drive.VerifyFixPrompt(failures)}},
			})
			continue
		}
		if iter+1 >= d.maxTurns {
			msg := fmt.Sprintf("drive-to-completion hit --max-turns=%d with %d file(s) still PENDING: %s", d.maxTurns, len(pending), strings.Join(pending, ", "))
			d.logger.Warn("chat: " + msg)
			fmt.Fprintf(d.errOut, "\n[notice: %s — re-run to resume]\n", msg)
			break
		}
		// P39.7 no-progress guard. A weak local model sometimes ends a
		// turn with a plan ("Now I'll write the file…") and no file
		// mutation at all — the "announce then yield" stall (reproduced on
		// gpt-oss:20b and qwen3.6:35b-a3b: markers present, 0 edit_file,
		// yields repeatedly). Treat a turn that neither mutated a suite
		// file nor changed the PENDING set (the model may narrate, read,
		// or re-run recon without editing) as no progress. Direct evidence
		// this is the right lever: adding an "act now" preamble to a
		// stalled gpt-oss:20b run landed the first real edit_file (P38.1).
		// Instead of silently yielding a partial suite, re-prompt with an
		// explicit "act now — call edit_file, no narration" nudge, bounded
		// to drive.MaxNoProgressTurns consecutive stalls before stopping.
		// Extends P39.2's tool-execution coaching from the malformed-call
		// case to the no-call case.
		madeProgress := d.iterMutations > 0 || !drive.SameStrings(pending, prevPending)
		prevPending = pending
		nudge := ""
		if !madeProgress {
			if noProgress++; noProgress >= drive.MaxNoProgressTurns {
				fmt.Fprintf(d.errOut, "\n[notice: model stalled %d turns without mutating a file while %d file(s) remain PENDING; stopping — re-run to resume]\n", noProgress, len(pending))
				break
			}
			nudge = drive.ActNowNudge()
		} else {
			noProgress = 0
		}
		d.conv.Append(provider.Message{
			Role:    provider.RoleUser,
			Content: []provider.Block{provider.TextBlock{Text: nudge + continuePrompt(pending)}},
		})
	}
	return runErr
}

// installSignalCancel derives a cancellable context from parent that cancels
// when an interrupt (Ctrl-C) or SIGTERM arrives, logging which signal fired
// first. It returns the context and a cleanup func that stops signal delivery
// and cancels the context (which also lets the watcher goroutine exit, so it
// does not leak). Ctrl-C behavior is preserved — an interrupt still cancels
// the run.
//
// P35.8: the bare signal.NotifyContext(os.Interrupt) this replaces recorded
// nothing about which signal fired, so a signal-driven exit was
// indistinguishable from a silent mid-run death in aegis.log. syscall.SIGTERM
// is portable in Go's os/signal across win32 and unix.
func installSignalCancel(parent context.Context, logger *slog.Logger) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go watchSignal(ctx, cancel, sigCh, logger)
	return ctx, func() {
		signal.Stop(sigCh)
		cancel()
	}
}

// driveCompaction builds the proactive-compaction wiring for the CLI drive
// engine: the effective context window and a Summarizer over it. Split out of
// the command closure (P47.1) so a regression test can assert the CLI path
// enables compaction (non-zero window + non-nil compactor) — guarding against
// the divergence where the CLI engine.New set neither ContextWindowTokens nor
// Compactor, so a multi-turn --skill drive grew context every turn with no
// defense until the model server hard-rejected the request. Mirrors the
// daemon's build (internal/server/server.go / engine_build.go): prefer a fast
// small model for the summary calls, and skip auto-compaction (rather than
// defaulting to the 120k cloud budget) when a local window is still unknown.
func driveCompaction(ctx context.Context, cfg *config.Config, adapter provider.Adapter, logger *slog.Logger) (engine.Compactor, int) {
	ctxWin := resolveDriveContextWindow(ctx, cfg, logger)
	compModel := cfg.Provider.Model
	if cfg.Provider.SmallModel != "" {
		compModel = cfg.Provider.SmallModel // prefer a fast small model for compaction
	}
	compOpts := compaction.Options{
		Adapter:       adapter,
		Model:         compModel,
		ContextWindow: ctxWin,
		// P66.14: the trigger reserves room for the completion, so it needs the
		// same max_tokens the engine's own gate uses. Without it the summarizer
		// gates at a flat 85% while the engine gates at half the window, and a
		// drive's compactions land too late to leave room for an answer.
		MaxTokens: cfg.Provider.MaxTokens,
		// Mirrors the daemon: on a prefix-caching local backend the prune
		// pre-pass is gated on headroom rather than run unconditionally, since
		// rewriting the middle of the conversation there costs a full prefill
		// recompute. A phased drive is exactly the workload that measured it.
		//
		// Resolved through PreservePrefixCacheOr rather than from
		// config.LocalBackend directly, which is what this line used to do — and
		// which meant compaction.preserve_prefix_cache was silently ignored on
		// the CLI path. The escape hatch the daemon honoured did not exist on the
		// path phased drives actually run on, i.e. the one the gate was measured
		// against in the first place.
		PreservePrefixCache: cfg.Compaction.PreservePrefixCacheOr(
			config.LocalBackend(cfg.Provider.Default, cfg.Provider.BaseURL)),
	}
	// A local provider whose window is still unknown: skip auto-compaction
	// rather than defaulting to the 120k cloud budget, which on a 4k-32k local
	// server would never fire before the server front-truncates the prompt.
	if ctxWin == 0 && cfg.Provider.Default == "ollama" {
		compOpts.MaxBudget = 0 // explicit skip
	}
	return compaction.New(compOpts), ctxWin
}

// recommendPhasedDriveWindow resolves, for a phased --skill drive on an
// Ollama-backed provider, the serving context window to size the run to (P47.5a)
// and the model's training-context max (the P47.5b escalation ceiling). It
// recommends ollamainfo.RecommendContextWindow(model max) — half the model max,
// memory-capped — so the phased build gets the room it needs without the manual
// AEGIS_PROVIDER_CONTEXT_WINDOW bump the 2026-07-24 run required. ok is false
// when the provider isn't plausibly Ollama-backed or the server can't be
// probed, in which case the caller leaves the configured window untouched.
// Mirrors resolveDriveContextWindow's Ollama-target gate.
func recommendPhasedDriveWindow(ctx context.Context, cfg *config.Config) (win, modelMax int, ok bool) {
	p := cfg.Provider
	if p.Default != "ollama" && (p.Default != "openai" || p.BaseURL == "") {
		return 0, 0, false
	}
	res, detected := ollamainfo.Detect(ctx, ollamainfo.NativeBase(p.BaseURL), p.Model)
	if !detected {
		return 0, 0, false
	}
	return ollamainfo.RecommendContextWindow(res.ModelMax), res.ModelMax, true
}

// resolveDriveContextWindow returns the context window the model server will
// actually honor, for the one-shot CLI path. It mirrors the daemon's
// initContextWindow (internal/server/contextwindow.go): the configured
// provider.context_window, reconciled downward when a *loaded* Ollama model is
// found to be serving less (Ollama silently front-truncates an oversized prompt
// otherwise). Returns 0 ("unknown") only when neither config nor detection
// yields a value — the compaction fallback handles that case. Unlike the
// daemon there is no long-lived retry state to maintain: a single `aegis chat`
// invocation resolves the window once, up front.
func resolveDriveContextWindow(ctx context.Context, cfg *config.Config, logger *slog.Logger) int {
	cfgWin := cfg.Provider.ContextWindow
	p := cfg.Provider
	// Only probe when the target could plausibly be Ollama: the explicit
	// "ollama" provider, or an "openai" provider re-pointed at a custom base
	// URL (the documented way to run against a local OpenAI-compat endpoint).
	if p.Default != "ollama" && (p.Default != "openai" || p.BaseURL == "") {
		return cfgWin
	}
	res, ok := ollamainfo.Detect(ctx, ollamainfo.NativeBase(p.BaseURL), p.Model)
	if !ok {
		return cfgWin
	}
	switch {
	case cfgWin > 0 && res.Authoritative() && res.ContextWindow < cfgWin:
		// Config promises more than Ollama is actually serving — trusting the
		// config here is exactly the silent-truncation failure. Serve reality.
		logger.Warn("configured context_window exceeds what Ollama is serving; using the served value",
			"configured", cfgWin, "served", res.ContextWindow,
			"hint", "raise OLLAMA_CONTEXT_LENGTH on the Ollama server or pin num_ctx in a modelfile")
		return res.ContextWindow
	case cfgWin > 0:
		return cfgWin
	default:
		logger.Info("auto-detected Ollama context window for drive engine", "window", res.Describe(), "model_max", res.ModelMax)
		return res.ContextWindow
	}
}

// watchSignal blocks until either a signal arrives (logs it, then cancels) or
// the context is done (run completed / cleanup called — nothing to log, just
// return so the goroutine does not leak). Split out from installSignalCancel so
// the log-and-cancel behavior is unit-testable without delivering a real OS
// signal.
func watchSignal(ctx context.Context, cancel context.CancelFunc, sigCh <-chan os.Signal, logger *slog.Logger) {
	select {
	case sig := <-sigCh:
		if logger != nil {
			// P35.8: log the signal cause BEFORE cancelling the run.
			logger.Warn("chat: received signal, cancelling run", "signal", sig.String())
		}
		cancel()
	case <-ctx.Done():
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

type outputFormatKind int

const (
	outputText outputFormatKind = iota
	outputJSON
	outputStreamJSON
)

func parseOutputFormat(s string) (outputFormatKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return outputText, nil
	case "json":
		return outputJSON, nil
	case "stream-json", "stream_json", "streamjson":
		return outputStreamJSON, nil
	default:
		return outputText, fmt.Errorf("invalid --output-format %q (want text, json, or stream-json)", s)
	}
}

// chatResult is the machine-readable summary emitted in json / stream-json mode.
type chatResult struct {
	Type    string  `json:"type,omitempty"` // "result" in stream-json trailer
	Answer  string  `json:"answer,omitempty"`
	CostUSD float64 `json:"cost_usd"`
	Turns   int     `json:"turns"`
	// InputTokens is the sum of the per-turn prompt-token counts across every
	// turn of the run — i.e. total input tokens *processed*, which is the
	// billable-input basis for a cloud provider (each agentic turn re-sends the
	// growing conversation and is charged for it; prompt-cache reads are billed
	// separately and priced at the discounted rate — see internal/cost). It is
	// deliberately NOT a de-duplicated or cache-adjusted figure. On the
	// native-Ollama path this is prompt_eval_count, the full context size every
	// turn (P35.13), so the sum overstates the *local prefill work actually
	// done* by the KV-cache-hit factor — but local cost is $0, so the number is
	// informational there; it is the cloud cost figure it must be accurate for.
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ToolCalls    int    `json:"tool_calls"`
	Error        string `json:"error,omitempty"`
}

// streamEvent is one line of stream-json output.
type streamEvent struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolResult string          `json:"tool_result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Error      string          `json:"error,omitempty"`
	// Usage fields, populated on a "turn_done" line (P38.3) so per-turn context
	// growth is observable from a stream-json consumer without SQLite
	// spelunking or debug-log tailing — previously this Kind fell through the
	// switch below with none of its Usage/CostUSD payload serialized at all.
	InputTokens          int     `json:"input_tokens,omitempty"`
	OutputTokens         int     `json:"output_tokens,omitempty"`
	CacheReadTokens      int     `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens  int     `json:"cache_creation_tokens,omitempty"`
	PromptEvalDurationMS int64   `json:"prompt_eval_duration_ms,omitempty"`
	CostUSD              float64 `json:"cost_usd,omitempty"`
}

func emitStreamEvent(w io.Writer, ev engine.Event) {
	se := streamEvent{Type: string(ev.Kind)}
	switch ev.Kind {
	case engine.KindText, engine.KindThinking, engine.KindNotice:
		// A notice carries its whole payload in Text — omitting it here shipped
		// bare `{"type":"notice"}` lines that told a stream-json consumer
		// nothing at all.
		se.Text = ev.Text
	case engine.KindToolCall:
		se.Tool, se.ToolInput = ev.ToolName, ev.ToolInput
	case engine.KindToolResult:
		se.Tool, se.ToolResult, se.IsError = ev.ToolName, ev.ToolResult, ev.ToolIsError
	case engine.KindError:
		se.Error = errString(ev.Err)
	case engine.KindTurnDone:
		if ev.Usage != nil {
			se.InputTokens = ev.Usage.InputTokens
			se.OutputTokens = ev.Usage.OutputTokens
			se.CacheReadTokens = ev.Usage.CacheReadTokens
			se.CacheCreationTokens = ev.Usage.CacheCreationTokens
			se.PromptEvalDurationMS = ev.Usage.PromptEvalDurationMS
		}
		se.CostUSD = ev.CostUSD
	case engine.KindTrace:
		return // server-internal; never emit
	}
	line, err := json.Marshal(se)
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(line))
}

func emitFinalJSON(w io.Writer, res chatResult) {
	line, err := json.Marshal(res)
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(line))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func handleCLISlash(cmd *cobra.Command, cfg *config.Config, parsed *commands.ParsedCommand) error {
	out := cmd.OutOrStdout()
	switch parsed.Name {
	case "help":
		fmt.Fprintln(out, "Available slash commands (in TUI): /help, /persona, /mode, /clear, /memory, /remember, /skills, /commands, /models, /session, /quit")
		fmt.Fprintln(out, "\nCustom commands:")
		cwd, _ := os.Getwd()
		reg := commands.Discover(commands.CommandDirs(cfg.DataDir, cwd)...)
		for _, c := range reg.List() {
			argStr := ""
			if len(c.Args) > 0 {
				argStr = " <" + strings.Join(c.Args, "> <") + ">"
			}
			fmt.Fprintf(out, "  /%-22s %s\n", c.Name+argStr, c.Description)
		}
		if len(reg.List()) == 0 {
			fmt.Fprintln(out, "  (none)")
		}
		return nil
	default:
		cwd, _ := os.Getwd()
		reg := commands.Discover(commands.CommandDirs(cfg.DataDir, cwd)...)
		if c, ok := reg.Get(parsed.Name); ok {
			values := make(map[string]string)
			for i, arg := range c.Args {
				if i < len(parsed.Args) {
					values[arg] = parsed.Args[i]
				}
			}
			expanded := c.Expand(values)
			fmt.Fprintln(out, expanded)
			return nil
		}
		return fmt.Errorf("/%s is only available in the TUI (run aegis without arguments)", parsed.Name)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// appendUnique adds name to list unless a case-insensitive match is already
// present, returning a copy so the caller's slice (cfg's) is never mutated.
func appendUnique(list []string, name string) []string {
	for _, e := range list {
		if strings.EqualFold(e, name) {
			return list
		}
	}
	out := make([]string, len(list), len(list)+1)
	copy(out, list)
	return append(out, name)
}

// skillPreamble frames a preloaded skill body as authoritative instructions
// ahead of the task, mirroring the TUI's skillTaskMessage (internal/tui) so the
// scripted --skill path and the interactive /threat-model path present the skill
// to the model identically.
func skillPreamble(name, body string) string {
	return fmt.Sprintf("The %s skill has been loaded for you. Its full instructions are below — follow them for this task.\n\n<skill name=%q>\n%s\n</skill>\n\n", name, name, body)
}

// driveContextFloor is the served Ollama context window (tokens) below which a
// --skill drive on the compat path is warned about (P39.9). It matches the
// num_ctx the reference wrapper had to bake for the 35B MoE re-test: a
// skill-driven prompt reached ~34774 tokens, overflowing the stock 16384
// modelfile default.
const driveContextFloor = 32768

// warnCompatDriveWindow probes the served Ollama context window and, when a
// --skill drive is about to run on the OpenAI-compat (/v1) adapter with a window
// too small to hold a skill-driven prompt, prints a notice naming the fix
// (P39.9). Best-effort: a probe failure yields a softer notice (the compat path
// never honors context_window regardless), never an error.
func warnCompatDriveWindow(w io.Writer, cfg *config.Config) {
	if !providerfactory.IsLegacyOllamaCompat(cfg.Provider) {
		return
	}
	dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, ok := ollamainfo.Detect(dctx, ollamainfo.NativeBase(cfg.Provider.BaseURL), cfg.Provider.Model)
	served := 0
	want := driveContextFloor
	if ok {
		served = res.ContextWindow
		if rec := ollamainfo.RecommendContextWindow(res.ModelMax); rec > want {
			want = rec
		}
	}
	if msg := compatDriveWindowNotice(cfg.Provider.Model, served, want); msg != "" {
		fmt.Fprintf(w, "\n[notice: %s]\n", msg)
	}
}

// compatDriveWindowNotice is the pure decision behind warnCompatDriveWindow: it
// returns the notice text when the served window (served, 0 = undetectable) is
// below want, or "" when the served window is already large enough. Split out so
// the threshold and message are testable without a live Ollama server.
func compatDriveWindowNotice(model string, served, want int) string {
	if served >= want && served > 0 {
		return "" // served window already holds a skill-driven prompt
	}
	servedDesc := "unknown (could not probe the Ollama server)"
	if served > 0 {
		servedDesc = fmt.Sprintf("%d tokens", served)
	}
	notice := fmt.Sprintf("this --skill drive is on the /v1 compat adapter, which cannot send num_ctx; the served context window is %s and a skill-driven prompt can exceed it (Ollama then silently truncates it from the front). Switch to provider.default: ollama so context_window is honored", servedDesc)
	if recipe := providerfactory.LegacyOllamaModelfileRecipe(model, want); recipe != "" {
		notice += " — or " + recipe
	}
	return notice
}

// compactFirstSkillMessage rewrites the drive's first user message in place,
// replacing the full preloaded SKILL.md body with a compact pointer plus the
// original task (P39.5). It reports whether it actually rewrote anything: it is
// a no-op (returning false) unless the first message is still the user
// preamble message skillPreamble produced — engine compaction may have already
// rewritten it, in which case there is nothing to do. After a rewrite it calls
// conv.Invalidate so the cached token estimate and persistence offset stay
// correct.
func compactFirstSkillMessage(conv *engine.Conversation, skillName, taskPrompt, skillDir string) bool {
	if skillName == "" || len(conv.Messages) == 0 {
		return false
	}
	first := conv.Messages[0]
	if first.Role != provider.RoleUser || len(first.Content) == 0 {
		return false
	}
	txt, ok := first.Content[0].(provider.TextBlock)
	if !ok || !strings.Contains(txt.Text, "<skill name=") {
		return false // not (or no longer) the full preamble message
	}
	conv.Messages[0].Content = []provider.Block{provider.TextBlock{Text: compactSkillPreamble(skillName, taskPrompt, skillDir)}}
	conv.Invalidate()
	return true
}

// compactSkillPreamble is the slimmed replacement for skillPreamble used after
// the opening drive turn (P39.5): it reminds the model the skill's instructions
// were already given and remain in force, names the on-disk file to re-read for
// any specific rule, and re-states the task — without re-sending the ~9K-token
// body every turn.
func compactSkillPreamble(name, taskPrompt, skillDir string) string {
	ref := "its bundled instructions"
	if skillDir != "" {
		ref = fmt.Sprintf("`%s` (and its `references/`)", filepath.ToSlash(filepath.Join(skillDir, "SKILL.md")))
	}
	return fmt.Sprintf("The %s skill's full instructions were provided at the start of this task and remain in effect — keep following them. They are not repeated here so the context stays small enough to work in; re-read %s if you need a specific rule.\n\n", name, ref) + taskPrompt
}

// mutatingTools are the built-in tools whose successful call mutates a suite
// file on disk. The P39.7 no-progress guard uses a call to one of these (or a
// change in the PENDING marker set) as the signal that a drive turn made real
// progress, as opposed to only narrating, reading, or re-running recon. Kept
// deliberately narrow to the file-writing tools: recon/scaffold create files
// via `shell` (python), but those turns are still caught as progress because
// they change the PENDING set.
var mutatingTools = map[string]bool{
	"write_file": true,
	"edit_file":  true,
	"multi_edit": true,
}

// continuePrompt is the drive-to-completion continuation turn (P38.2): it names
// the files still carrying `<!-- PENDING -->` markers and tells the model to
// resume in dependency order without pausing to ask, matching the resume
// contract in the threat-modeling skill's SKILL.md §4.2. Only called with a
// non-empty list (the drive loop stops when no markers remain).
func continuePrompt(pending []string) string {
	return "Continue — the task is not finished. These files still contain `<!-- PENDING: … -->` markers and must be completed:\n- " +
		strings.Join(pending, "\n- ") +
		"\n\nResume from the first unfinished file in dependency order and keep working until NO `<!-- PENDING` marker remains in any file. Each marker is section-keyed (`<!-- PENDING: <section> -->`): edit that exact marker one at a time — never a bare `<!-- PENDING -->` and never `replace_all` on a marker, which would overwrite every section at once. Fill ONE section per `edit_file`; do not write a whole file or several sections in a single call — a monolithic write is slow and can truncate into a malformed edit. This is a non-interactive run: do not stop to ask whether to proceed, and do not return a partial result."
}

// scanPendingMarkers walks root (typically <cwd>/.aegis) and returns the
// root-relative paths of text files that still contain a `<!-- PENDING`
// marker — the stub-first pattern multi-phase skills use to mark unfinished
// files. The match is the marker *prefix*, not the exact `<!-- PENDING -->`
// literal: scaffold.py now emits section-keyed markers
// (`<!-- PENDING: deployment-classification -->`, P38.7) so an `edit_file` can
// target one section without a `replace_all` file-nuke, and the prefix catches
// those as well as any bare legacy marker. Only small text-ish files are read
// (the marker only ever appears in generated markdown/yaml/mmd), so the walk
// stays cheap. A missing root yields no matches.
func scanPendingMarkers(root string) []string {
	const marker = "<!-- PENDING"
	const maxFileSize = 1 << 20 // 1 MiB — generated report files are far smaller
	var hits []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// P39.11: the materialized builtin-skills assets (skeleton
			// templates, SKILL.md/README) carry their own `<!-- PENDING`
			// markers as examples; counting them would keep the drive's
			// completion oracle permanently non-empty (it never converges,
			// phase-6 verify never fires). Skip that subtree — it is skill
			// source, not build output.
			if d.Name() == pendingSkipDir {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".mmd", ".markdown", ".yaml", ".yml", ".txt":
		default:
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), marker) {
			if rel, err := filepath.Rel(root, path); err == nil {
				hits = append(hits, filepath.ToSlash(rel))
			} else {
				hits = append(hits, filepath.ToSlash(path))
			}
		}
		return nil
	})
	sort.Strings(hits)
	return hits
}

// suiteFileCount returns how many text-ish files exist anywhere under root
// (typically <cwd>/.aegis). The P38.6 drive-to-completion floor check uses it to
// tell "finished — every marker resolved" from "nothing was ever written" —
// both of which leave scanPendingMarkers empty, but only the latter is a
// fabricated success. A missing root yields zero.
func suiteFileCount(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// P39.11: exclude the materialized builtin-skills source (see
			// scanPendingMarkers) — otherwise its asset files count as "suite
			// written", defeating the P38.6 fabricated-success floor check that
			// distinguishes "finished" from "nothing was ever written".
			if d.Name() == pendingSkipDir {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".mmd", ".markdown", ".yaml", ".yml", ".txt":
			n++
		}
		return nil
	})
	return n
}

// pendingSkipDir is the subdirectory of <cwd>/.aegis holding builtin skill
// source materialized into the workspace (P39.10) — skeleton templates and
// SKILL.md bodies that carry their own `<!-- PENDING` example markers. Both
// drive-completion scans skip it so skill source is never mistaken for build
// output. Mirrors skills.builtinSkillsDirName (kept as a local literal to avoid
// exporting an internal constant across the package boundary).
const pendingSkipDir = "builtin-skills"

// skillPhaseSpecs returns the named skill's declared `phases:` plan (P52.12),
// or nil when the skill has none, cannot be found, or none was named.
//
// Read here — before the workspace is otherwise set up — only so the P47.5(a)
// up-front window sizing knows whether the run will be phased. skills.Discover
// memoizes its walk, so this costs nothing beyond the first call; the drive
// itself uses the specs carried on the Skill it loads later.
func skillPhaseSpecs(cfg *config.Config, skillName string) []skills.PhaseSpec {
	if skillName == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	sk, ok := skills.Load(cwd, cfg.DataDir, appendUnique(cfg.Skills.BuiltinEnabled, skillName), skillName)
	if !ok {
		return nil
	}
	return sk.Phases
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
