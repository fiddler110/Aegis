package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// --- authentication & security middleware ---

// generateAndWriteToken creates a cryptographic random token and writes it to
// path with user-only permissions. The client reads this file to authenticate.
//
// The 0o600 mode bit is sufficient on POSIX but cosmetic on Windows, where a
// new file inherits its parent directory's ACL rather than deriving
// permissions from the mode argument — on a shared Windows host another
// local account can often still read the file. fsguard.RestrictToOwner
// applies a real, non-inherited ACL restricting the file to its owner on
// Windows and is a no-op on POSIX (see internal/fsguard).
func generateAndWriteToken(path string) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", err
	}
	if err := fsguard.RestrictToOwner(path); err != nil {
		return "", fmt.Errorf("restrict auth token permissions: %w", err)
	}
	return token, nil
}

// authMiddleware checks for a valid Bearer token on all requests except
// /healthz. Requests without a valid token receive 401. While a P27.12/
// FIND-14 lockout window is active (see registerAuthFailure), every request
// — including one carrying a correct token — is rejected with 429 instead.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz is public; the web UI page itself is served without a token
		// (a browser navigation can't send one). /ui no longer injects the real
		// daemon token — it mints a short-lived, single-use page token instead
		// (see mintPageToken) and /auth/exchange trades that page token for the
		// real one the frontend then uses for every other call, so neither of
		// those two endpoints can require the real token up front either.
		if r.URL.Path == "/healthz" || r.URL.Path == "/ui" || strings.HasPrefix(r.URL.Path, "/ui/") || r.URL.Path == "/auth/exchange" {
			next.ServeHTTP(w, r)
			return
		}
		// authToken is always non-empty at startup (ListenAndServe rejects an
		// empty token), but guard defensively to avoid an accidental open-door
		// if the field were ever zero-valued in a test helper.
		if s.authToken == "" {
			writeError(w, http.StatusInternalServerError, "server misconfigured: auth token missing")
			return
		}
		if remaining, locked := s.authLockoutRemaining(); locked {
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
				"too many invalid authentication attempts; try again in %s", remaining.Round(time.Second)))
			return
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			s.recordInvalidAuthAttempt(r)
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
		provided := auth[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) != 1 {
			s.recordInvalidAuthAttempt(r)
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		s.resetAuthFailureStreak()
		next.ServeHTTP(w, r)
	})
}

// invalidAuthLogEvery caps how often a rejected-auth attempt is logged: one
// in every N cumulative failures, so a probe hammering the daemon produces
// steady signal in the log without spamming it once per request (FIND-11).
// The counter itself is still incremented on every failure regardless of
// whether this particular one is logged.
const invalidAuthLogEvery = 5

// authLockThreshold is the number of consecutive invalid-auth attempts
// (since the last successful request, or process start) that engages a
// P27.12/FIND-14 lockout window. Set well above ordinary operator error — a
// stale token after a restart, a copy-paste mistake — so a normal client
// never trips it, while still throttling a process methodically guessing at
// the bearer token. Deliberately higher than invalidAuthLogEvery: logging
// cadence and lockout are independent controls with independent tuning.
const authLockThreshold = 10

// authLockBaseDelay/authLockMaxDelay bound the P27.12/FIND-14 exponential
// backoff: the first lockout lasts authLockBaseDelay; each additional
// authLockThreshold-sized batch of failures beyond that doubles the window,
// capped at authLockMaxDelay so a persistent attacker is throttled at a
// bounded (not unbounded) rate.
const (
	authLockBaseDelay = 1 * time.Second
	authLockMaxDelay  = 60 * time.Second
)

// recordInvalidAuthAttempt bumps the process-wide invalid-bearer-token
// counter and, on a coarse cadence, emits a slog.Warn with the cumulative
// count so a probe against the daemon's auth is auditable. It deliberately
// never logs the attempted token value itself. Both authMiddleware failure
// branches (missing "Bearer " prefix, and a token that doesn't match) call
// this — a probe that never sends a Bearer prefix at all is just as worth
// surfacing as one that guesses wrong. Also feeds registerAuthFailure, the
// P27.12/FIND-14 lockout tracker.
func (s *Server) recordInvalidAuthAttempt(r *http.Request) {
	n := s.invalidAuthAttempts.Add(1)
	if n == 1 || n%invalidAuthLogEvery == 0 {
		s.logger.Warn("rejected request with invalid or missing bearer token",
			"remote_addr", r.RemoteAddr,
			"path", r.URL.Path,
			"cumulative_count", n,
		)
	}
	s.registerAuthFailure()
}

// registerAuthFailure extends the FIND-11 logging-only counter above with
// actual throttling (P27.12/FIND-14): once the consecutive-failure streak
// reaches authLockThreshold, it opens (or extends) a lockout window with
// exponential backoff. Requests arriving while already locked are rejected
// by authLockoutRemaining before they ever reach here, so the streak — and
// therefore the backoff — only grows from failures that land after a
// previous window has fully expired, which is what produces the
// exponential-not-linear growth for a persistent attacker.
func (s *Server) registerAuthFailure() {
	s.authLockMu.Lock()
	defer s.authLockMu.Unlock()
	s.authConsecutiveFailures++
	if s.authConsecutiveFailures < authLockThreshold {
		return
	}
	delay := authLockBaseDelay << uint(s.authConsecutiveFailures-authLockThreshold)
	if delay <= 0 || delay > authLockMaxDelay { // <=0 guards a pathological shift overflow
		delay = authLockMaxDelay
	}
	until := time.Now().Add(delay)
	if until.After(s.authLockedUntil) {
		s.authLockedUntil = until
	}
	s.logger.Warn("invalid-auth lockout engaged",
		"consecutive_failures", s.authConsecutiveFailures,
		"locked_for", delay,
	)
}

// authLockoutRemaining reports whether a P27.12/FIND-14 lockout window is
// currently active and, if so, how much longer it lasts.
func (s *Server) authLockoutRemaining() (time.Duration, bool) {
	s.authLockMu.Lock()
	defer s.authLockMu.Unlock()
	if s.authLockedUntil.IsZero() {
		return 0, false
	}
	remaining := time.Until(s.authLockedUntil)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// resetAuthFailureStreak clears the consecutive-invalid-attempt streak after
// a successful authenticated request, so a client that briefly presented a
// stale token (e.g. mid-token-rotation) never carries a permanent penalty
// toward the next lockout.
func (s *Server) resetAuthFailureStreak() {
	s.authLockMu.Lock()
	defer s.authLockMu.Unlock()
	s.authConsecutiveFailures = 0
}

// originMiddleware blocks requests with a non-loopback Origin header to
// mitigate DNS rebinding attacks against the local daemon.
func (s *Server) originMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if !isLoopbackOrigin(origin) {
				writeError(w, http.StatusForbidden, "cross-origin request blocked")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// pageTokenTTL is how long a page token minted by GET /ui stays redeemable
// at POST /auth/exchange before it expires unused. A browser load exchanges
// it within milliseconds of parsing the page, so this only needs to survive
// normal page-load latency, not idle time.
const pageTokenTTL = 60 * time.Second

// uiCSRFCookieName/uiCSRFHeaderName implement a double-submit CSRF binding
// for the page-token mint/exchange flow (FIND-01/P24.1). Without this, any
// local process that can reach the loopback port — not only the operator's
// own browser — can call GET /ui, read the minted page token out of the
// response body, and redeem it at /auth/exchange for the real daemon token,
// collapsing the whole auth model to "can this process reach 127.0.0.1".
// Binding the exchange to a same-origin proof closes the realistic instance
// of that gap: a hostile *webpage* (another tab, a malicious site, a
// compromised browser extension) driving the flow on the victim's behalf.
// It relies on two facts the browser enforces and this handler does not:
//  1. GET /ui's Set-Cookie is HttpOnly, so no page's JS — same-origin or
//     not — can read its value; the browser attaches it automatically only
//     to requests to this origin.
//  2. A cross-origin page cannot read the response body of its own fetch to
//     GET /ui (blocked by CORS — no Access-Control-Allow-Origin is sent)
//     and cannot frame this page to scrape its DOM (X-Frame-Options: DENY),
//     so it cannot obtain the matching nonce embedded in the HTML to send
//     back as the header.
//
// A raw local process with direct HTTP access (not going through a browser)
// is unaffected by either fact and can still complete the whole flow itself
// — that residual risk is the same class as reading daemon.token directly
// off disk for a same-OS-user adversary, and is out of scope for this
// mitigation (see FIND-01 in the threat model report and its P24.1 roadmap
// entry for the fuller writeup).
const (
	uiCSRFCookieName = "aegis_ui_csrf"
	uiCSRFHeaderName = "X-Aegis-CSRF"
)

// pageTokenEntry records a minted page token's expiry and the CSRF nonce
// bound to it at mint time.
type pageTokenEntry struct {
	expiry time.Time
	csrf   string
}

// mintPageToken generates a random, single-use token scoped to one /ui page
// load plus a CSRF nonce bound to it, recording both in s.pageTokens (keyed
// by token, guarded by pageTokenMu). The token stands in for the real daemon
// auth token in the HTML response so that token never appears in a page a
// local process could read off disk/DOM and replay indefinitely (P15.12);
// the frontend trades both for the real token via exchangePageToken/POST
// /auth/exchange, which requires the csrf nonce as well (see
// uiCSRFCookieName doc comment).
func (s *Server) mintPageToken() (token, csrf string, err error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(buf[:])
	var cbuf [32]byte
	if _, err := rand.Read(cbuf[:]); err != nil {
		return "", "", err
	}
	csrf = hex.EncodeToString(cbuf[:])
	s.pageTokenMu.Lock()
	if s.pageTokens == nil {
		s.pageTokens = make(map[string]pageTokenEntry)
	}
	s.pageTokens[token] = pageTokenEntry{expiry: time.Now().Add(pageTokenTTL), csrf: csrf}
	s.pageTokenMu.Unlock()
	return token, csrf, nil
}

// exchangePageToken redeems a page token minted by mintPageToken: it must
// exist, not be expired, and csrf must match the nonce minted alongside it.
// The token is removed on this call regardless of outcome so it can never be
// redeemed twice (single-use), and expired entries are swept opportunistically
// here rather than by a background goroutine, since exchanges are the only
// place page tokens are ever read.
func (s *Server) exchangePageToken(token, csrf string) bool {
	if token == "" {
		return false
	}
	s.pageTokenMu.Lock()
	defer s.pageTokenMu.Unlock()
	entry, ok := s.pageTokens[token]
	delete(s.pageTokens, token)
	now := time.Now()
	for t, e := range s.pageTokens {
		if now.After(e.expiry) {
			delete(s.pageTokens, t)
		}
	}
	if !ok || now.After(entry.expiry) {
		return false
	}
	return csrf != "" && subtle.ConstantTimeCompare([]byte(csrf), []byte(entry.csrf)) == 1
}

// handleAuthExchange trades a single-use page token (minted by GET /ui, sent
// here as a Bearer token since the frontend has no other credential yet) for
// the real daemon auth token used on every subsequent request. The page
// token is invalidated whether or not the exchange succeeds, so it can be
// redeemed at most once. The request must also present the double-submit
// CSRF nonce minted alongside the page token — both via the HttpOnly cookie
// GET /ui set and via an explicit header only same-origin JS could have
// constructed — see uiCSRFCookieName's doc comment for why this binds the
// exchange to the browser that actually loaded the page.
func (s *Server) handleAuthExchange(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		writeError(w, http.StatusUnauthorized, "missing page token")
		return
	}
	cookie, err := r.Cookie(uiCSRFCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "missing csrf cookie")
		return
	}
	header := r.Header.Get(uiCSRFHeaderName)
	if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		writeError(w, http.StatusUnauthorized, "csrf token mismatch")
		return
	}
	if !s.exchangePageToken(auth[len(prefix):], header) {
		writeError(w, http.StatusUnauthorized, "invalid or expired page token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.authToken})
}

func isLoopbackOrigin(origin string) bool {
	host := origin
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host = strings.TrimRight(host, "/")
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	// Strip IPv6 brackets that remain when there is no port (e.g. "[::1]").
	h = strings.Trim(h, "[]")
	ip := net.ParseIP(h)
	return (ip != nil && ip.IsLoopback()) || h == "localhost"
}

// isLoopbackAddr reports whether a listen address (e.g. "127.0.0.1:4127",
// "localhost:4127", or ":4127") resolves to loopback only. An empty host
// (":4127") binds every interface and is therefore not loopback (FIND-08).
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
