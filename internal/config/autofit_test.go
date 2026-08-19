package config

import (
	"strings"
	"testing"
)

// autofit_context is the permission to replace a configured context_window, so
// it defaults off for the same reason vram_budget_gb defaults to 0: an existing
// install must keep serving the window it was serving.
func TestAutofitContextDefaultsOff(t *testing.T) {
	redirectConfigDir(t)
	clearEnv(t, "AEGIS_PROVIDER_AUTOFIT_CONTEXT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.AutofitContext {
		t.Error("autofit_context = true by default; a configured context_window must be left alone unless asked")
	}
}

func TestAutofitContextLoadsFromEnv(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_PROVIDER_AUTOFIT_CONTEXT", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Provider.AutofitContext {
		t.Error("autofit_context did not load from the environment")
	}
}

// It is a property of the operator's machine, not of the work in a repo — the
// same reason vram_budget_gb is frozen. A project that could set it could hand
// itself the override of a hand-tuned window the flag exists to gate.
func TestAutofitContextIsFrozenFromProjectConfig(t *testing.T) {
	if policyFor("provider.autofit_context") == projectSettable {
		t.Error("provider.autofit_context is project-settable; it must stay frozen until trusted")
	}
}

func TestProviderBlockOmitsAutofitUnlessAsked(t *testing.T) {
	block := buildProviderBlock(ProviderPatch{
		Adapter: "ollama", BaseURL: "http://localhost:11434",
		Model: "qwen", MaxTokens: 8192, MaxRetries: 4,
		ContextWindow: 25600, VRAMBudgetGB: 14.5,
	})
	if strings.Contains(block, "autofit_context") {
		t.Errorf("autofit_context written without being asked for:\n%s", block)
	}
}

func TestProviderBlockWritesAutofitWhenAsked(t *testing.T) {
	block := buildProviderBlock(ProviderPatch{
		Adapter: "ollama", BaseURL: "http://localhost:11434",
		Model: "qwen", MaxTokens: 8192, MaxRetries: 4,
		ContextWindow: 25600, VRAMBudgetGB: 14.5, AutofitContext: true,
	})
	if !strings.Contains(block, "autofit_context: true") {
		t.Errorf("autofit_context not written:\n%s", block)
	}
	// The flag says the window above it is provisional, so it has to be readable
	// as that rather than as one more opaque toggle.
	if !strings.Contains(block, "# Re-solve context_window") {
		t.Errorf("autofit_context written with no explanation:\n%s", block)
	}
}
