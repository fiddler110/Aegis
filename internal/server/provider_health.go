package server

import (
	"context"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// probeProviderReachability performs the cheap liveness check GET /status
// (P28.7) surfaces to the TUI/web UI so "is the model actually reachable"
// doesn't require spending a conversational turn on it — the exact pattern
// that motivated this: this daemon's own session history had a recurring
// run of near-duplicate sessions titled things like "test that the model is
// connected" / "validate model is connected".
//
// Mirrors `aegis doctor`'s provider check (internal/cli/doctor.go,
// doctorProviderCheck/ollamaNativeBase) but returns a bool+latency instead
// of a report row, and stays cheap enough to run on every /status poll: for
// an Ollama-style provider (provider.default == "ollama", or a base_url
// pointing at the default Ollama port — the same detection doctor.go uses)
// this is a live GET /api/version with a short timeout, timed for latency.
// For anything else (a cloud provider, or a non-Ollama OpenAI-compatible
// server) a live call on every poll would be wasteful or, for a paid API,
// costly — reachability there is just "an API key is present in the
// resolved config", the same signal doctor uses for that case, with latency
// left unmeasured (0).
func (s *Server) probeProviderReachability(ctx context.Context) (reachable bool, latencyMS int64) {
	p := s.cfg.Provider
	isOllama := strings.EqualFold(p.Default, "ollama") || strings.Contains(p.BaseURL, ":11434")
	if isOllama {
		base := ollamainfo.NativeBase(p.BaseURL)
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		start := time.Now()
		if ollamainfo.IsOllama(pctx, base) {
			return true, time.Since(start).Milliseconds()
		}
		return false, 0
	}
	return p.APIKey != "", 0
}
