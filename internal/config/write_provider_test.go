package config

import (
	"os"
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
