package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/workspacetrust"
	"github.com/spf13/cobra"
)

// newTrustCmd builds `aegis trust`, the P27.1 workspace-trust gate's
// operator-facing surface: a cloned repository's .aegis/config.yaml is
// auto-merged with no confirmation by default (FIND-01/FIND-02), so
// security-relevant keys (permission.*, sandbox.*, mcp.servers,
// notify.webhook, hooks) stay frozen to their user/global values until the
// current directory is explicitly trusted here. config.Load() already
// computes the diff of what's frozen (WorkspaceTrustStatus) — this command
// just surfaces it and records the decision.
func newTrustCmd() *cobra.Command {
	var yes, revoke, status bool
	var dirFlag string
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Review and accept (or revoke) this directory's project-sourced security config",
		Long: "Aegis freezes security-relevant settings from a project's .aegis/config.yaml\n" +
			"(permission.*, sandbox.*, mcp.servers, notify.webhook, hooks,\n" +
			"workspace.additional_roots) to their user/global values until the directory\n" +
			"they live in is explicitly trusted — a cloned repository should not be able to\n" +
			"silently widen its own permissions, add an attacker MCP server, or run\n" +
			"lifecycle hooks just by being checked out.\n\n" +
			"Run this in a project you've reviewed and want to trust; it shows exactly\n" +
			"which settings the project config would change before asking for confirmation.\n\n" +
			"Use --dir to record a trust decision for another directory without cd-ing into\n" +
			"it. That is how a workspace.additional_roots entry (P52.13) is authorized: an\n" +
			"additional root does not inherit the primary workspace's trust, and — unlike\n" +
			"the current directory — usually has no .aegis/config.yaml of its own to review.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			dir := cfg.WorkspaceTrust.Dir
			store := workspacetrust.Open(config.WorkspaceTrustStorePath())

			if dirFlag != "" {
				return trustNamedDir(cmd, store, dirFlag, yes, revoke, status)
			}

			if revoke {
				if err := store.Revoke(dir); err != nil {
					return fmt.Errorf("revoke trust: %w", err)
				}
				fmt.Fprintf(out, "%s is no longer trusted; its project config's security-relevant settings will be frozen to user/global values on next load.\n", dir)
				return nil
			}

			if cfg.WorkspaceTrust.Trusted {
				fmt.Fprintf(out, "%s is already trusted.\n", dir)
				return nil
			}
			if len(cfg.WorkspaceTrust.Changes) == 0 {
				fmt.Fprintf(out, "%s has no security-relevant project config to trust (nothing frozen). Trusting it anyway so future edits apply immediately.\n", dir)
				if status {
					return nil
				}
			} else {
				fmt.Fprintf(out, "%s is not trusted. Its project config (.aegis/config.yaml) would change:\n\n", dir)
				for _, c := range cfg.WorkspaceTrust.Changes {
					fmt.Fprintf(out, "  %s\n", c)
				}
				fmt.Fprintln(out)
			}
			if status {
				fmt.Fprintln(out, "Run `aegis trust` (without --status) to accept these settings, or review .aegis/config.yaml first.")
				return nil
			}

			if !yes {
				fmt.Fprint(out, "Trust this directory and apply these settings? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
					fmt.Fprintln(out, "Not trusted — nothing changed.")
					return nil
				}
			}
			if err := store.Trust(dir); err != nil {
				return fmt.Errorf("trust: %w", err)
			}
			fmt.Fprintf(out, "%s is now trusted. Restart the daemon to apply.\n", dir)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (for non-interactive/scripted use)")
	cmd.Flags().BoolVar(&revoke, "revoke", false, "remove trust for this directory instead of granting it")
	cmd.Flags().BoolVar(&status, "status", false, "show what's frozen without prompting or changing anything")
	cmd.Flags().StringVar(&dirFlag, "dir", "", "record the trust decision for this directory instead of the current one (use to authorize a workspace.additional_roots entry)")
	return cmd
}

// trustNamedDir handles `aegis trust --dir <path>`: a trust decision for a
// directory other than the current one.
//
// It deliberately does *not* go through cfg.WorkspaceTrust, which reports on
// the process's own cwd, and it does not short-circuit on "there's nothing to
// freeze". The no-project-config case is the common one for an additional
// root — a research repo has no .aegis/config.yaml — and the whole point of
// P52.13's per-root trust is that the decision must be recordable for exactly
// such a directory. What is being granted here is not "apply this project's
// config" but "let the session reach into this directory at all", so the
// prompt says that instead of listing frozen settings.
func trustNamedDir(cmd *cobra.Command, store *workspacetrust.Store, dir string, yes, revoke, status bool) error {
	out := cmd.OutOrStdout()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not an existing directory", abs)
	}

	if revoke {
		if err := store.Revoke(abs); err != nil {
			return fmt.Errorf("revoke trust: %w", err)
		}
		fmt.Fprintf(out, "%s is no longer trusted; it will be dropped from workspace.additional_roots on next load.\n", abs)
		return nil
	}
	if status {
		if store.IsTrusted(abs) {
			fmt.Fprintf(out, "%s is trusted.\n", abs)
		} else {
			fmt.Fprintf(out, "%s is not trusted. Run `aegis trust --dir %s` to grant it.\n", abs, dir)
		}
		return nil
	}
	if store.IsTrusted(abs) {
		fmt.Fprintf(out, "%s is already trusted.\n", abs)
		return nil
	}

	if !yes {
		fmt.Fprintf(out, "Trust %s? Workspace-confined tools in a session that lists it under\nworkspace.additional_roots will be able to read files there. [y/N] ", abs)
		reader := bufio.NewReader(cmd.InOrStdin())
		line, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
			fmt.Fprintln(out, "Not trusted — nothing changed.")
			return nil
		}
	}
	if err := store.Trust(abs); err != nil {
		return fmt.Errorf("trust: %w", err)
	}
	fmt.Fprintf(out, "%s is now trusted. Restart the daemon to apply.\n", abs)
	return nil
}
