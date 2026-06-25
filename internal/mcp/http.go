package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxMCPResponseBytes = 16 << 20 // 16 MiB

// NewHTTP connects to an MCP server over HTTP+SSE. The endpoint is the base
// URL (e.g. "http://localhost:8080"). auth is an optional Bearer token sent
// on every request; pass empty string to omit the Authorization header.
func NewHTTP(ctx context.Context, server, endpoint, auth string) (*Client, error) {
	transport := &httpTransport{
		endpoint:  strings.TrimRight(endpoint, "/"),
		client:    &http.Client{Timeout: 30 * time.Second},
		auth:      auth,
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
func (t *httpTransport) listenSSE(ctx context.Context, w *io.PipeWriter) {
	defer w.Close()

	req, err := http.NewRequestWithContext(ctx, "GET", t.endpoint+"/sse", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	if t.auth != "" {
		req.Header.Set("Authorization", "Bearer "+t.auth)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		// Verify it's valid JSON before forwarding.
		if json.Valid([]byte(data)) {
			w.Write(append([]byte(data), '\n'))
		}
	}
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
