package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/sandbox"
	"github.com/spf13/cobra"
)

func newSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Inspect and configure the shell-execution sandbox",
		Long: "Detect available container runtimes (Docker, Podman, WSL containers, Apple Containers), " +
			"see which one Aegis will use, switch backends, and test execution.",
	}
	cmd.AddCommand(newSandboxDetectCmd())
	cmd.AddCommand(newSandboxStatusCmd())
	cmd.AddCommand(newSandboxUseCmd())
	cmd.AddCommand(newSandboxTestCmd())
	return cmd
}

// detectAndRender probes runtimes and writes a table to w, marking the runtime
// that `auto` selection would pick (given priority).
func detectAndRender(ctx context.Context, w io.Writer, priority []sandbox.ContainerRuntime) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	fmt.Fprintln(w, sandbox.Report(ctx, priority))
}

func newSandboxDetectCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "detect",
		Aliases: []string{"list"},
		Short:   "Probe the host for available container runtimes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			detectAndRender(cmd.Context(), cmd.OutOrStdout(), sandbox.ParseRuntimes(cfg.Sandbox.Priority))
			return nil
		},
	}
}

func newSandboxStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the configured sandbox backend and what is detected",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			s := cfg.Sandbox
			backend := s.Backend
			if backend == "" {
				backend = "local"
			}
			fmt.Fprintf(out, "Configured backend: %s\n", backend)
			if s.Runtime != "" {
				fmt.Fprintf(out, "Forced runtime:     %s\n", s.Runtime)
			}
			if len(s.Priority) > 0 {
				fmt.Fprintf(out, "Auto priority:      %s\n", strings.Join(s.Priority, ", "))
			}
			image := s.Image
			if image == "" {
				image = "ubuntu:22.04"
			}
			fmt.Fprintf(out, "Image:              %s\n", image)
			fmt.Fprintf(out, "Network:            %t\n\n", s.Network)

			if backend == "local" {
				fmt.Fprintln(out, "Commands run directly on the host. Switch with: aegis sandbox use auto")
				return nil
			}
			detectAndRender(cmd.Context(), out, sandbox.ParseRuntimes(s.Priority))
			return nil
		},
	}
}

func newSandboxUseCmd() *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "use <local|auto|docker|podman|wslc|container>",
		Short: "Set the sandbox backend and persist it to config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			// Preserve existing image/network/priority; only change backend/runtime.
			patch := config.SandboxPatch{
				Image:    cfg.Sandbox.Image,
				Network:  cfg.Sandbox.Network,
				Priority: cfg.Sandbox.Priority,
			}
			target := strings.ToLower(strings.TrimSpace(args[0]))
			switch target {
			case "local", "auto":
				patch.Backend = target
				patch.Runtime = ""
			case "wsl", "wslc":
				patch.Backend = "container"
				patch.Runtime = "wslc"
			case "docker", "podman", "container":
				patch.Backend = "container"
				patch.Runtime = target
			default:
				return fmt.Errorf("unknown sandbox target %q (want local, auto, docker, podman, wslc, or container)", args[0])
			}

			write := config.PatchGlobalSandbox
			scope := "global"
			if project {
				write = config.PatchProjectSandbox
				scope = "project (.aegis/config.yaml)"
			}
			if err := write(patch); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sandbox backend set to %q (%s config). Restart Aegis to apply.\n", target, scope)
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write to the project .aegis/config.yaml instead of the global config")
	return cmd
}

func newSandboxTestCmd() *cobra.Command {
	var image string
	var network bool
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run a command in the configured sandbox to verify it works",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if image == "" {
				image = cfg.Sandbox.Image
			}

			backend := cfg.Sandbox.Backend
			if backend == "local" || backend == "" {
				fmt.Fprintln(out, "Configured backend is 'local' — commands run on the host (no container to test).")
				fmt.Fprintln(out, "Enable isolation with: aegis sandbox use auto")
				return nil
			}

			opts := sandbox.ContainerOpts{
				Image:    image,
				Network:  network || cfg.Sandbox.Network,
				Priority: sandbox.ParseRuntimes(cfg.Sandbox.Priority),
			}
			if backend == "container" {
				opts.Prefer = sandbox.ContainerRuntime(cfg.Sandbox.Runtime)
			}
			be, err := sandbox.NewContainerBackend(opts)
			if err != nil {
				return fmt.Errorf("sandbox unavailable: %w", err)
			}
			fmt.Fprintf(out, "Using %s (image %s)…\n", be.Name(), image)

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			res, err := be.Exec(ctx, "uname -a 2>/dev/null || echo no-uname; echo aegis-sandbox-ok", sandbox.ExecOpts{})
			if err != nil {
				return fmt.Errorf("sandbox test failed: %w", err)
			}
			fmt.Fprint(out, res)
			if !strings.Contains(res, "aegis-sandbox-ok") {
				return fmt.Errorf("sandbox test did not complete cleanly")
			}
			fmt.Fprintln(out, "\nSandbox OK.")
			return nil
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "override the test image")
	cmd.Flags().BoolVar(&network, "network", false, "allow network access for the test")
	return cmd
}
