package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/config"
)

// TestNewFromConfigTLSDependsOnCertOnDisk pins down the ordering hazard behind
// the first-run "client sent an HTTP request to an HTTPS server" handshake
// failure: with server.tls.enabled true, NewFromConfig only produces an https://
// client once the daemon's cert exists on disk. Before that, it errors rather
// than silently falling back to plaintext against what may be a TLS listener.
//
// This is why internal/cli's auto-start paths treat that error, when raised
// by a client built *before* a daemon is known to exist, the same as an
// unreachable daemon — and only rebuild the client (fatally, if it still
// errors) after startEmbeddedDaemon returns and server.New has generated the
// cert.
func TestNewFromConfigTLSDependsOnCertOnDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir,
		Server: config.ServerConfig{
			Addr: "127.0.0.1:4127",
			TLS:  config.ServerTLSConfig{Enabled: true},
		},
	}

	// Before the daemon exists there is no cert to pin: NewFromConfig must
	// error loudly rather than silently falling back to plaintext against
	// what may be a TLS listener.
	if _, err := NewFromConfig(cfg); err == nil {
		t.Error("NewFromConfig before cert exists: expected an error, got nil")
	}

	// Once the daemon has written a valid cert, the same config yields a
	// pinned https:// client.
	writeTestCert(t, cfg.TLSCertPath())
	c, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewFromConfig after cert exists: %v", err)
	}
	if c.base != "https://127.0.0.1:4127" {
		t.Errorf("base after cert exists = %q, want https://127.0.0.1:4127", c.base)
	}
}

// writeTestCert writes a minimal self-signed certificate to path, standing in
// for the daemon.crt that server.New generates on first start.
func writeTestCert(t *testing.T, path string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNewNormalizesAddr verifies New adds a scheme when missing and strips a
// trailing slash, since callers pass raw host:port from config/flags.
func TestNewNormalizesAddr(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:4127":         "http://127.0.0.1:4127",
		"http://127.0.0.1:4127/": "http://127.0.0.1:4127",
		"https://example.com":    "https://example.com",
	}
	for in, want := range cases {
		c := New(in)
		if c.base != want {
			t.Errorf("New(%q).base = %q, want %q", in, c.base, want)
		}
	}
}

// TestWithTokenSetsAuthHeader verifies WithToken produces a client whose
// requests carry a bearer token, without mutating the original client.
// /healthz is intentionally unauthenticated (see server.authMiddleware), so
// this exercises an ordinary do()-based call instead.
func TestWithTokenSetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]api.SessionMeta{})
	}))
	defer srv.Close()

	base := New(srv.URL)
	authed := base.WithToken("secret-token")

	if len(base.authToken) != 0 {
		t.Error("New client should not have a token")
	}
	if _, err := authed.ListSessions(context.Background()); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization header = %q, want \"Bearer secret-token\"", gotAuth)
	}
	if string(authed.authToken) != "secret-token" {
		t.Errorf("authed.authToken = %q, want %q", authed.authToken, "secret-token")
	}
	if len(base.authToken) != 0 {
		t.Error("expected WithToken to not mutate the receiver")
	}
}

// TestWithTokenFileReadsToken verifies WithTokenFile loads and trims a token
// from disk, matching how the CLI reads the daemon's auth token file.
func TestWithTokenFileReadsToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New("http://example.com").WithTokenFile(path)
	if string(c.authToken) != "file-token" {
		t.Errorf("authToken = %q, want \"file-token\"", c.authToken)
	}
}

// TestWithTokenFileMissingFallsBack verifies a missing token file returns the
// original client unchanged rather than erroring, since an unauthenticated
// local daemon (no token file yet) is a valid state.
func TestWithTokenFileMissingFallsBack(t *testing.T) {
	c := New("http://example.com")
	c2 := c.WithTokenFile(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(c2.authToken) != 0 {
		t.Errorf("expected empty authToken, got %q", c2.authToken)
	}
}

// TestZeroOverwritesBackingBytes verifies Zero actually scrubs the token's
// backing byte array in place — not merely reassigning/nilling the authToken
// field — since a memory scrape could otherwise still find the old bytes at
// the same address after Zero "cleared" the field (FIND-33/P24.21).
func TestZeroOverwritesBackingBytes(t *testing.T) {
	c := New("http://example.com").WithToken("very-secret-token-value")

	// Alias the same backing array c.authToken points at before Zero drops
	// its own reference, so we can inspect the bytes after Zero runs.
	backing := c.authToken

	var sawNonZero bool
	for _, b := range backing {
		if b != 0 {
			sawNonZero = true
			break
		}
	}
	if !sawNonZero {
		t.Fatal("test setup: token bytes were already zero before Zero was called")
	}

	c.Zero()

	for i, b := range backing {
		if b != 0 {
			t.Errorf("backing[%d] = %d, want 0 — Zero must overwrite the backing array in place", i, b)
		}
	}
	if c.authToken != nil {
		t.Errorf("authToken = %v, want nil after Zero", c.authToken)
	}
}

// TestZeroSafeOnEmptyClient verifies Zero is a harmless no-op on a Client
// with no token, since callers scrub unconditionally without checking
// whether authentication was ever configured.
func TestZeroSafeOnEmptyClient(t *testing.T) {
	c := New("http://example.com")
	c.Zero() // must not panic
	if c.authToken != nil {
		t.Errorf("authToken = %v, want nil", c.authToken)
	}
}

// TestStatusReturnsHealth verifies a healthy daemon's fields (including the
// P7.4 sandbox-fallback flags) decode correctly.
func TestStatusReturnsHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("path = %q, want /healthz", r.URL.Path)
		}
		json.NewEncoder(w).Encode(api.HealthStatus{Status: "ok", Model: "test-model", SandboxFallback: true, SandboxFallbackReason: "docker unavailable"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "ok" || st.Model != "test-model" || !st.SandboxFallback || st.SandboxFallbackReason != "docker unavailable" {
		t.Errorf("Status = %+v", st)
	}
}

// TestHealthErrorsOnNon200 verifies Health surfaces an error when the daemon
// responds with a non-200 status (e.g. still starting up).
func TestHealthErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(srv.URL)
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("expected an error for a 503 healthz response")
	}
}

// TestDoDecodesErrorResponse verifies a JSON {"error": "..."} body from a
// non-2xx response surfaces as a descriptive Go error, not a generic one.
func TestDoDecodesErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(api.ErrorResponse{Error: "session not found"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetSession(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "session not found") || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want it to mention the status and message", err)
	}
}

// TestDoErrorIsTypedStatusError verifies decodeError's result carries the
// HTTP status code as a typed *StatusError recoverable via errors.As, not
// just embedded in the error string — the P33.15 fix that lets callers (the
// TUI's steer path) distinguish a 404 ("run already ended", not retryable)
// from a 429 ("steer buffer full", retryable) without parsing text. Error()
// text must stay exactly what it was before this existed (TestDoDecodesErrorResponse).
func TestDoErrorIsTypedStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(api.ErrorResponse{Error: "steer buffer full"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.Steer(context.Background(), "s1", "hi")
	if err == nil {
		t.Fatal("expected an error")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v (%T), want it to unwrap to a *StatusError", err, err)
	}
	if statusErr.Code != http.StatusTooManyRequests {
		t.Errorf("statusErr.Code = %d, want %d", statusErr.Code, http.StatusTooManyRequests)
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "steer buffer full") {
		t.Errorf("err.Error() = %q, want unchanged daemon-error text", err.Error())
	}
}

// TestDoErrorWithoutBodyStillTypes covers the no-JSON-body fallback branch of
// decodeError — it must still produce a *StatusError, not a plain error.
func TestDoErrorWithoutBodyStillTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.Steer(context.Background(), "s1", "hi")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v (%T), want it to unwrap to a *StatusError", err, err)
	}
	if statusErr.Code != http.StatusNotFound {
		t.Errorf("statusErr.Code = %d, want 404", statusErr.Code)
	}
}

// TestCreateAndListSessions exercises the do() JSON request/response path
// end to end against a fake daemon.
func TestCreateAndListSessions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		var req api.CreateSessionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Mode != "build" {
			t.Errorf("CreateSessionRequest.Mode = %q, want build", req.Mode)
		}
		json.NewEncoder(w).Encode(api.SessionMeta{ID: "s1", Mode: req.Mode})
	})
	mux.HandleFunc("GET /sessions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]api.SessionMeta{{ID: "s1"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	ctx := context.Background()

	meta, err := c.CreateSession(ctx, api.CreateSessionRequest{Mode: "build"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if meta.ID != "s1" {
		t.Errorf("meta.ID = %q, want s1", meta.ID)
	}

	list, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 || list[0].ID != "s1" {
		t.Errorf("ListSessions = %+v", list)
	}
}

// TestPruneSessionsWithDays verifies the days query parameter is only added
// when explicitly requested, since 0 means "use the server's configured TTL".
func TestPruneSessionsWithDays(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(api.PruneResponse{Deleted: 2})
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.PruneSessions(context.Background(), 7)
	if err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	if resp.Deleted != 2 {
		t.Errorf("Deleted = %d, want 2", resp.Deleted)
	}
	if gotQuery != "days=7" {
		t.Errorf("query = %q, want days=7", gotQuery)
	}
}

// TestPostMessageStreamsEvents verifies SSE frames from the daemon are parsed
// into api.Event values on the returned channel, and that non-"data:" lines
// (comments/heartbeats) are skipped.
func TestPostMessageStreamsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept header = %q, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, ": ping\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "event: text\ndata: %s\n\n", mustJSON(api.Event{Kind: api.KindText, Text: "hi"}))
		flusher.Flush()
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSON(api.Event{Kind: api.KindDone}))
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(srv.URL)
	ch, err := c.PostMessage(context.Background(), "s1", "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	var kinds []api.EventKind
	for ev := range ch {
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) != 2 || kinds[0] != api.KindText || kinds[1] != api.KindDone {
		t.Errorf("received kinds = %v, want [text done]", kinds)
	}
}

// TestPostMessageErrorsOnNon200 verifies a non-200 response (e.g. the P9.5
// spend-cap preflight rejection) surfaces as an error rather than an empty
// event stream.
func TestPostMessageErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(api.ErrorResponse{Error: "session spend cap reached"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.PostMessage(context.Background(), "s1", "hello")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "session spend cap reached") {
		t.Errorf("err = %v", err)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
