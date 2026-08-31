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
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{
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
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{
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
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{
		{Name: "gemma3:12b", Size: 8_000_000_000, Capabilities: []string{"completion"}},
		{Name: "llama3.2:1b", Size: 1_000_000_000, Capabilities: []string{"completion"}},
	})
	if !strings.Contains(got, `model: "gemma3:12b"`) {
		t.Errorf("expected provider.model pinned to the larger model, got:\n%s", extractProviderBlock(got))
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
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{
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
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{
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
			got := applyModelDefaultsNoBudget(globalConfigTemplate, models)
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

// The P82 regression, and the reason this ranking exists at all: /api/tags is
// ordered most-recently-modified first, so pulling a 3B for one experiment used
// to re-pin the whole machine to it on the next --first-init. Size decides now,
// and the listing order does not.
func TestApplyDiscoveredModelDefaultsIgnoresListingOrder(t *testing.T) {
	tiny := ollamaModelInfo{Name: "llama3.2:3b", Size: 2_000_000_000, Capabilities: []string{"completion", "tools"}}
	tiny.Details.ParameterSize = "3.2B"
	tiny.Details.Family = "llama"
	big := ollamaModelInfo{Name: "qwen35:9b", Size: 7_000_000_000, Capabilities: []string{"completion"}}
	big.Details.ParameterSize = "9.2B"
	big.Details.Family = "qwen35"

	// The small one listed first — exactly the shape that produced the bug.
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{tiny, big})
	if !strings.Contains(got, `model: "qwen35:9b"`) {
		t.Errorf("expected the 9.2B pinned regardless of listing order, got:\n%s", extractProviderBlock(got))
	}
	if !strings.Contains(got, `small_model: "llama3.2:3b"`) {
		t.Errorf("expected the 3.2B as small_model, got:\n%s", extractProviderBlock(got))
	}
	loadTemplate(t, got)
}

// A qwen3-family model carries no "thinking" capability in its Ollama manifest
// when it was imported from a GGUF, and "qwen3" was absent from the old name
// heuristic entirely — so a machine whose best model reasons landed on
// think: false. Both halves of the fix are asserted here.
func TestApplyDiscoveredModelDefaultsDetectsAQwenThinkingModel(t *testing.T) {
	m := ollamaModelInfo{Name: "aegis-qwen35-9b:32k", Size: 7_000_000_000, Capabilities: []string{"completion", "vision"}}
	m.Details.ParameterSize = "9.2B"
	m.Details.Family = "qwen35"
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{m})
	if !strings.Contains(got, "think: true") {
		t.Errorf("expected think: true for a qwen3-family model, got:\n%s", extractProviderBlock(got))
	}
	loadTemplate(t, got)
}

// The guard's own documented precondition is a small model to route verdicts
// to. With every pulled model the same size there is no such model, so the
// guard must stay off rather than quietly doubling turn latency.
func TestApplyDiscoveredModelDefaultsLeavesGuardOffWithoutASmallModel(t *testing.T) {
	a := ollamaModelInfo{Name: "alpha:9b", Size: 7_000_000_000, Capabilities: []string{"completion"}}
	a.Details.ParameterSize = "9B"
	b := ollamaModelInfo{Name: "beta:8b", Size: 6_500_000_000, Capabilities: []string{"completion"}}
	b.Details.ParameterSize = "8B"
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{a, b})
	if !strings.Contains(got, `# small_model: "llama3.2"`) {
		t.Errorf("an 8B is not a cheap companion to a 9B; expected the small_model placeholder left commented, got:\n%s", extractProviderBlock(got))
	}
	if !strings.Contains(got, "enabled: false             # validate each final answer") {
		t.Errorf("expected output_guard to stay off without a small_model, got:\n%s", extractGuardBlock(got))
	}
	loadTemplate(t, got)
}

// The project template is the global template's counterpart, not a different
// document: every section heading in one has a heading in the other, except the
// three the project scope deliberately cannot set. This is the alignment the
// P82 rewrite was asked for, and it is the thing that silently rots.
func TestProjectTemplateMirrorsTheGlobalSections(t *testing.T) {
	for _, want := range []string{
		"Provider", "Permission & behaviour", "Output validation",
		"Per-persona model overrides", "Spend guard", "Multi-agent / swarm",
		"Shell execution sandbox", "Security policies", "LSP servers",
		"MCP servers", "Process plugins",
	} {
		if !strings.Contains(projectConfigTemplate, want) {
			t.Errorf("project template has no %q section; it no longer mirrors the global one", want)
		}
	}
	// Precedence has to be stated the same way round in both files, since
	// getting it backwards is the whole risk of having two of them.
	if !strings.Contains(projectConfigTemplate, "environment variables  >  this file  >  global config") {
		t.Error("project template does not state the precedence chain")
	}
	// The three deliberate omissions must stay omitted, and stay explained.
	for _, absent := range []string{"allowed_targets:", "log_level:", "addr:"} {
		if strings.Contains(projectConfigTemplate, absent) {
			t.Errorf("project template offers %q, which is not project-settable", absent)
		}
	}
	// Every key is commented: a project override is only worth writing when
	// this repo genuinely differs, and an active key here would shadow the
	// global config the moment the file is created.
	for _, line := range strings.Split(projectConfigTemplate, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			t.Errorf("project template has an active (uncommented) line: %q", line)
		}
	}
}

// applyModelDefaultsNoBudget is the no-budget case — a true first run, where no
// config exists to have stated one. Kept as a helper so these tests never read
// the machine's real config, which applyDiscoveredModelDefaults does.
func applyModelDefaultsNoBudget(template string, models []ollamaModelInfo) string {
	return applyModelDefaults(template, models, 0, false)
}

// A re-run of --first-init --overwrite must rank against the budget the
// operator already stated, and must write it back — a budget that is not
// carried forward is deleted by the very run that should have used it.
func TestApplyModelDefaultsCarriesAStatedBudgetForward(t *testing.T) {
	big := ollamaModelInfo{Name: "big:14b", Size: 9_000_000_000, Capabilities: []string{"completion"}}
	big.Details.ParameterSize = "14B"
	small := ollamaModelInfo{Name: "small:3b", Size: 2_000_000_000, Capabilities: []string{"completion", "tools"}}
	small.Details.ParameterSize = "3B"
	models := []ollamaModelInfo{big, small}

	got := applyModelDefaults(globalConfigTemplate, models, 14.5, true)
	if !strings.Contains(got, "vram_budget_gb: 14.5") {
		t.Errorf("stated budget not written back, so --overwrite would delete it:\n%s", extractProviderBlock(got))
	}
	if strings.Contains(got, "# vram_budget_gb: 14.5") {
		t.Errorf("budget line left commented, so it has no effect:\n%s", extractProviderBlock(got))
	}
	if !strings.Contains(got, "autofit_context: true") {
		t.Errorf("autofit_context not carried forward:\n%s", extractProviderBlock(got))
	}
	cfg := loadTemplate(t, got)
	if cfg.Provider.VRAMBudgetGB != 14.5 {
		t.Errorf("template parses to vram_budget_gb %v, want 14.5", cfg.Provider.VRAMBudgetGB)
	}

	// And the budget is the ceiling the ranking actually solves against: 9 GB
	// of weights needs ~10.4 GB with KV headroom, so it fits 14.5 but not 6.
	if !strings.Contains(got, `model: "big:14b"`) {
		t.Errorf("14 GiB budget should admit the 14B:\n%s", extractProviderBlock(got))
	}
	tight := applyModelDefaults(globalConfigTemplate, models, 6, false)
	if !strings.Contains(tight, `model: "small:3b"`) {
		t.Errorf("6 GiB budget should exclude the 14B:\n%s", extractProviderBlock(tight))
	}
	if strings.Contains(tight, "autofit_context: true") {
		t.Error("autofit_context written without the operator having enabled it")
	}
}

// With no budget stated the template's own commented examples must survive
// untouched — a fresh install should read as documentation, not as a config
// with two keys mysteriously already active.
func TestApplyModelDefaultsLeavesBudgetCommentedWithoutOne(t *testing.T) {
	m := ollamaModelInfo{Name: "solo:8b", Size: 5_000_000_000, Capabilities: []string{"completion"}}
	m.Details.ParameterSize = "8B"
	got := applyModelDefaultsNoBudget(globalConfigTemplate, []ollamaModelInfo{m})
	if !strings.Contains(got, "  # vram_budget_gb: 14.5") {
		t.Errorf("commented budget example was disturbed:\n%s", extractProviderBlock(got))
	}
	cfg := loadTemplate(t, got)
	if cfg.Provider.VRAMBudgetGB != 0 || cfg.Provider.AutofitContext {
		t.Errorf("template activated budget keys nobody asked for: %+v", cfg.Provider)
	}
}

// The template has to document the two keys at all. Neither appeared in it
// before P82, so the whole P69.6/P72.1 feature was invisible to anyone who
// only ever read their own generated config.
func TestGlobalTemplateDocumentsTheBudgetKeys(t *testing.T) {
	for _, want := range []string{"vram_budget_gb", "autofit_context"} {
		if !strings.Contains(globalConfigTemplate, want) {
			t.Errorf("global template never mentions %q", want)
		}
	}
	if !strings.Contains(globalConfigTemplate, "Stated, never detected") {
		t.Error("template does not say the budget is stated rather than detected (P17.5)")
	}
}
