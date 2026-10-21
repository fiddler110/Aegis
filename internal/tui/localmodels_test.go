package tui

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/modelcatalog"
)

func TestLocalModelsToCatalog(t *testing.T) {
	in := []api.LocalModelSummary{
		{Name: "aegis-qwen35-9b:16k", Family: "qwen35", ParameterSize: "9.2B", Quantization: "Q4_K_M", SizeBytes: 7056739633},
		{Name: "gemma4:12b", Family: "gemma4", ParameterSize: "11.9B", Quantization: "Q4_K_M", SizeBytes: 7556508396},
	}
	out := localModelsToCatalog(in)
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}

	qwen := out[0]
	if qwen.ID != "aegis-qwen35-9b:16k" || qwen.Provider != "ollama" || qwen.Tier != modelcatalog.TierLocal {
		t.Errorf("qwen entry wrong: %+v", qwen)
	}
	// The ":16k" tag suffix is a serving-window hint (this project's own
	// aegis-*:16k/:32k convention) and should win over the quantization label.
	if qwen.Context != "16K" {
		t.Errorf("want Context derived from the :16k tag suffix, got %q", qwen.Context)
	}
	if qwen.Notes == "" {
		t.Error("want non-empty notes carrying parameter size / family / size")
	}

	// gemma4:12b's tag suffix ("12b") is not a context-window hint, so the
	// quantization level should be shown there instead.
	gemma := out[1]
	if gemma.Context != "Q4_K_M" {
		t.Errorf("want Context to fall back to quantization for a non-window tag, got %q", gemma.Context)
	}
}

func TestRemoteModelsToCatalogKeepsCuratedNotes(t *testing.T) {
	got := remoteModelsToCatalog(api.RemoteModelsResponse{Provider: "anthropic", Models: []api.RemoteModelSummary{
		{ID: "claude-opus-5-5", ContextWindow: 1_000_000, Notes: "1M ctx"},
		{ID: "claude-new-thing", DisplayName: "Claude New Thing"},
	}})
	if len(got) != 2 {
		t.Fatalf("got %d entries", len(got))
	}
	if got[0].Tier != modelcatalog.TierFrontier || got[0].Context != "1M" || !strings.Contains(got[0].Notes, "Default.") {
		t.Errorf("curated entry lost its recommendation: %+v", got[0])
	}
	if got[1].Tier != "available" || got[1].Notes != "Claude New Thing" || got[1].Context != "—" {
		t.Errorf("uncurated entry = %+v", got[1])
	}
}
