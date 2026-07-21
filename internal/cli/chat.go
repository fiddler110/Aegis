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

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/logging"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/repomap"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/skills"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
)

func newChatCmd() *cobra.Command {
	var (
		system       string
		mode         string
		personaName  string
		autoApprove  bool
		outputFormat string
		skillName    string
		maxTurns     int
	)

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Run a one-shot chat turn through the agent engine (no TUI)",
		Long: "Sends a single prompt to the model and streams the response. Reads the prompt from arguments, or from stdin if none are given.\n\n" +
			"With --skill <name>, the named skill's full instructions are preloaded into the prompt (so a small local model never has to discover and fetch them via the `skill` tool) and the run is driven to completion: after each turn, if any file under .aegis/ still contains a `<!-- PENDING -->` marker, chat auto-continues rather than stopping when the model yields. This is what lets a long, multi-phase skill (threat model, deep research) finish non-interactively; a plain one-shot turn would stop at the first yield with a partial suite (P38.2). --max-turns bounds the drive.",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			const maxPromptSize = 1 << 20 // 1 MiB
			prompt := strings.TrimSpace(strings.Join(args, " "))
			if prompt == "" {
				data, _ := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxPromptSize))
				prompt = strings.TrimSpace(string(data))
			}
			if prompt == "" {
				return fmt.Errorf("no prompt provided (pass as arguments or via stdin)")
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
			if skillName != "" && cfg.Provider.Think != nil && *cfg.Provider.Think {
				off := false
				cfg.Provider.Think = &off
				fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: --skill drives the run to completion via tool calls; disabling provider.think for this run so the model executes the phases instead of simulating them in its reasoning trace (P38.6)]\n")
			}

			adapter, err := providerfactory.Build(cfg, nil)
			if err != nil {
				return err
			}

			// Wire cfg.LogLevel into a real logger (P35.7 follow-up): without
			// this, engine.New falls back to slog.Default() (info level,
			// unstructured stderr), so the debug-level prompt_eval
			// instrumentation added in P35.7 is invisible from `aegis chat`,
			// the natural non-interactive/scriptable path. Mirrors the
			// logging.New usage in serve/acp/mcp-serve.
			if err := cfg.EnsureDataDir(); err != nil {
				return fmt.Errorf("ensure data dir: %w", err)
			}
			// At debug level, also mirror logs to stderr so the diagnostics an
			// operator explicitly opted into (prompt_eval prefill counts, tool
			// timings) are visible immediately without tailing aegis.log. The
			// answer streams to stdout, so stderr mirroring never corrupts it.
			// Kept off below debug so a normal `aegis chat` run stays quiet on
			// stderr (info-level WARNs about sandbox/MCP would otherwise leak
			// into scripted output).
			debugToStderr := strings.EqualFold(strings.TrimSpace(cfg.LogLevel), "debug")
			logger, logCloser, err := logging.New(logging.Options{
				Level:    cfg.LogLevel,
				Path:     cfg.LogPath(),
				ToStderr: debugToStderr,
			})
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer logCloser.Close()

			// P35.8: capture an otherwise-silent panic to aegis.log before the
			// process dies. A live `aegis chat` run once vanished mid-turn
			// leaving nothing on disk — no panic, no signal record, no final
			// answer. Registered AFTER logCloser's defer so, by LIFO ordering,
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
					return fmt.Errorf("materialize built-in skills: %w", err)
				}
			}

			reg := tool.NewRegistry()
			if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, BuiltinSkills: enabledBuiltins, SecurityScan: security.OptionsFromConfig(cfg.Security), DASTAllowedTargets: cfg.Security.DAST.AllowedTargets, DASTAllowActive: cfg.Security.DAST.AllowActive}); err != nil {
				return err
			}

			resolvedMode := cfg.Permission.Mode
			if mode != "" {
				resolvedMode = mode
			}
			var approver permission.Approver = permission.AutoDeny{}
			if autoApprove {
				approver = permission.AutoApprove{}
			}
			gate := permission.New(permission.ParseMode(resolvedMode), approver)

			tracker := cost.NewTracker()
			eng, err := engine.New(engine.Options{
				Adapter:   adapter,
				Tools:     reg,
				Gate:      gate,
				Cost:      tracker,
				BudgetUSD: cfg.Cost.BudgetUSD,
				Model:     cfg.Provider.Model,
				MaxTokens: cfg.Provider.MaxTokens,
				Logger:    logger,
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
			if skillName != "" {
				if sk, ok := skills.Load(cwd, cfg.DataDir, enabledBuiltins, skillName); ok && strings.TrimSpace(sk.Content) != "" {
					prompt = skillPreamble(skillName, sk.Content) + prompt
					driveToCompletion = true
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "\n[warning: --skill %q not found or empty; running without preloaded skill body]\n", skillName)
				}
			}

			conv := &engine.Conversation{System: buildChatSystem(cfg, cwd, enabledBuiltins, system, personaName)}
			conv.Append(provider.Message{
				Role:    provider.RoleUser,
				Content: []provider.Block{provider.TextBlock{Text: prompt}},
			})

			format, err := parseOutputFormat(outputFormat)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			var answer strings.Builder
			toolCalls := 0
			iterToolCalls := 0
			onEvent := func(ev engine.Event) {
				// Counted for every format: both json and stream-json report
				// this in their trailer, so counting it per-branch left
				// stream-json's summary permanently reading zero.
				if ev.Kind == engine.KindToolCall {
					toolCalls++
					iterToolCalls++
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
						fmt.Fprint(out, ev.Text)
					case engine.KindNotice:
						// Advisories (empty answer, cold load, a model that
						// can't call tools) were dropped entirely here, so the
						// default output format was the one surface that never
						// showed them.
						fmt.Fprintf(out, "\n[notice: %s]\n", ev.Text)
					case engine.KindToolCall:
						fmt.Fprintf(out, "\n[tool: %s %s]\n", ev.ToolName, string(ev.ToolInput))
					case engine.KindToolResult:
						tag := "ok"
						if ev.ToolIsError {
							tag = "error"
						}
						fmt.Fprintf(out, "[tool result (%s): %s]\n", tag, truncate(ev.ToolResult, 500))
					case engine.KindDone:
						fmt.Fprintln(out)
					}
				}
			}

			// P35.8: bracket the engine run with boundary markers. A silent
			// mid-run disappearance then shows up as a "run starting" line with
			// no matching "run finished" — exactly the diagnostic signal that
			// was missing when a live chat turn vanished without a trace.
			logger.Info("chat: run starting", "prompt_bytes", len(prompt), "drive", driveToCompletion)

			// P38.2 drive-to-completion. A multi-phase skill (threat model, deep
			// research) is many turns in one context; a plain one-shot chat stops
			// at the first yield, so a model that pauses to ask "shall I proceed?"
			// mid-build (the motivating failure) leaves a partial suite behind its
			// unresolved `<!-- PENDING -->` stubs. When --skill preloaded such a
			// skill, keep running while any file under .aegis/ still carries a
			// PENDING marker: append a continuation turn and run again — reusing
			// the SAME conversation so context threads (and pruning/compaction
			// apply) across the whole drive. Bounded by --max-turns and a
			// no-progress guard (three consecutive yields that call no tool at all
			// means the model is only talking, so stop rather than burn tokens).
			//
			// The oracle is the stub-first pattern the skill uses (SKILL.md §4.1):
			// a PENDING marker is unambiguous unfinished work. If a model instead
			// writes full file content without ever stubbing (observed on
			// qwen3:14b, which skips the setup step), no markers appear and the
			// drive simply ends when the model yields — correct when it finished,
			// and a known limitation when it didn't, but never a wrong forced
			// continuation.
			pendingRoot := filepath.Join(cwd, ".aegis")
			noProgress := 0
			var runErr error
			for iter := 0; ; iter++ {
				iterToolCalls = 0
				runErr = eng.Run(ctx, conv, onEvent)
				logger.Info("chat: run finished", "iter", iter, "err", errString(runErr), "tool_calls", toolCalls, "iter_tool_calls", iterToolCalls)
				if runErr != nil || !driveToCompletion || ctx.Err() != nil {
					break
				}
				pending := scanPendingMarkers(pendingRoot)
				if len(pending) == 0 {
					break // no unfinished stubs remain — the run is complete
				}
				if iter+1 >= maxTurns {
					msg := fmt.Sprintf("drive-to-completion hit --max-turns=%d with %d file(s) still PENDING: %s", maxTurns, len(pending), strings.Join(pending, ", "))
					logger.Warn("chat: " + msg)
					fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: %s — re-run to resume]\n", msg)
					break
				}
				if iterToolCalls == 0 {
					if noProgress++; noProgress >= 3 {
						fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: model yielded %d times without calling a tool; stopping]\n", noProgress)
						break
					}
				} else {
					noProgress = 0
				}
				conv.Append(provider.Message{
					Role:    provider.RoleUser,
					Content: []provider.Block{provider.TextBlock{Text: continuePrompt(pending)}},
				})
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
			if driveToCompletion && runErr == nil && ctx.Err() == nil && suiteFileCount(pendingRoot) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "\n[notice: the run reported completion but wrote no files under .aegis/ — the model may have described the work instead of executing it. Nothing was produced; re-run (with provider.think disabled if it is on) and check the transcript.]\n")
			}

			snap := tracker.Snapshot()
			switch format {
			case outputJSON:
				emitFinalJSON(out, chatResult{
					Answer:       strings.TrimSpace(answer.String()),
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
					fmt.Fprintf(cmd.ErrOrStderr(), "\n[cost: $%.4f over %d turn(s), %d in / %d out tokens]\n",
						snap.TotalUSD, snap.Turns, snap.Usage.InputTokens, snap.Usage.OutputTokens)
				}
			}
			return runErr
		},
	}

	cmd.Flags().StringVar(&system, "system", "", "system prompt (overrides --persona)")
	cmd.Flags().StringVar(&mode, "mode", "", "permission mode: plan (read-only) or build (default from config)")
	cmd.Flags().StringVar(&personaName, "persona", "", "persona to use (e.g. security, developer, sre)")
	cmd.Flags().BoolVar(&autoApprove, "yes", false, "auto-approve tool calls that would otherwise require confirmation")
	cmd.Flags().StringVar(&outputFormat, "output-format", "text", "output format: text, json (final result object), or stream-json (one event per line)")
	cmd.Flags().StringVar(&skillName, "skill", "", "preload the named skill's full instructions into the prompt and drive the run to completion (auto-continue while `<!-- PENDING -->` markers remain under .aegis/) — for multi-phase skills like threat-modeling that a one-shot turn would leave half-finished")
	cmd.Flags().IntVar(&maxTurns, "max-turns", 40, "with --skill, the maximum number of drive-to-completion turns before stopping with a resumable partial result")
	return cmd
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

// buildChatSystem assembles the one-shot chat system prompt so the CLI path is
// equivalent to the daemon's effectiveSystem (internal/server/helpers.go):
// persona base + shared blocks + memory/context + the <skills_available> index
// + the cached repo map. Extracted from the command closure so the assembly —
// in particular that skills are advertised, without which the registered
// `skill` tool is undiscoverable — is unit-testable. explicit --system wins,
// then --persona, then general.
func buildChatSystem(cfg *config.Config, cwd string, enabledBuiltins []string, system, personaName string) string {
	resolvedSystem := system
	if resolvedSystem == "" {
		p, _ := persona.Get(personaName)
		resolvedSystem = p.System
	}
	resolvedSystem = resolvedSystem + "\n\n" + persona.ToolUseBlock()
	resolvedSystem = resolvedSystem + "\n\n" + persona.CompletingTasksBlock()
	resolvedSystem = resolvedSystem + "\n\n" + persona.PlatformBlock()
	src := memory.Sources{ProjectRoot: cwd, DataDir: cfg.DataDir}
	if ctxFiles := src.LoadContext(); ctxFiles != "" {
		resolvedSystem = resolvedSystem + "\n\n" + ctxFiles
	}
	if mem := src.Load(); mem != "" {
		resolvedSystem = resolvedSystem + "\n\n" + mem
	}
	// Advertise available skills so the model can discover and load them on
	// demand. builtin.Register wires the `skill` tool into the registry, but
	// without this <skills_available> index the model is never told the skills
	// exist and never calls it.
	if sk := skills.BuildIndex(cwd, cfg.DataDir, enabledBuiltins); sk != "" {
		resolvedSystem = resolvedSystem + "\n\n" + sk
	}
	// Inject the cached repository map when present (built via `aegis index`).
	if rm, fresh, _ := repomap.Load(cwd, repoMapCachePath(cwd), repomap.Options{}); rm != "" && fresh {
		if block := repomap.Block(rm); block != "" {
			resolvedSystem = resolvedSystem + "\n\n" + block
		}
	}
	return resolvedSystem
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

// continuePrompt is the drive-to-completion continuation turn (P38.2): it names
// the files still carrying `<!-- PENDING -->` markers and tells the model to
// resume in dependency order without pausing to ask, matching the resume
// contract in the threat-modeling skill's SKILL.md §4.2. Only called with a
// non-empty list (the drive loop stops when no markers remain).
func continuePrompt(pending []string) string {
	return "Continue — the task is not finished. These files still contain `<!-- PENDING: … -->` markers and must be completed:\n- " +
		strings.Join(pending, "\n- ") +
		"\n\nResume from the first unfinished file in dependency order and keep working until NO `<!-- PENDING` marker remains in any file. Each marker is section-keyed (`<!-- PENDING: <section> -->`): edit that exact marker one at a time — never a bare `<!-- PENDING -->` and never `replace_all` on a marker, which would overwrite every section at once. This is a non-interactive run: do not stop to ask whether to proceed, and do not return a partial result."
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
		if err != nil || d.IsDir() {
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
		if err != nil || d.IsDir() {
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
