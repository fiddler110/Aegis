package tui

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/modelpick"
)

func trueP() *bool { b := true; return &b }

// sampleConfig is a config with something set in every section this dialog can
// edit, so a round-trip can tell "carried through" from "happened to be zero".
func sampleConfig() config.Config {
	var cfg config.Config
	cfg.Provider.Default = "ollama"
	cfg.Provider.BaseURL = "http://localhost:11434"
	cfg.Provider.Model = "aegis-qwen35-9b:32k"
	cfg.Provider.SmallModel = "llama3.2:3b"
	cfg.Provider.MaxTokens = 8192
	cfg.Provider.MaxRetries = 3
	cfg.Provider.Think = trueP()
	cfg.Provider.ContextWindow = 32768
	cfg.Provider.VRAMBudgetGB = 14.5
	cfg.Cost.BudgetUSD = 2
	cfg.Cost.MaxTurnStallSec = 2100
	cfg.Cost.MaxTokensPerRun = 400000
	cfg.Cost.MaxGeneratedTokensPerRun = 120000
	cfg.Cost.SessionCapUSD = 5
	cfg.OutputGuard.Enabled = true
	cfg.OutputGuard.Mode = "llm"
	cfg.OutputGuard.MaxRetries = 1
	cfg.OutputGuard.Rubric = "be correct"
	return cfg
}

// The dialog opens from the config on disk, not from an empty form. The
// pre-P82 wizard started blank, so opening it and pressing enter through
// re-wrote settings the operator had tuned by hand.
func TestAdoptConfigSeedsEveryEditableField(t *testing.T) {
	w := &wizardModel{}
	w.adoptConfig(sampleConfig())

	for _, tc := range []struct{ name, got, want string }{
		{"model", w.modelName, "aegis-qwen35-9b:32k"},
		{"small model", w.smallModelName, "llama3.2:3b"},
		{"base URL", w.baseURL, "http://localhost:11434"},
		{"max tokens", w.maxTokensStr, "8192"},
		{"max retries", w.maxRetriesStr, "3"},
		{"think", w.thinkStr, "enabled"},
		{"context window", w.contextWindowStr, "32768"},
		{"VRAM budget", w.vramBudgetStr, "14.5"},
		{"budget USD", w.budgetUSDStr, "2"},
		{"turn stall", w.turnStallStr, "2100"},
		{"guard mode", w.guardMode, "llm"},
		{"preset", w.presetLabel, "Ollama (local)"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if !w.guardEnabled {
		t.Error("guard enabled was not carried in from the config")
	}
}

// An unset value must round-trip as unset. "" and "0" mean different things for
// a context window, and only one of them is what the operator typed.
func TestAdoptConfigLeavesUnsetFieldsBlank(t *testing.T) {
	var cfg config.Config
	cfg.Provider.Default = "ollama"
	w := &wizardModel{}
	w.adoptConfig(cfg)
	for name, got := range map[string]string{
		"context window": w.contextWindowStr,
		"VRAM budget":    w.vramBudgetStr,
		"budget USD":     w.budgetUSDStr,
		"turn stall":     w.turnStallStr,
		"max retries":    w.maxRetriesStr,
	} {
		if got != "" {
			t.Errorf("%s = %q for an unset value, want blank", name, got)
		}
	}
	if w.thinkStr != "auto" {
		t.Errorf("think = %q with no value in config, want auto", w.thinkStr)
	}
}

// Nothing is written until Save, so a section the operator never opened must
// produce a patch identical to what was loaded — otherwise saving a model
// change silently reflows the cost block's comments.
func TestUntouchedSectionsProduceNoChange(t *testing.T) {
	w := &wizardModel{}
	w.adoptConfig(sampleConfig())
	if w.costPatch() != w.costPatchFromLoaded() {
		t.Errorf("cost patch differs from the loaded config without an edit:\n got %+v\nwant %+v",
			w.costPatch(), w.costPatchFromLoaded())
	}
	if w.guardPatch() != w.guardPatchFromLoaded() {
		t.Errorf("guard patch differs from the loaded config without an edit:\n got %+v\nwant %+v",
			w.guardPatch(), w.guardPatchFromLoaded())
	}
}

// patchCost and patchOutputGuard splice in a freshly built block, so a key
// absent from the patch struct is deleted from the operator's file. Editing the
// two fields this dialog shows must not drop the six it does not.
func TestEditingOneCostFieldCarriesTheRestThrough(t *testing.T) {
	w := &wizardModel{}
	w.adoptConfig(sampleConfig())
	w.budgetUSDStr = "9"

	got := w.costPatch()
	if got.BudgetUSD != 9 {
		t.Errorf("budget_usd = %v, want the edited 9", got.BudgetUSD)
	}
	for name, pair := range map[string][2]int{
		"max_tokens_per_run":           {got.MaxTokensPerRun, 400000},
		"max_generated_tokens_per_run": {got.MaxGeneratedTokensPerRun, 120000},
		"max_turn_stall":               {got.MaxTurnStallSec, 2100},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d after editing budget_usd, want %d carried through", name, pair[0], pair[1])
		}
	}
	if got.SessionCapUSD != 5 {
		t.Errorf("session_cap_usd = %v, want 5 carried through", got.SessionCapUSD)
	}

	w.guardEnabled = false
	if r := w.guardPatch().Rubric; r != "be correct" {
		t.Errorf("rubric = %q after toggling the guard off, want it carried through", r)
	}
}

// The menu is meant to be read as a settings summary — the point of the
// redesign — so every row has to name its current value, not just its section.
func TestMenuRowsShowCurrentValues(t *testing.T) {
	w := &wizardModel{}
	w.adoptConfig(sampleConfig())
	rows := w.menuRows()

	byID := map[string]string{}
	for _, r := range rows {
		byID[r.id] = r.summary
	}
	for id, want := range map[string]string{
		secModels:     "aegis-qwen35-9b:32k",
		secGeneration: "8192",
		secMemory:     "14.5",
		secSpend:      "$2",
		secGuard:      "on",
	} {
		if !strings.Contains(byID[id], want) {
			t.Errorf("menu row %q summary %q does not mention %q", id, byID[id], want)
		}
	}
	for _, required := range []string{secSave, secCancel} {
		if _, ok := byID[required]; !ok {
			t.Errorf("menu has no %q row; there is no way out of the dialog", required)
		}
	}
}

// The memory section is Ollama-only, for the same reason the VRAM budget
// question always was: every other backend runs where Aegis does not manage
// residency (P69.6).
func TestMemorySectionOnlyAppearsForOllama(t *testing.T) {
	w := &wizardModel{}
	w.adoptConfig(sampleConfig())
	if !hasRow(w.menuRows(), secMemory) {
		t.Error("no memory section on an Ollama backend")
	}

	var cloud config.Config
	cloud.Provider.Default = "anthropic"
	cloud.Provider.Model = "claude-opus-4-8"
	w2 := &wizardModel{}
	w2.adoptConfig(cloud)
	if hasRow(w2.menuRows(), secMemory) {
		t.Error("memory section offered on a cloud backend, where there is nothing to plan residency for")
	}
}

func hasRow(rows []menuRow, id string) bool {
	for _, r := range rows {
		if r.id == id {
			return true
		}
	}
	return false
}

// "Use recommended" is one keystroke for the whole decision: main model, small
// model, and the think setting that follows from the main model. Leaving think
// behind is how the reported machine ended up on think: false with a qwen.
func TestApplyingTheRecommendationSetsBothModelsAndThink(t *testing.T) {
	w := &wizardModel{section: secModels}
	w.adoptConfig(sampleConfig())
	w.rec = modelpick.Selection{Main: "qwen35:9b", Small: "llama3.2:3b", Think: true}
	w.modelName = applyRecommendedValue
	w.thinkStr = "disabled"

	w.leaveSection()

	if w.modelName != "qwen35:9b" || w.smallModelName != "llama3.2:3b" {
		t.Errorf("applied recommendation gave main=%q small=%q", w.modelName, w.smallModelName)
	}
	if w.thinkStr != "enabled" {
		t.Errorf("think = %q after applying a recommendation that reasons, want enabled", w.thinkStr)
	}
	if w.notice == "" {
		t.Error("applying the recommendation said nothing about what it changed")
	}
}

// Pointing background calls at the primary model is the same as having no small
// model, and saying so out loud is what keeps the guard's cost note honest.
func TestSmallModelEqualToMainIsCleared(t *testing.T) {
	w := &wizardModel{section: secModels}
	w.adoptConfig(sampleConfig())
	w.smallModelName = w.modelName
	w.leaveSection()
	if w.smallModelName != "" {
		t.Errorf("small model = %q, want cleared when it equals the main model", w.smallModelName)
	}
}

// The guard's precondition — a small model to route verdicts to — was
// previously invisible from the UI, so an operator enabling it had no way to
// learn why their turns had just doubled in length.
func TestGuardNoteNamesTheLatencyTrade(t *testing.T) {
	w := &wizardModel{}
	w.adoptConfig(sampleConfig())
	if !strings.Contains(w.guardCostNote(), "llama3.2:3b") {
		t.Errorf("guard note %q does not say where verdicts run", w.guardCostNote())
	}

	w.smallModelName = ""
	note := w.guardCostNote()
	if !strings.Contains(note, "doubling") || !strings.Contains(note, "Models") {
		t.Errorf("guard note %q does not warn about the cost or point at the fix", note)
	}
}

// The think question is only answerable if the dialog says what the selected
// model looks like. The pre-P82 form asked it against a generic "for reasoning
// models (Claude 3.7+, o1, etc.)".
func TestThinkNoteDescribesTheSelectedModel(t *testing.T) {
	w := &wizardModel{
		modelName: "aegis-qwen35-9b:32k",
		local: []modelpick.Model{
			{Name: "aegis-qwen35-9b:32k", Family: "qwen35", Capabilities: []string{"completion"}},
			{Name: "llama3.2:3b", Family: "llama", Capabilities: []string{"completion", "tools"}},
		},
	}
	if !strings.Contains(w.thinkNote(), "looks like a reasoning model") {
		t.Errorf("think note %q says nothing about the qwen being a reasoning model", w.thinkNote())
	}
	w.modelName = "llama3.2:3b"
	if !strings.Contains(w.thinkNote(), "does not look like a reasoning model") {
		t.Errorf("think note %q says nothing about llama not being one", w.thinkNote())
	}
}

// The picker has to show what the ranking ranked on, or an operator overriding
// the ★ cannot see what they are trading away.
func TestModelOptionsAnnotateWhatTheRankingUsed(t *testing.T) {
	w := &wizardModel{
		local: []modelpick.Model{
			{Name: "qwen35:9b", ParameterSize: "9.2B", SizeBytes: 7 << 30, Family: "qwen35", Capabilities: []string{"completion"}},
			{Name: "llama3.2:3b", ParameterSize: "3.2B", SizeBytes: 2 << 30, Family: "llama", Capabilities: []string{"completion", "tools"}},
		},
		rec: modelpick.Selection{Main: "qwen35:9b", Small: "llama3.2:3b"},
	}
	main, small := w.modelOptions()
	if len(main) != 3 { // the "use recommended" row plus both models
		t.Fatalf("main picker has %d options, want 3 (recommendation + 2 models)", len(main))
	}
	if main[0].Value != applyRecommendedValue {
		t.Errorf("first option value = %q, want the apply-recommended sentinel", main[0].Value)
	}
	joined := main[1].Key + "|" + main[2].Key
	for _, want := range []string{"9.2B", "3.2B", "tools", "thinks", "★"} {
		if !strings.Contains(joined, want) {
			t.Errorf("model labels %q do not mention %q", joined, want)
		}
	}
	if small[0].Value != "" {
		t.Error("small-model picker offers no way back to \"none\"")
	}
}

// With no local models and no curated list the picker degrades to a free-text
// field rather than an empty select the operator cannot escape.
func TestModelOptionsEmptyForAnUnknownBackend(t *testing.T) {
	w := &wizardModel{}
	main, small := w.modelOptions()
	if len(main) != 0 || len(small) != 0 {
		t.Errorf("expected no options for an unprobed backend, got %d/%d", len(main), len(small))
	}
}

// Switching provider must not leave a model name behind that cannot resolve on
// the new backend — a "claude-opus-4-8" pointed at Ollama fails on the next
// turn, not now.
func TestSwitchingProviderDropsAnIncompatibleModel(t *testing.T) {
	var cfg config.Config
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Model = "claude-opus-4-8"
	w := &wizardModel{section: secProvider}
	w.adoptConfig(cfg)

	w.presetLabel = "OpenAI"
	w.leaveSection()
	if w.modelName == "claude-opus-4-8" {
		t.Error("an Anthropic model survived a switch to OpenAI")
	}

	// A model the new preset does know is kept.
	w2 := &wizardModel{section: secProvider}
	w2.adoptConfig(cfg)
	w2.presetLabel = "Anthropic (Claude)"
	w2.leaveSection()
	if w2.modelName != "claude-opus-4-8" {
		t.Errorf("model = %q after re-selecting the same provider, want it kept", w2.modelName)
	}
}

// presetForConfig has to recognize what is already on disk, or the dialog opens
// claiming the operator is on Ollama when they are on Groq.
func TestPresetForConfigRecognizesExistingBackends(t *testing.T) {
	for _, tc := range []struct {
		cfg  config.ProviderConfig
		want string
	}{
		{config.ProviderConfig{Default: "ollama", BaseURL: "http://localhost:11434"}, "Ollama (local)"},
		{config.ProviderConfig{Default: "ollama"}, "Ollama (local)"},
		{config.ProviderConfig{Default: "anthropic"}, "Anthropic (Claude)"},
		{config.ProviderConfig{Default: "openai"}, "OpenAI"},
		{config.ProviderConfig{Default: "openai", BaseURL: "https://api.groq.com/openai/v1"}, "Groq"},
		{config.ProviderConfig{Default: "openai", BaseURL: "http://localhost:1234/v1"}, "LM Studio (local)"},
		{config.ProviderConfig{Default: "openai", BaseURL: "https://gateway.internal/v1"}, "Custom"},
	} {
		if got := presetForConfig(tc.cfg).label; got != tc.want {
			t.Errorf("presetForConfig(%+v) = %q, want %q", tc.cfg, got, tc.want)
		}
	}
}

// The VRAM budget question, the num_ctx write and the resident-set planning all
// key off the resolved adapter, so an unrecognized label must not resolve to
// Ollama.
func TestUnknownPresetLabelIsNotOllama(t *testing.T) {
	if got := presetForLabel("nonsense").adapter; got == "ollama" {
		t.Errorf("unknown preset resolved to %q", got)
	}
}
