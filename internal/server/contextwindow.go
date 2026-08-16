package server

import (
	"context"
	"time"

	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/providerfactory"
)

// Context-window resolution (P23.1): the daemon needs one number — the
// effective context window the model server will actually honor — to drive
// compaction thresholds, the engine's proactive-compaction check, and the
// TUI's usage bar. For cloud providers the configured context_window (or the
// adapter's own limits) is fine; for Ollama the OpenAI-compat endpoint gives
// no way to set or read num_ctx, and an oversized prompt is silently
// truncated from the front (system prompt first). So when the provider is —
// or the base URL turns out to be — an Ollama server, the daemon queries the
// native API for the served context and reconciles it with config.
//
// P52.1: that number is resolved *per model*, not once per server. The model a
// turn runs on is resolved per turn (session /model override > persona config
// override > persona file Model > global, plus small-model task routing), so a
// single server-wide window detected against cfg.Provider.Model enforced the
// wrong ceiling in both directions: too small for a persona pinning a
// larger-context model (burning summarizer calls on a conversation that had
// room), and — the failure this subsystem exists to prevent — too large for a
// persona-pinned or routed small model, where the engine believes it has
// headroom, never compacts, and Ollama silently drops the system prompt.

// ctxWinDetectTimeout bounds the first-use detection for a model the daemon
// hasn't seen before. The probes are cheap local GETs against the server the
// turn is about to hit anyway (ollamainfo applies its own per-call timeouts on
// top), but a turn must never hang on them.
const ctxWinDetectTimeout = 5 * time.Second

// ctxWinEntry is one model's resolved effective window. final marks the value
// authoritative — a loaded-model /api/ps reading, or an explicitly configured
// window on a backend with nothing to detect. Each model carries its own final
// state (P52.1) because a model that hasn't been loaded yet only yields a
// modelfile/default guess, so its entry stays non-final and re-detects after
// the first run that loads it, independently of every other model's.
type ctxWinEntry struct {
	win   int
	src   string
	final bool
	// max is the model's training-context maximum, when the backend reports
	// one. It is the ceiling the phased drive's P47.5b escalation climbs
	// toward — recorded here rather than re-probed because a daemon-hosted
	// drive (P52.12) needs it mid-run, where a synchronous probe would stall
	// the very turn that just overflowed.
	max int
}

// isGlobalModel reports whether model is the configured provider model (the
// server-wide default). The empty string means "unspecified", which every
// caller resolves to the global model.
func (s *Server) isGlobalModel(model string) bool {
	return model == "" || model == s.cfg.Provider.Model
}

// windowLocked returns model's cached entry, and whether one exists. Callers
// must hold ctxWinMu.
//
// The global model's entry lives in the ctxWin/ctxWinSrc/ctxWinFinal fields
// rather than in the map: those are the numbers /status reports and the
// summarizer is tuned to, both of which are server-wide by construction, and
// keeping one storage location for them avoids a second source of truth that
// could drift. Every other model lives in ctxWinByModel.
func (s *Server) windowLocked(model string) (ctxWinEntry, bool) {
	if s.isGlobalModel(model) {
		return ctxWinEntry{win: s.ctxWin, src: s.ctxWinSrc, final: s.ctxWinFinal}, true
	}
	e, ok := s.ctxWinByModel[model]
	return e, ok
}

// setWindowLocked records model's entry. Callers must hold ctxWinMu (or be in
// single-threaded startup).
//
// The summarizer is retuned only by the model *compaction itself runs on*
// (s.compModel — provider.small_model when configured, else the global model),
// never by an arbitrary session's persona-pinned model: the compactor is
// daemon-wide, so one session's reading must not redefine what every other
// session compacts against. Keying it to compModel rather than to the global
// model is the second half of P52.1 — compaction prefers a small model, and
// tuning its budget to the *primary* model's window is the same wrong-model
// mismatch, one layer down, where the truncated request is the summarizer's own.
func (s *Server) setWindowLocked(model string, e ctxWinEntry) {
	if s.summarizer != nil && s.compModel != "" && model == s.compModel {
		s.summarizer.SetContextWindow(e.win)
		// Retuned alongside the window (P66.14): the trigger is a function of
		// both, and a window that moved without its completion budget would
		// re-open the disagreement LLM-02 closed.
		s.summarizer.SetMaxTokens(s.cfg.Provider.MaxTokens)
	}
	if s.isGlobalModel(model) {
		s.ctxWin, s.ctxWinSrc, s.ctxWinFinal = e.win, e.src, e.final
		return
	}
	if s.ctxWinByModel == nil {
		s.ctxWinByModel = make(map[string]ctxWinEntry)
	}
	s.ctxWinByModel[model] = e
}

// legacyCompatPath reports whether requests reach Ollama through the
// OpenAI-compat (/v1) adapter, which cannot carry num_ctx. On that path a
// configured provider.context_window is a statement of intent the server never
// hears (P61.8), so it is not evidence of what is actually served.
func (s *Server) legacyCompatPath() bool {
	return providerfactory.IsLegacyOllamaCompat(s.cfg.Provider)
}

// compatDefaultApplies reports whether Ollama's documented out-of-the-box
// window should stand in for an unservable configured one. It needs the
// stricter of the two predicates: IsLegacyOllamaCompat deliberately
// over-matches so its *advice* reaches a proxied server, but its bare-/v1 half
// also matches LM Studio and liteLLM, which have their own serving defaults —
// substituting Ollama's 4096 there would invent a number for a server that
// isn't Ollama. This is doctorServedWindow's rule exactly (P61.4), which is the
// point: the daemon and the diagnostic must not answer the same question two
// different ways for the same config.
func (s *Server) compatDefaultApplies() bool {
	return s.legacyCompatPath() && providerfactory.IsOllamaPortBaseURL(s.cfg.Provider.BaseURL)
}

// configEntry is the pre-detection entry for any model: the configured window,
// or nothing at all when context_window is unset. final says whether there is
// anything left to detect (false keeps the model in the re-detect path).
//
// P61.8: on the compat path with an unambiguously-Ollama base URL, config is
// not what will be served and Ollama's default stands in for it. That matters
// most for the first turn on a model — the one carrying the full system prompt
// plus any skill body — which would otherwise be spent believing there is 8x
// the room Ollama is actually serving, so nothing compacts and Ollama truncates
// from the front (P39.9's shape).
func (s *Server) configEntry(final bool) ctxWinEntry {
	if s.compatDefaultApplies() {
		return ctxWinEntry{win: ollamainfo.DefaultServeContext, src: "ollama:compat-default", final: final}
	}
	e := ctxWinEntry{win: s.cfg.Provider.ContextWindow, final: final}
	if e.win > 0 {
		e.src = "config"
	}
	return e
}

// initContextWindow resolves the effective window once at daemon startup.
// Called from New with s.cfg/s.logger already set, before the compactor is
// built. When the value is not yet authoritative (Ollama up but the model not
// loaded, so only the modelfile/default is known), later runs re-detect via
// maybeRefreshContextWindowFor. Only the global model is resolved here; any other
// model a turn routes to is resolved on first use (effectiveContextWindowFor).
func (s *Server) initContextWindow(ctx context.Context) {
	model := s.cfg.Provider.Model
	s.setWindowLocked(model, s.configEntry(false))

	p := s.cfg.Provider
	// The native Ollama adapter (P33.9) pins options.num_ctx to cfgWin on every
	// request, so on well-resourced hardware the served window is exactly what's
	// configured. But num_ctx is a *request*, not a guarantee: on VRAM-constrained
	// hardware Ollama can allocate less than asked (or offload KV/layers to CPU),
	// silently truncating prompts from the front just like the compat path (P39.9).
	// So we no longer trust cfgWin outright for native either — we run the same
	// /api/ps-backed detection and let applyDetectedWindowFor downgrade to the real
	// allocation when a *loaded* (authoritative) reading comes back below cfgWin.
	// A non-authoritative reading (model not loaded yet) keeps cfgWin and retries
	// via maybeRefreshContextWindow after the first run loads the model.
	//
	// Only probe when the target could plausibly be Ollama: the explicit
	// "ollama" provider, or an "openai" provider re-pointed at a custom base URL
	// (the documented way to run Aegis against a local server via the
	// OpenAI-compat endpoint).
	if p.Default != "ollama" && (p.Default != "openai" || p.BaseURL == "") {
		s.setWindowLocked(model, s.configEntry(true))
		return
	}
	base := ollamainfo.NativeBase(p.BaseURL)
	res, ok := ollamainfo.Detect(ctx, base, p.Model)
	if !ok {
		if p.Default == "ollama" || s.compatDefaultApplies() {
			// Ollama not answering yet (daemon may start first) — keep the
			// base around and retry at run time. The compat path takes this
			// branch too (P61.8): letting config stand final there would pin
			// an unservable number for the daemon's lifetime, where the seeded
			// configEntry above already holds Ollama's default until a real
			// reading replaces it.
			s.ollamaBase = base
		} else {
			// A non-Ollama OpenAI-compatible server (LM Studio, liteLLM, real
			// OpenAI behind a gateway): nothing to detect, config stands.
			s.setWindowLocked(model, s.configEntry(true))
		}
		return
	}
	s.ollamaBase = base
	s.applyDetectedWindowFor(model, res)
}

// applyDetectedWindowFor reconciles a detection result with config and updates
// that model's effective window. Callers must hold ctxWinMu (or be in
// single-threaded startup). The reconciliation runs per entry (P52.1): the
// configured context_window expresses one desired window for the daemon, but
// what a *given* model is actually served is a property of that model, so each
// entry is compared against config on its own.
func (s *Server) applyDetectedWindowFor(model string, res ollamainfo.Result) {
	cfgWin := s.cfg.Provider.ContextWindow
	// P61.8: on the /v1 compat path config is not a promise the server ever
	// received, so any reading beats it regardless of authority — a modelfile
	// or default reading there *is* what will be served, because nothing
	// overrides it on the wire. Neutralizing cfgWin rather than adding a branch
	// keeps one reconciliation rule: whatever the server says, wins. Gated on
	// the broad predicate, not compatDefaultApplies, because arriving here at
	// all means ollamainfo reached a native Ollama API — the ambiguity that
	// predicate guards against is already resolved by the successful probe.
	if cfgWin > 0 && s.legacyCompatPath() {
		if res.ContextWindow != cfgWin {
			s.logger.Warn("configured context_window is not what the OpenAI-compat endpoint serves; using the detected value",
				"model", model, "configured", cfgWin, "served", res.ContextWindow, "source", string(res.Source),
				"hint", "the /v1 path never sends num_ctx — switch to provider.default: ollama, raise OLLAMA_CONTEXT_LENGTH, or pin num_ctx in a modelfile")
		}
		cfgWin = 0
	}
	// A loaded-model reading is the ground truth; anything else is worth
	// re-checking once the first run has actually loaded the model.
	e := ctxWinEntry{final: res.Authoritative(), max: res.ModelMax}
	switch {
	case cfgWin > 0 && res.Authoritative() && res.ContextWindow < cfgWin:
		// Config promises more than Ollama is actually serving — trusting the
		// config here is exactly the silent-truncation failure. Serve reality,
		// tell the user how to get what they configured.
		s.logger.Warn("configured context_window exceeds what Ollama is serving; using the served value",
			"model", model, "configured", cfgWin, "served", res.ContextWindow,
			"hint", "raise OLLAMA_CONTEXT_LENGTH on the Ollama server or pin num_ctx in a modelfile")
		e.win, e.src = res.ContextWindow, "ollama:"+string(res.Source)
	case cfgWin > 0:
		e.win, e.src = cfgWin, "config"
	default:
		e.win, e.src = res.ContextWindow, "ollama:"+string(res.Source)
		s.logger.Info("auto-detected Ollama context window", "model", model, "window", res.Describe(), "model_max", res.ModelMax)
	}
	s.setWindowLocked(model, e)
}

// effectiveContextWindow returns the current effective window and its source
// ("config", "ollama:loaded", "ollama:modelfile", "ollama:default",
// "ollama:compat-default", or "" when unknown) for the globally configured
// model — what /status reports and
// what the daemon-wide compactor is tuned to.
func (s *Server) effectiveContextWindow() (int, string) {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	return s.ctxWin, s.ctxWinSrc
}

// effectiveContextWindowFor returns the effective window and source for the
// model a turn will actually run on (P52.1), detecting and caching it on first
// use. Detection is synchronous because the alternative — seed from the global
// model's window now, correct it after the run — is precisely the failure being
// fixed: a routed/pinned small model would spend its first turn (the one
// carrying the full system prompt plus any skill body) believing it had the
// primary model's headroom.
func (s *Server) effectiveContextWindowFor(ctx context.Context, model string) (int, string) {
	s.ctxWinMu.Lock()
	e, ok := s.windowLocked(model)
	base := s.ollamaBase
	s.ctxWinMu.Unlock()
	if ok {
		return e.win, e.src
	}
	if base == "" {
		// Nothing to detect against (cloud provider, or an OpenAI-compatible
		// server that isn't Ollama): config is the only source, exactly as it
		// is for the global model in initContextWindow.
		s.ctxWinMu.Lock()
		defer s.ctxWinMu.Unlock()
		return s.storeLocked(model, s.configEntry(true))
	}

	dctx, cancel := context.WithTimeout(ctx, ctxWinDetectTimeout)
	defer cancel()
	res, detected := ollamainfo.Detect(dctx, base, model)

	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	// A concurrent turn on the same new model may have won the race while we
	// were probing. Detection is idempotent, so the duplicate probe is merely
	// wasted, but the entry it produced stands rather than being overwritten.
	if e, ok := s.windowLocked(model); ok {
		return e.win, e.src
	}
	if !detected {
		// Ollama stopped answering between startup and now. Keep config and
		// stay non-final so the post-run refresh retries.
		return s.storeLocked(model, s.configEntry(false))
	}
	s.applyDetectedWindowFor(model, res)
	e, _ = s.windowLocked(model)
	return e.win, e.src
}

// storeLocked records e for model and returns its window/source, saving the
// callers above a re-read. Callers must hold ctxWinMu.
func (s *Server) storeLocked(model string, e ctxWinEntry) (int, string) {
	s.setWindowLocked(model, e)
	return e.win, e.src
}

// maybeRefreshContextWindowFor re-runs detection for model while that model's
// value is not authoritative — cheap local GETs, called after a run completes
// (the run is what loads the model into Ollama, making /api/ps report the real
// allocation). No-op for non-Ollama providers and once a loaded-model reading
// has been captured for that model. It refreshes the model the finished run
// actually used, not cfg.Provider.Model (P52.1): a run routed to the small
// model taught us nothing about the primary's allocation, and everything about
// the small one's.
func (s *Server) maybeRefreshContextWindowFor(ctx context.Context, model string) {
	s.ctxWinMu.Lock()
	e, known := s.windowLocked(model)
	base := s.ollamaBase
	s.ctxWinMu.Unlock()
	if base == "" || (known && e.final) {
		return
	}

	res, ok := ollamainfo.Detect(ctx, base, model)
	if !ok {
		return
	}
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	if cur, known := s.windowLocked(model); known {
		if cur.final {
			return
		}
		e = cur
	}
	prev := e.win
	s.applyDetectedWindowFor(model, res)
	if cur, _ := s.windowLocked(model); cur.win != prev {
		s.logger.Info("effective context window updated", "model", model, "before", prev, "after", cur.win, "source", cur.src)
	}
}

// modelContextMax returns the model's training-context maximum as detected by
// ollamainfo, or 0 when unknown (a cloud provider, or a model never probed).
//
// Zero is the honest answer for "no ceiling to climb toward", and every caller
// treats it that way: the P47.5b escalation simply does not run, and the drive
// falls back to the fresh-context reset alone.
func (s *Server) modelContextMax(model string) int {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	e, ok := s.windowLocked(model)
	if !ok {
		return 0
	}
	return e.max
}
