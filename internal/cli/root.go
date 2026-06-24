// Package cli wires the harness command-line interface.
package cli

import (
	"fmt"

	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/spf13/cobra"
)

// Version is the harness version, overridable at build time via -ldflags.
var Version = "0.0.1-dev"

// Execute builds the root command tree and runs it.
func Execute() error {
	root := newRootCmd()
	return root.Execute()
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "harness",
		Short:         "A personal agent harness for research, documentation, coding, and security architecture",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// With no subcommand the root will eventually launch the TUI client.
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"agentharness %s\n  data dir: %s\n  provider: %s (%s)\n  daemon:   %s\n  mode:     %s\n\nTUI client not implemented yet (Phase 4). Run `harness serve` to start the daemon.\n",
				Version, cfg.DataDir, cfg.Provider.Default, cfg.Provider.Model, cfg.Server.Addr, cfg.Permission.Mode)
			return nil
		},
	}

	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newChatCmd())
	return cmd
}
