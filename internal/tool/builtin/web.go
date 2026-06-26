package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/tool"
	"golang.org/x/net/html"
)

const maxWebBytes = 2 << 20 // 2 MiB cap on fetched bodies

// ssrfClient is a shared HTTP client whose transport enforces SSRF protection
// on every new connection. Reusing one client allows TCP/TLS connection pooling
// across fetch and search calls.
var ssrfClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: ssrfSafeDialer,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return validateNotPrivate(req.Context(), req.URL)
	},
}

// --- fetch ---

type fetchTool struct{ userAgent string }

func (t *fetchTool) Name() string                { return "web_fetch" }
func (t *fetchTool) Capability() tool.Capability { return tool.CapNetwork }
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

	body, ctype, err := t.get(ctx, u.String())
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("fetch failed: %v", err), IsError: true}, nil
	}

	text := string(body)
	if strings.Contains(ctype, "html") {
		text = htmlToText(body)
	}
	limit := 20000
	if args.MaxChars > 0 {
		limit = args.MaxChars
	}
	return tool.Result{Content: clip(text, limit)}, nil
}

func (t *fetchTool) get(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", t.userAgent)
	resp, err := ssrfClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBytes))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ssrfSafeDialer resolves the target address and rejects connections to
// private/loopback/link-local IPs, preventing SSRF attacks.
func ssrfSafeDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, fmt.Errorf("blocked: %s resolves to private/internal address %s", host, ip.IP)
		}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// validateNotPrivate checks a URL's hostname against private IP ranges.
func validateNotPrivate(ctx context.Context, u *url.URL) error {
	host := u.Hostname()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return fmt.Errorf("blocked: redirect to private/internal address %s (%s)", host, ip.IP)
		}
	}
	return nil
}

var privateRanges = []*net.IPNet{
	mustParseCIDR("10.0.0.0/8"),
	mustParseCIDR("172.16.0.0/12"),
	mustParseCIDR("192.168.0.0/16"),
	mustParseCIDR("127.0.0.0/8"),
	mustParseCIDR("169.254.0.0/16"),
	mustParseCIDR("::1/128"),
	mustParseCIDR("fc00::/7"),
	mustParseCIDR("fe80::/10"),
}

func isPrivateIP(ip net.IP) bool {
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// --- search (DuckDuckGo HTML endpoint, no API key required) ---

type searchTool struct{ userAgent string }

func (t *searchTool) Name() string                { return "web_search" }
func (t *searchTool) Capability() tool.Capability { return tool.CapNetwork }
func (t *searchTool) Description() string {
	return "Search the web and return a list of result titles, URLs, and snippets. Best-effort via DuckDuckGo."
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

	endpoint := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(args.Query)
	f := &fetchTool{userAgent: t.userAgent}
	body, _, err := f.get(ctx, endpoint)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}

	results := parseDDG(body, max)
	if len(results) == 0 {
		return tool.Result{Content: "no results found"}, nil
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.title, r.urlStr)
		if r.snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.snippet)
		}
	}
	return tool.Result{Content: b.String()}, nil
}

type searchResult struct {
	title, urlStr, snippet string
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
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript") {
			return
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

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…[truncated]"
}
