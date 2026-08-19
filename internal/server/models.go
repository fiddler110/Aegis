package server

import (
	"net/http"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// handleListLocalModels answers GET /models/local: whatever is actually
// pulled on the configured Ollama server, for a client-side model picker that
// wants real tags instead of modelcatalog's generic family placeholders
// ("qwen3" is not a model Ollama can load; "aegis-qwen35-9b:16k" is). The
// daemon does this rather than the TUI because it already owns the
// provider.base_url connection — the client has no independent route to it.
//
// Reachable=false covers both "not an Ollama server" (a cloud provider's
// base_url) and "Ollama unreachable right now"; the client's fallback is the
// same either way, so this endpoint deliberately does not distinguish them.
func (s *Server) handleListLocalModels(w http.ResponseWriter, r *http.Request) {
	base := ollamainfo.NativeBase(s.cfg.Provider.BaseURL)
	if base == "" {
		writeJSON(w, http.StatusOK, api.LocalModelsResponse{Reachable: false})
		return
	}
	local, ok := ollamainfo.ListLocal(r.Context(), base)
	if !ok {
		writeJSON(w, http.StatusOK, api.LocalModelsResponse{Reachable: false})
		return
	}
	out := make([]api.LocalModelSummary, 0, len(local))
	for _, m := range local {
		out = append(out, api.LocalModelSummary{
			Name:          m.Name,
			Family:        m.Family,
			ParameterSize: m.ParameterSize,
			Quantization:  m.Quantization,
			SizeBytes:     m.SizeBytes,
		})
	}
	writeJSON(w, http.StatusOK, api.LocalModelsResponse{Reachable: true, Models: out})
}
