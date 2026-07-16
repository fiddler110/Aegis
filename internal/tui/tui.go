// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
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
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/session"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// Config configures the TUI.
type Config struct {
	Client         *client.Client
	SessionID      string
	Mode           string
	Model          string
	WorkDir        string
	HumorMode      bool                // D&D-themed thinking phrases; false = plain "thinking…"
	Theme          string              // color scheme name: "dark" (default) or "light" (TQ10)
	Notifications  string              // attention-system mode (P16.1): off/bell/desktop/both
	ImageRendering string              // inline image thumbnails (P16.9): "auto" (default) or "off"
	Keybindings    map[string][]string // P13.3.5: action name -> key sequence overrides
}

// Run starts the TUI event loop and blocks until the user quits.
func Run(cfg Config) error {
	// Bind the configured color scheme before any styles are built — lipgloss
	// styles capture colors at creation time (TQ10).
	cfg.Theme = applyTheme(cfg.Theme, cfg.WorkDir)
	// Validate keybinding overrides up front so a typo in config fails fast
	// with a clear error instead of silently doing nothing (P13.3.5).
	if _, err := buildKeyMap(cfg.Keybindings); err != nil {
		return err
	}
	m := newModel(cfg)
	p := tea.NewProgram(m)
	_, err := p.Run()

	// The daemon client's job ends with the TUI: this is the last consumer
	// of the bearer token before the CLI process exits, so scrub it here
	// (FIND-33/P24.21). Every quit path in this package cancels its
	// in-flight request's context before triggering tea.Quit, so by the
	// time p.Run() returns no goroutine should still be reading the token —
	// but bubbletea does not guarantee a dispatched Cmd goroutine has fully
	// unwound by then, so treat this as best-effort, not a hard guarantee
	// (see the authToken doc comment in internal/client for the full
	// caveat).
	cfg.Client.Zero()
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

// toolCard is the in-place-updating transcript item for one tool call
// (P21.2): appended once at KindToolCall in a pending state, then the same
// item (blk) is mutated via transcriptPane.SetItemRaw to its ok/err
// rendering when the matching KindToolResult arrives — replacing the old
// two-independent-static-blocks approach. call is the pre-rendered call
// block (renderToolCall / renderEditDiff / renderShellCall / etc.),
// computed once and reused for both the pending state (redrawn every
// animation tick — see renderToolCardPending) and the final combined
// render, so neither pays for re-running a diff computation.
// P33.3 adds one earlier state: a card appended at KindToolCallStart, whose
// call block is only the tool's name until the KindToolCall carrying the
// finished arguments reconciles it in place (awaitingCall).
type toolCard struct {
	blk          *transcriptItem
	name         string
	call         string
	awaitingCall bool
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
	imageProto imageProtocol // inline image thumbnail capability (P16.9)

	toolCompact  bool // when true, tool results are capped at toolMaxLinesCompact lines
	tools        []toolEntry
	inputTokens  int // uncached input tokens (last turn)
	outputTokens int
	// inputTokensKnown is false from beginStream() until KindTurnDone reports
	// the current turn's usage. inputTokens itself keeps the previous turn's
	// number across that gap (it feeds the idle sidebar/status readouts, which
	// are fine showing a "last known" figure) but streamStats() must not pass
	// it off as the in-flight turn's prompt size — P33.17: an absent number
	// beats a wrong one, and the live hint has nowhere honest to source a
	// mid-stream prompt size from until the model reports it.
	inputTokensKnown    bool
	cacheReadTokens     int  // prompt-cache hits (last turn)
	cacheCreationTokens int  // prompt-cache writes (last turn)
	tokensEstimated     bool // true when token counts are derived from heuristic
	costUSD             float64
	srvCtxWin           int    // effective context window from daemon /status; 0 = unknown (fall back to name-based guess)
	srvCtxWinSrc        string // provenance: "config", "ollama:loaded", "ollama:modelfile", "ollama:default"

	// Connection/model-health indicator (P28.7): last known daemon /status
	// result, refreshed periodically (see statusTickMsg) rather than only at
	// startup/after a run, so "is the model reachable" is answerable at a
	// glance without spending a prompt on it.
	connKnown     bool  // false until the first /status round trip completes
	connReachable bool  // provider reachable per Server.probeProviderReachability
	connLatencyMS int64 // last measured latency in ms; 0 when unmeasured (cloud provider)

	streamStart time.Time // when the current stream began; zero when idle
	thinkStart  time.Time // when extended thinking began this turn; zero when idle
	turnCount   int       // conversation turns sent; guards turn separator logic
	animStep    int       // frame counter for the streaming "working" shimmer
	humorMode   bool      // when true, D&D phrases replace plain "thinking…"

	// firstTokenAt is when the current run's first model output landed; zero
	// while the run is still in its waiting phase (P33.4). outBytes accumulates
	// the run's model output bytes — text and reasoning both — across the whole
	// run rather than reading liveText, which flushLiveText resets at every tool
	// round and at turn end.
	firstTokenAt time.Time
	outBytes     int

	// followBottom tracks whether the viewport should auto-scroll to the newest
	// content. It is true while the user is parked at the bottom and false once
	// they scroll up, so streaming output never yanks them back down mid-read.
	followBottom bool

	// escPending is true after a first ESC press arms a double-tap action; a
	// second ESC confirms it. Any non-ESC key clears this state. Only the
	// not-streaming path arms it, and only once the input box is already empty
	// (so a plain "clear the input" ESC doesn't arm it): a second ESC there
	// opens the P22.3 backtrack picker. While streaming, ESC interrupts on the
	// first press (P33.5) and never arms.
	escPending bool

	// input history: sent messages oldest-first; histIdx is -1 when not navigating.
	history    []string
	histIdx    int
	draftInput string

	// queued holds messages typed with enter during streaming (TQ8); they
	// render as dimmed pending blocks and auto-send one at a time when the
	// current stream closes. An explicit cancel discards the queue.
	queued []string

	// pendingSteers holds steers posted during the current run that the daemon
	// hasn't reported back on yet (P33.2); they render as dimmed pending blocks
	// until the matching KindSteer (injected) or KindSteerUnconsumed (never
	// reached a tool round) event resolves them.
	pendingSteers []string

	// interrupted is true from an explicit cancel until the next stream starts.
	// A steer the daemon hands back unconsumed after one is surfaced as a note
	// rather than requeued, for the same reason an interrupt discards m.queued.
	interrupted bool

	// Lazily-built workspace file index for @file mention completion.
	fileIndex      []string
	fileIndexBuilt bool

	// Cached command-entry list (built-ins + custom), rebuilt only when the
	// custom-command count changes rather than on every keystroke.
	cmdEntriesCache []cmdEntry
	cmdEntriesLen   int

	// Sidebar visibility (Ctrl+B / /sidebar to toggle, default off).
	sidebarOpen bool

	// rawScrollback (P22.6), when true, releases the terminal's native
	// scrollback/selection/search: the transcript pane renders unclipped
	// (every segment, not just a bounded viewport window) and View() drops
	// alt-screen + mouse capture, so growing conversation content scrolls
	// through the terminal's own history the way plain stdout output would.
	// Sidebar/scrollbar/terminal-pane chrome — which assume a fixed-height
	// dashboard — are suppressed while this is on. /scrollback toggles it;
	// default off, resets on restart (same convention as /tools, /humor).
	rawScrollback bool

	// lastAssistantText holds the most recent complete assistant message for /copy.
	lastAssistantText string

	// lastAnswerBlock is the transcript item holding the most recently flushed
	// assistant answer, kept so a guard-retry event (P25.3) can withdraw the
	// failed answer in place instead of leaving it above its replacement.
	// Nil whenever no withdrawable answer is on screen (run finished, session
	// switched, transcript reset).
	lastAnswerBlock *transcriptItem

	// todo strip — populated from todo_add/todo_update/todo_list tool events.
	todoItems       []todoStripItem
	pendingTodoText string // captured from todo_add call input, matched to result

	// pendingReadPaths maps a pending tool call's card key (see pendingTools)
	// to the read_file path awaiting its KindToolResult, used to
	// chroma-highlight the result body by file extension (P16.2). Keyed by
	// card key (P21.2) rather than a same-name FIFO queue so concurrent
	// read_file calls (engine.runTools runs read tools concurrently) can't
	// cross-attribute paths if their results arrive out of call order.
	pendingReadPaths map[string]string

	// pendingTools holds one addressable transcript-card handle per
	// in-flight tool call (P21.2), keyed by tool_use ID (api.Event.ToolID)
	// so concurrent calls each own their own card. pendingToolOrder tracks
	// insertion order for the fallback path (an event with no ToolID —
	// tests, or a producer that predates it — falls back to the oldest
	// pending card with a matching tool name, the pre-P21.2 FIFO behavior).
	// pendingToolSeq synthesizes a unique key for a call whose event has no
	// ToolID, so two such calls never collide on the same map slot.
	pendingTools     map[string]*toolCard
	pendingToolOrder []string
	pendingToolSeq   int

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

	// lastFailure is the most recent failed command run outside the model's
	// automatic view — the embedded terminal pane or a ! bang command — kept
	// around so the P13.3.1 "diagnose" action has something to send. A
	// shell-tool call the model itself made needs no such bridge: its result
	// already flows back to the model on the very next turn.
	lastFailure *shellFailure

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
	// pendingThreatModelTarget carries the already-parsed /threat-model
	// target text (scope, "" for the whole project) from the moment the
	// framework picker opens through to the follow-up dispatch once a
	// framework is chosen.
	pendingThreatModelTarget string
	helpOpen                 bool
	quitConfirm              bool // P16.6: confirm before quitting while a turn is streaming
	activeToast              *toast
	completion               completionState
	approval                 *approvalState // non-nil while engine is blocked waiting for user approval

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

// batchEventMsg carries one or more streamed events drained together by
// waitForEvent (P21.1). Collapsing a burst of token deltas into a single
// Update — and therefore a single markdown re-render — keeps render cost
// bounded by frame rate rather than token rate. closed is set when the event
// channel closed during the same drain, so the batch and the stream-teardown
// are delivered in one cycle instead of an extra round-trip.
type batchEventMsg struct {
	events []api.Event
	closed bool
}

type streamClosedMsg struct{}
type errMsg struct{ err error }

// bangMsg carries the result of a ! shell command (P2.2).
type bangMsg struct {
	cmd    string
	output string
	code   int
}

// shellFailure captures a failed command run outside the model's automatic
// view, for the P13.3.1 "diagnose" action (see model.lastFailure).
type shellFailure struct {
	source  string // "!" or "terminal", for the synthesized prompt
	command string
	output  string
	code    int
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

// backtrackTargetsMsg carries the P22.3 Esc-Esc picker's candidate list: one
// entry per checkpoint on the current session, paired with that turn's
// verbatim original user message (see fetchBacktrackTargets).
type backtrackTargetsMsg struct {
	items []backtrackItem
	err   error
}

// forkedMsg reports the result of forking the current session (P22.3),
// whether triggered by /fork or by picking an entry from the Esc-Esc
// backtrack picker. prefill, when non-empty, is set on the new session's
// input box so the user can edit the original message before resending —
// only the backtrack-picker path populates it; /fork n leaves it empty and
// just switches sessions.
type forkedMsg struct {
	sess    *session.Session
	title   string
	prefill string
	err     error
}

// statusInfoMsg carries the daemon /status payload; fetched at startup (and
// after runs while the value can still improve) for the effective context
// window driving the usage bar (P23.1), and periodically (P28.7, see
// statusTickMsg) for the connection/model-health indicator.
type statusInfoMsg struct {
	info api.StatusInfo
	err  error
}

// statusRefreshInterval is how often the TUI re-polls GET /status for the
// P28.7 connection/model-health indicator. Cheap enough to poll this often —
// the daemon-side probe (probeProviderReachability) is a 2s-timeout local
// call for Ollama, or a config-only check for a cloud provider — and frequent
// enough that a dropped connection shows up quickly without user action.
const statusRefreshInterval = 20 * time.Second

// statusTickMsg fires statusRefreshInterval after the previous one to
// re-fetch /status in the background (P28.7), independent of the
// after-a-run refresh statusInfoMsg's handler already does for the context
// window.
type statusTickMsg struct{}

// statusTickCmd schedules the next periodic /status refresh.
func statusTickCmd() tea.Cmd {
	return tea.Tick(statusRefreshInterval, func(time.Time) tea.Msg { return statusTickMsg{} })
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
		keys:         mustKeyMap(cfg.Keybindings),
		followBottom: true,
		toolCompact:  true,
		humorMode:    cfg.HumorMode,
		term:         newTermPane(workDir, 10), // height recalculated on first resize
		stashPath:    stashPath,
		notifyMode:   notify.ParseMode(cfg.Notifications),
		imageProto:   imageProtoFor(cfg.ImageRendering),
	}
	// P13.3.5: keep /help's shortcut list in sync with any keybinding remap.
	m.slash.keys = m.keys
	// P5.6: restore an unsent draft if one was saved from the previous session.
	if draft := loadStash(stashPath); draft != "" {
		m.ta.SetValue(draft)
	}
	m.transcript.Append(buildWelcomeContent(cfg, workDir, th))
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.sp.Tick, m.fetchStatusInfo(), statusTickCmd())
}

// fetchStatusInfo pulls the daemon /status payload for the effective context
// window (P23.1) so the usage bar divides by what the model server actually
// honors, not a name-based guess.
func (m model) fetchStatusInfo() tea.Cmd {
	cl := m.cfg.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		info, err := cl.StatusInfo(ctx)
		if err != nil {
			return statusInfoMsg{err: err}
		}
		return statusInfoMsg{info: *info}
	}
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
// bangShellCommand picks the shell binary and argv used to run a `!<command>`
// passthrough, mirroring internal/sandbox.shellCommand and
// internal/security.shellInvocation: PowerShell (preferring "pwsh" via
// sandbox.WindowsShellBinary) on Windows, where a POSIX "sh" is not
// guaranteed to be on PATH, and "/bin/sh -c" elsewhere.
func bangShellCommand(cmd string) (string, []string) {
	if runtime.GOOS == "windows" {
		return sandbox.WindowsShellBinary(), []string{"-NoProfile", "-NonInteractive", "-Command", cmd}
	}
	return "/bin/sh", []string{"-c", cmd}
}

func (m model) execBangCmd(cmd string) tea.Cmd {
	workDir := m.workDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shell, args := bangShellCommand(cmd)
		c := exec.CommandContext(ctx, shell, args...) //nolint:gosec
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

// awaitingPicker reports whether kind's dialog — opened on the keypress with a
// loading row (P33.7) — is still the one on screen, and so is still the right
// place to put the fetch's result. It is false once the user dismissed it with
// esc or moved on to another dialog: late data must fill in what's open, never
// re-open or hijack what isn't.
func (m model) awaitingPicker(kind dialogKind) bool {
	return m.dialog != nil && m.dialog.kind == kind
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

// userMessageText extracts the concatenated text blocks of msgs[idx] if it is
// a user message, or "" otherwise (out-of-range idx, a non-user role, or an
// image/tool-result-only message with no text). Used to recover a checkpoint
// turn's verbatim original prompt: Checkpoint.Label is the same text but
// truncated to 120 runes, so it is only a reliable stand-in for short
// messages — this reads the real message content instead.
func userMessageText(msgs []provider.Message, idx int) string {
	if idx < 0 || idx >= len(msgs) {
		return ""
	}
	msg := msgs[idx]
	if msg.Role != provider.RoleUser {
		return ""
	}
	var sb strings.Builder
	for _, b := range msg.Content {
		if tb, ok := b.(provider.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

// fetchBacktrackTargets loads the P22.3 Esc-Esc picker's candidate list: the
// current session's checkpoints (newest first, one per turn) paired with each
// turn's verbatim user message recovered via userMessageText, falling back to
// the checkpoint's own truncated label if that message can't be found (e.g.
// a pre-P22.3 checkpoint layout edge case).
func (m model) fetchBacktrackTargets() tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cps, err := cl.ListCheckpoints(ctx, id)
		if err != nil {
			return backtrackTargetsMsg{err: err}
		}
		if len(cps) == 0 {
			return backtrackTargetsMsg{}
		}
		sess, err := cl.GetSession(ctx, id)
		if err != nil {
			return backtrackTargetsMsg{err: err}
		}
		items := make([]backtrackItem, 0, len(cps))
		for _, cp := range cps {
			text := userMessageText(sess.Messages, cp.Seq)
			if text == "" {
				text = cp.Label
			}
			items = append(items, backtrackItem{cpID: cp.ID, text: text, createdAt: cp.CreatedAt, fileCount: cp.FileCount})
		}
		return backtrackTargetsMsg{items: items}
	}
}

// forkAndSwitchCmd forks the current session at checkpointID (empty = current
// end of conversation) and loads the resulting session, same shape as
// switchSessionCmd but starting from a Fork call instead of a plain fetch.
// prefill is threaded through to forkedMsg unexamined — see its doc comment.
func (m model) forkAndSwitchCmd(checkpointID, prefill string) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resp, err := cl.Fork(ctx, id, checkpointID)
		if err != nil {
			return forkedMsg{err: err}
		}
		sess, err := cl.GetSession(ctx, resp.SessionID)
		if err != nil {
			return forkedMsg{err: err}
		}
		return forkedMsg{sess: sess, title: resp.Title, prefill: prefill}
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

// shellRefDefaultLines is how many trailing lines of the terminal pane's most
// recent output an unqualified "@shell" token injects (P13.3.2).
const shellRefDefaultLines = 50

// shellRefRe matches "@shell" and "@shell:N" tokens (N = line count). \b
// anchors the end so it doesn't fire inside a longer word like "@shellac".
var shellRefRe = regexp.MustCompile(`@shell(?::(\d+))?\b`)

// extractShellRefs resolves @shell / @shell:N tokens in text into the last N
// lines of the embedded terminal pane's most recent command + output,
// splicing the resolved text in place of each token (unlike @image:, this is
// a textual injection, not an attachment). A missing terminal run never
// fails submission — it substitutes a short placeholder instead.
func extractShellRefs(text string, term termPane) string {
	if !shellRefRe.MatchString(text) {
		return text
	}
	return shellRefRe.ReplaceAllStringFunc(text, func(tok string) string {
		n := shellRefDefaultLines
		if sub := shellRefRe.FindStringSubmatch(tok); sub[1] != "" {
			if v, err := strconv.Atoi(sub[1]); err == nil && v > 0 {
				n = v
			}
		}
		return shellRefText(term, n)
	})
}

// shellRefText renders the resolved text for a single @shell token, mirroring
// the phrasing of the P13.3.1 diagnose-on-failure prompt (tui.go
// diagnoseLastFailureCmd) so the model sees a consistent framing for
// terminal-pane activity it didn't run as a tool call.
func shellRefText(term termPane, n int) string {
	if term.lastCmd == "" {
		return "(no terminal output yet)"
	}
	out := lastNLines(term.lastOutput, n)
	status := "succeeded"
	if term.lastFailed {
		status = fmt.Sprintf("failed with exit code %d", term.lastExitCode)
	}
	return fmt.Sprintf(
		"The following command (run in the terminal pane, not a tool call) %s:\n\n```\n%s\n```\n\nOutput (last %d lines):\n```\n%s\n```",
		status, term.lastCmd, n, out)
}

// lastNLines returns the trailing n newline-delimited lines of s (fewer if s
// has less), with any trailing blank line from a final "\n" trimmed first.
func lastNLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// setQueueMode switches the textarea between normal input and queue mode.
// In queue mode the placeholder and border colour signal that Enter holds the
// draft back as the next user turn instead of sending it now; injecting into
// the running turn is alt+enter, which the placeholder names because it is the
// deliberate action rather than the reflex one (P33.8).
func (m *model) setQueueMode(on bool) {
	styles := m.ta.Styles()
	if on {
		m.ta.Placeholder = "Queue the next message… (alt+enter steers)"
		styles.Focused.Base = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colTextMuted)
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

// dispatchSlash wraps handleSlashCommand, additionally opening the persona
// picker's loading dialog synchronously (P33.13, finishing P33.7) for a bare
// "/persona" — the one slash command whose dispatch is known, before its RPC
// (ListPersonas) even starts, to open a dialog once slashResultMsg lands.
// handleSlashCommand alone can't do this: its value receiver only returns a
// tea.Cmd, with no way to mutate the model ahead of that command actually
// running. Callers that submit a slash command from an addressable model (a
// pointer receiver, or Update's own local value — always addressable) use
// this instead of handleSlashCommand directly. /persona <name> (switching
// directly, no picker) and every other command pass through unchanged.
func (m *model) dispatchSlash(parsed *commands.ParsedCommand) tea.Cmd {
	cmd := m.handleSlashCommand(parsed)
	if parsed.Name != "persona" || len(parsed.Args) != 0 {
		return cmd
	}
	picker := newPersonaPicker(m.width, m.height, m.sp.View())
	m.dialog = &picker
	return tea.Batch(cmd, m.sp.Tick)
}

// The two phases of a streaming run the TUI can actually tell apart (P33.4).
// Everything before the first model output — Ollama reloading a model whose
// keep_alive lapsed, then prompt eval — is one indistinguishable wait from
// here, and is reported as exactly that instead of being guessed at: no event
// on the stream separates model load from prompt eval.
const (
	statusWaiting    = "waiting for first token"
	statusGenerating = "generating…"
)

// phaseStatus is the status word for the run's current phase.
func (m model) phaseStatus() string {
	if m.firstTokenAt.IsZero() {
		return statusWaiting
	}
	return statusGenerating
}

// beginStream marks the start of a run and resets the per-run phase state.
// streamStart is zeroed too so the elapsed readout can't briefly quote the
// previous run's clock in the frames before streamStartedMsg lands.
func (m *model) beginStream() {
	m.streaming = true
	m.streamStart = time.Time{}
	m.firstTokenAt = time.Time{}
	m.outBytes = 0
	m.status = statusWaiting
	// P33.17: the previous turn's inputTokens is now stale for this turn's
	// prompt size — streamStats() must hide the ↑ segment rather than quote it
	// until KindTurnDone reports this turn's real usage.
	m.inputTokensKnown = false
}

// markModelOutput ends the waiting phase and accumulates n output bytes. Any
// evidence of model output counts, not just prose: a run whose first act is a
// tool call reaches this through KindToolCallStart, so the phase line never
// claims the run is still waiting while P33.3's provisional "preparing <tool>…"
// card is on screen saying otherwise.
func (m *model) markModelOutput(n int) {
	if m.firstTokenAt.IsZero() {
		m.firstTokenAt = time.Now()
		m.status = statusGenerating
	}
	m.outBytes += n
}

// streamStats snapshots the in-flight run's throughput for the status line.
// The output side is heuristic today — bytesPerTokenEstimate over the model's
// own output bytes — and says so via estimated. P33.9's native Ollama adapter
// reports real per-delta counts: assigning those to outputToks and clearing
// estimated is the entire change, since nothing above this method knows where
// the numbers came from.
func (m model) streamStats() streamStats {
	st := streamStats{estimated: true}
	// P33.17: inputTokens holds the previous turn's number until this turn's
	// KindTurnDone lands — showing it mid-stream would misrepresent it as the
	// current turn's prompt size, so the ↑ segment stays absent (inputToks
	// zero) rather than quote a stale figure.
	if m.inputTokensKnown {
		st.inputToks = m.inputTokens
	}
	if !m.streamStart.IsZero() {
		st.elapsedSecs = int(time.Since(m.streamStart).Seconds())
	}
	st.outputToks = m.outBytes / bytesPerTokenEstimate
	// Rate over the generation window only. The wait for the first token runs
	// to a minute on a cold local model; averaging it in would report a
	// throughput the model never ran at.
	if !m.firstTokenAt.IsZero() && st.outputToks > 0 {
		if secs := time.Since(m.firstTokenAt).Seconds(); secs >= 1 {
			st.tokPerSec = float64(st.outputToks) / secs
		}
	}
	return st
}

// sendUserMessage appends text as a user turn and starts the stream. Shared by
// the enter/alt+enter key paths and the queued-message drain (TQ8).
func (m *model) sendUserMessage(text string) tea.Cmd {
	m.history = append(m.history, text)
	m.histIdx = -1
	m.draftInput = ""
	cleanText, images := extractImageRefs(text, m.cfg.WorkDir)
	cleanText = extractShellRefs(cleanText, m.term)
	displayText := cleanText
	if displayText == "" && len(images) > 0 {
		suffix := ""
		if len(images) != 1 {
			suffix = "s"
		}
		displayText = fmt.Sprintf("(%d image%s attached)", len(images), suffix)
	}
	m.appendUser(displayText, m.renderImageThumbnails(images))
	m.beginStream()
	m.followBottom = true // jump to the freshly sent message
	// The callers reset the textarea just before this; with DynamicHeight
	// that changes fixedH, so resync the pane height (which also re-pins).
	// Skipped before the first WindowSizeMsg, when m.height is still zero.
	if m.height > 0 {
		m.applyViewportHeight()
	}
	m.refresh()
	return m.startStream(cleanText, images)
}

// diagnoseLastFailureCmd sends the most recent failed !-command or terminal-
// pane command to the model as a new turn, asking it to diagnose and fix the
// failure (P13.3.1). Both surfaces run outside the model's normal view — a
// shell tool call the model makes itself needs no such bridge, since its
// result already flows back to the model on the next turn automatically.
func (m *model) diagnoseLastFailureCmd() tea.Cmd {
	f := m.lastFailure
	if f == nil || m.streaming {
		return nil
	}
	m.lastFailure = nil
	if m.termFocused {
		m.termFocused = false
		m.ta.Focus()
	}
	out := truncate(strings.TrimSpace(f.output), 4000)
	prompt := fmt.Sprintf(
		"The following command (run via %s, not a tool call) failed with exit code %d:\n\n```\n%s\n```\n\nOutput:\n```\n%s\n```\n\nDiagnose the failure and fix it.",
		f.source, f.code, f.command, out)
	return m.sendUserMessage(prompt)
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

// resolvePendingSteer drops the send-time echo of text once the daemon has
// reported what became of it — injected (KindSteer) or handed back
// (KindSteerUnconsumed).
func (m *model) resolvePendingSteer(text string) {
	for i, st := range m.pendingSteers {
		if st == text {
			m.pendingSteers = append(m.pendingSteers[:i], m.pendingSteers[i+1:]...)
			return
		}
	}
}

// requeueSteer lands a steer the run never injected in the TQ8 queue, so it
// auto-sends as the next user turn when the stream closes. After an explicit
// interrupt it becomes a transcript note instead: sending into a run the user
// just stopped is the surprise TQ8's own queue discard exists to avoid, and
// the text stays on screen either way.
func (m *model) requeueSteer(text string) {
	if m.interrupted {
		m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered (interrupted): "+oneLine(text)) + "\n\n")
		return
	}
	m.queued = append(m.queued, text)
}

// maxEventsPerBatch caps how many events a single waitForEvent drain collapses
// into one batchEventMsg. Without a cap a model that streams faster than the
// TUI renders could hand back an unbounded batch and starve input handling; the
// cap yields control back to Update (and thus to key/mouse/tick handling and a
// paint) at a bounded interval while still coalescing the common bursty case.
const maxEventsPerBatch = 512

// waitForEvent blocks for the next streamed event, then non-blockingly drains
// whatever else is already buffered on the channel, returning them as one
// batchEventMsg (P21.1). The blocking first read means an idle stream costs
// nothing; the drain means a fast stream is rendered once per Update rather
// than once per token. A close observed mid-drain is folded into the batch via
// the closed flag so no separate round-trip is needed to tear the stream down.
func waitForEvent(ch <-chan api.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		batch := make([]api.Event, 0, 16)
		batch = append(batch, api.Event(ev))
		for len(batch) < maxEventsPerBatch {
			select {
			case ev, ok := <-ch:
				if !ok {
					return batchEventMsg{events: batch, closed: true}
				}
				batch = append(batch, ev)
			default:
				return batchEventMsg{events: batch}
			}
		}
		return batchEventMsg{events: batch}
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
		// P33.7: keep the loading row's spinner turning. The tick is claimed
		// here rather than left to the spinner.TickMsg case below, which this
		// block returns ahead of, and re-queued only while a fetch is actually
		// outstanding — once the rows land it stops on its own.
		if _, ok := msg.(spinner.TickMsg); ok && m.dialog.loading {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			m.dialog.setLoadingFrame(m.sp.View())
			return m, cmd
		}
		if c, ok := msg.(dialogCancelMsg); ok && c.kind == m.dialog.kind {
			m.dialog = nil
			m.ta.Focus()
			if c.kind == dialogPersonaPicker {
				m.refresh()
			}
			if c.kind == dialogThreatModelPicker {
				m.pendingThreatModelTarget = ""
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
				return m, m.dispatchSlash(parsed)
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
			case dialogThreatModelPicker:
				item := sel.item.(frameworkItem)
				target := m.pendingThreatModelTarget
				m.pendingThreatModelTarget = ""
				args := strings.Fields(item.name) // splits "NIST 800-154" into the two tokens extractThreatModelFramework expects
				if target != "" {
					args = append(args, target)
				}
				parsed := &commands.ParsedCommand{Name: "threat-model", Args: args, Raw: "/threat-model " + strings.Join(args, " ")}
				return m, m.handleSlashCommand(parsed)
			case dialogHistoryPicker:
				item := sel.item.(historyItem)
				// Recall the entry onto the input line for further editing or
				// sending, same as a shell reverse-search accepting a match —
				// it does not send immediately.
				m.ta.SetValue(item.text)
				m.histIdx = -1
				m.draftInput = ""
				return m, nil
			case dialogBacktrackPicker:
				item := sel.item.(backtrackItem)
				// P22.3: fork at that turn's checkpoint and pre-fill its
				// original text so the user edits before resending, rather
				// than the plain "load onto the input line" the history
				// picker above does — the picked entry has already been sent
				// once in the (now-untouched) source session.
				return m, m.forkAndSwitchCmd(item.cpID, item.text)
			}
			return m, nil
		}
		// P33.7: the fetches that fill a picker opened on its loading row
		// belong to their handlers in the main switch below — routing them into
		// the list instead would leave the spinner up forever.
		switch msg.(type) {
		case sessionsLoadedMsg, backtrackTargetsMsg:
		case slashResultMsg:
			// P33.13: the persona picker opens ahead of its data through the
			// same generic slashResultMsg every other slash command uses, so
			// (unlike sessionsLoadedMsg/backtrackTargetsMsg, each dedicated to
			// one picker) it can only fall through here while the dialog on
			// screen is actually the persona picker awaiting it — anything
			// else stays swallowed by the dialog below, same as always.
			if m.dialog.kind != dialogPersonaPicker {
				updated, cmd := m.dialog.Update(msg)
				m.dialog = &updated
				return m, cmd
			}
		default:
			updated, cmd := m.dialog.Update(msg)
			m.dialog = &updated
			return m, cmd
		}
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
				m.updatePendingToolCards() // P21.2: keep pending cards' shimmer live
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
		// falls through to the textarea's own paste handling. The approval
		// dialog owns all input while open (P25.4a), same as it does for
		// tea.KeyMsg below, so a paste can't land silently in the composer.
		if !m.termFocused && m.approval == nil {
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
				m.refresh()
				return m, nil
			case "tab":
				return m, m.acceptCompletion(false)
			case "enter":
				return m, m.acceptCompletion(true)
			}
		}

		// P13.3.1: diagnose the last failed !-command, if any is pending.
		if key.Matches(msg, m.keys.Diagnose) {
			if cmd := m.diagnoseLastFailureCmd(); cmd != nil {
				return m, cmd
			}
		}

		switch msg.String() {
		case "esc", "alt+esc":
			if m.streaming {
				// P33.5: a single ESC cancels the run. Text in the input box
				// still gets cleared first, so there the interrupt is the next
				// press — except when a fast double-tap lands both ESC bytes in
				// the same terminal read, likeliest exactly here while streaming
				// keeps the reader busy re-rendering. Ultraviolet's decoder
				// reports that as one "alt+esc" event instead of two separate
				// "esc" ones, so treat it as the clear plus the confirmed
				// interrupt it was meant to be.
				if strings.TrimSpace(m.ta.Value()) != "" {
					m.ta.Reset()
					if msg.String() != "alt+esc" {
						m.escPending = false
						return m, nil
					}
				}
				// An explicit interrupt also discards any queued messages
				// (TQ8) — auto-sending after the user hit the brakes would be
				// a surprise.
				if m.cancel != nil {
					m.cancel()
				}
				m.escPending = false
				m.queued = nil
				m.interrupted = true
				m.refresh()
				return m, nil
			}
			// Not streaming: an empty input box has nothing to clear, so a
			// genuine second Esc press there opens the P22.3 backtrack
			// picker instead of the no-op this used to be — same double-tap
			// detection as the streaming branch above, including its
			// documented same-frame alt+esc quirk.
			if strings.TrimSpace(m.ta.Value()) == "" {
				if m.escPending || msg.String() == "alt+esc" {
					m.escPending = false
					m.refresh()
					picker := newBacktrackPicker(m.width, m.height, m.sp.View())
					m.dialog = &picker
					return m, tea.Batch(m.fetchBacktrackTargets(), m.sp.Tick)
				}
				m.escPending = true
				m.refresh()
				return m, nil
			}
			m.ta.Reset()
			m.escPending = false
			return m, nil

		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel() // interrupt the in-flight run; press again to quit
				m.escPending = false
				m.queued = nil // TQ8: explicit interrupt discards the queue
				m.interrupted = true
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
		case "ctrl+y":
			if !m.streaming {
				picker := newSessionPicker(m.width, m.height, m.sp.View())
				m.dialog = &picker
				return m, tea.Batch(m.fetchSessions(), m.sp.Tick)
			}
		case "ctrl+r":
			// P22.4: reverse-search over sent-message history, like a shell's
			// Ctrl+R — moved the session switcher to ctrl+y to free this key
			// up for the muscle-memory binding shell users expect.
			if !m.streaming {
				if len(m.history) == 0 {
					t, cmd := newToastCmd("no input history yet", toastInfo)
					m.activeToast = t
					return m, cmd
				}
				m.completion = completionState{}
				picker := newHistoryPicker(m.width, m.height, m.history)
				m.dialog = &picker
				return m, nil
			}
		case "ctrl+l":
			if !m.streaming {
				return m, m.handleSlashCommand(&commands.ParsedCommand{Name: "clear", Raw: "/clear"})
			}
		case "ctrl+k":
			if !m.streaming {
				m.completion = completionState{}
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
				// TQ8: while the model is running, Enter queues the draft as the
				// next user turn; it auto-sends when the current run finishes.
				text := strings.TrimSpace(m.ta.Value())
				if text == "" {
					return m, nil
				}
				m.ta.Reset()
				m.escPending = false
				m.queued = append(m.queued, text)
				m.followBottom = true
				m.applyViewportHeight() // ta was just Reset; resync pane height
				m.refresh()
				return m, nil
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
				return m, m.dispatchSlash(parsed)
			}
			m.ta.Reset()
			return m, m.sendUserMessage(text)

		case "alt+enter":
			// P33.8: while streaming, alt+enter injects the draft as a steering
			// message between tool rounds instead of queueing it. When idle it
			// behaves like a plain send.
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			m.ta.Reset()
			if m.streaming {
				m.escPending = false
				m.pendingSteers = append(m.pendingSteers, text)
				m.followBottom = true
				m.applyViewportHeight() // ta was just Reset; resync pane height
				m.refresh()
				return m, m.sendSteerCmd(text)
			}
			return m, m.sendUserMessage(text)
		}

	case streamStartedMsg:
		m.events = msg.ch
		m.cancel = msg.cancel
		m.streamStart = time.Now()
		m.escPending = false
		m.interrupted = false
		m.setQueueMode(true)
		return m, tea.Batch(waitForEvent(m.events), m.sp.Tick)

	case eventMsg:
		// Single-event path (kept for direct drivers such as the integration
		// tests); the live stream arrives as batchEventMsg. Both share
		// applyStreamBatch so the follow-bottom and notify bookkeeping stay
		// identical.
		notifyCmd := m.applyStreamBatch([]api.Event{api.Event(msg)})
		return m, tea.Batch(waitForEvent(m.events), notifyCmd)

	case batchEventMsg:
		notifyCmd := m.applyStreamBatch(msg.events)
		if msg.closed {
			// The stream closed within this same drain: run the exact
			// teardown the dedicated streamClosedMsg path does, on the
			// already-applied state, and carry any per-event notification
			// (e.g. a cost alert) alongside the closed path's own command.
			nm, closeCmd := m.Update(streamClosedMsg{})
			return nm, tea.Batch(notifyCmd, closeCmd)
		}
		return m, tea.Batch(waitForEvent(m.events), notifyCmd)

	case streamClosedMsg:
		m.flushThinking()
		m.flushLiveText() // safety: in case KindTurnDone wasn't the last event
		// P21.2: the universal safety net for a stuck-pending tool card — the
		// stream can close without KindError or KindDone at all on a
		// client-initiated cancel (engine.ErrInterrupted's callers return
		// before emitting anything), so this is the one place guaranteed to
		// run after every kind of run end. A no-op if KindError already
		// resolved everything.
		m.resolveStuckToolCards()
		m.streaming = false
		m.events = nil
		m.cancel = nil
		m.status = "ready"
		m.escPending = false
		m.setQueueMode(false)
		m.transcript.Append("\n")
		// P33.2: a steer the daemon never reported a verdict on — an older
		// daemon that doesn't emit KindSteerUnconsumed at all, or an event the
		// SSE buffer dropped — is unconsumed by definition once the stream is
		// gone, so treat it as such instead of leaving its echo dangling.
		for _, st := range m.pendingSteers {
			m.requeueSteer(st)
		}
		m.pendingSteers = nil
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
		m.setQueueMode(false)
		m.transcript.Append(m.th.errLine.Render("error: "+msg.err.Error()) + "\n\n")
		// TQ8: don't auto-send into a failing session.
		if len(m.queued) > 0 {
			m.queued = nil
			m.transcript.Append(m.th.statusDim.Render("⏳ queued messages discarded after error") + "\n\n")
		}
		for _, st := range m.pendingSteers {
			m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered: "+oneLine(st)) + "\n\n")
		}
		m.pendingSteers = nil
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
			// P13.3.1: a ! command never reaches the model automatically —
			// offer the same diagnose bridge the terminal pane gets.
			m.lastFailure = &shellFailure{source: "!", command: msg.cmd, output: msg.output, code: msg.code}
			m.transcript.Append(m.th.statusDim.Render(m.keys.Diagnose.Help().Key+" to ask Aegis to diagnose this") + "\n")
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

	case statusInfoMsg:
		// Silent: a daemon that predates /status context fields (or an
		// unreachable one) just leaves the name-based fallback in place.
		if msg.err == nil && msg.info.ContextWindow > 0 {
			m.srvCtxWin = msg.info.ContextWindow
			m.srvCtxWinSrc = msg.info.ContextWindowSource
		}
		// P28.7 connection/model-health indicator: a request error means the
		// daemon itself is unreachable, distinct from the daemon being up but
		// reporting its configured provider as unreachable.
		m.connKnown = true
		if msg.err != nil {
			m.connReachable, m.connLatencyMS = false, 0
		} else {
			m.connReachable = msg.info.ProviderReachable
			m.connLatencyMS = msg.info.ProviderLatencyMS
		}
		return m, nil

	case statusTickMsg:
		return m, tea.Batch(m.fetchStatusInfo(), statusTickCmd())

	case sessionsLoadedMsg:
		if !m.awaitingPicker(dialogSessionPicker) {
			if msg.err != nil {
				t, cmd := newToastCmd("sessions: "+msg.err.Error(), toastError)
				m.activeToast = t
				return m, cmd
			}
			return m, nil
		}
		if msg.err != nil {
			return m, m.dialog.setNotice("sessions: " + msg.err.Error())
		}
		if len(msg.items) == 0 {
			return m, m.dialog.setNotice("no sessions to switch to")
		}
		return m, m.dialog.setItems(sessionPickerItems(msg.items), sessionPickerH(m.height, len(msg.items)))

	case sessionSwitchedMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("switch: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		m.applySwitchedSession(msg.sess)
		m.refresh()
		return m, nil

	case backtrackTargetsMsg:
		if !m.awaitingPicker(dialogBacktrackPicker) {
			if msg.err != nil {
				t, cmd := newToastCmd("backtrack: "+msg.err.Error(), toastError)
				m.activeToast = t
				return m, cmd
			}
			return m, nil
		}
		if msg.err != nil {
			return m, m.dialog.setNotice("backtrack: " + msg.err.Error())
		}
		if len(msg.items) == 0 {
			return m, m.dialog.setNotice("no checkpoints yet — send a message first")
		}
		return m, m.dialog.setItems(backtrackPickerItems(msg.items), backtrackPickerH(m.height, len(msg.items)))

	case forkedMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("fork: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		m.applySwitchedSession(msg.sess)
		if msg.prefill != "" {
			// P22.3: hand the original message back for editing rather than
			// resending it verbatim — the whole point of backtracking.
			m.ta.SetValue(msg.prefill)
		}
		t, cmd := newToastCmd(fmt.Sprintf("Forked into %q — edit and send to continue.", msg.title), toastInfo)
		m.activeToast = t
		m.refresh()
		return m, cmd

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
		if m.term.lastFailed {
			// P13.3.1: bridge the failure to the model on request — the
			// terminal pane's output never reaches it automatically.
			m.lastFailure = &shellFailure{
				source:  "terminal",
				command: m.term.lastCmd,
				output:  m.term.lastOutput,
				code:    m.term.lastExitCode,
			}
		}
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
			// P33.13: the picker already opened (in its loading state) the
			// moment "/persona" was dispatched — populate it in place rather
			// than opening a second one. Not awaiting means the user dismissed
			// it (or moved on to another dialog) before this landed: drop it,
			// same as the session/backtrack pickers' late-data handling.
			if m.awaitingPicker(dialogPersonaPicker) {
				return m, m.dialog.setItems(personaPickerItems(msg.Personas), personaPickerH(m.height, len(msg.Personas)))
			}
			return m, nil
		}
		if m.awaitingPicker(dialogPersonaPicker) {
			// The bare "/persona" dispatch that opened the loading dialog
			// came back with nothing to list (msg.Output alone) or failed
			// (msg.IsError) — report it inside the dialog the user is already
			// looking at instead of as a transcript line below.
			return m, m.dialog.setNotice(msg.Output)
		}
		if msg.Models != nil {
			picker := newModelPicker(m.width, m.height, msg.Models, m.cfg.Model)
			m.dialog = &picker
			return m, nil
		}
		if msg.ThreatModelTarget != nil {
			picker := newThreatModelFrameworkPicker(m.width, m.height)
			m.dialog = &picker
			m.pendingThreatModelTarget = *msg.ThreatModelTarget
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
		if msg.SwitchToSession != "" {
			// P22.3: /fork created a genuinely different session (not just a
			// truncated version of this one) — load it the same way Ctrl+Y's
			// session picker does, rather than ReloadSession's "refetch this
			// same id" path above.
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
			cmds = append(cmds, m.switchSessionCmd(msg.SwitchToSession))
			return m, tea.Batch(cmds...)
		}
		if msg.Output == "\x00clear" {
			m.transcript.Reset()
			m.lastAnswerBlock = nil
			m.thinkEntries = nil
			m.tools = m.tools[:0]
			m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
			m.inputTokensKnown = false
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
		if msg.Output == "\x00scrollback-on" {
			return m, m.setRawScrollbackCmd(true)
		}
		if msg.Output == "\x00scrollback-off" {
			return m, m.setRawScrollbackCmd(false)
		}
		if msg.Output == "\x00scrollback-toggle" {
			return m, m.setRawScrollbackCmd(!m.rawScrollback)
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
		if strings.HasPrefix(msg.Output, "\x00diff\n") {
			// P22.1: chroma-highlight the raw git diff text here, where m.th
			// (the active theme) is available — the dispatcher that produced
			// it has no theme reference, same reason /theme and /clear pass
			// through a \x00 marker instead of pre-rendering.
			diffText := strings.TrimPrefix(msg.Output, "\x00diff\n")
			rendered := diffText
			if lines, ok := highlightUnifiedDiff(m.th, diffText); ok {
				rendered = strings.Join(lines, "\n")
			}
			m.transcript.Append(rendered + "\n")
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
			m.appendUser(msg.Message, nil)
			m.beginStream()
			m.followBottom = true
			m.refresh()
			return m, tea.Batch(m.startStream(msg.Message, nil), m.sp.Tick)
		}
		m.refresh()
		return m, nil
	}

	// The approval dialog owns all input while open (P25.4a): tea.KeyMsg
	// already returns above before reaching here, but this guard covers
	// every other message type too so no future case can leak a keystroke,
	// paste, or other input into the composer out from under the dialog.
	if m.approval == nil {
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
			// Any non-ESC key while escPending clears the not-streaming
			// double-tap-to-backtrack arm state (P22.3) — the ESC case already
			// returns early and manages escPending itself.
			if m.escPending {
				m.escPending = false
				m.refresh()
			}
		}
	}
	switch tmsg := msg.(type) {
	case tea.KeyMsg:
		// P21.7: while the textarea owns typed input, only the dedicated page
		// keys scroll the transcript. Forwarding every key (the old "known
		// existing quirk") meant typing any 'u'/'k'/'b'/space — or pressing the
		// arrow keys to edit the draft — both edited the text AND scrolled the
		// transcript, which silently killed auto-follow mid-stream. The vi-style
		// scroll keys still work where the textarea isn't capturing input (the
		// approval dialog's fall-through path in approval.go).
		switch tmsg.String() {
		case "pgup", "pgdown":
			if m.transcript.HandleKey(tmsg) {
				m.followBottom = m.transcript.AtBottom()
			}
		}
	case tea.MouseWheelMsg:
		if m.transcript.HandleMouseWheel(tmsg) {
			m.followBottom = m.transcript.AtBottom()
		}
	case tea.MouseClickMsg:
		cmds = append(cmds, m.handleMouseClick(tmsg))
	case tea.MouseMotionMsg:
		m.handleMouseMotion(tmsg)
	case tea.MouseReleaseMsg:
		cmds = append(cmds, m.handleMouseRelease(tmsg))
	}
	// P21.7: followBottom is user intent — paused by an explicit scroll away
	// from the bottom, resumed by scrolling back to it (both handled above, at
	// the scroll inputs themselves) or by sending/queueing a message. It is
	// deliberately NOT re-derived from geometry here on every message: any
	// layout perturbation (approval dialog, textarea growth) briefly makes
	// AtBottom() read false, and a blanket re-derivation would
	// turn that into a permanently dead auto-follow with no user scroll having
	// happened.
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
// Height budget: title(1) + content(vpH) + textarea+border(ta.Height()+2) + belowBar(1).
// The completion popup (P33.18) is composited over this layout rather than
// reserving space in it — see render()'s anchored-overlay compositing.
func (m *model) layout() {
	vpW := m.width - 1 // -1 for PaddingLeft on the main panel
	if m.rawScrollback {
		// P22.6: raw scrollback mode suppresses the sidebar, terminal pane,
		// and scrollbar column (renderChat/renderScrollbar) — none of that
		// dashboard-column width is reserved, so the plain transcript text
		// gets the full body width instead.
	} else {
		if m.sidebarOpen && m.width >= sidebarMinTermW {
			// sidebar consumes sidebarTotalW; main panel gets the rest minus left pad
			vpW = m.width - sidebarTotalW - 1
		}
		if m.termOpen {
			vpW -= termPaneTotalW
		}
		vpW -= 1 // scrollbar column (P16.5), rendered to the right of the transcript
	}
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
// belowBar, plus optional strips. The completion popup (P33.18) is
// composited over the finished layout instead of reserved here — see
// renderCompletionPopup and render()'s anchored-overlay compositing.
func (m *model) fixedH() int {
	h := 1 + m.ta.Height() + 2 + 1
	if len(m.todoItems) > 0 {
		h += 1 // todo strip: one line
	}
	return h
}

// applyViewportHeight resizes the transcript pane to fit the current fixed budget.
//
// P22.6: in raw scrollback mode the pane height tracks the content's own
// total height instead of the terminal window, so View() (transcript.go)
// renders every segment with nothing clipped — the frame genuinely grows
// turn over turn instead of redrawing the same fixed-height region in place,
// which is what lets bubbletea's non-alt-screen renderer scroll old lines
// through the terminal's real history (see View()'s doc comment on model).
func (m *model) applyViewportHeight() {
	if m.rawScrollback {
		m.transcript.SetSize(m.transcript.Width(), m.transcript.TotalHeight())
	} else {
		m.transcript.SetSize(m.transcript.Width(), max(m.height-m.fixedH(), 3))
	}
	// P21.7: a height change moves the bottom edge out from under the pinned
	// offset. While following, re-pin immediately so a pane shrink (approval
	// dialog, textarea wrap) never leaves the newest content below the fold.
	if m.followBottom {
		m.transcript.GotoBottom()
	}
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
// value. The popup composites over the finished layout (P33.18) rather than
// resizing the viewport, so opening/closing it no longer perturbs the
// transcript pane.
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
		m.refresh()
	}
}

// setRawScrollbackCmd applies the P22.6 raw-scrollback toggle: flips
// m.rawScrollback, re-runs layout (View()'s AltScreen/MouseMode and
// applyViewportHeight's clipped-vs-unclipped transcript height both key off
// it) and refresh, and reports the new state as a transcript status line
// (the same "\x00foo-on/off" -> status-line pattern /humor and /theme use).
func (m *model) setRawScrollbackCmd(on bool) tea.Cmd {
	m.rawScrollback = on
	m.layout()
	var msg string
	if on {
		msg = "Raw scrollback mode: on — plain text, native terminal scroll/select/search restored. Sidebar and terminal pane hidden."
	} else {
		msg = "Raw scrollback mode: off — normal dashboard restored."
	}
	m.transcript.Append(m.th.statusText.Render(msg) + "\n\n")
	m.refresh()
	return nil
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
		return m.dispatchSlash(&commands.ParsedCommand{Name: e.name, Raw: "/" + e.name})
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
		rendered := m.live.render(w, m.mdRender)
		if m.streaming {
			// P21.3: a blinking caret at the true write-head, so streaming
			// reads as "alive" rather than "redrawing". mdRender/glamour
			// normalize their output to end in exactly one trailing "\n";
			// strip it, land the caret glyph directly after the last
			// rendered character, then restore exactly one trailing "\n" so
			// SetTail's own newline-enforcement doesn't double up or orphan
			// the caret on its own blank line. Never baked into the
			// persisted transcript — flushLiveText re-renders from raw
			// liveText, not from this tail string.
			rendered = strings.TrimRight(rendered, "\n") + m.caretGlyph() + "\n"
		}
		tail.WriteString(rendered)
	} else if m.streaming {
		// P33.4: a flavor phrase describes what the model is doing, which is
		// only knowable once it has started doing it. Before the first output
		// the honest line is the wait itself and how long it has run.
		phrase := statusWaiting
		if !m.firstTokenAt.IsZero() {
			cat := catThinking
			if n := len(m.tools); n > 0 && m.tools[n-1].status == "pending" {
				cat = categoryFor(m.tools[n-1].name)
			}
			phrase = thinkingPhrase(m.animStep, m.humorMode, cat)
		}
		hint := formatStreamHint(m.streamStats())
		work := shimmerText("● "+phrase, m.animStep, colTextMuted, colAccent)
		tail.WriteString(wrap(work+m.th.elapsedDim.Render(hint), w))
	}

	// P33.2: a steer is echoed the moment it's sent, so the typed text is
	// visible while the daemon decides whether it lands mid-run or comes back
	// unconsumed — the same dimmed pending treatment TQ8 gives queued messages.
	for _, st := range m.pendingSteers {
		line := m.th.statusDim.Render("⇢ steer ▸ " + truncate(oneLine(st), max(w-12, 16)))
		tail.WriteString("\n" + wrap(line, w))
	}

	// TQ8: queued messages render as dimmed pending blocks below the live tail.
	for _, q := range m.queued {
		line := m.th.statusDim.Render("⏳ queued ▸ " + truncate(oneLine(q), max(w-12, 16)))
		tail.WriteString("\n" + wrap(line, w))
	}

	m.transcript.SetTail(tail.String())
	if m.rawScrollback {
		// P22.6: refresh() runs after nearly every transcript mutation
		// (append, tool-result update, tail rebuild) — re-sync the pane
		// height to the content's own total here too, not just on resize
		// (applyViewportHeight), so a raw-mode pane never falls behind
		// newly appended content and clips it.
		m.transcript.SetSize(m.transcript.Width(), m.transcript.TotalHeight())
	}
	if m.followBottom {
		m.transcript.GotoBottom()
	}
}

// caretBlinkPeriod is the number of animStep ticks (driven by the existing
// spinner tick — see spinner.TickMsg handling — one tick per ~100ms) in one
// full blink cycle of the streaming caret: half on, half off. No dedicated
// ticker is introduced; this reuses the tick that already drives the
// "thinking" shimmer.
const caretBlinkPeriod = 8

// caretChar is the block character used for the P21.3 streaming write-head
// caret. Deliberately a full block (█) rather than the left-half block (▌)
// already used elsewhere in this TUI as the crush-style message-bar accent
// (see "▌ You" / "▌ Assistant" headers) — reusing that glyph here would make
// the caret visually and programmatically indistinguishable from those bars.
const caretChar = "█"

// caretGlyph returns the styled caret glyph for the current animation frame,
// or "" on the "off" half of the blink cycle.
func (m *model) caretGlyph() string {
	if m.animStep%caretBlinkPeriod < caretBlinkPeriod/2 {
		return m.th.caret.Render(caretChar)
	}
	return ""
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
//
// s is the model's own generated text, so it is sanitized with
// stripControlSeqs before either path sees it (P24.20, FIND-17): an
// unsanitized ANSI/OSC sequence embedded in adversarial model output (e.g.
// reproduced verbatim via a prompt-injection vector) could otherwise
// manipulate the terminal — cursor repositioning, hidden text, or
// OSC-based clipboard/title-bar tricks — once it reached the terminal
// either through glamour's output or the plain-wrap fallback.
func (m *model) mdRender(s string) string {
	s = stripControlSeqs(s)
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
	// AppendBlock rather than Append so a guard-retry event can withdraw this
	// answer in place (P25.3); nil when the render was empty or the block was
	// immediately trimmed out.
	m.lastAnswerBlock = m.transcript.AppendBlock(m.mdRender(raw))
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

	// P13.3.1: diagnose the terminal pane's last failed command, if any.
	if m.term.lastFailed && key.Matches(msg, m.keys.Diagnose) {
		return m.diagnoseLastFailureCmd()
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
		m.term.beginRun(cmd)
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
	m.lastAnswerBlock = nil
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
	m.pendingTools = nil
	m.pendingToolOrder = nil
	m.streaming = false
	m.status = "ready"
	m.lastFailure = nil

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
			var imageBlocks []provider.ImageBlock
			for _, b := range msg.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ToolResultBlock:
					results = append(results, v)
				case provider.ImageBlock:
					imageBlocks = append(imageBlocks, v)
				}
			}
			if len(results) == 0 {
				if len(imageBlocks) > 0 {
					suffix := ""
					if len(imageBlocks) != 1 {
						suffix = "s"
					}
					note := fmt.Sprintf("🖼 %d image%s", len(imageBlocks), suffix)
					if text != "" {
						text += "  " + note
					} else {
						text = "(" + note + ")"
					}
				}
				if text != "" {
					m.appendUser(text, m.renderImageThumbnailsFromBlocks(imageBlocks))
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

// renderImageThumbnails reads each attached image's local file and renders
// an inline thumbnail (P16.9) for the ones that decode successfully;
// unreadable paths and unsupported formats (e.g. WebP) are skipped rather
// than surfaced as an error — the existing "(N images attached)" text notice
// already covers that case.
func (m *model) renderImageThumbnails(images []api.ImageInput) []string {
	if m.imageProto == protocolNone || len(images) == 0 {
		return nil
	}
	var out []string
	for _, img := range images {
		data, err := os.ReadFile(img.Path)
		if err != nil {
			continue
		}
		if raw := renderImageThumbnail(data, m.imageProto); raw != "" {
			out = append(out, raw)
		}
	}
	return out
}

// renderImageThumbnailsFromBlocks is renderImageThumbnails for replayed
// session history (loadHistory), where images arrive as base64-encoded
// provider.ImageBlock content rather than local file paths.
func (m *model) renderImageThumbnailsFromBlocks(blocks []provider.ImageBlock) []string {
	if m.imageProto == protocolNone || len(blocks) == 0 {
		return nil
	}
	var out []string
	for _, b := range blocks {
		data, err := base64.StdEncoding.DecodeString(b.Data)
		if err != nil {
			continue
		}
		if raw := renderImageThumbnail(data, m.imageProto); raw != "" {
			out = append(out, raw)
		}
	}
	return out
}

// appendUser appends a user turn, followed by any rendered image thumbnails
// (P16.9, already-rendered ANSI blocks — see renderImageThumbnails /
// renderImageThumbnailsFromBlocks) and the "Assistant" bar that opens the
// reply.
func (m *model) appendUser(text string, thumbnails []string) {
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
	for _, thumb := range thumbnails {
		m.transcript.AppendRaw(thumb)
	}
	m.transcript.Append(barLabel("Assistant", colAssistFg) + "\n")
}

// applyStreamBatch applies a run of streamed events and refreshes the view
// exactly once (P21.1), so a burst of token deltas costs one markdown
// re-render instead of one per token. It returns a notification command if any
// applied event set one. Both the single-event (eventMsg) and coalesced
// (batchEventMsg) paths funnel through here so their bookkeeping is identical.
func (m *model) applyStreamBatch(evs []api.Event) tea.Cmd {
	// P18.3/P21.7: resume-on-return-to-bottom, one-way. If the pre-batch
	// scroll position is at the bottom, (re-)arm follow — checked before
	// applyEvent grows the transcript, since the content this batch appends
	// would itself make AtBottom() read false afterward. Never cleared here:
	// pausing follow is exclusively an explicit user scroll (wheel-up/pgup),
	// so a mid-stream geometry change (completion popup, approval dialog,
	// textarea growing a line — all of which shrink the pane and briefly
	// falsify AtBottom) can no longer silently kill auto-follow.
	if m.transcript.AtBottom() {
		m.followBottom = true
	}
	sawDone := false
	for _, ev := range evs {
		m.applyEvent(ev)
		if ev.Kind == api.KindDone {
			sawDone = true
		}
	}
	m.refresh()
	var cmds []tea.Cmd
	if m.pendingNotify != nil {
		cmds = append(cmds, m.notifyCmd(*m.pendingNotify))
		m.pendingNotify = nil
	}
	// A finished run may have improved the daemon's context-window detection
	// (first run loads the model into Ollama); re-fetch until authoritative.
	if sawDone && m.srvCtxWinSrc != "config" && m.srvCtxWinSrc != "ollama:loaded" {
		cmds = append(cmds, m.fetchStatusInfo())
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
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
		m.markModelOutput(len(ev.Text))
		m.thinkText.WriteString(ev.Text)

	case api.KindText:
		m.flushThinking() // reasoning is done once the answer starts
		// Buffer text in liveText; flushed through glamour at turn end.
		m.markModelOutput(len(ev.Text))
		m.liveText.WriteString(ev.Text)

	case api.KindToolCallStart:
		// P33.3: the model has named the tool but is still generating its
		// arguments — frequently the longest phase of a turn on a local
		// model, and one the shimmer phrase alone used to cover. The card
		// goes up now and the KindToolCall below fills in the arguments in
		// place. Only a repeat of a tool_use ID already on screen is dropped:
		// two starts can legitimately share an empty ID (the OpenAI wire
		// format can name a call in an earlier delta than it IDs it).
		if ev.Tool == "" {
			break
		}
		if c := m.pendingTools[ev.ToolID]; ev.ToolID != "" && c != nil {
			break
		}
		m.markModelOutput(0)
		m.flushThinking()
		m.flushLiveText() // render any preceding prose before the tool line
		key := m.toolCardKey(ev.ToolID)
		call := "\n" + renderToolCardStartCall(m.th, ev.Tool)
		if blk := m.transcript.AppendBlock(renderToolCardStart(m.th, call, m.animStep)); blk != nil {
			m.trackPendingTool(key, &toolCard{blk: blk, name: ev.Tool, call: call, awaitingCall: true})
		}

	case api.KindToolCall:
		// A daemon that emits no KindToolCallStart at all still leaves the
		// waiting phase here rather than at the tool's result.
		m.markModelOutput(0)
		m.flushThinking()
		m.flushLiveText() // render any preceding prose before the tool line
		// P21.2: a tool call gets one addressable card, appended pending and
		// later mutated in place to ok/err (see KindToolResult below) instead
		// of appending a second, independent block for the result. key
		// prefers the real tool_use ID so concurrent calls (engine.runTools
		// runs read/network tools concurrently) each own their own card; the
		// synthetic fallback only matters for an event with no ToolID (older
		// producers, or hand-built test events).
		call := "\n" + renderToolCall(m.th, ev.Tool, ev.ToolInput, m.transcript.Width())
		key, reconciled := m.reconcileStartedToolCard(ev, call)
		if !reconciled {
			key = m.toolCardKey(ev.ToolID)
			if blk := m.transcript.AppendBlock(renderToolCardPending(m.th, call, m.animStep)); blk != nil {
				m.trackPendingTool(key, &toolCard{blk: blk, name: ev.Tool, call: call})
			}
		}
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
					// Keyed by the same card key as pendingTools above (P21.2)
					// rather than a same-name FIFO queue, so concurrent
					// read_file calls can't cross-attribute paths if their
					// results arrive out of call order.
					if m.pendingReadPaths == nil {
						m.pendingReadPaths = make(map[string]string)
					}
					m.pendingReadPaths[key] = inp.Path
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
		card, key := m.resolveToolCard(ev)
		path := ""
		if key != "" {
			if p, ok := m.pendingReadPaths[key]; ok {
				path = p
				delete(m.pendingReadPaths, key)
			}
		}
		if card != nil {
			m.transcript.SetItemRaw(card.blk, renderToolCardDone(m.th, card.call, ev.Tool, ev.ToolResult, ev.ToolIsError, m.transcript.Width(), m.toolMaxLines(), path))
		} else {
			// No matching pending card (e.g. a result event replayed or
			// synthesized without a preceding KindToolCall in this session) —
			// fall back to appending it as its own item rather than
			// silently dropping the result.
			m.transcript.Append(renderToolResult(m.th, ev.Tool, ev.ToolResult, ev.ToolIsError, m.transcript.Width(), m.toolMaxLines(), path) + "\n")
		}
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
			m.inputTokensKnown = true
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
		// Blur the composer so its cursor stops implying it's the thing
		// listening (P25.4a) — the approval dialog is the only visual and
		// input focus target until it's answered.
		m.ta.Blur()
		m.pendingNotify = &notify.Event{Title: "Aegis", Body: "Approval needed: " + ev.Tool}

	case api.KindSteer:
		// A steering instruction was injected mid-run. Flush any partial model
		// output, show the steer as a user message, then open a new assistant bar
		// so the continuation renders under its own label.
		m.resolvePendingSteer(ev.Text)
		m.flushThinking()
		m.flushLiveText()
		sepW := max(m.transcript.Width()-2, 10)
		m.transcript.Append(m.th.turnSep.Render(strings.Repeat("─", sepW)) + "\n")
		m.transcript.Append(barLabel("You", colUserFg) + "\n" + ev.Text + "\n\n")
		m.transcript.Append(barLabel("Assistant", colAssistFg) + "\n")

	case api.KindSteerUnconsumed:
		// The run ended without the steer ever reaching a tool round (P33.2).
		m.resolvePendingSteer(ev.Text)
		m.requeueSteer(ev.Text)

	case api.KindGuard:
		m.flushThinking()
		m.flushLiveText()
		switch {
		case ev.GuardRetrying:
			// The engine is about to replace this answer with a corrective
			// retry (P25.3): withdraw the failed answer in place so the retry
			// renders as *the* answer, not as a second one below it.
			note := m.th.elapsedDim.Render("⚠ output guard: answer withdrawn ("+ev.Text+") — retrying…") + "\n\n"
			if m.lastAnswerBlock != nil {
				m.transcript.SetItemRaw(m.lastAnswerBlock, note)
				m.lastAnswerBlock = nil
				m.lastAssistantText = ""
			} else {
				m.transcript.Append(note)
			}
		case ev.Text != "":
			// Terminal validation failure (retries exhausted, answer surfaced
			// anyway); surface it as a dim warning.
			m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ output guard: "+ev.Text) + "\n")
		}
		// A pass event (empty Text) needs no transcript line at all.

	case api.KindCostAlert:
		// Spend crossed the configured alert threshold; surface it as a dim warning.
		m.flushThinking()
		m.flushLiveText()
		m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ "+ev.Text) + "\n")

	case api.KindNotice:
		// Engine advisory (context fill, compaction, step limit) — same dim
		// warning treatment as cost alerts.
		m.flushThinking()
		m.flushLiveText()
		m.transcript.Append("\n" + m.th.elapsedDim.Render("⚠ "+ev.Text) + "\n")

	case api.KindDone:
		// Flush any buffered text (safety net — normally flushed at KindTurnDone).
		// If the last action was a tool call with no follow-up text, this ensures
		// the transcript is fully rendered before the run is marked complete.
		m.flushLiveText()
		m.lastAnswerBlock = nil // the surfaced answer is final; nothing left to withdraw

	case api.KindError:
		m.flushThinking()
		m.flushLiveText()
		m.lastAnswerBlock = nil
		if m.approval != nil { // clear any pending approval if the run aborts
			m.approval = nil
			if !m.termFocused {
				m.ta.Focus()
			}
		}
		m.transcript.Append("\n" + m.th.errLine.Render("error: "+ev.Error) + "\n")
		m.pendingNotify = &notify.Event{Title: "Aegis", Body: "Error: " + truncate(ev.Error, 100)}
		// P21.2: a turn that errors mid-round can leave a concurrently-run
		// tool's card stuck pending (its own KindToolResult may simply never
		// arrive — see runTools/runToolsSequential, which stop emitting
		// further events once the round is abandoned). Resolve it here
		// rather than leaving it stuck; streamClosedMsg is the same
		// safety net for the interrupt path, which reaches neither
		// KindError nor KindDone (engine.ErrInterrupted returns silently).
		m.resolveStuckToolCards()
	}
}

// resolveToolCard looks up and removes the pending card matching a
// KindToolResult event (P21.2), returning it and the card key it was
// registered under (also the key pendingReadPaths uses) so the caller can
// pop that too. Prefers an exact tool_use-ID match (set on every
// live-engine-emitted event — see engine.Event.ToolID); falls back to the
// oldest pending card with a matching tool name for an event with no ID
// (the pre-P21.2 FIFO behavior), which keeps history-replay-shaped and
// hand-built test events working unchanged. Returns (nil, "") if nothing
// matches.
func (m *model) resolveToolCard(ev api.Event) (*toolCard, string) {
	if ev.ToolID != "" {
		if c, ok := m.pendingTools[ev.ToolID]; ok {
			m.removePendingTool(ev.ToolID)
			return c, ev.ToolID
		}
		return nil, ""
	}
	for _, k := range m.pendingToolOrder {
		if c := m.pendingTools[k]; c != nil && c.name == ev.Tool {
			m.removePendingTool(k)
			return c, k
		}
	}
	return nil, ""
}

// toolCardKey returns the pendingTools key for a tool event: its real
// tool_use ID, or a synthesized one for an event that carries none (P21.2).
func (m *model) toolCardKey(toolID string) string {
	if toolID != "" {
		return toolID
	}
	m.pendingToolSeq++
	return fmt.Sprintf("#%d", m.pendingToolSeq)
}

// trackPendingTool registers a freshly-appended card under key.
func (m *model) trackPendingTool(key string, c *toolCard) {
	if m.pendingTools == nil {
		m.pendingTools = make(map[string]*toolCard)
	}
	m.pendingTools[key] = c
	m.pendingToolOrder = append(m.pendingToolOrder, key)
}

// reconcileStartedToolCard folds a KindToolCall's rendered arguments into the
// provisional card its KindToolCallStart already put on screen (P33.3),
// returning the key the card ends up registered under. reconciled is false
// when there is no such card — a daemon that never emits KindToolCallStart,
// an adapter whose provider doesn't announce tool calls early, or a start
// event the SSE buffer dropped — and the caller appends a card as it always
// has, so nothing here can produce a second card for one call.
//
// Identity mirrors resolveToolCard's: an exact tool_use-ID match first, then
// the oldest still-provisional card of the same tool name. The fallback is
// load-bearing rather than legacy-only — the OpenAI wire format may name a
// tool call in an earlier delta than the one carrying its ID, so a start
// event can be ID-less even when the call that follows it isn't. Both events
// are emitted in stream order, so the i-th start of a name pairs with the
// i-th call of it.
func (m *model) reconcileStartedToolCard(ev api.Event, call string) (string, bool) {
	card, key := m.startedToolCard(ev)
	if card == nil {
		return "", false
	}
	card.call = call
	card.awaitingCall = false
	m.transcript.SetItemRaw(card.blk, renderToolCardPending(m.th, card.call, m.animStep))
	if ev.ToolID != "" && ev.ToolID != key {
		m.rekeyPendingTool(key, ev.ToolID)
		key = ev.ToolID
	}
	return key, true
}

// startedToolCard finds the still-provisional card belonging to ev and the
// key it is registered under, or (nil, "").
func (m *model) startedToolCard(ev api.Event) (*toolCard, string) {
	if c := m.pendingTools[ev.ToolID]; ev.ToolID != "" && c != nil && c.awaitingCall {
		return c, ev.ToolID
	}
	for _, k := range m.pendingToolOrder {
		if c := m.pendingTools[k]; c != nil && c.awaitingCall && c.name == ev.Tool {
			return c, k
		}
	}
	return nil, ""
}

// rekeyPendingTool re-registers a pending card under newKey without
// disturbing its position in pendingToolOrder. A card appended at
// KindToolCallStart is keyed by whatever the start event carried — a
// synthetic key when the provider hadn't assigned the tool_use ID that early
// — while the KindToolResult that eventually arrives looks it up by the real
// ID (P33.3).
func (m *model) rekeyPendingTool(oldKey, newKey string) {
	c := m.pendingTools[oldKey]
	if c == nil {
		return
	}
	delete(m.pendingTools, oldKey)
	m.pendingTools[newKey] = c
	for i, k := range m.pendingToolOrder {
		if k == oldKey {
			m.pendingToolOrder[i] = newKey
			break
		}
	}
}

// removePendingTool drops key from both pendingTools and pendingToolOrder.
// The order slice is small (bounded by maxParallelTools concurrent calls in
// one round), so a linear scan to remove it is cheap.
func (m *model) removePendingTool(key string) {
	delete(m.pendingTools, key)
	for i, k := range m.pendingToolOrder {
		if k == key {
			m.pendingToolOrder = append(m.pendingToolOrder[:i], m.pendingToolOrder[i+1:]...)
			break
		}
	}
}

// resolveStuckToolCards finalizes every still-pending tool card to a
// terminal "interrupted" state (P21.2). Called wherever a run can end
// without producing a KindToolResult for every KindToolCall it started —
// KindError above, and streamClosedMsg (the universal safety net: it fires
// for every run end, including a client-initiated cancel or a budget/loop
// abort, none of which necessarily emit any terminal event at all — see
// engine.ErrInterrupted's callers, which return before emitting anything).
// A no-op when nothing is pending.
func (m *model) resolveStuckToolCards() {
	if len(m.pendingTools) == 0 {
		return
	}
	for _, k := range m.pendingToolOrder {
		if c := m.pendingTools[k]; c != nil {
			m.transcript.SetItemRaw(c.blk, renderToolCardStuck(m.th, c.call))
		}
		delete(m.pendingReadPaths, k)
	}
	m.pendingTools = nil
	m.pendingToolOrder = nil
}

// updatePendingToolCards refreshes every still-pending tool card's shimmer
// frame in place (P21.2), driven by the same animation tick that already
// advances animStep for the "● thinking…" status line and the streaming
// write-head caret (P21.3) — so a long-running tool call visibly keeps
// working instead of sitting static. A no-op when nothing is pending.
func (m *model) updatePendingToolCards() {
	for _, c := range m.pendingTools {
		if c.awaitingCall {
			m.transcript.SetItemRaw(c.blk, renderToolCardStart(m.th, c.call, m.animStep))
			continue
		}
		m.transcript.SetItemRaw(c.blk, renderToolCardPending(m.th, c.call, m.animStep))
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
//
// P22.6: rawScrollback drops both alt-screen and mouse capture. Alt-screen
// alone isn't the blocker for native terminal scrollback — bubbletea's
// non-alt-screen renderer already resizes its frame to the content height and
// lets genuinely new lines scroll through the terminal's own history (see
// cursed_renderer.go's flush: "the frame height can change based on the
// content... different from the alt screen buffer, which has a fixed
// height"). What actually defeats it in this app's normal mode is that the
// transcript is a bounded, in-place-redrawn viewport (transcriptPane) that
// clips to a fixed visible window regardless of alt-screen — see
// applyViewportHeight's rawScrollback branch, which is the other half of this
// mode: it renders every segment unclipped so the frame truly grows. Mouse
// capture is released too, since MouseModeCellMotion alone is enough to stop
// a terminal emulator from offering its own click-drag text selection.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = !m.rawScrollback
	v.BackgroundColor = colSurface
	if m.rawScrollback {
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion
	}
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
	if m.completion.active {
		// P33.18: the completion popup used to insert into the vertical layout
		// and shrink the transcript pane by its own height, the same reflow
		// jump P33.6 fixed for the approval dialog. Unlike that dialog it is
		// non-modal and anchored (the user is still typing behind it, not
		// looking at a centered form), so it composites via
		// renderAnchoredOverlay — no centering, no dimming — positioned just
		// above the composer instead of the screen center.
		popup, x, y := m.renderCompletionPopup()
		base = renderAnchoredOverlay(base, popup, x, y, m.width, m.height)
	}
	if m.approval != nil {
		// P33.6: the approval prompt used to sit between transcript and input,
		// shrinking the pane by its own height every time the engine asked —
		// the loudest layout jump in the normal flow. Compositing it leaves the
		// transcript's geometry alone; modality is unchanged, since the
		// composer was already blurred while one is pending (P25.4a).
		base = renderOverlay(base, m.renderApprovalDialog(), m.width, m.height)
	}
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
// terminal pane, todo strip, and input area. Split out of render() so overlay
// dialogs — and, since P33.18, the completion popup — composite over it
// instead of being laid out inline.
func (m model) renderChat() string {
	titleBar := m.renderTitleBar()
	inputArea := m.renderInputArea()

	var content string
	if m.rawScrollback {
		// P22.6: no scrollbar column (nothing to indicate — the terminal owns
		// scroll position) and no sidebar/terminal pane (both assume a
		// fixed-height dashboard next to a bounded transcript; here the
		// transcript's own height is unbounded and grows with content, so
		// joining a fixed-height column beside it would either misalign or,
		// for the sidebar's Height()-padded block, emit thousands of blank
		// lines). Plain sequential text gets the full body width instead.
		content = lipgloss.NewStyle().PaddingLeft(1).Render(m.renderTranscriptContent())
	} else {
		main := lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().PaddingLeft(1).Render(m.renderTranscriptContent()),
			m.renderScrollbar(),
		)
		if m.sidebarOpen && m.width >= sidebarMinTermW {
			sidebar := m.renderSidebar(m.transcript.Height())
			if m.termOpen {
				content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main, m.term.view(m.th, m.termFocused, m.keys.Diagnose.Help().Key))
			} else {
				content = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
			}
		} else {
			if m.termOpen {
				content = lipgloss.JoinHorizontal(lipgloss.Top, main, m.term.view(m.th, m.termFocused, m.keys.Diagnose.Help().Key))
			} else {
				content = main
			}
		}
	}

	parts := []string{titleBar, content}
	if len(m.todoItems) > 0 {
		parts = append(parts, m.renderTodoStrip())
	}
	parts = append(parts, inputArea)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderCompletionPopup renders the inline completion popup and the (x, y)
// screen position render() should composite it at (P33.18): left-aligned,
// bottom-anchored just above the composer — above the todo strip too, when
// one is showing, matching the popup's old position in the vertical layout
// before it moved to compositing. Only meaningful when m.completion.active;
// callers must check that first.
func (m model) renderCompletionPopup() (popup string, x, y int) {
	popupW := min(m.width-2, 72)
	popup = lipgloss.NewStyle().PaddingLeft(1).Render(m.completion.view(popupW))

	inputAreaH := m.ta.Height() + 2 + 1 // border(2) + belowBar(1), mirrors fixedH()
	todoH := 0
	if len(m.todoItems) > 0 {
		todoH = 1
	}
	y = m.height - inputAreaH - todoH - lipgloss.Height(popup)
	return popup, 0, y
}

func (m model) renderTitleBar() string {
	brand := renderBrandMark()
	brandW := lipgloss.Width(brand)

	// P16.5: the scroll-position indicator moved to a rendered scrollbar
	// glyph column beside the transcript (see renderScrollbar) — this bar no
	// longer carries scroll state.
	rightW := max(m.width-brandW, 0)
	// P28.7: a colored dot (+ latency once measured) ahead of the model name
	// gives an always-visible, glance-only connection/model-health signal —
	// no /status command or wasted "is the model connected" prompt needed.
	right := lipgloss.NewStyle().
		Background(colSurface).
		Foreground(colTextMuted).
		Width(rightW).
		Align(lipgloss.Right).
		Render(m.renderConnBadge(colSurface) + " " + m.cfg.Model + " ")

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
	add(m.renderConnDetail()) // P28.7: reachable/unreachable + latency at a glance
	add("")

	if m.streaming && !m.streamStart.IsZero() {
		if m.firstTokenAt.IsZero() {
			section("WAITING")
		} else {
			section("GENERATING")
		}
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
		add(renderContextBar(promptTokens, m.contextWindowSize(), w))
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
	if m.approval != nil {
		// P25.4a: the composer is blurred while the dialog is open (no
		// blinking cursor down here) — spell out where input goes instead of
		// leaving that to be inferred from the missing cursor alone.
		statusLeft = lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⏸ respond to the approval dialog")
	} else if !m.streaming && m.escPending {
		// P22.3: armed by a first ESC press on an already-empty input box;
		// a second press opens the backtrack picker.
		statusLeft = lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render("⚠  ESC again to backtrack")
	} else if m.streaming {
		// P33.4: the transcript tail loses its hint the moment live text starts
		// flowing, so the status bar carries the token/throughput readout for
		// the whole run — on a local model the rate is the vital sign, and it
		// is worth least when it disappears at the first token.
		hint := m.th.elapsedDim.Render(formatStreamHint(m.streamStats()))
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
			segs = append(segs, renderContextBar(promptTokens, m.contextWindowSize(), 14))
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

// connColor picks the P28.7 connection-indicator color: muted while the
// first /status round trip is still in flight, green when the daemon
// reports its configured provider reachable, red otherwise.
func (m model) connColor() color.Color {
	switch {
	case !m.connKnown:
		return colTextMuted
	case m.connReachable:
		return colSuccess
	default:
		return colError
	}
}

// renderConnBadge renders the compact P28.7 connection/model-health glyph
// used in the always-visible title bar: a colored dot, plus a latency
// suffix once one has been measured. bg must match the enclosing
// Background() so the nested style's reset doesn't leave a stray
// mismatched-background segment on the line.
func (m model) renderConnBadge(bg color.Color) string {
	dot := lipgloss.NewStyle().Foreground(m.connColor()).Background(bg).Bold(true).Render("●")
	if m.connKnown && m.connReachable && m.connLatencyMS > 0 {
		dot += lipgloss.NewStyle().Foreground(colTextMuted).Background(bg).Render(fmt.Sprintf(" %dms", m.connLatencyMS))
	}
	return dot
}

// renderConnDetail renders the fuller P28.7 connection-status line for the
// sidebar's MODEL section: reachable/unreachable/checking, plus latency once
// measured (0/unmeasured for a cloud provider, where reachability is just an
// API-key-present check — see Server.probeProviderReachability).
func (m model) renderConnDetail() string {
	style := lipgloss.NewStyle().Foreground(m.connColor())
	switch {
	case !m.connKnown:
		return style.Render("◌ checking…")
	case m.connReachable && m.connLatencyMS > 0:
		return style.Render(fmt.Sprintf("● reachable · %dms", m.connLatencyMS))
	case m.connReachable:
		return style.Render("● reachable")
	default:
		return style.Render("● unreachable")
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

// contextWindowSize returns the context window the usage bar divides by: the
// daemon's effective value (config or Ollama-detected, from /status) when
// known, else the name-based guess. The distinction matters most for local
// models — contextWindowFor guesses 128k for "gemma4:12b" while Ollama may be
// serving 4k, making the bar read 3% at the moment the prompt starts silently
// truncating.
func (m model) contextWindowSize() int {
	if m.srvCtxWin > 0 {
		return m.srvCtxWin
	}
	return contextWindowFor(m.cfg.Model)
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
