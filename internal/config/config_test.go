package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"provider default", cfg.Provider.Default, "anthropic"},
		{"provider model", cfg.Provider.Model, "claude-opus-4-8"},
		{"server addr", cfg.Server.Addr, "127.0.0.1:4127"},
		{"permission mode", cfg.Permission.Mode, "plan"},
		{"log level", cfg.LogLevel, "info"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if cfg.Provider.MaxTokens != 8192 {
		t.Errorf("max tokens = %d, want 8192", cfg.Provider.MaxTokens)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("AEGIS_PROVIDER_MODEL", "claude-sonnet-4-6")
	t.Setenv("AEGIS_PERMISSION_MODE", "build")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", cfg.Provider.Model)
	}
	if cfg.Permission.Mode != "build" {
		t.Errorf("mode = %q, want build", cfg.Permission.Mode)
	}
}

func TestAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-123")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider.APIKey != "sk-test-123" {
		t.Errorf("api key not read from env, got %q", cfg.Provider.APIKey)
	}
}
