package cli

import (
	"context"
	"os"
	"os/signal"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/logging"
	"github.com/scottymacleod/aegis/internal/server"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Aegis daemon (owns sessions, the agent loop, and tools)",
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

			srv, err := server.New(cfg, logger)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			return srv.ListenAndServe(ctx)
		},
	}

	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in foreground and mirror logs to stderr")
	return cmd
}
