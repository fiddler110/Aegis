package tui

import (
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
