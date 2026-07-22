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

// fakeOllamaServer stands up the three native endpoints initContextWindow's
// detection touches, with a /api/ps that reports psCtx as the loaded model's
// context_length (0 = model not loaded / field absent).
func fakeOllamaServer(t *testing.T, model string, psCtx int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, _ *http.Request) {
		if psCtx <= 0 {
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"name": model, "model": model, "context_length": psCtx},
		}})
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// TestInitContextWindowNativeOllamaDowngradesUnderVRAMLimit: the P39.9 fix. On
// VRAM-constrained hardware Ollama may allocate less than the requested
// num_ctx; the native path now probes /api/ps and serves the smaller *loaded*
// allocation instead of blindly trusting the configured window (which would
// reintroduce the silent front-truncation detection exists to prevent).
func TestInitContextWindowNativeOllamaDowngradesUnderVRAMLimit(t *testing.T) {
	ts := fakeOllamaServer(t, "gemma4:12b", 8192) // asked for 32768, got 8192
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.initContextWindow(context.Background())
	win, src := s.effectiveContextWindow()
	if win != 8192 || src != "ollama:loaded" {
		t.Errorf("got %d/%q, want 8192/ollama:loaded", win, src)
	}
	if !s.ctxWinFinal {
		t.Error("a loaded-model reading should be final")
	}
}

// TestInitContextWindowNativeOllamaHonoredConfigStaysConfig: when the loaded
// allocation matches (or exceeds) the configured window — the common
// well-resourced case — the config value stands and its provenance stays
// "config", not "ollama:loaded".
func TestInitContextWindowNativeOllamaHonoredConfigStaysConfig(t *testing.T) {
	ts := fakeOllamaServer(t, "gemma4:12b", 32768) // asked for 32768, got 32768
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.initContextWindow(context.Background())
	win, src := s.effectiveContextWindow()
	if win != 32768 || src != "config" {
		t.Errorf("got %d/%q, want 32768/config", win, src)
	}
	if !s.ctxWinFinal {
		t.Error("a matching loaded reading should be final")
	}
}

// TestInitContextWindowNativeOllamaUnreachableKeepsConfigAndRetries: when
// Ollama is not answering yet (the daemon commonly starts first), the native
// path keeps the configured window, stashes the base for a run-time retry, and
// stays non-final so maybeRefreshContextWindow re-detects once the model loads.
func TestInitContextWindowNativeOllamaUnreachableKeepsConfigAndRetries(t *testing.T) {
	s := ctxWinServer(32768, "ollama", "http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.initContextWindow(ctx)
	win, src := s.effectiveContextWindow()
	if win != 32768 || src != "config" {
		t.Errorf("got %d/%q, want 32768/config", win, src)
	}
	if s.ctxWinFinal {
		t.Error("an unreachable Ollama should stay non-final so it re-detects at run time")
	}
	if s.ollamaBase == "" {
		t.Error("the native base should be stashed for a run-time retry")
	}
}

// TestInitContextWindowNativeOllamaNotLoadedKeepsConfig: Ollama is up but the
// model is not loaded yet — /api/ps has no reading, /api/show pins nothing, so
// detection is non-authoritative. The configured window stands and the server
// stays non-final to re-detect after the first run loads the model.
func TestInitContextWindowNativeOllamaNotLoadedKeepsConfig(t *testing.T) {
	ts := fakeOllamaServer(t, "gemma4:12b", 0) // reachable, nothing loaded
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.initContextWindow(context.Background())
	win, src := s.effectiveContextWindow()
	if win != 32768 || src != "config" {
		t.Errorf("got %d/%q, want 32768/config", win, src)
	}
	if s.ctxWinFinal {
		t.Error("a non-authoritative reading should stay non-final until a loaded reading confirms it")
	}
}
