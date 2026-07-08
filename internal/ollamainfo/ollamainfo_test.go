package ollamainfo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOllama serves the three native endpoints Detect touches. Any of the
// response bodies may be nil to simulate an older server / missing field.
type fakeOllama struct {
	ps   any // GET /api/ps body
	show any // POST /api/show body
}

func (f fakeOllama) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": "0.12.0"})
	})
	mux.HandleFunc("GET /api/ps", func(w http.ResponseWriter, _ *http.Request) {
		if f.ps == nil {
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
			return
		}
		json.NewEncoder(w).Encode(f.ps)
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, _ *http.Request) {
		if f.show == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(f.show)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestNativeBase(t *testing.T) {
	cases := map[string]string{
		"":                           "http://localhost:11434",
		"http://localhost:11434/v1":  "http://localhost:11434",
		"http://localhost:11434/v1/": "http://localhost:11434",
		"http://10.0.0.5:11434":      "http://10.0.0.5:11434",
		"http://gateway/ollama/v1":   "http://gateway/ollama",
	}
	for in, want := range cases {
		if got := NativeBase(in); got != want {
			t.Errorf("NativeBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectNotOllama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	if _, ok := Detect(context.Background(), ts.URL, "gemma4:12b"); ok {
		t.Fatal("Detect reported a non-Ollama server as Ollama")
	}
}

// TestDetectLoadedModelWins: /api/ps context_length is authoritative even when
// the modelfile pins a different num_ctx.
func TestDetectLoadedModelWins(t *testing.T) {
	ts := fakeOllama{
		ps: map[string]any{"models": []map[string]any{
			{"name": "gemma4:12b", "model": "gemma4:12b", "context_length": 32768},
		}},
		show: map[string]any{
			"parameters": "num_ctx  8192\nstop  \"<end>\"",
			"model_info": map[string]any{
				"general.architecture":  "gemma4",
				"gemma4.context_length": 131072,
			},
		},
	}.server(t)

	res, ok := Detect(context.Background(), ts.URL, "gemma4:12b")
	if !ok {
		t.Fatal("Detect: not recognized as Ollama")
	}
	if res.ContextWindow != 32768 || res.Source != SourceLoaded {
		t.Errorf("got %d from %q, want 32768 from loaded", res.ContextWindow, res.Source)
	}
	if !res.Authoritative() {
		t.Error("loaded result should be authoritative")
	}
	if res.ModelMax != 131072 {
		t.Errorf("ModelMax = %d, want 131072", res.ModelMax)
	}
}

// TestDetectModelfileNumCtx: model not loaded, modelfile pins num_ctx.
func TestDetectModelfileNumCtx(t *testing.T) {
	ts := fakeOllama{
		show: map[string]any{
			"parameters": "num_ctx 16384",
			"model_info": map[string]any{
				"general.architecture": "llama",
				"llama.context_length": 131072,
			},
		},
	}.server(t)

	res, ok := Detect(context.Background(), ts.URL, "llama3.2")
	if !ok {
		t.Fatal("Detect: not recognized as Ollama")
	}
	if res.ContextWindow != 16384 || res.Source != SourceModelfile {
		t.Errorf("got %d from %q, want 16384 from modelfile", res.ContextWindow, res.Source)
	}
	if res.Authoritative() {
		t.Error("modelfile result should not be authoritative")
	}
}

// TestDetectDefaultCappedByModelMax: nothing pinned, model not loaded — the
// conservative Ollama default applies, capped by a smaller training context.
func TestDetectDefaultCappedByModelMax(t *testing.T) {
	ts := fakeOllama{
		show: map[string]any{
			"model_info": map[string]any{
				"general.architecture": "phi",
				"phi.context_length":   2048,
			},
		},
	}.server(t)

	res, ok := Detect(context.Background(), ts.URL, "phi:latest")
	if !ok {
		t.Fatal("Detect: not recognized as Ollama")
	}
	if res.ContextWindow != 2048 || res.Source != SourceDefault {
		t.Errorf("got %d from %q, want 2048 from default", res.ContextWindow, res.Source)
	}
}

// TestDetectBareDefault: older server, no ps context_length, no show data.
func TestDetectBareDefault(t *testing.T) {
	ts := fakeOllama{
		ps: map[string]any{"models": []map[string]any{
			{"name": "llama3.2:latest", "model": "llama3.2:latest"}, // no context_length field
		}},
	}.server(t)

	res, ok := Detect(context.Background(), ts.URL, "llama3.2")
	if !ok {
		t.Fatal("Detect: not recognized as Ollama")
	}
	if res.ContextWindow != DefaultServeContext || res.Source != SourceDefault {
		t.Errorf("got %d from %q, want %d from default", res.ContextWindow, res.Source, DefaultServeContext)
	}
}

func TestSameModel(t *testing.T) {
	if !sameModel("llama3.2", "llama3.2:latest") {
		t.Error("missing :latest tag should match")
	}
	if sameModel("gemma4", "gemma4:12b") {
		t.Error("different explicit tags must not match")
	}
}

func TestParseNumCtx(t *testing.T) {
	if got := parseNumCtx("temperature 0.7\nnum_ctx 8192\n"); got != 8192 {
		t.Errorf("parseNumCtx = %d, want 8192", got)
	}
	if got := parseNumCtx("temperature 0.7"); got != 0 {
		t.Errorf("parseNumCtx = %d, want 0", got)
	}
}
