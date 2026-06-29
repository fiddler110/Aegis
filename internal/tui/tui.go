// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/client"
	"github.com/scottymacleod/aegis/internal/commands"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/session"
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
	p := tea.NewProgram(m)
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
	thinkText  *strings.Builder // accumulates extended-thinking text for the current turn
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

	tools               []toolEntry
	inputTokens         int  // uncached input tokens (last turn)
	outputTokens        int
	cacheReadTokens     int  // prompt-cache hits (last turn)
	cacheCreationTokens int  // prompt-cache writes (last turn)
	tokensEstimated     bool // true when token counts are derived from heuristic
	costUSD             float64

	streamStart time.Time // when the current stream began; zero when idle
	turnCount   int       // conversation turns sent; guards turn separator logic
	animStep    int       // frame counter for the streaming "working" shimmer

	// Wrapped-transcript cache. Re-wrapping the whole (up to 1 MiB) transcript
	// on every streamed token is O(n²) per turn; instead we wrap once and reuse
	// until the transcript content length or viewport width changes. Only the
	// small live tail is wrapped per refresh.
	wrapCache    string
	wrapCacheLen int
	wrapCacheW   int

	// Streaming live-text prefix cache. The live tail (liveText) is re-wrapped
	// on every token; caching the wrapped prefix up to the last safe markdown
	// boundary reduces this from O(n²) to O(tail) per turn.
	liveWrapCache   string
	liveWrapCacheTo int // byte offset up to which liveWrapCache is valid
	liveWrapCacheW  int // viewport width at which the cache was built

	// followBottom tracks whether the viewport should auto-scroll to the newest
	// content. It is true while the user is parked at the bottom and false once
	// they scroll up, so streaming output never yanks them back down mid-read.
	followBottom bool

	// escPending is true after a first ESC press during streaming; a second ESC
	// cancels the run. Any non-ESC key clears this state.
	escPending bool

	// input history: sent messages oldest-first; histIdx is -1 when not navigating.
	history    []string
	histIdx    int
	draftInput string

	// Lazily-built workspace file index for @file mention completion.
	fileIndex      []string
	fileIndexBuilt bool

	// Cached command-entry list (built-ins + custom), rebuilt only when the
	// custom-command count changes rather than on every keystroke.
	cmdEntriesCache []cmdEntry
	cmdEntriesLen   int

	// Terminal split pane (Ctrl+X to toggle).
	termOpen    bool
	termFocused bool
	term        termPane
	termRun     *termRun // non-nil while a command is running in the terminal

	// overlay / modals
	keys          keyMap
	palette       *paletteModel
	personaPicker *personaPickerModel
	sessionPicker *sessionPickerModel
	helpOpen      bool
	activeToast   *toast
	completion    completionState
	approval      *approvalState // non-nil while engine is blocked waiting for user approval
}

// approvalState holds the details of a pending tool-execution approval request.
type approvalState struct {
	toolName string
	input    string
	reason   string
	id       string // run id echoed back when answering
}

const approvalBannerH = 4 // lines rendered by renderApprovalBanner(): separator + 3 content

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
type sessionsLoadedMsg struct {
	items []api.SessionMeta
	err   error
}
type sessionSwitchedMsg struct {
	sess *session.Session
	err  error
}

func newModel(cfg Config) model {
	ta := textarea.New()
	ta.Placeholder = "Message Aegis…"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.DynamicHeight = true
	ta.MinHeight = 3
	ta.MaxHeight = 8

	// Crush-style editor prompt: a ❯ caret on the focused first line, and ":::"
	// continuation dots on wrapped/subsequent lines. Width 4 keeps the text
	// gutter aligned regardless of which variant is shown.
	ta.SetPromptFunc(4, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 && info.Focused {
			return lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("  ❯ ")
		}
		dots := colSuccessMost
		if !info.Focused {
			dots = colFgMore
		}
		return lipgloss.NewStyle().Foreground(dots).Render("::: ")
	})

	styles := ta.Styles()
	styles.Focused.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent)
	styles.Focused.CursorLine = lipgloss.NewStyle() // clear Bubbles default ANSI-black bg
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(colTextMuted)
	styles.Blurred.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder)
	styles.Blurred.CursorLine = lipgloss.NewStyle() // same
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colTextMuted)
	ta.SetStyles(styles)

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
		cfg:          cfg,
		ta:           ta,
		sp:           sp,
		th:           th,
		status:       "ready",
		slash:        NewSlashDispatcher(cfg.Client, cfg.SessionID, cfg.Mode, cfg.Model),
		histIdx:      -1,
		workDir:      workDir,
		liveText:     &strings.Builder{},
		thinkText:    &strings.Builder{},
		renderer:     newGlamourRenderer(80), // initial width; recreated on first resize
		keys:         defaultKeyMap(),
		followBottom: true,
		term:         newTermPane(workDir, 10), // height recalculated on first resize
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

func (m model) fetchSessions() tea.Cmd {
	cl := m.cfg.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		items, err := cl.ListSessions(ctx)
		return sessionsLoadedMsg{items: items, err: err}
	}
}

func (m model) switchSessionCmd(id string) tea.Cmd {
	cl := m.cfg.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sess, err := cl.GetSession(ctx, id)
		return sessionSwitchedMsg{sess: sess, err: err}
	}
}

func (m model) startStream(text string, images []api.ImageInput) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := cl.PostMessageReq(ctx, id, api.PostMessageRequest{Text: text, Images: images})
		if err != nil {
			cancel()
			return errMsg{err}
		}
		return streamStartedMsg{ch: ch, cancel: cancel}
	}
}

// extractImageRefs pulls @image:<path> tokens out of the submitted text and
// resolves each path (expanding ~ and making it absolute relative to workDir)
// into an image attachment. The remaining text is returned with those tokens
// removed. Paths must be whitespace-free in this syntax.
func extractImageRefs(text, workDir string) (clean string, images []api.ImageInput) {
	fields := strings.Fields(text)
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if path, ok := strings.CutPrefix(f, "@image:"); ok && path != "" {
			images = append(images, api.ImageInput{Path: resolveAttachPath(path, workDir)})
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " "), images
}

// resolveAttachPath expands a leading ~ and resolves relative paths against the
// workspace directory so the daemon receives an absolute path it can read.
func resolveAttachPath(path, workDir string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if workDir != "" {
		return filepath.Join(workDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// setSteerMode switches the textarea between normal input and steer mode.
// In steer mode the placeholder and border colour signal that Enter will
// inject a steering instruction into the running model turn rather than
// start a new conversation turn.
func (m *model) setSteerMode(on bool) {
	styles := m.ta.Styles()
	if on {
		m.ta.Placeholder = "Steer the model…"
		styles.Focused.Base = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colWarning)
		m.ta.Focus()
	} else {
		m.ta.Placeholder = "Message Aegis…"
		styles.Focused.Base = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent)
	}
	m.ta.SetStyles(styles)
}

func (m model) handleSlashCommand(parsed *commands.ParsedCommand) tea.Cmd {
	slash := m.slash
	return func() tea.Msg { return slashResultMsg(slash.Dispatch(parsed)) }
}

// sendSteerCmd posts a steering instruction to the daemon. The instruction is
// injected into the conversation between tool rounds by the engine.
func (m model) sendSteerCmd(text string) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cl.Steer(ctx, id, text); err != nil {
			return errMsg{err: fmt.Errorf("steer: %w", err)}
		}
		return nil
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
	// Result messages are handled here so they are not re-intercepted by this
	// same block on the next tick (the overlay would swallow them otherwise).
	if m.palette != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		if _, ok := msg.(paletteCancelMsg); ok {
			m.palette = nil
			m.ta.Focus()
			return m, nil
		}
		if sel, ok := msg.(paletteSelectedMsg); ok {
			m.palette = nil
			m.ta.Focus()
			needsArgs := map[string]bool{"mode": true, "remember": true}
			if needsArgs[sel.name] {
				m.ta.SetValue("/" + sel.name + " ")
				return m, nil
			}
			parsed := &commands.ParsedCommand{Name: sel.name, Raw: "/" + sel.name}
			return m, m.handleSlashCommand(parsed)
		}
		updated, cmd := m.palette.Update(msg)
		m.palette = &updated
		return m, cmd
	}

	// Persona picker overlay: route all input to the picker.
	// Result messages are handled here for the same reason as the palette above.
	if m.personaPicker != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		if _, ok := msg.(personaPickerCancelMsg); ok {
			m.personaPicker = nil
			m.ta.Focus()
			m.refresh()
			return m, nil
		}
		if sel, ok := msg.(personaPickerSelectedMsg); ok {
			m.personaPicker = nil
			m.ta.Focus()
			parsed := &commands.ParsedCommand{Name: "persona", Args: []string{sel.name}, Raw: "/persona " + sel.name}
			return m, m.handleSlashCommand(parsed)
		}
		updated, cmd := m.personaPicker.Update(msg)
		m.personaPicker = &updated
		return m, cmd
	}

	// Session picker overlay: route all input to the picker.
	if m.sessionPicker != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		if _, ok := msg.(sessionPickerCancelMsg); ok {
			m.sessionPicker = nil
			m.ta.Focus()
			return m, nil
		}
		if sel, ok := msg.(sessionPickerSelectedMsg); ok {
			m.sessionPicker = nil
			m.ta.Focus()
			if sel.id == m.cfg.SessionID {
				return m, nil // already on this session
			}
			return m, m.switchSessionCmd(sel.id)
		}
		updated, cmd := m.sessionPicker.Update(msg)
		m.sessionPicker = &updated
		return m, cmd
	}

	// Help overlay: only Escape or F1 closes it.
	if m.helpOpen {
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.String() == "esc" || k.String() == "f1" {
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
			m.animStep++ // advance the gradient working shimmer
			cmds = append(cmds, cmd)
			m.refresh() // animate the in-transcript thinking indicator
		}

	case toastExpiredMsg:
		m.activeToast = nil

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
		// Terminal toggle: always available regardless of focus or streaming state.
		if key.Matches(msg, m.keys.Terminal) {
			m.toggleTerminal()
			return m, nil
		}

		// Route all input to the terminal pane while it holds keyboard focus.
		if m.termFocused {
			return m, m.handleTerminalKey(msg)
		}

		// Approval prompt intercepts all keys while the engine is waiting for
		// user confirmation. Y/enter approves; N/esc denies; everything else
		// is swallowed (except viewport scroll which is handled after the switch).
		if m.approval != nil {
			switch msg.String() {
			case "y", "Y", "enter":
				id := m.approval.id
				m.approval = nil
				m.status = "thinking…"
				m.applyViewportHeight()
				return m, m.sendApprovalCmd(id, true)
			case "n", "N", "esc":
				id := m.approval.id
				m.approval = nil
				m.status = "thinking…"
				m.applyViewportHeight()
				return m, m.sendApprovalCmd(id, false)
			}
			// Let viewport scroll keys fall through to the vp.Update below.
			var vpCmd tea.Cmd
			m.vp, vpCmd = m.vp.Update(msg)
			m.followBottom = m.vp.AtBottom()
			return m, vpCmd
		}

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
		case "esc":
			if m.streaming {
				if m.escPending {
					// Second ESC: cancel the run.
					if m.cancel != nil {
						m.cancel()
					}
					m.escPending = false
				} else {
					// First ESC: arm the interrupt; status bar will show the warning.
					m.escPending = true
				}
				m.refresh()
				return m, nil
			}
			// Not streaming: clear the input box.
			m.ta.Reset()
			m.escPending = false
			return m, nil

		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel() // interrupt the in-flight run; press again to quit
				m.escPending = false
				return m, nil
			}
			if m.cancel != nil {
				m.cancel() // ensure no run keeps streaming (and spending) after we exit
			}
			return m, tea.Quit
		case "ctrl+t":
			return m, m.fetchTeammates()
		case "ctrl+r":
			if !m.streaming {
				return m, m.fetchSessions()
			}
		case "ctrl+l":
			if !m.streaming {
				return m, m.handleSlashCommand(&commands.ParsedCommand{Name: "clear", Raw: "/clear"})
			}
		case "ctrl+k":
			if !m.streaming {
				m.completion = completionState{}
				m.applyViewportHeight()
				pal := newPalette(m.width, m.height, m.commandEntries())
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
				// While the model is running, Enter injects a steering message
				// between tool rounds rather than starting a new conversation turn.
				text := strings.TrimSpace(m.ta.Value())
				if text == "" {
					return m, nil
				}
				m.ta.Reset()
				m.escPending = false
				return m, m.sendSteerCmd(text)
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
			cleanText, images := extractImageRefs(text, m.cfg.WorkDir)
			displayText := cleanText
			if displayText == "" && len(images) > 0 {
				suffix := ""
				if len(images) != 1 {
					suffix = "s"
				}
				displayText = fmt.Sprintf("(%d image%s attached)", len(images), suffix)
			}
			m.appendUser(displayText)
			m.ta.Reset()
			m.streaming = true
			m.status = "thinking…"
			m.followBottom = true // jump to the freshly sent message
			m.refresh()
			return m, m.startStream(cleanText, images)
		}

	case streamStartedMsg:
		m.events = msg.ch
		m.cancel = msg.cancel
		m.streamStart = time.Now()
		m.escPending = false
		m.setSteerMode(true)
		return m, tea.Batch(waitForEvent(m.events), m.sp.Tick)

	case eventMsg:
		m.applyEvent(api.Event(msg))
		m.refresh()
		return m, waitForEvent(m.events)

	case streamClosedMsg:
		m.flushThinking()
		m.flushLiveText() // safety: in case KindTurnDone wasn't the last event
		m.streaming = false
		m.events = nil
		m.cancel = nil
		m.status = "ready"
		m.escPending = false
		m.setSteerMode(false)
		m.transcript.WriteString("\n")
		m.refresh()
		return m, nil

	case errMsg:
		m.streaming = false
		m.escPending = false
		m.setSteerMode(false)
		m.transcript.WriteString(m.th.errLine.Render("error: "+msg.err.Error()) + "\n\n")
		m.status = "ready"
		m.refresh()
		return m, nil

	case teammatesMsg:
		m.renderTeammates(msg)
		m.refresh()
		return m, nil

	case sessionsLoadedMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("sessions: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		if len(msg.items) == 0 {
			t, cmd := newToastCmd("no sessions to switch to", toastInfo)
			m.activeToast = t
			return m, cmd
		}
		picker := newSessionPicker(m.width, m.height, msg.items, m.cfg.SessionID)
		m.sessionPicker = &picker
		return m, nil

	case sessionSwitchedMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("switch: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		m.applySwitchedSession(msg.sess)
		m.refresh()
		return m, nil

	case termOutputMsg:
		m.term.handleOutput(msg.text)
		m.refresh()
		if m.termRun != nil {
			return m, waitForTermOutput(m.termRun)
		}
		return m, nil

	case termDoneMsg:
		m.termRun = nil
		m.term.handleDone(msg.err)
		m.term.refreshVP()
		m.refresh()
		return m, nil

	case slashResultMsg:
		if msg.Quit {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if msg.Personas != nil {
			picker := newPersonaPicker(m.width, m.height, msg.Personas)
			m.personaPicker = &picker
			return m, nil
		}
		if msg.Output == "\x00wizard" {
			wiz := newWizard(m.width, m.height, m.th)
			m.wizard = wiz
			return m, wiz.init()
		}
		if msg.ReloadSession {
			// A rewind changed the conversation: reload it and report via toast,
			// since the reload resets the transcript.
			var cmds []tea.Cmd
			if msg.Output != "" {
				level := toastInfo
				if msg.IsError {
					level = toastError
				}
				t, c := newToastCmd(msg.Output, level)
				m.activeToast = t
				cmds = append(cmds, c)
			}
			cmds = append(cmds, m.switchSessionCmd(m.cfg.SessionID))
			return m, tea.Batch(cmds...)
		}
		if msg.Output == "\x00clear" {
			m.transcript.Reset()
			m.tools = m.tools[:0]
			m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
			m.cacheReadTokens, m.cacheCreationTokens = 0, 0
			m.tokensEstimated = false
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
			m.followBottom = true
			m.refresh()
			return m, tea.Batch(m.startStream(msg.Message, nil), m.sp.Tick)
		}
		m.refresh()
		return m, nil
	}

	{
		var cmd tea.Cmd
		prevTAH := m.ta.Height()
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
		// Recompute inline completion after the textarea consumes the key.
		if _, isKey := msg.(tea.KeyMsg); isKey {
			// With DynamicHeight, typing or deleting can grow/shrink the textarea;
			// update the viewport height immediately so it never overlaps the input.
			if m.ta.Height() != prevTAH {
				m.applyViewportHeight()
			}
			m.syncCompletion()
			// Any non-ESC key while escPending clears the interrupt arm state
			// (the ESC case already returns early and manages escPending itself).
			if m.streaming && m.escPending {
				m.escPending = false
				m.refresh()
			}
		}
	}
	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	// Re-derive scroll-follow state: auto-scroll resumes once the user returns
	// to the bottom and pauses the moment they scroll up.
	m.followBottom = m.vp.AtBottom()
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
	if m.termOpen {
		vpW -= termPaneTotalW
	}
	vpW = max(vpW, 10)

	if !m.ready {
		m.vp = viewport.New(viewport.WithWidth(vpW), viewport.WithHeight(max(m.height-m.fixedH(), 3)))
		m.vp.SoftWrap = true // wrap long lines; disables horizontal scrolling
	} else {
		m.vp.SetWidth(vpW)
	}
	// SetWidth must run before applyViewportHeight: with DynamicHeight it
	// triggers recalculateHeight, which changes ta.Height() and therefore fixedH().
	m.ta.SetWidth(m.width)
	m.applyViewportHeight()

	if m.termOpen {
		m.term.resize(max(m.height-m.fixedH(), 3))
	}

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
	if m.approval != nil {
		h += approvalBannerH
	}
	return h
}

// applyViewportHeight resizes the viewport to fit the current fixed budget.
func (m *model) applyViewportHeight() {
	m.vp.SetHeight(max(m.height-m.fixedH(), 3))
}

// commandEntries returns the cached built-in + custom command list, rebuilding
// it only when the custom-command count changes.
func (m *model) commandEntries() []cmdEntry {
	customs := m.slash.Customs()
	if m.cmdEntriesCache == nil || len(customs) != m.cmdEntriesLen {
		m.cmdEntriesCache = allCommandEntries(customs)
		m.cmdEntriesLen = len(customs)
	}
	return m.cmdEntriesCache
}

// syncCompletion recomputes the inline completion popup from the textarea
// value, resizing the viewport when the popup opens or closes.
func (m *model) syncCompletion() {
	prev := m.completion.active
	val := m.ta.Value()
	// Build the workspace file index lazily the first time an @mention appears.
	if !m.fileIndexBuilt && atTokenStart(val) >= 0 {
		m.fileIndex = buildFileIndex(m.workDir)
		m.fileIndexBuilt = true
	}
	m.completion = computeCompletion(val, m.commandEntries(), m.fileIndex)
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

	// @file mention / @ref: splice the choice in place of the typed @token.
	if m.completion.kind == compFile {
		val := m.ta.Value()
		start := m.completion.tokenStart
		if start < 0 || start > len(val) {
			start = len(val)
		}
		// Ref kinds taking a value (e.g. "image:") keep the cursor right after
		// the colon so the user types the value; others get a trailing space.
		sep := " "
		if strings.HasSuffix(e.name, ":") {
			sep = ""
		}
		m.ta.SetValue(val[:start] + "@" + e.name + sep)
		m.completion = completionState{}
		m.applyViewportHeight()
		m.refresh()
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
	// Wrap the static transcript once and cache it; only re-wrap when its byte
	// length or the viewport width changes. The transcript is append/reset/trim
	// only, so length is a sufficient change signal.
	base := m.transcript.String()
	if base == "" {
		m.wrapCache, m.wrapCacheLen, m.wrapCacheW = "", 0, m.vp.Width()
	} else if len(base) != m.wrapCacheLen || m.vp.Width() != m.wrapCacheW {
		m.wrapCache = wrap(base, m.vp.Width())
		m.wrapCacheLen = len(base)
		m.wrapCacheW = m.vp.Width()
	}
	content := m.wrapCache

	// Streaming extended-thinking is shown dim above the answer until it flushes.
	if think := m.thinkText.String(); think != "" {
		content += wrap(m.th.thinking.Render("✻ thinking")+"\n"+m.th.thinkingDim.Render(think)+"\n", m.vp.Width())
	}

	// The live tail is re-wrapped on every token. Cache the wrapped prefix up to
	// the last safe markdown boundary and only re-wrap the short suffix.
	if live := m.liveText.String(); live != "" {
		w := m.vp.Width()
		boundary := findSafeMarkdownBoundary(live)
		if boundary > m.liveWrapCacheTo || w != m.liveWrapCacheW {
			if boundary > 0 {
				m.liveWrapCache = wrap(live[:boundary], w)
			} else {
				m.liveWrapCache = ""
			}
			m.liveWrapCacheTo = boundary
			m.liveWrapCacheW = w
		}
		content += m.liveWrapCache + wrap(live[boundary:], w)
	} else if m.streaming {
		elapsed := ""
		if !m.streamStart.IsZero() {
			if secs := int(time.Since(m.streamStart).Seconds()); secs > 0 {
				elapsed = fmt.Sprintf("  %ds", secs)
			}
		}
		work := shimmerText("● thinking…", m.animStep, colTextMuted, colAccent)
		content += wrap(work+m.th.elapsedDim.Render(elapsed), m.vp.Width())
	}

	m.vp.SetContent(content)
	if m.followBottom {
		m.vp.GotoBottom()
	}
}

// flushThinking writes the accumulated extended-thinking text to the transcript
// as a dim block. Called when the answer or a tool call begins, or at turn end.
func (m *model) flushThinking() {
	if m.thinkText.Len() == 0 {
		return
	}
	raw := strings.TrimSpace(m.thinkText.String())
	m.thinkText.Reset()
	if raw == "" {
		return
	}
	m.transcript.WriteString(m.th.thinking.Render("✻ thinking") + "\n")
	m.transcript.WriteString(m.th.thinkingDim.Render(raw) + "\n\n")
}

// flushLiveText renders accumulated assistant text through glamour and appends
// it to the transcript. Called at KindTurnDone, KindToolCall, and KindError.
func (m *model) flushLiveText() {
	if m.liveText.Len() == 0 {
		return
	}
	raw := m.liveText.String()
	m.liveText.Reset()
	m.liveWrapCache, m.liveWrapCacheTo, m.liveWrapCacheW = "", 0, 0
	if m.renderer != nil {
		if rendered, err := m.renderer.Render(raw); err == nil {
			rendered = strings.TrimRight(rendered, "\n")
			m.transcript.WriteString(rendered + "\n")
			return
		}
	}
	m.transcript.WriteString(raw)
}

// sendApprovalCmd fires a POST to /sessions/{id}/approve with the user's
// decision. It runs in a goroutine so the TUI stays responsive while the
// request travels to the daemon.
func (m model) sendApprovalCmd(approvalID string, approved bool) tea.Cmd {
	cl := m.cfg.Client
	sessionID := m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cl.SendApproval(ctx, sessionID, approvalID, approved); err != nil {
			return errMsg{err: fmt.Errorf("approval: %w", err)}
		}
		return nil // engine continues via the existing SSE stream
	}
}

// toggleTerminal opens the terminal pane (with keyboard focus) if it is
// closed, or closes it (returning focus to the chat input) if it is open.
// Pressing ctrl+x while the terminal is open but chat is focused re-focuses
// the terminal; pressing ctrl+x while terminal is focused closes the pane.
func (m *model) toggleTerminal() {
	if !m.termOpen {
		m.termOpen = true
		m.termFocused = true
		m.ta.Blur()
		m.layout()
		m.refresh()
	} else if m.termFocused {
		// Close the pane and return focus to chat.
		m.termOpen = false
		m.termFocused = false
		m.ta.Focus()
		m.layout()
		m.refresh()
	} else {
		// Pane is open but chat is focused: focus the terminal.
		m.termFocused = true
		m.ta.Blur()
		m.refresh()
	}
}

// handleTerminalKey processes a key event when the terminal pane has focus.
// Printable characters append to the command line; named keys perform
// actions (run, cancel, history, etc.). Returns an optional tea.Cmd.
func (m *model) handleTerminalKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// When a command is running, only ctrl+c (interrupt) is active.
	if m.term.running {
		if k == "ctrl+c" && m.termRun != nil {
			m.termRun.cancel()
		}
		m.refresh()
		return nil
	}

	switch k {
	case "esc":
		m.termFocused = false
		m.ta.Focus()

	case "ctrl+c":
		m.term.input = ""

	case "enter":
		cmd := strings.TrimSpace(m.term.input)
		if cmd == "" {
			break
		}
		m.term.history = append(m.term.history, cmd)
		m.term.histIdx = -1
		m.term.draft = ""
		m.term.input = ""
		m.term.appendText("❯ " + cmd + "\n")
		if m.term.handleCD(cmd) {
			break
		}
		m.term.running = true
		run, execCmd := execTermCmd(m.term.workDir, cmd)
		m.termRun = run
		m.refresh()
		return execCmd

	case "up":
		m.term.historyPrev()

	case "down":
		m.term.historyNext()

	case "backspace":
		r := []rune(m.term.input)
		if len(r) > 0 {
			m.term.input = string(r[:len(r)-1])
		}

	case "ctrl+u":
		m.term.input = ""

	case "ctrl+l":
		m.term.buf.Reset()
		m.term.refreshVP()

	case "pgup", "pgdown":
		m.term.vp, _ = m.term.vp.Update(msg)

	default:
		// Append any single printable rune to the command line.
		if runes := []rune(k); len(runes) == 1 {
			m.term.input += k
		}
	}

	m.refresh()
	return nil
}

// renderApprovalBanner renders the 3-line prompt shown when the engine is
// blocked waiting for the user to approve or deny a tool execution.
func (m model) renderApprovalBanner() string {
	a := m.approval
	w := max(m.width-2, 20)

	// Line 1: tool name + truncated input
	header := lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⚡ "+a.toolName) +
		"  " + lipgloss.NewStyle().Foreground(colTextDim).Render(
		truncate(a.input, max(w-len(a.toolName)-6, 12)))

	// Line 2: permission reason
	reason := "  " + lipgloss.NewStyle().Foreground(colTextMuted).Render(a.reason)

	// Line 3: Y/N prompt
	prompt := "  " +
		lipgloss.NewStyle().Foreground(colSuccess).Bold(true).Render("[y]") +
		lipgloss.NewStyle().Foreground(colTextDim).Render(" approve  ") +
		lipgloss.NewStyle().Foreground(colDanger).Bold(true).Render("[n]") +
		lipgloss.NewStyle().Foreground(colTextDim).Render(" deny")

	sep := lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("─", w))
	return sep + "\n" + " " + header + "\n" + reason + "\n" + prompt
}

// applySwitchedSession swaps the active session, resetting per-session UI state
// and replaying the loaded transcript.
func (m *model) applySwitchedSession(sess *session.Session) {
	m.cfg.SessionID = sess.ID
	m.cfg.Mode = sess.Mode
	m.slash.SetSession(sess.ID, sess.Mode)

	m.transcript.Reset()
	m.tools = m.tools[:0]
	m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
	m.cacheReadTokens, m.cacheCreationTokens = 0, 0
	m.tokensEstimated = false
	m.turnCount = 0
	m.wrapCacheLen = -1 // force a re-wrap on next refresh
	m.streaming = false
	m.status = "ready"

	m.transcript.WriteString(buildWelcomeContent(m.cfg, m.workDir, m.th))
	m.loadHistory(sess.Messages)
	m.followBottom = true
}

// loadHistory replays stored conversation messages into the transcript so a
// resumed session shows its prior turns (user text, assistant prose, and tool
// activity) using the same rendering as a live run.
func (m *model) loadHistory(msgs []provider.Message) {
	toolNames := map[string]string{} // tool_use ID → name, for labelling results
	for _, msg := range msgs {
		switch msg.Role {
		case provider.RoleUser:
			var text string
			var results []provider.ToolResultBlock
			var images int
			for _, b := range msg.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ToolResultBlock:
					results = append(results, v)
				case provider.ImageBlock:
					images++
				}
			}
			if len(results) == 0 {
				if images > 0 {
					suffix := ""
					if images != 1 {
						suffix = "s"
					}
					note := fmt.Sprintf("🖼 %d image%s", images, suffix)
					if text != "" {
						text += "  " + note
					} else {
						text = "(" + note + ")"
					}
				}
				if text != "" {
					m.appendUser(text)
				}
			}
			for _, r := range results {
				name := toolNames[r.ToolUseID]
				if name == "" {
					name = "tool"
				}
				m.transcript.WriteString(renderToolResult(m.th, name, r.Content, r.IsError, m.vp.Width()) + "\n")
			}
		case provider.RoleAssistant:
			for _, b := range msg.Content {
				switch v := b.(type) {
				case provider.ThinkingBlock:
					if t := strings.TrimSpace(v.Text); t != "" {
						m.transcript.WriteString(m.th.thinking.Render("✻ thinking") + "\n")
						m.transcript.WriteString(m.th.thinkingDim.Render(t) + "\n\n")
					}
				case provider.TextBlock:
					if v.Text != "" {
						m.liveText.WriteString(v.Text)
						m.flushLiveText()
					}
				case provider.ToolUseBlock:
					toolNames[v.ID] = v.Name
					m.transcript.WriteString("\n" + renderToolCall(m.th, v.Name, v.Input, m.vp.Width()) + "\n")
				}
			}
		}
	}
}

func (m *model) appendUser(text string) {
	if m.turnCount > 0 {
		sepW := m.vp.Width() - 2
		if sepW < 10 {
			sepW = 60
		}
		m.transcript.WriteString(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
	}
	m.turnCount++
	m.transcript.WriteString(barLabel("You", colUserFg) + "\n" + text + "\n\n")
	m.transcript.WriteString(barLabel("Assistant", colAssistFg) + "\n")
}

func (m *model) applyEvent(ev api.Event) {
	switch ev.Kind {
	case api.KindThinking:
		// Buffer extended-thinking text; flushed as a dim block when the answer
		// (or a tool call) begins.
		m.thinkText.WriteString(ev.Text)

	case api.KindText:
		m.flushThinking() // reasoning is done once the answer starts
		// Buffer text in liveText; flushed through glamour at turn end.
		m.liveText.WriteString(ev.Text)

	case api.KindToolCall:
		m.flushThinking()
		m.flushLiveText() // render any preceding prose before the tool line
		m.transcript.WriteString("\n" + renderToolCall(m.th, ev.Tool, ev.ToolInput, m.vp.Width()) + "\n")
		m.tools = append(m.tools, toolEntry{name: ev.Tool, status: "pending"})
		if len(m.tools) > maxToolHistory {
			m.tools = m.tools[1:]
		}

	case api.KindToolResult:
		m.transcript.WriteString(renderToolResult(m.th, ev.Tool, ev.ToolResult, ev.ToolIsError, m.vp.Width()) + "\n")
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
		// Some local reasoning models (e.g. Gemma4 in Ollama) route their entire
		// output — both the reasoning chain and the final answer — through the
		// thinking/reasoning SSE field, leaving the content field empty. When that
		// happens, thinkText has content but liveText is empty. Promote the
		// thinking text to the response buffer so it renders with normal styling
		// rather than disappearing as dim unreachable text.
		if m.liveText.Len() == 0 && m.thinkText.Len() > 0 {
			m.liveText.WriteString(m.thinkText.String())
			m.thinkText.Reset()
		}
		m.flushThinking()
		m.flushLiveText() // render final prose through glamour
		if ev.OutputTokens > 0 || ev.TokensEstimated {
			m.inputTokens = ev.InputTokens
			m.outputTokens = ev.OutputTokens
			m.cacheReadTokens = ev.CacheReadTokens
			m.cacheCreationTokens = ev.CacheCreationTokens
			m.tokensEstimated = ev.TokensEstimated
			if !ev.TokensEstimated {
				m.costUSD += ev.CostUSD
			}
		}

	case api.KindApprovalRequest:
		m.approval = &approvalState{
			toolName: ev.Tool,
			input:    string(ev.ToolInput),
			reason:   ev.ApprovalReason,
			id:       ev.ApprovalID,
		}
		m.status = "approval required"

	case api.KindSteer:
		// A steering instruction was injected mid-run. Flush any partial model
		// output, show the steer as a user message, then open a new assistant bar
		// so the continuation renders under its own label.
		m.flushThinking()
		m.flushLiveText()
		sepW := max(m.vp.Width()-2, 10)
		m.transcript.WriteString(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
		m.transcript.WriteString(barLabel("You", colUserFg) + "\n" + ev.Text + "\n\n")
		m.transcript.WriteString(barLabel("Assistant", colAssistFg) + "\n")

	case api.KindDone:
		// Flush any buffered text (safety net — normally flushed at KindTurnDone).
		// If the last action was a tool call with no follow-up text, this ensures
		// the transcript is fully rendered before the run is marked complete.
		m.flushLiveText()

	case api.KindError:
		m.flushThinking()
		m.flushLiveText()
		m.approval = nil // clear any pending approval if the run aborts
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

// View wraps the rendered content in a tea.View, setting the v2 terminal modes
// (alt-screen, mouse, background) that were previously program options.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = colSurface
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) render() string {
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
	if m.personaPicker != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.personaPicker.View())
	}
	if m.sessionPicker != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.sessionPicker.View())
	}

	titleBar := m.renderTitleBar()
	inputArea := m.renderInputArea()

	main := lipgloss.NewStyle().PaddingLeft(1).Render(m.vp.View())
	var content string
	if m.width >= sidebarMinTermW {
		sidebar := m.renderSidebar(m.vp.Height())
		if m.termOpen {
			content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main, m.term.view(m.th, m.termFocused))
		} else {
			content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
		}
	} else {
		if m.termOpen {
			content = lipgloss.JoinHorizontal(lipgloss.Top, main, m.term.view(m.th, m.termFocused))
		} else {
			content = main
		}
	}

	parts := []string{titleBar, content}
	if m.completion.active {
		popupW := min(m.width-2, 72)
		popup := lipgloss.NewStyle().PaddingLeft(1).Render(m.completion.view(popupW))
		parts = append(parts, popup)
	}
	if m.approval != nil {
		parts = append(parts, m.renderApprovalBanner())
	}
	parts = append(parts, inputArea)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderTitleBar() string {
	brand := renderBrandMark()
	brandW := lipgloss.Width(brand)

	// Scroll indicator: shown whenever transcript content overflows the viewport.
	scroll := ""
	if m.vp.TotalLineCount() > m.vp.Height() {
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
	// Section headers carry a small diamond marker (Crush-style) so the panel
	// reads as a set of labelled groups rather than a flat column of words.
	section := func(title string) {
		add(m.th.sideSection.Render("◇ " + title))
	}

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
			tag, style := "●", m.th.tool
			switch t.status {
			case "ok":
				tag, style = "✓", m.th.sideValue
			case "err":
				tag, style = "×", m.th.toolErr
			}
			add(style.Render(tag + " " + truncate(t.name, w-2)))
		}
		add("")
	}

	// promptTokens is the full last-turn prompt size: uncached input plus any
	// cache reads/writes (Anthropic reports these separately).
	promptTokens := m.inputTokens + m.cacheReadTokens + m.cacheCreationTokens
	if promptTokens > 0 {
		section("CONTEXT")
		add(renderContextBar(promptTokens, contextWindowFor(m.cfg.Model), w))
		if m.cacheReadTokens > 0 {
			hit := int(float64(m.cacheReadTokens)/float64(promptTokens)*100 + 0.5)
			add(m.th.sideMuted.Render(fmt.Sprintf("cache %d%% hit", hit)))
		}
		add("")
	}

	if promptTokens > 0 || m.costUSD > 0 {
		section("COST")
		if m.costUSD > 0 {
			add(m.th.costText.Render(fmt.Sprintf("$%.4f", m.costUSD)))
		}
		if promptTokens > 0 {
			add(m.th.sideMuted.Render(fmt.Sprintf("in  %d", promptTokens)))
			add(m.th.sideMuted.Render(fmt.Sprintf("out %d", m.outputTokens)))
		}
	}

	return lipgloss.NewStyle().
		Width(sidebarInnerW).
		Height(h).
		MaxHeight(h). // prevent overflow: lipgloss Height() pads but never truncates
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		PaddingLeft(1).
		Render(b.String())
}

func (m model) renderInputArea() string {
	// Left side: streaming indicator with elapsed time, toast, or ready dot.
	var statusLeft string
	if m.streaming && m.escPending {
		statusLeft = lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⚠  ESC again to stop")
	} else if m.streaming {
		elapsed := ""
		if !m.streamStart.IsZero() {
			elapsed = m.th.elapsedDim.Render(fmt.Sprintf(" %ds", int(time.Since(m.streamStart).Seconds())))
		}
		statusLeft = shimmerText("● "+m.status, m.animStep, colTextMuted, colAccent) + elapsed
	} else if m.activeToast != nil {
		tag, fg, bg := toastTag(m.activeToast.level)
		statusLeft = statusTag(tag, fg, bg) + " " + m.toastStyle(m.activeToast.level).Render(m.activeToast.message)
	} else {
		statusLeft = statusTag("READY", colBgLess, colSuccess)
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

// statusTag renders a Crush-style padded, coloured indicator chip (e.g. READY,
// ERROR) — bold foreground on a solid status background.
func statusTag(label string, fg, bg color.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(true).Padding(0, 1).Render(label)
}

// toastTag maps a toast level to its indicator chip label and colours, mirroring
// Crush's Status.{Success,Warn,Error}Indicator pairings.
func toastTag(level toastLevel) (label string, fg, bg color.Color) {
	switch level {
	case toastWarn:
		return "WARN", colBgMost, colWarnSubtle
	case toastError:
		return "ERROR", colOnPrimary, colError
	default:
		return "INFO", colBgLess, colInfo
	}
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
		return m.th.sideValue.Render("build")
	case "auto":
		return m.th.sideValue.Render("auto")
	default:
		return m.th.sideValue.Render("plan")
	}
}

func (m model) renderStats() string {
	if m.inputTokens == 0 && m.outputTokens == 0 {
		return ""
	}
	est := ""
	if m.tokensEstimated {
		est = "~"
	}
	s := fmt.Sprintf("%sin:%d out:%d", est, m.inputTokens, m.outputTokens)
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
		{km.Interrupt.Help().Key, km.Interrupt.Help().Desc},
		{km.Complete.Help().Key, km.Complete.Help().Desc},
		{km.Palette.Help().Key, km.Palette.Help().Desc},
		{km.Cancel.Help().Key, km.Cancel.Help().Desc},
		{km.Help.Help().Key, km.Help.Help().Desc},
		{km.Clear.Help().Key, km.Clear.Help().Desc},
		{km.Editor.Help().Key, km.Editor.Help().Desc},
		{km.CycleMode.Help().Key, km.CycleMode.Help().Desc},
		{km.Teammates.Help().Key, km.Teammates.Help().Desc},
		{km.Sessions.Help().Key, km.Sessions.Help().Desc},
		{km.Terminal.Help().Key, km.Terminal.Help().Desc},
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

	shield := renderAegisLogo()
	var b strings.Builder
	b.WriteString("\n")
	for i, shieldLine := range shield {
		b.WriteString(shieldLine)
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
	if err != nil {
		return path
	}
	// Windows paths are case-insensitive; compare lowercased to avoid misses.
	homeCmp, pathCmp := home, path
	if runtime.GOOS == "windows" {
		homeCmp = strings.ToLower(home)
		pathCmp = strings.ToLower(path)
	}
	if strings.HasPrefix(pathCmp, homeCmp) {
		return "~" + path[len(home):]
	}
	return path
}

// --- helpers ---

func newGlamourRenderer(width int) *glamour.TermRenderer {
	// The TUI is dark-first (Charmtone Pantera), so use glamour's dark markdown
	// theme to match the lipgloss palette.
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
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

// contextWindowFor returns an approximate context-window size (in tokens) for a
// model, used to render the usage indicator. Values are conservative defaults
// matched on common model-name fragments; unknown models fall back to 128k.
func contextWindowFor(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gemini"):
		return 1_000_000
	case strings.Contains(m, "claude"), strings.Contains(m, "o1"), strings.Contains(m, "o3"):
		return 200_000
	case strings.Contains(m, "gpt-4.1"):
		return 1_000_000
	case strings.Contains(m, "gpt-4o"), strings.Contains(m, "gpt-4"), strings.Contains(m, "llama"), strings.Contains(m, "qwen"):
		return 128_000
	default:
		return 128_000
	}
}

// renderContextBar renders a compact usage meter for the context window:
// a filled/empty bar plus a percentage, coloured green→amber→red as it fills.
func renderContextBar(used, total, width int) string {
	if total <= 0 {
		total = 128_000
	}
	frac := float64(used) / float64(total)
	if frac > 1 {
		frac = 1
	}
	barW := max(width-5, 4) // leave room for " 99%"
	filled := int(frac*float64(barW) + 0.5)

	col := colSuccess
	switch {
	case frac >= 0.9:
		col = colDanger
	case frac >= 0.7:
		col = colWarning
	}
	bar := lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("▰", filled)) +
		lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("▱", barW-filled))
	pct := lipgloss.NewStyle().Foreground(colTextMuted).Render(fmt.Sprintf(" %d%%", int(frac*100+0.5)))
	return bar + pct
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// truncate shortens s to a display width of n cells, appending an ellipsis when
// it overflows. It is width- and rune-aware (and ANSI-aware), so it never slices
// a multi-byte rune in half or miscounts wide glyphs — important because these
// strings feed straight into lipgloss layout.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
