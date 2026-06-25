package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/memory"
	"github.com/scottymacleod/aegis/internal/tool"
	"github.com/scottymacleod/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
)

func newDryRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dry-run",
		Short: "Preview the resolved configuration, tools, memory, and context without calling the model",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.Provider.APIKey != "" {
				cfg.Provider.APIKey = "***redacted***"
			}

			cwd, _ := os.Getwd()
			out := cmd.OutOrStdout()

			// Config summary.
			fmt.Fprintln(out, "=== Resolved Config ===")
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			enc.Encode(cfg)

			// Tools.
			fmt.Fprintln(out, "\n=== Registered Tools ===")
			reg := tool.NewRegistry()
			if err := builtin.Register(reg, builtin.Options{Root: cwd, DataDir: cfg.DataDir, KrokiURL: cfg.Diagram.KrokiURL}); err != nil {
				fmt.Fprintf(out, "(error registering tools: %v)\n", err)
			} else {
				schemas := reg.Schemas()
				for _, s := range schemas {
					fmt.Fprintf(out, "  %s\n", s.Name)
				}
				fmt.Fprintf(out, "(%d tools)\n", len(schemas))
			}

			// Memory.
			fmt.Fprintln(out, "\n=== Memory ===")
			src := memory.Sources{ProjectRoot: cwd, DataDir: cfg.DataDir}
			mem := src.Load()
			if mem == "" {
				fmt.Fprintln(out, "(none)")
			} else {
				lines := strings.Split(mem, "\n")
				if len(lines) > 20 {
					lines = append(lines[:20], fmt.Sprintf("... (%d more lines)", len(lines)-20))
				}
				fmt.Fprintln(out, strings.Join(lines, "\n"))
			}

			// Context files.
			fmt.Fprintln(out, "\n=== Context Files ===")
			ctx := src.LoadContext()
			if ctx == "" {
				fmt.Fprintln(out, "(none)")
			} else {
				lines := strings.Split(ctx, "\n")
				if len(lines) > 20 {
					lines = append(lines[:20], fmt.Sprintf("... (%d more lines)", len(lines)-20))
				}
				fmt.Fprintln(out, strings.Join(lines, "\n"))
			}

			// Ignore patterns.
			patterns := src.LoadIgnorePatterns()
			if len(patterns) > 0 {
				fmt.Fprintln(out, "\n=== Ignore Patterns ===")
				for _, p := range patterns {
					fmt.Fprintf(out, "  %s\n", p)
				}
			}

			// MCP servers.
			if len(cfg.MCP) > 0 {
				fmt.Fprintln(out, "\n=== MCP Servers ===")
				for _, m := range cfg.MCP {
					fmt.Fprintf(out, "  %s: %s %s\n", m.Name, m.Command, strings.Join(m.Args, " "))
				}
			}

			// LSP servers.
			if len(cfg.LSP) > 0 {
				fmt.Fprintln(out, "\n=== LSP Servers ===")
				for _, l := range cfg.LSP {
					fmt.Fprintf(out, "  %s: %s %s (extensions: %s)\n", l.Name, l.Command, strings.Join(l.Args, " "), strings.Join(l.Extensions, ", "))
				}
			}

			return nil
		},
	}
}
