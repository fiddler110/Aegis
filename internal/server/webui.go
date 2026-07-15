package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed webui/dist
var webUIDist embed.FS

// webUIAssets roots webUIDist at the built dist/ directory itself, so paths
// match what the daemon serves (e.g. "assets/index-<hash>.js") rather than
// the embed's repo-relative "webui/dist/assets/...". dist/ is the committed
// build output of internal/server/webui/frontend (Vite + Preact); run
// `npm run build` there after editing frontend source and commit the result.
var webUIAssets = mustSubFS(webUIDist, "webui/dist")

func mustSubFS(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		// Programmer error: dir must exist in the embed — a missing dist/
		// fails the go:embed directive itself at compile time, so this can
		// only happen if the sub-path above is wrong.
		panic(err)
	}
	return sub
}

// handleWebUI serves the built single-page app's HTML shell. Rather than
// injecting the real daemon auth token — a standing secret that would then
// sit in cleartext in page source for any local process to read and replay
// for the life of the daemon (P15.12) — a short-lived, single-use page token
// is minted per load (mintPageToken, auth.go) and injected instead. The
// frontend trades it for the real token via POST /auth/exchange on startup,
// after which it authenticates exactly as before; this is safe because the
// daemon binds to loopback only and the real token already lives on local
// disk for any local client.
//
// The web UI has full TUI-parity scope (P15, shipped): session list,
// streaming transcript, inline tool-call approvals, persona/mode switching,
// cost/token display, checkpoints/rewind, security scanning, skills, and
// memory management. This page is built from internal/server/webui/frontend
// (Vite + Preact + TypeScript) and its output (internal/server/webui/dist/)
// is embedded here — see that directory before extending this file ad hoc.
func (s *Server) handleWebUI(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(webUIAssets, "index.html")
	if err != nil {
		http.Error(w, "web UI not available", http.StatusInternalServerError)
		return
	}
	pageToken, csrf, err := s.mintPageToken()
	if err != nil {
		http.Error(w, "web UI not available", http.StatusInternalServerError)
		return
	}
	page := strings.NewReplacer(
		"__AEGIS_TOKEN__", pageToken,
		"__AEGIS_CSRF__", csrf,
	).Replace(string(raw))
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	// Strict CSP: only allow same-origin scripts/styles/connections, no
	// inline/eval. Bundled JS/CSS are external files under 'self', so unlike
	// the old hand-rolled inline-script page this no longer needs
	// 'unsafe-inline' for script-src/style-src.
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
	// HttpOnly + SameSite=Strict double-submit CSRF cookie (FIND-01/P24.1):
	// see uiCSRFCookieName's doc comment in auth.go for the full rationale.
	// Secure is conditional on the request having arrived over TLS (P31.3) —
	// the default loopback-only deployment is plaintext HTTP, where Secure
	// would make the browser silently drop the cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     uiCSRFCookieName,
		Value:    csrf,
		Path:     "/",
		MaxAge:   int(pageTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	_, _ = w.Write([]byte(page))
}

// handleWebUIAssets serves the hashed JS/CSS bundle built by the Vite
// frontend. Filenames are content-hashed, so a long, immutable cache
// lifetime is safe.
func (s *Server) handleWebUIAssets(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	http.StripPrefix("/ui/", http.FileServerFS(webUIAssets)).ServeHTTP(w, r)
}
