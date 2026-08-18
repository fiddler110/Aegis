package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The VRAM budget is an Ollama-only question (P69.6). Every other backend either
// runs where Aegis does not manage residency, or has nothing co-resident to plan
// against, so asking would be a question with no consequence.
func TestWizardAsksForAVRAMBudgetOnlyForOllama(t *testing.T) {
	for _, tc := range []struct {
		preset string
		want   string
	}{
		{"Ollama (local)", "ollama"},
		{"Anthropic (Claude)", "anthropic"},
		{"LM Studio (local)", "openai"},
		{"nonsense", "openai"},
	} {
		w := &wizardModel{presetLabel: tc.preset}
		if got := w.adapterName(); got != tc.want {
			t.Errorf("preset %q resolved to adapter %q, want %q", tc.preset, got, tc.want)
		}
	}
}

// fitOllama serves the geometry and placement a fitted window is solved from.
// weights of 0 means "nothing loaded", the common first-init case.
func fitOllama(t *testing.T, weights int64) *httptest.Server {
	t.Helper()
	const perToken = 33 * 4 * (256 + 256) * 2
	const loadedWindow = 16000
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"model_info": map[string]any{
			"general.architecture":          "qwen3",
			"qwen3.block_count":             33,
			"qwen3.attention.head_count_kv": 4,
			"qwen3.attention.key_length":    256,
			"qwen3.attention.value_length":  256,
			"qwen3.context_length":          262144,
		}})
	})
	mux.HandleFunc("/api/ps", func(w http.ResponseWriter, r *http.Request) {
		if weights <= 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
			return
		}
		size := weights + loadedWindow*perToken
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{
			"name": "qwen", "model": "qwen", "context_length": loadedWindow,
			"size": size, "size_vram": size,
		}}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// With a budget stated and the model loaded, the window is solved from what the
// hardware holds rather than from the training maximum — which for a 262144-
// context model is 131072 tokens, 16.5 GiB of KV cache before any weights.
func TestFitWindowForBudgetSizesFromTheHardware(t *testing.T) {
	ts := fitOllama(t, 4*(1<<30))
	win, note := fitWindowForBudget(context.Background(), ts.URL, "qwen", 10.5)
	if win != 51200 {
		t.Errorf("fitted window = %d, want 51200 (the figure P69.5 measured for this budget)", win)
	}
	if note != "" {
		t.Errorf("unexpected note on a successful fit: %q", note)
	}
}

// A freshly pulled model has never been loaded, so its resident weights cannot
// be measured — and the tempting substitute, /api/tags' on-disk size, overstates
// a multimodal model by a vision projector that is never resident. So: no window
// is fitted, and the user is told the one command that finishes the job.
func TestFitWindowForBudgetRefusesAnUnloadedModel(t *testing.T) {
	ts := fitOllama(t, 0)
	win, note := fitWindowForBudget(context.Background(), ts.URL, "qwen", 10.5)
	if win != 0 {
		t.Errorf("fitted %d tokens against unmeasured weights, want a refusal", win)
	}
	if !strings.Contains(note, "aegis models --fit --write") {
		t.Errorf("note %q does not name the command that completes the setup", note)
	}
	if !strings.Contains(note, "Budget saved") {
		t.Errorf("note %q does not say the budget was still written", note)
	}
}
