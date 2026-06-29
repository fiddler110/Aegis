package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scottymacleod/aegis/internal/config"
)

func TestResolveOllamaModel(t *testing.T) {
	t.Run("auto picks first available model", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "gemma3:12b"},
					{"name": "llama3.2"},
				},
			})
		}))
		defer srv.Close()

		cfg := &config.Config{}
		cfg.Provider.Default = "ollama"
		cfg.Provider.BaseURL = srv.URL + "/v1"
		cfg.Provider.Model = "auto"

		if err := resolveOllamaModel(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Provider.Model != "gemma3:12b" {
			t.Errorf("got model %q, want %q", cfg.Provider.Model, "gemma3:12b")
		}
	})

	t.Run("empty model picks first available", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "phi4"}},
			})
		}))
		defer srv.Close()

		cfg := &config.Config{}
		cfg.Provider.Default = "ollama"
		cfg.Provider.BaseURL = srv.URL + "/v1"
		cfg.Provider.Model = ""

		if err := resolveOllamaModel(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Provider.Model != "phi4" {
			t.Errorf("got model %q, want %q", cfg.Provider.Model, "phi4")
		}
	})

	t.Run("explicit model is left unchanged", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Provider.Default = "ollama"
		cfg.Provider.BaseURL = "http://localhost:11434/v1"
		cfg.Provider.Model = "mistral"

		if err := resolveOllamaModel(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Provider.Model != "mistral" {
			t.Errorf("model should be unchanged, got %q", cfg.Provider.Model)
		}
	})

	t.Run("non-ollama provider is a no-op", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Provider.Default = "anthropic"
		cfg.Provider.Model = "auto"

		if err := resolveOllamaModel(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Provider.Model != "auto" {
			t.Errorf("model should be unchanged for non-ollama provider, got %q", cfg.Provider.Model)
		}
	})

	t.Run("auto with no models returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
		}))
		defer srv.Close()

		cfg := &config.Config{}
		cfg.Provider.Default = "ollama"
		cfg.Provider.BaseURL = srv.URL + "/v1"
		cfg.Provider.Model = "auto"

		if err := resolveOllamaModel(cfg); err == nil {
			t.Fatal("expected error for empty model list, got nil")
		}
	})
}

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
