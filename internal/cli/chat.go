package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
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
	)

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Run a one-shot chat turn through the agent engine (no TUI)",
		Long:  "Sends a single prompt to the model and streams the response. Reads the prompt from arguments, or from stdin if none are given.",
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
			reg := tool.NewRegistry()
			if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL, BuiltinSkills: cfg.Skills.BuiltinEnabled, SecurityScan: security.OptionsFromConfig(cfg.Security), DASTAllowedTargets: cfg.Security.DAST.AllowedTargets, DASTAllowActive: cfg.Security.DAST.AllowActive}); err != nil {
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

			conv := &engine.Conversation{System: buildChatSystem(cfg, cwd, system, personaName)}
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
			// P35.8: bracket the engine run with boundary markers. A silent
			// mid-run disappearance then shows up as a "run starting" line with
			// no matching "run finished" — exactly the diagnostic signal that
			// was missing when a live chat turn vanished without a trace.
			logger.Info("chat: run starting", "prompt_bytes", len(prompt))
			runErr := eng.Run(ctx, conv, func(ev engine.Event) {
				// Counted for every format: both json and stream-json report
				// this in their trailer, so counting it per-branch left
				// stream-json's summary permanently reading zero.
				if ev.Kind == engine.KindToolCall {
					toolCalls++
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
			})
			logger.Info("chat: run finished", "err", errString(runErr), "tool_calls", toolCalls)

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
func buildChatSystem(cfg *config.Config, cwd, system, personaName string) string {
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
	if sk := skills.BuildIndex(cwd, cfg.DataDir, cfg.Skills.BuiltinEnabled); sk != "" {
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
