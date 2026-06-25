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

func TestHTTPTransportSetsAuthHeader(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/message" {
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	_, pw := io.Pipe()
	transport := &httpTransport{
		endpoint:  srv.URL,
		client:    srv.Client(),
		sseWriter: pw,
		auth:      "mysecrettoken",
	}
	defer pw.Close()

	_, err := transport.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`))
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if authHeader != "Bearer mysecrettoken" {
		t.Errorf("expected Bearer mysecrettoken, got %q", authHeader)
	}
}

func TestHTTPTransportNoAuthHeader(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/message" {
			authHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	_, pw := io.Pipe()
	transport := &httpTransport{
		endpoint:  srv.URL,
		client:    srv.Client(),
		sseWriter: pw,
		// auth is empty — no Authorization header should be sent
	}
	defer pw.Close()

	_, err := transport.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`))
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if authHeader != "" {
		t.Errorf("expected no Authorization header, got %q", authHeader)
	}
}
