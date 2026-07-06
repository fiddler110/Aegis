package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
)

// handleKnowledge indexes or queries the project knowledge base directly
// against the daemon's own store (POST /knowledge) — the same
// project_knowledge tool and `aegis knowledge index` machinery, exposed so
// `/knowledge` in the TUI (P14.3) can rebuild or search the index without
// spending a model turn. Goes through the daemon rather than opening a
// second sqlite connection from the TUI process, since s.knowledge is the
// one live store instance for this workspace.
func (s *Server) handleKnowledge(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req api.KnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if s.knowledge == nil {
		writeError(w, http.StatusServiceUnavailable, "knowledge base unavailable")
		return
	}

	switch req.Action {
	case "index":
		n, err := s.knowledge.Index(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, api.KnowledgeResponse{
			DocCount:          n,
			DBPath:            s.cfg.KnowledgeDBPath(s.workspace),
			EmbeddingsEnabled: s.cfg.Embeddings.Enabled,
		})
	case "query":
		if strings.TrimSpace(req.Query) == "" {
			writeError(w, http.StatusBadRequest, "query is required")
			return
		}
		limit := req.Limit
		if limit <= 0 || limit > 20 {
			limit = 5
		}
		results, err := s.knowledge.Search(r.Context(), req.Query, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]api.KnowledgeResult, len(results))
		for i, res := range results {
			out[i] = api.KnowledgeResult{Path: res.Path, Title: res.Title, Snippet: res.Snippet, Score: res.Score}
		}
		writeJSON(w, http.StatusOK, api.KnowledgeResponse{Results: out, Count: len(out)})
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown action %q (want index or query)", req.Action))
	}
}
