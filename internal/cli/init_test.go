package cli

import (
	"strings"
	"testing"
)

// TestApplyDiscoveredModelDefaultsPinsModel checks the base substitution:
// a single detected model replaces the "auto" placeholder.
func TestApplyDiscoveredModelDefaultsPinsModel(t *testing.T) {
	// Deliberately not "llama3.2": that's the template's own commented-out
	// small_model placeholder text, which would make an uncommented
	// small_model assertion below a false negative/positive against the
	// untouched comment rather than an actual substitution.
	got := applyDiscoveredModelDefaults(globalConfigTemplate, []ollamaModelInfo{
		{Name: "mistral", Size: 2_000_000_000, Capabilities: []string{"completion"}},
	})
	if !strings.Contains(got, `model: "mistral"`) {
		t.Errorf("expected provider.model pinned to mistral, got:\n%s", extractProviderBlock(got))
	}
	if strings.Contains(got, `small_model: "mistral"`) {
		t.Error("expected no small_model with only one model detected")
	}
	// One model only: the guard's documented precondition (small_model set)
	// is not met, so it must stay off.
	if !strings.Contains(got, "enabled: false             # validate each final answer") {
		t.Errorf("expected output_guard to stay disabled with no small_model, got:\n%s", extractGuardBlock(got))
	}
}

// TestApplyDiscoveredModelDefaultsSkipsEmbeddingOnlyModels checks that an
// embedding-only model (capabilities: ["embedding"], no "completion") is
// never picked as provider.model or provider.small_model — it cannot serve
// chat completions at all.
func TestApplyDiscoveredModelDefaultsSkipsEmbeddingOnlyModels(t *testing.T) {
	got := applyDiscoveredModelDefaults(globalConfigTemplate, []ollamaModelInfo{
		{Name: "nomic-embed-text", Size: 100, Capabilities: []string{"embedding"}},
	})
	if !strings.Contains(got, `model: "auto"`) {
		t.Errorf("expected provider.model left as \"auto\" with only an embedding model pulled, got:\n%s", extractProviderBlock(got))
	}
}

// TestApplyDiscoveredModelDefaultsSetsSmallModelAndEnablesGuard is the P73/
// "optimal first-time use" regression: when a second, smaller chat-capable
// model is pulled alongside the main one, --first-init should not just fill
// in provider.small_model — it should also flip output_guard.enabled to
// true, since the template's own stated reason the guard ships off (the
// rubric call doubling latency on the primary model with nowhere else to
// run) no longer applies once a small_model exists to route it to.
func TestApplyDiscoveredModelDefaultsSetsSmallModelAndEnablesGuard(t *testing.T) {
	got := applyDiscoveredModelDefaults(globalConfigTemplate, []ollamaModelInfo{
		{Name: "gemma3:12b", Size: 8_000_000_000, Capabilities: []string{"completion"}},
		{Name: "llama3.2:1b", Size: 1_000_000_000, Capabilities: []string{"completion"}},
	})
	if !strings.Contains(got, `model: "gemma3:12b"`) {
		t.Errorf("expected provider.model pinned to the first (most-recently-used) model, got:\n%s", extractProviderBlock(got))
	}
	if !strings.Contains(got, `small_model: "llama3.2:1b"`) {
		t.Errorf("expected small_model set to the smallest other model, got:\n%s", extractProviderBlock(got))
	}
	if !strings.Contains(got, "enabled: true") {
		t.Errorf("expected output_guard auto-enabled once small_model was detected, got:\n%s", extractGuardBlock(got))
	}
}

// TestApplyDiscoveredModelDefaultsDetectsThinkingModel checks that a model
// whose name carries a known reasoning-model marker (the same heuristic
// `aegis doctor` warns from, looksLikeThinkingModel) flips provider.think to
// true instead of leaving the template's hardcoded false — sparing a fresh
// qwen3/deepseek-r1 install the "why is it ignoring its reasoning trace"
// surprise the template's own comment describes.
func TestApplyDiscoveredModelDefaultsDetectsThinkingModel(t *testing.T) {
	got := applyDiscoveredModelDefaults(globalConfigTemplate, []ollamaModelInfo{
		{Name: "deepseek-r1:8b", Size: 5_000_000_000, Capabilities: []string{"completion"}},
	})
	if !strings.Contains(got, "think: true") {
		t.Errorf("expected think: true for a detected reasoning-model name, got:\n%s", extractProviderBlock(got))
	}
}

// TestApplyDiscoveredModelDefaultsLeavesThinkFalseForOrdinaryModel is the
// converse of the above: a model with no reasoning-model marker in its name
// must not flip provider.think.
func TestApplyDiscoveredModelDefaultsLeavesThinkFalseForOrdinaryModel(t *testing.T) {
	got := applyDiscoveredModelDefaults(globalConfigTemplate, []ollamaModelInfo{
		{Name: "llama3.2", Size: 2_000_000_000, Capabilities: []string{"completion"}},
	})
	if !strings.Contains(got, "think: false") {
		t.Errorf("expected think: false left unchanged for an ordinary model, got:\n%s", extractProviderBlock(got))
	}
}

// TestApplyDiscoveredModelDefaultsProducesValidYAML guards against a
// substitution rule drifting out of sync with the template's literal text
// (a strings.Replace anchor silently matching nothing, or leaving the file
// malformed) — every branch above must still parse and unmarshal into
// config.Config, the same check TestTemplatesParseAndUnmarshal runs on the
// untouched templates.
func TestApplyDiscoveredModelDefaultsProducesValidYAML(t *testing.T) {
	for name, models := range map[string][]ollamaModelInfo{
		"one model":              {{Name: "llama3.2", Capabilities: []string{"completion"}}},
		"two models":             {{Name: "gemma3:12b", Capabilities: []string{"completion"}}, {Name: "llama3.2:1b", Size: 1, Capabilities: []string{"completion"}}},
		"thinking model, paired": {{Name: "deepseek-r1:8b", Capabilities: []string{"completion"}}, {Name: "llama3.2:1b", Size: 1, Capabilities: []string{"completion"}}},
		"embedding only":         {{Name: "nomic-embed-text", Capabilities: []string{"embedding"}}},
	} {
		t.Run(name, func(t *testing.T) {
			got := applyDiscoveredModelDefaults(globalConfigTemplate, models)
			loadTemplate(t, got) // fails the test on parse/unmarshal error
		})
	}
}

func extractProviderBlock(template string) string {
	i := strings.Index(template, "provider:")
	j := strings.Index(template, "stream_idle_timeout")
	if i < 0 || j < 0 || j < i {
		return template
	}
	return template[i:j]
}

func extractGuardBlock(template string) string {
	i := strings.Index(template, "output_guard:")
	if i < 0 {
		return template
	}
	end := i + 400
	if end > len(template) {
		end = len(template)
	}
	return template[i:end]
}
