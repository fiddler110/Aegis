package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// searchAPIClient is used for configured search providers. Unlike ssrfClient it
// does not block private IPs, because a self-hosted SearXNG instance is commonly
// reachable only on a LAN/localhost address and the provider URL comes from
// trusted config, not model/attacker input.
var searchAPIClient = &http.Client{Timeout: 20 * time.Second}

// providerSearch dispatches to the configured search provider.
func (t *searchTool) providerSearch(ctx context.Context, provider, query string, max int) ([]searchResult, error) {
	switch provider {
	case "brave":
		return t.braveSearch(ctx, query, max)
	case "tavily":
		return t.tavilySearch(ctx, query, max)
	case "searxng":
		return t.searxngSearch(ctx, query, max)
	default:
		return nil, fmt.Errorf("unknown search provider %q", provider)
	}
}

func (t *searchTool) braveSearch(ctx context.Context, query string, max int) ([]searchResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("brave: missing api key (set search.api_key)")
	}
	endpoint := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d", url.QueryEscape(query), max)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)
	body, err := doSearchRequest(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var results []searchResult
	for _, r := range out.Web.Results {
		results = append(results, searchResult{title: collapse(r.Title), urlStr: r.URL, snippet: collapse(stripHTML(r.Description))})
		if len(results) >= max {
			break
		}
	}
	return results, nil
}

func (t *searchTool) tavilySearch(ctx context.Context, query string, max int) ([]searchResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("tavily: missing api key (set search.api_key)")
	}
	payload, _ := json.Marshal(map[string]any{
		"api_key":     t.apiKey,
		"query":       query,
		"max_results": max,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := doSearchRequest(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var results []searchResult
	for _, r := range out.Results {
		results = append(results, searchResult{title: collapse(r.Title), urlStr: r.URL, snippet: collapse(r.Content)})
		if len(results) >= max {
			break
		}
	}
	return results, nil
}

func (t *searchTool) searxngSearch(ctx context.Context, query string, max int) ([]searchResult, error) {
	if t.baseURL == "" {
		return nil, fmt.Errorf("searxng: missing base_url (set search.base_url)")
	}
	base := strings.TrimRight(t.baseURL, "/")
	endpoint := fmt.Sprintf("%s/search?q=%s&format=json", base, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	body, err := doSearchRequest(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var results []searchResult
	for _, r := range out.Results {
		results = append(results, searchResult{title: collapse(r.Title), urlStr: r.URL, snippet: collapse(r.Content)})
		if len(results) >= max {
			break
		}
	}
	return results, nil
}

func doSearchRequest(req *http.Request) ([]byte, error) {
	resp, err := searchAPIClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, clip(strings.TrimSpace(string(body)), 200))
	}
	return body, nil
}

// stripHTML removes tags from a snippet (Brave descriptions include <strong>).
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
