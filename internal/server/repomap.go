package server

import (
	"net/http"
	"path/filepath"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/repomap"
)

// handleRepoMapIndex rebuilds the repository map against the daemon's own
// workspace (POST /repomap/index) — the same build `aegis index` runs,
// exposed so `/index` in the TUI (P14.3) can refresh both the on-disk cache
// and the daemon's cached system-prompt block without a restart.
func (s *Server) handleRepoMapIndex(w http.ResponseWriter, r *http.Request) {
	m, err := repomap.Build(s.workspace, repomap.Options{})
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
	s.repoMapMu.Unlock()
	writeJSON(w, http.StatusOK, api.RepoMapIndexResponse{FileCount: len(m.Files), Path: cache})
}
