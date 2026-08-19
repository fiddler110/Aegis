package server

import (
	"context"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// Fitting the serving window to the hardware (P72.1).
//
// context_window was a number an operator worked out once and pasted into
// config.yaml. The arithmetic that would have computed it has existed since
// P69.5 — ollamainfo.Fit solves for the largest window whose KV cache fits a
// stated budget alongside the model's measured weights, validated to 0.2%
// against real Ollama placements — but nothing ever ran it except `aegis models
// --fit`, which an operator has to find on their own. The measured cost of that
// gap on the machine this was calibrated against: a solo session serving 16,384
// tokens of a safely-fittable 65,000+.
//
// Three things make this more than a wire:
//
// The chicken-and-egg. Fitting needs the model's resident weight bytes, which
// come from /api/ps, which requires the model already loaded — and loading it is
// what commits it to a window. There is no way to know the weights before
// picking a window. So the daemon picks one (config, or detection), *loads the
// model at it*, measures, fits, and installs — see autofitPreload. The load is
// issued at the window the first turn would have used anyway, so a fit that
// changes nothing costs no reload.
//
// The plan, not the fit. provider.small_model is co-resident with the primary
// without any debate in sight — compaction runs on it while the primary is still
// held by keep_alive — so sizing each of them against the whole budget is
// exactly the bug P69.6 closed one layer out. Everything here goes through
// ollamainfo.PlanFor, the resident-set solver, never through Fit directly; with
// a single member the two agree by construction, so there is no second, simpler
// answer to keep in sync.
//
// And the permission. A configured context_window is frequently load-bearing
// (this repo's own debate topology pins 16k for documented reasons), so a fit is
// allowed to *replace* one only when provider.autofit_context says so. With no
// context_window configured there is nothing to contradict and the budget alone
// is the opt-in. Nothing here writes config.yaml: the fitted window is the
// effective window for this daemon's lifetime, announced in the log and visible
// in /status as its source.

// autofitSrc marks a context-window entry as solved from the memory budget
// rather than configured or detected, so /status and the logs say why a window
// is the size it is.
const autofitSrc = "fit:vram-budget"

// autofitLoadTimeout bounds one model's measurement load. A cold mid-size model
// on a mid-range card is tens of seconds — well past ollamainfo.Warm's 3s
// pre-warm budget, which is why WarmAt takes the caller's deadline instead.
// Reaching it is not fatal: the fit is skipped and the daemon serves the window
// it already had.
const autofitLoadTimeout = 90 * time.Second

// autofitPlanTimeout bounds the planning round-trip, matching
// residentSetPlanTimeout — same solver, same handful of local GETs.
const autofitPlanTimeout = 20 * time.Second

// autofitContextWindows sizes the serving windows from provider.vram_budget_gb
// at daemon start, and reports whether it reached a verdict worth printing.
//
// A true return means this function has already said everything
// warnResidentBudget would: it planned the same models against the same budget
// and either installed the result or named why it could not. False means it did
// not get that far — no budget, no local model server, not permitted, or nothing
// measurable yet — and the P69.6 warning should run as it always did.
func (s *Server) autofitContextWindows(ctx context.Context) bool {
	p := s.cfg.Provider
	budget := p.VRAMBudgetBytes()
	if budget <= 0 {
		return false
	}
	s.ctxWinMu.Lock()
	base := s.ollamaBase
	s.ctxWinMu.Unlock()
	if base == "" {
		// No local Ollama to measure or serve against. warnResidentBudget names
		// a budget set against a cloud provider, so this stays quiet.
		return false
	}
	if s.legacyCompatPath() {
		// The /v1 path cannot carry num_ctx (P61.8), so a fitted window is a
		// number the server would never receive. Installing it would have the
		// daemon budget its conversation against a window nothing serves, which
		// is the silent front-truncation this whole subsystem exists to prevent.
		s.logger.Warn("provider.vram_budget_gb cannot size the context window on the OpenAI-compat path; leaving it as configured",
			"hint", "set provider.default: ollama to use the native API, which can send num_ctx")
		return false
	}
	if !s.autofitPermitted() {
		s.logger.Info("provider.context_window is set, so it is left alone; set provider.autofit_context: true to size it from provider.vram_budget_gb instead",
			"context_window", p.ContextWindow, "budget", ollamainfo.FormatGiB(budget))
		return false
	}

	models := s.autofitSet(nil)
	if len(models) == 0 {
		return false
	}
	s.preloadForMeasurement(ctx, base, models, "context-window fit")
	return s.autofitPlanAndApply(ctx, base, models, budget)
}

// autofitPlanAndApply runs the resident-set solver over models and installs the
// result. It is shared by the boot pass and the post-run admission below so
// there is one place a fit becomes a window.
func (s *Server) autofitPlanAndApply(ctx context.Context, base string, models []string, budget int64) bool {
	pctx, cancel := context.WithTimeout(ctx, autofitPlanTimeout)
	plan, ok, reason := ollamainfo.PlanFor(pctx, base, models, budget, ollamainfo.KVCacheType(s.cfg.Provider.KVCacheType), s.knownWeights())
	cancel()
	if !ok {
		if isUnmeasuredReason(reason) {
			// Nothing was measurable — the preload did not take, or Ollama went
			// away. That is "no verdict", not "does not fit", and the two must
			// not read the same: warning on it would train the operator to
			// ignore the warning that matters.
			return false
		}
		s.logger.Warn("cannot fit the configured models in provider.vram_budget_gb; leaving the context windows as they were",
			"budget", ollamainfo.FormatGiB(budget), "models", models, "reason", reason)
		return true
	}
	s.recordWeights(plan)
	s.commitWindows(ctx, base, plan, s.applyAutofit(plan), "context-window fit")
	return true
}

// commitWindows reloads each model whose window a plan moved, at its new size,
// and checks Ollama's own verdict on the result.
//
// Without this the fit would be a belief the daemon holds about a runner that is
// still the old size, and the next detection — /api/ps on the old runner — would
// read as "Ollama is serving less than we asked for" and reconcile the fit away
// before any turn had a chance to apply it. Committing it here is not extra
// work either: it is exactly the reload the next turn's num_ctx would have
// forced, moved to where it costs no turn.
//
// The reload is also what makes the arithmetic falsifiable. If the fitted window
// does not really fit, Ollama answers by spilling to system RAM, and
// Footprint.FullyOnGPU reports its own placement decision rather than a guess
// about it — at daemon start, rather than as a mysteriously slow first turn.
func (s *Server) commitWindows(ctx context.Context, base string, plan ollamainfo.Plan, changed []string, why string) {
	for _, model := range changed {
		win := plan.Windows[model]
		lctx, cancel := context.WithTimeout(ctx, autofitLoadTimeout)
		err := ollamainfo.WarmAt(lctx, base, model, win)
		cancel()
		if err != nil {
			s.logger.Warn("could not reload the model at its planned window; the next turn will apply it instead",
				"model", model, "window", win, "for", why, "err", err)
			continue
		}
		if f, ok := ollamainfo.Loaded(ctx, base, model); ok && !f.FullyOnGPU() {
			s.logger.Warn("Ollama spilled part of the model to system RAM at the planned window; provider.vram_budget_gb is larger than the card really has spare",
				"model", model, "window", win, "for", why,
				"resident", ollamainfo.FormatGiB(f.Size), "in_vram", ollamainfo.FormatGiB(f.SizeVRAM))
		}
	}
}

// autofitPermitted reports whether a fitted window may be installed. The budget
// alone is enough when nothing was configured; replacing a configured
// context_window needs the explicit provider.autofit_context, because that
// number is often one someone worked out for a reason and silently overwriting
// it is the one thing P72.1 was filed saying not to do.
func (s *Server) autofitPermitted() bool {
	return s.cfg.Provider.AutofitContext || s.cfg.Provider.ContextWindow <= 0
}

// autofitSet is the set of models to plan as co-resident: the global model,
// provider.small_model when it is a distinct model, plus extra (the models a
// post-run admission has since seen a turn actually run on). Duplicates and
// blanks are dropped here; the planner would collapse them anyway, but the
// caller's "is this model already in the set" check needs the deduplicated list.
//
// small_model is in the set for a reason that has nothing to do with debate:
// compaction runs on it while the primary is still held resident by keep_alive,
// so the two share the card whether or not anything asked them to.
func (s *Server) autofitSet(extra []string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		out = append(out, m)
	}
	add(s.cfg.Provider.Model)
	add(s.cfg.Provider.SmallModel)
	for _, m := range extra {
		add(m)
	}
	return out
}

// preloadForMeasurement loads each model that is not already resident, so its
// weights can be measured, and returns the ones it actually issued a load for.
//
// This resolves the chicken-and-egg both fitting paths hit: a window cannot be
// solved without the weights, the weights cannot be read without a load, and a
// load itself picks a window. Nothing else can break the cycle — /api/tags'
// on-disk size is the tempting substitute and overstates a multimodal model by
// the size of a vision projector that is never resident (2.57 GiB on qwen35-9b),
// which is more than a fitted window's whole margin.
//
// Each model is loaded at the window the daemon has already resolved for it, so
// the runner left behind is the one the next turn would have created — a fit
// that confirms the current number then costs nothing at all, and one that does
// not was going to pay a reload either way. An already-resident model is not
// re-loaded (the /api/ps gate ollamainfo.WarmIfUnloaded takes for the same
// reason), and a failure is not fatal: planning simply finds nothing measured
// and the caller keeps the windows it had.
//
// The returned list is what the *caller* brought into residency, which a
// resident-set claim needs in order to say which models are finished with when
// its workload ends (P72.3). A model that was already loaded is not in it: this
// call did not make it resident and has no standing to evict it.
func (s *Server) preloadForMeasurement(ctx context.Context, base string, models []string, why string) []string {
	var loaded []string
	for _, model := range models {
		if strings.TrimSpace(model) == "" || ollamainfo.IsLoaded(ctx, base, model) {
			continue
		}
		s.ctxWinMu.Lock()
		e, _ := s.windowLocked(model)
		s.ctxWinMu.Unlock()
		s.logger.Info("loading model to measure its weights", "model", model, "at_window", e.win, "for", why)
		lctx, cancel := context.WithTimeout(ctx, autofitLoadTimeout)
		err := ollamainfo.WarmAt(lctx, base, model, e.win)
		cancel()
		if err != nil {
			s.logger.Warn("could not load the model to measure it; its window is left as it was",
				"model", model, "for", why, "err", err)
			continue
		}
		loaded = append(loaded, model)
	}
	return loaded
}

// applyAutofit installs a plan's windows as the daemon's effective ones.
//
// Unlike installPlan (P69.6, residentset.go) this may *raise* a window, which is
// the whole point — the case that motivated the item is a 16,384 that should be
// 65,536. The two differ because they answer different questions: a resident-set
// claim makes an already-sized set fit for the duration of one workload, so
// growing a member only buys a cold reload; this decides what the model should
// have been served all along.
//
// Each window is recorded in autofitWin as well as installed, because the two
// are different facts: the entry is what the daemon currently believes is
// served, and autofitWin is what it asked for. Reconciliation compares a fresh
// detection against the second (configWindowFor), which is what makes the
// arithmetic verifiable rather than merely asserted — if Ollama serves less than
// the fit asked for, the existing authoritative-reading rule downgrades to
// reality and says so.
// It returns the models whose window actually moved, which is what needs
// committing to the runner.
func (s *Server) applyAutofit(plan ollamainfo.Plan) []string {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	if s.autofitWin == nil {
		s.autofitWin = make(map[string]int, len(plan.Models))
	}
	var changed []string
	for _, model := range plan.Models {
		win := plan.Windows[model]
		cur, _ := s.windowLocked(model)
		s.autofitWin[model] = win
		if cur.win == win {
			continue
		}
		changed = append(changed, model)
		// final:false so the post-run refresh re-measures: the fit is a
		// prediction and /api/ps is the verdict, the same posture ollamainfo
		// takes toward its own arithmetic.
		s.setWindowLocked(model, ctxWinEntry{win: win, src: autofitSrc, final: false, max: cur.max})
		s.logger.Info("fitted the context window to provider.vram_budget_gb",
			"model", model, "before", cur.win, "before_source", cur.src, "after", win,
			"kv_cache", ollamainfo.FormatGiB(plan.KVBytes[model]),
			"weights", ollamainfo.FormatGiB(plan.MemberWeights[model]))
	}
	s.logger.Info("context-window fit complete",
		"models", plan.Models, "windows", plan.Windows,
		"total", ollamainfo.FormatGiB(plan.Total), "budget", ollamainfo.FormatGiB(plan.Budget),
		"spare", ollamainfo.FormatGiB(plan.Spare()),
		"note", "effective for this daemon only; provider.context_window is not rewritten")
	return changed
}

// knownWeights snapshots the measured-weights cache for a planning call.
//
// It is a copy because planning happens outside the lock and the map is written
// by every plan that succeeds.
func (s *Server) knownWeights() map[string]int64 {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	if len(s.weightsSeen) == 0 {
		return nil
	}
	out := make(map[string]int64, len(s.weightsSeen))
	for k, v := range s.weightsSeen {
		out[k] = v
	}
	return out
}

// recordWeights remembers what a successful plan measured, so a later plan can
// still be made for a model whose weights have stopped being derivable.
//
// That happens, and it is this feature's own doing. WeightsBytes derives weights
// by subtracting the KV cache a loaded model's window accounts for, and
// BytesPerToken is a deliberate upper bound for sliding-window architectures. At
// a small window the over-estimate is small enough to leave a sane remainder; at
// a large one it can exceed the whole footprint, and the subtraction yields
// nothing. Measured live on 2026-08-19: aegis-qwen35-9b at 16000 reports 6.01
// GiB and derives 4.00 GiB of weights cleanly, and after this feature resized it
// to 82944 it reports 8.01 GiB against a predicted 10.44 GiB of cache — so a
// debate started after the fit would have been refused for want of a figure the
// daemon had already measured half a minute earlier. Weights are
// window-invariant, so remembering the first reading is not a workaround for a
// bad number; it is keeping a good one.
func (s *Server) recordWeights(plan ollamainfo.Plan) {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	if s.weightsSeen == nil {
		s.weightsSeen = make(map[string]int64, len(plan.MemberWeights))
	}
	for model, w := range plan.MemberWeights {
		if w > 0 {
			s.weightsSeen[model] = w
		}
	}
}

// autofitAdmit re-plans when a turn has just run on a model the fit has never
// accounted for — a /model switch, a persona pin, or small-model routing to
// something the boot pass did not know about.
//
// This is P72.1's third step, and it is deliberately not a per-model solo fit.
// A newly-selected model does not replace the one the daemon was already
// serving; it joins it, because keep_alive holds the previous model resident and
// Ollama keeps one runner per model name. Fitting the new arrival against the
// whole budget on its own would size it as if it were alone next to a model that
// demonstrably is not gone — the P69.6 bug, arrived at from the other direction.
// So the new model is *admitted to the set* and the whole set is re-planned.
//
// The set only ever grows within a daemon's lifetime, so the windows it hands
// out are monotonically non-increasing: each admission can shrink a member (one
// reload, in the safe direction) but no sequence of them can oscillate. It runs
// after a run completes, off the turn's critical path, because the model is only
// measurable once that run has loaded it — the same first-load constraint the
// boot pass resolves with a preload, once per newly-seen model rather than once
// per daemon.
func (s *Server) autofitAdmit(ctx context.Context, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	budget := s.cfg.Provider.VRAMBudgetBytes()
	if budget <= 0 || !s.autofitPermitted() {
		return
	}
	s.ctxWinMu.Lock()
	base := s.ollamaBase
	known := len(s.autofitWin) > 0
	_, inSet := s.autofitWin[model]
	s.ctxWinMu.Unlock()
	// known guards the case where the boot fit never installed anything — no
	// local server, an unmeasurable model, a set that does not fit. Admitting a
	// model into a set that was never established would fit it alone, which is
	// the thing this function exists not to do.
	if base == "" || !known || inSet {
		return
	}
	if s.isGlobalModel(model) {
		return
	}
	s.autofitPlanAndApply(ctx, base, s.autofitSet([]string{model}), budget)
}
