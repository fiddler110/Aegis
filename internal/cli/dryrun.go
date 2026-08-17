package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
	"github.com/spf13/cobra"
)

// profileLabel names the active prompt profile for the dry-run report, so the
// tool counts below are read against the profile that produced them rather than
// mistaken for the only surface Aegis has.
func profileLabel(local bool) string {
	if local {
		return "local"
	}
	return "default"
}

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
			//
			// P62.10: this command's whole job is "preview what this session will
			// send without calling the model", so it has to register the surface
			// the session actually registers. It passed no LocalProfile, so
			// against a local model it printed a tool list the session would not
			// use — 1,318 schema tokens' worth of tools the daemon defers,
			// security_scan alone 818. Fixing an instrument that reports the wrong
			// answer is P62.4's lesson: this is the operator-facing report for
			// exactly the question P62.6 and P62.9 were measuring.
			//
			// Deferred tools are printed separately rather than dropped. Under the
			// local profile most of the inventory moves there, and a shorter list
			// with no explanation would read as "these tools are gone" when they
			// are one tool_search call away.
			local := cfg.Provider.LocalPromptProfile()
			fmt.Fprintf(out, "\n=== Registered Tools (%s prompt profile) ===\n", profileLabel(local))
			reg := tool.NewRegistry()
			// LocalProfile and the rest of the config-derived option set come
			// from enginecfg.BuiltinOptions (P66.13/QUAL-06), so this preview
			// reports the tool surface a real run would get rather than a
			// separately-maintained approximation of it.
			//
			// dry-run builds no engine and executes nothing, which is why it
			// needs no permission gate — the P66.13 finding that it "has no
			// gate at all" is true and harmless. TestEveryEngineCallSiteDecidesItsGate
			// scans engine.New sites for exactly that reason.
			if err := builtin.Register(reg, enginecfg.BuiltinOptions(cfg, cwd)); err != nil {
				fmt.Fprintf(out, "(error registering tools: %v)\n", err)
			} else {
				schemas := reg.Schemas()
				for _, s := range schemas {
					fmt.Fprintf(out, "  %s\n", s.Name)
				}
				fmt.Fprintf(out, "(%d exposed tools)\n", len(schemas))
				if deferred := reg.Deferred(); len(deferred) > 0 {
					fmt.Fprintln(out, "\n=== Deferred Tools (advertised as one line; loaded on demand via tool_search) ===")
					for _, d := range deferred {
						fmt.Fprintf(out, "  %s\n", d.Name)
					}
					fmt.Fprintf(out, "(%d deferred tools)\n", len(deferred))
				}
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
