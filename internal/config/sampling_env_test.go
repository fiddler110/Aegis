package config

import (
	"testing"
)

// The sampling knobs are pointer-typed so "unset" stays distinguishable from
// "explicitly zero" — and zero is exactly what a deterministic run asks for,
// so an env override of 0 must survive as a set value.
func TestSamplingEnvOverrides(t *testing.T) {
	t.Setenv("AEGIS_PROVIDER_TEMPERATURE", "0")
	t.Setenv("AEGIS_PROVIDER_SEED", "42")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.Temperature == nil {
		t.Fatal("AEGIS_PROVIDER_TEMPERATURE=0 did not reach the config")
	}
	if *cfg.Provider.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", *cfg.Provider.Temperature)
	}
	if cfg.Provider.Seed == nil || *cfg.Provider.Seed != 42 {
		t.Errorf("seed = %v, want 42", cfg.Provider.Seed)
	}
}
