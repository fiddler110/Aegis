package cli

import (
	"fmt"

	"github.com/scottymacleod/agentharness/internal/config"
	"github.com/scottymacleod/agentharness/internal/logging"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the harness daemon (owns sessions, the agent loop, and tools)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.EnsureDataDir(); err != nil {
				return err
			}

			logger, closer, err := logging.New(logging.Options{
				Level:    cfg.LogLevel,
				Path:     cfg.LogPath(),
				ToStderr: foreground,
			})
			if err != nil {
				return err
			}
			defer closer.Close()

			logger.Info("daemon starting", "addr", cfg.Server.Addr, "data_dir", cfg.DataDir)
			fmt.Fprintf(cmd.OutOrStdout(), "daemon configured to listen on %s (server impl arrives in Phase 4)\n", cfg.Server.Addr)
			// The HTTP server and agent engine are wired up in later phases.
			return nil
		},
	}

	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in foreground and mirror logs to stderr")
	return cmd
}
