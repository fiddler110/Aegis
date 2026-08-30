package cli

import (
	"os"
	"os/signal"

	"github.com/fiddler110/aegis/internal/acp"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/logging"
	"github.com/spf13/cobra"
)

func newACPCmd() *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "acp",
		Short: "Speak the Agent Client Protocol over stdio (for Zed, Neovim, and other ACP editors)",
		Long: "Run Aegis as an ACP (Agent Client Protocol) agent. The editor launches this " +
			"command as a subprocess and drives it over stdin/stdout with JSON-RPC. Protocol " +
			"frames use stdout exclusively; logs go to the configured log file.\n\n" +
			"Authentication is always required: set AEGIS_ACP_TOKEN in this process's environment " +
			"to pin the shared secret yourself, or leave it unset and Aegis generates one on " +
			"startup, writing it to <data_dir>/acp.token (owner-only permissions) for your " +
			"editor to read and send back via the \"authenticate\" request before session/new " +
			"or session/prompt.",
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
				Level:        cfg.LogLevel,
				Path:         cfg.LogPath(),
				MaxSizeBytes: cfg.LogMaxSizeBytes(),
				MaxBackups:   cfg.Log.MaxBackups,
			})
			if err != nil {
				return err
			}
			defer closer.Close()

			// Reuse a running daemon if present; otherwise start one embedded
			// in this process and stop it when the editor disconnects. cl
			// lives for the whole ACP session — agent.Serve blocks until the
			// editor disconnects — and cleanup scrubs its token once that
			// returns (FIND-33/P24.21).
			cl, _, cleanup, err := connectOrStartDaemon(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			resolvedMode := cfg.Permission.Mode
			if mode != "" {
				resolvedMode = mode
			}
			if resolvedMode == "" {
				resolvedMode = "build"
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			// A shared secret is always required before session/new or
			// session/prompt is allowed (FIND-02/P24.2, hardened by
			// FIND-06/P27.4): if the trusted parent process that spawns this
			// subprocess (the editor) set AEGIS_ACP_TOKEN, that value wins;
			// otherwise a fresh token is generated and written to
			// cfg.ACPTokenPath() for the editor to read after spawning us.
			// This closes the previous unauthenticated default — any local
			// process able to write to this subprocess's stdin could
			// otherwise drive full agent turns.
			authToken, err := resolveStdioAuthToken("AEGIS_ACP_TOKEN", cfg.ACPTokenPath(), "acp", logger)
			if err != nil {
				return err
			}

			logger.Info("acp agent starting", "mode", resolvedMode, "addr", cfg.Server.Addr)
			agent := acp.NewAgent(cl, resolvedMode, logger, authToken)
			return agent.Serve(ctx, os.Stdin, os.Stdout)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "permission mode for sessions: plan, build (default), or auto")
	return cmd
}
