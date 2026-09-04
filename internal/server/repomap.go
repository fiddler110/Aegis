package server

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/repomap"
)

// handleRepoMapIndex rebuilds the repository map against the daemon's own
// workspace (POST /repomap/index) — the same build `aegis index` runs,
// exposed so `/index` in the TUI (P14.3) can refresh both the on-disk cache
// and the daemon's cached system-prompt block without a restart.
func (s *Server) handleRepoMapIndex(w http.ResponseWriter, r *http.Request) {
	// Same budget the injector reads with (repoMapOptions): a rebuild triggered
	// from /index must write the cache the startup path would have written, or
	// `/index` in the TUI would silently resize the injected block.
	m, err := repomap.Build(s.workspace, repoMapOptions(s.cfg))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cache := filepath.Join(s.workspace, ".aegis", "repomap.json")
	if err := m.Save(cache); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.repoMapMu.Lock()
	s.repoMap = repomap.Block(m.Render())
	// This rebuild *is* a freshness check, and a stricter one than
	// repoMapFor's: stamp the clock so the next prompt build reuses it rather
	// than immediately re-walking the tree we have just finished walking.
	s.repoMapCheckedAt = time.Now()
	s.repoMapMu.Unlock()
	writeJSON(w, http.StatusOK, api.RepoMapIndexResponse{FileCount: len(m.Files), Path: cache})
}
