package providerfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func TestBuild_NoFallbackReturnsPlainAdapter(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "anthropic", APIKey: "fake-key", MaxRetries: 2}}
	a, err := Build(cfg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.Name() != "anthropic" {
		t.Fatalf("got name %q, want anthropic", a.Name())
	}
}

func TestBuild_MissingAPIKeyErrors(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{Default: "anthropic", MaxRetries: 2}}
	if _, err := Build(cfg, nil); err == nil {
		t.Fatal("expected error for missing ANTHROPIC_API_KEY")
	}
}

func TestBuild_LocalToCloudFallbackSkippedWithoutOptIn(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default:    "ollama",
			MaxRetries: 2,
			Fallback:   []config.ProviderFallbackConfig{{Provider: "anthropic", Model: "claude-x"}},
			// AllowCloudFallback intentionally left false.
		},
	}
	a, err := Build(cfg, testLogger(&buf))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.Name() != "ollama" { // ollama uses its native /api/chat adapter (P33.9)
		t.Fatalf("got name %q, want ollama", a.Name())
	}
	if !strings.Contains(buf.String(), "skipping cloud fallback") {
		t.Fatalf("expected a warning about the skipped cloud fallback, got log: %s", buf.String())
	}
}

func TestBuild_LocalToCloudFallbackAllowedWithOptIn(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default:            "ollama",
			MaxRetries:         2,
			AllowCloudFallback: true,
			Fallback:           []config.ProviderFallbackConfig{{Provider: "anthropic", Model: "claude-x"}},
		},
	}
	if _, err := Build(cfg, testLogger(&buf)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(buf.String(), "skipping cloud fallback") {
		t.Fatalf("did not expect the cloud fallback to be skipped when opted in, got log: %s", buf.String())
	}
}

func TestBuild_CloudToLocalFallbackNeverGated(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default:    "anthropic",
			APIKey:     "fake-key",
			MaxRetries: 2,
			Fallback:   []config.ProviderFallbackConfig{{Provider: "ollama", Model: "llama3"}},
			// AllowCloudFallback left false — must not matter for cloud->local.
		},
	}
	if _, err := Build(cfg, testLogger(&buf)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(buf.String(), "skipping") {
		t.Fatalf("cloud->local fallback must never be gated, got log: %s", buf.String())
	}
}

// TestBuild_RefusesPlaintextHTTPNonLoopbackWithRealKey is the P27.2/FIND-03
// regression: a non-loopback plaintext-HTTP base_url must not be allowed to
// carry a real API key.
func TestBuild_RefusesPlaintextHTTPNonLoopbackWithRealKey(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "anthropic", APIKey: "sk-real-secret", BaseURL: "http://attacker.example/v1", MaxRetries: 2,
	}}
	_, err := Build(cfg, nil)
	if err == nil {
		t.Fatal("expected an error for plaintext HTTP to a non-loopback host with a real API key")
	}
	if !strings.Contains(err.Error(), "plaintext HTTP") {
		t.Errorf("error should explain the plaintext-HTTP refusal, got: %v", err)
	}
}

// TestBuild_AllowsPlaintextHTTPLoopback confirms the refusal above doesn't
// break the common local/LAN Ollama-over-HTTP setup, which never carries a
// real credential.
func TestBuild_AllowsPlaintextHTTPLoopback(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "ollama", BaseURL: "http://localhost:11434/v1", MaxRetries: 2,
	}}
	if _, err := Build(cfg, nil); err != nil {
		t.Fatalf("loopback http base_url should be allowed: %v", err)
	}
}

// TestBuild_WarnsOnNonDefaultCloudHost confirms a non-default HTTPS host for
// a cloud provider is allowed through (many legitimate gateway/proxy setups
// exist) but logs a prominent warning rather than silently sending the key
// there.
func TestBuild_WarnsOnNonDefaultCloudHost(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "anthropic", APIKey: "sk-real-secret", BaseURL: "https://gateway.example.com/v1", MaxRetries: 2,
	}}
	if _, err := Build(cfg, testLogger(&buf)); err != nil {
		t.Fatalf("non-default https host should be allowed: %v", err)
	}
	if !strings.Contains(buf.String(), "overrides the default API host") {
		t.Errorf("expected a warning about the non-default host, got log: %s", buf.String())
	}
}

// TestBuild_NoWarningForDefaultHost confirms the warning above doesn't fire
// for the provider's own real default host.
func TestBuild_NoWarningForDefaultHost(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "anthropic", APIKey: "sk-real-secret", BaseURL: "https://api.anthropic.com", MaxRetries: 2,
	}}
	if _, err := Build(cfg, testLogger(&buf)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(buf.String(), "overrides the default API host") {
		t.Errorf("should not warn for the provider's own default host, got log: %s", buf.String())
	}
}

func TestBuild_UnsupportedFallbackProviderSkippedNotFatal(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default:    "anthropic",
			APIKey:     "fake-key",
			MaxRetries: 2,
			Fallback:   []config.ProviderFallbackConfig{{Provider: "bogus", Model: "x"}},
		},
	}
	a, err := Build(cfg, testLogger(&buf))
	if err != nil {
		t.Fatalf("Build should not fail on one bad fallback entry: %v", err)
	}
	if a == nil {
		t.Fatal("expected a usable adapter despite the bad fallback entry")
	}
	if !strings.Contains(buf.String(), "misconfigured fallback") {
		t.Fatalf("expected a warning about the bad fallback, got log: %s", buf.String())
	}
}

// TestBuildOne_OllamaDefaultsKeepAliveResident is the P35.4 guard: an unset
// provider.keep_alive must be substituted with the bounded resident default on
// the native path, so a multi-turn run reuses its KV cache across turns instead
// of the model unloading between turns and every turn reprocessing the whole
// conversation.
func TestBuildOne_OllamaDefaultsKeepAliveResident(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The adapter also reads /api/show once per model to detect the
		// tool-call-dropping chat template; only the chat request carries
		// keep_alive, so answer the probe and assert on nothing else.
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			KeepAlive *string `json:"keep_alive"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.KeepAlive == nil {
			t.Error("keep_alive must not be omitted when config leaves it unset (P35.4)")
		} else if *body.KeepAlive != defaultOllamaKeepAlive {
			t.Errorf("keep_alive = %q, want default %q", *body.KeepAlive, defaultOllamaKeepAlive)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	a, err := buildOne(buildOneConfig{name: "ollama", baseURL: srv.URL})
	if err != nil {
		t.Fatalf("buildOne: %v", err)
	}
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
}

// TestBuildOne_OllamaKeepAliveExplicitWins is the P33.10 half of the same
// contract: an explicit config value is passed through verbatim, including
// "-1" (pin forever) — the default only fills the unset case.
func TestBuildOne_OllamaKeepAliveExplicitWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The adapter also reads /api/show once per model to detect the
		// tool-call-dropping chat template; only the chat request carries
		// keep_alive, so answer the probe and assert on nothing else.
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			KeepAlive *string `json:"keep_alive"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.KeepAlive == nil || *body.KeepAlive != "-1" {
			t.Errorf("keep_alive = %v, want explicit %q", body.KeepAlive, "-1")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	a, err := buildOne(buildOneConfig{name: "ollama", baseURL: srv.URL, keepAlive: "-1"})
	if err != nil {
		t.Fatalf("buildOne: %v", err)
	}
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
}

// TestBuildOne_OllamaThinkDefaultsOn is the P77.1 wiring check: native Ollama
// requests `think` by default so a reasoning-capable local model's output
// reaches the TUI's collapsible thinking block without the user hand-editing
// config.yaml — local reasoning is unbilled, unlike the Anthropic thinking
// budget, and a model that rejects the parameter just 400s once (the ollama
// adapter's own thinkRejected latch handles the retry).
func TestBuildOne_OllamaThinkDefaultsOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Think *bool `json:"think"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Think == nil || !*body.Think {
			t.Errorf("think = %v, want true when config leaves it unset", body.Think)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	a, err := buildOne(buildOneConfig{name: "ollama", baseURL: srv.URL})
	if err != nil {
		t.Fatalf("buildOne: %v", err)
	}
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
}

// TestBuildOne_OllamaThinkExplicitFalseWins is the opt-out half of the same
// contract: an explicit `provider.think: false` must still suppress the
// parameter even though the default (above) is now on.
func TestBuildOne_OllamaThinkExplicitFalseWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Think *bool `json:"think"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Think == nil || *body.Think {
			t.Errorf("think = %v, want explicit false", body.Think)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	defer srv.Close()

	falseVal := false
	a, err := buildOne(buildOneConfig{name: "ollama", baseURL: srv.URL, think: &falseVal})
	if err != nil {
		t.Fatalf("buildOne: %v", err)
	}
	stream, err := a.Stream(context.Background(), provider.Request{
		Model:    "llama3.2",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range stream {
	}
}

// TestBuild_AdmissionBoundsLocalNotCloud is the P59.9 wiring check: the queue
// depth is resolved per backend, so an unconfigured local primary is bounded by
// default and an unconfigured cloud primary is not.
// TestBuild_RedactsOutboundOnCloudProvider is P81.5/FIND-05's plumbing
// guard: a non-loopback endpoint with the config default (on) gets the
// redaction decorator wired in.
func TestBuild_RedactsOutboundOnCloudProvider(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Default: "anthropic", APIKey: "fake-key", MaxRetries: 2},
		Security: config.SecurityConfig{RedactOutboundPayloads: true},
	}
	a, err := Build(cfg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !provider.IsRedacted(a) {
		t.Error("expected the redaction decorator on a non-loopback cloud provider")
	}
}

// TestBuild_NoRedactionOnLoopback: a local Ollama deployment sends nothing
// off the machine, so the decorator is skipped regardless of the config
// default — config.IsLoopbackBaseURL is the same gate MeteredCloudEndpoint
// (P81.15) uses to draw this line.
func TestBuild_NoRedactionOnLoopback(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Default: "ollama", BaseURL: "http://localhost:11434/v1", MaxRetries: 2},
		Security: config.SecurityConfig{RedactOutboundPayloads: true},
	}
	a, err := Build(cfg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if provider.IsRedacted(a) {
		t.Error("expected no redaction decorator on a loopback endpoint")
	}
}

// TestBuild_NoRedactionWhenDisabled: an operator who turned the setting off
// gets exactly that, even against a cloud endpoint.
func TestBuild_NoRedactionWhenDisabled(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Default: "anthropic", APIKey: "fake-key", MaxRetries: 2},
		Security: config.SecurityConfig{RedactOutboundPayloads: false},
	}
	a, err := Build(cfg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if provider.IsRedacted(a) {
		t.Error("expected no redaction decorator when security.redact_outbound_payloads is false")
	}
}

func TestBuild_AdmissionBoundsLocalNotCloud(t *testing.T) {
	local, err := Build(&config.Config{Provider: config.ProviderConfig{Default: "ollama", MaxRetries: 2}}, nil)
	if err != nil {
		t.Fatalf("Build(ollama): %v", err)
	}
	if got := provider.AdmissionDepth(local); got != config.MaxConcurrentRequestsDefaultLocal {
		t.Errorf("local admission depth = %d, want %d", got, config.MaxConcurrentRequestsDefaultLocal)
	}

	cloud, err := Build(&config.Config{Provider: config.ProviderConfig{Default: "anthropic", APIKey: "fake-key", MaxRetries: 2}}, nil)
	if err != nil {
		t.Fatalf("Build(anthropic): %v", err)
	}
	if got := provider.AdmissionDepth(cloud); got != 0 {
		t.Errorf("cloud admission depth = %d, want 0 (unbounded)", got)
	}
}

// TestBuild_AdmissionExplicitValues: a positive value applies to any backend,
// and a negative one opts a local backend back out of the default bound.
func TestBuild_AdmissionExplicitValues(t *testing.T) {
	cloud, err := Build(&config.Config{Provider: config.ProviderConfig{
		Default: "anthropic", APIKey: "fake-key", MaxRetries: 2, MaxConcurrentRequests: 3,
	}}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := provider.AdmissionDepth(cloud); got != 3 {
		t.Errorf("explicit cloud admission depth = %d, want 3", got)
	}

	unbounded, err := Build(&config.Config{Provider: config.ProviderConfig{
		Default: "ollama", MaxRetries: 2, MaxConcurrentRequests: -1,
	}}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := provider.AdmissionDepth(unbounded); got != 0 {
		t.Errorf("explicitly unbounded local admission depth = %d, want 0", got)
	}
}

// TestBuild_AdmissionIsPerBackendAcrossFailover: a local primary must not hand
// its single-GPU queue depth to a cloud fallback target, and the cloud target
// must not leave the local primary unbounded either.
func TestBuild_AdmissionIsPerBackendAcrossFailover(t *testing.T) {
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default: "ollama", MaxRetries: 2, AllowCloudFallback: true,
		Fallback: []config.ProviderFallbackConfig{{Provider: "anthropic", Model: "claude-x"}},
	}}
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")
	a, err := Build(cfg, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := provider.AdmissionDepth(a); got != config.MaxConcurrentRequestsDefaultLocal {
		t.Errorf("primary (ollama) admission depth = %d, want %d", got, config.MaxConcurrentRequestsDefaultLocal)
	}

	// The failover decorator keeps its targets private, so assert the same
	// property on the function its construction loop calls: admit() is what
	// decides per target, and it must decide "unbounded" for the cloud one.
	base := &noopAdapter{name: "anthropic"}
	if got := provider.AdmissionDepth(admit(cfg, "anthropic", "", base, testLogger(&bytes.Buffer{}))); got != 0 {
		t.Errorf("cloud fallback admission depth = %d, want 0 (unbounded)", got)
	}
	if got := provider.AdmissionDepth(admit(cfg, "ollama", "", base, testLogger(&bytes.Buffer{}))); got != config.MaxConcurrentRequestsDefaultLocal {
		t.Errorf("local fallback admission depth = %d, want %d", got, config.MaxConcurrentRequestsDefaultLocal)
	}
}

// noopAdapter is a bare Adapter used where only the decorator chain matters.
type noopAdapter struct{ name string }

func (n *noopAdapter) Name() string { return n.name }
func (n *noopAdapter) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event)
	close(ch)
	return ch, nil
}
