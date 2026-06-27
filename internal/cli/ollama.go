package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/config"
)

// unloadOllamaModel sends keep_alive=0 to the Ollama native API, which
// immediately evicts the model from GPU/RAM. This is best-effort — errors
// are silently ignored because the user is already exiting.
//
// The call is skipped if the configured provider does not look like Ollama.
func unloadOllamaModel(cfg *config.Config) {
	base := ollamaNativeBase(cfg)
	if base == "" || cfg.Provider.Model == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"model":      cfg.Provider.Model,
		"keep_alive": 0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// ollamaNativeBase returns the native Ollama base URL (e.g. "http://localhost:11434")
// when the provider config points at an Ollama instance, or "" otherwise.
//
// Detection rules:
//  1. provider.default == "ollama"  → use base_url if set, else the default port
//  2. provider.base_url contains ":11434"  → strip the trailing /v1 OpenAI-compat suffix
func ollamaNativeBase(cfg *config.Config) string {
	isOllama := strings.EqualFold(cfg.Provider.Default, "ollama") ||
		strings.Contains(cfg.Provider.BaseURL, ":11434")
	if !isOllama {
		return ""
	}
	base := cfg.Provider.BaseURL
	if base == "" {
		return "http://localhost:11434"
	}
	// Strip the /v1 suffix added by the OpenAI-compat adapter path.
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/v1")
	return base
}
