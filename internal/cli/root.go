// Package cli wires the Aegis command-line interface.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/config"
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
		// With no subcommand, launch the TUI client against the daemon.
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cl := client.New(cfg.Server.Addr).WithTokenFile(cfg.AuthTokenPath())

			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			if err := cl.Health(ctx); err != nil {
				return fmt.Errorf("cannot reach daemon at %s: %w\nStart it first with: aegis serve", cfg.Server.Addr, err)
			}

			resolvedMode := cfg.Permission.Mode
			if mode != "" {
				resolvedMode = mode
			}

			sessionID := resume
			if sessionID == "" {
				meta, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{Mode: resolvedMode, Persona: persona})
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
