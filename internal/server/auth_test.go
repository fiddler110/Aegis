package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tool"
)

// newAuthTestServer builds a minimal Server behind an httptest.Server for
// exercising authMiddleware, mirroring the setup
// TestServerInvalidAuthAttemptsLoggedAndCounted (server_test.go) already
// uses for the FIND-11 counter/logging test this lockout extends.
func newAuthTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, logger, store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token-123"
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); store.Close() })
	return srv, ts
}

func doAuthedGet(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// TestAuthLockoutEngagesAfterThresholdAndExpires is the P27.12/FIND-14
// regression: recordInvalidAuthAttempt only logged before this (FIND-11);
// now, once the consecutive-failure streak reaches authLockThreshold, the
// daemon must reject every request — even one carrying the correct token —
// with 429 until the backoff window elapses, at which point normal auth
// resumes.
func TestAuthLockoutEngagesAfterThresholdAndExpires(t *testing.T) {
	_, ts := newAuthTestServer(t)

	// Drive exactly authLockThreshold consecutive invalid attempts — the
	// streak crosses the threshold on the last one, engaging the lockout.
	for i := 0; i < authLockThreshold; i++ {
		resp := doAuthedGet(t, ts.URL+"/sessions", "totally-wrong-guess")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, resp.StatusCode)
		}
	}

	// Even a request with the *correct* token must now be rejected while
	// locked — the lockout blocks by request volume, not by guess accuracy.
	resp := doAuthedGet(t, ts.URL+"/sessions", "secret-token-123")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status while locked = %d, want 429", resp.StatusCode)
	}

	// After the (short, base-delay) window elapses, normal auth resumes.
	time.Sleep(authLockBaseDelay + 200*time.Millisecond)
	resp = doAuthedGet(t, ts.URL+"/sessions", "secret-token-123")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after lockout expired = %d, want 200", resp.StatusCode)
	}
}

// TestAuthLockoutStreakResetsOnSuccess confirms a successful authenticated
// request clears the consecutive-failure streak (resetAuthFailureStreak), so
// intermittent failures interleaved with successes — e.g. a client that
// briefly retried a stale token — never accumulate toward a lockout.
func TestAuthLockoutStreakResetsOnSuccess(t *testing.T) {
	srv, ts := newAuthTestServer(t)

	for round := 0; round < 3; round++ {
		for i := 0; i < authLockThreshold-1; i++ {
			resp := doAuthedGet(t, ts.URL+"/sessions", "wrong-guess")
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("round %d attempt %d: status = %d, want 401", round, i, resp.StatusCode)
			}
		}
		resp := doAuthedGet(t, ts.URL+"/sessions", "secret-token-123")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("round %d: valid-token status = %d, want 200 (streak should not have locked us out)", round, resp.StatusCode)
		}
	}

	srv.authLockMu.Lock()
	streak := srv.authConsecutiveFailures
	srv.authLockMu.Unlock()
	if streak != 0 {
		t.Errorf("authConsecutiveFailures = %d, want 0 after a successful request", streak)
	}
}

// TestAuthLockoutMessageDoesNotLeakToken confirms the 429 response body
// during a lockout never echoes the attempted (or real) token value.
func TestAuthLockoutMessageDoesNotLeakToken(t *testing.T) {
	_, ts := newAuthTestServer(t)

	for i := 0; i < authLockThreshold; i++ {
		doAuthedGet(t, ts.URL+"/sessions", "totally-wrong-guess-value")
	}

	req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if strings.Contains(body, "secret-token-123") || strings.Contains(body, "totally-wrong-guess-value") {
		t.Errorf("lockout response body must never contain a token value, got: %s", body)
	}
}

// newLoggingAuthTestServer is newAuthTestServer with a capturing logger, for
// asserting on the audit lines themselves rather than only on status codes.
func newLoggingAuthTestServer(t *testing.T) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	cfg := &config.Config{Provider: config.ProviderConfig{Model: "test"}, Permission: config.PermissionConfig{Mode: "plan"}}
	srv := newWithDeps(cfg, logger, store, fixedAdapter{}, tool.NewRegistry())
	srv.authToken = "secret-token-123"
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); store.Close() })
	return srv, ts, &logBuf
}

// exchangeRequest builds a POST /auth/exchange carrying whichever of the
// three credentials the caller wants present; an empty string omits that
// piece entirely, which is how each failure branch is reached.
func exchangeRequest(t *testing.T, url, pageToken, csrfCookie, csrfHeader string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/auth/exchange", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pageToken != "" {
		req.Header.Set("Authorization", "Bearer "+pageToken)
	}
	if csrfCookie != "" {
		req.AddCookie(&http.Cookie{Name: uiCSRFCookieName, Value: csrfCookie})
	}
	if csrfHeader != "" {
		req.Header.Set(uiCSRFHeaderName, csrfHeader)
	}
	return req
}

// TestAuthExchangeFailuresAreLogged covers P63.5: /auth/exchange is exempt
// from authMiddleware (the frontend has no daemon token yet), which used to
// mean every rejection there was invisible while a probe against any
// authenticated route produced a warn line. Each branch gets its own server
// so the attempt is the first one and therefore always lands on the
// invalidAuthLogEvery cadence.
func TestAuthExchangeFailuresAreLogged(t *testing.T) {
	cases := []struct {
		name   string
		build  func(t *testing.T, srv *Server, url string) *http.Request
		reason string
	}{
		{
			name: "missing page token",
			build: func(t *testing.T, _ *Server, url string) *http.Request {
				return exchangeRequest(t, url, "", "nonce", "nonce")
			},
			reason: "page token missing",
		},
		{
			name: "missing csrf cookie",
			build: func(t *testing.T, _ *Server, url string) *http.Request {
				return exchangeRequest(t, url, "some-page-token", "", "nonce")
			},
			reason: "csrf cookie missing",
		},
		{
			name: "csrf mismatch",
			build: func(t *testing.T, _ *Server, url string) *http.Request {
				return exchangeRequest(t, url, "some-page-token", "nonce", "different-nonce")
			},
			reason: "csrf header missing or mismatched",
		},
		{
			name: "unknown page token",
			build: func(t *testing.T, _ *Server, url string) *http.Request {
				return exchangeRequest(t, url, "never-minted", "nonce", "nonce")
			},
			reason: "page token invalid, expired, or already redeemed",
		},
		{
			name: "expired page token",
			build: func(t *testing.T, srv *Server, url string) *http.Request {
				token, csrf, err := srv.mintPageToken()
				if err != nil {
					t.Fatal(err)
				}
				srv.pageTokenMu.Lock()
				srv.pageTokens[token] = pageTokenEntry{expiry: time.Now().Add(-time.Minute), csrf: csrf}
				srv.pageTokenMu.Unlock()
				return exchangeRequest(t, url, token, csrf, csrf)
			},
			reason: "page token invalid, expired, or already redeemed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, ts, logBuf := newLoggingAuthTestServer(t)
			resp, err := http.DefaultClient.Do(tc.build(t, srv, ts.URL))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			logged := logBuf.String()
			if !strings.Contains(logged, "rejected request with invalid or missing bearer token") {
				t.Errorf("no audit line for this branch, got: %s", logged)
			}
			// The FIND-11 cadence: remote address, path and a cumulative
			// count, matching what authMiddleware emits.
			for _, want := range []string{"remote_addr=", "path=/auth/exchange", "cumulative_count=1", "reason=" + strconv.Quote(tc.reason)} {
				if !strings.Contains(logged, want) {
					t.Errorf("log line missing %q, got: %s", want, logged)
				}
			}
			if got := srv.invalidAuthAttempts.Load(); got != 1 {
				t.Errorf("invalidAuthAttempts = %d, want 1", got)
			}
		})
	}
}

// TestAuthExchangeFailuresDoNotArmLockout is the load-bearing half of P63.5:
// the exchange endpoint logs but must never feed the P27.12/FIND-14 lockout
// streak. If it did, any local process could hammer /auth/exchange and wedge
// the operator's own browser out of loading the UI — turning an observability
// fix into a self-DoS.
func TestAuthExchangeFailuresDoNotArmLockout(t *testing.T) {
	srv, ts, _ := newLoggingAuthTestServer(t)

	for i := 0; i < authLockThreshold*3; i++ {
		resp, err := http.DefaultClient.Do(exchangeRequest(t, ts.URL, "never-minted", "nonce", "nonce"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("exchange %d status = %d, want 401", i, resp.StatusCode)
		}
	}

	srv.authLockMu.Lock()
	streak, until := srv.authConsecutiveFailures, srv.authLockedUntil
	srv.authLockMu.Unlock()
	if streak != 0 {
		t.Errorf("authConsecutiveFailures = %d, want 0 — exchange failures must not feed the lockout streak", streak)
	}
	if !until.IsZero() {
		t.Errorf("authLockedUntil = %v, want zero — exchange failures must not open a lockout window", until)
	}
	if _, locked := srv.authLockoutRemaining(); locked {
		t.Error("lockout engaged after exchange failures alone")
	}

	// The operator's next real request, with the correct token, still works.
	resp := doAuthedGet(t, ts.URL+"/sessions", "secret-token-123")
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		t.Fatal("authenticated request locked out by /auth/exchange failures (self-DoS)")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated request status = %d, want 200", resp.StatusCode)
	}
}
