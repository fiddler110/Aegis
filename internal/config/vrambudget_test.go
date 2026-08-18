package config

import "testing"

// The whole of P69.6 is inert until an operator states a budget. If the default
// ever became non-zero, every existing install would start planning co-resident
// windows against a number nobody chose.
func TestVRAMBudgetDefaultsToNoPlanning(t *testing.T) {
	redirectConfigDir(t)
	clearEnv(t, "AEGIS_PROVIDER_VRAM_BUDGET_GB", "AEGIS_PROVIDER_KV_CACHE_TYPE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.VRAMBudgetGB != 0 {
		t.Errorf("vram_budget_gb = %v, want 0 (planning is opt-in)", cfg.Provider.VRAMBudgetGB)
	}
	if got := cfg.Provider.VRAMBudgetBytes(); got != 0 {
		t.Errorf("VRAMBudgetBytes() = %d, want 0", got)
	}
	if cfg.Provider.KVCacheType != "f16" {
		t.Errorf("kv_cache_type = %q, want %q (Ollama's own default)", cfg.Provider.KVCacheType, "f16")
	}
}

func TestVRAMBudgetLoadsFromEnv(t *testing.T) {
	redirectConfigDir(t)
	t.Setenv("AEGIS_PROVIDER_VRAM_BUDGET_GB", "14.5")
	t.Setenv("AEGIS_PROVIDER_KV_CACHE_TYPE", "q8_0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.VRAMBudgetGB != 14.5 {
		t.Errorf("vram_budget_gb = %v, want 14.5", cfg.Provider.VRAMBudgetGB)
	}
	if want := int64(14.5 * float64(int64(1)<<30)); cfg.Provider.VRAMBudgetBytes() != want {
		t.Errorf("VRAMBudgetBytes() = %d, want %d", cfg.Provider.VRAMBudgetBytes(), want)
	}
	if !cfg.Provider.KVCacheTypeValid() {
		t.Error("q8_0 rejected as a KV cache type")
	}
}

// A mistyped budget is ignored rather than fatal: it is an opt-in hint about
// hardware, and refusing to start the daemon over it would be the worse trade.
// Zero means "plan nothing", which is exactly the behavior a negative figure
// should get.
func TestNegativeVRAMBudgetReadsAsNoBudget(t *testing.T) {
	p := ProviderConfig{VRAMBudgetGB: -4}
	if got := p.VRAMBudgetBytes(); got != 0 {
		t.Errorf("VRAMBudgetBytes() = %d for a negative budget, want 0", got)
	}
}

func TestKVCacheTypeValidity(t *testing.T) {
	for _, v := range []string{"", "f16", "q8_0", "q4_0"} {
		if !(ProviderConfig{KVCacheType: v}).KVCacheTypeValid() {
			t.Errorf("%q rejected", v)
		}
	}
	for _, v := range []string{"q8", "fp16", "int8", "nonsense"} {
		if (ProviderConfig{KVCacheType: v}).KVCacheTypeValid() {
			t.Errorf("%q accepted; a typo must be nameable, not silently treated as a working setting", v)
		}
	}
}
