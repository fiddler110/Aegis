package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// `aegis models --calibrate` (P83): measure provider.vram_budget_gb instead of
// asking the operator to guess it.
//
// This is the last stated number in an otherwise exact chain. `--fit` computes
// a window from the budget precisely; provider.autofit_context re-solves it at
// every daemon start; both are validated against real Ollama placements. And
// both rest on a figure someone typed, which does not survive moving the config
// to another machine.
//
// The measurement is Ollama's own placement verdict, bisected — see
// internal/ollamainfo/calibrate.go for why that is measurement rather than the
// GPU introspection P17.5 rejected. What this file adds is the operator-facing
// half: the probe ladder as a table, the headroom decision, and the write.

// calibrateOptions are the flags of `aegis models --calibrate`.
type calibrateOptions struct {
	enabled bool
	model   string
	kvType  string
	// headroomGB is subtracted from the measured capacity before it is offered
	// as vram_budget_gb. A calibration measures what fit *at that moment*, with
	// whatever else the machine was doing on the GPU; the desktop, a browser or
	// a second Ollama client can all take memory afterwards. The default is
	// deliberately modest — the measurement is real, so this is a cushion, not
	// a fudge factor correcting a guess.
	headroomGB float64
	maxProbes  int
	write      bool
}

// defaultCalibrationHeadroomGB is the cushion left for whatever else claims
// VRAM after the calibration ran.
const defaultCalibrationHeadroomGB = 0.5

// calibrateProbeTimeout bounds one probe. Each is a full model reload — Ollama
// rebuilds the runner whenever num_ctx changes — and a cold mid-size model on a
// mid-range card is tens of seconds, so this is generous on purpose. It matches
// the daemon's own autofitLoadTimeout for the same load.
const calibrateProbeTimeout = 90 * time.Second

func runCalibrate(ctx context.Context, out io.Writer, cfg *config.Config, o calibrateOptions) error {
	base := ollamainfo.NativeBase(cfg.Provider.BaseURL)
	if base == "" {
		return fmt.Errorf("--calibrate needs a local Ollama server; provider.base_url is unset")
	}
	model := o.model
	if model == "" {
		model = cfg.Provider.Model
	}
	if model == "" || model == "auto" {
		return fmt.Errorf("--calibrate needs a concrete model: set provider.model or pass --fit-model")
	}
	kvType := ollamainfo.KVCacheType(o.kvType)
	if kvType == "" {
		kvType = ollamainfo.KVCacheType(cfg.Provider.KVCacheType)
	}

	g, ok := ollamainfo.Geometry(ctx, base, model)
	if !ok || !g.Complete() {
		return fmt.Errorf("could not read the KV geometry for %q from %s; nothing can be measured without it", model, base)
	}

	fmt.Fprintf(out, "Calibrating against: %s\n", model)
	fmt.Fprintf(out, "Architecture:        %s (%d blocks, %d KV heads, key %d + value %d)\n",
		g.Arch, g.BlockCount, g.HeadCountKV, g.KeyLength, g.ValueLength)
	perToken, _ := g.BytesPerToken(kvType)
	fmt.Fprintf(out, "KV cache:            %.0f KiB per token at %s\n", float64(perToken)/1024, kvTypeLabel(kvType))
	fmt.Fprintf(out, "Host:                %s\n\n", hwinfo.Detect().Describe())
	fmt.Fprintf(out, "This loads %s repeatedly at different context windows and reads Ollama's\n", model)
	fmt.Fprintln(out, "own placement verdict each time (size vs size_vram). Every probe is a full")
	fmt.Fprintln(out, "reload, so expect a few minutes. Nothing is written without --write.")
	fmt.Fprintln(out, "\nFor the most useful number, run this with the machine in the state you")
	fmt.Fprintln(out, "normally work in — a calibration taken with nothing else on the GPU")
	fmt.Fprintln(out, "measures a card you will not have when your browser is open.")
	fmt.Fprintln(out)

	probe := probeWithTimeout(ollamainfo.LiveProbe(base, model), calibrateProbeTimeout)
	cal, ok := ollamainfo.Calibrate(ctx, g, kvType, o.maxProbes, probe)
	if !ok {
		return fmt.Errorf("the first probe did not complete; is %s reachable, and can it load %q?", base, model)
	}

	printCalibrationSteps(out, cal)
	fmt.Fprintf(out, "\n%s\n", cal.Explain(model))

	if cal.NoGPU || cal.WeightsSpill {
		printCalibrationFallback(out, cal, model)
		return nil
	}

	fmt.Fprintf(out, "\nWeights:             %s (fitted across the ladder, window-invariant)\n", ollamainfo.FormatGiB(cal.Weights))
	fmt.Fprintf(out, "KV cache measured:   %.1f KiB per token\n", float64(cal.BytesPerToken)/1024)
	fmt.Fprintf(out, "Measured capacity:   %s held entirely in VRAM at %d tokens\n",
		ollamainfo.FormatGiB(cal.Capacity), cal.LargestFit)
	if res := cal.Resolution(); res > 0 {
		fmt.Fprintf(out, "Resolution:          ±%s (bracketed between %d and %d tokens)\n",
			ollamainfo.FormatGiB(res), cal.LargestFit, cal.SmallestSpill)
	}
	printCacheDisagreement(out, cal, model)

	headroom := o.headroomGB
	if headroom < 0 {
		headroom = 0
	}
	budget := cal.CapacityGiB() - headroom
	if budget <= 0 {
		fmt.Fprintf(out, "\nA %.2f GiB headroom allowance exceeds the measured capacity; lower --headroom-gb.\n", headroom)
		return nil
	}
	fmt.Fprintf(out, "\nRecommended:         vram_budget_gb: %.2f  (measured %.2f, less %.2f GiB headroom)\n",
		budget, cal.CapacityGiB(), headroom)

	// Say what the number will actually do, since the budget is an input to
	// three separate things and none of them is obvious from its name. Both
	// figures are printed when they disagree, because the daemon and `--fit`
	// still size windows from the formula: the operator needs to know that the
	// window they will actually be served is the smaller one.
	if win := cal.WindowForBudget(ollamainfo.BudgetBytes(budget), g.ContextMax); win > 0 {
		fmt.Fprintf(out, "At that budget %s fits a %d-token window, measured.\n", model, win)
		if pred, ok := ollamainfo.Fit(g, ollamainfo.BudgetBytes(budget), cal.Weights, kvType); ok && pred < win {
			fmt.Fprintf(out, "`aegis models --fit` and the daemon's autofit will say %d instead — they size\n", pred)
			fmt.Fprintln(out, "from the KV formula, which over-reserves for this architecture (see above).")
		}
	}
	if cal.AtCeiling {
		fmt.Fprintln(out, "\nNote: nothing spilled, so this is a floor on the machine rather than its")
		fmt.Fprintln(out, "capacity. Calibrating against a larger model would measure more of it.")
	}

	if !o.write {
		fmt.Fprintf(out, "\nTo apply:\n\nprovider:\n  vram_budget_gb: %.2f\n  autofit_context: true\n\n", budget)
		fmt.Fprintln(out, "Or re-run with --write to patch the global config in place. autofit_context")
		fmt.Fprintln(out, "is the half that makes it ongoing: with it set, the daemon re-solves")
		fmt.Fprintln(out, "context_window from this budget at every start, on whatever machine it is")
		fmt.Fprintln(out, "running on and for whatever model is configured.")
		return nil
	}
	if err := config.PatchGlobalVRAMBudget(budget); err != nil {
		return fmt.Errorf("write vram_budget_gb: %w", err)
	}
	fmt.Fprintf(out, "\nWrote vram_budget_gb: %.2f to %s\n", budget, config.GlobalConfigPath())
	if !cfg.Provider.AutofitContext {
		fmt.Fprintln(out, "\nprovider.autofit_context is still false, so context_window is unchanged.")
		fmt.Fprintln(out, "Set it to true to have the daemon size the window from this budget at every")
		fmt.Fprintln(out, "start — that is what makes the calibration ongoing rather than a one-off.")
	}
	fmt.Fprintln(out, "Restart the daemon (`aegis serve`) for it to take effect.")
	return nil
}

// probeWithTimeout gives each probe its own deadline. Without one a single
// wedged load would consume the whole command rather than ending the ladder
// with the measurements already taken.
func probeWithTimeout(p ollamainfo.Probe, d time.Duration) ollamainfo.Probe {
	return func(ctx context.Context, numCtx int) (ollamainfo.Footprint, bool) {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		return p(ctx, numCtx)
	}
}

func printCalibrationSteps(out io.Writer, cal ollamainfo.Calibration) {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  WINDOW\tRESIDENT\tIN VRAM\tVERDICT")
	for _, s := range cal.Steps {
		verdict := "spilled to system RAM"
		if s.Fit {
			verdict = "fully on GPU"
		}
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\n", s.Window,
			ollamainfo.FormatGiB(s.Size), ollamainfo.FormatGiB(s.SizeVRAM), verdict)
	}
	tw.Flush()
}

// printCalibrationFallback covers the two outcomes where no budget can be
// stated. Both are real findings about the machine, so they get an answer
// rather than an error.
func printCalibrationFallback(out io.Writer, cal ollamainfo.Calibration, model string) {
	if cal.NoGPU {
		hw := hwinfo.Detect()
		fmt.Fprintln(out, "\nvram_budget_gb is meaningless on a CPU-only host: leave it unset, and the")
		fmt.Fprintln(out, "model ranking falls back to a share of system RAM on its own.")
		if hw.RAMKnown() {
			fmt.Fprintf(out, "Detected %.0f GB of system RAM here, which is what that fallback uses.\n", hw.TotalRAMGB())
		}
		fmt.Fprintln(out, "Size context_window with `aegis models --fit --budget-gb <N>` against however")
		fmt.Fprintln(out, "much RAM you are willing to give it.")
		return
	}
	fmt.Fprintf(out, "\nNo window makes %s fit: the weights are the problem, not the KV cache.\n", model)
	if cal.Weights > 0 {
		fmt.Fprintf(out, "Measured weights: %s, and Ollama still offloaded part of them.\n", ollamainfo.FormatGiB(cal.Weights))
	}
	fmt.Fprintln(out, "Run `aegis models --recommend` for what this machine can hold, or calibrate")
	fmt.Fprintln(out, "against a smaller model with --fit-model.")
}

// printCacheDisagreement surfaces a gap between the measured per-token cache
// cost and what KVGeometry's formula predicts.
//
// This is not a diagnostic for its own sake. The formula is what `aegis models
// --fit`, the daemon's boot-time autofit and the debate resident-set planner
// all size windows from, so a fourfold over-reservation there is a fourfold
// under-sized context window everywhere — silently, and with no symptom except
// a smaller window than the card can serve. Measuring it is the only way an
// operator finds out.
func printCacheDisagreement(out io.Writer, cal ollamainfo.Calibration, model string) {
	if cal.PredictedPerToken <= 0 || cal.BytesPerToken <= 0 {
		return
	}
	ratio := float64(cal.PredictedPerToken) / float64(cal.BytesPerToken)
	if ratio < 1.25 && ratio > 0.8 {
		return // agreement, within the noise of a handful of loads
	}
	fmt.Fprintf(out, "\nNOTE: the KV formula predicts %.1f KiB per token for %s — %.1fx the %.1f KiB\n",
		float64(cal.PredictedPerToken)/1024, model, ratio, float64(cal.BytesPerToken)/1024)
	fmt.Fprintln(out, "actually measured. The formula assumes every transformer block holds a KV")
	fmt.Fprintln(out, "cache; a hybrid-attention model (linear attention in most layers) holds one")
	fmt.Fprintln(out, "in only a fraction of them, and the geometry in /api/show does not say so.")
	fmt.Fprintln(out, "Everything above is measured and unaffected, but `aegis models --fit` and")
	fmt.Fprintln(out, "provider.autofit_context both size from the formula, so they will reserve")
	fmt.Fprintln(out, "more than this model needs and serve a smaller window than it could.")
}
