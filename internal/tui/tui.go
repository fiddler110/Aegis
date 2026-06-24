// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/scottymacleod/agentharness/internal/api"
	"github.com/scottymacleod/agentharness/internal/client"
)

// Config configures the TUI.
type Config struct {
	Client    *client.Client
	SessionID string
	Mode      string
	Model     string
}

// Run starts the TUI event loop and blocks until the user quits.
func Run(cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type model struct {
	cfg        Config
	vp         viewport.Model
	ta         textarea.Model
	transcript strings.Builder
	streaming  bool
	events     <-chan api.Event
	cancel     context.CancelFunc
	width      int
	height     int
	ready      bool
	status     string
	st         styles
}

type styles struct {
	user      lipgloss.Style
	assistant lipgloss.Style
	tool      lipgloss.Style
	toolErr   lipgloss.Style
	errLine   lipgloss.Style
	status    lipgloss.Style
}

func newStyles() styles {
	return styles{
		user:      lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
		assistant: lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true),
		tool:      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		toolErr:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		errLine:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		status:    lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
	}
}

func newModel(cfg Config) model {
	ta := textarea.New()
	ta.Placeholder = "Send a message (Enter to send, Ctrl+J newline, Ctrl+T teammates, Ctrl+C quit)…"
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.Focus()

	m := model{cfg: cfg, ta: ta, st: newStyles(), status: "ready"}
	m.transcript.WriteString(m.st.status.Render(fmt.Sprintf("session %s · model %s · mode %s\n\n", short(cfg.SessionID), cfg.Model, cfg.Mode)))
	return m
}

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

// fetchTeammates asks the daemon for the current swarm registry.
func (m model) fetchTeammates() tea.Cmd {
	cl := m.cfg.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		items, err := cl.Teammates(ctx)
		return teammatesMsg{items: items, err: err}
	}
}

func (m model) Init() tea.Cmd { return textarea.Blink }

func (m model) startStream(text string) tea.Cmd {
	cl := m.cfg.Client
	id := m.cfg.SessionID
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

func waitForEvent(ch <-chan api.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return eventMsg(ev)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refresh()
		m.ready = true

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.streaming && m.cancel != nil {
				m.cancel() // interrupt the in-flight run
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyCtrlT:
			return m, m.fetchTeammates() // show the swarm panel
		case tea.KeyEnter:
			if m.streaming {
				return m, nil // ignore input mid-run
			}
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
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
		return m, waitForEvent(m.events)

	case eventMsg:
		m.applyEvent(api.Event(msg))
		m.refresh()
		return m, waitForEvent(m.events)

	case streamClosedMsg:
		m.streaming = false
		m.events = nil
		m.cancel = nil
		m.status = "ready"
		m.transcript.WriteString("\n")
		m.refresh()
		return m, nil

	case errMsg:
		m.streaming = false
		m.transcript.WriteString(m.st.errLine.Render("error: "+msg.err.Error()) + "\n\n")
		m.status = "ready"
		m.refresh()
		return m, nil

	case teammatesMsg:
		m.renderTeammates(msg)
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

func (m *model) layout() {
	inputH := m.ta.Height() + 1
	statusH := 1
	vpH := m.height - inputH - statusH
	if vpH < 3 {
		vpH = 3
	}
	if !m.ready {
		m.vp = viewport.New(m.width, vpH)
	} else {
		m.vp.Width = m.width
		m.vp.Height = vpH
	}
	m.ta.SetWidth(m.width)
}

func (m *model) refresh() {
	m.vp.SetContent(wrap(m.transcript.String(), m.width))
	m.vp.GotoBottom()
}

func (m *model) appendUser(text string) {
	m.transcript.WriteString(m.st.user.Render("You") + "\n" + text + "\n\n")
	m.transcript.WriteString(m.st.assistant.Render("Assistant") + "\n")
}

func (m *model) applyEvent(ev api.Event) {
	switch ev.Kind {
	case api.KindText:
		m.transcript.WriteString(ev.Text)
	case api.KindToolCall:
		m.transcript.WriteString("\n" + m.st.tool.Render(fmt.Sprintf("⚙ %s %s", ev.Tool, truncate(string(ev.ToolInput), 200))) + "\n")
	case api.KindToolResult:
		style := m.st.tool
		tag := "✓"
		if ev.ToolIsError {
			style = m.st.toolErr
			tag = "✗"
		}
		m.transcript.WriteString(style.Render(fmt.Sprintf("%s %s → %s", tag, ev.Tool, truncate(oneLine(ev.ToolResult), 200))) + "\n")
	case api.KindTurnDone:
		if ev.OutputTokens > 0 {
			m.status = fmt.Sprintf("thinking… (in %d / out %d tokens)", ev.InputTokens, ev.OutputTokens)
			if ev.CostUSD > 0 {
				m.status += fmt.Sprintf(" · $%.4f", ev.CostUSD)
			}
		}
	case api.KindError:
		m.transcript.WriteString("\n" + m.st.errLine.Render("error: "+ev.Error) + "\n")
	}
}

// renderTeammates appends a swarm panel (the current sub-agents) to the
// transcript on demand (Ctrl+T).
func (m *model) renderTeammates(msg teammatesMsg) {
	if msg.err != nil {
		m.transcript.WriteString("\n" + m.st.errLine.Render("teammates: "+msg.err.Error()) + "\n\n")
		return
	}
	if len(msg.items) == 0 {
		m.transcript.WriteString("\n" + m.st.status.Render("⚇ no sub-agents spawned yet") + "\n\n")
		return
	}
	var b strings.Builder
	b.WriteString("\n" + m.st.assistant.Render(fmt.Sprintf("⚇ Teammates (%d)", len(msg.items))) + "\n")
	for _, tm := range msg.items {
		tag := "•"
		style := m.st.tool
		switch tm.Status {
		case "failed":
			tag, style = "✗", m.st.toolErr
		case "done":
			tag = "✓"
		}
		line := fmt.Sprintf("  %s %s [%s] %s", tag, tm.AgentID, tm.Status, oneLine(tm.Summary))
		b.WriteString(style.Render(truncate(line, m.width-1)) + "\n")
	}
	b.WriteString("\n")
	m.transcript.WriteString(b.String())
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	status := m.st.status.Render(m.status)
	return lipgloss.JoinVertical(lipgloss.Left, m.vp.View(), status, m.ta.View())
}

// --- helpers ---

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
