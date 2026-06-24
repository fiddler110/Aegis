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
)

// NewHTTP connects to an MCP server over HTTP+SSE. The endpoint is the base
// URL (e.g. "http://localhost:8080"). Requests are POSTed as JSON-RPC;
// responses arrive as SSE events. This follows the MCP HTTP/SSE transport spec.
func NewHTTP(ctx context.Context, server, endpoint string) (*Client, error) {
	transport := &httpTransport{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{},
	}
	// SSE event stream for server→client messages.
	sseReader, sseWriter := io.Pipe()
	transport.sseWriter = sseWriter

	c := newClient(server, sseReader, transport, transport)

	// Start SSE listener.
	go transport.listenSSE(ctx, sseWriter)

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
	sseWriter *io.PipeWriter
	mu        sync.Mutex
}

// Write sends a JSON-RPC request to the HTTP endpoint via POST.
func (t *httpTransport) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	resp, err := t.client.Post(t.endpoint+"/message", "application/json", bytes.NewReader(p))
	if err != nil {
		return 0, fmt.Errorf("mcp http: POST failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("mcp http: POST %d: %s", resp.StatusCode, string(body))
	}

	// If the response is JSON (direct response mode), pipe it to the reader.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		body, err := io.ReadAll(resp.Body)
		if err == nil && len(body) > 0 {
			t.sseWriter.Write(append(body, '\n'))
		}
	}

	return len(p), nil
}

// Close terminates the transport.
func (t *httpTransport) Close() error {
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
		return NewHTTP(ctx, sc.Name, sc.Command)
	}
	return NewStdio(ctx, sc.Name, sc.Command, sc.Args, flattenEnv(sc.Env))
}

func isHTTPEndpoint(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
