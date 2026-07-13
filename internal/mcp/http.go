package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxMCPResponseBytes = 16 << 20 // 16 MiB

// mcpHTTPClient is a shared HTTP client whose transport enforces SSRF
// protection on every new connection (FIND-10). An HTTP/SSE MCP server
// address comes from config, which can be sourced from a project's
// .aegis/config.yaml — i.e. from an untrusted repo — so it needs the same
// private/loopback/link-local dialer guard as web_fetch
// (internal/tool/builtin's ssrfSafeDialer). Kept as a small, deliberate
// duplicate here rather than a cross-package import of internal/tool/builtin,
// matching how internal/security's networkPrivateRanges is already kept
// decoupled from internal/tool/builtin for the same reason (see
// internal/security/target.go).
var mcpHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: mcpSSRFSafeDialer,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return mcpValidateNotPrivate(req.Context(), req.URL)
	},
}

// mcpSSRFSafeDialer resolves the target address and rejects connections to
// private/loopback/link-local IPs, preventing an HTTP/SSE MCP server address
// (which may be sourced from untrusted project config) from being used to
// reach internal network services.
func mcpSSRFSafeDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if mcpIsPrivateIP(ip.IP) {
			return nil, fmt.Errorf("blocked: %s resolves to private/internal address %s", host, ip.IP)
		}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// mcpValidateNotPrivate checks a URL's hostname against private IP ranges;
// used as a CheckRedirect hook so a 3xx response from an MCP server can't
// redirect the client to an internal address after the initial connection
// was already validated.
func mcpValidateNotPrivate(ctx context.Context, u *url.URL) error {
	host := u.Hostname()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if mcpIsPrivateIP(ip.IP) {
			return fmt.Errorf("blocked: redirect to private/internal address %s (%s)", host, ip.IP)
		}
	}
	return nil
}

var mcpPrivateRanges = []*net.IPNet{
	mustParseCIDRMCP("10.0.0.0/8"),
	mustParseCIDRMCP("172.16.0.0/12"),
	mustParseCIDRMCP("192.168.0.0/16"),
	mustParseCIDRMCP("127.0.0.0/8"),
	mustParseCIDRMCP("169.254.0.0/16"),
	mustParseCIDRMCP("::1/128"),
	mustParseCIDRMCP("fc00::/7"),
	mustParseCIDRMCP("fe80::/10"),
}

func mcpIsPrivateIP(ip net.IP) bool {
	for _, r := range mcpPrivateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func mustParseCIDRMCP(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// NewHTTP connects to an MCP server over HTTP+SSE. The endpoint is the base
// URL (e.g. "http://localhost:8080"). auth is an optional Bearer token sent
// on every request; pass empty string to omit the Authorization header.
func NewHTTP(ctx context.Context, server, endpoint, auth string) (*Client, error) {
	transport := &httpTransport{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   mcpHTTPClient,
		auth:     auth,
	}
	// SSE event stream for server→client messages.
	sseReader, sseWriter := io.Pipe()
	transport.sseWriter = sseWriter

	c := newClient(server, sseReader, transport, transport)

	// Start SSE listener with a cancellable context so Close() can stop it.
	sseCtx, sseCancel := context.WithCancel(ctx)
	transport.sseCancel = sseCancel
	go transport.listenSSE(sseCtx, sseWriter)

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// httpTransport implements io.Writer (for sending requests) and io.Closer.
type httpTransport struct {
	endpoint  string
	client    *http.Client
	auth      string // Bearer token; empty means no Authorization header
	sseWriter *io.PipeWriter
	sseCancel context.CancelFunc // cancels the SSE listener's HTTP request
	mu        sync.Mutex
}

// Write sends a JSON-RPC request to the HTTP endpoint via POST.
func (t *httpTransport) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	req, err := http.NewRequest("POST", t.endpoint+"/message", bytes.NewReader(p))
	if err != nil {
		return 0, fmt.Errorf("mcp http: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if t.auth != "" {
		req.Header.Set("Authorization", "Bearer "+t.auth)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("mcp http: POST failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxMCPResponseBytes))
		return 0, fmt.Errorf("mcp http: POST %d: %s", resp.StatusCode, string(body))
	}

	// If the response is JSON (direct response mode), pipe it to the reader.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxMCPResponseBytes))
		if err == nil && len(body) > 0 {
			t.sseWriter.Write(append(body, '\n'))
		}
	}

	return len(p), nil
}

// Close terminates the transport, cancelling the SSE listener goroutine.
func (t *httpTransport) Close() error {
	if t.sseCancel != nil {
		t.sseCancel()
	}
	t.sseWriter.Close()
	return nil
}

// listenSSE opens the SSE endpoint and forwards events to the pipe writer.
// Closing the pipe with the terminal error (rather than a bare Close, which
// only ever signals plain EOF) lets readLoop's scanner.Err() on the other end
// see why the connection actually died — an oversized line, a request
// failure, or the stream simply ending — instead of always looking like a
// clean disconnect.
func (t *httpTransport) listenSSE(ctx context.Context, w *io.PipeWriter) {
	w.CloseWithError(t.runSSE(ctx, w))
}

func (t *httpTransport) runSSE(ctx context.Context, w *io.PipeWriter) error {
	req, err := http.NewRequestWithContext(ctx, "GET", t.endpoint+"/sse", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if t.auth != "" {
		req.Header.Set("Authorization", "Bearer "+t.auth)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Raised from bufio's 64KB default (P9): a verbose or malicious server
	// emitting one SSE data line over that default would otherwise silently
	// kill this scanner with no indication why, since the failure previously
	// went unchecked below.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMCPScanTokenBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		// Verify it's valid JSON before forwarding.
		if json.Valid([]byte(data)) {
			if _, err := w.Write(append([]byte(data), '\n')); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// NewHTTPOrStdio tries HTTP first if the config looks like a URL, otherwise
// falls back to stdio. This is a convenience constructor for RegisterServers.
func NewHTTPOrStdio(ctx context.Context, sc ServerConfig) (*Client, error) {
	if isHTTPEndpoint(sc.Command) {
		return NewHTTP(ctx, sc.Name, sc.Command, sc.Auth)
	}
	return NewStdio(ctx, sc.Name, sc.Command, sc.Args, flattenEnv(sc.Env))
}

func isHTTPEndpoint(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
