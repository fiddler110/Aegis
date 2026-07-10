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
)

// --- authentication & security middleware ---

// generateAndWriteToken creates a cryptographic random token and writes it to
// path with user-only permissions. The client reads this file to authenticate.
//
// The 0o600 mode bit is sufficient on POSIX but cosmetic on Windows, where a
// new file inherits its parent directory's ACL rather than deriving
// permissions from the mode argument — on a shared Windows host another
// local account can often still read the file. restrictToOwner applies a
// real, non-inherited ACL restricting the file to its owner on Windows and
// is a no-op on POSIX (see token_windows.go / token_other.go).
func generateAndWriteToken(path string) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", err
	}
	if err := restrictToOwner(path); err != nil {
		return "", fmt.Errorf("restrict auth token permissions: %w", err)
	}
	return token, nil
}

// authMiddleware checks for a valid Bearer token on all requests except
// /healthz. Requests without a valid token receive 401.
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
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		}
		provided := auth[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
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

// mintPageToken generates a random, single-use token scoped to one /ui page
// load and records its expiry in s.pageTokens (keyed by token, guarded by
// pageTokenMu). It stands in for the real daemon auth token in the HTML
// response so that token never appears in a page a local process could read
// off disk/DOM and replay indefinitely (P15.12); the frontend immediately
// trades it for the real token via exchangePageToken/POST /auth/exchange.
func (s *Server) mintPageToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	s.pageTokenMu.Lock()
	if s.pageTokens == nil {
		s.pageTokens = make(map[string]time.Time)
	}
	s.pageTokens[token] = time.Now().Add(pageTokenTTL)
	s.pageTokenMu.Unlock()
	return token, nil
}

// exchangePageToken redeems a page token minted by mintPageToken: it must
// exist and not be expired. Either way the token is removed on this call so
// it can never be redeemed twice (single-use), and expired entries are
// swept opportunistically here rather than by a background goroutine, since
// exchanges are the only place page tokens are ever read.
func (s *Server) exchangePageToken(token string) bool {
	if token == "" {
		return false
	}
	s.pageTokenMu.Lock()
	defer s.pageTokenMu.Unlock()
	exp, ok := s.pageTokens[token]
	delete(s.pageTokens, token)
	if !ok {
		return false
	}
	now := time.Now()
	for t, e := range s.pageTokens {
		if now.After(e) {
			delete(s.pageTokens, t)
		}
	}
	return now.Before(exp)
}

// handleAuthExchange trades a single-use page token (minted by GET /ui, sent
// here as a Bearer token since the frontend has no other credential yet) for
// the real daemon auth token used on every subsequent request. The page
// token is invalidated whether or not the exchange succeeds, so it can be
// redeemed at most once.
func (s *Server) handleAuthExchange(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		writeError(w, http.StatusUnauthorized, "missing page token")
		return
	}
	if !s.exchangePageToken(auth[len(prefix):]) {
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
