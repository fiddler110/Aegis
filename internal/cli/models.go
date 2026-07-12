package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/discover"
	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/modelcatalog"
	"github.com/spf13/cobra"
)

func newModelsCmd() *cobra.Command {
	var local bool
	var recommend bool

	cmd := &cobra.Command{
		Use:   "models",
		Short: "Show a curated model catalog (and optionally discover local servers)",
		Long: "Prints a curated, qualitative guide to recommended models for Aegis. Model IDs and " +
			"availability change — confirm against the provider's docs. With --local, also probe " +
			"for locally running model servers (Ollama, LM Studio, LiteLLM). With --recommend, " +
			"detect this machine's CPU/RAM (internal/hwinfo — no GPU/VRAM introspection, see P17.5) " +
			"and narrow the local-model recommendations to what it can run, suggesting `ollama pull` " +
			"for anything recommended but not already pulled.",
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
				} else {
					ltw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
					for _, m := range models {
						fmt.Fprintf(ltw, "  %s\t%s\t%s\n", m.Provider, m.Name, m.Endpoint)
					}
					ltw.Flush()
				}
			}

			if recommend {
				pulled := pulledOllamaModels(cmd.Context(), 3*time.Second)
				printRecommendation(out, hwinfo.Detect(), pulled)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "also discover locally running model servers")
	cmd.Flags().BoolVar(&recommend, "recommend", false, "detect CPU/RAM and narrow local-model recommendations to what this machine can run")
	return cmd
}

// pulledOllamaModels probes the standard local model sources (P20.3 reuses
// internal/discover the same way --local already does) and returns just the
// Ollama model names found — the ones that matter for deciding whether an
// `ollama pull` suggestion is still needed.
func pulledOllamaModels(ctx context.Context, timeout time.Duration) []string {
	return filterOllamaNames(discover.Discover(ctx, discover.DefaultSources(), timeout))
}

// filterOllamaNames extracts just the Ollama-provider model names from a
// discover.Discover result. Split out from pulledOllamaModels so the
// filtering logic is unit-testable without a live probe.
func filterOllamaNames(found []discover.Model) []string {
	var names []string
	for _, m := range found {
		if m.Provider == "ollama" {
			names = append(names, m.Name)
		}
	}
	return names
}

// familyPulled reports whether any pulled Ollama model name belongs to the
// given catalog family (e.g. "qwen3" matches a pulled "qwen3:8b" or a bare
// "qwen3"). Catalog entries are families, not exact tags — the tag you pull
// is your choice — so this only compares the part before ":".
func familyPulled(family string, pulledNames []string) bool {
	for _, p := range pulledNames {
		base := p
		if idx := strings.Index(p, ":"); idx >= 0 {
			base = p[:idx]
		}
		if strings.EqualFold(base, family) {
			return true
		}
	}
	return false
}

// printRecommendation prints the detected hardware, the hardware-narrowed
// local recommendation list (modelcatalog.RecommendLocal), and — for any
// recommended family not already pulled — a suggested `ollama pull <id>`
// command. It never executes the pull itself: this is a printed suggestion
// the user runs themselves, the same "guarded suggestion, never
// auto-execute" posture as the security_advise tool (P13.4) and this
// project's general non-destructive-by-default default.
func printRecommendation(out io.Writer, hw hwinfo.Info, pulledNames []string) {
	fmt.Fprintf(out, "\nDetected hardware: %s\n", hw.Describe())
	if !hw.RAMKnown() {
		fmt.Fprintln(out, "RAM undetected on this platform/environment — showing the full local catalog unnarrowed.")
	}

	rec := modelcatalog.RecommendLocal(hw)
	if len(rec) == 0 {
		fmt.Fprintln(out, "\nNo curated local model meets this machine's detected RAM — consider a cloud provider instead, or a smaller custom model.")
		return
	}

	fmt.Fprintln(out, "\nRecommended local models for this hardware:")
	rtw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(rtw, "  MODEL\tCONTEXT\tMIN RAM\tNOTES")
	for _, m := range rec {
		ram := "—"
		if m.MinRAMGB > 0 {
			ram = fmt.Sprintf("~%dGB", m.MinRAMGB)
		}
		fmt.Fprintf(rtw, "  %s\t%s\t%s\t%s\n", m.ID, m.Context, ram, m.Notes)
	}
	rtw.Flush()

	var toPull []string
	for _, m := range rec {
		if !familyPulled(m.ID, pulledNames) {
			toPull = append(toPull, m.ID)
		}
	}
	if len(toPull) == 0 {
		fmt.Fprintln(out, "\nAll recommended local models already appear to be pulled.")
		return
	}
	fmt.Fprintln(out, "\nNot yet pulled — run these yourself if you want them (not run automatically):")
	for _, id := range toPull {
		fmt.Fprintf(out, "  ollama pull %s\n", id)
	}
}
