package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/logging"
	"github.com/fiddler110/aegis/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newMCPServeCmd() *cobra.Command {
	var (
		mode        string
		autoApprove bool
	)

	cmd := &cobra.Command{
		Use:   "mcp-serve",
		Short: "Expose this Aegis daemon as an MCP server over stdio",
		Long: "Run Aegis as a Model Context Protocol (MCP) server. The calling harness (Claude " +
			"Code, Codex, an editor, ...) launches this command as a subprocess and drives it over " +
			"stdin/stdout with JSON-RPC. Aegis sessions are exposed as tools (aegis_prompt, " +
			"aegis_new_session, aegis_list_sessions) — the reverse direction of the `mcp:` client " +
			"config, which lets Aegis call out to external MCP servers.\n\n" +
			"New sessions default to plan mode (read-only) unless a caller explicitly asks for " +
			"build/auto. A tool call needing approval in that higher mode is denied by default " +
			"(no human is in the loop to ask) — pass --auto-approve, or set mcp_server.auto_approve " +
			"in config, to allow it instead.\n\n" +
			"Authentication is always required: set AEGIS_MCP_TOKEN in this process's environment " +
			"to pin the shared secret yourself, or leave it unset and Aegis generates one on " +
			"startup, writing it to <data_dir>/mcp.token (owner-only permissions) for your " +
			"harness to read and send back via the \"aegis/authenticate\" request before " +
			"tools/call.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.EnsureDataDir(); err != nil {
				return err
			}

			// Logs must never touch stdout, which carries the MCP protocol frames.
			logger, closer, err := logging.New(logging.Options{
				Level: cfg.LogLevel,
				Path:  cfg.LogPath(),
			})
			if err != nil {
				return err
			}
			defer closer.Close()

			cl := client.NewFromConfig(cfg)

			// Reuse a running daemon if present; otherwise start one embedded in
			// this process and stop it when the client disconnects.
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
				cl = client.NewFromConfig(cfg)
			}
			// cl (the final value, after any reassignment above) lives for
			// the whole MCP session — srv.Serve blocks until the calling
			// harness disconnects — so scrub its token once that returns,
			// right before this command's process exits (FIND-33/P24.21).
			// Placed after the reassignment so the defer captures the
			// client that actually gets used, not the discarded
			// pre-embedded-daemon one.
			defer cl.Zero()

			resolvedMode := mode
			if resolvedMode == "" {
				resolvedMode = cfg.MCPServer.DefaultMode
			}
			resolvedAutoApprove := cfg.MCPServer.AutoApprove || autoApprove

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			// A shared secret is always required before tools/call is allowed
			// (FIND-02/P24.2, hardened by FIND-06/P27.4): if the trusted
			// parent process that spawns this subprocess set AEGIS_MCP_TOKEN,
			// that value wins; otherwise a fresh token is generated and
			// written to cfg.MCPTokenPath() for the launching harness to read
			// after spawning us. This closes the previous unauthenticated
			// default — any local process able to write to this subprocess's
			// stdin could otherwise drive full agent turns.
			authToken, err := resolveStdioAuthToken("AEGIS_MCP_TOKEN", cfg.MCPTokenPath(), "mcp-serve", logger)
			if err != nil {
				return err
			}

			logger.Info("mcp-serve starting", "default_mode", resolvedMode, "auto_approve", resolvedAutoApprove, "addr", cfg.Server.Addr)
			srv := mcpserver.NewServer(cl, mcpserver.Options{
				DefaultMode: resolvedMode,
				AutoApprove: resolvedAutoApprove,
				Version:     Version,
				AuthToken:   authToken,
			}, logger)
			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "default permission mode for new sessions: plan (default), build, or auto")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve tool calls that would otherwise need interactive approval")
	return cmd
}
