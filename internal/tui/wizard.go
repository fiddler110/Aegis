package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/discover"
)

// ─── Steps ────────────────────────────────────────────────────────────────────

type wStep int

const (
	wProvider wStep = iota
	wBaseURL
	wModel
	wMaxTokens
	wThink
	wConfirm
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

// ─── Model ───────────────────────────────────────────────────────────────────

const (
	wizardPanelW   = 62
	wizardMaxList  = 9
)

type wizardModel struct {
	step   wStep
	preset *wPreset
	cursor int

	// text input (base URL, max tokens, manual model name)
	input textinput.Model

	// model step state
	modelList      []string
	modelScroll    int
	modelInputMode bool
	loadingModels  bool

	// collected values
	adapter   string
	baseURL   string
	model     string
	maxTokens string
	think     *bool

	// end state
	done    bool
	saved   bool
	saveErr string

	width  int
	height int
	th     theme
}

func newWizard(width, height int, th theme) *wizardModel {
	ti := textinput.New()
	ti.CharLimit = 256
	return &wizardModel{
		width:  width,
		height: height,
		th:     th,
		input:  ti,
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (w *wizardModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case modelsDiscoveredMsg:
		w.loadingModels = false
		for _, m := range msg.models {
			w.modelList = append(w.modelList, m.Name)
		}
		if len(w.modelList) == 0 {
			// Nothing found — drop straight to manual text input
			w.modelInputMode = true
			w.input = newTextInput(w.model, 256, "model name")
			return textinput.Blink
		}
		w.cursor = 0
		w.modelScroll = 0
		return nil

	case wizardSavedMsg:
		if msg.err != nil {
			w.saveErr = msg.err.Error()
		} else {
			w.saved = true
			w.done = true
		}
		return nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			w.done = true
			return nil
		}
		return w.handleKey(msg)
	}

	// Forward all other messages (blink ticks etc.) to the active text input.
	if w.isTextStep() {
		var cmd tea.Cmd
		w.input, cmd = w.input.Update(msg)
		return cmd
	}
	return nil
}

func (w *wizardModel) isTextStep() bool {
	return w.step == wBaseURL || w.step == wMaxTokens ||
		(w.step == wModel && w.modelInputMode)
}

func (w *wizardModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	if w.isTextStep() {
		switch msg.Type {
		case tea.KeyEnter:
			return w.confirmText()
		case tea.KeyEsc:
			return w.goBack()
		default:
			var cmd tea.Cmd
			w.input, cmd = w.input.Update(msg)
			return cmd
		}
	}

	switch w.step {
	case wProvider:
		return w.keyProvider(msg)
	case wModel:
		return w.keyModel(msg)
	case wThink:
		return w.keyThink(msg)
	case wConfirm:
		return w.keyConfirm(msg)
	}
	return nil
}

// ─── Provider step ────────────────────────────────────────────────────────────

func (w *wizardModel) keyProvider(msg tea.KeyMsg) tea.Cmd {
	n := len(wPresets)
	switch msg.Type {
	case tea.KeyUp:
		if w.cursor > 0 {
			w.cursor--
		}
	case tea.KeyDown:
		if w.cursor < n-1 {
			w.cursor++
		}
	case tea.KeyEnter:
		return w.selectProvider(w.cursor)
	case tea.KeyEsc:
		w.done = true
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			if idx := int(msg.Runes[0] - '1'); idx >= 0 && idx < n {
				w.cursor = idx
				return w.selectProvider(idx)
			}
		}
	}
	return nil
}

func (w *wizardModel) selectProvider(idx int) tea.Cmd {
	w.preset = &wPresets[idx]
	w.adapter = w.preset.adapter
	if w.baseURL == "" {
		w.baseURL = w.preset.defaultURL
	}
	if w.maxTokens == "" {
		w.maxTokens = strconv.Itoa(w.preset.defaultMax)
	}
	w.step = wBaseURL
	w.input = newTextInput(w.baseURL, 256, "leave empty for provider default")
	return textinput.Blink
}

// ─── Text step (base URL / max tokens) ───────────────────────────────────────

func (w *wizardModel) confirmText() tea.Cmd {
	switch w.step {
	case wBaseURL:
		w.baseURL = strings.TrimSpace(w.input.Value())
		return w.enterModelStep()

	case wMaxTokens:
		v := strings.TrimSpace(w.input.Value())
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			w.maxTokens = v
		} else if w.preset != nil {
			w.maxTokens = strconv.Itoa(w.preset.defaultMax)
		}
		w.step = wThink
		w.cursor = 0
		return nil

	case wModel: // manual-entry sub-mode
		v := strings.TrimSpace(w.input.Value())
		if v == "" {
			return nil
		}
		w.model = v
		return w.enterMaxTokens()
	}
	return nil
}

// ─── Model step ───────────────────────────────────────────────────────────────

func (w *wizardModel) enterModelStep() tea.Cmd {
	w.step = wModel
	w.modelList = nil
	w.modelInputMode = false
	w.cursor = 0
	w.modelScroll = 0

	src := w.preset.modelSource
	switch {
	case strings.HasPrefix(src, "discover:"):
		w.loadingModels = true
		provider := strings.TrimPrefix(src, "discover:")
		return w.discoverCmd(provider)
	case strings.HasPrefix(src, "curated:"):
		key := strings.TrimPrefix(src, "curated:")
		w.modelList = wCurated[key]
	case src == "input":
		w.modelInputMode = true
		w.input = newTextInput(w.model, 256, "model name")
		return textinput.Blink
	}
	return nil
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
		models := discover.Discover(ctx, sources, 3*time.Second)
		return modelsDiscoveredMsg{models: models}
	}
}

func (w *wizardModel) keyModel(msg tea.KeyMsg) tea.Cmd {
	// List includes discovered/curated models plus "(enter manually)" at the end.
	total := len(w.modelList) + 1
	manualIdx := total - 1

	switch msg.Type {
	case tea.KeyUp:
		if w.cursor > 0 {
			w.cursor--
			if w.cursor < w.modelScroll {
				w.modelScroll--
			}
		}
	case tea.KeyDown:
		if w.cursor < total-1 {
			w.cursor++
			if w.cursor >= w.modelScroll+wizardMaxList {
				w.modelScroll++
			}
		}
	case tea.KeyEnter:
		if w.cursor == manualIdx {
			w.modelInputMode = true
			w.input = newTextInput(w.model, 256, "model name")
			return textinput.Blink
		}
		w.model = w.modelList[w.cursor]
		return w.enterMaxTokens()
	case tea.KeyEsc:
		return w.goBack()
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			if idx := int(msg.Runes[0] - '1'); idx >= 0 && idx < total {
				if idx == manualIdx {
					w.modelInputMode = true
					w.input = newTextInput(w.model, 256, "model name")
					return textinput.Blink
				}
				w.model = w.modelList[idx]
				return w.enterMaxTokens()
			}
		}
	}
	return nil
}

func (w *wizardModel) enterMaxTokens() tea.Cmd {
	w.step = wMaxTokens
	w.input = newTextInput(w.maxTokens, 16, "e.g. 8192")
	return textinput.Blink
}

// ─── Think step ───────────────────────────────────────────────────────────────

func (w *wizardModel) keyThink(msg tea.KeyMsg) tea.Cmd {
	const n = 3
	switch msg.Type {
	case tea.KeyUp:
		if w.cursor > 0 {
			w.cursor--
		}
	case tea.KeyDown:
		if w.cursor < n-1 {
			w.cursor++
		}
	case tea.KeyEnter:
		return w.selectThink(w.cursor)
	case tea.KeyEsc:
		return w.goBack()
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			if idx := int(msg.Runes[0] - '1'); idx >= 0 && idx < n {
				w.cursor = idx
				return w.selectThink(idx)
			}
		}
	}
	return nil
}

func (w *wizardModel) selectThink(idx int) tea.Cmd {
	switch idx {
	case 0:
		w.think = nil
	case 1:
		b := true
		w.think = &b
	case 2:
		b := false
		w.think = &b
	}
	w.step = wConfirm
	w.cursor = 0
	return nil
}

// ─── Confirm step ─────────────────────────────────────────────────────────────

func (w *wizardModel) keyConfirm(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		return w.saveCmd()
	case tea.KeyEsc:
		return w.goBack()
	}
	return nil
}

func (w *wizardModel) saveCmd() tea.Cmd {
	mt, _ := strconv.Atoi(w.maxTokens)
	if mt <= 0 {
		mt = 8192
	}
	p := config.ProviderPatch{
		Adapter:    w.adapter,
		BaseURL:    w.baseURL,
		Model:      w.model,
		MaxTokens:  mt,
		MaxRetries: 4,
		Think:      w.think,
	}
	return func() tea.Msg {
		return wizardSavedMsg{err: config.PatchGlobalProvider(p)}
	}
}

// ─── Navigation ───────────────────────────────────────────────────────────────

func (w *wizardModel) goBack() tea.Cmd {
	switch w.step {
	case wProvider:
		w.done = true

	case wBaseURL:
		w.step = wProvider
		w.cursor = w.presetIndex()

	case wModel:
		if w.modelInputMode && len(w.modelList) > 0 {
			w.modelInputMode = false
			w.cursor = 0
		} else {
			w.step = wBaseURL
			w.input = newTextInput(w.baseURL, 256, "leave empty for provider default")
			return textinput.Blink
		}

	case wMaxTokens:
		w.step = wModel
		w.modelInputMode = false
		w.cursor = 0

	case wThink:
		w.step = wMaxTokens
		w.input = newTextInput(w.maxTokens, 16, "e.g. 8192")
		return textinput.Blink

	case wConfirm:
		w.step = wThink
		w.cursor = 0
	}
	return nil
}

func (w *wizardModel) presetIndex() int {
	if w.preset == nil {
		return 0
	}
	for i := range wPresets {
		if wPresets[i].label == w.preset.label {
			return i
		}
	}
	return 0
}

// ─── View ────────────────────────────────────────────────────────────────────

func (w *wizardModel) view() string {
	inner := w.renderStep()

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Width(wizardPanelW).
		Render(inner)

	return lipgloss.Place(w.width, w.height, lipgloss.Center, lipgloss.Center, panel)
}

func (w *wizardModel) renderStep() string {
	title := w.th.titleBrand.Render("⬡ AEGIS  ·  Configuration Wizard")
	step := w.th.statusDim.Render(w.stepLabel())
	header := title + "\n" + step + "\n"

	switch {
	case w.saveErr != "":
		return header + w.renderError()
	case w.step == wProvider:
		return header + w.renderProvider()
	case w.step == wBaseURL:
		return header + w.renderBaseURL()
	case w.step == wModel:
		return header + w.renderModel()
	case w.step == wMaxTokens:
		return header + w.renderMaxTokens()
	case w.step == wThink:
		return header + w.renderThink()
	case w.step == wConfirm:
		return header + w.renderConfirm()
	}
	return header
}

func (w *wizardModel) stepLabel() string {
	switch w.step {
	case wProvider:
		return "Step 1 / 5  ·  Provider"
	case wBaseURL:
		return "Step 2 / 5  ·  Base URL"
	case wModel:
		return "Step 3 / 5  ·  Model"
	case wMaxTokens:
		return "Step 4 / 5  ·  Max tokens"
	case wThink:
		return "Step 5 / 5  ·  Thinking mode"
	case wConfirm:
		return "Review & Save"
	}
	return ""
}

// ─── Step renderers ──────────────────────────────────────────────────────────

func (w *wizardModel) renderProvider() string {
	var b strings.Builder
	b.WriteString("\nSelect your AI provider:\n\n")
	for i, p := range wPresets {
		label := fmt.Sprintf("[%d] %s", i+1, p.label)
		if i == w.cursor {
			b.WriteString(w.th.assistant.Render("▶ "+label) + "\n")
		} else {
			b.WriteString(w.th.statusDim.Render("  "+label) + "\n")
		}
	}
	b.WriteString(w.hint("↑↓ or 1–7 navigate", "Enter select", "Esc cancel"))
	return b.String()
}

func (w *wizardModel) renderBaseURL() string {
	var b strings.Builder
	b.WriteString("\nAPI base URL:\n\n")
	b.WriteString(w.input.View() + "\n")
	b.WriteString(w.hint("Enter confirm", "Esc back"))
	return b.String()
}

func (w *wizardModel) renderModel() string {
	var b strings.Builder

	if w.loadingModels {
		b.WriteString("\nDiscovering models…\n")
		b.WriteString(w.hint("Esc back"))
		return b.String()
	}

	if w.modelInputMode {
		b.WriteString("\nEnter model name:\n\n")
		b.WriteString(w.input.View() + "\n")
		b.WriteString(w.hint("Enter confirm", "Esc back"))
		return b.String()
	}

	// Label depends on source
	if w.preset != nil && strings.HasPrefix(w.preset.modelSource, "discover:") {
		provider := strings.TrimPrefix(w.preset.modelSource, "discover:")
		b.WriteString(fmt.Sprintf("\nDiscovered %s models:\n\n", provider))
	} else {
		b.WriteString("\nAvailable models:\n\n")
	}

	items := append(w.modelList, "(enter model name manually)")
	total := len(items)
	start := w.modelScroll
	end := start + wizardMaxList
	if end > total {
		end = total
	}

	if start > 0 {
		b.WriteString(w.th.statusDim.Render("  ↑ more above") + "\n")
	}
	for i := start; i < end; i++ {
		label := fmt.Sprintf("[%d] %s", i+1, items[i])
		if i == w.cursor {
			b.WriteString(w.th.assistant.Render("▶ "+label) + "\n")
		} else {
			b.WriteString(w.th.statusDim.Render("  "+label) + "\n")
		}
	}
	if end < total {
		b.WriteString(w.th.statusDim.Render("  ↓ more below") + "\n")
	}
	b.WriteString(w.hint("↑↓ navigate", "Enter select", "Esc back"))
	return b.String()
}

func (w *wizardModel) renderMaxTokens() string {
	var b strings.Builder
	b.WriteString("\nMaximum tokens per response:\n\n")
	b.WriteString(w.input.View() + "\n")
	b.WriteString(w.hint("Enter confirm", "Esc back"))
	return b.String()
}

func (w *wizardModel) renderThink() string {
	opts := []string{"Auto (provider default)", "Enabled", "Disabled"}
	var b strings.Builder
	b.WriteString("\nExtended thinking for reasoning models:\n\n")
	for i, opt := range opts {
		label := fmt.Sprintf("[%d] %s", i+1, opt)
		if i == w.cursor {
			b.WriteString(w.th.assistant.Render("▶ "+label) + "\n")
		} else {
			b.WriteString(w.th.statusDim.Render("  "+label) + "\n")
		}
	}
	b.WriteString(w.hint("↑↓ or 1–3 navigate", "Enter select", "Esc back"))
	return b.String()
}

func (w *wizardModel) renderConfirm() string {
	var b strings.Builder
	b.WriteString("\nReady to save to config.yaml:\n\n")

	providerLabel := ""
	if w.preset != nil {
		providerLabel = w.preset.label
	}

	thinkStr := "auto"
	if w.think != nil {
		if *w.think {
			thinkStr = "enabled"
		} else {
			thinkStr = "disabled"
		}
	}

	rows := [][2]string{
		{"Provider", providerLabel},
		{"Adapter", w.adapter},
	}
	if w.baseURL != "" {
		rows = append(rows, [2]string{"Base URL", w.baseURL})
	}
	rows = append(rows,
		[2]string{"Model", w.model},
		[2]string{"Max tokens", w.maxTokens},
		[2]string{"Think", thinkStr},
	)

	for _, row := range rows {
		key := w.th.sideSection.Render(fmt.Sprintf("  %-12s", row[0]))
		val := w.th.sideValue.Render(row[1])
		b.WriteString(key + " " + val + "\n")
	}

	b.WriteString("\n")
	b.WriteString(w.th.statusDim.Render("  File: "+config.GlobalConfigPath()) + "\n")
	b.WriteString(w.hint("Enter save", "Esc back", "Ctrl+C cancel"))
	return b.String()
}

func (w *wizardModel) renderError() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(w.th.errLine.Render("Failed to save configuration:") + "\n\n")
	b.WriteString(w.th.statusDim.Render("  "+w.saveErr) + "\n")
	b.WriteString(w.hint("Esc close"))
	return b.String()
}

func (w *wizardModel) hint(parts ...string) string {
	var styled []string
	for _, p := range parts {
		styled = append(styled, w.th.statusDim.Render(p))
	}
	return "\n" + strings.Join(styled, w.th.statusDim.Render("  ·  "))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newTextInput(value string, charLimit int, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.CharLimit = charLimit
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.Focus()
	return ti
}
