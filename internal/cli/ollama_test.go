package cli

import (
	"testing"

	"github.com/scottymacleod/aegis/internal/config"
)

func TestOllamaNativeBase(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		baseURL  string
		want     string
	}{
		{"explicit ollama default, no base_url", "ollama", "", "http://localhost:11434"},
		{"explicit ollama default, with /v1 base_url", "ollama", "http://localhost:11434/v1", "http://localhost:11434"},
		{"openai compat pointing at 11434", "openai", "http://localhost:11434/v1", "http://localhost:11434"},
		{"trailing slash stripped", "ollama", "http://localhost:11434/v1/", "http://localhost:11434"},
		{"anthropic cloud — not ollama", "anthropic", "", ""},
		{"openai cloud — not ollama", "openai", "https://api.openai.com/v1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Provider.Default = tc.provider
			cfg.Provider.BaseURL = tc.baseURL
			got := ollamaNativeBase(cfg)
			if got != tc.want {
				t.Errorf("ollamaNativeBase() = %q, want %q", got, tc.want)
			}
		})
	}
}
