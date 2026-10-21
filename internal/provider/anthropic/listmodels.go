package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/fiddler110/aegis/internal/provider"
)

var _ provider.ModelLister = (*Adapter)(nil)

// listModelsMaxPages bounds pagination: the Models API lists a few dozen
// models, so a cursor that never ends is a broken gateway, not a long list.
const listModelsMaxPages = 10

// ListModels implements provider.ModelLister over GET /v1/models, following
// has_more/last_id pagination. Each entry carries the API's own context
// window (max_input_tokens) and output cap (max_tokens) when it reports them.
func (a *Adapter) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	if a.apiKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key (set ANTHROPIC_API_KEY)")
	}
	var out []provider.ModelInfo
	after := ""
	for page := 0; page < listModelsMaxPages; page++ {
		q := url.Values{"limit": {"100"}}
		if after != "" {
			q.Set("after_id", after)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/models?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", a.apiKey)
		req.Header.Set("anthropic-version", apiVersion)
		for k, v := range a.headers {
			req.Header.Set(k, v)
		}
		resp, err := provider.HealthClient(a.client).Do(req)
		if err != nil {
			return nil, provider.NewTransportError(providerName, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return nil, provider.NewTransportError(providerName, err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, provider.NewHTTPError(providerName, resp.StatusCode, resp.Header.Get("retry-after"), string(body))
		}
		var list struct {
			Data []struct {
				ID             string `json:"id"`
				DisplayName    string `json:"display_name"`
				MaxInputTokens int    `json:"max_input_tokens"`
				MaxTokens      int    `json:"max_tokens"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("anthropic: decode /v1/models: %w", err)
		}
		for _, m := range list.Data {
			out = append(out, provider.ModelInfo{
				ID: m.ID, DisplayName: m.DisplayName,
				ContextWindow: m.MaxInputTokens, MaxOutput: m.MaxTokens,
			})
		}
		if !list.HasMore || list.LastID == "" || list.LastID == after {
			break
		}
		after = list.LastID
	}
	return out, nil
}
