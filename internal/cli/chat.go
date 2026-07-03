package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/providerfactory"
	"github.com/fiddler110/aegis/internal/repomap"
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

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			reg := tool.NewRegistry()
			if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL}); err != nil {
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
			})
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// Build the system prompt: explicit --system wins, then --persona, then general.
			resolvedSystem := system
			if resolvedSystem == "" {
				p, _ := persona.Get(personaName)
				resolvedSystem = p.System
			}
			// Append shared blocks (applied to all personas in the server path via
			// effectiveSystem; mirrored here so the CLI path is equivalent).
			resolvedSystem = resolvedSystem + "\n\n" + persona.ToolUseBlock()
			resolvedSystem = resolvedSystem + "\n\n" + persona.CompletingTasksBlock()
			resolvedSystem = resolvedSystem + "\n\n" + persona.PlatformBlock()
			// Append memory and context files, matching the daemon's effectiveSystem.
			src := memory.Sources{ProjectRoot: cwd, DataDir: cfg.DataDir}
			if ctxFiles := src.LoadContext(); ctxFiles != "" {
				resolvedSystem = resolvedSystem + "\n\n" + ctxFiles
			}
			if mem := src.Load(); mem != "" {
				resolvedSystem = resolvedSystem + "\n\n" + mem
			}
			// Inject the cached repository map when present (built via `aegis index`).
			if rm, fresh, _ := repomap.Load(cwd, repoMapCachePath(cwd), repomap.Options{}); rm != "" && fresh {
				if block := repomap.Block(rm); block != "" {
					resolvedSystem = resolvedSystem + "\n\n" + block
				}
			}

			conv := &engine.Conversation{System: resolvedSystem}
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
			runErr := eng.Run(ctx, conv, func(ev engine.Event) {
				switch format {
				case outputStreamJSON:
					emitStreamEvent(out, ev)
				case outputJSON:
					if ev.Kind == engine.KindText {
						answer.WriteString(ev.Text)
					}
					if ev.Kind == engine.KindToolCall {
						toolCalls++
					}
				default: // text
					switch ev.Kind {
					case engine.KindText:
						fmt.Fprint(out, ev.Text)
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
	Type         string  `json:"type,omitempty"` // "result" in stream-json trailer
	Answer       string  `json:"answer,omitempty"`
	CostUSD      float64 `json:"cost_usd"`
	Turns        int     `json:"turns"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	ToolCalls    int     `json:"tool_calls"`
	Error        string  `json:"error,omitempty"`
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
	case engine.KindText, engine.KindThinking:
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
