// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	"github.com/fiddler110/aegis/internal/ollamainfo"
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
	// OllamaBaseURL is the native Ollama API base (e.g. "http://localhost:11434")
	// when the provider points at Ollama, else "". Set, it enables the P33.10
	// pre-warm: a keep_alive-neutral load request fired on focus regain or the
	// first keystroke of a new message, gated on /api/ps reporting the model
	// unloaded, so a message sent after Ollama's 5m idle unload no longer opens
	// with a full cold reload.
	OllamaBaseURL string
}

// Run starts the TUI event loop and blocks until the user quits.
func Run(cfg Config) error {
	// Bind the configured color scheme before any styles are built — lipgloss
	// styles capture colors at creation time (TQ10). When the theme is "auto"
	// (P40.5) we can't yet know the terminal background — that arrives async as
	// a tea.BackgroundColorMsg after the program starts — so bind a provisional
	// dark scheme now and correct it in Update once the terminal reports.
	auto := isAutoTheme(cfg.Theme)
	if auto {
		cfg.Theme = applyTheme("dark", cfg.WorkDir)
	} else {
		cfg.Theme = applyTheme(cfg.Theme, cfg.WorkDir)
	}
	// Validate keybinding overrides up front so a typo in config fails fast
	// with a clear error instead of silently doing nothing (P13.3.5).
	if _, err := buildKeyMap(cfg.Keybindings); err != nil {
		return err
	}
	m := newModel(cfg)
	m.autoTheme = auto
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
	// Sidebar geometry. sidebarInnerW is the default content width passed to
	// lipgloss Width(); the rendered block is sidebarInnerW+1 wide (right border
	// char). P40.1 makes the live width per-model (m.sidebarW), adjustable with
	// ctrl+left/ctrl+right; sidebarInnerW is only the starting value.
	sidebarInnerW   = 21
	sidebarTotalW   = 22 // sidebarInnerW + 1 border
	sidebarMinTermW = 88 // terminal width below which sidebar collapses
	sidebarMinW     = 14 // P40.1: min/max adjustable inner width
	sidebarMaxW     = 40

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
	// autoTheme (P40.5) is true when the theme is "auto" (the default): the
	// scheme starts at a provisional dark and is corrected to light/dark once
	// the terminal reports its background color via tea.BackgroundColorMsg.
	// Cleared the moment the user picks an explicit theme with /theme.
	autoTheme bool
	// sidebarW (P40.1) is the sidebar's live inner content width, adjustable
	// with ctrl+left/ctrl+right when the sidebar has focus; starts at
	// sidebarInnerW. The terminal pane's adjustable width lives on m.term.
	sidebarW int

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

	// modelWaitAt is when the current tool round's last result landed and the
	// model was re-invoked with the enlarged prompt — i.e. the start of a
	// post-tool-round model wait (P33.19). Zero while a tool is still running
	// (that is not a model wait) and once the model resumes producing output.
	// It exists because firstTokenAt only marks the *first* wait of a run; on
	// every round after the first the model re-evaluates a now-larger prompt
	// (10-60s of prompt eval on a local model) with firstTokenAt long since
	// set, so without this the status line reads "generating…" through dead
	// air. Set only when pendingTools drains to empty, the one moment the TUI
	// can tell "tool finished, model re-evaluating" from "tool still running".
	modelWaitAt time.Time

	// followBottom tracks whether the viewport should auto-scroll to the newest
	// content. It is true while the user is parked at the bottom and false once
	// they scroll up, so streaming output never yanks them back down mid-read.
	followBottom bool

	// backtrackArmed is true after a first ESC press arms a double-tap action; a
	// second ESC confirms it. Any non-ESC key clears this state. Only the
	// not-streaming path arms it, and only once the input box is already empty
	// (so a plain "clear the input" ESC doesn't arm it): a second ESC there
	// opens the P22.3 backtrack picker. While streaming, ESC interrupts on the
	// first press (P33.5) and never arms.
	backtrackArmed bool

	// warmPinged guards the P33.10 first-keystroke pre-warm so the async warm
	// request fires at most once per empty→typing transition, not on every
	// keystroke. Reset when the composer goes empty again (message sent or
	// cleared); a fresh idle period can then re-warm if the model has unloaded.
	warmPinged bool

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
	// reached a tool round) event resolves them. Each entry carries an origin
	// (P33.15 #3) so an unconsumed system-authored steer (denial feedback,
	// approval.go) can be told apart from a user-typed one when it comes back
	// unconsumed — only the latter is safe to requeue as the next user turn.
	pendingSteers []pendingSteerEntry

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
	// transientPanel is the dismissable, scrollable overlay for informational
	// slash-command output (/status, /help, /memory …) — P33.11. It renders
	// over the live chat and never enters the transcript, so housekeeping
	// commands don't leave stale blocks behind.
	transientPanel *transientPanel
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

// ollamaWarmedMsg reports the outcome of an async pre-warm attempt (P33.10).
// It carries no UI state — the warm-up is a latency optimization the user
// never sees directly — so the Update handler ignores it; the type exists only
// because a tea.Cmd must return a tea.Msg.
type ollamaWarmedMsg struct{}

// maybeWarmOllamaCmd returns a tea.Cmd that pre-warms the Ollama model when it
// makes sense to (P33.10 lever 2), or nil when it doesn't. It is a no-op —
// returns nil — unless the provider is Ollama (OllamaBaseURL set), a concrete
// model is pinned (not "" or the unresolved "auto" sentinel), and no stream is
// in flight (an active run has the model loaded already). The /api/ps
// unloaded-gate lives inside ollamainfo.WarmIfUnloaded, off the UI goroutine.
func (m *model) maybeWarmOllamaCmd() tea.Cmd {
	base := m.cfg.OllamaBaseURL
	model := m.cfg.Model
	if base == "" || model == "" || model == "auto" || m.streaming {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ollamainfo.WarmIfUnloaded(ctx, base, model)
		return ollamaWarmedMsg{}
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

// steerFailedMsg reports a failed steer POST (P33.15 #2). Unlike errMsg —
// which represents an error that ends (or prevents the start of) the main
// stream — a steer POST failing doesn't mean the run itself died: the SSE
// stream this steer was meant to interrupt may well still be live. Routing
// it through its own message type instead of errMsg lets the two be handled
// differently: errMsg tears the whole run's UI state down, steerFailedMsg
// only resolves the one steer attempt that failed.
type steerFailedMsg struct {
	text   string
	origin steerOrigin
	err    error
}

// steerOrigin tags a pendingSteers entry with who authored the steer text,
// so the KindSteerUnconsumed requeue path (P33.15 #3) can tell a user-typed
// steer — safe to requeue as the next user turn — from a system-authored one
// (currently just approval.go's denial-feedback steer) that would be
// misattributed to the user if sent the same way.
type steerOrigin int

const (
	steerOriginUser steerOrigin = iota
	steerOriginDenialFeedback
)

// pendingSteerEntry is one entry in model.pendingSteers.
type pendingSteerEntry struct {
	text   string
	origin steerOrigin
}

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
		sidebarW:     sidebarInnerW, // P40.1: adjustable at runtime

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
	cmds := []tea.Cmd{textarea.Blink, m.sp.Tick, m.fetchStatusInfo(), statusTickCmd()}
	// P40.5: with an "auto" theme, ask the terminal for its background color so
	// Update can pick light vs. dark; the reply arrives as tea.BackgroundColorMsg.
	if m.autoTheme {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
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
	// statusReeval names the post-tool-round wait (P33.19): the round's tools
	// have all returned and the model is re-evaluating the enlarged prompt
	// before it resumes. Deliberately not "cold loading" — whether this wait is
	// a model reload or prompt eval is only knowable from LoadDurationMS, which
	// the native adapter reports post-turn (a KindNotice), never live, so the
	// same indistinguishability that makes the first-token wait unnameable
	// applies here. What *is* measurable is that the tool results just arrived
	// and the model hasn't spoken yet, which is exactly what this says.
	statusReeval = "processing tool results…"
)

// phaseStatus is the status word for the run's current phase.
func (m model) phaseStatus() string {
	switch {
	case m.firstTokenAt.IsZero():
		return statusWaiting
	case !m.modelWaitAt.IsZero():
		return statusReeval
	default:
		return statusGenerating
	}
}

// beginStream marks the start of a run and resets the per-run phase state.
// streamStart is zeroed too so the elapsed readout can't briefly quote the
// previous run's clock in the frames before streamStartedMsg lands.
func (m *model) beginStream() {
	m.streaming = true
	// P33.10: re-arm the first-keystroke pre-warm for the next message. The run
	// starting now loads the model itself, but it may have unloaded again by the
	// time the user composes their next turn.
	m.warmPinged = false
	m.streamStart = time.Time{}
	m.firstTokenAt = time.Time{}
	m.modelWaitAt = time.Time{}
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
	// The model has resumed producing output, so any post-tool-round wait
	// (P33.19) has ended — clear it unconditionally, since a later round sets
	// it afresh from its own last tool result.
	if !m.modelWaitAt.IsZero() {
		m.modelWaitAt = time.Time{}
		m.status = statusGenerating
	}
	m.outBytes += n
}

// streamStats snapshots the in-flight run's throughput for the status line.
// The output side is heuristic today — bytesPerTokenEstimate over the model's
// own output bytes — and says so via estimated. This stays a heuristic even
// after P33.9's native Ollama adapter: verified against Ollama's actual wire
// format, prompt_eval_count/eval_count arrive only on the final done:true
// chunk, not per delta, so there is no real count to assign here mid-stream —
// P33.9's real counts land post-turn instead (KindTurnDone / TurnTrace,
// already IsEstimated=false for this adapter), which is what P33.17's
// inputTokensKnown gate and the sidebar's "last known" panels already read.
// An earlier draft of this comment claimed real per-delta counts would be
// available here; that turned out to be the P33-batch pattern the roadmap's
// own retrospective warns about (see roadmap.md) — verify before trusting a
// diagnosis like that again.
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
// injected into the conversation between tool rounds by the engine. A
// failure reports back as steerFailedMsg rather than errMsg (P33.15 #2) —
// the stream this steer targets may still be live, so a failed POST here
// must not read as "the run died."
func (m model) sendSteerCmd(text string, origin steerOrigin) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cl.Steer(ctx, id, text); err != nil {
			return steerFailedMsg{text: text, origin: origin, err: fmt.Errorf("steer: %w", err)}
		}
		return nil
	}
}

// resolvePendingSteer drops the send-time echo of text once the daemon has
// reported what became of it — injected (KindSteer), handed back
// (KindSteerUnconsumed), or the POST that sent it failed (steerFailedMsg).
// Returns the entry's origin and whether one was actually found, so a caller
// racing another resolution path (e.g. steerFailedMsg arriving after the
// stream already closed and swept pendingSteers itself) can tell it has
// nothing left to do.
func (m *model) resolvePendingSteer(text string) (steerOrigin, bool) {
	for i, st := range m.pendingSteers {
		if st.text == text {
			origin := st.origin
			m.pendingSteers = append(m.pendingSteers[:i], m.pendingSteers[i+1:]...)
			return origin, true
		}
	}
	return steerOriginUser, false
}

// requeueSteer lands a steer the run never injected in the TQ8 queue, so it
// auto-sends as the next user turn when the stream closes. After an explicit
// interrupt it becomes a transcript note instead: sending into a run the user
// just stopped is the surprise TQ8's own queue discard exists to avoid, and
// the text stays on screen either way.
//
// A system-authored steer (steerOriginDenialFeedback) is never requeued as a
// user turn regardless of interrupt state (P33.15 #3): it's system-phrased
// text ("The user denied the X call. Feedback: ...") the model was meant to
// receive as steering context, not a message the user typed, so sending it
// as the next turn would misattribute it. It only gets a note that it wasn't
// delivered.
func (m *model) requeueSteer(text string, origin steerOrigin) {
	if origin == steerOriginDenialFeedback {
		m.transcript.Append(m.th.statusDim.Render("⇢ feedback not delivered: "+oneLine(text)) + "\n\n")
		return
	}
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

// isStreamLifecycleMsg reports whether msg is a stream-run lifecycle event
// that must always reach the main Update switch, never be swallowed by an open
// overlay (P33.20). A transient panel (P33.11) or any future overlay left up
// during a run would otherwise drop the run's streamed output. Kept in sync
// with the equivalent allowlist inside the dialog block.
func isStreamLifecycleMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case streamStartedMsg, eventMsg, batchEventMsg, streamClosedMsg, errMsg, steerFailedMsg:
		return true
	}
	return false
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
			// sidebar consumes m.sidebarW + 1 border; main panel gets the rest
			// minus left pad (P40.1: width is adjustable, was sidebarTotalW).
			vpW = m.width - (m.sidebarW + 1) - 1
		}
		if m.termOpen {
			vpW -= m.term.totalW()
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

// paneResizeStep is how many columns one ctrl+left/ctrl+right press moves a
// pane edge (P40.1).
const paneResizeStep = 2

// resizePane grows (delta>0) or shrinks (delta<0) the focused pane by delta
// columns and re-runs layout. The terminal pane wins when it has focus;
// otherwise the sidebar resizes if it's open and wide enough to show. Returns
// whether anything actually changed (so callers only redraw on a real resize).
func (m *model) resizePane(delta int) bool {
	switch {
	case m.termFocused && m.termOpen:
		if !m.term.setWidth(m.term.width + delta) {
			return false
		}
	case m.sidebarOpen && m.width >= sidebarMinTermW:
		w := max(sidebarMinW, min(m.sidebarW+delta, sidebarMaxW))
		if w == m.sidebarW {
			return false
		}
		m.sidebarW = w
	default:
		return false
	}
	m.layout()
	m.refresh()
	return true
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
	// Like the answer text in mdRender, this is the model's own generated text
	// and is sanitized before it reaches the terminal (P24.20, FIND-17) — the
	// dim styling here is lipgloss, not glamour, so nothing else on this path
	// would strip an embedded control sequence.
	if think := m.thinkText.String(); think != "" {
		think = stripControlSeqs(think)
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
		var phrase, hint string
		switch {
		case m.firstTokenAt.IsZero():
			phrase = statusWaiting
			hint = formatStreamHint(m.streamStats())
		case !m.modelWaitAt.IsZero():
			// P33.19: post-tool-round wait — the round's tools have returned and
			// the model is re-evaluating the enlarged prompt, no output yet. Like
			// the first-token wait this is honest dead air, not a flavor phrase;
			// its clock runs from the last tool result (modelWaitAt), so it times
			// this wait rather than the whole turn.
			phrase = statusReeval
			if secs := int(time.Since(m.modelWaitAt).Seconds()); secs > 0 {
				hint = fmt.Sprintf(" · %ds", secs)
			}
		default:
			cat := catThinking
			if n := len(m.tools); n > 0 && m.tools[n-1].status == "pending" {
				cat = categoryFor(m.tools[n-1].name)
			}
			phrase = thinkingPhrase(m.animStep, m.humorMode, cat)
			hint = formatStreamHint(m.streamStats())
		}
		work := shimmerText("● "+phrase, m.animStep, colTextMuted, colAccent)
		tail.WriteString(wrap(work+m.th.elapsedDim.Render(hint), w))
	}

	// P33.2: a steer is echoed the moment it's sent, so the typed text is
	// visible while the daemon decides whether it lands mid-run or comes back
	// unconsumed — the same dimmed pending treatment TQ8 gives queued messages.
	for _, st := range m.pendingSteers {
		line := m.th.statusDim.Render("⇢ steer ▸ " + truncate(oneLine(st.text), max(w-12, 16)))
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
//
// raw is the model's own generated reasoning — from a live stream via
// flushThinking, or from replayed history via loadHistory — so it is
// sanitized here for the same reason mdRender sanitizes the answer text
// (P24.20, FIND-17). This is the single choke point for settled thinking
// blocks, and it renders through lipgloss rather than glamour, so without
// this an embedded ANSI/OSC sequence would reach the terminal intact.
func (m *model) appendThinkingBlock(raw string, secs int) {
	raw = stripControlSeqs(raw)
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
	s = renderMathUnicode(s) // P40.8: LaTeX math → Unicode before glamour sees it
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

	// P40.1: resize the terminal pane while it has focus.
	if key.Matches(msg, m.keys.PaneNarrower) {
		m.resizePane(-paneResizeStep)
		return nil
	}
	if key.Matches(msg, m.keys.PaneWider) {
		m.resizePane(paneResizeStep)
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
