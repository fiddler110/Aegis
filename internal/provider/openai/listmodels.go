package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/fiddler110/aegis/internal/provider"
)

var _ provider.ModelLister = (*Adapter)(nil)

// ListModels implements provider.ModelLister over GET <base>/models — the one
// listing endpoint every OpenAI-compatible server (OpenAI, OpenRouter, Groq,
// LM Studio, vLLM) shares. The shared shape carries ids only; OpenRouter adds
// context_length, which is read when present. Sorted by id for a stable
// picker.
func (a *Adapter) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
	resp, err := provider.HealthClient(a.client).Do(req)
	if err != nil {
		return nil, provider.NewTransportError(a.Name(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, provider.NewTransportError(a.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, provider.NewHTTPError(a.Name(), resp.StatusCode, resp.Header.Get("retry-after"), string(body))
	}
	var list struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("%s: decode /models: %w", a.Name(), err)
	}
	out := make([]provider.ModelInfo, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID == "" {
			continue
		}
		out = append(out, provider.ModelInfo{ID: m.ID, DisplayName: m.Name, ContextWindow: m.ContextLength})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
