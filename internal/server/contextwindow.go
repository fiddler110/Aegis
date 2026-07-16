package server

import (
	"context"

	"github.com/fiddler110/aegis/internal/ollamainfo"
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

// initContextWindow resolves the effective window once at daemon startup.
// Called from New with s.cfg/s.logger already set, before the compactor is
// built. When the value is not yet authoritative (Ollama up but the model not
// loaded, so only the modelfile/default is known), later runs re-detect via
// maybeRefreshContextWindow.
func (s *Server) initContextWindow(ctx context.Context) {
	cfgWin := s.cfg.Provider.ContextWindow
	s.ctxWin, s.ctxWinSrc = cfgWin, ""
	if cfgWin > 0 {
		s.ctxWinSrc = "config"
	}

	p := s.cfg.Provider
	if p.Default == "ollama" && cfgWin > 0 {
		// The native Ollama adapter (P33.9) pins options.num_ctx to cfgWin on
		// every request, so the served window is exactly what's configured —
		// no probing needed, unlike the OpenAI-compat path below.
		s.ctxWinFinal = true
		return
	}
	// Only probe when the target could plausibly be Ollama: the explicit
	// "ollama" provider (with no configured window to pin), or an "openai"
	// provider re-pointed at a custom base URL (the documented way to run
	// Aegis against a local server via the OpenAI-compat endpoint).
	if p.Default != "ollama" && (p.Default != "openai" || p.BaseURL == "") {
		s.ctxWinFinal = true
		return
	}
	base := ollamainfo.NativeBase(p.BaseURL)
	res, ok := ollamainfo.Detect(ctx, base, p.Model)
	if !ok {
		if p.Default == "ollama" {
			// Ollama not answering yet (daemon may start first) — keep the
			// base around and retry at run time.
			s.ollamaBase = base
		} else {
			// A non-Ollama OpenAI-compatible server (LM Studio, liteLLM, real
			// OpenAI behind a gateway): nothing to detect, config stands.
			s.ctxWinFinal = true
		}
		return
	}
	s.ollamaBase = base
	s.applyDetectedWindow(res)
}

// applyDetectedWindow reconciles a detection result with config and updates
// the effective window. Callers must hold ctxWinMu (or be in single-threaded
// startup).
func (s *Server) applyDetectedWindow(res ollamainfo.Result) {
	cfgWin := s.cfg.Provider.ContextWindow
	switch {
	case cfgWin > 0 && res.Authoritative() && res.ContextWindow < cfgWin:
		// Config promises more than Ollama is actually serving — trusting the
		// config here is exactly the silent-truncation failure. Serve reality,
		// tell the user how to get what they configured.
		s.logger.Warn("configured context_window exceeds what Ollama is serving; using the served value",
			"configured", cfgWin, "served", res.ContextWindow,
			"hint", "raise OLLAMA_CONTEXT_LENGTH on the Ollama server or pin num_ctx in a modelfile")
		s.ctxWin, s.ctxWinSrc = res.ContextWindow, "ollama:"+string(res.Source)
	case cfgWin > 0:
		s.ctxWin, s.ctxWinSrc = cfgWin, "config"
	default:
		s.ctxWin, s.ctxWinSrc = res.ContextWindow, "ollama:"+string(res.Source)
		s.logger.Info("auto-detected Ollama context window", "window", res.Describe(), "model_max", res.ModelMax)
	}
	// A loaded-model reading is the ground truth; anything else is worth
	// re-checking once the first run has actually loaded the model.
	s.ctxWinFinal = res.Authoritative()
	if s.summarizer != nil {
		s.summarizer.SetContextWindow(s.ctxWin)
	}
}

// effectiveContextWindow returns the current effective window and its source
// ("config", "ollama:loaded", "ollama:modelfile", "ollama:default", or ""
// when unknown).
func (s *Server) effectiveContextWindow() (int, string) {
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	return s.ctxWin, s.ctxWinSrc
}

// maybeRefreshContextWindow re-runs detection while the current value is not
// authoritative — cheap local GETs, called after a run completes (the run is
// what loads the model into Ollama, making /api/ps report the real
// allocation). No-op for non-Ollama providers and once a loaded-model
// reading has been captured.
func (s *Server) maybeRefreshContextWindow(ctx context.Context) {
	s.ctxWinMu.Lock()
	if s.ctxWinFinal || s.ollamaBase == "" {
		s.ctxWinMu.Unlock()
		return
	}
	base, model := s.ollamaBase, s.cfg.Provider.Model
	s.ctxWinMu.Unlock()

	res, ok := ollamainfo.Detect(ctx, base, model)
	if !ok {
		return
	}
	s.ctxWinMu.Lock()
	defer s.ctxWinMu.Unlock()
	if s.ctxWinFinal {
		return
	}
	prev := s.ctxWin
	s.applyDetectedWindow(res)
	if s.ctxWin != prev {
		s.logger.Info("effective context window updated", "before", prev, "after", s.ctxWin, "source", s.ctxWinSrc)
	}
}
