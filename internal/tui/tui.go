// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/commands"
)

// Config configures the TUI.
type Config struct {
	Client    *client.Client
	SessionID string
	Mode      string
	Model     string
	WorkDir   string
}

// Run starts the TUI event loop and blocks until the user quits.
func Run(cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

const (
	maxTranscriptBytes = 1 << 20

	// Sidebar geometry. sidebarInnerW is the content width passed to lipgloss
	// Width(); the rendered block is sidebarInnerW+1 wide (right border char).
	sidebarInnerW   = 21
	sidebarTotalW   = 22 // sidebarInnerW + 1 border
	sidebarMinTermW = 88 // terminal width below which sidebar collapses

	maxToolHistory = 8
)

// toolEntry tracks one tool call for the sidebar activity panel.
type toolEntry struct {
	name   string
	status string // "pending" | "ok" | "err"
}

type model struct {
	cfg        Config
	vp         viewport.Model
	ta         textarea.Model
	sp         spinner.Model
	transcript cappedBuffer
	liveText   *strings.Builder // pointer: strings.Builder panics if copied by value after first write
	renderer   *glamour.TermRenderer
	rendererW  int // tracks viewport width to know when to recreate renderer
	slash      *SlashDispatcher
	streaming  bool
	events     <-chan api.Event
	cancel     context.CancelFunc
	width      int
	height     int
	ready      bool
	status     string
	th         theme
	wizard     *wizardModel
	workDir    string

	tools        []toolEntry
	inputTokens  int
	outputTokens int
	costUSD      float64

	streamStart time.Time // when the current stream began; zero when idle
	turnCount   int       // conversation turns sent; guards turn separator logic

	// input history: sent messages oldest-first; histIdx is -1 when not navigating.
	history    []string
	histIdx    int
	draftInput string

	// overlay / modals
	keys        keyMap
	palette     *paletteModel
	helpOpen    bool
	activeToast *toast
	completion  completionState
}

type slashResultMsg SlashResult
type editorDoneMsg struct {
	content string
	err     error
}

// cappedBuffer is a []byte-backed writer that trims old content when it
// exceeds maxTranscriptBytes, preventing unbounded memory growth.
type cappedBuffer struct{ buf []byte }

const trimPrefix = "[earlier output trimmed]\n\n"

func (b *cappedBuffer) WriteString(s string) {
	b.buf = append(b.buf, s...)
	cap := maxTranscriptBytes - len(trimPrefix)
	if len(b.buf) > maxTranscriptBytes {
		trim := len(b.buf) - cap
		for trim < len(b.buf) && b.buf[trim] != '\n' {
			trim++
		}
		if trim < len(b.buf) {
			trim++
		}
		copy(b.buf, b.buf[trim:])
		b.buf = b.buf[:len(b.buf)-trim]
		b.buf = append([]byte(trimPrefix), b.buf...)
	}
}

func (b *cappedBuffer) String() string { return string(b.buf) }
func (b *cappedBuffer) Reset()         { b.buf = b.buf[:0] }

// --- messages ---

type streamStartedMsg struct {
	ch     <-chan api.Event
	cancel context.CancelFunc
}
type eventMsg api.Event
type streamClosedMsg struct{}
type errMsg struct{ err error }
type teammatesMsg struct {
	items []api.Teammate
	err   error
}

func newModel(cfg Config) model {
	ta := textarea.New()
	ta.Placeholder = "Message Aegis…"
	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(3)

	focusedStyle := ta.FocusedStyle
	focusedStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent)
	focusedStyle.CursorLine = lipgloss.NewStyle() // clear Bubbles default ANSI-black bg
	focusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colTextMuted)
	ta.FocusedStyle = focusedStyle

	blurredStyle := ta.BlurredStyle
	blurredStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder)
	blurredStyle.CursorLine = lipgloss.NewStyle() // same
	blurredStyle.Placeholder = lipgloss.NewStyle().Foreground(colTextMuted)
	ta.BlurredStyle = blurredStyle

	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	th := newTheme()

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	m := model{
		cfg:      cfg,
		ta:       ta,
		sp:       sp,
		th:       th,
		status:   "ready",
		slash:    NewSlashDispatcher(cfg.Client, cfg.SessionID, cfg.Mode, cfg.Model),
		histIdx:  -1,
		workDir:  workDir,
		liveText: &strings.Builder{},
		renderer: newGlamourRenderer(80), // initial width; recreated on first resize
		keys:     defaultKeyMap(),
	}
	m.transcript.WriteString(buildWelcomeContent(cfg, workDir, th))
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.sp.Tick)
}

// --- commands ---

func (m model) fetchTeammates() tea.Cmd {
	cl := m.cfg.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		items, err := cl.Teammates(ctx)
		return teammatesMsg{items: items, err: err}
	}
}

func (m model) startStream(text string) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := cl.PostMessage(ctx, id, text)
		if err != nil {
			cancel()
			return errMsg{err}
		}
		return streamStartedMsg{ch: ch, cancel: cancel}
	}
}

func (m model) handleSlashCommand(parsed *commands.ParsedCommand) tea.Cmd {
	slash := m.slash
	return func() tea.Msg { return slashResultMsg(slash.Dispatch(parsed)) }
}

func waitForEvent(ch <-chan api.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return eventMsg(ev)
	}
}

// --- update ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Wizard overlay: delegate all messages while the wizard is open.
	if m.wizard != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.wizard.width = ws.Width
			m.wizard.height = ws.Height
			m.layout()
			return m, nil
		}
		cmd := m.wizard.update(msg)
		if m.wizard.done {
			if m.wizard.saved {
				m.transcript.WriteString(
					m.th.statusText.Render("✓ Configuration saved — restart Aegis to apply changes.") + "\n\n",
				)
			}
			m.wizard = nil
			m.refresh()
		}
		return m, cmd
	}

	// Command palette overlay: route all input to the palette.
	if m.palette != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		updated, cmd := m.palette.Update(msg)
		m.palette = &updated
		return m, cmd
	}

	// Help overlay: only Escape or F1 closes it.
	if m.helpOpen {
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.Type == tea.KeyEsc || k.String() == "f1" {
				m.helpOpen = false
			}
		}
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refresh()
		m.ready = true

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		if m.streaming {
			cmds = append(cmds, cmd)
		}

	case toastExpiredMsg:
		m.activeToast = nil

	case paletteSelectedMsg:
		m.palette = nil
		m.ta.Focus()
		// Commands that need arguments get pre-filled in the textarea so the
		// user can add args before pressing Enter.
		needsArgs := map[string]bool{"persona": true, "mode": true, "remember": true}
		if needsArgs[msg.name] {
			m.ta.SetValue("/" + msg.name + " ")
		} else {
			parsed := &commands.ParsedCommand{Name: msg.name, Raw: "/" + msg.name}
			return m, m.handleSlashCommand(parsed)
		}

	case paletteCancelMsg:
		m.palette = nil
		m.ta.Focus()

	case editorDoneMsg:
		m.ta.Focus()
		if msg.err != nil {
			t, cmd := newToastCmd("editor: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		if strings.TrimSpace(msg.content) != "" {
			m.ta.SetValue(msg.content)
		}

	case tea.KeyMsg:
		// Inline completion popup intercepts navigation/accept keys first.
		// Other keys fall through to the textarea and trigger a recompute.
		if m.completion.active {
			switch msg.String() {
			case "up":
				m.completion.move(-1)
				return m, nil
			case "down", "ctrl+n":
				m.completion.move(1)
				return m, nil
			case "ctrl+p":
				m.completion.move(-1)
				return m, nil
			case "esc":
				m.completion = completionState{}
				m.applyViewportHeight()
				m.refresh()
				return m, nil
			case "tab":
				return m, m.acceptCompletion(false)
			case "enter":
				return m, m.acceptCompletion(true)
			}
		}

		switch msg.String() {
		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel()
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+t":
			return m, m.fetchTeammates()
		case "ctrl+l":
			if !m.streaming {
				return m, m.handleSlashCommand(&commands.ParsedCommand{Name: "clear", Raw: "/clear"})
			}
		case "ctrl+k":
			if !m.streaming {
				m.completion = completionState{}
				m.applyViewportHeight()
				pal := newPalette(m.width, m.height, allCommandEntries(m.slash.Customs()))
				m.palette = &pal
				return m, nil
			}
		case "f1":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "ctrl+e":
			if !m.streaming {
				return m, m.openEditorCmd()
			}
		case "shift+tab":
			if !m.streaming {
				return m, m.cycleModeCmd()
			}
		case "up":
			// Intercept only when input is single-line (no newlines) so that
			// multi-line editing keeps normal cursor-up behaviour.
			if !m.streaming && !strings.Contains(m.ta.Value(), "\n") && len(m.history) > 0 {
				if m.histIdx == -1 {
					m.draftInput = m.ta.Value()
					m.histIdx = len(m.history) - 1
				} else if m.histIdx > 0 {
					m.histIdx--
				}
				m.ta.SetValue(m.history[m.histIdx])
				return m, nil
			}
		case "down":
			if !m.streaming && m.histIdx != -1 {
				if m.histIdx == len(m.history)-1 {
					m.histIdx = -1
					m.ta.SetValue(m.draftInput)
					m.draftInput = ""
				} else {
					m.histIdx++
					m.ta.SetValue(m.history[m.histIdx])
				}
				return m, nil
			}
		case "enter":
			if m.streaming {
				return m, nil
			}
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			if parsed := commands.Parse(text); parsed != nil {
				m.ta.Reset()
				m.histIdx = -1
				m.draftInput = ""
				return m, m.handleSlashCommand(parsed)
			}
			m.history = append(m.history, text)
			m.histIdx = -1
			m.draftInput = ""
			m.appendUser(text)
			m.ta.Reset()
			m.streaming = true
			m.status = "thinking…"
			m.refresh()
			return m, m.startStream(text)
		}

	case streamStartedMsg:
		m.events = msg.ch
		m.cancel = msg.cancel
		m.streamStart = time.Now()
		return m, tea.Batch(waitForEvent(m.events), m.sp.Tick)

	case eventMsg:
		m.applyEvent(api.Event(msg))
		m.refresh()
		return m, waitForEvent(m.events)

	case streamClosedMsg:
		m.flushLiveText() // safety: in case KindTurnDone wasn't the last event
		m.streaming = false
		m.events = nil
		m.cancel = nil
		m.status = "ready"
		m.transcript.WriteString("\n")
		m.refresh()
		return m, nil

	case errMsg:
		m.streaming = false
		m.transcript.WriteString(m.th.errLine.Render("error: "+msg.err.Error()) + "\n\n")
		m.status = "ready"
		m.refresh()
		return m, nil

	case teammatesMsg:
		m.renderTeammates(msg)
		m.refresh()
		return m, nil

	case slashResultMsg:
		if msg.Quit {
			return m, tea.Quit
		}
		if msg.Output == "\x00wizard" {
			wiz := newWizard(m.width, m.height, m.th)
			m.wizard = wiz
			return m, wiz.init()
		}
		if msg.Output == "\x00clear" {
			m.transcript.Reset()
			m.tools = m.tools[:0]
			m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
			m.turnCount = 0
			m.transcript.WriteString(buildWelcomeContent(m.cfg, m.workDir, m.th))
			m.refresh()
			return m, nil
		}
		if msg.Output != "" {
			style := m.th.statusText
			if msg.IsError {
				style = m.th.errLine
			}
			m.transcript.WriteString(style.Render(msg.Output) + "\n\n")
		}
		if msg.Message != "" {
			m.appendUser(msg.Message)
			m.streaming = true
			m.status = "thinking…"
			m.refresh()
			return m, tea.Batch(m.startStream(msg.Message), m.sp.Tick)
		}
		m.refresh()
		return m, nil
	}

	if !m.streaming {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
		// Recompute inline completion after the textarea consumes the key.
		if _, isKey := msg.(tea.KeyMsg); isKey {
			m.syncCompletion()
		}
	}
	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	cmds = append(cmds, vpCmd)
	return m, tea.Batch(cmds...)
}

func (m model) cycleModeCmd() tea.Cmd {
	var next string
	switch m.slash.mode {
	case "plan":
		next = "build"
	case "build":
		next = "auto"
	default:
		next = "plan"
	}
	parsed := &commands.ParsedCommand{Name: "mode", Args: []string{next}, Raw: "/mode " + next}
	return m.handleSlashCommand(parsed)
}

// --- layout ---

// layout recalculates pane dimensions after a terminal resize.
// Height budget: title(1) + content(vpH) + textarea+border(ta.Height()+2) + belowBar(1)
// plus the completion popup box (completionBoxH) when the popup is active.
func (m *model) layout() {
	vpW := m.width - 1 // -1 for PaddingLeft on the main panel
	if m.width >= sidebarMinTermW {
		// sidebar consumes sidebarTotalW; main panel gets the rest minus left pad
		vpW = m.width - sidebarTotalW - 1
	}
	vpW = max(vpW, 10)

	if !m.ready {
		m.vp = viewport.New(vpW, max(m.height-m.fixedH(), 3))
	} else {
		m.vp.Width = vpW
	}
	m.applyViewportHeight()
	m.ta.SetWidth(m.width - 2) // -2 for left+right border chars

	if vpW != m.rendererW {
		m.rendererW = vpW
		m.renderer = newGlamourRenderer(vpW)
	}
}

// fixedH is the non-viewport vertical budget: title + textarea(+border) +
// belowBar, plus the completion popup when it is open.
func (m *model) fixedH() int {
	h := 1 + m.ta.Height() + 2 + 1
	if m.completion.active {
		h += completionBoxH
	}
	return h
}

// applyViewportHeight resizes the viewport to fit the current fixed budget.
func (m *model) applyViewportHeight() {
	m.vp.Height = max(m.height-m.fixedH(), 3)
}

// syncCompletion recomputes the inline completion popup from the textarea
// value, resizing the viewport when the popup opens or closes.
func (m *model) syncCompletion() {
	prev := m.completion.active
	m.completion = computeCompletion(m.ta.Value(), allCommandEntries(m.slash.Customs()))
	if m.completion.active != prev {
		m.applyViewportHeight()
		m.refresh()
	}
}

// acceptCompletion fills the highlighted command into the textarea. When run
// is true and the typed name already equals the highlighted command, it runs
// the command immediately (Enter behaviour); otherwise it completes the name.
func (m *model) acceptCompletion(run bool) tea.Cmd {
	e, ok := m.completion.current()
	if !ok {
		return nil
	}
	typed := strings.ToLower(strings.TrimPrefix(m.ta.Value(), "/"))
	if run && typed == e.name {
		m.ta.Reset()
		m.completion = completionState{}
		m.histIdx = -1
		m.draftInput = ""
		m.applyViewportHeight()
		m.refresh()
		return m.handleSlashCommand(&commands.ParsedCommand{Name: e.name, Raw: "/" + e.name})
	}
	if commandsNeedingArgs[e.name] {
		m.ta.SetValue("/" + e.name + " ")
	} else {
		m.ta.SetValue("/" + e.name)
	}
	m.syncCompletion()
	return nil
}

func (m *model) refresh() {
	content := m.transcript.String()
	if live := m.liveText.String(); live != "" {
		content += live
	}
	m.vp.SetContent(wrap(content, m.vp.Width))
	m.vp.GotoBottom()
}

// flushLiveText renders accumulated assistant text through glamour and appends
// it to the transcript. Called at KindTurnDone, KindToolCall, and KindError.
func (m *model) flushLiveText() {
	if m.liveText.Len() == 0 {
		return
	}
	raw := m.liveText.String()
	m.liveText.Reset()
	if m.renderer != nil {
		if rendered, err := m.renderer.Render(raw); err == nil {
			rendered = strings.TrimRight(rendered, "\n")
			m.transcript.WriteString(rendered + "\n")
			return
		}
	}
	m.transcript.WriteString(raw)
}

func (m *model) appendUser(text string) {
	if m.turnCount > 0 {
		sepW := m.vp.Width - 2
		if sepW < 10 {
			sepW = 60
		}
		m.transcript.WriteString(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
	}
	m.turnCount++
	m.transcript.WriteString(m.th.user.Render("You") + "\n" + text + "\n\n")
	m.transcript.WriteString(m.th.assistant.Render("Assistant") + "\n")
}

func (m *model) applyEvent(ev api.Event) {
	switch ev.Kind {
	case api.KindText:
		// Buffer text in liveText; flushed through glamour at turn end.
		m.liveText.WriteString(ev.Text)

	case api.KindToolCall:
		m.flushLiveText() // render any preceding prose before the tool line
		m.transcript.WriteString("\n" + m.th.tool.Render(
			fmt.Sprintf("⚙ %s  %s", ev.Tool, truncate(string(ev.ToolInput), 120))) + "\n")
		m.tools = append(m.tools, toolEntry{name: ev.Tool, status: "pending"})
		if len(m.tools) > maxToolHistory {
			m.tools = m.tools[1:]
		}

	case api.KindToolResult:
		style, tag := m.th.tool, "✓"
		if ev.ToolIsError {
			style, tag = m.th.toolErr, "✗"
		}
		m.transcript.WriteString(style.Render(
			fmt.Sprintf("%s %s → %s", tag, ev.Tool, truncate(oneLine(ev.ToolResult), 160))) + "\n")
		for i := len(m.tools) - 1; i >= 0; i-- {
			if m.tools[i].name == ev.Tool && m.tools[i].status == "pending" {
				if ev.ToolIsError {
					m.tools[i].status = "err"
				} else {
					m.tools[i].status = "ok"
				}
				break
			}
		}

	case api.KindTurnDone:
		m.flushLiveText() // render final prose through glamour
		if ev.OutputTokens > 0 {
			m.inputTokens = ev.InputTokens
			m.outputTokens = ev.OutputTokens
			m.costUSD += ev.CostUSD
		}

	case api.KindError:
		m.flushLiveText()
		m.transcript.WriteString("\n" + m.th.errLine.Render("error: "+ev.Error) + "\n")
	}
}

func (m *model) renderTeammates(msg teammatesMsg) {
	if msg.err != nil {
		m.transcript.WriteString("\n" + m.th.errLine.Render("teammates: "+msg.err.Error()) + "\n\n")
		return
	}
	if len(msg.items) == 0 {
		m.transcript.WriteString("\n" + m.th.statusDim.Render("⚇ no sub-agents spawned yet") + "\n\n")
		return
	}
	var b strings.Builder
	b.WriteString("\n" + m.th.assistant.Render(fmt.Sprintf("⚇ Teammates (%d)", len(msg.items))) + "\n")
	for _, tm := range msg.items {
		tag, style := "•", m.th.tool
		switch tm.Status {
		case "failed":
			tag, style = "✗", m.th.toolErr
		case "done":
			tag = "✓"
		}
		line := fmt.Sprintf("  %s %s [%s] %s", tag, tm.AgentID, tm.Status, oneLine(tm.Summary))
		b.WriteString(style.Render(truncate(line, m.width-1)) + "\n")
	}
	b.WriteString("\n")
	m.transcript.WriteString(b.String())
}

// --- view ---

func (m model) View() string {
	if !m.ready {
		return "initializing…"
	}
	if m.wizard != nil {
		return m.wizard.view()
	}
	if m.helpOpen {
		return m.renderHelpOverlay()
	}
	if m.palette != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.palette.View())
	}

	titleBar := m.renderTitleBar()
	inputArea := m.renderInputArea()

	var content string
	if m.width >= sidebarMinTermW {
		sidebar := m.renderSidebar(m.vp.Height)
		main := lipgloss.NewStyle().PaddingLeft(1).Render(m.vp.View())
		content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	} else {
		content = lipgloss.NewStyle().PaddingLeft(1).Render(m.vp.View())
	}

	parts := []string{titleBar, content}
	if m.completion.active {
		popupW := min(m.width-2, 72)
		popup := lipgloss.NewStyle().PaddingLeft(1).Render(m.completion.view(popupW))
		parts = append(parts, popup)
	}
	parts = append(parts, inputArea)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderTitleBar() string {
	brand := m.th.brandLabel.Render(" ⬡ AEGIS ")
	brandW := lipgloss.Width(brand)

	// Scroll indicator: shown whenever transcript content overflows the viewport.
	scroll := ""
	if m.vp.TotalLineCount() > m.vp.Height {
		if m.vp.AtBottom() {
			scroll = "end · "
		} else {
			scroll = fmt.Sprintf("%d%% · ", int(m.vp.ScrollPercent()*100))
		}
	}

	rightW := max(m.width-brandW, 0)
	right := lipgloss.NewStyle().
		Background(colSurface).
		Foreground(colTextMuted).
		Width(rightW).
		Align(lipgloss.Right).
		Render(scroll + m.cfg.Model + " ")

	return brand + right
}

func (m model) renderSidebar(h int) string {
	var b strings.Builder
	w := sidebarInnerW - 2 // usable text width (inner - left padding)

	add := func(s string) { b.WriteString(s + "\n") }
	section := func(title string) { add(m.th.sideSection.Render(title)) }

	add("")
	section("SESSION")
	add(m.th.sideValue.Render(short(m.cfg.SessionID)))
	add("")

	section("MODE")
	add(m.renderModeBadge())
	add("")

	section("MODEL")
	add(m.th.sideMuted.Render(truncate(m.cfg.Model, w)))
	add("")

	if m.streaming && !m.streamStart.IsZero() {
		section("GENERATING")
		secs := int(time.Since(m.streamStart).Seconds())
		add(m.th.elapsedDim.Render(fmt.Sprintf("%ds elapsed", secs)))
		add("")
	}

	if len(m.tools) > 0 {
		section("TOOLS")
		for _, t := range m.tools {
			tag, style := "⚙", m.th.tool
			switch t.status {
			case "ok":
				tag, style = "✓", m.th.sideValue
			case "err":
				tag, style = "✗", m.th.toolErr
			}
			add(style.Render(tag + " " + truncate(t.name, w-2)))
		}
		add("")
	}

	if m.inputTokens > 0 || m.costUSD > 0 {
		section("COST")
		if m.costUSD > 0 {
			add(m.th.costText.Render(fmt.Sprintf("$%.4f", m.costUSD)))
		}
		if m.inputTokens > 0 {
			add(m.th.sideMuted.Render(fmt.Sprintf("in  %d", m.inputTokens)))
			add(m.th.sideMuted.Render(fmt.Sprintf("out %d", m.outputTokens)))
		}
	}

	return lipgloss.NewStyle().
		Width(sidebarInnerW).
		Height(h).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		PaddingLeft(1).
		Render(b.String())
}

func (m model) renderInputArea() string {
	// Left side: streaming indicator with elapsed time, toast, or ready dot.
	var statusLeft string
	if m.streaming {
		elapsed := ""
		if !m.streamStart.IsZero() {
			elapsed = m.th.elapsedDim.Render(fmt.Sprintf(" %ds", int(time.Since(m.streamStart).Seconds())))
		}
		statusLeft = m.sp.View() + " " + m.th.statusText.Render(m.status) + elapsed
	} else if m.activeToast != nil {
		statusLeft = m.toastStyle(m.activeToast.level).Render(m.activeToast.message)
	} else {
		statusLeft = m.th.statusDim.Render("● ready")
	}
	leftW := lipgloss.Width(statusLeft)

	// Right side segments, highest → lowest priority. The loop drops from the
	// tail so lower-value segments disappear first on narrow terminals.
	//   badge (always)  →  hints  →  stats  →  cwd
	// Hints are more useful than cwd (cwd is also in the sidebar).
	segs := []string{m.renderModeBadge()}
	segs = append(segs, m.th.statusDim.Render("ctrl+k · f1 · ctrl+e"))
	if stats := m.renderStats(); stats != "" {
		segs = append(segs, m.th.statusDim.Render(stats))
	}
	segs = append(segs, m.th.cwdStyle.Render(shortenPath(m.workDir)))

	budget := m.width - leftW - 3 // 2 outer spaces + 1 minimum gap
	for len(segs) > 1 && joinedWidth(segs) > budget {
		segs = segs[:len(segs)-1]
	}
	right := strings.Join(segs, "  ")

	pad := max(m.width-leftW-lipgloss.Width(right)-2, 0)
	belowBar := " " + statusLeft + strings.Repeat(" ", pad) + right + " "

	return m.ta.View() + "\n" + belowBar
}

// joinedWidth returns the rendered width of segments joined by a two-space
// separator, used to decide how many status segments fit on one line.
func joinedWidth(segs []string) int {
	if len(segs) == 0 {
		return 0
	}
	w := 2 * (len(segs) - 1)
	for _, s := range segs {
		w += lipgloss.Width(s)
	}
	return w
}

func (m model) toastStyle(level toastLevel) lipgloss.Style {
	switch level {
	case toastWarn:
		return lipgloss.NewStyle().Foreground(colWarning)
	case toastError:
		return m.th.errLine
	default:
		return m.th.statusText
	}
}

func (m model) renderModeBadge() string {
	switch m.slash.mode {
	case "build":
		return m.th.modeBuild.Render(" build ")
	case "auto":
		return m.th.modeAuto.Render(" auto ")
	default:
		return m.th.modePlan.Render(" plan ")
	}
}

func (m model) renderStats() string {
	if m.inputTokens == 0 && m.outputTokens == 0 {
		return ""
	}
	s := fmt.Sprintf("in:%d out:%d", m.inputTokens, m.outputTokens)
	if m.costUSD > 0 {
		s += fmt.Sprintf("  $%.4f", m.costUSD)
	}
	return s
}

// --- help overlay ---

func (m model) renderHelpOverlay() string {
	km := m.keys
	entries := []struct{ k, d string }{
		{km.Send.Help().Key, km.Send.Help().Desc},
		{km.Newline.Help().Key, km.Newline.Help().Desc},
		{km.Complete.Help().Key, km.Complete.Help().Desc},
		{km.Palette.Help().Key, km.Palette.Help().Desc},
		{km.Cancel.Help().Key, km.Cancel.Help().Desc},
		{km.Help.Help().Key, km.Help.Help().Desc},
		{km.Clear.Help().Key, km.Clear.Help().Desc},
		{km.Editor.Help().Key, km.Editor.Help().Desc},
		{km.CycleMode.Help().Key, km.CycleMode.Help().Desc},
		{km.Teammates.Help().Key, km.Teammates.Help().Desc},
		{km.HistUp.Help().Key, km.HistUp.Help().Desc},
		{km.HistDown.Help().Key, km.HistDown.Help().Desc},
	}

	keyStyle := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Width(14)
	descStyle := lipgloss.NewStyle().Foreground(colTextDim)

	var rows strings.Builder
	for _, e := range entries {
		rows.WriteString(keyStyle.Render(e.k) + "  " + descStyle.Render(e.d) + "\n")
	}

	heading := lipgloss.NewStyle().Foreground(colBrandFg).Bold(true).Render("Keyboard Shortcuts")
	footer := lipgloss.NewStyle().Foreground(colTextMuted).Render("press f1 or esc to close")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(1, 3).
		Width(50).
		Render(heading + "\n\n" + rows.String() + "\n" + footer)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- external editor ---

func (m model) openEditorCmd() tea.Cmd {
	current := m.ta.Value()
	f, err := os.CreateTemp("", "aegis-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	tmpPath := f.Name()
	if current != "" {
		_, _ = f.WriteString(current)
	}
	f.Close()

	editor := defaultEditor()
	c := exec.Command(editor, tmpPath) //nolint:gosec
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		if err != nil {
			return editorDoneMsg{err: err}
		}
		raw, readErr := os.ReadFile(tmpPath)
		return editorDoneMsg{content: strings.TrimRight(string(raw), "\n"), err: readErr}
	})
}

func defaultEditor() string {
	for _, env := range []string{"EDITOR", "VISUAL"} {
		if e := os.Getenv(env); e != "" {
			return e
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// --- welcome content ---

// welcomeShield is the ASCII art shield shown on startup, each line exactly 14 chars.
var welcomeShield = []string{
	`  ╔══════════╗`,
	`  ║  AEGIS   ║`,
	`  ╠══════════╣`,
	`  ║    /\    ║`,
	`  ║   /  \   ║`,
	`  ║  / ◆  \  ║`,
	`  ║ /      \ ║`,
	`  ╚══╗    ╔══╝`,
	`     ╚════╝   `,
}

func buildWelcomeContent(cfg Config, workDir string, th theme) string {
	username := getUsername()
	shortCWD := shortenPath(workDir)

	info := []string{
		"",
		th.titleMeta.Render("AI agent harness"),
		"",
		"Welcome back, " + th.welcomeName.Render(username) + "!",
		"",
		th.welcomeKey.Render("Model  ") + th.welcomeVal.Render(cfg.Model),
		th.welcomeKey.Render("Mode   ") + th.welcomeVal.Render(cfg.Mode),
		th.welcomeKey.Render("Dir    ") + th.cwdStyle.Render(shortCWD),
		"",
	}

	var b strings.Builder
	b.WriteString("\n")
	for i, shieldLine := range welcomeShield {
		b.WriteString(th.shieldArt.Render(shieldLine))
		b.WriteString("  ")
		if i < len(info) {
			b.WriteString(info[i])
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(th.welcomeKey.Render("  ") +
		th.welcomeName.Render("/help") + th.welcomeKey.Render(" commands · ") +
		th.welcomeName.Render("ctrl+k") + th.welcomeKey.Render(" palette · ") +
		th.welcomeName.Render("shift+tab") + th.welcomeKey.Render(" mode"))
	b.WriteString("\n\n")
	return b.String()
}

func getUsername() string {
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "there"
}

func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// --- helpers ---

func newGlamourRenderer(width int) *glamour.TermRenderer {
	r, _ := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	return r
}

func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
