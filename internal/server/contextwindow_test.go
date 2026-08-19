package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/compaction"
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
	s.applyDetectedWindowFor(s.cfg.Provider.Model, ollamainfo.Result{ContextWindow: 4096, Source: ollamainfo.SourceLoaded})
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
	s.applyDetectedWindowFor(s.cfg.Provider.Model, ollamainfo.Result{ContextWindow: 4096, Source: ollamainfo.SourceDefault})
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
	s.applyDetectedWindowFor(s.cfg.Provider.Model, ollamainfo.Result{ContextWindow: 16384, Source: ollamainfo.SourceModelfile})
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

	s.maybeRefreshContextWindowFor(context.Background(), s.cfg.Provider.Model)
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

// fakeOllamaMultiModel is fakeOllamaServer with a per-model loaded window, so a
// test can give two models genuinely different served contexts — the situation
// P52.1 exists for. loaded maps model name → /api/ps context_length (a model
// absent from the map is reported as not loaded). psHits counts /api/ps
// requests so a test can assert the per-model cache actually caches.
func fakeOllamaMultiModel(t *testing.T, loaded map[string]int, psHits *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, _ *http.Request) {
		if psHits != nil {
			psHits.Add(1)
		}
		models := []map[string]any{}
		for name, ctxLen := range loaded {
			if ctxLen > 0 {
				models = append(models, map[string]any{"name": name, "model": name, "context_length": ctxLen})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"models": models})
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// TestEffectiveContextWindowForResolvesPerModel is the P52.1 core: the window is
// keyed by model, not by server. A persona-pinned or routed small model gets its
// own (smaller) window — believing it had the primary model's headroom is what
// let Ollama silently drop the system prompt — and resolving it must not disturb
// the global model's entry, which /status reports. (The summarizer is tuned from
// the compaction model's entry instead — see
// TestSummarizerTunedToCompactionModelNotGlobal.)
func TestEffectiveContextWindowForResolvesPerModel(t *testing.T) {
	var psHits atomic.Int32
	ts := fakeOllamaMultiModel(t, map[string]int{"gemma4:12b": 32768, "small:1b": 4096}, &psHits)
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.initContextWindow(context.Background())

	win, src := s.effectiveContextWindowFor(context.Background(), "small:1b")
	if win != 4096 || src != "ollama:loaded" {
		t.Errorf("small model: got %d/%q, want 4096/ollama:loaded", win, src)
	}
	if gwin, gsrc := s.effectiveContextWindow(); gwin != 32768 || gsrc != "config" {
		t.Errorf("global model changed to %d/%q; a per-model reading must not touch it", gwin, gsrc)
	}

	// Second resolution is served from the cache — no second detection round.
	hits := psHits.Load()
	if win, _ := s.effectiveContextWindowFor(context.Background(), "small:1b"); win != 4096 {
		t.Errorf("cached window = %d, want 4096", win)
	}
	if psHits.Load() != hits {
		t.Errorf("/api/ps hit again (%d → %d); the per-model entry should be cached", hits, psHits.Load())
	}
}

// TestEffectiveContextWindowForReconcilesConfigPerEntry: the config-vs-served
// reconciliation runs per entry. The same configured context_window is a
// promise about the daemon, but what a given model is actually served is a
// property of that model — so one model can be downgraded to its served value
// while another keeps the configured one.
func TestEffectiveContextWindowForReconcilesConfigPerEntry(t *testing.T) {
	ts := fakeOllamaMultiModel(t, map[string]int{
		"gemma4:12b": 32768, // the global model, served exactly what's configured
		"small:1b":   4096,  // serves less than configured → downgrade
		"big:70b":    65536, // serves more than configured → config stands
	}, nil)
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.initContextWindow(context.Background())

	if win, src := s.effectiveContextWindowFor(context.Background(), "small:1b"); win != 4096 || src != "ollama:loaded" {
		t.Errorf("small model: got %d/%q, want 4096/ollama:loaded", win, src)
	}
	if win, src := s.effectiveContextWindowFor(context.Background(), "big:70b"); win != 32768 || src != "config" {
		t.Errorf("large model: got %d/%q, want 32768/config", win, src)
	}
}

// TestEffectiveContextWindowForNonAuthoritativeReDetects: a model that isn't
// loaded yet only yields a guess, so its entry stays non-final and the post-run
// refresh — keyed on the model the run actually used — picks up the real
// allocation once the run has loaded it. Each entry carries its own final state:
// the global model being settled must not stop a second model re-detecting.
func TestEffectiveContextWindowForNonAuthoritativeReDetects(t *testing.T) {
	loaded := map[string]int{"gemma4:12b": 32768}
	ts := fakeOllamaMultiModel(t, loaded, nil)
	s := ctxWinServer(32768, "ollama", ts.URL)
	s.initContextWindow(context.Background())
	if !s.ctxWinFinal {
		t.Fatal("the global model's loaded reading should be final")
	}

	// small:1b is not loaded: config stands, entry stays non-final.
	if win, src := s.effectiveContextWindowFor(context.Background(), "small:1b"); win != 32768 || src != "config" {
		t.Errorf("pre-load: got %d/%q, want 32768/config", win, src)
	}
	s.ctxWinMu.Lock()
	e, ok := s.ctxWinByModel["small:1b"]
	s.ctxWinMu.Unlock()
	if !ok || e.final {
		t.Fatalf("entry = %+v (present=%v); a not-yet-loaded model must stay non-final", e, ok)
	}

	// The run loads it, and Ollama reports an allocation below what config
	// promised — exactly the silent-truncation case, now caught per model.
	loaded["small:1b"] = 4096
	s.maybeRefreshContextWindowFor(context.Background(), "small:1b")
	if win, src := s.effectiveContextWindowFor(context.Background(), "small:1b"); win != 4096 || src != "ollama:loaded" {
		t.Errorf("post-run: got %d/%q, want 4096/ollama:loaded", win, src)
	}
	if gwin, gsrc := s.effectiveContextWindow(); gwin != 32768 || gsrc != "config" {
		t.Errorf("global model changed to %d/%q; refreshing another model must not touch it", gwin, gsrc)
	}
}

// TestEffectiveContextWindowForNonOllamaUsesConfig: with nothing to detect
// against (a cloud provider), every model resolves to the configured window,
// exactly as the global model does — no probing, no behavior change.
func TestEffectiveContextWindowForNonOllamaUsesConfig(t *testing.T) {
	s := ctxWinServer(200000, "anthropic", "")
	s.initContextWindow(context.Background())
	if win, src := s.effectiveContextWindowFor(context.Background(), "claude-other"); win != 200000 || src != "config" {
		t.Errorf("got %d/%q, want 200000/config", win, src)
	}
}

// TestSummarizerTunedToCompactionModelNotGlobal is the second half of P52.1:
// compaction prefers provider.small_model, so tuning the summarizer to the
// *primary* model's window is the same wrong-model mismatch one layer down —
// and worse there, because the request that gets silently truncated is the
// summarizer's own, producing the broken/empty summary P39.8's latch exists to
// stop looping on. The global entry must stay on the primary model's window,
// since that is what /status reports.
func TestSummarizerTunedToCompactionModelNotGlobal(t *testing.T) {
	ts := fakeOllamaMultiModel(t, map[string]int{"gemma4:12b": 32768, "small:1b": 4096}, nil)
	s := ctxWinServer(0, "ollama", ts.URL)
	s.cfg.Provider.SmallModel = "small:1b"
	s.compModel = "small:1b"
	s.initContextWindow(context.Background())

	win, _ := s.effectiveContextWindowFor(context.Background(), s.compModel)
	if win != 4096 {
		t.Fatalf("compaction model window = %d, want 4096", win)
	}
	s.summarizer = compaction.New(compaction.Options{ContextWindow: win, MaxBudget: 1})
	if got := s.summarizer.ContextWindow(); got != 4096 {
		t.Fatalf("summarizer built with %d, want the compaction model's 4096", got)
	}

	// A later reading for the *global* model must not retune the summarizer —
	// the compactor is daemon-wide but it runs on small:1b, not on this model.
	s.ctxWinMu.Lock()
	s.applyDetectedWindowFor("gemma4:12b", ollamainfo.Result{ContextWindow: 32768, Source: ollamainfo.SourceLoaded})
	s.ctxWinMu.Unlock()
	if got := s.summarizer.ContextWindow(); got != 4096 {
		t.Errorf("summarizer retuned to %d by the global model's reading; want it left at the compaction model's 4096", got)
	}

	// A reading for the compaction model itself must retune it.
	s.ctxWinMu.Lock()
	s.applyDetectedWindowFor("small:1b", ollamainfo.Result{ContextWindow: 2048, Source: ollamainfo.SourceLoaded})
	s.ctxWinMu.Unlock()
	if got := s.summarizer.ContextWindow(); got != 2048 {
		t.Errorf("summarizer window = %d after the compaction model was re-detected, want 2048", got)
	}
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

// TestApplyDetectedWindowCompatPathTakesGuessOverConfig is P61.8's core, and
// the deliberate inverse of TestApplyDetectedWindowConfigWinsOverGuess above:
// the same non-authoritative reading that must lose to config on the native
// adapter must *win* on the OpenAI-compat path. There, config is never sent as
// num_ctx, so a modelfile/default reading is not a guess about what will be
// served — it is what will be served.
func TestApplyDetectedWindowCompatPathTakesGuessOverConfig(t *testing.T) {
	s := ctxWinServer(32768, "openai", "http://127.0.0.1:9/v1")
	s.applyDetectedWindowFor(s.cfg.Provider.Model, ollamainfo.Result{ContextWindow: 4096, Source: ollamainfo.SourceDefault})
	win, src := s.effectiveContextWindow()
	if win != 4096 || src != "ollama:default" {
		t.Errorf("got %d/%q, want 4096/ollama:default — a configured window the /v1 path never sends is not evidence", win, src)
	}
	if s.ctxWinFinal {
		t.Error("a non-authoritative reading should still stay non-final so a loaded reading can refine it")
	}
}

// TestApplyDetectedWindowNativePathUnaffectedByCompatRule guards the carve-out's
// boundary: config still has to mean something on the adapter that actually
// sends it. Same inputs as the test above, native provider.
func TestApplyDetectedWindowNativePathUnaffectedByCompatRule(t *testing.T) {
	s := ctxWinServer(32768, "ollama", "http://127.0.0.1:9")
	s.applyDetectedWindowFor(s.cfg.Provider.Model, ollamainfo.Result{ContextWindow: 4096, Source: ollamainfo.SourceDefault})
	if win, src := s.effectiveContextWindow(); win != 32768 || src != "config" {
		t.Errorf("got %d/%q, want 32768/config", win, src)
	}
}

// TestConfigEntryCompatSubstitutesOllamaDefault: with nothing detected, a
// configured window on an unambiguously-Ollama compat base is replaced by
// Ollama's documented out-of-the-box window rather than trusted. This is the
// first-turn exposure P61.8 is about — the turn carrying the full system prompt
// is otherwise spent believing there is 8x the room Ollama is serving.
func TestConfigEntryCompatSubstitutesOllamaDefault(t *testing.T) {
	s := ctxWinServer(32768, "openai", "http://127.0.0.1:11434/v1")
	e := s.configEntry(s.cfg.Provider.Model, false)
	if e.win != ollamainfo.DefaultServeContext || e.src != "ollama:compat-default" {
		t.Errorf("got %d/%q, want %d/ollama:compat-default", e.win, e.src, ollamainfo.DefaultServeContext)
	}
}

// TestConfigEntryAmbiguousCompatBaseKeepsConfig: the substitution rides the
// stricter of the two predicates. A bare /v1 base also matches LM Studio and
// liteLLM, which have their own serving defaults — inventing Ollama's 4096 for
// a server that isn't Ollama would be a new wrong answer, not a fix.
func TestConfigEntryAmbiguousCompatBaseKeepsConfig(t *testing.T) {
	s := ctxWinServer(32768, "openai", "http://127.0.0.1:1234/v1")
	if e := s.configEntry(s.cfg.Provider.Model, true); e.win != 32768 || e.src != "config" {
		t.Errorf("got %d/%q, want 32768/config", e.win, e.src)
	}
}

// TestInitContextWindowCompatUnreachableSubstitutesAndRetries: the compat
// counterpart of TestInitContextWindowNativeOllamaUnreachableKeepsConfigAndRetries.
// Before P61.8 an unreachable server on this path pinned the configured window
// *final*, so the overstatement outlived every re-detection opportunity.
func TestInitContextWindowCompatUnreachableSubstitutesAndRetries(t *testing.T) {
	if c, err := net.DialTimeout("tcp", "127.0.0.1:11434", 200*time.Millisecond); err == nil {
		c.Close()
		t.Skip("an Ollama server is answering on :11434; this test needs the server-not-up branch")
	}
	s := ctxWinServer(32768, "openai", "http://127.0.0.1:11434/v1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.initContextWindow(ctx)
	win, src := s.effectiveContextWindow()
	if win != ollamainfo.DefaultServeContext || src != "ollama:compat-default" {
		t.Errorf("got %d/%q, want %d/ollama:compat-default", win, src, ollamainfo.DefaultServeContext)
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
