package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

func ctxWinServer(cfgWin int, providerDefault, baseURL string) *Server {
	cfg := &config.Config{Provider: config.ProviderConfig{
		Default:       providerDefault,
		Model:         "gemma4:12b",
		BaseURL:       baseURL,
		ContextWindow: cfgWin,
	}}
	return &Server{cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestApplyDetectedWindowServedValueWinsOverLargerConfig: a verified smaller
// served window must beat a larger configured one — honoring the config would
// reintroduce exactly the silent-truncation failure detection exists to fix.
func TestApplyDetectedWindowServedValueWinsOverLargerConfig(t *testing.T) {
	s := ctxWinServer(32768, "ollama", "")
	s.applyDetectedWindow(ollamainfo.Result{ContextWindow: 4096, Source: ollamainfo.SourceLoaded})
	win, src := s.effectiveContextWindow()
	if win != 4096 || src != "ollama:loaded" {
		t.Errorf("got %d/%q, want 4096/ollama:loaded", win, src)
	}
	if !s.ctxWinFinal {
		t.Error("a loaded-model reading should be final")
	}
}

// TestApplyDetectedWindowConfigWinsOverGuess: a non-authoritative detection
// (model not loaded, Ollama default assumed) must not override explicit
// config — the user may have raised OLLAMA_CONTEXT_LENGTH server-side.
func TestApplyDetectedWindowConfigWinsOverGuess(t *testing.T) {
	s := ctxWinServer(32768, "ollama", "")
	s.applyDetectedWindow(ollamainfo.Result{ContextWindow: 4096, Source: ollamainfo.SourceDefault})
	win, src := s.effectiveContextWindow()
	if win != 32768 || src != "config" {
		t.Errorf("got %d/%q, want 32768/config", win, src)
	}
	if s.ctxWinFinal {
		t.Error("should keep re-detecting until a loaded-model reading confirms or refutes the config")
	}
}

// TestApplyDetectedWindowAutoFillsUnsetConfig: context_window 0 takes the
// detected value outright.
func TestApplyDetectedWindowAutoFillsUnsetConfig(t *testing.T) {
	s := ctxWinServer(0, "ollama", "")
	s.applyDetectedWindow(ollamainfo.Result{ContextWindow: 16384, Source: ollamainfo.SourceModelfile})
	win, src := s.effectiveContextWindow()
	if win != 16384 || src != "ollama:modelfile" {
		t.Errorf("got %d/%q, want 16384/ollama:modelfile", win, src)
	}
}

// TestMaybeRefreshContextWindowUpgradesToLoaded: after a run loads the model,
// the refresh path picks up the /api/ps reading and stops re-detecting. Also
// covers the user's real deployment shape: provider "openai" with base_url
// pointed at an Ollama server.
func TestMaybeRefreshContextWindowUpgradesToLoaded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"name": "gemma4:12b", "model": "gemma4:12b", "context_length": 32768},
		}})
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	s := ctxWinServer(0, "openai", ts.URL+"/v1")
	s.initContextWindow(context.Background())
	// Detection already sees the fake /api/ps here; force the pre-load state a
	// real first run starts from, then refresh.
	s.ctxWinMu.Lock()
	s.ctxWin, s.ctxWinSrc, s.ctxWinFinal = ollamainfo.DefaultServeContext, "ollama:default", false
	s.ctxWinMu.Unlock()

	s.maybeRefreshContextWindow(context.Background())
	win, src := s.effectiveContextWindow()
	if win != 32768 || src != "ollama:loaded" {
		t.Errorf("got %d/%q, want 32768/ollama:loaded", win, src)
	}
	if !s.ctxWinFinal {
		t.Error("refresh to a loaded reading should be final")
	}
}

// TestInitContextWindowNonOllamaIsFinal: cloud providers never probe and
// never re-detect.
func TestInitContextWindowNonOllamaIsFinal(t *testing.T) {
	s := ctxWinServer(200000, "anthropic", "")
	s.initContextWindow(context.Background())
	win, src := s.effectiveContextWindow()
	if win != 200000 || src != "config" {
		t.Errorf("got %d/%q, want 200000/config", win, src)
	}
	if !s.ctxWinFinal {
		t.Error("non-Ollama providers should be final at startup")
	}
}

// TestInitContextWindowNativeOllamaWithConfigSkipsProbe: the native Ollama
// adapter (P33.9) pins options.num_ctx to the configured window on every
// request, so a configured context_window is trusted outright — no /api/ps
// or /api/show probe needed. Uses a bogus, unreachable base_url to prove no
// network call happens: a probe attempt would time out and this test would
// fail on the deadline instead of returning immediately.
func TestInitContextWindowNativeOllamaWithConfigSkipsProbe(t *testing.T) {
	s := ctxWinServer(32768, "ollama", "http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	s.initContextWindow(ctx)
	win, src := s.effectiveContextWindow()
	if win != 32768 || src != "config" {
		t.Errorf("got %d/%q, want 32768/config", win, src)
	}
	if !s.ctxWinFinal {
		t.Error("a configured window on the native ollama adapter should be final immediately")
	}
}
