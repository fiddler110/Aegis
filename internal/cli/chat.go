package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/drive"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/logging"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/skills"
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
			foldAnswerEvent(&answer, ev, 0)
		default: // text
			switch ev.Kind {
			case engine.KindText:
				rend.Text(ev.Text)
			case engine.KindGuard:
				// Streamed text is already on the user's terminal and cannot
				// be unprinted, so say plainly that what was just shown is
				// being withdrawn rather than letting the retry read as a
				// second answer (EXEC-2).
				switch {
				case ev.GuardRetrying:
					rend.Notice("⚠ output guard: answer withdrawn (" + ev.GuardReason + ") — retrying…")
				case !ev.GuardPassed && ev.GuardReason != "":
					rend.Notice("⚠ output guard: " + ev.GuardReason)
				}
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
		Level:        cfg.LogLevel,
		Path:         cfg.LogPath(),
		ToStderr:     debugToStderr,
		MaxSizeBytes: cfg.LogMaxSizeBytes(),
		MaxBackups:   cfg.Log.MaxBackups,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("init logger: %w", err)
	}
	return logger, func() { logCloser.Close() }, nil
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
