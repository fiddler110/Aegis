package server

import (
	"bytes"
	"errors"
	"fmt"
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
// now, once the consecutive-failure streak reaches authLockThreshold, a
// request that cannot present the token is rejected with 429 until the
// backoff window elapses, at which point normal auth resumes.
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

	// A further *invalid* attempt now meets the window rather than a 401:
	// the throttle against guessing is what the lockout is for.
	resp := doAuthedGet(t, ts.URL+"/sessions", "totally-wrong-guess")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("invalid attempt while locked = %d, want 429", resp.StatusCode)
	}

	// After the (short, base-delay) window elapses, normal auth resumes.
	time.Sleep(authLockBaseDelay + 200*time.Millisecond)
	resp = doAuthedGet(t, ts.URL+"/sessions", "secret-token-123")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after lockout expired = %d, want 200", resp.StatusCode)
	}
}

// TestAuthLockoutDoesNotWedgeTheTokenHolder is SEC-D. The lockout window used
// to be consulted *before* the token comparison, and the streak is
// process-wide, so any local process could spend ten bad requests and lock the
// operator's own client out of its own daemon for up to 60s, renewably. The
// daemon is loopback-only, so per-remote-address scoping is no help — every
// request comes from 127.0.0.1. The useful distinction is per-credential.
//
// Fails against the pre-fix code with 429 on the valid-token request.
func TestAuthLockoutDoesNotWedgeTheTokenHolder(t *testing.T) {
	srv, ts := newAuthTestServer(t)

	// A hostile (or merely broken) local process arms the window.
	for i := 0; i < authLockThreshold; i++ {
		doAuthedGet(t, ts.URL+"/sessions", "totally-wrong-guess")
	}
	if _, locked := srv.authLockoutRemaining(); !locked {
		t.Fatal("precondition: the lockout window should be armed")
	}

	// The holder of the real token is still served.
	resp := doAuthedGet(t, ts.URL+"/sessions", "secret-token-123")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid token while locked = %d, want 200; the window must not wedge the token holder", resp.StatusCode)
	}

	// ...and being served did not clear the guesser's progress toward the
	// next, longer window.
	srv.authLockMu.Lock()
	streak := srv.authConsecutiveFailures
	srv.authLockMu.Unlock()
	if streak == 0 {
		t.Error("a success inside the window cleared the failure streak; the throttle must survive it")
	}

	// The guesser still meets the window.
	if resp := doAuthedGet(t, ts.URL+"/sessions", "totally-wrong-guess"); resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("invalid attempt while locked = %d, want 429", resp.StatusCode)
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

	// The 429 is now reached by a request that cannot present the token, so
	// this asks for the body of *that* response. A valid token is served
	// through the window (SEC-D) and would return 200 with no message to check.
	req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer totally-wrong-guess-value")
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
				token, csrf, err := srv.mintPageToken("127.0.0.1:12345")
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

// TestPageTokenMintRateLimitIsPerAddress is P81.16/FIND-16. maxPageTokens
// refuses rather than evicts — deliberately, since evicting would let a flood
// invalidate legitimate page tokens — but with nothing in front of that cap,
// whoever reaches it first denies everyone else, and the operator experiences
// it as "the UI won't load". The per-address bucket makes the flood spend its
// own budget instead of the shared one: the flooding address is refused long
// before the cap is anywhere near, and a different address still mints.
func TestPageTokenMintRateLimitIsPerAddress(t *testing.T) {
	srv, _, logBuf := newLoggingAuthTestServer(t)

	const flood = 4 * maxPageTokens
	minted, limited := 0, 0
	for i := 0; i < flood; i++ {
		_, _, err := srv.mintPageToken("10.1.1.1:5000")
		switch {
		case err == nil:
			minted++
		case errors.Is(err, errMintRateLimited):
			limited++
		default:
			t.Fatalf("mint %d: unexpected error %v", i, err)
		}
	}
	if limited == 0 {
		t.Fatalf("a %d-mint flood from one address was never rate limited", flood)
	}
	// The flood must be stopped by its own bucket, not by the shared cap:
	// if it reached maxPageTokens the operator is locked out again.
	if minted > int(mintBurst)+1 {
		t.Errorf("flood minted %d tokens, want at most the %d-token burst", minted, int(mintBurst))
	}
	srv.pageTokenMu.Lock()
	outstanding := len(srv.pageTokens)
	srv.pageTokenMu.Unlock()
	if outstanding >= maxPageTokens {
		t.Errorf("outstanding page tokens = %d; the flood reached the shared cap despite the limiter", outstanding)
	}

	// The whole point: a different address is unaffected.
	if _, _, err := srv.mintPageToken("10.2.2.2:5000"); err != nil {
		t.Fatalf("a second address could not mint during another address's flood: %v", err)
	}

	// And the condition is diagnosable, naming the address responsible.
	logged := logBuf.String()
	if !strings.Contains(logged, "page-token mint rate limit engaged") {
		t.Errorf("no warn line for the engaged rate limit, got: %s", logged)
	}
	if !strings.Contains(logged, "10.1.1.1:5000") {
		t.Errorf("warn line does not name the flooding address, got: %s", logged)
	}
	if strings.Contains(logged, "10.2.2.2") {
		t.Errorf("the innocent address was reported as rate limited, got: %s", logged)
	}
	// One line per episode, not one per refused request.
	if n := strings.Count(logged, "page-token mint rate limit engaged"); n != 1 {
		t.Errorf("rate-limit warn logged %d times for one flood, want 1", n)
	}
}

// TestPageTokenMintRateLimitOverHTTP checks the same control through the
// handler an unauthenticated browser actually hits, including the status code
// split: 429 means "this address is going too fast" (retryable in a second),
// which is a different operator story from the 503 the shared cap produces.
func TestPageTokenMintRateLimitOverHTTP(t *testing.T) {
	srv := newTestWebUIServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sawLimit := false
	for i := 0; i < int(mintBurst)*3; i++ {
		resp, err := http.Get(ts.URL + "/ui")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			if resp.Header.Get("Retry-After") == "" {
				t.Error("429 from /ui carries no Retry-After")
			}
			sawLimit = true
			break
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /ui %d status = %d, want 200 or 429", i, resp.StatusCode)
		}
	}
	if !sawLimit {
		t.Error("flooding GET /ui never produced a 429")
	}
}

// TestMintLimiterStateStaysBounded is the other half of P81.16: a control
// added to stop a resource-exhaustion DoS must not become one. An attacker
// varying source addresses creates a limiter entry per address, so the map is
// swept of idle entries and hard-bounded at maxMintLimiters.
func TestMintLimiterStateStaysBounded(t *testing.T) {
	srv, _, _ := newLoggingAuthTestServer(t)

	for i := 0; i < maxMintLimiters*2; i++ {
		// Errors are expected here — the shared page-token cap is reached
		// partway through — but the limiter entry is created regardless, which
		// is exactly the state whose growth is under test.
		_, _, _ = srv.mintPageToken(fmt.Sprintf("10.%d.%d.%d:1234", i/65536, (i/256)%256, i%256))
	}

	srv.mintLimitMu.Lock()
	n := len(srv.mintLimiters)
	srv.mintLimitMu.Unlock()
	if n > maxMintLimiters {
		t.Errorf("limiter map holds %d entries, want at most %d", n, maxMintLimiters)
	}

	// An idle entry is swept rather than held for the process's lifetime.
	srv.mintLimitMu.Lock()
	srv.mintLimiters = map[string]*mintLimiter{
		"10.9.9.9": {tokens: mintBurst, last: time.Now().Add(-2 * mintLimiterIdleAfter)},
	}
	srv.mintLimitMu.Unlock()
	_, _, _ = srv.mintPageToken("10.8.8.8:1")
	srv.mintLimitMu.Lock()
	_, stillThere := srv.mintLimiters["10.9.9.9"]
	srv.mintLimitMu.Unlock()
	if stillThere {
		t.Error("an idle limiter entry survived a later mint; the map must be swept")
	}
}

// TestPageTokenCapEngagementIsLogged covers the reporting half of P81.16: if
// the shared cap does engage — the limiter bounds one address, not a hundred
// cooperating ones — the operator must be able to find out why the UI stopped
// loading, instead of reading a bare 503 with nothing in the log.
func TestPageTokenCapEngagementIsLogged(t *testing.T) {
	srv, _, logBuf := newLoggingAuthTestServer(t)

	srv.pageTokenMu.Lock()
	srv.pageTokens = make(map[string]pageTokenEntry, maxPageTokens)
	for i := range maxPageTokens {
		srv.pageTokens[strconv.Itoa(i)] = pageTokenEntry{expiry: time.Now().Add(pageTokenTTL), csrf: "x"}
	}
	srv.pageTokenMu.Unlock()

	if _, _, err := srv.mintPageToken("192.168.5.5:4444"); !errors.Is(err, errTooManyPageTokens) {
		t.Fatalf("mint past the cap returned %v, want errTooManyPageTokens", err)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "page-token cap reached") {
		t.Errorf("no warn line when the cap engaged, got: %s", logged)
	}
	if !strings.Contains(logged, "192.168.5.5:4444") {
		t.Errorf("cap warn does not name the remote address, got: %s", logged)
	}
}

// TestBrowserSessionNeverCarriesTheRealToken is the core P81.4/FIND-04
// regression: the browser must never receive s.authToken. It gets a
// revocable, expiring session credential instead.
func TestBrowserSessionNeverCarriesTheRealToken(t *testing.T) {
	srv, _, _ := newLoggingAuthTestServer(t)

	id, csrf, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if id == srv.authToken || csrf == srv.authToken {
		t.Fatal("minted browser session equals the real daemon token")
	}
	if !srv.validateAndTouchBrowserSession(id, csrf) {
		t.Fatal("a freshly minted session must validate")
	}
}

// TestBrowserSessionRejectsWrongCSRFOrUnknownID covers the two ways a
// browser-session credential can fail to authenticate: a session id that was
// never minted, and a known id presented with the wrong CSRF nonce (the
// value that never leaves the HttpOnly cookie, so a page that can only guess
// it must be rejected).
func TestBrowserSessionRejectsWrongCSRFOrUnknownID(t *testing.T) {
	srv, _, _ := newLoggingAuthTestServer(t)

	if srv.validateAndTouchBrowserSession("never-minted", "whatever") {
		t.Error("an unknown session id validated")
	}

	id, csrf, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if srv.validateAndTouchBrowserSession(id, "wrong-nonce") {
		t.Error("a session validated with the wrong CSRF nonce")
	}
	// The correct pair still works afterward — a failed guess must not have
	// consumed or corrupted the entry.
	if !srv.validateAndTouchBrowserSession(id, csrf) {
		t.Error("the correct CSRF pair failed to validate after a wrong guess")
	}
}

// TestBrowserSessionExpires covers both the idle sliding window and the
// absolute cap: an entry past its expiry is rejected and removed, and the
// sliding window can never be pushed past mintedAt+browserSessionMaxTTL.
func TestBrowserSessionExpires(t *testing.T) {
	srv, _, _ := newLoggingAuthTestServer(t)

	id, csrf, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	srv.browserSessionMu.Lock()
	entry := srv.browserSessions[id]
	entry.expiry = time.Now().Add(-time.Second)
	srv.browserSessions[id] = entry
	srv.browserSessionMu.Unlock()

	if srv.validateAndTouchBrowserSession(id, csrf) {
		t.Fatal("an expired session validated")
	}
	srv.browserSessionMu.Lock()
	_, stillThere := srv.browserSessions[id]
	srv.browserSessionMu.Unlock()
	if stillThere {
		t.Error("an expired session survived a failed validation attempt")
	}

	// The absolute cap: even continuous use cannot slide expiry past
	// mintedAt+browserSessionMaxTTL.
	id2, csrf2, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	srv.browserSessionMu.Lock()
	entry2 := srv.browserSessions[id2]
	entry2.mintedAt = time.Now().Add(-browserSessionMaxTTL + time.Minute)
	entry2.expiry = time.Now().Add(time.Hour)
	srv.browserSessions[id2] = entry2
	srv.browserSessionMu.Unlock()

	if !srv.validateAndTouchBrowserSession(id2, csrf2) {
		t.Fatal("a session inside its max TTL failed to validate")
	}
	srv.browserSessionMu.Lock()
	gotExpiry := srv.browserSessions[id2].expiry
	wantCeiling := entry2.mintedAt.Add(browserSessionMaxTTL)
	srv.browserSessionMu.Unlock()
	if gotExpiry.After(wantCeiling.Add(time.Second)) {
		t.Errorf("expiry slid past the absolute cap: got %v, cap %v", gotExpiry, wantCeiling)
	}
}

// TestBrowserSessionRevoke covers the "revocable" half of P81.4: a session
// deleted via revokeBrowserSession must stop validating, and
// revokeAllBrowserSessions must clear every outstanding one (the hook a
// future daemon.token rotation, P81.25, is expected to call).
func TestBrowserSessionRevoke(t *testing.T) {
	srv, _, _ := newLoggingAuthTestServer(t)

	id, csrf, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	srv.revokeBrowserSession(id)
	if srv.validateAndTouchBrowserSession(id, csrf) {
		t.Fatal("a revoked session still validated")
	}

	id2, csrf2, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	id3, csrf3, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	srv.revokeAllBrowserSessions()
	if srv.validateAndTouchBrowserSession(id2, csrf2) || srv.validateAndTouchBrowserSession(id3, csrf3) {
		t.Fatal("a session survived revokeAllBrowserSessions")
	}
}

// TestBrowserSessionCapEngages mirrors TestPageTokenCapEngagementIsLogged for
// the browser-session store: a flood of outstanding sessions is refused, not
// silently evicted, so a flood cannot invalidate another tab's live session.
func TestBrowserSessionCapEngages(t *testing.T) {
	srv, _, logBuf := newLoggingAuthTestServer(t)

	srv.browserSessionMu.Lock()
	srv.browserSessions = make(map[string]browserSessionEntry, maxBrowserSessions)
	for i := range maxBrowserSessions {
		srv.browserSessions[strconv.Itoa(i)] = browserSessionEntry{
			csrf: "x", mintedAt: time.Now(), expiry: time.Now().Add(browserSessionIdleTTL),
		}
	}
	srv.browserSessionMu.Unlock()

	if _, _, err := srv.mintBrowserSession("192.168.9.9:1"); !errors.Is(err, errTooManyBrowserSessions) {
		t.Fatalf("mint past the cap returned %v, want errTooManyBrowserSessions", err)
	}
	if !strings.Contains(logBuf.String(), "browser-session cap reached") {
		t.Errorf("no warn line when the browser-session cap engaged, got: %s", logBuf.String())
	}
}

// TestAuthMiddlewareAcceptsBrowserSession is the end-to-end regression for
// P81.4: a request with no Authorization header at all, only the session
// cookie and its CSRF header, must reach a protected endpoint — and the
// CSRF header alone (session id withheld, as a hostile page unable to read
// the HttpOnly cookie would be limited to) must not.
func TestAuthMiddlewareAcceptsBrowserSession(t *testing.T) {
	srv, ts := newAuthTestServer(t)

	id, csrf, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sessions", nil)
	req.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: id})
	req.Header.Set(browserSessionCSRFHeaderName, csrf)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("session cookie + matching CSRF header = %d, want 200", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/sessions", nil)
	req2.Header.Set(browserSessionCSRFHeaderName, csrf)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("CSRF header without the session cookie = %d, want 401", resp2.StatusCode)
	}
}

// TestAuthLogoutRevokesSessionAndClearsCookie covers POST /auth/logout: it
// must require a valid credential to reach at all (it is not exempted from
// authMiddleware), and once reached must both invalidate the session server-
// side and clear the cookie in the response.
func TestAuthLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	srv, ts := newAuthTestServer(t)

	// Unauthenticated: no credential at all.
	resp, err := http.Post(ts.URL+"/auth/logout", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated logout = %d, want 401", resp.StatusCode)
	}

	id, csrf, err := srv.mintBrowserSession("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: id})
	req.Header.Set(browserSessionCSRFHeaderName, csrf)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("authenticated logout status = %d, want 204", r.StatusCode)
	}
	cleared := false
	for _, c := range r.Cookies() {
		if c.Name == browserSessionCookieName {
			cleared = true
			if c.MaxAge >= 0 {
				t.Errorf("logout cookie MaxAge = %d, want negative (delete)", c.MaxAge)
			}
		}
	}
	if !cleared {
		t.Error("logout response did not clear the aegis_session cookie")
	}

	if srv.validateAndTouchBrowserSession(id, csrf) {
		t.Error("session still validates after logout")
	}
}
