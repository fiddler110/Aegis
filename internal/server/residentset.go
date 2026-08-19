package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/debate"
	"github.com/fiddler110/aegis/internal/enginecfg"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// Resident-set claims (P69.6).
//
// Since P69.1 each debate seat resolves its own model, so a debate holds two or
// three models in VRAM at once — and every one of them was sized as if it were
// alone, because effectiveContextWindowFor answers per model and nothing above
// it ever asked "what else is resident". ollamainfo.PlanResidentSet answers the
// set question; this file is where a workload states that a set is about to
// exist, for how long, and gets the plan installed.
//
// Two mechanical facts shape the whole design:
//
// Ollama holds one runner per model *name*, keyed on its load options, so
// changing num_ctx for a resident model forces an unload and reload. The
// co-resident window and the solo window for the same model therefore cannot
// coexist — the split is temporal, not static, and the override has to be
// daemon-wide for its duration. Anything that resolves a window mid-debate must
// see the planned number, or it flips the runner back and the debate's next seat
// flips it again. provider.max_concurrent_requests defaults to 1 on a local
// backend, which serializes those requests but does nothing about the thrash.
//
// And a plan is installed by *writing real entries* through setWindowLocked,
// not by adding a lookup layer in front of the cache. The obvious
// implementation — a ctxWinPlanned map consulted first in
// effectiveContextWindowFor — bypasses setWindowLocked's summarizer retune, so
// the compactor would keep compacting against the solo window while the runner
// serves the planned one. That is the P66.14 disagreement one layer down, and it
// is worth the extra save/restore bookkeeping to not reopen it. Writing real
// entries also means effectiveContextWindowFor, newEngine and modelAdapter need
// no changes at all: they pick the plan up because it is simply what the cache
// now says.

// residentSetPlanTimeout bounds the whole planning round-trip. Planning is a
// handful of local /api/show and /api/ps GETs (ollamainfo applies its own
// per-call timeouts inside), but a debate must not hang on a wedged model server
// before it has spent a single turn.
const residentSetPlanTimeout = 20 * time.Second

// residentSetSrc marks a context-window entry as installed by a plan rather than
// detected or configured, so /status and the logs say why a window moved.
const residentSetSrc = "plan:resident-set"

// residentSetGate returns the 1-deep claim semaphore, creating it on first use.
//
// Lazy rather than initialized in New because Server values are also built
// field-by-field in tests, and a nil channel is not an empty semaphore — it is a
// deadlock. A cap-1 channel rather than a mutex because a second concurrent
// debate should wait *interruptibly*: two debates planning against one GPU would
// each install windows out from under the other, so queueing is the correct
// behavior, but a queued caller must still honor its own context.
func (s *Server) residentSetGate() chan struct{} {
	s.residentSetMu.Lock()
	defer s.residentSetMu.Unlock()
	if s.residentSetSem == nil {
		s.residentSetSem = make(chan struct{}, 1)
	}
	return s.residentSetSem
}

// savedWindow is one model's pre-claim context-window state, including whether
// it had an entry at all. "No entry yet" is a real state — a model no turn has
// run on has never been detected — and restoring a fabricated entry over it
// would pin a planned window as that model's permanent answer.
type savedWindow struct {
	model string
	entry ctxWinEntry
	had   bool
}

// claimResidentSet plans context windows for models and installs them
// daemon-wide until the returned release is called.
//
// It is a no-op — a release that does nothing, and no error — when no budget is
// configured (provider.vram_budget_gb unset) or the backend is not an Ollama
// server, which is what keeps P69.6 invisible to every install that did not opt
// in. When a budget *is* configured and no assignment fits, it returns an error
// naming the shortfall: the caller should refuse the workload before spending a
// model turn on it, which is the earliest honest point to refuse, since a
// resident set is a property of the workload and is not knowable at daemon start.
//
// The claim owns the *residency* of its set, not only the windows (P72.3).
// Before planning it loads any member that is not already resident, because a
// member whose weights have never been measured cannot be planned for at all —
// which, for a debate whose arbiter runs a different model from the proposer, is
// every cold start. After installing the plan it reloads the members whose
// window moved, so the first seat runs against a runner that is actually the
// planned size rather than the solo one. And on release it unloads the members
// it brought in, so the solo window the daemon goes back to is one the card can
// really serve rather than one contending with an arbiter that is still resident
// on keep_alive.
//
// release is safe to call once; callers should defer it immediately.
func (s *Server) claimResidentSet(ctx context.Context, models []string) (func(), error) {
	noop := func() {}

	budget := s.cfg.Provider.VRAMBudgetBytes()
	if budget <= 0 {
		return noop, nil
	}
	// "Nothing to plan" is not a failure. A caller whose seat-model resolver is
	// unwired hands over a list of empty strings, which means every seat runs on
	// the daemon default — one model, already sized, nothing co-resident. That
	// must not read as a set that could not be fitted.
	if !anyNamed(models) {
		return noop, nil
	}
	s.ctxWinMu.Lock()
	base := s.ollamaBase
	s.ctxWinMu.Unlock()
	if base == "" {
		// No local model server to plan against: a cloud backend has nothing
		// resident, and an OpenAI-compatible server that is not Ollama exposes
		// neither the geometry nor the placement this rests on.
		return noop, nil
	}

	sem := s.residentSetGate()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return noop, ctx.Err()
	default:
		// Only log when actually queueing, so the common case stays quiet.
		s.logger.Info("waiting for another resident-set claim to finish before planning", "models", models)
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return noop, ctx.Err()
		}
	}
	release := func() { <-sem }

	// Bring the set into residency before planning it. Weights are only
	// measurable for a loaded model, so without this a debate whose arbiter is a
	// different model from its proposer is refused on every cold start — not
	// because it does not fit, but because nothing had asked the arbiter its
	// size. This is bounded per model rather than in aggregate: each load waits
	// at most autofitLoadTimeout, so a three-seat set collapsing to two distinct
	// models cannot approach the turn-stall bound the caller runs under.
	broughtIn := s.preloadForMeasurement(ctx, base, models, "resident-set claim")

	pctx, cancel := context.WithTimeout(ctx, residentSetPlanTimeout)
	plan, ok, reason := ollamainfo.PlanFor(pctx, base, models, budget, ollamainfo.KVCacheType(s.cfg.Provider.KVCacheType), s.knownWeights())
	cancel()
	if !ok {
		s.releaseResidency(broughtIn)
		release()
		return noop, fmt.Errorf("cannot fit %d models in the %s memory budget (provider.vram_budget_gb): %s",
			len(models), ollamainfo.FormatGiB(budget), reason)
	}

	s.recordWeights(plan)
	saved := s.installPlan(plan)
	s.logger.Info("installed resident-set context windows",
		"models", plan.Models, "windows", plan.Windows,
		"total", ollamainfo.FormatGiB(plan.Total), "budget", ollamainfo.FormatGiB(plan.Budget))
	// Reload the members whose window moved, so the first seat meets a runner of
	// the planned size. Leaving it to the seat's own request works — num_ctx is
	// stamped per request — but pays the reload inside the first model turn, and
	// leaves the window Ollama is really serving disagreeing with the installed
	// plan until then.
	s.commitWindows(ctx, base, plan, savedModels(saved), "resident-set claim")

	// sync.Once rather than a bool: a double release would return the semaphore
	// twice and let two debates hold the GPU's windows at once, which is the one
	// thing the semaphore exists to prevent.
	var once sync.Once
	return func() {
		once.Do(func() {
			s.restoreWindows(saved)
			s.releaseResidency(broughtIn)
			release()
		})
	}, nil
}

// installPlan writes the planned windows and returns what they replaced.
//
// A plan never *raises* a model's window. The claim exists to make a set fit,
// not to grow any member: raising one would force an unload/reload for no gain,
// and on the common single-model debate — all three seats on the same model —
// the planned window is larger than the detected one, so honoring it would make
// every debate pay a cold reload to gain context it did not ask for. Shrinking
// is the only direction that buys anything.
func (s *Server) installPlan(plan ollamainfo.Plan) []savedWindow {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()

	saved := make([]savedWindow, 0, len(plan.Models))
	for _, model := range plan.Models {
		win := plan.Windows[model]
		cur, had := s.windowLocked(model)
		if had && cur.win > 0 && cur.win <= win {
			continue
		}
		saved = append(saved, savedWindow{model: model, entry: cur, had: had})
		// final:false so maybeRefreshContextWindowFor re-measures once the first
		// seat actually loads: the plan is a prediction and /api/ps is the
		// verdict, which is the same posture kvfit.go takes toward its own
		// arithmetic.
		s.setWindowLocked(model, ctxWinEntry{win: win, src: residentSetSrc, final: false, max: cur.max})
	}
	return saved
}

// restoreWindows puts back what installPlan replaced, so a model's solo window
// returns the moment the set stops being resident.
//
// Restored entries are non-final for the same reason the planned ones were: the
// solo window is now also a prediction — the runner is still loaded at the
// planned size until something reloads it — so the next run's refresh is what
// makes it true again.
func (s *Server) restoreWindows(saved []savedWindow) {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	for _, sw := range saved {
		if !sw.had {
			// The model had no entry before the claim, and inventing one now
			// would freeze a planned window as its permanent answer. Dropping it
			// puts it back in the detect-on-first-use path it was in.
			delete(s.ctxWinByModel, sw.model)
			continue
		}
		e := sw.entry
		e.final = false
		s.setWindowLocked(sw.model, e)
	}
}

// releaseResidency unloads the models a claim brought into memory, so the set
// stops being resident when the workload that needed it is over (P72.3).
//
// Only models this claim loaded are unloaded, and never the daemon's own — the
// global model and provider.small_model are what every ordinary turn runs on,
// and evicting them to tidy up after a debate would make the next message pay a
// cold load for nothing. A member that was already resident when the claim
// started is left alone for the same reason in general form: this claim did not
// make it resident and has no standing to say it is finished with.
//
// This matters because restoreWindows puts the daemon's model back to its solo
// window, and that window was solved on the assumption that nothing else is on
// the card. An arbiter still held by keep_alive for another thirty minutes makes
// that assumption false, and Ollama's answer to a load it cannot place is to
// spill to system RAM. The *reload* at the solo window is deliberately left to
// the next turn, which stamps num_ctx and gets it for free: doing it here would
// pay a cold load that a second debate — the common next action — would
// immediately undo.
func (s *Server) releaseResidency(models []string) {
	if len(models) == 0 {
		return
	}
	s.ctxWinMu.Lock()
	base := s.ollamaBase
	s.ctxWinMu.Unlock()
	if base == "" {
		return
	}
	for _, model := range models {
		if s.isGlobalModel(model) || model == strings.TrimSpace(s.cfg.Provider.SmallModel) {
			continue
		}
		if err := ollamainfo.Unload(context.Background(), base, model); err != nil {
			s.logger.Warn("could not unload a model the resident-set claim loaded; it stays resident until keep_alive expires",
				"model", model, "err", err)
			continue
		}
		s.logger.Info("unloaded a model the resident-set claim had loaded", "model", model)
	}
}

// savedModels names the models installPlan actually wrote, which are exactly the
// ones whose runner is now the wrong size.
func savedModels(saved []savedWindow) []string {
	out := make([]string, 0, len(saved))
	for _, sw := range saved {
		out = append(out, sw.model)
	}
	return out
}

// warnResidentBudget sanity-checks a configured memory budget once at daemon
// start, so a mistyped or misplaced one is named before the first turn rather
// than as a mid-debate refusal.
//
// It only ever warns. The resident set a workload needs is not knowable here —
// that is why claimResidentSet refuses at debate start instead — and the check
// that *is* available (does the global model alone fit) is silent when the model
// is not yet loaded, because an unmeasured weight figure is not evidence of
// anything. Warning on it would train the operator to ignore the warning that
// matters.
func (s *Server) warnResidentBudget(ctx context.Context) {
	p := s.cfg.Provider
	if p.VRAMBudgetBytes() <= 0 {
		return
	}
	if !p.KVCacheTypeValid() {
		s.logger.Warn("provider.kv_cache_type is not a KV cache type Aegis can size; planning as f16",
			"value", p.KVCacheType, "want", "f16, q8_0 or q4_0")
	}
	if s.ollamaBase == "" {
		s.logger.Warn("provider.vram_budget_gb is set but the backend is not a local Ollama server; nothing is planned against it",
			"provider", p.Default)
		return
	}
	budget := p.VRAMBudgetBytes()
	plan, ok, reason := ollamainfo.PlanFor(ctx, s.ollamaBase, []string{p.Model}, budget, ollamainfo.KVCacheType(p.KVCacheType), s.knownWeights())
	if ok {
		s.recordWeights(plan)
		s.logger.Info("resident-set budget accepted", "budget", ollamainfo.FormatGiB(budget),
			"model", p.Model, "solo_window", plan.Windows[p.Model])
		return
	}
	if isUnmeasuredReason(reason) {
		return
	}
	s.logger.Warn("the configured model does not fit provider.vram_budget_gb on its own; every debate will refuse",
		"budget", ollamainfo.FormatGiB(budget), "model", p.Model, "reason", reason)
}

// isUnmeasuredReason reports whether a refusal is "nothing measured yet" rather
// than "does not fit". The two are opposite signals and only one of them is
// worth a warning at startup, where the model is normally not loaded.
func isUnmeasuredReason(reason string) bool {
	for _, frag := range []string{"not loaded", "could not read model_info", "could not derive resident weights"} {
		if strings.Contains(reason, frag) {
			return true
		}
	}
	return false
}

// debateSeatModels resolves the models a debate's seats will run on, in seat
// order, through the same resolver the runner itself uses
// (enginecfg.DebateSeatModel — P69.1). Duplicates are left in: the planner
// collapses them, and it is the planner's job to know that two seats sharing a
// model share a runner, not the caller's.
func (s *Server) debateSeatModels(cfg debate.Config) []string {
	seats := debate.Seats(cfg)
	models := make([]string, 0, len(seats))
	for _, seat := range seats {
		models = append(models, enginecfg.DebateSeatModel(s.cfg, seat.Persona))
	}
	return models
}

// anyNamed reports whether models contains at least one non-blank name.
func anyNamed(models []string) bool {
	for _, m := range models {
		if strings.TrimSpace(m) != "" {
			return true
		}
	}
	return false
}
