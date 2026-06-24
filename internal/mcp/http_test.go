package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsHTTPEndpoint(t *testing.T) {
	if !isHTTPEndpoint("http://localhost:8080") {
		t.Error("expected true for http://")
	}
	if !isHTTPEndpoint("https://mcp.example.com") {
		t.Error("expected true for https://")
	}
	if isHTTPEndpoint("gopls") {
		t.Error("expected false for bare command")
	}
	if isHTTPEndpoint("/usr/bin/mcp-server") {
		t.Error("expected false for file path")
	}
}

func TestHTTPTransportPostsJSON(t *testing.T) {
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/message" {
			body, _ := io.ReadAll(r.Body)
			received = string(body)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Create transport without the SSE pipe (test only the POST path).
	_, pw := io.Pipe()
	transport := &httpTransport{
		endpoint:  srv.URL,
		client:    srv.Client(),
		sseWriter: pw,
	}
	defer pw.Close()

	// Drain the pipe reader in background so writes don't block.
	pr, _ := io.Pipe()
	go io.Copy(io.Discard, pr)

	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`)
	n, err := transport.Write(data)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	if !strings.Contains(received, `"method":"test"`) {
		t.Errorf("server did not receive expected data: %s", received)
	}
}
