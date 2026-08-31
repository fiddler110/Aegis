package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
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
// /healthz. Requests without a valid token receive 401, or 429 while a
// P27.12/FIND-14 lockout window is active (see registerAuthFailure). A
// request carrying the correct token is served throughout the window.
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
		// The token is checked *before* the lockout window, and a valid one is
		// let through it. The window is process-wide and the daemon is
		// loopback-only, so consulting it first meant any local process could
		// spend ten bad requests and then wedge the operator's own client out
		// of its own daemon for up to 60s, renewably — a self-DoS reachable by
		// anything on the box. Per-remote-address scoping is no help when
		// every request comes from 127.0.0.1; the useful distinction is
		// per-credential, and "presented the right token" is exactly that.
		// The throttle against guessing is untouched: a guesser has no valid
		// token by definition, so it still meets the 429 on every attempt.
		// /auth/exchange already reasons its way to this same conclusion for
		// its own route.
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		hasPrefix := strings.HasPrefix(auth, prefix)
		// Compare unconditionally so the timing does not vary with the reason
		// for rejection; a missing header compares against the empty string.
		provided := ""
		if hasPrefix {
			provided = auth[len(prefix):]
		}
		valid := hasPrefix && subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) == 1

		if !valid {
			if remaining, locked := s.authLockoutRemaining(); locked {
				writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
					"too many invalid authentication attempts; try again in %s", remaining.Round(time.Second)))
				return
			}
			s.recordInvalidAuthAttempt(r)
			if !hasPrefix {
				writeError(w, http.StatusUnauthorized, "missing authorization")
				return
			}
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		// Deliberately do not reset the streak while a window is active: the
		// holder of a valid token gets service, but a concurrent guesser's
		// progress toward the next (longer) window is not cleared for it.
		if _, locked := s.authLockoutRemaining(); !locked {
			s.resetAuthFailureStreak()
		}
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

// logInvalidAuthAttempt bumps the process-wide invalid-auth counter and, on
// the coarse invalidAuthLogEvery cadence, emits a slog.Warn with the
// cumulative count so a probe against the daemon's auth is auditable. It
// deliberately never logs the attempted token value itself. reason names the
// branch that rejected the request; it is a fixed string from the handler,
// never attacker-supplied data.
//
// This is the logging half only — it does not touch the P27.12/FIND-14
// lockout streak. Callers that should also arm the lockout use
// recordInvalidAuthAttempt instead; POST /auth/exchange (P63.5) calls this
// one directly, because that endpoint is the one route a browser must be
// able to reach with no daemon token in hand, and a lockout armed from there
// would let any local process wedge the operator's own UI out of loading.
func (s *Server) logInvalidAuthAttempt(r *http.Request, reason string) {
	n := s.invalidAuthAttempts.Add(1)
	if n == 1 || n%invalidAuthLogEvery == 0 {
		s.logger.Warn("rejected request with invalid or missing bearer token",
			"remote_addr", r.RemoteAddr,
			"path", r.URL.Path,
			"reason", reason,
			"cumulative_count", n,
		)
	}
}

// recordInvalidAuthAttempt logs the attempt (see logInvalidAuthAttempt) and
// additionally feeds registerAuthFailure, the P27.12/FIND-14 lockout tracker.
// Both authMiddleware failure branches (missing "Bearer " prefix, and a token
// that doesn't match) call this — a probe that never sends a Bearer prefix at
// all is just as worth surfacing as one that guesses wrong.
func (s *Server) recordInvalidAuthAttempt(r *http.Request) {
	s.logInvalidAuthAttempt(r, "bearer token missing or mismatched")
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
func (s *Server) mintPageToken(remoteAddr string) (token, csrf string, err error) {
	// P81.16: the rate limit runs before any randomness is generated and
	// before the shared cap is consulted, so a flooder is stopped at its own
	// bucket rather than at the cap every other caller shares.
	if !s.allowPageTokenMint(remoteAddr) {
		return "", "", errMintRateLimited
	}
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
	defer s.pageTokenMu.Unlock()
	if s.pageTokens == nil {
		s.pageTokens = make(map[string]pageTokenEntry)
	}
	// M3: sweep here too, and refuse to mint past a cap. GET /ui is exempt from
	// authMiddleware and mints on every load, while the only sweep lived in
	// exchangePageToken — so a local process that loads the page and never
	// exchanges grows this map without bound and never triggers the cleanup.
	// That is the memory-growth DoS the invalidAuthAttempts design deliberately
	// avoids, reachable on the one endpoint that needs no credential.
	now := time.Now()
	for t, e := range s.pageTokens {
		if now.After(e.expiry) {
			delete(s.pageTokens, t)
		}
	}
	// Every entry lives at most pageTokenTTL (60s), so reaching the cap after
	// the sweep means a minting rate no browser produces. Refusing is the right
	// answer rather than evicting: evicting would let a flood invalidate the
	// page tokens of legitimate loads, turning a memory bound into a working
	// denial of the UI.
	if len(s.pageTokens) >= maxPageTokens {
		// P81.16: the cap engaging means the operator's own UI is about to
		// stop loading, and before this warning that condition presented as
		// "the UI won't load" with nothing in the log. Warn once per episode
		// (see pageTokenCapWarned) naming the address that met the cap.
		if !s.pageTokenCapWarned {
			s.pageTokenCapWarned = true
			s.logger.Warn("page-token cap reached; /ui loads are being refused",
				"remote_addr", remoteAddr,
				"outstanding", len(s.pageTokens),
				"cap", maxPageTokens,
				"ttl", pageTokenTTL,
			)
		}
		return "", "", errTooManyPageTokens
	}
	s.pageTokenCapWarned = false
	s.pageTokens[token] = pageTokenEntry{expiry: now.Add(pageTokenTTL), csrf: csrf}
	return token, csrf, nil
}

// mintLimiter is one remote address's token bucket for GET /ui page-token
// mints, plus the bookkeeping that keeps its logging quiet: warned is set on
// the first refusal and cleared once the bucket has refilled, so a sustained
// flood yields one warn line per episode rather than one per request.
type mintLimiter struct {
	tokens   float64
	last     time.Time
	warned   bool
	refusals uint64
}

// The P81.16 mint rate limit. A browser mints exactly one page token per
// GET /ui — assets under /ui/ do not mint — so even a user leaning on the
// reload key produces a handful of mints per second at most, and an
// automated reload loop in a dev workflow a few more. mintBurst allows a
// full minute's worth of that at once and mintRefillPerSecond sustains one
// load per second indefinitely, which is orders of magnitude below the
// >1024-inside-60s a flood needs to reach maxPageTokens, and orders of
// magnitude above anything a real client does. Sized to be generous on
// purpose: this control exists to stop a runaway, not to police the
// operator's browser.
const (
	mintBurst            = 60.0
	mintRefillPerSecond  = 1.0
	maxMintLimiters      = 1024
	mintLimiterIdleAfter = 10 * time.Minute
)

// errMintRateLimited is returned by mintPageToken when the calling address
// has exhausted its P81.16 bucket. Distinct from errTooManyPageTokens so the
// handler can answer 429 (this caller is going too fast) rather than 503
// (the daemon as a whole is saturated).
var errMintRateLimited = errors.New("too many /ui page loads from this address; retry shortly")

// allowPageTokenMint reports whether remoteAddr may mint another page token
// now, consuming one unit of its bucket when it may. Keyed on the address's
// host part: the port differs per connection, so keying on the full
// RemoteAddr would give every TCP connection a fresh budget and limit
// nothing.
//
// The limiter's own state is bounded two ways, because a control added to
// stop a resource-exhaustion DoS must not be one. Entries idle for
// mintLimiterIdleAfter with a full bucket are swept on each call, and if the
// map is still at maxMintLimiters the least-recently-seen entry is evicted
// to make room. Evicting here — unlike evicting a page token — costs nothing
// a legitimate client can miss: it only resets a rate budget, and
// maxPageTokens still stands behind it as the hard bound.
func (s *Server) allowPageTokenMint(remoteAddr string) bool {
	key := mintLimiterKey(remoteAddr)
	now := time.Now()

	s.mintLimitMu.Lock()
	defer s.mintLimitMu.Unlock()
	if s.mintLimiters == nil {
		s.mintLimiters = make(map[string]*mintLimiter)
	}
	lim, ok := s.mintLimiters[key]
	if !ok {
		s.sweepMintLimitersLocked(now)
		lim = &mintLimiter{tokens: mintBurst, last: now}
		s.mintLimiters[key] = lim
	}

	// Refill for the elapsed time, then spend.
	if elapsed := now.Sub(lim.last); elapsed > 0 {
		lim.tokens += elapsed.Seconds() * mintRefillPerSecond
		if lim.tokens > mintBurst {
			lim.tokens = mintBurst
		}
	}
	lim.last = now
	if lim.tokens >= 1 {
		lim.tokens--
		lim.warned = false
		return true
	}
	lim.refusals++
	if !lim.warned {
		lim.warned = true
		s.logger.Warn("page-token mint rate limit engaged; refusing /ui loads from this address",
			"remote_addr", remoteAddr,
			"burst", int(mintBurst),
			"refill_per_second", mintRefillPerSecond,
			"refusals", lim.refusals,
		)
	}
	return false
}

// sweepMintLimitersLocked drops limiter entries that have been idle long
// enough to have refilled completely, and — if that left the map still at
// its cap — the single least-recently-seen entry, so admitting a new address
// can never grow the map past maxMintLimiters. Callers must hold mintLimitMu.
func (s *Server) sweepMintLimitersLocked(now time.Time) {
	for k, l := range s.mintLimiters {
		if now.Sub(l.last) >= mintLimiterIdleAfter {
			delete(s.mintLimiters, k)
		}
	}
	for len(s.mintLimiters) >= maxMintLimiters {
		oldestKey := ""
		var oldest time.Time
		for k, l := range s.mintLimiters {
			if oldestKey == "" || l.last.Before(oldest) {
				oldestKey, oldest = k, l.last
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.mintLimiters, oldestKey)
	}
}

// mintLimiterKey reduces a net/http RemoteAddr to the host part it should be
// limited by. An address that doesn't parse is used whole rather than
// dropped: bucketing an unparseable address together is safer than exempting
// it from the limit.
func mintLimiterKey(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil && host != "" {
		return host
	}
	return remoteAddr
}

// maxPageTokens bounds the unexchanged page tokens held at once. Entries expire
// after pageTokenTTL, so this is a bound on the *rate* of unauthenticated /ui
// loads, far above any real browser's. It is a process-wide bound and refuses
// rather than evicts, which is why the P81.16 per-address limiter sits in front
// of it: without one, whoever reaches the cap first denies everyone else.
const maxPageTokens = 1024

// errTooManyPageTokens is returned by mintPageToken when maxPageTokens
// unexpired tokens are already outstanding.
var errTooManyPageTokens = errors.New("too many outstanding page tokens; retry shortly")

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
//
// Every rejection here is logged through logInvalidAuthAttempt (P63.5).
// authMiddleware exempts this path — it has to, since the frontend holds only
// a page token at this point — which previously left it the one route where a
// failure produced no audit record at all, while a probe against any
// *authenticated* route produced a warn line with a remote address and a
// running count. That inversion, not guessing a page token, is what this
// closes: FIND-01 accepts a local process driving this flow as residual risk,
// and accepted risk should still be observable. It logs only; arming the
// lockout from here is deliberately out of scope (see logInvalidAuthAttempt).
func (s *Server) handleAuthExchange(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		s.logInvalidAuthAttempt(r, "page token missing")
		writeError(w, http.StatusUnauthorized, "missing page token")
		return
	}
	cookie, err := r.Cookie(uiCSRFCookieName)
	if err != nil || cookie.Value == "" {
		s.logInvalidAuthAttempt(r, "csrf cookie missing")
		writeError(w, http.StatusUnauthorized, "missing csrf cookie")
		return
	}
	header := r.Header.Get(uiCSRFHeaderName)
	if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		s.logInvalidAuthAttempt(r, "csrf header missing or mismatched")
		writeError(w, http.StatusUnauthorized, "csrf token mismatch")
		return
	}
	if !s.exchangePageToken(auth[len(prefix):], header) {
		s.logInvalidAuthAttempt(r, "page token invalid, expired, or already redeemed")
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
