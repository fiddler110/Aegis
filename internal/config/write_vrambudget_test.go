package config

import (
	"strings"
	"testing"
)

// The budget line only appears when a budget was stated. An install that skipped
// the question must get a provider block byte-identical to the one written
// before P69.6 existed — that is what makes the whole feature safe to ship on by
// default.
func TestProviderBlockOmitsAnUnsetVRAMBudget(t *testing.T) {
	block := buildProviderBlock(ProviderPatch{
		Adapter: "ollama", BaseURL: "http://localhost:11434",
		Model: "qwen", MaxTokens: 8192, MaxRetries: 4,
	})
	if strings.Contains(block, "vram_budget_gb") {
		t.Errorf("an unset budget wrote a key:\n%s", block)
	}
}

func TestProviderBlockWritesAStatedVRAMBudget(t *testing.T) {
	block := buildProviderBlock(ProviderPatch{
		Adapter: "ollama", BaseURL: "http://localhost:11434",
		Model: "qwen", MaxTokens: 8192, MaxRetries: 4,
		ContextWindow: 25600, VRAMBudgetGB: 14.5,
	})
	if !strings.Contains(block, "vram_budget_gb: 14.5") {
		t.Errorf("budget not written:\n%s", block)
	}
	// The two keys answer halves of one question, so they belong together and in
	// this order: how big one model's window is, then how many models fit beside
	// it. A reader who finds only the first has the wrong mental model.
	if strings.Index(block, "context_window:") > strings.Index(block, "vram_budget_gb:") {
		t.Errorf("vram_budget_gb written above context_window:\n%s", block)
	}
	// The number looks arbitrary without its reason, the same way a fitted
	// context_window does — that is why PatchGlobalContextWindow is surgical.
	if !strings.Contains(block, "# Memory Aegis may assume") {
		t.Errorf("budget written with no explanation:\n%s", block)
	}
}
