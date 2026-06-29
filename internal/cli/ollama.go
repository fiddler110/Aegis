package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
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

// ollamaHealthy returns true when the Ollama native API at base responds to a
// GET /api/tags within one second.
func ollamaHealthy(base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ensureOllamaRunning checks whether Ollama is reachable for this config. If
// not, it attempts to start "ollama serve" as a child process and waits up to
// 15 s for it to become ready. The returned stop func kills the child process;
// it is a no-op when Ollama was already running or the provider is not Ollama.
func ensureOllamaRunning(cfg *config.Config) (stop func(), err error) {
	base := ollamaNativeBase(cfg)
	if base == "" {
		return func() {}, nil
	}
	if ollamaHealthy(base) {
		return func() {}, nil
	}
	cmd := exec.Command("ollama", "serve")
	if startErr := cmd.Start(); startErr != nil {
		return nil, fmt.Errorf("start ollama: %w (is ollama installed and in PATH?)", startErr)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ollamaHealthy(base) {
			return func() { _ = cmd.Process.Kill() }, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("ollama did not become ready within 15 s")
}

// resolveOllamaModel resolves the sentinel values "auto" and "" in
// cfg.Provider.Model to the first model available on the Ollama instance,
// mutating cfg.Provider.Model in place. Does nothing when the provider is not
// Ollama or the model name is already set to a non-sentinel value.
func resolveOllamaModel(cfg *config.Config) error {
	base := ollamaNativeBase(cfg)
	if base == "" {
		return nil
	}
	if cfg.Provider.Model != "auto" && cfg.Provider.Model != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("list ollama models: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read ollama models: %w", err)
	}
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode ollama models: %w", err)
	}
	if len(result.Models) == 0 {
		return fmt.Errorf("model: auto — no models available in Ollama; pull one first: ollama pull <model>")
	}
	cfg.Provider.Model = result.Models[0].Name
	return nil
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
