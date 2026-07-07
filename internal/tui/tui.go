// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// Config configures the TUI.
type Config struct {
	Client        *client.Client
	SessionID     string
	Mode          string
	Model         string
	WorkDir       string
	HumorMode     bool   // D&D-themed thinking phrases; false = plain "thinking…"
	Theme         string // color scheme name: "dark" (default) or "light" (TQ10)
	Notifications string // attention-system mode (P16.1): off/bell/desktop/both
}

// Run starts the TUI event loop and blocks until the user quits.
func Run(cfg Config) error {
	// Bind the configured color scheme before any styles are built — lipgloss
	// styles capture colors at creation time (TQ10).
	cfg.Theme = applyTheme(cfg.Theme, cfg.WorkDir)
	m := newModel(cfg)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

const (
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
	ta         textarea.Model
	sp         spinner.Model
	transcript *transcriptPane
	liveText   *strings.Builder // pointer: strings.Builder panics if copied by value after first write
	live       *liveBlock       // actively-streaming block; pointer for the same reason as liveText
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

	toolCompact         bool // when true, tool results are capped at toolMaxLinesCompact lines
	tools               []toolEntry
	inputTokens         int // uncached input tokens (last turn)
	outputTokens        int
	cacheReadTokens     int  // prompt-cache hits (last turn)
	cacheCreationTokens int  // prompt-cache writes (last turn)
	tokensEstimated     bool // true when token counts are derived from heuristic
	costUSD             float64

	streamStart time.Time // when the current stream began; zero when idle
	thinkStart  time.Time // when extended thinking began this turn; zero when idle
	turnCount   int       // conversation turns sent; guards turn separator logic
	animStep    int       // frame counter for the streaming "working" shimmer
	humorMode   bool      // when true, D&D phrases replace plain "thinking…"

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

	// queued holds messages typed with alt+enter during streaming (TQ8); they
	// render as dimmed pending blocks and auto-send one at a time when the
	// current stream closes. An explicit cancel discards the queue.
	queued []string

	// Lazily-built workspace file index for @file mention completion.
	fileIndex      []string
	fileIndexBuilt bool

	// Cached command-entry list (built-ins + custom), rebuilt only when the
	// custom-command count changes rather than on every keystroke.
	cmdEntriesCache []cmdEntry
	cmdEntriesLen   int

	// Sidebar visibility (Ctrl+B / /sidebar to toggle, default off).
	sidebarOpen bool

	// lastAssistantText holds the most recent complete assistant message for /copy.
	lastAssistantText string

	// todo strip — populated from todo_add/todo_update/todo_list tool events.
	todoItems       []todoStripItem
	pendingTodoText string // captured from todo_add call input, matched to result

	// pendingReadPaths is a FIFO queue of read_file paths awaiting their
	// KindToolResult (which carries no path/ID, only the tool name) — used to
	// chroma-highlight the result body by file extension (P16.2).
	pendingReadPaths []string

	// Collapsible thinking blocks (TQ9): each flushed thinking block keeps
	// both a one-line collapsed and a full expanded rendering; ctrl+o swaps
	// every block between the two in place.
	thinkEntries  []thinkEntry
	thinkExpanded bool

	// stashPath is the .aegis/stash.json path for draft persistence (P5.6).
	stashPath string

	// Terminal split pane (Ctrl+X to toggle).
	termOpen    bool
	termFocused bool
	term        termPane
	termRun     *termRun // non-nil while a command is running in the terminal

	// File frecency map for @-mention completion ranking (P2.3).
	fileFrecency map[string]int

	// Files written/edited this session for the sidebar FILES section (P2.4).
	changedFiles []string

	// Running sub-agents shown in the sidebar (P2.5).
	teammates []api.Teammate

	// Conversation timeline entries for /timeline picker (P2.8).
	timelineEntries []timelineEntry

	// overlay / modals
	keys keyMap
	// dialog is the single active filterable-list overlay (command palette,
	// persona/session/timeline/model picker) — P16.6 collapsed four
	// near-identical dialog types into one, tagged by dialog.kind.
	dialog         *listDialog
	securityConfig *securityConfigModel
	helpOpen       bool
	quitConfirm    bool // P16.6: confirm before quitting while a turn is streaming
	activeToast    *toast
	completion     completionState
	approval       *approvalState // non-nil while engine is blocked waiting for user approval

	// P16.1 attention system: notifyMode is parsed once from config/session
	// state; focused tracks terminal focus (via tea.FocusMsg/BlurMsg) so
	// notifications are suppressed while the user is already looking. Focus
	// reporting isn't supported by every terminal (see tea.BlurMsg docs), so
	// focused defaults to false ("not known to be focused") — when in doubt,
	// notify rather than silently suppress.
	notifyMode    notify.Mode
	focused       bool
	pendingNotify *notify.Event // set by applyEvent, consumed by the eventMsg handler

	// P16.5 mouse selection & click-to-focus: sel is documented on the
	// selection type; focusedIdx is the transcript segment index most
	// recently single-clicked (-1 for none) and drives the left accent bar
	// in renderTranscriptContent — purely a visual affordance, gates nothing.
	sel        selection
	focusedIdx int
}

// approvalState holds the details of a pending tool-execution approval request
// plus the option-list dialog state (TQ6).
type approvalState struct {
	toolName string
	input    string
	reason   string
	id       string // run id echoed back when answering

	selected     int    // highlighted dialog option
	pattern      string // suggested pattern for the "allow always" rule
	feedbackMode bool   // typing a deny reason
	feedback     string // the reason being typed
}

// todoStripItem is one entry in the live plan strip (TQ7).
type todoStripItem struct {
	id     int
	text   string
	status string // "pending" | "in_progress" | "done"
}

// clipboardResultMsg carries the result of an async clipboard write.
type clipboardResultMsg struct{ err error }

// pasteImageResultMsg carries the result of reading an image off the OS
// clipboard (P16.8). ok is false with a nil err when the clipboard simply
// held no image.
type pasteImageResultMsg struct {
	path string
	ok   bool
	err  error
}

// pasteClipboardImageCmd returns a tea.Cmd that reads an image from the OS
// clipboard into a temp file.
func pasteClipboardImageCmd() tea.Cmd {
	return func() tea.Msg {
		path, ok, err := pasteClipboardImage()
		return pasteImageResultMsg{path: path, ok: ok, err: err}
	}
}

type slashResultMsg SlashResult
type editorDoneMsg struct {
	content string
	err     error
}

// --- messages ---

type streamStartedMsg struct {
	ch     <-chan api.Event
	cancel context.CancelFunc
}
type eventMsg api.Event
type streamClosedMsg struct{}
type errMsg struct{ err error }

// bangMsg carries the result of a ! shell command (P2.2).
type bangMsg struct {
	cmd    string
	output string
	code   int
}

// teammatesUpdateMsg is a silent subagent poll result (P2.5).
type teammatesUpdateMsg struct{ items []api.Teammate }

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
	// TQ9: shift+enter inserts a newline on terminals speaking the Kitty
	// keyboard protocol (bubbletea v2 requests key disambiguation by default);
	// ctrl+j stays as the fallback everywhere else. Plain enter never reaches
	// the textarea — the Update switch intercepts it to send/steer.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "insert newline"),
	)

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

	stashPath := filepath.Join(workDir, ".aegis", "stash.json")
	m := model{
		cfg:          cfg,
		ta:           ta,
		sp:           sp,
		th:           th,
		status:       "ready",
		slash:        NewSlashDispatcher(cfg.Client, cfg.SessionID, cfg.Mode, cfg.Model, cfg.WorkDir),
		histIdx:      -1,
		focusedIdx:   -1,
		workDir:      workDir,
		transcript:   newTranscriptPane(80, 24), // initial size; resized on first WindowSizeMsg
		liveText:     &strings.Builder{},
		live:         &liveBlock{},
		thinkText:    &strings.Builder{},
		renderer:     newGlamourRenderer(80), // initial width; recreated on first resize
		keys:         defaultKeyMap(),
		followBottom: true,
		toolCompact:  true,
		humorMode:    cfg.HumorMode,
		term:         newTermPane(workDir, 10), // height recalculated on first resize
		stashPath:    stashPath,
		notifyMode:   notify.ParseMode(cfg.Notifications),
	}
	// P5.6: restore an unsent draft if one was saved from the previous session.
	if draft := loadStash(stashPath); draft != "" {
		m.ta.SetValue(draft)
	}
	m.transcript.Append(buildWelcomeContent(cfg, workDir, th))
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

// fetchTeammatesQuiet polls sub-agent status silently during streaming (P2.5).
func (m model) fetchTeammatesQuiet() tea.Cmd {
	cl := m.cfg.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		items, err := cl.Teammates(ctx)
		if err != nil {
			return nil
		}
		return teammatesUpdateMsg{items: items}
	}
}

// execBangCmd runs a ! shell command and returns its output (P2.2).
func (m model) execBangCmd(cmd string) tea.Cmd {
	workDir := m.workDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "sh", "-c", cmd) //nolint:gosec
		c.Dir = workDir
		out, err := c.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		return bangMsg{cmd: cmd, output: strings.TrimRight(string(out), "\n"), code: code}
	}
}

// recordChangedFile adds path to changedFiles if not already present (P2.4).
func (m *model) recordChangedFile(path string) {
	for _, f := range m.changedFiles {
		if f == path {
			return
		}
	}
	m.changedFiles = append(m.changedFiles, path)
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
		ch, err := cl.PostMessageReq(ctx, id, api.PostMessageRequest{Text: text, Images: images, GuardEnabled: m.slash.guardEnabled})
		if err != nil {
			cancel()
			return errMsg{err}
		}
		return streamStartedMsg{ch: ch, cancel: cancel}
	}
}

// imageRefRe matches @image:<path> tokens. The path is either double-quoted
// (may contain spaces — what pasted Windows/macOS paths often look like) or a
// bare whitespace-free token.
var imageRefRe = regexp.MustCompile(`@image:(?:"([^"]+)"|(\S+))`)

// extractImageRefs pulls @image:<path> tokens out of the submitted text and
// resolves each path (expanding ~ and making it absolute relative to workDir)
// into an image attachment. The remaining text is returned with those tokens
// removed. Paths containing spaces must be double-quoted (the paste handler
// quotes them automatically, TQ9).
func extractImageRefs(text, workDir string) (clean string, images []api.ImageInput) {
	clean = imageRefRe.ReplaceAllStringFunc(text, func(tok string) string {
		sub := imageRefRe.FindStringSubmatch(tok)
		path := sub[1]
		if path == "" {
			path = sub[2]
		}
		images = append(images, api.ImageInput{Path: resolveAttachPath(path, workDir)})
		return ""
	})
	if len(images) == 0 {
		return text, nil
	}
	// Tidy the holes the removed tokens left, preserving intentional newlines.
	lines := strings.Split(clean, "\n")
	for i, ln := range lines {
		lines[i] = strings.Join(strings.Fields(ln), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), images
}

// imageExts are the file extensions the paste handler recognizes as images.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
}

// looksLikeImagePath reports whether pasted text is a single-line path to an
// image file (TQ9): no newlines, an image extension, and no leftover quotes.
func looksLikeImagePath(s string) bool {
	if s == "" || strings.ContainsAny(s, "\n\r\"") {
		return false
	}
	return imageExts[strings.ToLower(filepath.Ext(s))]
}

// attachTokenFor builds the @image: token for a pasted path, quoting it when
// it contains spaces so extractImageRefs can recover it intact.
func attachTokenFor(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `@image:"` + path + `" `
	}
	return "@image:" + path + " "
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

// sendUserMessage appends text as a user turn and starts the stream. Shared by
// the enter/alt+enter key paths and the queued-message drain (TQ8).
func (m *model) sendUserMessage(text string) tea.Cmd {
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
	m.streaming = true
	m.status = "thinking…"
	m.followBottom = true // jump to the freshly sent message
	m.refresh()
	return m.startStream(cleanText, images)
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

	// Focus tracking (P16.1) always updates regardless of any open overlay —
	// it carries no interaction semantics of its own, just suppression state
	// for the attention system.
	switch msg.(type) {
	case tea.FocusMsg:
		m.focused = true
		return m, nil
	case tea.BlurMsg:
		m.focused = false
		return m, nil
	}

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
				m.transcript.Append(
					m.th.statusText.Render("✓ Configuration saved — restart Aegis to apply changes.") + "\n\n",
				)
			}
			m.wizard = nil
			m.refresh()
		}
		return m, cmd
	}

	// Security-config overlay: delegate all messages while it's open (P11.11).
	if m.securityConfig != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.securityConfig.width = ws.Width
			m.securityConfig.height = ws.Height
			m.layout()
			return m, nil
		}
		cmd := m.securityConfig.update(msg)
		if m.securityConfig.done {
			if m.securityConfig.saved {
				m.transcript.Append(
					m.th.statusText.Render("✓ Security config saved — restart Aegis to apply changes.") + "\n\n",
				)
			}
			m.securityConfig = nil
			m.refresh()
		}
		return m, cmd
	}

	// Dialog overlay (command palette, persona/session/timeline/model picker):
	// route all input to it. Result messages are handled here so they are not
	// re-intercepted by this same block on the next tick (the overlay would
	// swallow them otherwise). P16.6 collapsed four near-identical blocks
	// into one, dispatching by dialog.kind.
	if m.dialog != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		if c, ok := msg.(dialogCancelMsg); ok && c.kind == m.dialog.kind {
			m.dialog = nil
			m.ta.Focus()
			if c.kind == dialogPersonaPicker {
				m.refresh()
			}
			return m, nil
		}
		if sel, ok := msg.(dialogSelectedMsg); ok && sel.kind == m.dialog.kind {
			kind := m.dialog.kind
			m.dialog = nil
			m.ta.Focus()
			switch kind {
			case dialogPalette:
				item := sel.item.(paletteItem)
				needsArgs := map[string]bool{"mode": true, "remember": true}
				if needsArgs[item.name] {
					m.ta.SetValue("/" + item.name + " ")
					return m, nil
				}
				parsed := &commands.ParsedCommand{Name: item.name, Raw: "/" + item.name}
				return m, m.handleSlashCommand(parsed)
			case dialogPersonaPicker:
				item := sel.item.(personaItem)
				parsed := &commands.ParsedCommand{Name: "persona", Args: []string{item.name}, Raw: "/persona " + item.name}
				return m, m.handleSlashCommand(parsed)
			case dialogSessionPicker:
				item := sel.item.(sessionItem)
				if item.id == m.cfg.SessionID {
					return m, nil // already on this session
				}
				return m, m.switchSessionCmd(item.id)
			case dialogTimelinePicker:
				item := sel.item.(timelineItem)
				// Scroll to the selected turn's recorded item position.
				m.transcript.ScrollToItem(item.e.blockIndex)
				m.followBottom = false
				m.refresh()
				return m, nil
			case dialogModelPicker:
				item := sel.item.(modelItem)
				parsed := &commands.ParsedCommand{Name: "model", Args: []string{item.id}, Raw: "/model " + item.id}
				return m, m.handleSlashCommand(parsed)
			}
			return m, nil
		}
		updated, cmd := m.dialog.Update(msg)
		m.dialog = &updated
		return m, cmd
	}

	// Quit-confirmation overlay: shown instead of quitting outright when a
	// turn is streaming (ctrl+c / /quit / /exit) — P16.6.
	if m.quitConfirm {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "y", "enter":
				if m.cancel != nil {
					m.cancel()
				}
				saveStash(m.stashPath, m.ta.Value())
				return m, tea.Quit
			case "n", "esc":
				m.quitConfirm = false
			}
		}
		return m, nil
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
			m.layout()
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
			cmds = append(cmds, cmd) // always re-queue so animation resumes on scroll-back
			// P3.7: suppress redraws when the "● thinking…" indicator is scrolled
			// off-screen — it lives at the viewport bottom, visible only when
			// followBottom is true.
			if m.followBottom {
				m.animStep++
				m.refresh()
				// P2.5: poll sub-agent roster every 20 animation frames.
				if m.animStep%20 == 0 {
					cmds = append(cmds, m.fetchTeammatesQuiet())
				}
			}
		}

	case toastExpiredMsg:
		m.activeToast = nil

	case clipboardResultMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("copy: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		t, cmd := newToastCmd("copied to clipboard", toastInfo)
		m.activeToast = t
		return m, cmd

	case pasteImageResultMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("paste image: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		if !msg.ok {
			t, cmd := newToastCmd("clipboard has no image", toastInfo)
			m.activeToast = t
			return m, cmd
		}
		m.ta.InsertString(attachTokenFor(msg.path))
		t, cmd := newToastCmd("image attached: "+filepath.Base(msg.path), toastInfo)
		m.activeToast = t
		return m, cmd

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

	case tea.PasteMsg:
		// TQ9: a pasted image path becomes an attachment token instead of raw
		// text, replacing the typed @image:<path> incantation. Anything else
		// falls through to the textarea's own paste handling.
		if !m.termFocused {
			p := strings.TrimSpace(msg.Content)
			// Windows "Copy as path" and shell copies often quote the path.
			if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
				p = p[1 : len(p)-1]
			}
			if looksLikeImagePath(p) {
				m.ta.InsertString(attachTokenFor(p))
				t, cmd := newToastCmd("image attached: "+filepath.Base(p), toastInfo)
				m.activeToast = t
				return m, cmd
			}
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

		// Approval dialog intercepts all keys while the engine is waiting for
		// user confirmation (TQ6): ↑/↓ + enter select an option; y/a/n/f are
		// shortcuts; unmatched keys fall through to viewport scrolling.
		if m.approval != nil {
			return m.handleApprovalKey(msg)
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
		case "esc", "alt+esc":
			if m.streaming {
				// A fast double-tap can land both ESC bytes in the same terminal
				// read — likeliest exactly here, while streaming keeps the reader
				// busy re-rendering. Ultraviolet's decoder then reports that as one
				// "alt+esc" event instead of two separate "esc" ones, so treat it
				// as an already-confirmed second press rather than requiring a
				// third/fourth tap to get through.
				if m.escPending || msg.String() == "alt+esc" {
					// Second ESC: cancel the run. An explicit interrupt also
					// discards any queued messages (TQ8) — auto-sending after
					// the user hit the brakes would be a surprise.
					if m.cancel != nil {
						m.cancel()
					}
					m.escPending = false
					m.queued = nil
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
				m.queued = nil // TQ8: explicit interrupt discards the queue
				return m, nil
			}
			if m.cancel != nil {
				m.cancel()
			}
			saveStash(m.stashPath, m.ta.Value())
			return m, tea.Quit
		case "ctrl+b":
			m.sidebarOpen = !m.sidebarOpen
			m.layout()
			m.refresh()
			return m, nil
		case "ctrl+o":
			// TQ9: expand/collapse all thinking blocks in the transcript.
			m.toggleThinking()
			return m, nil
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
				m.dialog = &pal
				return m, nil
			}
		case "f1":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "ctrl+e":
			if !m.streaming {
				return m, m.openEditorCmd()
			}
		case "ctrl+v":
			return m, pasteClipboardImageCmd()
		case "shift+tab":
			if !m.streaming {
				return m, m.cycleModeCmd()
			}
		case "up":
			// TQ9: within a multiline draft ↑ moves the cursor; history
			// navigation only triggers when the cursor is already on the first
			// line (the standard Claude Code/opencode behaviour).
			if !m.streaming && m.ta.Line() == 0 && len(m.history) > 0 {
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
			// TQ9: mirror of ↑ — only leave history when the cursor sits on
			// the last line of the recalled entry.
			if !m.streaming && m.histIdx != -1 && m.ta.Line() == m.ta.LineCount()-1 {
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
			// P2.2: Bang ! shell mode — execute the rest as a shell command.
			if strings.HasPrefix(text, "!") {
				shellCmd := strings.TrimSpace(text[1:])
				if shellCmd == "" {
					return m, nil
				}
				m.ta.Reset()
				m.history = append(m.history, text)
				m.histIdx = -1
				m.draftInput = ""
				return m, m.execBangCmd(shellCmd)
			}
			if parsed := commands.Parse(text); parsed != nil {
				m.ta.Reset()
				m.histIdx = -1
				m.draftInput = ""
				return m, m.handleSlashCommand(parsed)
			}
			m.ta.Reset()
			return m, m.sendUserMessage(text)

		case "alt+enter":
			// TQ8: while streaming, alt+enter queues the draft as the next
			// user turn instead of steering; it auto-sends when the current
			// run finishes. When idle it behaves like a plain send.
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			m.ta.Reset()
			if m.streaming {
				m.queued = append(m.queued, text)
				m.escPending = false
				m.followBottom = true
				m.refresh()
				return m, nil
			}
			return m, m.sendUserMessage(text)
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
		var notifyCmd tea.Cmd
		if m.pendingNotify != nil {
			notifyCmd = m.notifyCmd(*m.pendingNotify)
			m.pendingNotify = nil
		}
		return m, tea.Batch(waitForEvent(m.events), notifyCmd)

	case streamClosedMsg:
		m.flushThinking()
		m.flushLiveText() // safety: in case KindTurnDone wasn't the last event
		m.streaming = false
		m.events = nil
		m.cancel = nil
		m.status = "ready"
		m.escPending = false
		m.setSteerMode(false)
		m.transcript.Append("\n")
		// TQ8: auto-send the next queued message, one per completed run. Don't
		// notify here — another run is about to start immediately.
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m, m.sendUserMessage(next)
		}
		m.refresh()
		return m, m.notifyCmd(notify.Event{Title: "Aegis", Body: "Run finished"})

	case errMsg:
		m.streaming = false
		m.escPending = false
		m.setSteerMode(false)
		m.transcript.Append(m.th.errLine.Render("error: "+msg.err.Error()) + "\n\n")
		// TQ8: don't auto-send into a failing session.
		if len(m.queued) > 0 {
			m.queued = nil
			m.transcript.Append(m.th.statusDim.Render("⏳ queued messages discarded after error") + "\n\n")
		}
		m.status = "ready"
		m.refresh()
		return m, m.notifyCmd(notify.Event{Title: "Aegis", Body: "Error: " + truncate(msg.err.Error(), 100)})

	case bangMsg: // P2.2: shell command result
		header := m.th.tool.Render("! " + msg.cmd)
		m.transcript.Append("\n" + header + "\n")
		if msg.output != "" {
			style := m.th.sideValue
			if msg.code != 0 {
				style = m.th.toolErr
			}
			m.transcript.Append(style.Render(msg.output) + "\n")
		}
		if msg.code != 0 {
			m.transcript.Append(m.th.toolErr.Render(fmt.Sprintf("exit %d", msg.code)) + "\n")
		}
		m.transcript.Append("\n")
		m.followBottom = true
		m.refresh()
		return m, nil

	case teammatesUpdateMsg: // P2.5: silent sub-agent poll
		m.teammates = msg.items
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
		m.dialog = &picker
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
			// P16.6: confirm before discarding an in-flight stream instead of
			// quitting silently — /quit and /exit used to cancel and exit
			// unconditionally even mid-response.
			if m.streaming {
				m.quitConfirm = true
				return m, nil
			}
			if m.cancel != nil {
				m.cancel()
			}
			saveStash(m.stashPath, m.ta.Value())
			return m, tea.Quit
		}
		if msg.Model != nil {
			m.cfg.Model = *msg.Model
		}
		if msg.Personas != nil {
			picker := newPersonaPicker(m.width, m.height, msg.Personas)
			m.dialog = &picker
			return m, nil
		}
		if msg.Models != nil {
			picker := newModelPicker(m.width, m.height, msg.Models, m.cfg.Model)
			m.dialog = &picker
			return m, nil
		}
		if msg.Output == "\x00wizard" {
			wiz := newWizard(m.width, m.height, m.th)
			m.wizard = wiz
			return m, wiz.init()
		}
		if msg.SecurityConfigGlobal != nil {
			sc := newSecurityConfigModel(m.width, m.height, m.th, *msg.SecurityConfigGlobal)
			m.securityConfig = sc
			return m, sc.init()
		}
		if msg.Output == "\x00timeline" { // P2.8
			if len(m.timelineEntries) == 0 {
				t, cmd := newToastCmd("no turns in timeline yet", toastInfo)
				m.activeToast = t
				return m, cmd
			}
			picker := newTimelinePicker(m.width, m.height, m.timelineEntries)
			m.dialog = &picker
			return m, nil
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
			m.thinkEntries = nil
			m.tools = m.tools[:0]
			m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
			m.cacheReadTokens, m.cacheCreationTokens = 0, 0
			m.tokensEstimated = false
			m.turnCount = 0
			m.changedFiles = m.changedFiles[:0]
			m.teammates = nil
			m.timelineEntries = m.timelineEntries[:0]
			m.transcript.Append(buildWelcomeContent(m.cfg, m.workDir, m.th))
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00tools-compact" {
			m.toolCompact = true
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00tools-full" {
			m.toolCompact = false
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00sidebar-toggle" {
			m.sidebarOpen = !m.sidebarOpen
			m.layout()
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00theme-show" {
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Current theme: %s", m.cfg.Theme)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00theme ") {
			// P14.8: applyTheme only rebinds the package-level col* vars —
			// m.th and m.renderer were built from those vars at creation time
			// (lipgloss styles and the glamour renderer both capture colors
			// once) and must be explicitly rebuilt to actually change what's
			// on screen. Already-rendered transcript content keeps its old
			// colors, same limitation /humor and /sidebar already have for
			// past output.
			name := strings.TrimPrefix(msg.Output, "\x00theme ")
			name = applyTheme(name, m.cfg.WorkDir)
			m.cfg.Theme = name
			m.th = newTheme()
			m.renderer = newGlamourRenderer(m.rendererW)
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Theme switched to %s. This session only — set tui.theme: %s in config to persist.", name, name)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00notify-show" {
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Current notify mode: %s", m.notifyMode)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00notify ") {
			name := strings.TrimPrefix(msg.Output, "\x00notify ")
			m.notifyMode = notify.ParseMode(name)
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Notify mode switched to %s. This session only — set tui.notifications: %s in config to persist.", name, name)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00copy") {
			arg := strings.TrimPrefix(msg.Output, "\x00copy")
			arg = strings.TrimSpace(arg)
			var text string
			if arg == "" {
				text = m.lastAssistantText
			} else {
				n := 0
				fmt.Sscanf(arg, "%d", &n)
				blocks := extractCodeBlocks(m.lastAssistantText)
				if n >= 1 && n <= len(blocks) {
					text = blocks[n-1]
				} else {
					t, cmd := newToastCmd(fmt.Sprintf("no code block #%d in last message", n), toastError)
					m.activeToast = t
					return m, cmd
				}
			}
			if text == "" {
				t, cmd := newToastCmd("nothing to copy (no assistant message yet)", toastInfo)
				m.activeToast = t
				return m, cmd
			}
			return m, copyToClipboardCmd(text)
		}
		if msg.Output == "\x00paste-image" {
			return m, pasteClipboardImageCmd()
		}
		if msg.Output == "\x00humor-on" {
			m.humorMode = true
			m.transcript.Append(m.th.statusText.Render("Humor mode: on — rolling for initiative 🎲") + "\n\n")
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00humor-off" {
			m.humorMode = false
			m.transcript.Append(m.th.statusText.Render("Humor mode: off — plain status text") + "\n\n")
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00humor-toggle" {
			m.humorMode = !m.humorMode
			if m.humorMode {
				m.transcript.Append(m.th.statusText.Render("Humor mode: on — rolling for initiative 🎲") + "\n\n")
			} else {
				m.transcript.Append(m.th.statusText.Render("Humor mode: off — plain status text") + "\n\n")
			}
			m.refresh()
			return m, nil
		}
		if msg.Output != "" {
			style := m.th.statusText
			if msg.IsError {
				style = m.th.errLine
			}
			m.transcript.Append(style.Render(msg.Output) + "\n\n")
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
	switch tmsg := msg.(type) {
	case tea.KeyMsg:
		m.transcript.HandleKey(tmsg)
	case tea.MouseWheelMsg:
		m.transcript.HandleMouseWheel(tmsg)
	case tea.MouseClickMsg:
		cmds = append(cmds, m.handleMouseClick(tmsg))
	case tea.MouseMotionMsg:
		m.handleMouseMotion(tmsg)
	case tea.MouseReleaseMsg:
		cmds = append(cmds, m.handleMouseRelease(tmsg))
	}
	// Re-derive scroll-follow state: auto-scroll resumes once the user returns
	// to the bottom and pauses the moment they scroll up.
	m.followBottom = m.transcript.AtBottom()
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
	if m.sidebarOpen && m.width >= sidebarMinTermW {
		// sidebar consumes sidebarTotalW; main panel gets the rest minus left pad
		vpW = m.width - sidebarTotalW - 1
	}
	if m.termOpen {
		vpW -= termPaneTotalW
	}
	vpW -= 1 // scrollbar column (P16.5), rendered to the right of the transcript
	vpW = max(vpW, 10)

	m.transcript.SetSize(vpW, m.transcript.Height())
	// SetSize's width must be applied before applyViewportHeight: with
	// DynamicHeight it triggers recalculateHeight, which changes ta.Height()
	// and therefore fixedH().
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
// belowBar, plus the completion popup and optional strips.
func (m *model) fixedH() int {
	h := 1 + m.ta.Height() + 2 + 1
	if m.completion.active {
		h += completionBoxH
	}
	if m.approval != nil {
		// The dialog height varies with its preview and option list (TQ6).
		h += lipgloss.Height(m.renderApprovalDialog())
	}
	if len(m.todoItems) > 0 {
		h += 1 // todo strip: one line
	}
	return h
}

// applyViewportHeight resizes the transcript pane to fit the current fixed budget.
func (m *model) applyViewportHeight() {
	m.transcript.SetSize(m.transcript.Width(), max(m.height-m.fixedH(), 3))
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
	// P2.3: sort file index by frecency so recently-used files appear first.
	files := m.fileIndex
	if len(m.fileFrecency) > 0 {
		sorted := make([]string, len(files))
		copy(sorted, files)
		sort.SliceStable(sorted, func(i, j int) bool {
			return m.fileFrecency[sorted[i]] > m.fileFrecency[sorted[j]]
		})
		files = sorted
	}
	m.completion = computeCompletion(val, m.commandEntries(), files)
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
	// The real conversation content lives in m.transcript's items, each
	// caching its own wrapped output (TQ1) and windowed by View() so a
	// refresh costs O(visible), not O(whole history) (P16.4). Only the
	// ephemeral trailing content — streaming preview, live tail, queued
	// messages — is built fresh here every call, exactly as it was built
	// into the old flat `content` string, just handed to the pane as a
	// separate trailing segment instead of concatenated onto everything
	// that came before it.
	w := m.transcript.Width()
	var tail strings.Builder

	// Streaming extended-thinking is shown dim above the answer until it flushes.
	if think := m.thinkText.String(); think != "" {
		tail.WriteString(wrap(m.th.thinking.Render("✻ thinking")+"\n"+m.th.thinkingDim.Render(think)+"\n", w))
	}

	// The live tail keeps its own boundary-cached re-render (see liveBlock) so
	// a long streaming reply stays O(tail) per token instead of O(n). Text is
	// styled through glamour as it streams (TQ3) so there is no end-of-turn
	// restyle pop.
	if live := m.liveText.String(); live != "" {
		m.live.setText(live)
		tail.WriteString(m.live.render(w, m.mdRender))
	} else if m.streaming {
		secs := 0
		if !m.streamStart.IsZero() {
			secs = int(time.Since(m.streamStart).Seconds())
		}
		cat := catThinking
		if n := len(m.tools); n > 0 && m.tools[n-1].status == "pending" {
			cat = categoryFor(m.tools[n-1].name)
		}
		phrase := thinkingPhrase(m.animStep, m.humorMode, cat)
		hint := formatStreamHint(secs, m.inputTokens, 0) // no live-text bytes here; liveText is empty
		work := shimmerText("● "+phrase, m.animStep, colTextMuted, colAccent)
		tail.WriteString(wrap(work+m.th.elapsedDim.Render(hint), w))
	}

	// TQ8: queued messages render as dimmed pending blocks below the live tail.
	for _, q := range m.queued {
		line := m.th.statusDim.Render("⏳ queued ▸ " + truncate(oneLine(q), max(w-12, 16)))
		tail.WriteString("\n" + wrap(line, w))
	}

	m.transcript.SetTail(tail.String())
	if m.followBottom {
		m.transcript.GotoBottom()
	}
}

// thinkEntry pairs a flushed thinking transcript block with its two renderings
// so ctrl+o can swap them in place (TQ9).
type thinkEntry struct {
	blk       *transcriptItem
	collapsed string
	expanded  string
}

// flushThinking writes the accumulated extended-thinking text to the
// transcript as a collapsible block (TQ9): a one-line "✻ thought for Ns"
// header by default, expandable to the full text with ctrl+o. Called when the
// answer or a tool call begins, or at turn end.
func (m *model) flushThinking() {
	if m.thinkText.Len() == 0 {
		return
	}
	raw := strings.TrimSpace(m.thinkText.String())
	m.thinkText.Reset()
	secs := 0
	if !m.thinkStart.IsZero() {
		secs = int(time.Since(m.thinkStart).Seconds() + 0.5)
	}
	m.thinkStart = time.Time{}
	if raw == "" {
		return
	}
	m.appendThinkingBlock(raw, secs)
}

// appendThinkingBlock adds one collapsible thinking block to the transcript,
// honouring the current expand/collapse state. secs 0 (e.g. replayed history,
// where the duration is unknown) omits the "for Ns" suffix.
func (m *model) appendThinkingBlock(raw string, secs int) {
	header := "✻ thought"
	if secs > 0 {
		header += fmt.Sprintf(" for %ds", secs)
	}
	collapsed := m.th.thinking.Render(header) + m.th.thinkingDim.Render("  (ctrl+o to expand)") + "\n\n"
	expanded := m.th.thinking.Render(header) + "\n" + m.th.thinkingDim.Render(raw) + "\n\n"
	use := collapsed
	if m.thinkExpanded {
		use = expanded
	}
	blk := m.transcript.AppendBlock(use)
	if blk != nil {
		m.thinkEntries = append(m.thinkEntries, thinkEntry{blk: blk, collapsed: collapsed, expanded: expanded})
	}
}

// toggleThinking swaps every thinking block between its collapsed and expanded
// form in place (TQ9, ctrl+o). Entries whose block has been trimmed out of the
// transcript are dropped.
func (m *model) toggleThinking() {
	if len(m.thinkEntries) == 0 {
		return
	}
	m.thinkExpanded = !m.thinkExpanded
	present := make(map[*transcriptItem]bool, m.transcript.Len())
	for _, b := range m.transcript.items {
		present[b] = true
	}
	kept := m.thinkEntries[:0]
	for _, e := range m.thinkEntries {
		if !present[e.blk] {
			continue
		}
		raw := e.collapsed
		if m.thinkExpanded {
			raw = e.expanded
		}
		m.transcript.SetItemRaw(e.blk, raw)
		kept = append(kept, e)
	}
	m.thinkEntries = kept
	m.refresh()
}

// mdRender renders markdown through glamour with trailing newlines normalized
// to exactly one, so a settled-prefix + tail concatenation (liveBlock) is
// byte-identical to a single whole-source render split at the same paragraph
// boundary. Falls back to a plain wrap if the renderer is unavailable.
func (m *model) mdRender(s string) string {
	if m.renderer != nil {
		if rendered, err := m.renderer.Render(s); err == nil {
			return strings.TrimRight(rendered, "\n") + "\n"
		}
	}
	return wrap(s, m.transcript.Width())
}

// flushLiveText renders accumulated assistant text through glamour and appends
// it to the transcript. Called at KindTurnDone, KindToolCall, and KindError.
func (m *model) flushLiveText() {
	if m.liveText.Len() == 0 {
		return
	}
	raw := m.liveText.String()
	m.liveText.Reset()
	m.live.reset()
	m.lastAssistantText = raw // TQ4: capture for /copy
	m.transcript.Append(m.mdRender(raw))
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

// applySwitchedSession swaps the active session, resetting per-session UI state
// and replaying the loaded transcript.
func (m *model) applySwitchedSession(sess *session.Session) {
	m.cfg.SessionID = sess.ID
	m.cfg.Mode = sess.Mode
	m.slash.SetSession(sess.ID, sess.Mode)

	m.transcript.Reset()
	m.thinkEntries = nil
	m.tools = m.tools[:0]
	m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
	m.cacheReadTokens, m.cacheCreationTokens = 0, 0
	m.tokensEstimated = false
	m.turnCount = 0
	m.changedFiles = nil
	m.teammates = nil
	m.timelineEntries = nil
	m.pendingReadPaths = nil
	m.streaming = false
	m.status = "ready"

	m.transcript.Append(buildWelcomeContent(m.cfg, m.workDir, m.th))
	m.loadHistory(sess.Messages)
	m.followBottom = true
}

// loadHistory replays stored conversation messages into the transcript so a
// resumed session shows its prior turns (user text, assistant prose, and tool
// activity) using the same rendering as a live run.
func (m *model) loadHistory(msgs []provider.Message) {
	toolNames := map[string]string{} // tool_use ID → name, for labelling results
	toolPaths := map[string]string{} // tool_use ID → path, for read_file highlighting (P16.2)
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
				m.transcript.Append(renderToolResult(m.th, name, r.Content, r.IsError, m.transcript.Width(), m.toolMaxLines(), toolPaths[r.ToolUseID]) + "\n")
			}
		case provider.RoleAssistant:
			for _, b := range msg.Content {
				switch v := b.(type) {
				case provider.ThinkingBlock:
					if t := strings.TrimSpace(v.Text); t != "" {
						m.appendThinkingBlock(t, 0) // duration unknown for replayed turns
					}
				case provider.TextBlock:
					if v.Text != "" {
						m.liveText.WriteString(v.Text)
						m.flushLiveText()
					}
				case provider.ToolUseBlock:
					toolNames[v.ID] = v.Name
					if v.Name == "read_file" {
						var inp struct {
							Path string `json:"path"`
						}
						if json.Unmarshal(v.Input, &inp) == nil {
							toolPaths[v.ID] = inp.Path
						}
					}
					m.transcript.Append("\n" + renderToolCall(m.th, v.Name, v.Input, m.transcript.Width()) + "\n")
				}
			}
		}
	}
}

func (m *model) appendUser(text string) {
	// P2.8: record this turn in the timeline before writing anything.
	m.timelineEntries = append(m.timelineEntries, timelineEntry{
		text:       oneLine(text),
		ts:         time.Now(),
		blockIndex: m.transcript.Len(),
	})

	if m.turnCount > 0 {
		sepW := m.transcript.Width() - 2
		if sepW < 10 {
			sepW = 60
		}
		m.transcript.Append(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
	}
	m.turnCount++
	m.transcript.Append(barLabel("You", colUserFg) + "\n" + text + "\n\n")
	m.transcript.Append(barLabel("Assistant", colAssistFg) + "\n")
}

func (m *model) applyEvent(ev api.Event) {
	switch ev.Kind {
	case api.KindThinking:
		// Buffer extended-thinking text; flushed as a collapsible block when
		// the answer (or a tool call) begins. The first token starts the
		// "thought for Ns" clock.
		if m.thinkText.Len() == 0 {
			m.thinkStart = time.Now()
		}
		m.thinkText.WriteString(ev.Text)

	case api.KindText:
		m.flushThinking() // reasoning is done once the answer starts
		// Buffer text in liveText; flushed through glamour at turn end.
		m.liveText.WriteString(ev.Text)

	case api.KindToolCall:
		m.flushThinking()
		m.flushLiveText() // render any preceding prose before the tool line
		m.transcript.Append("\n" + renderToolCall(m.th, ev.Tool, ev.ToolInput, m.transcript.Width()) + "\n")
		m.tools = append(m.tools, toolEntry{name: ev.Tool, status: "pending"})
		if len(m.tools) > maxToolHistory {
			m.tools = m.tools[1:]
		}
		// P2.3 + P2.4: track file access frecency and changed-file list.
		switch ev.Tool {
		case "read_file", "write_file", "edit_file", "multi_edit":
			var inp struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(ev.ToolInput, &inp) == nil && inp.Path != "" {
				if m.fileFrecency == nil {
					m.fileFrecency = make(map[string]int)
				}
				m.fileFrecency[inp.Path]++
				if ev.Tool == "read_file" {
					// FIFO queue matched by KindToolResult below (P16.2): that
					// event carries no path/ID, only the tool name, so pairing
					// relies on call/result ordering per tool name.
					m.pendingReadPaths = append(m.pendingReadPaths, inp.Path)
				} else {
					m.recordChangedFile(inp.Path)
				}
			}
		case "todo_add":
			// TQ7: capture the task text so we can match it when the result arrives.
			var inp struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(ev.ToolInput, &inp) == nil {
				m.pendingTodoText = inp.Text
			}
		}

	case api.KindToolResult:
		path := ""
		if ev.Tool == "read_file" && len(m.pendingReadPaths) > 0 {
			path = m.pendingReadPaths[0]
			m.pendingReadPaths = m.pendingReadPaths[1:]
		}
		m.transcript.Append(renderToolResult(m.th, ev.Tool, ev.ToolResult, ev.ToolIsError, m.transcript.Width(), m.toolMaxLines(), path) + "\n")
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
		// TQ7: update the live todo strip from tool results.
		if !ev.ToolIsError {
			switch ev.Tool {
			case "todo_add":
				var id int
				fmt.Sscanf(ev.ToolResult, "added todo #%d", &id)
				if id > 0 {
					m.todoItems = append(m.todoItems, todoStripItem{id: id, text: m.pendingTodoText, status: "pending"})
					m.pendingTodoText = ""
				}
			case "todo_update":
				var id int
				var status string
				if parts := strings.SplitN(ev.ToolResult, " → ", 2); len(parts) == 2 {
					fmt.Sscanf(parts[0], "todo #%d", &id)
					status = strings.TrimSpace(parts[1])
				}
				if id > 0 && status != "" {
					for i := range m.todoItems {
						if m.todoItems[i].id == id {
							m.todoItems[i].status = status
							break
						}
					}
				}
			case "todo_list":
				m.todoItems = parseTodoList(ev.ToolResult)
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
			m.thinkStart = time.Time{}
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
			pattern:  suggestRulePattern(string(ev.ToolInput)),
		}
		m.status = "approval required"
		m.applyViewportHeight() // dialog height varies with the preview (TQ6)
		m.pendingNotify = &notify.Event{Title: "Aegis", Body: "Approval needed: " + ev.Tool}

	case api.KindSteer:
		// A steering instruction was injected mid-run. Flush any partial model
		// output, show the steer as a user message, then open a new assistant bar
		// so the continuation renders under its own label.
		m.flushThinking()
		m.flushLiveText()
		sepW := max(m.transcript.Width()-2, 10)
		m.transcript.Append(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
		m.transcript.Append(barLabel("You", colUserFg) + "\n" + ev.Text + "\n\n")
		m.transcript.Append(barLabel("Assistant", colAssistFg) + "\n")

	case api.KindGuard:
		// Output validation flagged the answer; surface it as a dim warning.
		m.flushThinking()
		m.flushLiveText()
		m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ output guard: "+ev.Text) + "\n")

	case api.KindCostAlert:
		// Spend crossed the configured alert threshold; surface it as a dim warning.
		m.flushThinking()
		m.flushLiveText()
		m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ "+ev.Text) + "\n")

	case api.KindDone:
		// Flush any buffered text (safety net — normally flushed at KindTurnDone).
		// If the last action was a tool call with no follow-up text, this ensures
		// the transcript is fully rendered before the run is marked complete.
		m.flushLiveText()

	case api.KindError:
		m.flushThinking()
		m.flushLiveText()
		m.approval = nil // clear any pending approval if the run aborts
		m.transcript.Append("\n" + m.th.errLine.Render("error: "+ev.Error) + "\n")
		m.pendingNotify = &notify.Event{Title: "Aegis", Body: "Error: " + truncate(ev.Error, 100)}
	}
}

func (m *model) renderTeammates(msg teammatesMsg) {
	if msg.err != nil {
		m.transcript.Append("\n" + m.th.errLine.Render("teammates: "+msg.err.Error()) + "\n\n")
		return
	}
	if len(msg.items) == 0 {
		m.transcript.Append("\n" + m.th.statusDim.Render("⚇ no sub-agents spawned yet") + "\n\n")
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
	m.transcript.Append(b.String())
}

// --- view ---

// View wraps the rendered content in a tea.View, setting the v2 terminal modes
// (alt-screen, mouse, background) that were previously program options.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = colSurface
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.windowTitle() // P16.1: OSC 0/2, reflects session state
	v.ReportFocus = true            // P16.1: enables tea.FocusMsg/BlurMsg
	return v
}

// windowTitle reflects session state in the terminal title (P16.1) so a
// tabbed-away user can tell streaming/ready/awaiting-approval apart from the
// tab/window list alone.
func (m model) windowTitle() string {
	switch {
	case m.approval != nil:
		return "Aegis — approval needed"
	case m.streaming:
		return "Aegis — working…"
	default:
		return "Aegis — ready"
	}
}

// notifyCmd returns a tea.Cmd that fires the P16.1 attention system for ev —
// bell and/or OS desktop notification per m.notifyMode — or nil if the
// terminal is known to be focused or the mode has nothing to send.
func (m model) notifyCmd(ev notify.Event) tea.Cmd {
	if m.focused {
		return nil
	}
	seq := notify.Sequence(m.notifyMode, ev)
	if seq == "" {
		return nil
	}
	return tea.Raw(seq)
}

// render dispatches to whichever overlay is active. The wizard and
// security-config dialogs are large multi-step forms that still replace the
// frame outright (full-screen makes sense for a form you're filling in);
// everything else — the filterable-list dialogs, help, and quit-confirm — is
// composited over the live chat view via renderOverlay (P16.6) so closing
// them doesn't lose your place.
func (m model) render() string {
	if !m.ready {
		return "initializing…"
	}
	if m.wizard != nil {
		return m.wizard.view()
	}
	if m.securityConfig != nil {
		return m.securityConfig.view()
	}

	base := m.renderChat()
	switch {
	case m.helpOpen:
		return renderOverlay(base, renderHelpBox(m.keys), m.width, m.height)
	case m.quitConfirm:
		return renderOverlay(base, renderQuitConfirmBox(), m.width, m.height)
	case m.dialog != nil:
		return renderOverlay(base, m.dialog.View(), m.width, m.height)
	}
	return base
}

// renderChat renders the normal chat frame: title bar, transcript/sidebar/
// terminal pane, completion popup, approval dialog, todo strip, and input
// area. Split out of render() so overlay dialogs can composite over it
// instead of replacing it (P16.6).
func (m model) renderChat() string {
	titleBar := m.renderTitleBar()
	inputArea := m.renderInputArea()

	main := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().PaddingLeft(1).Render(m.renderTranscriptContent()),
		m.renderScrollbar(),
	)
	var content string
	if m.sidebarOpen && m.width >= sidebarMinTermW {
		sidebar := m.renderSidebar(m.transcript.Height())
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
		parts = append(parts, m.renderApprovalDialog())
	}
	if len(m.todoItems) > 0 {
		parts = append(parts, m.renderTodoStrip())
	}
	parts = append(parts, inputArea)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderTitleBar() string {
	brand := renderBrandMark()
	brandW := lipgloss.Width(brand)

	// P16.5: the scroll-position indicator moved to a rendered scrollbar
	// glyph column beside the transcript (see renderScrollbar) — this bar no
	// longer carries scroll state.
	rightW := max(m.width-brandW, 0)
	right := lipgloss.NewStyle().
		Background(colSurface).
		Foreground(colTextMuted).
		Width(rightW).
		Align(lipgloss.Right).
		Render(m.cfg.Model + " ")

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

	// P2.4: show files edited this session.
	if len(m.changedFiles) > 0 {
		section("FILES")
		for _, f := range m.changedFiles {
			add(m.th.sideValue.Render("✎ " + truncate(filepath.Base(f), w-2)))
		}
		add("")
	}

	// P2.5: show running sub-agents.
	var runningAgents []api.Teammate
	for _, tm := range m.teammates {
		if tm.Status == "running" {
			runningAgents = append(runningAgents, tm)
		}
	}
	if len(runningAgents) > 0 {
		section("AGENTS")
		for _, tm := range runningAgents {
			id := tm.AgentID
			if len(id) > 8 {
				id = id[:8]
			}
			label := "⚇ " + id
			if tm.Summary != "" {
				label += ": " + oneLine(tm.Summary)
			}
			add(m.th.tool.Render(truncate(label, w)))
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
		secs := 0
		if !m.streamStart.IsZero() {
			secs = int(time.Since(m.streamStart).Seconds())
		}
		hint := ""
		if secs > 0 {
			hint = m.th.elapsedDim.Render(fmt.Sprintf(" %ds", secs))
		}
		statusLeft = shimmerText("● "+m.status, m.animStep, colTextMuted, colAccent) + hint
	} else if m.activeToast != nil {
		tag, fg, bg := toastTag(m.activeToast.level)
		statusLeft = statusTag(tag, fg, bg) + " " + m.toastStyle(m.activeToast.level).Render(m.activeToast.message)
	} else {
		statusLeft = statusTag("READY", colBgLess, colSuccess)
	}
	leftW := lipgloss.Width(statusLeft)

	// Right side segments, highest → lowest priority. The loop drops from the
	// tail so lower-value segments disappear first on narrow terminals.
	//   badge (always)  →  hints  →  stats  →  context/agents (sidebar off)  →  cwd
	segs := []string{m.renderModeBadge()}
	segs = append(segs, m.th.statusDim.Render("ctrl+k · f1 · ctrl+e"))
	if stats := m.renderStats(); stats != "" {
		segs = append(segs, m.th.statusDim.Render(stats))
	}
	if !m.sidebarOpen {
		// Fold glanceable sidebar data into the status bar when sidebar is hidden.
		promptTokens := m.inputTokens + m.cacheReadTokens + m.cacheCreationTokens
		if promptTokens > 0 {
			segs = append(segs, renderContextBar(promptTokens, contextWindowFor(m.cfg.Model), 14))
		}
		running := 0
		for _, tm := range m.teammates {
			if tm.Status == "running" {
				running++
			}
		}
		if running > 0 {
			segs = append(segs, m.th.tool.Render(fmt.Sprintf("⚇%d", running)))
		}
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

// toolMaxLines returns the effective per-result line cap based on compact mode.
func (m *model) toolMaxLines() int {
	if m.toolCompact {
		return toolMaxLinesCompact
	}
	return 9999
}

// --- todo strip ---

// renderTodoStrip renders a compact one-line plan progress strip (TQ7).
// Format: ▣▣▢▢ 2/4  → refactor session store
func (m model) renderTodoStrip() string {
	done, inProg, total := 0, 0, len(m.todoItems)
	var activeText string
	for _, it := range m.todoItems {
		switch it.status {
		case "done":
			done++
		case "in_progress":
			inProg++
			if activeText == "" {
				activeText = it.text
			}
		}
	}

	var dots strings.Builder
	for _, it := range m.todoItems {
		switch it.status {
		case "done":
			dots.WriteString(m.th.sideValue.Render("▣"))
		case "in_progress":
			dots.WriteString(m.th.tool.Render("▶"))
		default:
			dots.WriteString(m.th.sideMuted.Render("▢"))
		}
	}

	counter := m.th.sideMuted.Render(fmt.Sprintf(" %d/%d ", done+inProg, total))
	active := ""
	if activeText != "" {
		maxW := max(m.width-total-8, 10)
		active = m.th.statusDim.Render("→ " + truncate(activeText, maxW))
	}
	sep := lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("─", m.width))
	return sep + "\n" + " " + dots.String() + counter + active
}

// parseTodoList parses the formatted output of todo_list into strip items.
func parseTodoList(result string) []todoStripItem {
	var items []todoStripItem
	for _, line := range strings.Split(strings.TrimRight(result, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 5 {
			continue
		}
		var status string
		rest := line
		switch {
		case strings.HasPrefix(line, "[x]"):
			status = "done"
			rest = strings.TrimPrefix(line, "[x]")
		case strings.HasPrefix(line, "[~]"):
			status = "in_progress"
			rest = strings.TrimPrefix(line, "[~]")
		case strings.HasPrefix(line, "[ ]"):
			status = "pending"
			rest = strings.TrimPrefix(line, "[ ]")
		default:
			continue
		}
		rest = strings.TrimSpace(rest)
		var id int
		var text string
		if n, _ := fmt.Sscanf(rest, "%d.", &id); n == 1 {
			if dot := strings.Index(rest, "."); dot >= 0 {
				text = strings.TrimSpace(rest[dot+1:])
			}
		}
		if text == "" {
			text = rest
		}
		items = append(items, todoStripItem{id: id, text: text, status: status})
	}
	return items
}

// --- clipboard ---

// extractCodeBlocks returns the contents of fenced code blocks in text.
func extractCodeBlocks(text string) []string {
	var blocks []string
	var current strings.Builder
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			if inBlock {
				blocks = append(blocks, strings.TrimRight(current.String(), "\n"))
				current.Reset()
				inBlock = false
			} else {
				inBlock = true
			}
			continue
		}
		if inBlock {
			current.WriteString(line + "\n")
		}
	}
	return blocks
}

// copyToClipboardCmd returns a tea.Cmd that copies text to the system clipboard.
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		return clipboardResultMsg{err: copyToClipboard(text)}
	}
}

// copyToClipboard writes text to the platform clipboard using native tools.
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		for _, tool := range [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
			{"wl-copy"},
		} {
			if _, err := exec.LookPath(tool[0]); err == nil {
				cmd = exec.Command(tool[0], tool[1:]...)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-copy)")
		}
	case "windows":
		cmd = exec.Command("clip.exe")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// --- stash (P5.6) ---

// loadStash reads a previously saved draft from path, returning "" on any error.
func loadStash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var v struct {
		Draft string `json:"draft"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	return v.Draft
}

// saveStash persists the draft text to path, silently ignoring errors.
func saveStash(path, draft string) {
	if path == "" {
		return
	}
	draft = strings.TrimSpace(draft)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if draft == "" {
		_ = os.Remove(path)
		return
	}
	data, _ := json.Marshal(struct {
		Draft string `json:"draft"`
	}{Draft: draft})
	_ = os.WriteFile(path, data, 0o600)
}

// --- help overlay ---

// renderHelpBox renders just the keyboard-shortcuts box; render() composites
// it over the chat via renderOverlay (P16.6) rather than placing it on a
// blank frame.
func renderHelpBox(keys keyMap) string {
	entries := keys.helpEntries()

	keyStyle := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Width(14)
	descStyle := lipgloss.NewStyle().Foreground(colTextDim)

	var rows strings.Builder
	for _, e := range entries {
		rows.WriteString(keyStyle.Render(e.Key) + "  " + descStyle.Render(e.Desc) + "\n")
	}

	heading := lipgloss.NewStyle().Foreground(colBrandFg).Bold(true).Render("Keyboard Shortcuts")
	footer := lipgloss.NewStyle().Foreground(colTextMuted).Render("press f1 or esc to close")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(1, 3).
		Width(50).
		Render(heading + "\n\n" + rows.String() + "\n" + footer)
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
	// The markdown style follows the active color scheme (TQ10) so rendered
	// prose matches the lipgloss palette in both dark and light themes.
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(glamourStyleName),
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
