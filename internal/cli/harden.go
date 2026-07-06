package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/spf13/cobra"
)

// Hardened cost-cap defaults, applied only to a cap that is currently unset
// (0 = unlimited) — harden tightens whatever was left wide open by default,
// it never silently overrides a value the user already chose. These are
// deliberately conservative starting points, not a prescription: edit
// cost.* in the resulting config to fit your own workflow.
const (
	hardenSessionCapUSD   = 5.0
	hardenDailyCapUSD     = 25.0
	hardenSessionTokenCap = 300_000
	hardenDailyTokenCap   = 2_000_000
)

// newHardenCmd builds `aegis harden`, a one-command way to flip the
// permissive-by-default knobs the fable-review.md security audit called
// out (sandbox=local, egress-then-write=off, cost caps unlimited) from
// opt-in to on, instead of requiring an operator to discover and set each
// one individually across `aegis sandbox use`, `aegis security-config`, and
// hand-editing cost.* in config.yaml.
func newHardenCmd() *cobra.Command {
	var project, yes bool
	cmd := &cobra.Command{
		Use:   "harden",
		Short: "Apply a hardened profile: sandboxed execution, egress-then-write, and spend caps",
		Long: "Flips the permissive-by-default security knobs to on, in one step:\n\n" +
			"  - sandbox.backend -> \"auto\" (containerized execution when a runtime is\n" +
			"    available, instead of running tool calls directly on the host)\n" +
			"  - security.egress_then_write -> true (a write after any network egress\n" +
			"    in the same run requires approval, closing the read-then-exfiltrate path)\n" +
			"  - any cost cap still at its unlimited default (0) is set to a conservative\n" +
			"    starting value; caps you've already customized are left alone\n\n" +
			"Not touched: security.network_allowlist (empty means no restriction — which\n" +
			"domains belong there can't be guessed) and plan-mode network access, which is\n" +
			"gated by permission policy rather than config (see docs/permissions.md).\n\n" +
			"Run 'aegis dry-run' afterward to see the resulting config, and hand-edit\n" +
			"config.yaml to loosen anything too strict for your workflow.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			sandboxPatch := config.SandboxPatch{
				Backend:  cfg.Sandbox.Backend,
				Runtime:  cfg.Sandbox.Runtime,
				Priority: cfg.Sandbox.Priority,
				Image:    cfg.Sandbox.Image,
				Network:  cfg.Sandbox.Network,
			}
			// "container" is already hardened (a specific runtime was pinned);
			// only "local" (or unset, which defaults to local) needs to move.
			sandboxChanged := sandboxPatch.Backend != "auto" && sandboxPatch.Backend != "container"
			if sandboxChanged {
				sandboxPatch.Backend = "auto"
			}

			securityPatch := config.SecurityPatch{
				EgressThenWrite:  true,
				NetworkAllowList: cfg.Security.NetworkAllowList,
				DefaultMethod:    cfg.Security.DefaultMethod,
				Tools:            cfg.Security.Tools,
				DAST:             cfg.Security.DAST,
			}
			securityChanged := !cfg.Security.EgressThenWrite

			costPatch := config.CostPatch{
				BudgetUSD:       cfg.Cost.BudgetUSD,
				MaxTokensPerRun: cfg.Cost.MaxTokensPerRun,
				SessionCapUSD:   cfg.Cost.SessionCapUSD,
				DailyCapUSD:     cfg.Cost.DailyCapUSD,
				SessionTokenCap: cfg.Cost.SessionTokenCap,
				DailyTokenCap:   cfg.Cost.DailyTokenCap,
				AlertThreshold:  cfg.Cost.AlertThreshold,
			}
			var costChanges []string
			if costPatch.SessionCapUSD <= 0 {
				costPatch.SessionCapUSD = hardenSessionCapUSD
				costChanges = append(costChanges, fmt.Sprintf("session_cap_usd -> $%.2f", hardenSessionCapUSD))
			}
			if costPatch.DailyCapUSD <= 0 {
				costPatch.DailyCapUSD = hardenDailyCapUSD
				costChanges = append(costChanges, fmt.Sprintf("daily_cap_usd -> $%.2f", hardenDailyCapUSD))
			}
			if costPatch.SessionTokenCap <= 0 {
				costPatch.SessionTokenCap = hardenSessionTokenCap
				costChanges = append(costChanges, fmt.Sprintf("session_token_cap -> %d", hardenSessionTokenCap))
			}
			if costPatch.DailyTokenCap <= 0 {
				costPatch.DailyTokenCap = hardenDailyTokenCap
				costChanges = append(costChanges, fmt.Sprintf("daily_token_cap -> %d", hardenDailyTokenCap))
			}

			fmt.Fprintln(out, "This will apply the following changes:")
			fmt.Fprintln(out)
			if sandboxChanged {
				fmt.Fprintf(out, "  sandbox.backend              %q -> \"auto\"\n", cfg.Sandbox.Backend)
			} else {
				fmt.Fprintf(out, "  sandbox.backend              already %q — unchanged\n", cfg.Sandbox.Backend)
			}
			if securityChanged {
				fmt.Fprintln(out, "  security.egress_then_write   false -> true")
			} else {
				fmt.Fprintln(out, "  security.egress_then_write   already true — unchanged")
			}
			if len(costChanges) == 0 {
				fmt.Fprintln(out, "  cost caps                    already set — unchanged")
			} else {
				for _, c := range costChanges {
					fmt.Fprintf(out, "  cost.%s\n", c)
				}
			}
			fmt.Fprintln(out)

			if !yes {
				fmt.Fprint(out, "Apply? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
					fmt.Fprintln(out, "Aborted — nothing was changed.")
					return nil
				}
			}

			writeSandbox := config.PatchGlobalSandbox
			writeSecurity := config.PatchGlobalSecurity
			writeCost := config.PatchGlobalCost
			scope := "global"
			if project {
				writeSandbox = config.PatchProjectSandbox
				writeSecurity = config.PatchProjectSecurity
				writeCost = config.PatchProjectCost
				scope = "project (.aegis/config.yaml)"
			}
			if err := writeSandbox(sandboxPatch); err != nil {
				return fmt.Errorf("write sandbox config: %w", err)
			}
			if err := writeSecurity(securityPatch); err != nil {
				return fmt.Errorf("write security config: %w", err)
			}
			if err := writeCost(costPatch); err != nil {
				return fmt.Errorf("write cost config: %w", err)
			}
			fmt.Fprintf(out, "Hardened profile applied to %s config. Restart Aegis to apply.\n", scope)
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write to the project .aegis/config.yaml instead of the global config")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (for non-interactive/scripted use)")
	return cmd
}
