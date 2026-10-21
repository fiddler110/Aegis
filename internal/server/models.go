package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/modelcatalog"
	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/provider"
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

// remoteModelsTimeout bounds GET /models/remote: the picker blocks on it.
const remoteModelsTimeout = 8 * time.Second

// nonChatModelMarkers drop listing entries no agent turn can use. OpenAI's
// /models mixes embedding, speech and image models in with chat ones; offering
// them invites a pick that fails on the next turn.
var nonChatModelMarkers = []string{"embedding", "embed-", "whisper", "tts", "dall-e", "moderation", "transcribe", "realtime", "audio", "image", "search-preview"}

// handleListRemoteModels answers GET /models/remote: the configured cloud or
// OpenAI-compatible provider's own model listing, annotated from
// modelcatalog.Lookup. The provider's window wins over the table's when it
// reports one. The native Ollama provider answers Supported=false — its
// picker path is /models/local, which reports far more than an id.
func (s *Server) handleListRemoteModels(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Provider.Default == "ollama" || s.adapter == nil {
		writeJSON(w, http.StatusOK, api.RemoteModelsResponse{Supported: false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), remoteModelsTimeout)
	defer cancel()
	resp := api.RemoteModelsResponse{Supported: true, Provider: s.cfg.Provider.Default}
	models, err := provider.ListModels(ctx, s.adapter)
	if errors.Is(err, provider.ErrModelListingUnsupported) {
		writeJSON(w, http.StatusOK, api.RemoteModelsResponse{Supported: false})
		return
	}
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	for _, m := range models {
		lower := strings.ToLower(m.ID)
		skip := false
		for _, marker := range nonChatModelMarkers {
			if strings.Contains(lower, marker) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		caps := modelcatalog.Lookup(m.ID)
		if m.ContextWindow > 0 {
			caps.ContextWindow = m.ContextWindow
		}
		resp.Models = append(resp.Models, api.RemoteModelSummary{
			ID: m.ID, DisplayName: m.DisplayName, ContextWindow: caps.ContextWindow, Notes: caps.Annotation(),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
