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

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/discover"
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
	{"Anthropic (Claude)", "anthropic", "", 16384, "curated:anthropic"},
	{"Ollama (local)", "openai", "http://localhost:11434/v1", 8192, "discover:ollama"},
	{"OpenAI", "openai", "", 16384, "curated:openai"},
	{"LM Studio (local)", "openai", "http://localhost:1234/v1", 4096, "discover:lmstudio"},
	{"Groq", "openai", "https://api.groq.com/openai/v1", 8192, "curated:groq"},
	{"OpenRouter", "openai", "https://openrouter.ai/api/v1", 16384, "curated:openrouter"},
	{"Custom", "openai", "", 8192, "input"},
}

var wCurated = map[string][]string{
	"anthropic":  {"claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5-20251001"},
	"openai":     {"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "o3", "o3-mini", "o1", "o1-mini"},
	"groq":       {"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768", "gemma2-9b-it"},
	"openrouter": {"anthropic/claude-opus-4", "openai/gpt-4o", "google/gemini-2.0-flash-001", "meta-llama/llama-3.3-70b-instruct"},
}

// ─── Internal messages ────────────────────────────────────────────────────────

type modelsDiscoveredMsg struct{ models []discover.Model }
type wizardSavedMsg struct{ err error }
type ripgrepInstalledMsg struct{ err error }

// ─── Phases ───────────────────────────────────────────────────────────────────

type wizardPhase int

const (
	wPhaseProvider           wizardPhase = iota // huh form: provider select
	wPhaseDiscovery                             // async model discovery
	wPhaseConfig                                // huh form: settings
	wPhaseSaving                                // async config save
	wPhaseRipgrep                               // huh confirm: install ripgrep?
	wPhaseRipgrepInstalling                     // async ripgrep install (brew only)
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
	confirmSave  bool

	// Discovered / curated model options
	modelOpts []huh.Option[string]

	done          bool
	saved         bool
	saveErr       string
	confirmRipgrep bool
	ripgrepMsg    string // shown after install attempt (info or error)

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

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("API base URL").
				Description("Leave empty to use the provider default.").
				Placeholder("https://...").
				Value(&w.baseURL),
			modelField,
		),
		huh.NewGroup(
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
		),
	).WithWidth(wizardPanelW - 8).WithTheme(aegisHuhTheme())
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

	p := config.ProviderPatch{
		Adapter:    adapter,
		BaseURL:    w.baseURL,
		Model:      w.modelName,
		MaxTokens:  mt,
		MaxRetries: 4,
		Think:      think,
	}
	return func() tea.Msg {
		return wizardSavedMsg{err: config.PatchGlobalProvider(p)}
	}
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

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Width(wizardPanelW).
		Render(header + body)

	return lipgloss.Place(w.width, w.height, lipgloss.Center, lipgloss.Center, panel)
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
