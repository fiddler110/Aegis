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

// handleWebUI serves the built single-page app's HTML shell. The daemon's
// auth token is injected into the page so its same-origin fetch/SSE calls
// can authenticate; this is safe because the daemon binds to loopback only
// and the token already lives on local disk for any local client.
//
// Current scope covers the core chat loop only — session list, streaming
// transcript, inline tool-call approvals — not persona/mode switching,
// cost/token display, checkpoints/rewind, security scanning, skills, or
// memory management, which today are TUI/CLI-only. That gap is the subject
// of research/roadmap.md's P15 track. P15.1 (single-file vs. a small
// bundled frontend) is resolved: this page is now built from
// internal/server/webui/frontend (Vite + Preact + TypeScript) and its
// output (internal/server/webui/dist/) is embedded here — see that
// directory and research/roadmap.md for the rest of the P15 breakdown
// before extending this file ad hoc.
func (s *Server) handleWebUI(w http.ResponseWriter, _ *http.Request) {
	raw, err := fs.ReadFile(webUIAssets, "index.html")
	if err != nil {
		http.Error(w, "web UI not available", http.StatusInternalServerError)
		return
	}
	page := strings.Replace(string(raw), "__AEGIS_TOKEN__", s.authToken, 1)
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
