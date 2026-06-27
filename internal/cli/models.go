package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/scottymacleod/aegis/internal/discover"
	"github.com/scottymacleod/aegis/internal/modelcatalog"
	"github.com/spf13/cobra"
)

func newModelsCmd() *cobra.Command {
	var local bool

	cmd := &cobra.Command{
		Use:   "models",
		Short: "Show a curated model catalog (and optionally discover local servers)",
		Long: "Prints a curated, qualitative guide to recommended models for Aegis. Model IDs and " +
			"availability change — confirm against the provider's docs. With --local, also probe " +
			"for locally running model servers (Ollama, LM Studio, LiteLLM).",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "PROVIDER\tMODEL\tTIER\tCONTEXT\tNOTES")
			for _, m := range modelcatalog.Curated() {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.Provider, m.ID, m.Tier, m.Context, m.Notes)
			}
			tw.Flush()
			fmt.Fprintln(out, "\nGuidance only — verify current model IDs with each provider.")

			if local {
				fmt.Fprintln(out, "\nDiscovering local model servers…")
				models := discover.Discover(cmd.Context(), discover.DefaultSources(), 3*time.Second)
				if len(models) == 0 {
					fmt.Fprintln(out, "  none found (checked Ollama :11434, LM Studio :1234, LiteLLM :4000)")
					return nil
				}
				ltw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
				for _, m := range models {
					fmt.Fprintf(ltw, "  %s\t%s\t%s\n", m.Provider, m.Name, m.Endpoint)
				}
				ltw.Flush()
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "also discover locally running model servers")
	return cmd
}
