package providerfactory

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
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
	if a.Name() != "openai" { // ollama uses the OpenAI-compatible adapter
		t.Fatalf("got name %q, want openai (ollama adapter)", a.Name())
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
