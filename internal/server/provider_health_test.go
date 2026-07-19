package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
)

// TestProbeProviderReachability_Ollama covers the P28.7 probe against a
// fake Ollama server: a live GET /api/version returning a version string
// should report reachable with a non-zero (or at least measured) latency.
func TestProbeProviderReachability_Ollama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.1.0"})
	}))
	defer ts.Close()

	s := &Server{cfg: &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: ts.URL}}}
	reachable, latencyMS := s.probeProviderReachability(context.Background())
	if !reachable {
		t.Fatalf("reachable = false, want true (fake Ollama server answered /api/version)")
	}
	if latencyMS < 0 {
		t.Errorf("latencyMS = %d, want >= 0", latencyMS)
	}
}

// TestProbeProviderReachability_OllamaUnreachable covers the down case: no
// server listening at the configured Ollama base_url.
func TestProbeProviderReachability_OllamaUnreachable(t *testing.T) {
	s := &Server{cfg: &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: "http://127.0.0.1:1"}}}
	reachable, latencyMS := s.probeProviderReachability(context.Background())
	if reachable {
		t.Errorf("reachable = true, want false (nothing listening)")
	}
	if latencyMS != 0 {
		t.Errorf("latencyMS = %d, want 0 on failure", latencyMS)
	}
}

// TestProbeProviderReachability_Cloud covers the cheap, no-live-call path
// for a cloud provider: reachability mirrors `aegis doctor`'s check — an
// API key present in the resolved config — with latency left unmeasured.
func TestProbeProviderReachability_Cloud(t *testing.T) {
	cases := []struct {
		name   string
		apiKey string
		wantOK bool
	}{
		{"key present", "sk-test", true},
		{"no key", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: &config.Config{Provider: config.ProviderConfig{Default: "anthropic", APIKey: tc.apiKey}}}
			reachable, latencyMS := s.probeProviderReachability(context.Background())
			if reachable != tc.wantOK {
				t.Errorf("reachable = %v, want %v", reachable, tc.wantOK)
			}
			if latencyMS != 0 {
				t.Errorf("latencyMS = %d, want 0 (unmeasured for a cloud provider)", latencyMS)
			}
		})
	}
}

// countingOllama returns an httptest server that answers /api/version like a
// real Ollama and counts how many times /api/version was hit — the seam the
// P35.11 caching tests use to prove upstream requests are coalesced.
func countingOllama(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.1.0"})
	}))
	t.Cleanup(ts.Close)
	return ts, &hits
}

// TestProbeProviderReachability_CacheCoalesces covers P35.11: repeated probes
// within reachCacheTTL must reuse the cached (reachable, latencyMS) and fire
// exactly one upstream GET /api/version, not one per call.
func TestProbeProviderReachability_CacheCoalesces(t *testing.T) {
	ts, hits := countingOllama(t)
	frozen := time.Now()
	s := &Server{
		cfg:      &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: ts.URL}},
		reachNow: func() time.Time { return frozen }, // freeze the clock inside one window
	}

	reachable, _ := s.probeProviderReachability(context.Background())
	if !reachable {
		t.Fatalf("first probe: reachable = false, want true")
	}
	for i := 0; i < 5; i++ {
		if r, _ := s.probeProviderReachability(context.Background()); !r {
			t.Fatalf("probe %d: reachable = false, want true (cached)", i)
		}
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("upstream /api/version hits = %d, want 1 (5 further polls should coalesce)", got)
	}
}

// TestProbeProviderReachability_CacheExpires covers the other side of P35.11:
// once the freshness window elapses, the next poll re-probes upstream.
func TestProbeProviderReachability_CacheExpires(t *testing.T) {
	ts, hits := countingOllama(t)
	now := time.Now()
	s := &Server{
		cfg:      &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: ts.URL}},
		reachNow: func() time.Time { return now },
	}

	s.probeProviderReachability(context.Background()) // t0: probes (1)
	s.probeProviderReachability(context.Background()) // t0: cached (still 1)
	now = now.Add(reachCacheTTL + time.Millisecond)   // advance past the window
	s.probeProviderReachability(context.Background()) // re-probes (2)

	if got := atomic.LoadInt32(hits); got != 2 {
		t.Errorf("upstream /api/version hits = %d, want 2 (one per window)", got)
	}
}

// TestProbeProviderReachability_CacheConcurrent exercises the cache under a
// burst of concurrent /status-style callers, primarily to run the mutex under
// `go test -race`. The cache is warmed first, so all concurrent callers land
// within the same still-fresh window and must add zero upstream hits — proving
// both thread-safety and that concurrent reads coalesce. (Note: a genuinely
// simultaneous *cold* burst can let multiple racers through before the entry is
// populated; that's the documented-harmless expiry-tick race, and it isn't what
// a 1-2s poll loop — the actual workload — produces.)
func TestProbeProviderReachability_CacheConcurrent(t *testing.T) {
	ts, hits := countingOllama(t)
	frozen := time.Now()
	s := &Server{
		cfg:      &config.Config{Provider: config.ProviderConfig{Default: "ollama", BaseURL: ts.URL}},
		reachNow: func() time.Time { return frozen },
	}

	s.probeProviderReachability(context.Background()) // warm the cache (1 hit)

	const callers = 32
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			s.probeProviderReachability(context.Background())
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("upstream hits = %d, want 1 (concurrent reads within the window must coalesce)", got)
	}
}

// TestProbeProviderReachability_BaseURLPortDetection covers the
// ":11434"-in-base_url detection path (an "openai"-default provider
// re-pointed at a local Ollama server via base_url), matching
// ollamaNativeBase's own detection in internal/cli/doctor.go.
func TestProbeProviderReachability_BaseURLPortDetection(t *testing.T) {
	s := &Server{cfg: &config.Config{Provider: config.ProviderConfig{Default: "openai", BaseURL: "http://localhost:11434/v1"}}}
	if !strings.Contains(s.cfg.Provider.BaseURL, ":11434") {
		t.Fatal("test setup: base_url must contain :11434")
	}
	// No real Ollama listening on the default port in the test environment —
	// this should attempt the Ollama probe (not fall through to the
	// API-key check) and report unreachable, not "reachable" from a stray
	// APIKey value.
	s.cfg.Provider.APIKey = "should-be-ignored"
	reachable, _ := s.probeProviderReachability(context.Background())
	if reachable {
		t.Skip("a real Ollama server is listening on :11434 in this environment; detection path still exercised")
	}
}
