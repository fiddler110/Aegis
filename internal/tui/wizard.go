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
	"github.com/fiddler110/aegis/internal/discover"
	"github.com/fiddler110/aegis/internal/ollamainfo"
)

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

// ─── Internal messages ────────────────────────────────────────────────────────

type modelsDiscoveredMsg struct{ models []discover.Model }

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
	wPhaseProvider          wizardPhase = iota // huh form: provider select
	wPhaseDiscovery                            // async model discovery
	wPhaseConfig                               // huh form: settings
	wPhaseSaving                               // async config save
	wPhaseRipgrep                              // huh confirm: install ripgrep?
	wPhaseRipgrepInstalling                    // async ripgrep install (brew only)
)

// ─── Model ────────────────────────────────────────────────────────────────────

const wizardPanelW = 64

type wizardModel struct {
	phase wizardPhase
	form  *huh.Form
	sp    spinner.Model

	// Provider form value
	presetLabel string

	// Config form values (bound to huh fields)
	baseURL      string
	modelName    string
	maxTokensStr string
	thinkStr     string
	// vramBudgetStr is the P69.6 memory budget, in GiB, and only ever asked for
	// on a local Ollama backend. Blank is a first-class answer: it means "do not
	// plan resident sets", and the config written is byte-identical to the one
	// this wizard produced before the question existed.
	vramBudgetStr string
	confirmSave   bool
	// fitNote explains, after saving, why a stated budget did not size the
	// window — normally because the model has never been loaded, so its resident
	// weights cannot be measured and the honest answer is to say so rather than
	// size against /api/tags' on-disk figure.
	fitNote string

	// Discovered / curated model options
	modelOpts []huh.Option[string]

	done           bool
	saved          bool
	saveErr        string
	confirmRipgrep bool
	ripgrepMsg     string // shown after install attempt (info or error)

	width  int
	height int
	th     theme
}

func newWizard(width, height int, th theme) *wizardModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	w := &wizardModel{
		width:  width,
		height: height,
		th:     th,
		sp:     sp,
	}
	w.form = w.buildProviderForm()
	return w
}

func (w *wizardModel) init() tea.Cmd {
	return w.form.Init()
}

// ─── Form builders ────────────────────────────────────────────────────────────

func (w *wizardModel) buildProviderForm() *huh.Form {
	opts := make([]huh.Option[string], len(wPresets))
	for i, p := range wPresets {
		opts[i] = huh.NewOption(p.label, p.label)
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("AI Provider").
				Description("Choose your provider. Change any time with /config.").
				Options(opts...).
				Value(&w.presetLabel).
				Height(len(wPresets) + 2),
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

func (w *wizardModel) buildConfigForm() *huh.Form {
	thinkOpts := []huh.Option[string]{
		huh.NewOption("Auto (provider default)", "auto"),
		huh.NewOption("Enabled", "enabled"),
		huh.NewOption("Disabled", "disabled"),
	}

	// Model field: Select from list if we have options, otherwise free text.
	var modelField huh.Field
	if len(w.modelOpts) > 0 {
		h := len(w.modelOpts) + 2
		if h > 10 {
			h = 10
		}
		modelField = huh.NewSelect[string]().
			Title("Model").
			Options(w.modelOpts...).
			Value(&w.modelName).
			Height(h)
	} else {
		modelField = huh.NewInput().
			Title("Model").
			Placeholder("e.g. gpt-4o, llama3:latest").
			Value(&w.modelName)
	}

	// The VRAM budget is an Ollama-only question. Every other backend either
	// runs somewhere Aegis does not manage residency for, or has nothing
	// co-resident to plan against.
	var extra []huh.Field
	if w.adapterName() == "ollama" {
		extra = append(extra, huh.NewInput().
			Title("VRAM budget (GiB)").
			Description("Memory Ollama may use, across all models at once. Blank to skip.").
			Placeholder("e.g. 14.5 on a 16 GB card").
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				v, err := strconv.ParseFloat(s, 64)
				if err != nil || v <= 0 {
					return fmt.Errorf("enter a positive number of GiB, or leave blank")
				}
				return nil
			}).
			Value(&w.vramBudgetStr))
	}

	settings := []huh.Field{
		huh.NewInput().
			Title("Max tokens per response").
			Placeholder("e.g. 8192").
			Validate(func(s string) error {
				n, err := strconv.Atoi(strings.TrimSpace(s))
				if err != nil || n <= 0 {
					return fmt.Errorf("enter a positive integer")
				}
				return nil
			}).
			Value(&w.maxTokensStr),
	}
	settings = append(settings, extra...)
	settings = append(settings,
		huh.NewSelect[string]().
			Title("Extended thinking").
			Description("For reasoning models (Claude 3.7+, o1, etc.).").
			Options(thinkOpts...).
			Value(&w.thinkStr).
			Height(5),
		huh.NewConfirm().
			Title("Save to config.yaml?").
			Affirmative("Save").
			Negative("Cancel").
			Value(&w.confirmSave),
	)

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("API base URL").
				Description("Leave empty to use the provider default.").
				Placeholder("https://...").
				Value(&w.baseURL),
			modelField,
		),
		huh.NewGroup(settings...),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
}

// adapterName resolves the adapter behind the selected provider preset, for the
// handful of questions that only make sense for one backend.
func (w *wizardModel) adapterName() string {
	for i := range wPresets {
		if wPresets[i].label == w.presetLabel {
			return wPresets[i].adapter
		}
	}
	return "openai"
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (w *wizardModel) update(msg tea.Msg) tea.Cmd {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		w.done = true
		return nil
	}

	switch w.phase {
	case wPhaseProvider:
		return w.updateProvider(msg)
	case wPhaseDiscovery:
		return w.updateDiscovery(msg)
	case wPhaseConfig:
		return w.updateConfig(msg)
	case wPhaseSaving:
		return w.updateSaving(msg)
	case wPhaseRipgrep:
		return w.updateRipgrep(msg)
	case wPhaseRipgrepInstalling:
		return w.updateRipgrepInstalling(msg)
	}
	return nil
}

func (w *wizardModel) updateProvider(msg tea.Msg) tea.Cmd {
	m, cmd := w.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		w.form = f
	}
	switch w.form.State {
	case huh.StateAborted:
		w.done = true
	case huh.StateCompleted:
		return w.onProviderSelected()
	}
	return cmd
}

func (w *wizardModel) onProviderSelected() tea.Cmd {
	var preset *wPreset
	for i := range wPresets {
		if wPresets[i].label == w.presetLabel {
			preset = &wPresets[i]
			break
		}
	}
	if preset == nil {
		preset = &wPresets[0]
	}

	if w.baseURL == "" {
		w.baseURL = preset.defaultURL
	}
	if w.maxTokensStr == "" {
		w.maxTokensStr = strconv.Itoa(preset.defaultMax)
	}
	if w.thinkStr == "" {
		w.thinkStr = "auto"
	}

	src := preset.modelSource
	switch {
	case strings.HasPrefix(src, "discover:"):
		provider := strings.TrimPrefix(src, "discover:")
		w.phase = wPhaseDiscovery
		return tea.Batch(w.sp.Tick, w.discoverCmd(provider))

	case strings.HasPrefix(src, "curated:"):
		key := strings.TrimPrefix(src, "curated:")
		for _, name := range wCurated[key] {
			w.modelOpts = append(w.modelOpts, huh.NewOption(name, name))
		}
		if len(wCurated[key]) > 0 && w.modelName == "" {
			w.modelName = wCurated[key][0]
		}
		return w.enterConfig()

	default: // "input" — no list, use text field
		return w.enterConfig()
	}
}

func (w *wizardModel) enterConfig() tea.Cmd {
	w.phase = wPhaseConfig
	w.form = w.buildConfigForm()
	return w.form.Init()
}

func (w *wizardModel) discoverCmd(provider string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		var sources []discover.Source
		for _, s := range discover.DefaultSources() {
			if s.Name == provider {
				sources = append(sources, s)
			}
		}
		if len(sources) == 0 {
			sources = discover.DefaultSources()
		}
		return modelsDiscoveredMsg{models: discover.Discover(ctx, sources, 3*time.Second)}
	}
}

func (w *wizardModel) updateDiscovery(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		w.sp, cmd = w.sp.Update(msg)
		return cmd
	case modelsDiscoveredMsg:
		for _, m := range msg.models {
			w.modelOpts = append(w.modelOpts, huh.NewOption(m.Name, m.Name))
		}
		if len(msg.models) > 0 && w.modelName == "" {
			w.modelName = msg.models[0].Name
		}
		return w.enterConfig()
	}
	return nil
}

func (w *wizardModel) updateConfig(msg tea.Msg) tea.Cmd {
	m, cmd := w.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		w.form = f
	}
	switch w.form.State {
	case huh.StateAborted:
		// Go back to provider selection
		w.presetLabel = ""
		w.modelOpts = nil
		w.phase = wPhaseProvider
		w.form = w.buildProviderForm()
		return w.form.Init()
	case huh.StateCompleted:
		if !w.confirmSave {
			w.done = true
			return nil
		}
		w.phase = wPhaseSaving
		return tea.Batch(w.sp.Tick, w.saveCmd())
	}
	return cmd
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
		// Offer ripgrep install only if rg is not already available.
		if _, err := exec.LookPath("rg"); err != nil {
			w.phase = wPhaseRipgrep
			w.form = w.buildRipgrepForm()
			return w.form.Init()
		}
		w.saved = true
		w.done = true
	}
	return nil
}

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

func (w *wizardModel) updateRipgrep(msg tea.Msg) tea.Cmd {
	m, cmd := w.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		w.form = f
	}
	switch w.form.State {
	case huh.StateAborted, huh.StateCompleted:
		_, hasBrew := exec.LookPath("brew")
		if hasBrew != nil || !w.confirmRipgrep {
			// No brew or user skipped — show instructions and finish.
			if hasBrew != nil {
				w.ripgrepMsg = "Install ripgrep manually then restart Aegis."
			}
			w.saved = true
			w.done = true
			return nil
		}
		w.phase = wPhaseRipgrepInstalling
		return tea.Batch(w.sp.Tick, w.installRipgrepCmd())
	}
	return cmd
}

func (w *wizardModel) updateRipgrepInstalling(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		w.sp, cmd = w.sp.Update(msg)
		return cmd
	case ripgrepInstalledMsg:
		if msg.err != nil {
			w.ripgrepMsg = "Install failed: " + msg.err.Error()
		} else {
			w.ripgrepMsg = "Ripgrep installed. Restart Aegis to enable fast search."
		}
		w.saved = true
		w.done = true
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

func (w *wizardModel) saveCmd() tea.Cmd {
	var preset *wPreset
	for i := range wPresets {
		if wPresets[i].label == w.presetLabel {
			preset = &wPresets[i]
			break
		}
	}
	adapter := "openai"
	if preset != nil {
		adapter = preset.adapter
	}

	mt, _ := strconv.Atoi(strings.TrimSpace(w.maxTokensStr))
	if mt <= 0 {
		mt = 8192
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

	budgetGB, _ := strconv.ParseFloat(strings.TrimSpace(w.vramBudgetStr), 64)
	if budgetGB < 0 {
		budgetGB = 0
	}

	p := config.ProviderPatch{
		Adapter:      adapter,
		BaseURL:      w.baseURL,
		Model:        w.modelName,
		MaxTokens:    mt,
		MaxRetries:   4,
		Think:        think,
		VRAMBudgetGB: budgetGB,
	}
	return func() tea.Msg {
		var fitNote string
		// For Ollama, emit an explicit context_window sized from the model's
		// training-context max so a skill-driven run's large prompt isn't
		// truncated at Ollama's small Modelfile default (P35.3). Detection is a
		// best-effort network call; if the model isn't pulled yet it falls back
		// to the baseline recommendation (RecommendContextWindow(0)).
		if adapter == "ollama" {
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
			if budgetGB > 0 {
				if win, note := fitWindowForBudget(ctx, base, p.Model, budgetGB); win > 0 {
					p.ContextWindow = win
				} else {
					fitNote = note
				}
			}
			cancel()
		}
		return wizardSavedMsg{err: config.PatchGlobalProvider(p), fitNote: fitNote}
	}
}

// fitWindowForBudget solves for the context window that fits budgetGB alongside
// the model's measured resident weights, or returns 0 and a line explaining why
// it could not.
//
// The refusal case is the common one at first-init: a freshly pulled model has
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
		w.th.statusDim.Render("Configuration Wizard") + "\n\n"

	var body string
	switch {
	case w.saveErr != "":
		body = w.th.errLine.Render("Failed to save:") + "\n" +
			w.th.statusDim.Render("  "+w.saveErr)
	case w.ripgrepMsg != "":
		body = w.th.statusDim.Render(w.ripgrepMsg)
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
