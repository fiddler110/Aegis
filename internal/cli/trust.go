package cli

import (
	"bufio"
	"fmt"
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
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Review and accept (or revoke) this directory's project-sourced security config",
		Long: "Aegis freezes security-relevant settings from a project's .aegis/config.yaml\n" +
			"(permission.*, sandbox.*, mcp.servers, notify.webhook, hooks) to their\n" +
			"user/global values until the directory they live in is explicitly trusted —\n" +
			"a cloned repository should not be able to silently widen its own permissions,\n" +
			"add an attacker MCP server, or run lifecycle hooks just by being checked out.\n\n" +
			"Run this in a project you've reviewed and want to trust; it shows exactly\n" +
			"which settings the project config would change before asking for confirmation.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			dir := cfg.WorkspaceTrust.Dir
			store := workspacetrust.Open(config.WorkspaceTrustStorePath())

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
	return cmd
}
