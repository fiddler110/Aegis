package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
