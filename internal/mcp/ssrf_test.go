package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestMCPSSRFBlocksPrivateIPs is the FIND-10 regression: the HTTP/SSE MCP
// client dialer must reject private/loopback/link-local destinations the
// same way internal/tool/builtin's web_fetch dialer does, since an MCP
// server's HTTP endpoint can be sourced from untrusted project config.
func TestMCPSSRFBlocksPrivateIPs(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.1.1", "::1"} {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			t.Fatalf("net.ParseIP(%s) failed", ip)
		}
		if !mcpIsPrivateIP(parsed) {
			t.Errorf("mcpIsPrivateIP(%s) = false, want true", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1"} {
		parsed := net.ParseIP(ip)
		if mcpIsPrivateIP(parsed) {
			t.Errorf("mcpIsPrivateIP(%s) = true, want false", ip)
		}
	}
}

// TestMCPHTTPClientRejectsLoopbackDial confirms the shared mcpHTTPClient's
// transport (as wired into NewHTTP) actually refuses to establish a TCP
// connection to a loopback address, not just that the private-IP predicate
// itself is correct.
func TestMCPHTTPClientRejectsLoopbackDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	_, err = mcpHTTPClient.Do(req)
	if err == nil {
		t.Fatal("expected mcpHTTPClient to block a loopback destination, got nil error")
	}
}

// TestMCPValidateNotPrivateBlocksRedirectTarget is the redirect-reuse
// regression: a server that returns a valid (non-private) initial address
// but redirects to a private one must still be blocked, mirroring
// validateNotPrivate's CheckRedirect use in internal/tool/builtin/web.go.
func TestMCPValidateNotPrivateBlocksRedirectTarget(t *testing.T) {
	u, err := url.Parse("http://127.0.0.1:9/mcp") // loopback, low port
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := mcpValidateNotPrivate(context.Background(), u); err == nil {
		t.Error("expected mcpValidateNotPrivate to reject a loopback redirect target")
	}

	u2, err := url.Parse("http://1.1.1.1/mcp")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := mcpValidateNotPrivate(context.Background(), u2); err != nil {
		t.Errorf("expected a public address to pass, got: %v", err)
	}
}

// TestMCPHTTPClientCheckRedirectCapsRedirects exercises the CheckRedirect
// hook's redirect-count cap directly (five hops is the same limit used by
// web.go's ssrfClient).
func TestMCPHTTPClientCheckRedirectCapsRedirects(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://1.1.1.1/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	via := make([]*http.Request, 5)
	for i := range via {
		via[i] = req
	}
	err = mcpHTTPClient.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("expected too-many-redirects error at 5 prior hops")
	}
}
