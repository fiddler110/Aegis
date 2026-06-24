// Package discover probes for locally running model servers (Ollama, LM Studio,
// LiteLLM) and reports their available models. This lets the harness auto-detect
// local models without manual configuration.
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Model represents a discovered model.
type Model struct {
	Name     string `json:"name"`
	Provider string `json:"provider"` // "ollama", "lmstudio", "litellm"
	Endpoint string `json:"endpoint"` // base URL to use for API calls
}

// Source is a local model provider to probe.
type Source struct {
	Name     string // "ollama", "lmstudio", "litellm"
	Endpoint string // base URL to probe
	Probe    func(ctx context.Context, endpoint string) ([]Model, error)
}

// DefaultSources returns the standard local model sources to probe.
func DefaultSources() []Source {
	return []Source{
		{Name: "ollama", Endpoint: "http://localhost:11434", Probe: probeOllama},
		{Name: "lmstudio", Endpoint: "http://localhost:1234", Probe: probeLMStudio},
		{Name: "litellm", Endpoint: "http://localhost:4000", Probe: probeLiteLLM},
	}
}

// Discover probes all sources and returns discovered models. Sources that are
// not running are silently skipped. The timeout is per-source.
func Discover(ctx context.Context, sources []Source, timeout time.Duration) []Model {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	var all []Model
	for _, src := range sources {
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		models, err := src.Probe(probeCtx, src.Endpoint)
		cancel()
		if err != nil {
			continue
		}
		all = append(all, models...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Provider != all[j].Provider {
			return all[i].Provider < all[j].Provider
		}
		return all[i].Name < all[j].Name
	})
	return all
}

// probeOllama checks the Ollama API for available models.
func probeOllama(ctx context.Context, endpoint string) ([]Model, error) {
	body, err := httpGet(ctx, endpoint+"/api/tags")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var out []Model
	for _, m := range resp.Models {
		out = append(out, Model{
			Name:     m.Name,
			Provider: "ollama",
			Endpoint: endpoint,
		})
	}
	return out, nil
}

// probeLMStudio checks LM Studio's OpenAI-compatible API.
func probeLMStudio(ctx context.Context, endpoint string) ([]Model, error) {
	return probeOpenAICompat(ctx, endpoint, "lmstudio")
}

// probeLiteLLM checks LiteLLM's OpenAI-compatible API.
func probeLiteLLM(ctx context.Context, endpoint string) ([]Model, error) {
	return probeOpenAICompat(ctx, endpoint, "litellm")
}

// probeOpenAICompat probes an OpenAI-compatible /v1/models endpoint.
func probeOpenAICompat(ctx context.Context, endpoint, provider string) ([]Model, error) {
	url := strings.TrimRight(endpoint, "/") + "/v1/models"
	body, err := httpGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var out []Model
	for _, m := range resp.Data {
		out = append(out, Model{
			Name:     m.ID,
			Provider: provider,
			Endpoint: endpoint,
		})
	}
	return out, nil
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
