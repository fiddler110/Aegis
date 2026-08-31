package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/hwinfo"
	"github.com/fiddler110/aegis/internal/modelpick"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

// `/config` — a hierarchical settings menu (P82).
//
// This used to be a linear wizard: provider, then base URL, then model, then
// max tokens, then VRAM budget, then think, then a save confirm, every time,
// with no way to change one setting without walking past the other six and no
// way to see what any of them currently were. It also started from an empty
// form rather than from the config on disk, so opening it and pressing enter
// through re-wrote settings the operator had tuned by hand.
//
// The shape here is raspi-config's: a menu that names each area with its
// current value, drill in to change one thing, come back. Nothing is written
// until "Save and exit" — the working copy lives in this model, so backing out
// of a section keeps the edit and "Discard and exit" throws all of them away.
//
// The section forms deliberately stay short. A section that needs more than a
// handful of fields (the security scanners) gets its own dialog, the way
// /security-config already does, rather than growing a page here.

// ─── Provider presets ─────────────────────────────────────────────────────────

type wPreset struct {
	label       string
	adapter     string
	defaultURL  string
	defaultMax  int
	modelSource string // "discover:ollama" | "discover:lmstudio" | "curated:X" | "input"
}

var wPresets = []wPreset{
	{"Ollama (local)", "ollama", "http://localhost:11434", 8192, "discover:ollama"},
	{"LM Studio (local)", "openai", "http://localhost:1234/v1", 4096, "discover:lmstudio"},
	{"Anthropic (Claude)", "anthropic", "", 16384, "curated:anthropic"},
	{"OpenAI", "openai", "", 16384, "curated:openai"},
	{"OpenRouter", "openai", "https://openrouter.ai/api/v1", 16384, "curated:openrouter"},
	{"Groq", "openai", "https://api.groq.com/openai/v1", 8192, "curated:groq"},
	{"Custom", "openai", "", 8192, "input"},
}

var wCurated = map[string][]string{
	"anthropic":  {"claude-opus-4-8", "claude-sonnet-5", "claude-haiku-4-5-20251001"},
	"openai":     {"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "o3", "o3-mini", "o1", "o1-mini"},
	"groq":       {"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768", "gemma2-9b-it"},
	"openrouter": {"anthropic/claude-opus-4", "openai/gpt-4o", "google/gemini-2.0-flash-001", "meta-llama/llama-3.3-70b-instruct"},
}

// presetForLabel resolves a preset by its menu label. An unrecognized label
// falls back to Custom — deliberately not to Ollama: the VRAM budget question,
// the num_ctx write and the resident-set planning all key off the resolved
// adapter, and a label this list does not know is not evidence of a local
// Ollama. TestWizardAsksForAVRAMBudgetOnlyForOllama pins that direction.
func presetForLabel(label string) wPreset {
	for _, p := range wPresets {
		if p.label == label {
			return p
		}
	}
	return wPresets[len(wPresets)-1]
}

// presetForConfig guesses which preset an existing config came from, so the
// dialog opens showing what the operator actually has rather than the first
// entry of the list. Base URL is the discriminator: several presets share the
// "openai" adapter and differ only by endpoint.
func presetForConfig(p config.ProviderConfig) wPreset {
	url := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	for _, preset := range wPresets {
		if preset.defaultURL != "" && strings.EqualFold(strings.TrimRight(preset.defaultURL, "/"), url) {
			return preset
		}
	}
	switch strings.ToLower(p.Default) {
	case "ollama":
		return wPresets[0]
	case "anthropic":
		return wPresets[2]
	}
	if url == "" {
		return wPresets[3] // plain OpenAI
	}
	return wPresets[len(wPresets)-1] // Custom
}

// ─── Internal messages ────────────────────────────────────────────────────────

// configLoadedMsg carries the opening state: the config on disk plus, for a
// local backend, the ranked model list. Both are gathered off the update loop
// because the second is a network call.
type configLoadedMsg struct {
	cfg    config.Config
	err    error
	models []modelpick.Model
	rec    modelpick.Selection
}

// modelsDiscoveredMsg re-reports the model list after the provider changed.
type modelsDiscoveredMsg struct {
	models []modelpick.Model
	rec    modelpick.Selection
}

// wizardSavedMsg carries the save outcome back onto the update loop. fitNote
// travels in the message rather than being written straight to the model:
// saveCmd runs in a tea.Cmd goroutine, and assigning to the model from there is
// a data race with the renderer, however harmless the value looks.
type wizardSavedMsg struct {
	err     error
	fitNote string
}
type ripgrepInstalledMsg struct{ err error }

// ─── Phases ───────────────────────────────────────────────────────────────────

type wizardPhase int

const (
	wPhaseLoading           wizardPhase = iota // async: read config, probe for local models
	wPhaseMenu                                 // the section menu
	wPhaseSection                              // one section's form
	wPhaseDiscovery                            // async model discovery after a provider change
	wPhaseSaving                               // async config write
	wPhaseRipgrep                              // huh confirm: install ripgrep?
	wPhaseRipgrepInstalling                    // async ripgrep install (brew only)
)

// Section ids. These are the Value of the menu's select, and the switch key for
// which form to build.
const (
	secProvider   = "provider"
	secModels     = "models"
	secGeneration = "generation"
	secMemory     = "memory"
	secSpend      = "spend"
	secGuard      = "guard"
	secHostTools  = "hosttools"
	secSave       = "__save__"
	secCancel     = "__cancel__"
)

// ─── Model ────────────────────────────────────────────────────────────────────

const wizardPanelW = 76

type wizardModel struct {
	phase wizardPhase
	form  *huh.Form
	sp    spinner.Model

	// loaded is the config as read from disk at open. Only the fields this
	// dialog can write are edited; everything else is carried through by the
	// section-splicing patch functions, or passed back into the patch structs
	// that rewrite a whole block (cost, output_guard).
	loaded config.Config

	// ── working copy, bound to huh fields ──
	presetLabel      string
	baseURL          string
	modelName        string
	smallModelName   string
	maxTokensStr     string
	maxRetriesStr    string
	thinkStr         string // "auto" | "enabled" | "disabled"
	vramBudgetStr    string
	contextWindowStr string
	autofitContext   bool
	budgetUSDStr     string
	turnStallStr     string
	guardEnabled     bool
	guardMode        string
	guardRetriesStr  string

	// ── discovered models ──
	// local is the ranked list behind the model picker; rec is what
	// modelpick would choose on this machine, offered as one keystroke rather
	// than left for the operator to work out from a list of tags.
	local     []modelpick.Model
	rec       modelpick.Selection
	curated   []string
	discovery bool // true when the current preset's models come from a probe

	// ── navigation ──
	section    string // which section's form is open
	menuChoice string
	notice     string // one-line banner on the menu (e.g. "applied recommended models")

	// fitNote explains, after saving, why a stated budget did not size the
	// window — normally because the model has never been loaded, so its resident
	// weights cannot be measured and the honest answer is to say so rather than
	// size against /api/tags' on-disk figure.
	fitNote string

	done           bool
	saved          bool
	saveErr        string
	confirmRipgrep bool
	ripgrepMsg     string

	width  int
	height int
	th     theme
}

func newWizard(width, height int, th theme) *wizardModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	return &wizardModel{
		phase:  wPhaseLoading,
		width:  width,
		height: height,
		th:     th,
		sp:     sp,
	}
}

func (w *wizardModel) init() tea.Cmd {
	return tea.Batch(w.sp.Tick, loadConfigCmd())
}

// loadConfigCmd reads the config and, when it points at a local Ollama server,
// the pulled-model list — both off the update loop, since the second is HTTP.
func loadConfigCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil || cfg == nil {
			return configLoadedMsg{err: err}
		}
		msg := configLoadedMsg{cfg: *cfg}
		if base := ollamainfo.NativeBase(cfg.Provider.BaseURL); base != "" || strings.EqualFold(cfg.Provider.Default, "ollama") {
			msg.models, msg.rec = probeLocalModels(cfg.Provider.BaseURL, cfg.Provider.VRAMBudgetGB)
		}
		return msg
	}
}

// probeLocalModels lists what a local Ollama has pulled and ranks it. The
// ranking is the same one `aegis --first-init` and provider.model: "auto" use
// (internal/modelpick) — three entry points that used to disagree about which
// model this machine should run.
func probeLocalModels(baseURL string, budgetGB float64) ([]modelpick.Model, modelpick.Selection) {
	base := ollamainfo.NativeBase(baseURL)
	if base == "" {
		base = "http://localhost:11434"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	local, ok := ollamainfo.ListLocal(ctx, base)
	if !ok || len(local) == 0 {
		return nil, modelpick.Selection{}
	}
	models := make([]modelpick.Model, 0, len(local))
	for _, m := range local {
		models = append(models, modelpick.Model{
			Name:          m.Name,
			Family:        m.Family,
			ParameterSize: m.ParameterSize,
			Quantization:  m.Quantization,
			SizeBytes:     m.SizeBytes,
			Capabilities:  m.Capabilities,
			ModifiedAt:    m.ModifiedAt,
		})
	}
	return models, modelpick.Select(models, hwinfo.Detect(), budgetGB)
}

// adoptConfig seeds the working copy from the config on disk. Every field is
// rendered as the string huh will edit, and an unset value becomes an empty
// field rather than a zero — "" and "0" mean different things for a context
// window, and only one of them is what the operator typed.
func (w *wizardModel) adoptConfig(cfg config.Config) {
	w.loaded = cfg
	preset := presetForConfig(cfg.Provider)
	w.presetLabel = preset.label
	w.baseURL = cfg.Provider.BaseURL
	w.modelName = cfg.Provider.Model
	w.smallModelName = cfg.Provider.SmallModel
	w.maxTokensStr = itoaOrBlank(cfg.Provider.MaxTokens)
	w.maxRetriesStr = itoaOrBlank(cfg.Provider.MaxRetries)
	w.thinkStr = "auto"
	if cfg.Provider.Think != nil {
		w.thinkStr = "disabled"
		if *cfg.Provider.Think {
			w.thinkStr = "enabled"
		}
	}
	w.vramBudgetStr = floatOrBlank(cfg.Provider.VRAMBudgetGB)
	w.contextWindowStr = itoaOrBlank(cfg.Provider.ContextWindow)
	w.autofitContext = cfg.Provider.AutofitContext
	w.budgetUSDStr = floatOrBlank(cfg.Cost.BudgetUSD)
	w.turnStallStr = itoaOrBlank(cfg.Cost.MaxTurnStallSec)
	w.guardEnabled = cfg.OutputGuard.Enabled
	w.guardMode = cfg.OutputGuard.Mode
	if w.guardMode == "" {
		w.guardMode = "llm"
	}
	w.guardRetriesStr = itoaOrBlank(cfg.OutputGuard.MaxRetries)
	w.adoptPreset(preset)
}

// adoptPreset refreshes the model-source state for a preset, without touching
// anything the operator has already chosen.
func (w *wizardModel) adoptPreset(p wPreset) {
	w.curated = nil
	w.discovery = strings.HasPrefix(p.modelSource, "discover:")
	if key, ok := strings.CutPrefix(p.modelSource, "curated:"); ok {
		w.curated = wCurated[key]
	}
	if w.baseURL == "" {
		w.baseURL = p.defaultURL
	}
	if w.maxTokensStr == "" {
		w.maxTokensStr = strconv.Itoa(p.defaultMax)
	}
}

func (w *wizardModel) adapterName() string { return presetForLabel(w.presetLabel).adapter }

func itoaOrBlank(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func floatOrBlank(f float64) string {
	if f <= 0 {
		return ""
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func atofOrZero(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

// ─── Menu ─────────────────────────────────────────────────────────────────────

// menuRow pairs a section with the one-line summary of its current values, so
// the menu answers "what is this set to" without drilling in.
type menuRow struct {
	id      string
	label   string
	summary string
}

func (w *wizardModel) menuRows() []menuRow {
	rows := []menuRow{
		{secProvider, "Provider", fmt.Sprintf("%s · %s", w.presetLabel, orDash(w.baseURL))},
		{secModels, "Models", w.modelSummary()},
		{secGeneration, "Generation", fmt.Sprintf("%s tokens · retries %s · think %s",
			orDash(w.maxTokensStr), orDash(w.maxRetriesStr), w.thinkStr)},
	}
	if w.adapterName() == "ollama" {
		rows = append(rows, menuRow{secMemory, "Memory & context", w.memorySummary()})
	}
	rows = append(rows,
		menuRow{secSpend, "Spend & stall limits", fmt.Sprintf("budget %s · stall %s",
			usdOrUnlimited(w.budgetUSDStr), secondsOrDefault(w.turnStallStr))},
		menuRow{secGuard, "Output guard", w.guardSummary()},
		menuRow{secHostTools, "Host tools", ripgrepSummary()},
		menuRow{secSave, "Save and exit", "write the changes above to " + config.GlobalConfigPath()},
		menuRow{secCancel, "Discard and exit", "leave the config file untouched"},
	)
	return rows
}

func (w *wizardModel) modelSummary() string {
	s := orDash(w.modelName)
	if w.smallModelName != "" {
		s += " · small " + w.smallModelName
	} else {
		s += " · no small model"
	}
	return s
}

func (w *wizardModel) memorySummary() string {
	parts := []string{"window " + orDash(w.contextWindowStr)}
	if w.vramBudgetStr != "" {
		parts = append(parts, "budget "+w.vramBudgetStr+" GiB")
	} else {
		parts = append(parts, "no VRAM budget")
	}
	if w.autofitContext {
		parts = append(parts, "autofit on")
	}
	return strings.Join(parts, " · ")
}

func (w *wizardModel) guardSummary() string {
	if !w.guardEnabled {
		return "off"
	}
	return fmt.Sprintf("on · %s · %s retries", w.guardMode, orDash(w.guardRetriesStr))
}

func ripgrepSummary() string {
	if _, err := exec.LookPath("rg"); err == nil {
		return "ripgrep installed"
	}
	return "ripgrep missing — searches use the slower pure-Go walker"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func usdOrUnlimited(s string) string {
	if atofOrZero(s) <= 0 {
		return "unlimited"
	}
	return "$" + s
}

func secondsOrDefault(s string) string {
	if atoiOrZero(s) <= 0 {
		return "default"
	}
	return s + "s"
}

func (w *wizardModel) buildMenuForm() *huh.Form {
	rows := w.menuRows()
	// Pad the labels into a column so the summaries line up — the menu is meant
	// to be read down the right-hand side as a settings summary, which only
	// works if the summaries start at the same place.
	width := 0
	for _, r := range rows {
		if len(r.label) > width {
			width = len(r.label)
		}
	}
	opts := make([]huh.Option[string], len(rows))
	for i, r := range rows {
		opts[i] = huh.NewOption(fmt.Sprintf("%-*s  %s", width, r.label, r.summary), r.id)
	}
	desc := "Pick a section to change it. Nothing is written until you save."
	if w.notice != "" {
		desc = w.notice
	}
	w.menuChoice = ""
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Aegis configuration").
				Description(desc).
				Options(opts...).
				Value(&w.menuChoice).
				Height(len(opts) + 2),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// ─── Section forms ────────────────────────────────────────────────────────────

func (w *wizardModel) buildSectionForm(section string) *huh.Form {
	switch section {
	case secProvider:
		return w.buildProviderForm()
	case secModels:
		return w.buildModelsForm()
	case secGeneration:
		return w.buildGenerationForm()
	case secMemory:
		return w.buildMemoryForm()
	case secSpend:
		return w.buildSpendForm()
	case secGuard:
		return w.buildGuardForm()
	case secHostTools:
		return w.buildHostToolsForm()
	}
	return nil
}

func (w *wizardModel) buildProviderForm() *huh.Form {
	opts := make([]huh.Option[string], len(wPresets))
	for i, p := range wPresets {
		opts[i] = huh.NewOption(p.label, p.label)
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("AI provider").
				Description("Esc goes back without changing anything.").
				Options(opts...).
				Value(&w.presetLabel).
				Height(len(wPresets)+2),
			huh.NewInput().
				Title("API base URL").
				Description("Leave empty to use the provider default.").
				Placeholder("https://...").
				Value(&w.baseURL),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// applyRecommendedValue is the sentinel Value of the picker's first option. It
// is not a model name — selecting it copies modelpick's whole recommendation
// (main *and* small) into the working copy, which is the one action an operator
// staring at eight tags actually wants.
const applyRecommendedValue = "__recommended__"

func (w *wizardModel) buildModelsForm() *huh.Form {
	mainOpts, smallOpts := w.modelOptions()

	var mainField huh.Field
	if len(mainOpts) > 0 {
		mainField = huh.NewSelect[string]().
			Title("Model").
			Description(w.recommendationNote()).
			Options(mainOpts...).
			Value(&w.modelName).
			Height(minInt(len(mainOpts)+2, 12))
	} else {
		mainField = huh.NewInput().
			Title("Model").
			Placeholder("e.g. gpt-4o, llama3.2:3b").
			Value(&w.modelName)
	}

	var smallField huh.Field
	if len(smallOpts) > 0 {
		smallField = huh.NewSelect[string]().
			Title("Small model (background calls)").
			Description("Session titles, compaction and output-guard verdicts run here. Keeping them off the primary model is what makes the guard affordable locally.").
			Options(smallOpts...).
			Value(&w.smallModelName).
			Height(minInt(len(smallOpts)+2, 10))
	} else {
		smallField = huh.NewInput().
			Title("Small model (background calls)").
			Description("Optional. Leave blank to run background calls on the main model.").
			Value(&w.smallModelName)
	}

	return huh.NewForm(
		huh.NewGroup(mainField, smallField),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// modelOptions builds the two pickers. Local models are annotated with the
// facts the ranking used — parameter count, size, whether the manifest claims
// tool support, whether it reasons — because an operator overriding a
// recommendation should be able to see what they are trading away.
func (w *wizardModel) modelOptions() (main, small []huh.Option[string]) {
	if len(w.local) == 0 {
		for _, name := range w.curated {
			main = append(main, huh.NewOption(name, name))
			small = append(small, huh.NewOption(name, name))
		}
		if len(small) > 0 {
			small = append([]huh.Option[string]{huh.NewOption("(none — use the main model)", "")}, small...)
		}
		return main, small
	}
	if w.rec.Main != "" {
		main = append(main, huh.NewOption(
			fmt.Sprintf("Use recommended: %s", w.recommendedPairLabel()), applyRecommendedValue))
	}
	for _, m := range w.local {
		main = append(main, huh.NewOption(annotateModel(m, m.Name == w.rec.Main), m.Name))
	}
	small = append(small, huh.NewOption("(none — use the main model)", ""))
	for _, m := range w.local {
		small = append(small, huh.NewOption(annotateModel(m, m.Name == w.rec.Small), m.Name))
	}
	return main, small
}

func (w *wizardModel) recommendedPairLabel() string {
	if w.rec.Small == "" {
		return w.rec.Main
	}
	return w.rec.Main + " + " + w.rec.Small
}

func annotateModel(m modelpick.Model, recommended bool) string {
	var tags []string
	if p := strings.TrimSpace(m.ParameterSize); p != "" {
		tags = append(tags, p)
	}
	if m.SizeBytes > 0 {
		tags = append(tags, fmt.Sprintf("%.1fG", float64(m.SizeBytes)/float64(int64(1)<<30)))
	}
	if m.ToolCapable() {
		tags = append(tags, "tools")
	}
	if m.Thinks() {
		tags = append(tags, "thinks")
	}
	label := m.Name
	if len(tags) > 0 {
		label += "  (" + strings.Join(tags, ", ") + ")"
	}
	if recommended {
		label += " ★"
	}
	return label
}

// recommendationNote is the description under the model picker: the ranking's
// own reasons, so the ★ is explained rather than asserted.
func (w *wizardModel) recommendationNote() string {
	if len(w.rec.Reasons) == 0 {
		return "★ marks this machine's recommended pick."
	}
	return "★ = recommended. " + strings.Join(w.rec.Reasons, " ")
}

func (w *wizardModel) buildGenerationForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Max tokens per response").
				Placeholder("e.g. 8192").
				Validate(positiveInt).
				Value(&w.maxTokensStr),
			huh.NewInput().
				Title("Max retries").
				Description("Provider-level retries on a failed call. Blank = 4.").
				Validate(optionalNonNegativeInt).
				Value(&w.maxRetriesStr),
			huh.NewSelect[string]().
				Title("Extended thinking").
				Description(w.thinkNote()).
				Options(
					huh.NewOption("Auto (provider default)", "auto"),
					huh.NewOption("Enabled", "enabled"),
					huh.NewOption("Disabled", "disabled"),
				).
				Value(&w.thinkStr).
				Height(5),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// thinkNote tells the operator what the selected model looks like, which is the
// context that makes this question answerable — the pre-P82 dialog asked it
// against a generic "for reasoning models (Claude 3.7+, o1, etc.)" and left
// them to know whether their local tag was one.
func (w *wizardModel) thinkNote() string {
	for _, m := range w.local {
		if m.Name == w.modelName {
			if m.Thinks() {
				return w.modelName + " looks like a reasoning model — Enabled is usually right."
			}
			return w.modelName + " does not look like a reasoning model — Disabled is usually right."
		}
	}
	return "For reasoning models (Claude 3.7+, o1, qwen3, deepseek-r1)."
}

func (w *wizardModel) buildMemoryForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("VRAM budget (GiB)").
				Description("Memory Ollama may use, across all models at once. Stated, never detected. Blank to skip.").
				Placeholder("e.g. 14.5 on a 16 GB card").
				Validate(optionalPositiveFloat).
				Value(&w.vramBudgetStr),
			huh.NewInput().
				Title("Context window (tokens)").
				Description("Sent to Ollama as num_ctx. Blank lets the daemon size it.").
				Validate(optionalNonNegativeInt).
				Value(&w.contextWindowStr),
			huh.NewConfirm().
				Title("Re-fit the window at daemon startup?").
				Description("With a budget stated, the daemon re-solves context_window once the model's weights can be measured.").
				Affirmative("Autofit").
				Negative("Keep mine").
				Value(&w.autofitContext),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

func (w *wizardModel) buildSpendForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Budget per run (USD)").
				Description("Abort a run past this estimated cost. Blank or 0 = unlimited. No effect on a local model, which has no price.").
				Validate(optionalPositiveFloat).
				Value(&w.budgetUSDStr),
			huh.NewInput().
				Title("Turn stall timeout (seconds)").
				Description("Abort a turn after this much complete silence — no token, no tool activity. Must stay above provider.stream_idle_timeout. Blank = the built-in default.").
				Validate(optionalNonNegativeInt).
				Value(&w.turnStallStr),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

func (w *wizardModel) buildGuardForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Validate every final answer?").
				Description(w.guardCostNote()).
				Affirmative("On").
				Negative("Off").
				Value(&w.guardEnabled),
			huh.NewSelect[string]().
				Title("Mode").
				Options(
					huh.NewOption("llm — judge the answer against a rubric", "llm"),
					huh.NewOption("schema — require the declared JSON keys", "schema"),
				).
				Value(&w.guardMode).
				Height(4),
			huh.NewInput().
				Title("Corrective retries").
				Description("Retries before the raw answer is surfaced anyway. Blank = 1.").
				Validate(optionalNonNegativeInt).
				Value(&w.guardRetriesStr),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// guardCostNote states the latency trade the guard makes, and whether this
// config has already paid for the way out of it. This is the setting whose
// precondition — a small model to route verdicts to — was previously invisible
// from the UI, so an operator enabling it here had no way to learn why their
// turns had just doubled in length.
func (w *wizardModel) guardCostNote() string {
	if w.smallModelName != "" {
		return "One extra model call per answer, routed to " + w.smallModelName + " rather than the primary model."
	}
	return "One extra model call per answer. With no small model set it runs on the primary model, roughly doubling turn latency on a local setup — set one under Models first."
}

func (w *wizardModel) buildHostToolsForm() *huh.Form {
	if _, err := exec.LookPath("rg"); err == nil {
		return huh.NewForm(
			huh.NewGroup(
				huh.NewNote().
					Title("Host tools").
					Description("ripgrep is installed — file search uses it.\n\nOther optional binaries (git, gh, mmdc, plantuml) are resolved through the commands: config block; `aegis doctor` reports what is missing."),
			),
		).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
	}
	return w.buildRipgrepForm()
}

func positiveInt(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fmt.Errorf("enter a positive integer")
	}
	return nil
}

func optionalNonNegativeInt(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return fmt.Errorf("enter a non-negative integer, or leave blank")
	}
	return nil
}

func optionalPositiveFloat(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 {
		return fmt.Errorf("enter a positive number, or leave blank")
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (w *wizardModel) update(msg tea.Msg) tea.Cmd {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		w.done = true
		return nil
	}

	switch w.phase {
	case wPhaseLoading:
		return w.updateLoading(msg)
	case wPhaseMenu:
		return w.updateMenu(msg)
	case wPhaseSection:
		return w.updateSection(msg)
	case wPhaseDiscovery:
		return w.updateDiscovery(msg)
	case wPhaseSaving:
		return w.updateSaving(msg)
	case wPhaseRipgrep:
		return w.updateRipgrep(msg)
	case wPhaseRipgrepInstalling:
		return w.updateRipgrepInstalling(msg)
	}
	return nil
}

func (w *wizardModel) updateLoading(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		w.sp, cmd = w.sp.Update(msg)
		return cmd
	case configLoadedMsg:
		if msg.err != nil {
			w.saveErr = "could not read the config: " + msg.err.Error()
			w.done = true
			return nil
		}
		w.local, w.rec = msg.models, msg.rec
		w.adoptConfig(msg.cfg)
		return w.enterMenu()
	}
	return nil
}

func (w *wizardModel) enterMenu() tea.Cmd {
	w.phase = wPhaseMenu
	w.section = ""
	w.form = w.buildMenuForm()
	return w.form.Init()
}

func (w *wizardModel) updateMenu(msg tea.Msg) tea.Cmd {
	m, cmd := w.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		w.form = f
	}
	switch w.form.State {
	case huh.StateAborted:
		// Esc at the top level is "leave without writing", the same as the
		// explicit Discard row. Deliberately not a confirm dialog: nothing has
		// been written yet, so there is nothing to lose but the edits, and a
		// confirm on every exit is the friction this redesign is removing.
		w.done = true
		return nil
	case huh.StateCompleted:
		w.notice = ""
		switch w.menuChoice {
		case secCancel:
			w.done = true
			return nil
		case secSave:
			w.phase = wPhaseSaving
			return tea.Batch(w.sp.Tick, w.saveCmd())
		default:
			return w.enterSection(w.menuChoice)
		}
	}
	return cmd
}

func (w *wizardModel) enterSection(section string) tea.Cmd {
	form := w.buildSectionForm(section)
	if form == nil {
		return w.enterMenu()
	}
	w.section = section
	w.phase = wPhaseSection
	w.form = form
	return w.form.Init()
}

func (w *wizardModel) updateSection(msg tea.Msg) tea.Cmd {
	// The host-tools section is the ripgrep confirm when rg is missing, and its
	// completion runs an install rather than returning to the menu.
	if w.section == secHostTools {
		if _, err := exec.LookPath("rg"); err != nil {
			return w.updateRipgrepFromSection(msg)
		}
	}
	m, cmd := w.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		w.form = f
	}
	switch w.form.State {
	case huh.StateAborted, huh.StateCompleted:
		// Both directions return to the menu, and both keep the edits: huh has
		// already written every field into the working copy through its bound
		// pointers, so "abort" here means "stop editing this section", not
		// "undo it". Undo lives at the top level, as Discard and exit.
		return w.leaveSection()
	}
	return cmd
}

// leaveSection applies the cross-field consequences of a section's edits, then
// returns to the menu.
func (w *wizardModel) leaveSection() tea.Cmd {
	switch w.section {
	case secProvider:
		preset := presetForLabel(w.presetLabel)
		if preset.adapter != w.loaded.Provider.Default || presetForConfig(w.loaded.Provider).label != preset.label {
			// The provider changed, so the model list this dialog is holding
			// belongs to the wrong backend. Blank the model rather than carry a
			// name that cannot resolve, and re-probe when the new backend is one
			// we can probe.
			if w.modelName != "" && !w.modelKnownToPreset(preset) {
				w.modelName = ""
				w.smallModelName = ""
			}
		}
		w.adoptPreset(preset)
		if w.discovery {
			w.phase = wPhaseDiscovery
			return tea.Batch(w.sp.Tick, w.discoverCmd())
		}
		w.local, w.rec = nil, modelpick.Selection{}
		if w.modelName == "" && len(w.curated) > 0 {
			w.modelName = w.curated[0]
		}
	case secModels:
		if w.modelName == applyRecommendedValue {
			w.modelName, w.smallModelName = w.rec.Main, w.rec.Small
			w.thinkStr = "disabled"
			if w.rec.Think {
				w.thinkStr = "enabled"
			}
			w.notice = "Applied the recommended pair and set think accordingly."
		}
		if w.smallModelName == w.modelName {
			// Pointing background calls at the primary model is the same as
			// having no small model, and saying so keeps the guard's cost note
			// honest.
			w.smallModelName = ""
		}
	}
	return w.enterMenu()
}

// modelKnownToPreset reports whether the currently-chosen model plausibly
// belongs to the newly-chosen preset, so switching provider does not silently
// discard a name the operator typed for that same backend.
func (w *wizardModel) modelKnownToPreset(p wPreset) bool {
	if key, ok := strings.CutPrefix(p.modelSource, "curated:"); ok {
		for _, name := range wCurated[key] {
			if name == w.modelName {
				return true
			}
		}
		return false
	}
	if p.modelSource == "input" {
		return true
	}
	for _, m := range w.local {
		if m.Name == w.modelName {
			return true
		}
	}
	return false
}

func (w *wizardModel) discoverCmd() tea.Cmd {
	baseURL, budget := w.baseURL, atofOrZero(w.vramBudgetStr)
	return func() tea.Msg {
		models, rec := probeLocalModels(baseURL, budget)
		return modelsDiscoveredMsg{models: models, rec: rec}
	}
}

func (w *wizardModel) updateDiscovery(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		w.sp, cmd = w.sp.Update(msg)
		return cmd
	case modelsDiscoveredMsg:
		w.local, w.rec = msg.models, msg.rec
		if w.modelName == "" && w.rec.Main != "" {
			w.modelName = w.rec.Main
			w.smallModelName = w.rec.Small
			w.notice = "Discovered " + strconv.Itoa(len(w.local)) + " local models; preselected the recommended pair."
		}
		return w.enterMenu()
	}
	return nil
}

func (w *wizardModel) updateSaving(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		w.sp, cmd = w.sp.Update(msg)
		return cmd
	case wizardSavedMsg:
		w.fitNote = msg.fitNote
		if msg.err != nil {
			w.saveErr = msg.err.Error()
			w.done = true
			return nil
		}
		w.saved = true
		w.done = true
	}
	return nil
}

// ─── Ripgrep ──────────────────────────────────────────────────────────────────

func (w *wizardModel) buildRipgrepForm() *huh.Form {
	_, hasBrew := exec.LookPath("brew")
	affirmative := "Install (brew)"
	negative := "Skip"
	desc := "Ripgrep speeds up file search significantly."
	if hasBrew != nil { // brew not found
		desc += "\n\nTo install manually: sudo apt-get install ripgrep\nor visit https://github.com/BurntSushi/ripgrep"
		affirmative = "OK"
		negative = ""
	}
	w.confirmRipgrep = false
	confirm := huh.NewConfirm().
		Title("Ripgrep not found").
		Description(desc).
		Affirmative(affirmative).
		Value(&w.confirmRipgrep)
	if negative != "" {
		confirm = confirm.Negative(negative)
	}
	return huh.NewForm(
		huh.NewGroup(confirm),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// updateRipgrepFromSection drives the install confirm when it was reached from
// the Host tools menu row. Unlike the pre-P82 flow — where the prompt fired
// unconditionally after every save, whether the operator was there to change a
// model or not — declining here just returns to the menu.
func (w *wizardModel) updateRipgrepFromSection(msg tea.Msg) tea.Cmd {
	m, cmd := w.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		w.form = f
	}
	switch w.form.State {
	case huh.StateAborted:
		return w.enterMenu()
	case huh.StateCompleted:
		if _, hasBrew := exec.LookPath("brew"); hasBrew != nil || !w.confirmRipgrep {
			if hasBrew != nil {
				w.notice = "Install ripgrep with your package manager, then restart Aegis."
			}
			return w.enterMenu()
		}
		w.phase = wPhaseRipgrepInstalling
		return tea.Batch(w.sp.Tick, w.installRipgrepCmd())
	}
	return cmd
}

func (w *wizardModel) updateRipgrep(msg tea.Msg) tea.Cmd { return w.updateRipgrepFromSection(msg) }

func (w *wizardModel) updateRipgrepInstalling(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		w.sp, cmd = w.sp.Update(msg)
		return cmd
	case ripgrepInstalledMsg:
		if msg.err != nil {
			w.notice = "Ripgrep install failed: " + msg.err.Error()
		} else {
			w.notice = "Ripgrep installed. Restart Aegis to enable fast search."
		}
		return w.enterMenu()
	}
	return nil
}

func (w *wizardModel) installRipgrepCmd() tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			brew, err := exec.LookPath("brew")
			if err != nil {
				return ripgrepInstalledMsg{err: fmt.Errorf("brew not found")}
			}
			cmd = exec.Command(brew, "install", "ripgrep")
		default:
			return ripgrepInstalledMsg{err: fmt.Errorf("install ripgrep with your package manager (e.g. sudo apt-get install ripgrep)")}
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return ripgrepInstalledMsg{err: fmt.Errorf("%v\n%s", err, out)}
		}
		return ripgrepInstalledMsg{}
	}
}

// ─── Save ─────────────────────────────────────────────────────────────────────

// providerPatch renders the working copy as the provider: block to write.
func (w *wizardModel) providerPatch() config.ProviderPatch {
	preset := presetForLabel(w.presetLabel)
	mt := atoiOrZero(w.maxTokensStr)
	if mt <= 0 {
		mt = preset.defaultMax
	}
	var think *bool
	switch w.thinkStr {
	case "enabled":
		b := true
		think = &b
	case "disabled":
		b := false
		think = &b
	}
	return config.ProviderPatch{
		Adapter:       preset.adapter,
		BaseURL:       w.baseURL,
		Model:         w.modelName,
		SmallModel:    w.smallModelName,
		MaxTokens:     mt,
		MaxRetries:    atoiOrZero(w.maxRetriesStr),
		Think:         think,
		ContextWindow: atoiOrZero(w.contextWindowStr),
		VRAMBudgetGB:  atofOrZero(w.vramBudgetStr),
		// A stated budget is also the permission to keep sizing against it
		// (P72.1). The fit below can only run when the model is already loaded,
		// which at first setup it usually is not — so without this the operator
		// answers "14.5 GiB", gets the training-max recommendation written
		// anyway, and nothing ever revisits it.
		AutofitContext: w.autofitContext && atofOrZero(w.vramBudgetStr) > 0 && preset.adapter == "ollama",
	}
}

// costPatch carries every cost: key through, changing only the two this dialog
// edits. patchCost splices in a freshly built block, so a key absent from the
// struct is deleted from the operator's file.
func (w *wizardModel) costPatch() config.CostPatch {
	c := w.loaded.Cost
	return config.CostPatch{
		BudgetUSD:                atofOrZero(w.budgetUSDStr),
		MaxTokensPerRun:          c.MaxTokensPerRun,
		MaxGeneratedTokensPerRun: c.MaxGeneratedTokensPerRun,
		MaxWallClockPerRunSec:    c.MaxWallClockPerRunSec,
		MaxTurnStallSec:          atoiOrZero(w.turnStallStr),
		SessionCapUSD:            c.SessionCapUSD,
		DailyCapUSD:              c.DailyCapUSD,
		SessionTokenCap:          c.SessionTokenCap,
		DailyTokenCap:            c.DailyTokenCap,
		AlertThreshold:           c.AlertThreshold,
	}
}

// guardPatch does the same for output_guard:, carrying the rubric and pinned
// verdict model this dialog never shows.
func (w *wizardModel) guardPatch() config.OutputGuardPatch {
	return config.OutputGuardPatch{
		Enabled:    w.guardEnabled,
		Mode:       w.guardMode,
		MaxRetries: atoiOrZero(w.guardRetriesStr),
		Rubric:     w.loaded.OutputGuard.Rubric,
		Model:      w.loaded.OutputGuard.Model,
	}
}

func (w *wizardModel) saveCmd() tea.Cmd {
	p := w.providerPatch()
	cost := w.costPatch()
	guard := w.guardPatch()
	// Compare against what was loaded so an untouched section is not rewritten
	// — a splice reflows comments, and an operator who came here to change a
	// model should not find their hand-annotated cost block reformatted.
	costChanged := cost != w.costPatchFromLoaded()
	guardChanged := guard != w.guardPatchFromLoaded()

	return func() tea.Msg {
		var fitNote string
		// For Ollama, emit an explicit context_window sized from the model's
		// training-context max so a skill-driven run's large prompt isn't
		// truncated at Ollama's small Modelfile default (P35.3). Detection is a
		// best-effort network call; if the model isn't pulled yet it falls back
		// to the baseline recommendation (RecommendContextWindow(0)).
		if p.Adapter == "ollama" && p.ContextWindow == 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			base := ollamainfo.NativeBase(p.BaseURL)
			modelMax := 0
			if res, ok := ollamainfo.Detect(ctx, base, p.Model); ok {
				modelMax = res.ModelMax
			}
			p.ContextWindow = ollamainfo.RecommendContextWindow(modelMax)
			// With a budget stated, the training max stops being the question
			// (P69.6/P69.5). RecommendContextWindow on a 262144-context model
			// writes 131072, which is 16.5 GiB of KV cache before any weights —
			// a number no 16 GB card can serve, written by the very command
			// meant to set the machine up. Fit answers what the hardware holds.
			if p.VRAMBudgetGB > 0 {
				if win, note := fitWindowForBudget(ctx, base, p.Model, p.VRAMBudgetGB); win > 0 {
					p.ContextWindow = win
				} else {
					fitNote = note
				}
			}
			cancel()
		}
		if err := config.PatchGlobalProvider(p); err != nil {
			return wizardSavedMsg{err: err, fitNote: fitNote}
		}
		if costChanged {
			if err := config.PatchGlobalCost(cost); err != nil {
				return wizardSavedMsg{err: err, fitNote: fitNote}
			}
		}
		if guardChanged {
			if err := config.PatchGlobalOutputGuard(guard); err != nil {
				return wizardSavedMsg{err: err, fitNote: fitNote}
			}
		}
		return wizardSavedMsg{fitNote: fitNote}
	}
}

func (w *wizardModel) costPatchFromLoaded() config.CostPatch {
	c := w.loaded.Cost
	return config.CostPatch{
		BudgetUSD:                c.BudgetUSD,
		MaxTokensPerRun:          c.MaxTokensPerRun,
		MaxGeneratedTokensPerRun: c.MaxGeneratedTokensPerRun,
		MaxWallClockPerRunSec:    c.MaxWallClockPerRunSec,
		MaxTurnStallSec:          c.MaxTurnStallSec,
		SessionCapUSD:            c.SessionCapUSD,
		DailyCapUSD:              c.DailyCapUSD,
		SessionTokenCap:          c.SessionTokenCap,
		DailyTokenCap:            c.DailyTokenCap,
		AlertThreshold:           c.AlertThreshold,
	}
}

func (w *wizardModel) guardPatchFromLoaded() config.OutputGuardPatch {
	g := w.loaded.OutputGuard
	mode := g.Mode
	if mode == "" {
		mode = "llm"
	}
	return config.OutputGuardPatch{
		Enabled:    g.Enabled,
		Mode:       mode,
		MaxRetries: g.MaxRetries,
		Rubric:     g.Rubric,
		Model:      g.Model,
	}
}

// fitWindowForBudget solves for the context window that fits budgetGB alongside
// the model's measured resident weights, or returns 0 and a line explaining why
// it could not.
//
// The refusal case is the common one at first setup: a freshly pulled model has
// never been loaded, so /api/ps reports nothing and its resident weights cannot
// be measured. The tempting substitute — /api/tags' on-disk size — overstates a
// multimodal model by the size of a vision projector that is never resident
// (2.57 GiB on qwen35-9b), which is more than a fitted window's whole margin. So
// the budget is still written, the pre-P69.6 recommendation still stands as the
// window, and the user is told the one command that finishes the job.
func fitWindowForBudget(ctx context.Context, base, model string, budgetGB float64) (int, string) {
	g, ok := ollamainfo.Geometry(ctx, base, model)
	if !ok || !g.Complete() {
		return 0, "Budget saved, but " + model + " did not report the KV geometry needed to size a window."
	}
	f, loaded := ollamainfo.Loaded(ctx, base, model)
	if !loaded {
		return 0, "Budget saved. " + model + " is not loaded yet, so its window could not be fitted —\n" +
			"run a turn, then `aegis models --fit --write`."
	}
	weights, ok := ollamainfo.WeightsBytes(f, g, ollamainfo.KVTypeF16)
	if !ok {
		return 0, "Budget saved, but " + model + "'s resident weights could not be measured;\n" +
			"run `aegis models --fit --write` once it has served a turn."
	}
	win, ok := ollamainfo.Fit(g, ollamainfo.BudgetBytes(budgetGB), weights, ollamainfo.KVTypeF16)
	if !ok {
		return 0, "Budget saved, but no viable window fits " + model + " in it. Try a larger budget\n" +
			"or a smaller model; `aegis models --fit` shows the curve."
	}
	return win, ""
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (w *wizardModel) view() string {
	header := renderBrandMark() + " " +
		w.th.statusDim.Render("Configuration") + "\n\n"

	var body string
	switch {
	case w.saveErr != "":
		body = w.th.errLine.Render("Failed to save:") + "\n" +
			w.th.statusDim.Render("  "+w.saveErr)
	case w.phase == wPhaseLoading:
		body = w.sp.View() + " Reading config and probing for local models…"
	case w.phase == wPhaseDiscovery:
		body = w.sp.View() + " Discovering models…"
	case w.phase == wPhaseSaving:
		body = w.sp.View() + " Saving configuration…"
	case w.phase == wPhaseRipgrepInstalling:
		body = w.sp.View() + " Installing ripgrep…"
	default:
		body = w.form.View()
	}

	return fixedPanelFrame(header+body, wizardPanelW)
}

// ─── Theme ────────────────────────────────────────────────────────────────────

func aegisHuhTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		return aegisHuhStyles(huh.ThemeCharm(isDark))
	})
}

func aegisHuhStyles(t *huh.Styles) *huh.Styles {
	t.Focused.Title = lipgloss.NewStyle().Foreground(colAssistFg).Bold(true)
	t.Focused.Description = lipgloss.NewStyle().Foreground(colTextMuted).Italic(true)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colAccent).SetString("▶ ")
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	t.Focused.Option = lipgloss.NewStyle().Foreground(colTextDim)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colTextDim)
	t.Focused.FocusedButton = lipgloss.NewStyle().
		Background(colAccent).Foreground(colBrandFg).Bold(true).Padding(0, 1)
	t.Focused.BlurredButton = lipgloss.NewStyle().
		Background(colSurface).Foreground(colTextMuted).Padding(0, 1)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colAccent)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(colAccent)
	t.Blurred.Title = lipgloss.NewStyle().Foreground(colTextMuted)
	t.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.SelectedOption = lipgloss.NewStyle().Foreground(colTextDim)
	return t
}
