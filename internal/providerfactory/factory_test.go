package providerfactory

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/config"
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
