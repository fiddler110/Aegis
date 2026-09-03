package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/netblock"
	"github.com/fiddler110/aegis/internal/redact"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/trust"
	"golang.org/x/net/html"
)

const maxWebBytes = 2 << 20 // 2 MiB cap on fetched bodies

// ssrfClient is a shared HTTP client whose transport enforces SSRF protection
// on every new connection. Reusing one client allows TCP/TLS connection pooling
// across fetch and search calls. The blocklist and the dialer live in
// internal/netblock, which is also what the MCP HTTP client uses — P66.10
// collapsed the two copies that had drifted (VULN-03).
var ssrfClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: netblock.SafeDialer,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return netblock.ValidateNotPrivate(req.Context(), req.URL)
	},
}

// --- fetch ---

// scanOutput opts fetched/searched content into the heuristic
// prompt-injection scan (FIND-04, mirrors mcp_server's per-server
// scan_output). The provenance marker itself is always applied regardless.
type fetchTool struct {
	userAgent  string
	scanOutput bool
}

func (t *fetchTool) Name() string                { return "web_fetch" }
func (t *fetchTool) Capability() tool.Capability { return tool.CapNetwork }

// Replay: re-fetching a URL is a read of external state, not a mutation of
// it — safe to reissue (P65.4).
func (t *fetchTool) Replay(json.RawMessage) tool.ReplayClass { return tool.ReplaySafe }
func (t *fetchTool) Description() string {
	return "Fetch a URL over HTTP(S) and return its content as readable text (HTML is converted to text)."
}
func (t *fetchTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"url":{"type":"string"},"max_chars":{"type":"integer","description":"truncate output to this many characters (optional)"}},"required":["url"]}`)
}
func (t *fetchTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		URL      string `json:"url"`
		MaxChars int    `json:"max_chars"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	u, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return tool.Result{Content: "url must be a valid http(s) URL", IsError: true}, nil
	}
	// P81.8/FIND-08: the inbound half of web_fetch is thoroughly guarded
	// (internal/netblock blocks the destination), but nothing inspected the
	// payload — the model chooses the URL, so workspace-derived data can be
	// encoded into a path or query string aimed at an otherwise-legitimate
	// public host. Refuse outright rather than merely warn: unlike a
	// provider payload, there is no legitimate reason a fetch *destination*
	// should itself contain something matching a credential pattern.
	if classes := redact.Classes(u.String()); len(classes) > 0 {
		return tool.Result{
			Content: fmt.Sprintf(
				"refusing to fetch: the URL itself appears to contain %s — this looks like an attempt to exfiltrate a credential via the request path rather than a legitimate fetch target",
				strings.Join(classes, ", ")),
			IsError: true,
		}, nil
	}

	// P71.6: a cache hit skips the network round-trip (and the egress record,
	// since a cache hit moves no bytes) but still goes through the limit/
	// truncate/wrap steps below fresh — the cached value is the converted
	// text before truncation, precisely so a later call with a different
	// max_chars is served correctly from the same entry rather than getting
	// whatever length the first call happened to request. Keyed on the URL
	// alone (fragment stripped, since a fragment is client-side-only and two
	// URLs differing only by it are the same resource) — not on max_chars,
	// matching the cache's own key space to "the same page" rather than "the
	// same call".
	cache, _ := tool.WebCacheFromContext(ctx)
	cacheKey := normalizeFetchKey(u)
	var text string
	var cacheAge time.Duration
	var cacheHit bool
	if cache != nil {
		if cached, age, ok := cache.Get(cacheKey); ok {
			text, cacheAge, cacheHit = cached, age, true
		}
	}
	if !cacheHit {
		body, ctype, err := t.get(ctx, u.String())
		if err != nil {
			return tool.Result{Content: fmt.Sprintf("fetch failed: %v", err), IsError: true}, nil
		}
		// P81.8: the durable record of this fetch lives in the audit sink
		// (hooks.Audit); this is the live counterpart a TUI/UI reads without
		// parsing JSONL. Recorded on success only — a refused or failed fetch
		// moved no bytes.
		if tracker, ok := tool.EgressTrackerFromContext(ctx); ok {
			tracker.Add(u.Hostname(), len(body))
		}
		text = string(body)
		if strings.Contains(ctype, "html") {
			text = htmlToText(body)
		}
		if cache != nil {
			cache.Set(cacheKey, text)
		}
	}
	limit := defaultFetchLimit(ctx)
	if args.MaxChars > 0 {
		limit = args.MaxChars
	}
	// Head (P64.3): a fetched page's answer is above the fold — the tail of an
	// HTML-to-text render is navigation, footers and cookie prose.
	//
	// Deliberately NOT spilled (P64.1), and this is the one exclusion beyond
	// read_file's. The clipped text is about to be wrapped by trust.Wrap, which
	// is what tells the model these bytes are untrusted and are data rather
	// than instructions. Spilling would write the *unwrapped* remainder to a
	// workspace file that read_file returns with no wrapper at all — turning a
	// context-budget feature into a prompt-injection laundering path. The
	// recovery path here is a second fetch, which stays inside the wrapper.
	text, _ = TruncateHead(text, limit, "re-fetch with a larger max_chars, or fetch a more specific URL")
	attrs := [][2]string{{"url", u.String()}}
	if cacheHit {
		// P71.6: visible on the result itself, not just skipped silently — the
		// content may be stale (the actual concern raised against this
		// mechanism when it was filed), so the model can say so if asked and
		// re-fetch is always one call away.
		attrs = append(attrs, [2]string{"served_from_session_cache", fmt.Sprintf("fetched %s ago", cacheAge.Round(time.Second))})
	}
	wrapped := trust.Wrap("web_untrusted_output", attrs, "a URL fetched from the web", text, t.scanOutput)
	return tool.Result{Content: wrapped}, nil
}

// normalizeFetchKey returns the cache key for u: the URL with its fragment
// stripped, since a fragment is client-side-only (never sent to the server)
// and two URLs differing only by it name the same fetched resource.
func normalizeFetchKey(u *url.URL) string {
	c := *u
	c.Fragment = ""
	c.RawFragment = ""
	return c.String()
}

// maxFetchLimit is today's flat default — kept as the ceiling for any window
// roomy enough not to need shrinking, so a cloud-scale context sees no
// behavior change.
const maxFetchLimit = 20000

// minFetchLimit floors the scaled cap so even a very small serving window
// still gets a usable read rather than a near-empty one.
const minFetchLimit = 4000

// defaultFetchLimit sizes web_fetch's output cap (P71.5) from the run's
// resolved context window instead of a flat 20,000 chars. At
// context_window: 16000 — this project's own shipped local-profile default —
// tokenest.CompactionTrigger(16000, 8192) is 8,000 tokens, and the flat
// default (~5,000 tokens) is 62% of that budget on its own: a single fetch
// could trigger compaction, and two consecutive ones could not avoid it. The
// live research runs that surfaced this took 25 compactions across 42 tool
// calls at that window, against 4 at double it.
//
// window*3/5 chars is window*0.15*4 (roughly 15% of the window, at ~4
// chars/token) restated as integer arithmetic: 9,600 chars at 16,000 tokens,
// crossing the 20,000 ceiling only above ~33,000 tokens — so this only ever
// shrinks the cap for a small window and leaves cloud-scale contexts at
// today's number. No window in context (a cloud adapter with nothing to
// report) also leaves today's number, unchanged.
func defaultFetchLimit(ctx context.Context) int {
	window, ok := tool.ContextWindowFromContext(ctx)
	if !ok || window <= 0 {
		return maxFetchLimit
	}
	limit := window * 3 / 5
	if limit > maxFetchLimit {
		return maxFetchLimit
	}
	if limit < minFetchLimit {
		return minFetchLimit
	}
	return limit
}

// webRetries/webRetryBaseDelay/webRetryMaxDelay bound the P71.3 retry loop
// shared by fetchTool.get and doSearchRequest. Two retries (three attempts
// total) at these delays puts the worst case — three 30s ssrfClient timeouts
// plus two backoff sleeps — around 100s, comfortably under the 900s
// MaxTurnStall bound a single tool call must stay under
// (TestToolTimeoutsStayUnderTheStallBound). Small on purpose: this is
// recovering a transient failure inside one tool call, not a background job
// that can afford to wait out a real outage.
const (
	webRetries        = 2
	webRetryBaseDelay = 500 * time.Millisecond
	webRetryMaxDelay  = 4 * time.Second
)

// maxFetchWait and maxSearchWait are the worst-case total time one
// fetchTool.get / doSearchRequest call can block across every retry
// attempt — every HTTP timeout back to back plus every backoff sleep.
// TestToolTimeoutsStayUnderTheStallBound checks these, not the single
// per-attempt client timeout: the stall detector isn't beaten between
// retries inside one tool call, so the whole retry sequence is one
// unbeaten wait from its perspective, the same way a single git subprocess
// or agent spawn is.
var (
	maxFetchWait  = time.Duration(webRetries+1)*ssrfClient.Timeout + time.Duration(webRetries)*webRetryMaxDelay
	maxSearchWait = time.Duration(webRetries+1)*searchAPIClient.Timeout + time.Duration(webRetries)*webRetryMaxDelay
)

// webBackoff computes the delay before retry attempt (0-indexed), honoring a
// server's Retry-After header when retryAfter > 0, else falling back to
// exponential backoff with equal jitter — the same shape
// internal/provider/retry.go uses, restated locally rather than imported so
// internal/tool/builtin stays a leaf package with no dependency on the
// provider adapter stack.
func webBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > webRetryMaxDelay {
			return webRetryMaxDelay
		}
		return retryAfter
	}
	d := webRetryBaseDelay << attempt
	if d <= 0 || d > webRetryMaxDelay {
		d = webRetryMaxDelay
	}
	half := d / 2
	return half + time.Duration(mrand.Int64N(int64(half)+1))
}

// webRetryable reports whether an HTTP status is worth a retry: a 429/503
// with an explicit Retry-After, or any 5xx (the server's own transient
// failure). A 4xx otherwise — 404 above all, since P71.1/P71.10 both traced
// a live run to a model that had started guessing URLs — must never be
// retried: retrying a wrong URL just spends the budget faster while reporting
// the same wrong answer.
func webRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func (t *fetchTool) get(ctx context.Context, rawURL string) ([]byte, string, error) {
	var lastErr error
	for attempt := 0; attempt <= webRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, webBackoff(attempt-1, 0)); err != nil {
				return nil, "", err
			}
		}
		data, ctype, status, err := t.getOnce(ctx, rawURL)
		if err == nil {
			return data, ctype, nil
		}
		lastErr = err
		// A transport-level error (DNS, connection reset, TLS) has no status
		// to check and is always worth one retry; an HTTP error retries only
		// per webRetryable.
		if status > 0 && !webRetryable(status) {
			return nil, "", err
		}
	}
	return nil, "", lastErr
}

// getOnce is the single-attempt request fetchTool.get retries around. status
// is 0 for a transport-level failure (no response at all), so the caller can
// tell "never got a response" from "got one it didn't like."
func (t *fetchTool) getOnce(ctx context.Context, rawURL string) (data []byte, contentType string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("User-Agent", t.userAgent)
	resp, err := ssrfClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxWebBytes))
	if err != nil {
		return nil, "", resp.StatusCode, err
	}
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

// sleepCtx waits for d or until ctx is cancelled, whichever comes first —
// duplicated in miniature from internal/provider/retry.go's sleepCtx rather
// than imported, for the same leaf-package reason as webBackoff.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- search (pluggable provider; DuckDuckGo HTML scrape is the zero-config default) ---

// searchTool queries a web-search provider. When provider is empty it scrapes
// DuckDuckGo (no API key). Configured providers (brave, tavily, searxng) use
// their APIs and fall back to DuckDuckGo on error (P5.3).
type searchTool struct {
	userAgent  string
	provider   string
	apiKey     string
	baseURL    string
	scanOutput bool
}

func (t *searchTool) Name() string                { return "web_search" }
func (t *searchTool) Capability() tool.Capability { return tool.CapNetwork }

// Replay: re-issuing a search is a read of external state, not a mutation of
// it — safe to reissue (P65.4).
func (t *searchTool) Replay(json.RawMessage) tool.ReplayClass { return tool.ReplaySafe }
func (t *searchTool) Description() string {
	return "Search the web and return a list of result titles, URLs, and snippets."
}
func (t *searchTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"integer"}},"required":["query"]}`)
}
func (t *searchTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return tool.Result{Content: "query is required", IsError: true}, nil
	}
	max := args.MaxResults
	if max <= 0 || max > 20 {
		max = 10
	}

	// P71.6: keyed on query+max_results (not on query alone) — a differently
	// sized request against the same query is a different call whose answer
	// this cache has not necessarily seen, unlike web_fetch's URL-only key
	// where the underlying page is the same resource regardless of how much
	// of it the caller asked to read.
	cache, _ := tool.WebCacheFromContext(ctx)
	cacheKey := searchCacheKey(args.Query, max)
	if cache != nil {
		if cached, age, ok := cache.Get(cacheKey); ok {
			if backend, body, ok := decodeSearchCacheValue(cached); ok {
				attrs := [][2]string{{"query", args.Query}}
				if backend != "" {
					attrs = append(attrs, [2]string{"backend", backend})
				}
				attrs = append(attrs, [2]string{"served_from_session_cache", fmt.Sprintf("searched %s ago", age.Round(time.Second))})
				wrapped := trust.Wrap("web_untrusted_output", attrs, "a web search", body, t.scanOutput)
				return tool.Result{Content: wrapped}, nil
			}
		}
	}

	var results []searchResult
	var provErr error
	var backend string
	if p := strings.ToLower(strings.TrimSpace(t.provider)); p != "" && p != "duckduckgo" {
		results, provErr = t.providerSearch(ctx, p, args.Query, max)
		if provErr != nil {
			// Configured provider failed; fall through to the DuckDuckGo scrape.
			results = nil
		} else if len(results) > 0 {
			backend = p
		}
	}

	var ddgBlocked, marginaliaBlocked bool
	if len(results) == 0 {
		results, ddgBlocked = t.duckDuckGo(ctx, args.Query, max)
		if len(results) > 0 {
			backend = "duckduckgo"
		}
	}

	if len(results) == 0 {
		// P71.2: a real cross-provider fallback below DuckDuckGo, so a
		// throttled DDG (P71.1) doesn't fall straight to "no results found"
		// — the ladder used to be DDG's own primary+lite pair, which share
		// one rate-limit bucket and buy zero resilience against throttling
		// (confirmed live 2026-08-19).
		results, marginaliaBlocked = t.marginalia(ctx, args.Query, max)
		if len(results) > 0 {
			backend = "marginalia"
		}
	}

	if len(results) == 0 {
		// P71.1: DuckDuckGo throttles after roughly two requests in quick
		// succession and serves its anomaly-challenge page as HTTP 200/202 —
		// no parseable results, no transport error. Reporting that as "no
		// results found" told the model the web was empty, and both live
		// research runs that surfaced this bug reacted by inventing URLs to
		// fetch or reimplementing the search through `shell`. `ddgBlocked`/
		// `marginaliaBlocked` distinguish that case from a query that
		// genuinely returned nothing, which stays IsError:false — it is not
		// a tool failure.
		switch {
		case ddgBlocked || marginaliaBlocked:
			var which []string
			if ddgBlocked {
				which = append(which, "DuckDuckGo")
			}
			if marginaliaBlocked {
				which = append(which, "Marginalia")
			}
			return tool.Result{
				Content: fmt.Sprintf("%s rate-limited this client (an anti-bot challenge page, not a parseable results page). ", strings.Join(which, " and ")) +
					"This clears in under a minute; wait and retry, or switch search.provider to a keyed backend (tavily, brave, searxng) for a workload doing more than one or two searches.",
				IsError: true,
			}, nil
		case provErr != nil:
			return tool.Result{
				Content: fmt.Sprintf("search failed (provider %q: %v; DuckDuckGo fallback returned nothing)", t.provider, provErr),
				IsError: true,
			}, nil
		default:
			return tool.Result{Content: "no results found"}, nil
		}
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.title, r.urlStr)
		if r.date != "" {
			fmt.Fprintf(&b, "   published: %s\n", r.date)
		}
		if r.snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.snippet)
		}
	}
	attrs := [][2]string{{"query", args.Query}}
	if backend != "" {
		// P71.4: name the backend that actually served the results, so a
		// configured provider silently falling through to the DuckDuckGo
		// scrape (an expired key, a wrong searxng base_url, a 429) is visible
		// on the result itself instead of looking identical to success.
		attrs = append(attrs, [2]string{"backend", backend})
		if provErr != nil {
			fmt.Fprintf(&b, "\n[note: configured provider %q failed (%v); DuckDuckGo served this instead]\n", t.provider, provErr)
		}
	}
	if cache != nil {
		cache.Set(cacheKey, encodeSearchCacheValue(backend, b.String()))
	}
	wrapped := trust.Wrap("web_untrusted_output", attrs, "a web search", b.String(), t.scanOutput)
	return tool.Result{Content: wrapped}, nil
}

// searchCacheKey normalizes query+max into a webcache key: collapsed
// whitespace and case-folded, since "Go generics" and "go   generics" name
// the same search, followed by the requested result count — two requests for
// the same query but a different count are different calls (see the P71.6
// comment at the call site).
func searchCacheKey(query string, max int) string {
	norm := strings.ToLower(strings.Join(strings.Fields(query), " "))
	return norm + "|" + strconv.Itoa(max)
}

// searchCacheSep separates the backend name from the formatted result body
// in a cached search value. NUL never appears in either half: backend is one
// of a small fixed set of provider names and body is text already run
// through collapse/collapseBlankLines.
const searchCacheSep = "\x00"

func encodeSearchCacheValue(backend, body string) string {
	return backend + searchCacheSep + body
}

// decodeSearchCacheValue splits a value produced by encodeSearchCacheValue.
// ok is false for a value that predates this format (defensive only — every
// value in the cache was written by encodeSearchCacheValue in this process).
func decodeSearchCacheValue(v string) (backend, body string, ok bool) {
	backend, body, found := strings.Cut(v, searchCacheSep)
	return backend, body, found
}

// ddgHTMLURL, ddgLiteURL and marginaliaURL are the zero-config scrape
// endpoints, overridable in tests (a real net.Listener URL swapped in) since
// none of these three take a base URL in config the way brave/tavily/searxng
// do.
var (
	ddgHTMLURL    = "https://html.duckduckgo.com/html/"
	ddgLiteURL    = "https://lite.duckduckgo.com/lite/"
	marginaliaURL = "https://old-search.marginalia.nu/search"
)

// duckDuckGo runs the zero-config HTML scrape (primary + lite fallback).
// blocked reports whether every non-empty attempt was actually DuckDuckGo's
// anomaly-challenge page rather than a genuine zero-result search — see
// looksLikeDDGChallenge and P71.1. The two endpoints share one rate-limit
// bucket (P71.2, confirmed live 2026-08-19: both return the challenge
// simultaneously once throttled), so the lite fallback is a parse-format
// fallback only, not a resilience one — searchTool.Execute falls further to
// marginalia below when this returns nothing.
func (t *searchTool) duckDuckGo(ctx context.Context, query string, max int) ([]searchResult, bool) {
	f := &fetchTool{userAgent: t.userAgent}
	encoded := url.QueryEscape(query)
	body, _, err := f.get(ctx, ddgHTMLURL+"?q="+encoded)
	var results []searchResult
	blocked := false
	if err == nil {
		if looksLikeDDGChallenge(body) {
			blocked = true
		} else {
			results = parseDDG(body, max)
		}
	}
	if len(results) == 0 {
		if body2, _, err2 := f.get(ctx, ddgLiteURL+"?q="+encoded); err2 == nil {
			if looksLikeDDGChallenge(body2) {
				blocked = true
			} else {
				results = parseDDGLite(body2, max)
				if len(results) > 0 {
					blocked = false
				}
			}
		}
	}
	return results, blocked && len(results) == 0
}

// looksLikeDDGChallenge reports whether body is DuckDuckGo's bot-check page
// rather than a results page. Both come back as a 2xx HTTP status, so this is
// the only way to tell them apart; markers are the challenge form's own
// action endpoint and copy, which are far more specific than "no results
// parsed" alone (a genuine zero-result query is a different, rarer page).
func looksLikeDDGChallenge(body []byte) bool {
	return bytes.Contains(body, []byte("duckduckgo.com/anomaly.js")) ||
		bytes.Contains(body, []byte(`id="challenge-form"`)) ||
		bytes.Contains(body, []byte("anomaly-modal"))
}

// marginalia is the P71.2 cross-*provider* fallback below duckDuckGo: a
// second unkeyed scrape so a DDG-throttled client (P71.1) is still
// survivable without a key. Candidates were probed live 2026-08-19 —
// Mojeek and Startpage both return an anti-bot challenge (a CAPTCHA and an
// Anubis proof-of-work page respectively) on the very first request from
// this client, so neither is usable unauthenticated; Marginalia served a
// genuine results page on the first request and only started rate-limiting
// after rapid repeat queries, recovering in single-digit seconds (measured:
// ~3s), against DuckDuckGo's ~60s. blocked mirrors duckDuckGo's return
// convention: true only when every attempt was Marginalia's own challenge
// page, never for a genuine zero-result search.
func (t *searchTool) marginalia(ctx context.Context, query string, max int) ([]searchResult, bool) {
	f := &fetchTool{userAgent: t.userAgent}
	body, _, err := f.get(ctx, marginaliaURL+"?query="+url.QueryEscape(query))
	if err != nil {
		return nil, false
	}
	if looksLikeMarginaliaChallenge(body) {
		return nil, true
	}
	return parseMarginalia(body, max), false
}

// looksLikeMarginaliaChallenge reports whether body is Marginalia's own
// rate-limit interstitial ("The search engine is currently barraged by
// queries from bots") rather than a results page — both are HTTP 200, so
// this is the only way to tell them apart, mirroring looksLikeDDGChallenge
// for the same reason (P71.1's lesson applied to the new backend, per this
// item's own closure condition).
func looksLikeMarginaliaChallenge(body []byte) bool {
	return bytes.Contains(body, []byte("barraged by queries from bots")) ||
		bytes.Contains(body, []byte("Wait For A Moment"))
}

type searchResult struct {
	title, urlStr, snippet string
	// date is a publication-date signal, populated only by providers that
	// carry one (P71.7): Brave's `page_age`/`age` fields, SearXNG's
	// `publishedDate`. DuckDuckGo and Marginalia results never set it — the
	// zero-config scrape carries no such signal.
	date string
}

// parseDDGLite parses the DuckDuckGo Lite endpoint HTML as a fallback when
// the primary HTML endpoint returns no results. The lite page is simpler
// (table-based) and has historically been more structurally stable.
func parseDDGLite(body []byte, max int) []searchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	var results []searchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= max {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			href := decodeDDGHref(attr(n, "href"))
			title := collapse(nodeText(n))
			// Accept links that look like real external results (not DDG nav links).
			if href != "" && title != "" &&
				(strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://")) &&
				!strings.Contains(href, "duckduckgo.com") {
				results = append(results, searchResult{title: title, urlStr: href})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func parseDDG(body []byte, max int) []searchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	var results []searchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= max {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "result__a") {
			r := searchResult{title: collapse(nodeText(n)), urlStr: decodeDDGHref(attr(n, "href"))}
			results = append(results, r)
		}
		if n.Type == html.ElementNode && hasClass(n, "result__snippet") && len(results) > 0 {
			results[len(results)-1].snippet = collapse(nodeText(n))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

// parseMarginalia parses Marginalia's search-result HTML. Each hit is an
// `a.title` (the href lives on this element directly, unlike DDG's
// redirect-wrapped `result__a`) optionally followed by a `p.description`
// snippet.
func parseMarginalia(body []byte, max int) []searchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	var results []searchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= max {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "title") {
			results = append(results, searchResult{title: collapse(nodeText(n)), urlStr: attr(n, "href")})
		}
		if n.Type == html.ElementNode && hasClass(n, "description") && len(results) > 0 {
			results[len(results)-1].snippet = collapse(nodeText(n))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func decodeDDGHref(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	return href
}

// --- html helpers ---

func htmlToText(body []byte) string {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return string(body)
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		// P71.12: nav/header/footer/aside are structurally boilerplate — site
		// navigation, breadcrumbs, cookie banners, footers — never the article
		// content a fetch is for. Unlike preferring a <main>/<article>
		// container (rejected: a mis-detected container can silently return
		// less than the naive walk), dropping these four tags only removes
		// text, so it carries no regression risk.
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "nav", "header", "footer", "aside":
				return
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && isBlockElement(n.Data) {
			b.WriteString("\n")
		}
	}
	walk(doc)
	return collapseBlankLines(b.String())
}

func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "br", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "section", "article", "header", "footer":
		return true
	}
	return false
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := false
	for _, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// clip is a bare byte cut for short diagnostic snippets (an HTTP error body in
// a Go error, not a model-facing result). Result truncation goes through
// TruncateHead/TruncateTail — see truncate.go's posture table (P64.3).
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…[truncated]"
}
