package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchGlobalProviderEmitsContextWindow verifies a non-zero ContextWindow
// is written as a context_window: line and round-trips through Load (P35.3): a
// skill-driven Ollama run needs num_ctx raised above the Modelfile default, so
// the generated config must carry the sizing rather than leaving it implicit.
func TestPatchGlobalProviderEmitsContextWindow(t *testing.T) {
	redirectConfigDir(t)

	if err := PatchGlobalProvider(ProviderPatch{
		Adapter:       "ollama",
		BaseURL:       "http://localhost:11434",
		Model:         "qwen3.6:35b-a3b-fast",
		MaxTokens:     8192,
		ContextWindow: 65536,
	}); err != nil {
		t.Fatalf("PatchGlobalProvider: %v", err)
	}

	data, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "context_window: 65536") {
		t.Errorf("context_window not written:\n%s", data)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Provider.ContextWindow != 65536 {
		t.Errorf("reloaded context_window = %d, want 65536", cfg.Provider.ContextWindow)
	}
}

// TestPatchGlobalProviderOmitsZeroContextWindow guards backward compatibility:
// callers that don't set ContextWindow (every pre-P35.3 caller, and cloud
// adapters) must produce the same block as before, with no context_window line.
func TestPatchGlobalProviderOmitsZeroContextWindow(t *testing.T) {
	redirectConfigDir(t)

	if err := PatchGlobalProvider(ProviderPatch{
		Adapter:   "anthropic",
		Model:     "claude-opus-4-8",
		MaxTokens: 1000,
	}); err != nil {
		t.Fatalf("PatchGlobalProvider: %v", err)
	}

	data, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "context_window") {
		t.Errorf("context_window should be omitted when unset:\n%s", data)
	}
}

// TestPatchGlobalProviderKeepsUnownedKeys pins the data-loss fix: the splice
// used to replace the whole provider: block, so saving /config deleted a
// configured fallback chain, gateway headers and every other key the wizard
// does not render.
func TestPatchGlobalProviderKeepsUnownedKeys(t *testing.T) {
	redirectConfigDir(t)
	path := GlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := `provider:
  default: anthropic
  model: "claude-opus-4-8"
  # gateway auth
  headers:
    X-Team: platform
  fallback:
    - provider: openai
      model: gpt-5
  keep_alive: 30m
  think: true
  max_tokens: 8000

cost:
  budget_usd: 5
`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PatchGlobalProvider(ProviderPatch{Adapter: "anthropic", Model: "claude-opus-5-5", MaxTokens: 16000, ReasoningEffort: "high"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	for _, want := range []string{"# gateway auth", "  headers:\n    X-Team: platform", "  fallback:\n    - provider: openai\n      model: gpt-5", "keep_alive: 30m", `model: "claude-opus-5-5"`, `reasoning_effort: "high"`, "budget_usd: 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, gone := range []string{"claude-opus-4-8", "max_tokens: 8000", "think: true"} {
		if strings.Contains(out, gone) {
			t.Errorf("owned key %q was not replaced:\n%s", gone, out)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("rewritten config does not load: %v\n%s", err, out)
	}
	if len(cfg.Provider.Fallback) != 1 || cfg.Provider.Headers["X-Team"] != "platform" || cfg.Provider.ReasoningEffort != "high" {
		t.Errorf("loaded provider = %+v", cfg.Provider)
	}
}
