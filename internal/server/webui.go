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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
