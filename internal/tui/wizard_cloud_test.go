package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
)

func TestTestRemoteNamesAMissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	c := testRemote(presetForLabel("Anthropic (Claude)"), "")
	if c.ok || !strings.Contains(c.status, "ANTHROPIC_API_KEY is not set") || !strings.Contains(c.status, ".aegis/.env") {
		t.Errorf("conn = %+v", c)
	}
}

func TestTestRemoteListsModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5-5","max_input_tokens":1000000},{"id":"claude-haiku-5-5"}],"has_more":false}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "k")
	c := testRemote(presetForLabel("Anthropic (Claude)"), srv.URL)
	if !c.ok || len(c.remote) != 2 || !strings.Contains(c.status, "2 models available") {
		t.Errorf("conn = %+v", c)
	}
}

func TestRemoteModelOptionsKeepTheCurrentModel(t *testing.T) {
	w := &wizardModel{modelName: "claude-opus-4-8", conn: connResult{ok: true, remote: []provider.ModelInfo{{ID: "claude-opus-5-5"}}}}
	main, small := w.modelOptions()
	if len(main) != 2 || main[0].Value != "claude-opus-4-8" || !strings.Contains(main[0].Key, "current") {
		t.Errorf("main options = %+v", main)
	}
	if !strings.Contains(main[1].Key, "1M ctx") {
		t.Errorf("listed model not annotated: %q", main[1].Key)
	}
	if small[0].Value != "" {
		t.Errorf("small options must start with none: %+v", small)
	}
}

func TestProviderPatchCarriesEffortCacheAndSmallBackend(t *testing.T) {
	var cfg config.Config
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Model = "claude-opus-5-5"
	w := &wizardModel{}
	w.adoptConfig(cfg)
	w.effortStr, w.cacheTTLStr = "high", "1h"
	w.smallBackend, w.smallModelName, w.smallBaseURL = smallBackendOllama, "qwen3:4b", "http://localhost:11434"
	p := w.providerPatch()
	if p.ReasoningEffort != "high" || p.CacheTTL != "1h" || p.SmallProvider != "ollama" || p.SmallBaseURL != "http://localhost:11434" || p.AllowCloudSmallModel {
		t.Errorf("patch = %+v", p)
	}
	// Choosing the main backend explicitly is the same as "same backend".
	w.smallBackend = smallBackendAnthropic
	if p := w.providerPatch(); p.SmallProvider != "" || p.SmallBaseURL != "" {
		t.Errorf("same-backend small model must not write small_provider: %+v", p)
	}
}

func TestCloudSmallModelFromLocalMainWritesTheOptIn(t *testing.T) {
	var cfg config.Config
	cfg.Provider.Default = "ollama"
	cfg.Provider.Model = "qwen3:14b"
	w := &wizardModel{}
	w.adoptConfig(cfg)
	w.smallBackend, w.smallModelName, w.allowCloudSmall = smallBackendAnthropic, "claude-haiku-5-5", true
	if p := w.providerPatch(); p.SmallProvider != "anthropic" || !p.AllowCloudSmallModel {
		t.Errorf("patch = %+v", p)
	}
}

func TestMenuShowsConnectionAndSmallBackendRows(t *testing.T) {
	w := &wizardModel{presetLabel: "Anthropic (Claude)", conn: connResult{tested: true, ok: true, status: "✓ 3 models available"}}
	var ids []string
	for _, r := range w.menuRows() {
		ids = append(ids, r.id)
		if r.id == secConnection && r.summary != "✓ 3 models available" {
			t.Errorf("connection summary = %q", r.summary)
		}
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, secConnection) || !strings.Contains(joined, secSmall) {
		t.Errorf("menu rows = %v", ids)
	}
}
