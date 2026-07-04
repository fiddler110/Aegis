package cli

import (
	"bufio"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/spf13/cobra"
)

// newSecurityCmd is the P11.10/P11.11 CLI surface for the scanner
// availability layer: status shows how each tool would actually run right
// now (host binary, container fallback, or unavailable and why); install
// runs a tool's guided host install after showing the operator the exact
// command and getting explicit confirmation — installing software is a
// privileged, host-modifying action, so this must never happen silently or
// non-interactively without --yes.
func newSecurityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Manage security scanner availability (semgrep, trivy, gitleaks, kubescape, hadolint, grype, dockle)",
		Long: "Inspects and provisions the scanners behind `aegis scan`/the security_scan tool. " +
			"`status` reports whether each tool will run via its host binary, a configured " +
			"container image, or not at all (with the exact reason). `install` walks through " +
			"a guided, approval-gated host install for one tool.",
	}
	cmd.AddCommand(newSecurityStatusCmd())
	cmd.AddCommand(newSecurityInstallCmd())
	cmd.AddCommand(newSecurityConfigCmd())
	return cmd
}

func newSecurityStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show how each security scanner would run right now",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			opts := security.OptionsFromConfig(cfg.Security)
			out := cmd.OutOrStdout()
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TOOL\tCATEGORY\tMETHOD\tDETAIL")
			for _, d := range security.Descriptors() {
				method, rt, _, reason := security.Resolve(cmd.Context(), d.Name, opts)
				detail := reason
				switch method {
				case security.MethodHost:
					detail = "on PATH"
				case security.MethodContainer:
					detail = fmt.Sprintf("via %s", rt)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.Name, d.Category, methodLabel(method), detail)
			}
			tw.Flush()
			return nil
		},
	}
}

func methodLabel(m security.Method) string {
	switch m {
	case security.MethodHost:
		return "host"
	case security.MethodContainer:
		return "container"
	default:
		return "unavailable"
	}
}

func newSecurityInstallCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "install <tool>",
		Short: "Guided host install for one security scanner (approval-gated)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			d, ok := security.DescriptorFor(name)
			if !ok {
				return fmt.Errorf("unknown scanner %q (known: %s)", name, knownScannerNames())
			}
			command, ok := d.Install[runtime.GOOS]
			if !ok || strings.TrimSpace(command) == "" {
				return fmt.Errorf("no guided install available for %s on %s — see the tool's own docs, or configure a container image (security.tools.%s.image)", d.Name, runtime.GOOS, d.Name)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s — %s\n", d.Name, d.Summary)
			fmt.Fprintf(out, "\nThis will run the following command on your host:\n\n    %s\n\n", command)

			if !yes {
				fmt.Fprint(out, "Proceed? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
					fmt.Fprintln(out, "Aborted — nothing was installed.")
					return nil
				}
			}

			shell, shellArgs := shellCommand(command)
			c := exec.CommandContext(cmd.Context(), shell, shellArgs...)
			c.Stdout = out
			c.Stderr = cmd.ErrOrStderr()
			if err := c.Run(); err != nil {
				return fmt.Errorf("install command failed: %w", err)
			}
			fmt.Fprintf(out, "\n%s installed. Run `aegis security status` to confirm.\n", d.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (for non-interactive/scripted use)")
	return cmd
}

// shellCommand returns the platform shell binary and args to run command
// through, mirroring the shell tool's own invocation convention.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func knownScannerNames() string {
	all := security.Descriptors()
	names := make([]string, len(all))
	for i, d := range all {
		names[i] = d.Name
	}
	return strings.Join(names, ", ")
}

func newSecurityConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the resolved security.tools configuration",
		Long:  "View-only: edit security.tools/security.default_method directly in .aegis/config.yaml (project) or ~/.config/aegis/config.yaml (user) — see docs/configuration.md.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "default_method: %s\n", defaultOr(cfg.Security.DefaultMethod, "auto"))
			if len(cfg.Security.Tools) == 0 {
				fmt.Fprintln(out, "tools: (none configured — every scanner uses default_method)")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TOOL\tENABLED\tMETHOD\tINSTALL\tIMAGE")
			for name, tc := range cfg.Security.Tools {
				fmt.Fprintf(tw, "%s\t%v\t%s\t%s\t%s\n", name, tc.ToolEnabled(), defaultOr(tc.Method, "auto"), defaultOr(tc.Install, "prompt"), defaultOr(tc.Image, "(none)"))
			}
			tw.Flush()
			return nil
		},
	}
}

func defaultOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
