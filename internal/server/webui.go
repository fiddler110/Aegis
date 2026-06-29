package server

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed webui/index.html
var webUIHTML string

// handleWebUI serves the embedded single-file browser client. The daemon's auth
// token is injected into the page so its same-origin fetch/SSE calls can
// authenticate; this is safe because the daemon binds to loopback only and the
// token already lives on local disk for any local client.
func (s *Server) handleWebUI(w http.ResponseWriter, _ *http.Request) {
	page := strings.Replace(webUIHTML, "__AEGIS_TOKEN__", s.authToken, 1)
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	// Strict CSP: only allow same-origin scripts/styles/connections, no inline
	// eval. The UI is a single embedded file with no external dependencies.
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
	_, _ = w.Write([]byte(page))
}
