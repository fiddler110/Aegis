package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/scottymacleod/agentharness/internal/engine"
	"github.com/scottymacleod/agentharness/internal/provider"
	"github.com/scottymacleod/agentharness/internal/provider/anthropic"
	"github.com/scottymacleod/agentharness/internal/tool"
	"github.com/spf13/cobra"
)

func newChatCmd() *cobra.Command {
	var system string

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Run a one-shot chat turn through the agent engine (no TUI)",
		Long:  "Sends a single prompt to the model and streams the response. Reads the prompt from arguments, or from stdin if none are given.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			prompt := strings.TrimSpace(strings.Join(args, " "))
			if prompt == "" {
				data, _ := io.ReadAll(cmd.InOrStdin())
				prompt = strings.TrimSpace(string(data))
			}
			if prompt == "" {
				return fmt.Errorf("no prompt provided (pass as arguments or via stdin)")
			}

			adapter, err := buildAdapter(cfg)
			if err != nil {
				return err
			}

			eng, err := engine.New(engine.Options{
				Adapter:   adapter,
				Tools:     tool.NewRegistry(), // builtins land in Phase 3
				Model:     cfg.Provider.Model,
				MaxTokens: cfg.Provider.MaxTokens,
			})
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			conv := &engine.Conversation{System: system}
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
			return runErr
		},
	}

	cmd.Flags().StringVar(&system, "system", "", "system prompt")
	return cmd
}

// buildAdapter constructs the provider adapter selected by config.
func buildAdapter(cfg *config.Config) (provider.Adapter, error) {
	switch cfg.Provider.Default {
	case "anthropic":
		if cfg.Provider.APIKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		return anthropic.New(cfg.Provider.APIKey, anthropic.WithBaseURL(cfg.Provider.BaseURL)), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q (only \"anthropic\" is implemented)", cfg.Provider.Default)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
