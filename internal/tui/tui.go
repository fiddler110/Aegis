// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"fmt"
	"os"
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
	liveText   strings.Builder    // streaming assistant text, flushed via glamour at turn end
	renderer   *glamour.TermRenderer
	rendererW  int                // tracks viewport width to know when to recreate renderer
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

	// input history: sent messages oldest-first; histIdx is -1 when not navigating.
	history    []string
	histIdx    int
	draftInput string
}

type slashResultMsg SlashResult

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
	ta.Placeholder = "Send a message  (Enter send · ↑↓ history · Ctrl+J newline · Shift+Tab mode · Ctrl+L clear · Ctrl+C quit)"
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(3)
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
		renderer: newGlamourRenderer(80), // initial width; recreated on first resize
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

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.streaming && m.cancel != nil {
				m.cancel()
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyCtrlT:
			return m, m.fetchTeammates()
		case tea.KeyCtrlL:
			if !m.streaming {
				return m, m.handleSlashCommand(&commands.ParsedCommand{Name: "clear", Raw: "/clear"})
			}
		case tea.KeyShiftTab:
			if !m.streaming {
				return m, m.cycleModeCmd()
			}
		case tea.KeyUp:
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
		case tea.KeyDown:
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
		case tea.KeyEnter:
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
// Height budget: title(1) + content(vpH) + topSep(1) + textarea(3) + botSep(1) + belowBar(1) = vpH+7
func (m *model) layout() {
	fixedH := 1 + 1 + m.ta.Height() + 1 + 1 // title + topSep + textarea + botSep + belowBar
	vpH := max(m.height-fixedH, 3)

	vpW := m.width - 1 // -1 for PaddingLeft on the main panel
	if m.width >= sidebarMinTermW {
		// sidebar consumes sidebarTotalW; main panel gets the rest minus left pad
		vpW = m.width - sidebarTotalW - 1
	}
	vpW = max(vpW, 10)

	if !m.ready {
		m.vp = viewport.New(vpW, vpH)
	} else {
		m.vp.Width = vpW
		m.vp.Height = vpH
	}
	m.ta.SetWidth(m.width)

	if vpW != m.rendererW {
		m.rendererW = vpW
		m.renderer = newGlamourRenderer(vpW)
	}
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

	return lipgloss.JoinVertical(lipgloss.Left,
		titleBar,
		content,
		inputArea,
	)
}

func (m model) renderTitleBar() string {
	brand := m.th.brandLabel.Render(" ⬡ AEGIS ")
	brandW := lipgloss.Width(brand)

	metaContent := " " + m.cfg.Model + " "
	metaW := len(metaContent)

	rightW := m.width - brandW
	if rightW < 0 {
		rightW = 0
	}

	padCount := max(rightW-metaW, 0)
	right := lipgloss.NewStyle().
		Background(colSurface).
		Foreground(colTextMuted).
		Render(strings.Repeat(" ", padCount) + metaContent)

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
	topSep := m.th.inputSep.Render(strings.Repeat("─", m.width))
	botSep := m.th.inputSep.Render(strings.Repeat("─", m.width))

	// Status on the left of the below-bar
	var statusLeft string
	if m.streaming {
		statusLeft = m.sp.View() + " " + m.th.statusText.Render(m.status)
	} else {
		statusLeft = m.th.statusDim.Render("● ready")
	}

	// Mode badge and cwd on the right
	badge := m.renderModeBadge()
	stats := m.renderStats()
	cwd := m.th.cwdStyle.Render(shortenPath(m.workDir))

	rightParts := badge
	if stats != "" {
		rightParts += "  " + m.th.statusDim.Render(stats)
	}
	rightParts += "  " + cwd

	leftW := lipgloss.Width(statusLeft)
	rightW := lipgloss.Width(rightParts)
	pad := max(m.width-leftW-rightW-2, 0)
	belowBar := " " + statusLeft + strings.Repeat(" ", pad) + rightParts + " "

	return topSep + "\n" + m.ta.View() + "\n" + botSep + "\n" + belowBar
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
		th.brandLabel.Render(" ⬡ AEGIS "),
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
