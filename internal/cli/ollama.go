package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/modelpick"
	"github.com/fiddler110/aegis/internal/ollamainfo"
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
	// ollamainfo.Unload is the same request the daemon sends when a co-resident
	// plan ends (P72.3). One spelling of "this model is finished with", so a
	// change to how residency is released reaches both paths.
	_ = ollamainfo.Unload(context.Background(), base, cfg.Provider.Model)
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
// cfg.Provider.Model to the best model available on the Ollama instance —
// modelpick.Select's ranking, not an arbitrary listing order — mutating
// cfg.Provider.Model in place. Does nothing when the provider is not Ollama or
// the model name is already set to a non-sentinel value.
func resolveOllamaModel(cfg *config.Config) error {
	base := ollamaNativeBase(cfg)
	if base == "" {
		return nil
	}
	if cfg.Provider.Model != "auto" && cfg.Provider.Model != "" {
		return nil
	}
	models, err := discoverOllamaModels(base, 5*time.Second)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("model: auto — no models available in Ollama; pull one first: ollama pull <model>")
	}
	// "auto" used to mean tags[0] — Ollama's most-recently-*modified* model,
	// which is whatever was pulled or touched last and has nothing to do with
	// which model this machine should run. It goes through the same ranking
	// --first-init and the /config wizard use, so all three agree.
	sel := modelpick.Select(pickModels(models), hwinfo.Detect(), cfg.Provider.VRAMBudgetGB)
	if sel.Main == "" {
		return fmt.Errorf("model: auto — no model in Ollama can serve chat completions; pull one first: ollama pull <model>")
	}
	cfg.Provider.Model = sel.Main
	return nil
}

// ollamaModelInfo is one entry from GET /api/tags. Details and ModifiedAt are
// carried because the model ranking (internal/modelpick) needs the parameter
// count to rank on and the timestamp as its final tiebreak — /api/tags already
// returns both, so reading them costs nothing beyond the fields.
type ollamaModelInfo struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	Capabilities []string  `json:"capabilities"`
	ModifiedAt   time.Time `json:"modified_at"`
	Details      struct {
		Family        string `json:"family"`
		ParameterSize string `json:"parameter_size"`
		Quantization  string `json:"quantization_level"`
	} `json:"details"`
}

// pick converts to the ranking package's model type.
func (m ollamaModelInfo) pick() modelpick.Model {
	return modelpick.Model{
		Name:          m.Name,
		Family:        m.Details.Family,
		ParameterSize: m.Details.ParameterSize,
		Quantization:  m.Details.Quantization,
		SizeBytes:     m.Size,
		Capabilities:  m.Capabilities,
		ModifiedAt:    m.ModifiedAt,
	}
}

// pickModels converts a whole /api/tags listing for ranking.
func pickModels(models []ollamaModelInfo) []modelpick.Model {
	out := make([]modelpick.Model, len(models))
	for i, m := range models {
		out[i] = m.pick()
	}
	return out
}

// chatCapable reports whether m can serve chat completions — i.e. is not an
// embedding-only model. /api/tags reports embedding models (e.g.
// nomic-embed-text) with capabilities: ["embedding"] and no "completion", and
// they are typically the smallest thing pulled, which made them the accidental
// "smallest model" pick before this check existed. A model that reports no
// capabilities at all (older Ollama servers) is assumed chat-capable rather
// than excluded, since absence of the field is not evidence either way.
func (m ollamaModelInfo) chatCapable() bool { return m.pick().ChatCapable() }

// discoverOllamaModels lists the models currently pulled into an Ollama
// instance at base, in whatever order /api/tags reports them. That order is
// most-recently-modified first and carries no quality signal, so every caller
// ranks the result through internal/modelpick rather than taking the head.
func discoverOllamaModels(base string, timeout time.Duration) ([]ollamaModelInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list ollama models: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ollama models: %w", err)
	}
	var result struct {
		Models []ollamaModelInfo `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode ollama models: %w", err)
	}
	return result.Models, nil
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
