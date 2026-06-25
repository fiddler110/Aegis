package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/scottymacleod/aegis/internal/commands"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/cost"
	"github.com/scottymacleod/aegis/internal/engine"
	"github.com/scottymacleod/aegis/internal/memory"
	"github.com/scottymacleod/aegis/internal/permission"
	"github.com/scottymacleod/aegis/internal/persona"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/providerfactory"
	"github.com/scottymacleod/aegis/internal/tool"
	"github.com/scottymacleod/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
)

func newChatCmd() *cobra.Command {
	var (
		system      string
		mode        string
		personaName string
		autoApprove bool
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

			adapter, err := providerfactory.Build(cfg)
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
			// Append memory and context files, matching the daemon's effectiveSystem.
			src := memory.Sources{ProjectRoot: cwd, DataDir: cfg.DataDir}
			if ctxFiles := src.LoadContext(); ctxFiles != "" {
				resolvedSystem = resolvedSystem + "\n\n" + ctxFiles
			}
			if mem := src.Load(); mem != "" {
				resolvedSystem = resolvedSystem + "\n\n" + mem
			}

			conv := &engine.Conversation{System: resolvedSystem}
			conv.Append(provider.Message{
				Role:    provider.RoleUser,
				Content: []provider.Block{provider.TextBlock{Text: prompt}},
			})

			out := cmd.OutOrStdout()
			runErr := eng.Run(ctx, conv, func(ev engine.Event) {
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
			})
			if snap := tracker.Snapshot(); snap.TotalUSD > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "\n[cost: $%.4f over %d turn(s), %d in / %d out tokens]\n",
					snap.TotalUSD, snap.Turns, snap.Usage.InputTokens, snap.Usage.OutputTokens)
			}
			return runErr
		},
	}

	cmd.Flags().StringVar(&system, "system", "", "system prompt (overrides --persona)")
	cmd.Flags().StringVar(&mode, "mode", "", "permission mode: plan (read-only) or build (default from config)")
	cmd.Flags().StringVar(&personaName, "persona", "", "persona to use (e.g. security, developer, sre)")
	cmd.Flags().BoolVar(&autoApprove, "yes", false, "auto-approve tool calls that would otherwise require confirmation")
	return cmd
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
