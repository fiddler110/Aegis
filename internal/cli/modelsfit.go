package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// `aegis models --fit` (P69.5): size a model's serving context window from what
// the machine can actually hold, instead of from the model's training maximum.
//
// The default recommendation path (ollamainfo.RecommendContextWindow) takes the
// model max and halves it, floored at 32768. For a model whose training context
// is 262144 that is 131072 tokens — 16.5 GiB of KV cache on its own, before any
// weights — which no 16 GB card can serve. Worse, the 32768 floor means it
// cannot express a co-resident configuration at all on such a card.
//
// This command computes the KV cache exactly from /api/show geometry, measures
// the resident weights from /api/ps, and solves for the window that fits a
// budget the operator states. It deliberately does not detect total VRAM: see
// the internal/ollamainfo/kvfit.go package note and P17.5. What it does instead
// is report Ollama's own placement verdict (size vs size_vram), which answers
// "did it fit" without guessing at "how much is there".

// fitOptions are the flags of `aegis models --fit`.
type fitOptions struct {
	enabled  bool
	model    string
	budgetGB float64
	kvType   string
	write    bool

	// set and debate select the resident-set planner (P69.6) instead of the
	// single-model fit above. They answer the question --fit cannot: a debate
	// holds two or three models in VRAM at once, and sizing each of them as if
	// it were alone is how a set that cannot possibly fit gets written to a
	// config file. --fit-debate resolves the *actual* configured seat trio, so
	// it answers "will my debate fit" without spending a debate to find out.
	set    []string
	debate bool
}

// runFit prints a KV-cache fit report for one model. cfg supplies the Ollama
// base URL and the default model.
func runFit(ctx context.Context, out io.Writer, cfg *config.Config, o fitOptions) error {
	base := ollamainfo.NativeBase(cfg.Provider.BaseURL)
	if base == "" {
		return fmt.Errorf("--fit needs a local Ollama server; provider.base_url is unset")
	}
	// provider.vram_budget_gb is the budget of record (P69.6). A flag still wins,
	// so a what-if run needs no config edit, but omitting the flag must not mean
	// "no budget" on a machine that has already stated one — that would make the
	// diagnostic disagree with the daemon about the same hardware.
	if o.budgetGB <= 0 {
		o.budgetGB = cfg.Provider.VRAMBudgetGB
	}
	if o.kvType == "" {
		o.kvType = cfg.Provider.KVCacheType
	}
	if o.debate || len(o.set) > 0 {
		return runFitSet(ctx, out, cfg, base, o)
	}
	model := o.model
	if model == "" {
		model = strings.TrimSpace(cfg.Provider.Model)
	}
	if model == "" {
		return fmt.Errorf("--fit needs a model: set provider.model or pass --fit-model")
	}

	kvType := ollamainfo.KVCacheType(o.kvType)
	if _, ok := ollamainfo.KVBytes(ollamainfo.KVGeometry{
		BlockCount: 1, HeadCountKV: 1, KeyLength: 1, ValueLength: 1,
	}, 1, kvType); !ok {
		return fmt.Errorf("unrecognized --kv-type %q (want f16, q8_0 or q4_0)", o.kvType)
	}

	g, ok := ollamainfo.Geometry(ctx, base, model)
	if !ok {
		return fmt.Errorf("could not read model_info for %q from %s (is Ollama running, and the model pulled?)", model, base)
	}
	if !g.Complete() {
		return fmt.Errorf("model_info for %q is missing the KV geometry (blocks=%d kv_heads=%d key=%d value=%d); "+
			"no window can be fitted without it", model, g.BlockCount, g.HeadCountKV, g.KeyLength, g.ValueLength)
	}
	perToken, _ := g.BytesPerToken(kvType)

	fmt.Fprintf(out, "Model:        %s\n", model)
	fmt.Fprintf(out, "Architecture: %s (%d blocks, %d KV heads, key %d + value %d)\n",
		g.Arch, g.BlockCount, g.HeadCountKV, g.KeyLength, g.ValueLength)
	fmt.Fprintf(out, "KV cache:     %.0f KiB per token at %s\n", float64(perToken)/1024, kvTypeLabel(kvType))
	if g.ContextMax > 0 {
		fmt.Fprintf(out, "Training max: %d tokens\n", g.ContextMax)
	}
	for _, inf := range g.Inferred {
		fmt.Fprintf(out, "  NOTE: %s — the estimate rests on this assumption.\n", inf)
	}
	if g.SWAWindow > 0 {
		fmt.Fprintf(out, "  NOTE: sliding-window attention (%d tokens) is reported but not discounted,\n"+
			"        so the figures below are an upper bound for this model.\n", g.SWAWindow)
	}

	weights, weightsSrc := fitWeights(ctx, out, base, model, g, kvType)
	if weights <= 0 {
		fmt.Fprintln(out, "\nCannot size a window without a weights figure. Load the model once")
		fmt.Fprintf(out, "(`ollama run %s ''`) and re-run — a loaded model gives an exact number.\n", model)
		return nil
	}
	fmt.Fprintf(out, "Weights:      %s (%s)\n", ollamainfo.FormatGiB(weights), weightsSrc)

	if o.budgetGB <= 0 {
		printFitTable(out, g, weights, kvType)
		return nil
	}

	budget := int64(o.budgetGB * float64(int64(1)<<30))
	win, ok := ollamainfo.Fit(g, budget, weights, kvType)
	if !ok {
		fmt.Fprintf(out, "\nNo viable window fits %.2f GiB alongside %s of weights.\n",
			o.budgetGB, ollamainfo.FormatGiB(weights))
		fmt.Fprintf(out, "Raise the budget, use a smaller model, or set --kv-type q8_0 to roughly halve the cache.\n")
		return nil
	}
	kv, _ := ollamainfo.KVBytes(g, win, kvType)
	fmt.Fprintf(out, "\nBudget:       %s\n", ollamainfo.FormatGiB(budget))
	fmt.Fprintf(out, "Fitted window: %d tokens (%s of KV, %s total, %s spare)\n",
		win, ollamainfo.FormatGiB(kv), ollamainfo.FormatGiB(kv+weights),
		ollamainfo.FormatGiB(budget-kv-weights))

	if rec := ollamainfo.RecommendContextWindow(g.ContextMax); rec != win {
		recKV, _ := ollamainfo.KVBytes(g, rec, kvType)
		fmt.Fprintf(out, "\nFor comparison, the model-max recommendation is %d tokens — %s of KV,\n",
			rec, ollamainfo.FormatGiB(recKV))
		fmt.Fprintf(out, "%s total. That is the number first-init would write.\n",
			ollamainfo.FormatGiB(recKV+weights))
	}

	if !o.write {
		fmt.Fprintf(out, "\nTo apply:\n\nprovider:\n  context_window: %d\n\n", win)
		fmt.Fprintln(out, "Or re-run with --write to patch the global config in place.")
		// The third option is to stop pasting the number at all (P72.1): with a
		// budget stated, the daemon runs this same solver at startup. Worth
		// naming here, because this report is where an operator learns the
		// arithmetic exists.
		fmt.Fprintln(out, "Or set provider.autofit_context: true and let the daemon solve this at")
		fmt.Fprintln(out, "startup — it re-fits whenever the model changes, and never edits config.")
		return nil
	}
	if err := config.PatchGlobalContextWindow(win); err != nil {
		return fmt.Errorf("write context_window: %w", err)
	}
	fmt.Fprintf(out, "\nWrote context_window: %d to %s\n", win, config.GlobalConfigPath())
	fmt.Fprintln(out, "Restart the daemon (`aegis serve`) for it to take effect.")
	return nil
}

// fitWeights resolves the model's resident weight bytes, preferring the
// measured figure from a loaded model. The distinction matters more than it
// looks: /api/tags reports on-disk size, which for a multimodal model includes
// a vision projector that is never resident unless an image is sent — 2.57 GiB
// of phantom weights on qwen35-9b, enough to swallow a fitted window's whole
// margin. So an unloaded model gets no estimate rather than a bad one.
func fitWeights(ctx context.Context, out io.Writer, base, model string, g ollamainfo.KVGeometry, t ollamainfo.KVCacheType) (int64, string) {
	f, loaded := ollamainfo.Loaded(ctx, base, model)
	if !loaded {
		fmt.Fprintln(out, "\nModel is not currently loaded, so its resident weights cannot be measured.")
		return 0, ""
	}
	fmt.Fprintf(out, "\nLoaded now:   %s resident, %s in VRAM, window %d\n",
		ollamainfo.FormatGiB(f.Size), ollamainfo.FormatGiB(f.SizeVRAM), f.ContextLength)
	if f.FullyOnGPU() {
		fmt.Fprintln(out, "              Ollama placed it entirely on the GPU at that window.")
	} else {
		fmt.Fprintln(out, "              Ollama SPILLED part of it to system RAM at that window —")
		fmt.Fprintln(out, "              the current window does not fit. Fit a smaller one below.")
	}
	w, ok := ollamainfo.WeightsBytes(f, g, t)
	if !ok {
		return 0, ""
	}
	return w, "measured: resident size minus the loaded window's KV cache"
}

// printFitTable shows the footprint at a range of windows, for when no budget
// was supplied. Inventing a VRAM figure would be the one thing this command
// refuses to do, so with no budget it reports the curve and lets the operator
// pick against a number only they know.
func printFitTable(out io.Writer, g ollamainfo.KVGeometry, weights int64, t ollamainfo.KVCacheType) {
	fmt.Fprintln(out, "\nNo --budget-gb given, so here is the curve. Pick the largest row that")
	fmt.Fprintln(out, "fits your card alongside anything else you keep resident:")
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "\n  WINDOW\tKV CACHE\tTOTAL")
	for _, win := range []int{4096, 8192, 16384, 32768, 65536, 131072} {
		if g.ContextMax > 0 && win > g.ContextMax {
			break
		}
		kv, _ := ollamainfo.KVBytes(g, win, t)
		fmt.Fprintf(tw, "  %d\t%s\t%s\n", win, ollamainfo.FormatGiB(kv), ollamainfo.FormatGiB(kv+weights))
	}
	tw.Flush()
	fmt.Fprintln(out, "\nRe-run with --budget-gb <N> to have the exact window solved for you.")
}

// runFitSet plans a whole resident set (P69.6) rather than one model at a time:
// the largest per-model windows that hold every member in VRAM simultaneously.
//
// This is the observable half of the feature and it lands before anything
// changes at runtime, which is deliberate — an operator should be able to see
// what the daemon will do to their windows before a debate does it.
func runFitSet(ctx context.Context, out io.Writer, cfg *config.Config, base string, o fitOptions) error {
	models, origin, err := fitSetModels(cfg, o)
	if err != nil {
		return err
	}
	budget := ollamainfo.BudgetBytes(o.budgetGB)
	if budget <= 0 {
		return fmt.Errorf("planning a resident set needs a memory budget: pass --budget-gb, or set provider.vram_budget_gb")
	}
	kvType := ollamainfo.KVCacheType(o.kvType)

	fmt.Fprintf(out, "Resident set: %s\n", origin)
	fmt.Fprintf(out, "Budget:       %s (%s KV cache)\n\n", ollamainfo.FormatGiB(budget), kvTypeLabel(kvType))

	plan, ok, reason := ollamainfo.PlanFor(ctx, base, models, budget, kvType, nil)
	if !ok {
		fmt.Fprintf(out, "No assignment fits: %s\n", reason)
		fmt.Fprintln(out, "\nRaise the budget, put a smaller model in one seat, or set kv_cache_type: q8_0")
		fmt.Fprintln(out, "on the Ollama server (and here) to roughly halve every cache.")
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tWINDOW\tKV CACHE\tWEIGHTS")
	for _, m := range plan.Models {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", m, plan.Windows[m],
			ollamainfo.FormatGiB(plan.KVBytes[m]), ollamainfo.FormatGiB(plan.MemberWeights[m]))
	}
	tw.Flush()
	fmt.Fprintf(out, "\nWeights:  %s\n", ollamainfo.FormatGiB(plan.Weights))
	fmt.Fprintf(out, "Total:    %s of %s (%s spare)\n",
		ollamainfo.FormatGiB(plan.Total), ollamainfo.FormatGiB(plan.Budget), ollamainfo.FormatGiB(plan.Spare()))
	if plan.Collapsed > 0 {
		fmt.Fprintf(out, "\n%d seat(s) share a model with another and were planned once: Ollama holds one\n", plan.Collapsed)
		fmt.Fprintln(out, "runner per model name, so they share its weights and its KV cache.")
	}
	fmt.Fprintln(out, "\nThese are the windows a debate will install for its duration; each model's solo")
	fmt.Fprintln(out, "window is restored when the debate finishes.")
	return nil
}

// fitSetModels resolves which models the set consists of, and a phrase naming
// where the list came from so the report is self-explanatory.
//
// --fit-debate goes through enginecfg.DebateSeatModel, the same resolver the
// daemon and `aegis debate` use (P69.1). Re-deriving persona→model precedence
// here would let the diagnostic pass while the debate it is diagnosing runs on
// different models.
func fitSetModels(cfg *config.Config, o fitOptions) ([]string, string, error) {
	if o.debate {
		seats := debate.Seats(debate.Config{})
		models := make([]string, 0, len(seats))
		parts := make([]string, 0, len(seats))
		for _, seat := range seats {
			m := enginecfg.DebateSeatModel(cfg, seat.Persona)
			if m == "" {
				return nil, "", fmt.Errorf("the %s seat (persona %q) resolves to no model; set provider.model", seat.Role, seat.Persona)
			}
			models = append(models, m)
			parts = append(parts, fmt.Sprintf("%s=%s", seat.Role, m))
		}
		return models, "configured debate seats — " + strings.Join(parts, ", "), nil
	}
	var models []string
	for _, m := range o.set {
		for _, part := range strings.Split(m, ",") {
			if part = strings.TrimSpace(part); part != "" {
				models = append(models, part)
			}
		}
	}
	if len(models) == 0 {
		return nil, "", fmt.Errorf("--fit-set needs at least one model name")
	}
	return models, strings.Join(models, ", "), nil
}

func kvTypeLabel(t ollamainfo.KVCacheType) string {
	if t == "" {
		return "f16"
	}
	return string(t)
}

// debateResidentPlan plans the serving windows a headless `aegis debate` should
// give its seats, or nil when there is nothing to plan (P69.6).
//
// `aegis debate` runs with no daemon, so there is no context-window cache to
// install a plan into — the plan rides each seat's adapter as a num_ctx stamp
// instead, via the same provider.WithNumCtx the daemon's modelAdapter uses. What
// must not differ is the *plan*: same config key, same planner, same seat-model
// resolver, for the same reason enginecfg.DebateSeatModel exists. Two ways of
// deciding which windows a debate gets is how the CLI and the daemon end up
// disagreeing about the machine they are both running on.
//
// nil, nil means "no planning": no budget stated, or no local Ollama server to
// measure against. An error means the set was planned and does not fit, which is
// a reason not to start.
func debateResidentPlan(ctx context.Context, out io.Writer, cfg *config.Config, dcfg debate.Config) (map[string]int, error) {
	budget := ollamainfo.BudgetBytes(cfg.Provider.VRAMBudgetGB)
	if budget <= 0 {
		return nil, nil
	}
	base := ollamainfo.NativeBase(cfg.Provider.BaseURL)
	if base == "" {
		return nil, nil
	}
	seats := debate.Seats(dcfg)
	models := make([]string, 0, len(seats))
	for _, seat := range seats {
		if m := enginecfg.DebateSeatModel(cfg, seat.Persona); m != "" {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return nil, nil
	}
	plan, ok, reason := ollamainfo.PlanFor(ctx, base, models, budget, ollamainfo.KVCacheType(cfg.Provider.KVCacheType), nil)
	if !ok {
		return nil, fmt.Errorf("this debate's models do not fit the %s budget (provider.vram_budget_gb): %s\n"+
			"Run `aegis models --fit-debate` to see the arithmetic", ollamainfo.FormatGiB(budget), reason)
	}
	fmt.Fprintf(out, "[resident set: %s in %s, %s spare]\n",
		strings.Join(planSummary(plan), ", "), ollamainfo.FormatGiB(plan.Budget), ollamainfo.FormatGiB(plan.Spare()))
	return plan.Windows, nil
}

// planSummary renders a plan as "model@window" pairs for a one-line notice.
func planSummary(plan ollamainfo.Plan) []string {
	parts := make([]string, 0, len(plan.Models))
	for _, m := range plan.Models {
		parts = append(parts, fmt.Sprintf("%s@%d", m, plan.Windows[m]))
	}
	return parts
}
