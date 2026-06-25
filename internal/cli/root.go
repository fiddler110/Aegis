// Package cli wires the Aegis command-line interface.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/logging"
	"github.com/scottymacleod/aegis/internal/server"
	"github.com/scottymacleod/aegis/internal/tui"
	"github.com/spf13/cobra"
)

// Version is the Aegis version, overridable at build time via -ldflags.
var Version = "0.0.1-dev"

// Execute builds the root command tree and runs it.
func Execute() error {
	root := newRootCmd()
	return root.Execute()
}

func newRootCmd() *cobra.Command {
	var (
		mode    string
		resume  string
		persona string
	)

	cmd := &cobra.Command{
		Use:           "aegis",
		Short:         "Aegis — AI agent harness for security, architecture, research, and development",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cl := client.New(cfg.Server.Addr).WithTokenFile(cfg.AuthTokenPath())

			// Check whether a daemon is already running.
			healthCtx, healthCancel := context.WithTimeout(cmd.Context(), 2*time.Second)
			healthErr := cl.Health(healthCtx)
			healthCancel()

			if healthErr != nil {
				// No daemon reachable — start one embedded in this process.
				// The returned cancel shuts it down when the TUI exits.
				stopDaemon, startErr := startEmbeddedDaemon(cfg)
				if startErr != nil {
					return fmt.Errorf("start daemon: %w", startErr)
				}
				defer stopDaemon()

				if !waitForDaemon(cl, 10*time.Second) {
					return fmt.Errorf("daemon at %s did not become ready within 10 s", cfg.Server.Addr)
				}
			}

			resolvedMode := cfg.Permission.Mode
			if mode != "" {
				resolvedMode = mode
			}

			sessionID := resume
			if sessionID == "" {
				meta, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{
					Mode:    resolvedMode,
					Persona: persona,
				})
				if err != nil {
					return err
				}
				sessionID = meta.ID
				resolvedMode = meta.Mode
			} else {
				sess, err := cl.GetSession(context.Background(), sessionID)
				if err != nil {
					return err
				}
				resolvedMode = sess.Mode
			}

			return tui.Run(tui.Config{
				Client:    cl,
				SessionID: sessionID,
				Mode:      resolvedMode,
				Model:     cfg.Provider.Model,
			})
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "permission mode: plan (read-only) or build")
	cmd.Flags().StringVar(&resume, "resume", "", "resume an existing session by id")
	cmd.Flags().StringVar(&persona, "persona", "", "persona for new sessions (e.g. general, security, developer, security-architect, sre, cloud-architect; see README for full list)")

	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newDryRunCmd())
	cmd.AddCommand(newChatCmd())
	cmd.AddCommand(newSessionsCmd())
	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newDiagramCmd())
	cmd.AddCommand(newWorkerCmd())
	return cmd
}

// startEmbeddedDaemon starts the Aegis daemon in-process. It returns a cancel
// function that stops the daemon; call it (e.g. via defer) when the TUI exits.
// Logs are written only to the configured log file, not stderr, so they don't
// bleed into the TUI.
func startEmbeddedDaemon(cfg *config.Config) (stop func(), err error) {
	if err := cfg.EnsureDataDir(); err != nil {
		return nil, fmt.Errorf("ensure data dir: %w", err)
	}
	logger, closer, err := logging.New(logging.Options{
		Level: cfg.LogLevel,
		Path:  cfg.LogPath(),
		// ToStderr intentionally false — keep daemon logs out of the TUI.
	})
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	srv, err := server.New(cfg, logger)
	if err != nil {
		closer.Close()
		return nil, fmt.Errorf("create server: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer closer.Close()
		_ = srv.ListenAndServe(ctx) // returns context.Canceled on clean shutdown
	}()

	return cancel, nil
}

// waitForDaemon polls the daemon health endpoint until it responds or the
// timeout expires. Returns true when the daemon is ready.
func waitForDaemon(cl *client.Client, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err := cl.Health(ctx)
		cancel()
		if err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
