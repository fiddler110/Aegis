package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

func newTestWebUIServer(t *testing.T) *Server {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token"
	return srv
}

var (
	pageTokenRE = regexp.MustCompile(`data-page-token="([0-9a-f]+)"`)
	csrfTokenRE = regexp.MustCompile(`data-csrf-token="([0-9a-f]+)"`)
)

func TestWebUIServedAndTokenInjected(t *testing.T) {
	srv := newTestWebUIServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// A cookie jar is required for this flow now: GET /ui sets the HttpOnly
	// aegis_ui_csrf cookie the double-submit check in /auth/exchange expects
	// (FIND-01/P24.1) — exercising this with a jar-backed client is what
	// distinguishes "this looks like a real browser page load" from a bare
	// request replaying only the values scraped out of the response body.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	// The UI page is reachable without a bearer token.
	resp, err := client.Get(ts.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if !strings.Contains(page, "Aegis") {
		t.Error("page missing app marker")
	}
	if strings.Contains(page, "__AEGIS_TOKEN__") || strings.Contains(page, "__AEGIS_CSRF__") {
		t.Error("a placeholder was not replaced")
	}
	if strings.Contains(page, "secret-token") {
		t.Error("the real, long-lived auth token leaked into the page (P15.12)")
	}
	m := pageTokenRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("page missing a data-page-token value")
	}
	pageToken := m[1]
	cm := csrfTokenRE.FindStringSubmatch(page)
	if cm == nil {
		t.Fatal("page missing a data-csrf-token value")
	}
	csrfToken := cm[1]

	// A protected endpoint still requires the real token, not the page token.
	r2, err := http.Get(ts.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /sessions without token = %d, want 401", r2.StatusCode)
	}

	// The page token exchanges for the real token exactly once. The jar
	// attaches the aegis_ui_csrf cookie GET /ui set automatically; the
	// explicit header simulates what the frontend's JS reads out of the DOM.
	exchange := func() (int, string) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+pageToken)
		req.Header.Set("X-Aegis-CSRF", csrfToken)
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out.Token
	}

	status, token := exchange()
	if status != http.StatusOK {
		t.Fatalf("first exchange status = %d, want 200", status)
	}
	if token != "secret-token" {
		t.Errorf("exchange returned token %q, want the real auth token", token)
	}

	status, _ = exchange()
	if status != http.StatusUnauthorized {
		t.Errorf("replayed exchange status = %d, want 401 (single-use)", status)
	}

	// The real token from the exchange now works on a protected endpoint.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r3.Body.Close()
	if r3.StatusCode != http.StatusOK {
		t.Errorf("GET /sessions with exchanged token = %d, want 200", r3.StatusCode)
	}
}

func TestAuthExchangeRejectsMismatchedCSRF(t *testing.T) {
	srv := newTestWebUIServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	resp, err := client.Get(ts.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)
	pageToken := pageTokenRE.FindStringSubmatch(page)[1]

	// A cross-origin page's blind attempt: it has neither the HttpOnly
	// cookie value nor the page's own DOM to read the real csrf nonce from,
	// so it can at best send a wrong (or empty) header.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", nil)
	req.Header.Set("Authorization", "Bearer "+pageToken)
	req.Header.Set("X-Aegis-CSRF", "wrong-nonce")
	r, err := client.Do(req) // jar still attaches the real cookie automatically
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Errorf("exchange with mismatched csrf header = %d, want 401", r.StatusCode)
	}

	// No cookie at all (a bare client with no jar, e.g. a raw same-machine
	// HTTP request that never fetched /ui through this client) is rejected too.
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", nil)
	req2.Header.Set("Authorization", "Bearer "+pageToken)
	req2.Header.Set("X-Aegis-CSRF", "wrong-nonce")
	r2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("exchange with no cookie = %d, want 401", r2.StatusCode)
	}
}

func TestAuthExchangeRejectsMissingOrUnknownToken(t *testing.T) {
	srv := newTestWebUIServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Errorf("exchange without a page token = %d, want 401", r.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", nil)
	req2.Header.Set("Authorization", "Bearer not-a-real-page-token")
	r2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("exchange with unknown page token = %d, want 401", r2.StatusCode)
	}
}

func TestPageTokenSingleUseAndExpiry(t *testing.T) {
	srv := newTestWebUIServer(t)

	tok, csrf, err := srv.mintPageToken()
	if err != nil {
		t.Fatal(err)
	}
	if !srv.exchangePageToken(tok, csrf) {
		t.Fatal("expected the first exchange of a fresh token to succeed")
	}
	if srv.exchangePageToken(tok, csrf) {
		t.Fatal("expected a replayed token to be rejected")
	}

	expired, expiredCSRF, err := srv.mintPageToken()
	if err != nil {
		t.Fatal(err)
	}
	srv.pageTokenMu.Lock()
	srv.pageTokens[expired] = pageTokenEntry{expiry: time.Now().Add(-time.Second), csrf: expiredCSRF}
	srv.pageTokenMu.Unlock()
	if srv.exchangePageToken(expired, expiredCSRF) {
		t.Fatal("expected an expired token to be rejected")
	}

	mismatched, _, err := srv.mintPageToken()
	if err != nil {
		t.Fatal(err)
	}
	if srv.exchangePageToken(mismatched, "wrong-nonce") {
		t.Fatal("expected a mismatched csrf nonce to be rejected")
	}
}

// TestWebUICSRFCookieSecureFlag covers P31.3: the CSRF cookie must carry
// Secure when the request arrived over TLS, but not on the default
// loopback-only plaintext deployment (Secure would make browsers silently
// drop the cookie there).
func TestWebUICSRFCookieSecureFlag(t *testing.T) {
	srv := newTestWebUIServer(t)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == uiCSRFCookieName {
			found = true
			if c.Secure {
				t.Error("expected Secure=false on the plain-HTTP cookie")
			}
		}
	}
	if !found {
		t.Fatal("plain-HTTP response missing the CSRF cookie")
	}

	tlsSrv := httptest.NewTLSServer(srv.Handler())
	defer tlsSrv.Close()
	tlsResp, err := tlsSrv.Client().Get(tlsSrv.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	tlsResp.Body.Close()
	found = false
	for _, c := range tlsResp.Cookies() {
		if c.Name == uiCSRFCookieName {
			found = true
			if !c.Secure {
				t.Error("expected Secure=true on the TLS cookie")
			}
		}
	}
	if !found {
		t.Fatal("TLS response missing the CSRF cookie")
	}
}

func TestWebUIAssetsServedWithLongCache(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	srv := newWithDeps(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token"

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	entries, err := fs.ReadDir(webUIAssets, "assets")
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected built assets under webui/dist/assets, err=%v entries=%d", err, len(entries))
	}

	resp, err := http.Get(ts.URL + "/ui/assets/" + entries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/assets/%s status = %d, want 200", entries[0].Name(), resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
}
