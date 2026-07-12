// Package cli wires the Aegis command-line interface.
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/logging"
	"github.com/fiddler110/aegis/internal/server"
	"github.com/fiddler110/aegis/internal/tui"
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
		mode        string
		resume      string
		persona     string
		firstInit   bool
		initProject bool
	)

	cmd := &cobra.Command{
		Use:   "aegis",
		Short: "Aegis — AI agent harness for security, architecture, research, and development",
		Long: `Aegis — AI agent harness for security, architecture, research, and development.

Aegis is a daemon + client harness: a single daemon owns sessions, the model adapter, and the
tool registry over a local HTTP API, and any of several clients (this interactive terminal UI,
the web UI, ACP editors, MCP-speaking harnesses) can drive it.

Quick start:
  aegis --first-init              create a global config (Ollama local models by default)
  export OPENAI_API_KEY=ollama    # or ANTHROPIC_API_KEY / OPENAI_API_KEY for a cloud provider
  aegis                            start an interactive session

Running "aegis" with no subcommand starts an interactive terminal session, auto-starting an
embedded daemon if none is already running (see "aegis serve" to run just the daemon). Once
inside a session, type /help to see slash commands (persona/mode switching, security scanning,
memory, checkpoints, and more) — those are separate from the subcommands listed below.

Other ways to run Aegis:
  aegis chat "prompt"              one-shot, non-interactive turn (scriptable, no TUI)
  aegis ui                          open the browser-based web UI
  aegis acp / aegis mcp-serve       speak ACP or MCP over stdio for editor/harness integration

Use "aegis <command> --help" for details on any command below.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if firstInit {
				return runFirstInit()
			}
			if initProject {
				return runProjectInit()
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cl := client.NewFromConfig(cfg)

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
				// The embedded daemon wrote a fresh token to disk; re-read it
				// so subsequent authenticated requests use the correct value.
				cl = client.NewFromConfig(cfg)
			}

			warnSandboxFallback(cl)

			resolvedMode := cfg.Permission.Mode
			if mode != "" {
				resolvedMode = mode
			}

			// An explicit --persona always wins; otherwise fall back to the
			// project's (or user's) configured default_persona, then finally
			// to the daemon's own "" -> general fallback.
			resolvedPersona := persona
			if resolvedPersona == "" {
				resolvedPersona = cfg.DefaultPersona
			}

			cwd, _ := os.Getwd()

			sessionID := resume
			if sessionID == "" {
				meta, err := cl.CreateSession(context.Background(), api.CreateSessionRequest{
					Mode:    resolvedMode,
					Persona: resolvedPersona,
					Workdir: cwd,
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
				// Sessions created before P25.1 (or without a client cwd)
				// have no persisted Workdir; keep the local cwd in that case
				// so the welcome banner and attach-path resolution still
				// have a sensible default.
				if sess.Workdir != "" {
					cwd = sess.Workdir
				}
			}

			runErr := tui.Run(tui.Config{
				Client:         cl,
				SessionID:      sessionID,
				Mode:           resolvedMode,
				Model:          cfg.Provider.Model,
				WorkDir:        cwd,
				HumorMode:      cfg.TUI.HumorMode,
				Theme:          cfg.TUI.Theme,
				Notifications:  cfg.TUI.Notifications,
				ImageRendering: cfg.TUI.ImageRendering,
				Keybindings:    cfg.TUI.Keybindings,
			})
			unloadOllamaModel(cfg)
			return runErr
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "permission mode: plan (read-only) or build")
	cmd.Flags().StringVar(&resume, "resume", "", "resume an existing session by id")
	cmd.Flags().StringVar(&persona, "persona", "", "persona for new sessions (e.g. general, security, developer, security-architect, sre, cloud-architect; see README for full list). Falls back to config's default_persona, then general (see: aegis persona use)")
	cmd.Flags().BoolVar(&firstInit, "first-init", false, "create the global config file with a full provider template (Ollama active by default)")
	cmd.Flags().BoolVar(&initProject, "init", false, "create a project-level .aegis/config.yaml override in the current directory")

	const (
		groupRun     = "run"
		groupSession = "session"
		groupSecure  = "security"
		groupConfig  = "config"
		groupProject = "project"
	)
	cmd.AddGroup(
		&cobra.Group{ID: groupRun, Title: "Run the agent:"},
		&cobra.Group{ID: groupSession, Title: "Sessions & runs:"},
		&cobra.Group{ID: groupSecure, Title: "Security scanning:"},
		&cobra.Group{ID: groupConfig, Title: "Configuration & setup:"},
		&cobra.Group{ID: groupProject, Title: "Project tools:"},
	)
	cmd.SetHelpCommandGroupID(groupConfig)
	cmd.SetCompletionCommandGroupID(groupConfig)

	group := func(c *cobra.Command, id string) *cobra.Command { c.GroupID = id; return c }

	// Run the agent — different interfaces onto the same daemon.
	cmd.AddCommand(group(newServeCmd(), groupRun))
	cmd.AddCommand(group(newChatCmd(), groupRun))
	cmd.AddCommand(group(newUICmd(), groupRun))
	cmd.AddCommand(group(newACPCmd(), groupRun))
	cmd.AddCommand(group(newMCPServeCmd(), groupRun))

	// Sessions & runs — lifecycle and multi-session management.
	cmd.AddCommand(group(newSessionsCmd(), groupSession))
	cmd.AddCommand(group(newBGCmd(), groupSession)) // P3.2: background session management
	cmd.AddCommand(group(newRunsCmd(), groupSession))
	cmd.AddCommand(group(newParallelCmd(), groupSession))
	cmd.AddCommand(group(newCompareCmd(), groupSession))
	cmd.AddCommand(group(newWorktreeCmd(), groupSession))

	// Security scanning.
	cmd.AddCommand(group(newScanCmd(), groupSecure))
	cmd.AddCommand(group(newSecurityCmd(), groupSecure))
	cmd.AddCommand(group(newDebateCmd(), groupSecure))

	// Configuration & setup.
	cmd.AddCommand(group(newConfigCmd(), groupConfig))
	cmd.AddCommand(group(newPersonaCmd(), groupConfig))
	cmd.AddCommand(group(newSkillsCmd(), groupConfig))
	cmd.AddCommand(group(newSandboxCmd(), groupConfig))
	cmd.AddCommand(group(newHardenCmd(), groupConfig))
	cmd.AddCommand(group(newModelsCmd(), groupConfig))
	cmd.AddCommand(group(newBundleCmd(), groupConfig))
	cmd.AddCommand(group(newDoctorCmd(), groupConfig))

	// Project tools.
	cmd.AddCommand(group(newIndexCmd(), groupProject))
	cmd.AddCommand(group(newKnowledgeCmd(), groupProject))
	cmd.AddCommand(group(newDiagramCmd(), groupProject))
	cmd.AddCommand(group(newDryRunCmd(), groupProject))

	cmd.AddCommand(newWorkerCmd()) // internal, hidden — no group needed
	return cmd
}

// ensureDaemon returns a client connected to a running daemon, starting an
// embedded one if none is reachable. The returned stop func shuts down any
// daemon this call started (a no-op when an existing daemon was reused).
func ensureDaemon(ctx context.Context, cfg *config.Config) (*client.Client, func(), error) {
	cl := client.NewFromConfig(cfg)
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	healthErr := cl.Health(hctx)
	cancel()
	if healthErr == nil {
		return cl, func() {}, nil
	}
	stop, err := startEmbeddedDaemon(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("start daemon: %w", err)
	}
	if !waitForDaemon(cl, 10*time.Second) {
		stop()
		return nil, nil, fmt.Errorf("daemon at %s did not become ready within 10s", cfg.Server.Addr)
	}
	cl = client.NewFromConfig(cfg)
	return cl, stop, nil
}

// startEmbeddedDaemon starts the Aegis daemon in-process. It returns a cancel
// function that stops the daemon; call it (e.g. via defer) when the TUI exits.
// Logs are written only to the configured log file, not stderr, so they don't
// bleed into the TUI.
func startEmbeddedDaemon(cfg *config.Config) (stop func(), err error) {
	stopOllama, err := ensureOllamaRunning(cfg)
	if err != nil {
		return nil, err
	}
	if err := resolveOllamaModel(cfg); err != nil {
		stopOllama()
		return nil, err
	}

	if err := cfg.EnsureDataDir(); err != nil {
		stopOllama()
		return nil, fmt.Errorf("ensure data dir: %w", err)
	}
	logger, closer, err := logging.New(logging.Options{
		Level: cfg.LogLevel,
		Path:  cfg.LogPath(),
		// ToStderr intentionally false — keep daemon logs out of the TUI.
	})
	if err != nil {
		stopOllama()
		return nil, fmt.Errorf("init logger: %w", err)
	}

	srv, err := server.New(cfg, logger)
	if err != nil {
		closer.Close()
		stopOllama()
		return nil, fmt.Errorf("create server: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer closer.Close()
		_ = srv.ListenAndServe(ctx) // returns context.Canceled on clean shutdown
	}()

	return func() {
		cancel()
		stopOllama()
	}, nil
}

// warnSandboxFallback prints a visible warning to stderr, before the TUI
// takes over the terminal, when the daemon reports it fell back to
// unsandboxed local execution (P7.4). Best-effort: a status-check failure is
// silently ignored since Health() already gated entry to this point.
func warnSandboxFallback(cl *client.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := cl.Status(ctx)
	if err != nil || !status.SandboxFallback {
		return
	}
	fmt.Fprintf(os.Stderr, "\n⚠ sandbox fallback: %s\n\n", status.SandboxFallbackReason)
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
