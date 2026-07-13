package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
