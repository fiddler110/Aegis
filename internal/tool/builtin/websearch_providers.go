package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	return parseBraveResults(body, max)
}

func parseBraveResults(body []byte, max int) ([]searchResult, error) {
	var out struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
				PageAge     string `json:"page_age"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var results []searchResult
	for _, r := range out.Web.Results {
		// P71.7: page_age is the page's own publication/update date; age is a
		// display-oriented freshness value. Prefer page_age, fall back to age
		// — either beats the zero-config scrape's total absence of a signal.
		date := r.PageAge
		if date == "" {
			date = r.Age
		}
		results = append(results, searchResult{title: collapse(r.Title), urlStr: r.URL, snippet: collapse(stripHTML(r.Description)), date: date})
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
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"publishedDate"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var results []searchResult
	for _, r := range out.Results {
		// P71.7: SearXNG's JSON schema always carries publishedDate, but it is
		// only populated when the underlying engine reports one — many
		// results leave it null, so this is a bonus signal, not a guarantee.
		results = append(results, searchResult{title: collapse(r.Title), urlStr: r.URL, snippet: collapse(r.Content), date: r.PublishedDate})
		if len(results) >= max {
			break
		}
	}
	return results, nil
}

// doSearchRequest runs req, retrying transient failures per P71.3 (webRetries
// attempts beyond the first, webRetryable's 429/5xx rule, Retry-After
// honored when the provider sends one). A request whose body can't be
// re-read on retry (req.GetBody nil — a caller that built its own io.Reader
// body rather than going through http.NewRequest's bytes.Reader/strings.Reader
// auto-detection) gets exactly one attempt, same as before this existed;
// every provider in this file uses bytes.NewReader or has no body, so this
// never actually triggers today, but it is the correct fallback if one ever
// doesn't.
func doSearchRequest(req *http.Request) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		body, status, retryAfter, err := doSearchRequestOnce(req)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if attempt >= webRetries || (status > 0 && !webRetryable(status)) {
			return nil, lastErr
		}
		if req.GetBody != nil {
			rc, gerr := req.GetBody()
			if gerr != nil {
				return nil, lastErr
			}
			req.Body = rc
		} else if req.Body != nil {
			return nil, lastErr
		}
		if serr := sleepCtx(req.Context(), webBackoff(attempt, retryAfter)); serr != nil {
			return nil, serr
		}
	}
}

// doSearchRequestOnce is the single-attempt request doSearchRequest retries
// around. status is 0 for a transport-level failure, matching
// fetchTool.getOnce's convention; retryAfter is parsed from the response's
// Retry-After header (seconds form only — every provider here uses it that
// way) when present.
func doSearchRequestOnce(req *http.Request) (body []byte, status int, retryAfter time.Duration, err error) {
	resp, err := searchAPIClient.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxWebBytes))
	if err != nil {
		return nil, resp.StatusCode, 0, err
	}
	if resp.StatusCode >= 400 {
		if secs, perr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); perr == nil && secs > 0 {
			retryAfter = time.Duration(secs) * time.Second
		}
		return nil, resp.StatusCode, retryAfter, fmt.Errorf("status %d: %s", resp.StatusCode, clip(strings.TrimSpace(string(body)), 200))
	}
	return body, resp.StatusCode, 0, nil
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
