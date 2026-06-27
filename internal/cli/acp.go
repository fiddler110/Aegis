package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/scottymacleod/aegis/internal/acp"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/logging"
	"github.com/spf13/cobra"
)

func newACPCmd() *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "acp",
		Short: "Speak the Agent Client Protocol over stdio (for Zed, Neovim, and other ACP editors)",
		Long: "Run Aegis as an ACP (Agent Client Protocol) agent. The editor launches this " +
			"command as a subprocess and drives it over stdin/stdout with JSON-RPC. Protocol " +
			"frames use stdout exclusively; logs go to the configured log file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.EnsureDataDir(); err != nil {
				return err
			}

			// Logs must never touch stdout, which carries the ACP protocol. Write
			// only to the log file (ToStderr stays false).
			logger, closer, err := logging.New(logging.Options{
				Level: cfg.LogLevel,
				Path:  cfg.LogPath(),
			})
			if err != nil {
				return err
			}
			defer closer.Close()

			cl := client.New(cfg.Server.Addr).WithTokenFile(cfg.AuthTokenPath())

			// Reuse a running daemon if present; otherwise start one embedded in
			// this process and stop it when the editor disconnects.
			healthCtx, healthCancel := context.WithTimeout(cmd.Context(), 2*time.Second)
			healthErr := cl.Health(healthCtx)
			healthCancel()
			if healthErr != nil {
				stopDaemon, startErr := startEmbeddedDaemon(cfg)
				if startErr != nil {
					return fmt.Errorf("start daemon: %w", startErr)
				}
				defer stopDaemon()
				if !waitForDaemon(cl, 10*time.Second) {
					return fmt.Errorf("daemon at %s did not become ready within 10s", cfg.Server.Addr)
				}
				cl = client.New(cfg.Server.Addr).WithTokenFile(cfg.AuthTokenPath())
			}

			resolvedMode := cfg.Permission.Mode
			if mode != "" {
				resolvedMode = mode
			}
			if resolvedMode == "" {
				resolvedMode = "build"
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			logger.Info("acp agent starting", "mode", resolvedMode, "addr", cfg.Server.Addr)
			agent := acp.NewAgent(cl, resolvedMode, logger)
			return agent.Serve(ctx, os.Stdin, os.Stdout)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "permission mode for sessions: plan, build (default), or auto")
	return cmd
}
