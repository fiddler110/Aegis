package server

import (
	"context"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// isOllamaProvider reports whether the configured provider is a local
// Ollama-style server: provider.default == "ollama", or a base_url pointing at
// the default Ollama port. Same detection `aegis doctor` uses
// (internal/cli/ollama.go's ollamaNativeBase), and the shared gate for every
// check that is only meaningful — or only affordable — against a local model
// server: reachability below, and the P34.2 tool-calling probe.
func (s *Server) isOllamaProvider() bool {
	p := s.cfg.Provider
	return strings.EqualFold(p.Default, "ollama") || strings.Contains(p.BaseURL, ":11434")
}

// reachCacheTTL is the freshness window for the P35.11 reachability-probe
// cache. Within this window repeated /status polls reuse the last probe's
// (reachable, latencyMS) instead of firing a fresh live GET /api/version at
// Ollama. 3s is chosen to sit just above the UI's own 1-2s poll cadence: it
// coalesces a fast poll loop to at most one upstream request per window while
// staying short enough that a provider going up or down is reflected within a
// poll or two — the same freshness the UI can actually render. It is not so
// long that a genuine reachability change lingers stale on screen.
const reachCacheTTL = 3 * time.Second

// reachEntry is one cached reachability-probe result: the (reachable,
// latencyMS) pair probeProviderReachability returns plus the wall-clock time
// it was measured. The zero value (at == zero time) is treated as "no cached
// probe yet", so the first call always probes.
type reachEntry struct {
	reachable bool
	latencyMS int64
	at        time.Time
}

// reachClock returns the current time, honoring the reachNow test seam.
func (s *Server) reachClock() time.Time {
	if s.reachNow != nil {
		return s.reachNow()
	}
	return time.Now()
}

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
//
// P35.11: the result is cached under reachCacheMu for reachCacheTTL so a fast
// /status poll loop (1-2s) coalesces to at most one upstream GET /api/version
// per window. The cache covers every path uniformly — the API-key path is a
// cheap in-memory read that gains nothing from caching, but caching it too
// keeps this one code path and one lock, and its result is equally stable.
func (s *Server) probeProviderReachability(ctx context.Context) (reachable bool, latencyMS int64) {
	now := s.reachClock()

	// Fast path: reuse a still-fresh cached probe. Held under the same lock as
	// the write below so a concurrent burst of /status polls sees a consistent
	// entry and only one of them falls through to the live probe per window.
	s.reachCacheMu.Lock()
	if !s.reachCache.at.IsZero() && now.Sub(s.reachCache.at) < reachCacheTTL {
		e := s.reachCache
		s.reachCacheMu.Unlock()
		return e.reachable, e.latencyMS
	}
	s.reachCacheMu.Unlock()

	// Cache miss or expired: do the actual probe outside the lock (it may block
	// on a 2s network timeout — holding reachCacheMu across it would serialize
	// every concurrent /status behind one slow probe). A brief race where two
	// polls both probe on the same expiry tick is harmless: both write an
	// equivalent fresh result and subsequent polls within the window coalesce.
	reachable, latencyMS = s.doProbeReachability(ctx)

	s.reachCacheMu.Lock()
	s.reachCache = reachEntry{reachable: reachable, latencyMS: latencyMS, at: now}
	s.reachCacheMu.Unlock()
	return reachable, latencyMS
}

// doProbeReachability is the uncached probe — the original
// probeProviderReachability body — split out so the P35.11 cache can wrap it.
func (s *Server) doProbeReachability(ctx context.Context) (reachable bool, latencyMS int64) {
	p := s.cfg.Provider
	if s.isOllamaProvider() {
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
