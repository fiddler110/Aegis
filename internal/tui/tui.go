// Package tui implements the terminal client. It connects to the daemon,
// streams engine events for each turn, and renders the conversation in a
// multi-panel dashboard layout.
package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/ollamainfo"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sandbox"
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
	Mouse          string              // mouse capture (P74.19): "on" (default) or "off"
	ReducedMotion  bool                // disable shimmer/caret-blink/card animation (P74.10)
	// MaxTurnStall is cost.max_turn_stall (P39.17) — the same bound the engine
	// aborts a run against. The TUI never enforces it; it only reads it to ramp
	// the P74.11 stall shimmer toward colWarning as a wait approaches it. Zero
	// (stall bound off) disables the ramp — see stallRampColor.
	MaxTurnStall time.Duration
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
	// (FIND-33/P24.21). Every quit path in this package cancels both the
	// in-flight request's context (m.cancel) and any running interactive-
	// terminal command's context (m.termRun.cancel, P76.2) before triggering
	// tea.Quit, so by the
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

// streamPhase (P77.2) groups the current run's streaming-phase state: when
// the run began, when the model's first output landed, how many output bytes
// it has produced so far, and when the most recent post-tool-round model
// re-invocation started. It feeds phaseStatus (the status-line phase word)
// and stallElapsed (the P74.11 stall-ramp color) and is reset at the start of
// every run (model.beginStream) and zeroed at turn end.
type streamPhase struct {
	// streamStart is when the current stream began; zero when idle.
	streamStart time.Time

	// firstTokenAt is when the current run's first model output landed; zero
	// while the run is still in its waiting phase (P33.4). outBytes
	// accumulates the run's model output bytes — text and reasoning both —
	// across the whole run rather than reading liveText, which flushLiveText
	// resets at every tool round and at turn end.
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
}

// toolState (P77.2) groups the in-flight and resolved tool-call tracking
// state: one addressable transcript-card handle per in-flight call, the
// P74.4 read/search grouping machinery, and the P75.1 registry of
// independently expandable resolved results.
type toolState struct {
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

	// activeReadGroup is the open P74.4 collapsed card that the next
	// resolving read_file/grep/glob success can extend, or nil when nothing
	// is currently extendable. soloReadCard/soloReadEntry remember the most
	// recently finalized *ungrouped* successful read/search card so the very
	// next resolving groupable call can detect it is positionally adjacent
	// (transcriptPane.ItemBefore) and promote both into a new two-member
	// group. See model.foldIntoReadGroup (stream.go) for the merge rule and
	// why plain pointer-identity adjacency is enough to keep a genuinely
	// out-of-order parallel-round result from ever joining a group its
	// result hasn't actually confirmed yet.
	activeReadGroup *toolGroup
	soloReadCard    *transcriptItem
	soloReadEntry   groupEntry

	// toolBlocks (P75.1) is every resolved tool result/group in transcript
	// order, each independently expandable — the registry the keyboard
	// toggle (toggleLastToolBlock) addresses. A solo card that later
	// upgrades into a two-member group (foldIntoReadGroup) replaces its own
	// entry here in place (trackToolBlock) rather than adding a second one,
	// since both share the same transcript item. A result with no matching
	// pending card (session replay, or a producer that skips
	// KindToolCallStart/KindToolCall) is appended to the transcript directly
	// and never tracked here — out of scope for this first, keyboard-only
	// slice; mouse click-to-expand's hit-testing pass is the natural place
	// to widen this if replayed history needs it too.
	toolBlocks []toolBlock
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
	inputTokensKnown bool
	// displayedInputTokens/displayedOutputTokens (P74.12) ease toward
	// inputTokens/outputTokens one animStep frame at a time instead of
	// snapping, so the status bar's counter climbs smoothly rather than
	// jumping in chunk-sized steps each time a turn's usage lands. See
	// easeStatCounters in update_tick.go.
	displayedInputTokens  int
	displayedOutputTokens int
	cacheReadTokens       int  // prompt-cache hits (last turn)
	cacheCreationTokens   int  // prompt-cache writes (last turn)
	tokensEstimated       bool // true when token counts are derived from heuristic
	costUSD               float64
	egressBytes           int64  // P81.8: cumulative web_fetch bytes this session, server-reported
	srvCtxWin             int    // effective context window from daemon /status; 0 = unknown (fall back to name-based guess)
	srvCtxWinSrc          string // provenance: "config", "ollama:loaded", "ollama:modelfile", "ollama:default", "ollama:compat-default"

	// Connection/model-health indicator (P28.7): last known daemon /status
	// result, refreshed periodically (see statusTickMsg) rather than only at
	// startup/after a run, so "is the model reachable" is answerable at a
	// glance without spending a prompt on it.
	connKnown     bool  // false until the first /status round trip completes
	connReachable bool  // provider reachable per Server.probeProviderReachability
	connLatencyMS int64 // last measured latency in ms; 0 when unmeasured (cloud provider)

	thinkStart time.Time // when extended thinking began this turn; zero when idle
	turnCount  int       // conversation turns sent; guards turn separator logic
	animStep   int       // frame counter for the streaming "working" shimmer
	humorMode  bool      // when true, D&D phrases replace plain "thinking…"

	// phase (P77.2) groups the current run's streaming-phase state — see
	// streamPhase's own doc comment.
	phase streamPhase

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

	// mouseOff (P74.19), when true, releases mouse capture while keeping
	// alt-screen — the combination rawScrollback can't give you, since that
	// releases both. Set once from Config.Mouse ("off") at startup; unlike
	// rawScrollback there is no runtime toggle, since the tradeoff (no wheel
	// scroll, no click-to-focus) is meant to be a considered per-session
	// choice, not a keystroke. View() reads this directly.
	mouseOff bool

	// reducedMotion (P74.10), when true, freezes animStep instead of advancing
	// it on every spinner tick — shimmerText, caretGlyph and thinkingPhrase all
	// read animStep, so freezing it freezes them without a separate check at
	// each call site. Set once from Config.ReducedMotion at startup. pollTick
	// is a second, independent tick counter for background polls (currently
	// the P2.5 sub-agent roster fetch) that must keep their cadence even when
	// animStep is frozen.
	reducedMotion bool
	pollTick      int

	// maxTurnStall (P74.11) mirrors Config.MaxTurnStall — the engine's
	// MaxTurnStall abort bound — so the stall shimmer can ramp toward
	// colWarning as the current wait approaches it. Zero disables the ramp.
	maxTurnStall time.Duration

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

	// toolState (P77.2) groups the in-flight/resolved tool-call tracking
	// state — see toolState's own doc comment.
	toolState toolState

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
	// pendingThreatModelUnattended carries the P52.12 `unattended` flag across
	// the same gap. Without it the flag is parsed, then thrown away the moment
	// the picker opens, and "/threat-model unattended" — the no-framework form,
	// which is the common one — silently runs interactively instead.
	pendingThreatModelUnattended bool
	helpOpen                     bool
	quitConfirm                  bool // P16.6: confirm before quitting while a turn is streaming
	activeToast                  *toast
	completion                   completionState
	approval                     *approvalState // non-nil while engine is blocked waiting for user approval

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

	// search (P40.3) is non-nil while the incremental transcript-search overlay
	// is active: it captures keyboard input, greps the transcript's rendered
	// content, and drives match navigation. See search.go.
	search *searchState
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
		cfg:        cfg,
		ta:         ta,
		sp:         sp,
		th:         th,
		status:     "ready",
		slash:      NewSlashDispatcher(cfg.Client, cfg.SessionID, cfg.Mode, cfg.Model, cfg.WorkDir),
		histIdx:    -1,
		focusedIdx: -1,
		workDir:    workDir,
		sidebarW:   sidebarInnerW, // P40.1: adjustable at runtime

		transcript:    newTranscriptPane(80, 24), // initial size; resized on first WindowSizeMsg
		liveText:      &strings.Builder{},
		live:          &liveBlock{},
		thinkText:     &strings.Builder{},
		renderer:      newGlamourRenderer(80), // initial width; recreated on first resize
		keys:          mustKeyMap(cfg.Keybindings),
		followBottom:  true,
		toolCompact:   true,
		humorMode:     cfg.HumorMode,
		term:          newTermPane(workDir, 10), // height recalculated on first resize
		stashPath:     stashPath,
		notifyMode:    notify.ParseMode(cfg.Notifications),
		imageProto:    imageProtoFor(cfg.ImageRendering),
		mouseOff:      strings.EqualFold(strings.TrimSpace(cfg.Mouse), "off"),
		reducedMotion: cfg.ReducedMotion,
		maxTurnStall:  cfg.MaxTurnStall,
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

// fetchCmd is the shared shape behind the package's simple single-call tea.Cmd
// constructors (P77.4): open a context.WithTimeout, make one client call, wrap
// the result in a message. Only the calls that are genuinely a single round
// trip use this — fetchBacktrackTargets/forkAndSwitchCmd (a second dependent
// call plus branching) and startStream/startDrive (context.WithCancel, not a
// timeout, since the returned cancel keeps the stream alive) don't fit the
// shape and stay literal.
func fetchCmd[T any](timeout time.Duration, fn func(context.Context) (T, error), wrap func(T, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		v, err := fn(ctx)
		return wrap(v, err)
	}
}

func (m model) fetchTeammates() tea.Cmd {
	cl := m.cfg.Client
	return fetchCmd(5*time.Second, cl.Teammates, func(items []api.Teammate, err error) tea.Msg {
		return teammatesMsg{items: items, err: err}
	})
}

// fetchTeammatesQuiet polls sub-agent status silently during streaming (P2.5).
func (m model) fetchTeammatesQuiet() tea.Cmd {
	cl := m.cfg.Client
	return fetchCmd(3*time.Second, cl.Teammates, func(items []api.Teammate, err error) tea.Msg {
		if err != nil {
			return nil
		}
		return teammatesUpdateMsg{items: items}
	})
}

// execBangCmd runs a ! shell command and returns its output (P2.2).
func (m model) execBangCmd(cmd string) tea.Cmd {
	workDir := m.workDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shell, args := sandbox.ShellCommand(cmd)
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

// cycleModeCmd advances the permission mode plan→build→auto→plan on shift+tab.
// It updates m.slash.mode optimistically and synchronously (the pointer
// receiver mutates the shared dispatcher), then returns the command that
// persists the change server-side. Advancing locally first is what actually
// makes shift+tab feel like a cycle: the mode badge reflects the new mode on
// the very next render instead of waiting on the /mode UpdateSession round-trip,
// and two quick presses advance two steps rather than both re-reading a mode the
// in-flight RPC hasn't written back yet. cmdMode re-sets the same value on RPC
// success (idempotent) and surfaces an error toast on failure.
func (m *model) cycleModeCmd() tea.Cmd {
	var next string
	switch m.slash.mode {
	case "plan":
		next = "build"
	case "build":
		next = "auto"
	default:
		next = "plan"
	}
	m.slash.mode = next
	parsed := &commands.ParsedCommand{Name: "mode", Args: []string{next}, Raw: "/mode " + next}
	return m.handleSlashCommand(parsed)
}

// --- layout ---

// layout recalculates pane dimensions after a terminal resize.
// Height budget: content(vpH) + textarea+border(ta.Height()+2) + belowBar(1).
// P74.2 removed the title-bar row from that budget, folding its content into
// the status line instead. The completion popup (P33.18) is composited over
// this layout rather than reserving space in it — see render()'s
// anchored-overlay compositing, which is also how the sidebar composites
// (P74.2) — it no longer reserves layout width the way it did here.
func (m *model) layout() {
	vpW := m.width - 1 // -1 for PaddingLeft on the main panel
	if m.rawScrollback {
		// P22.6: raw scrollback mode suppresses the terminal pane and
		// scrollbar column (renderChat/renderScrollbar) — none of that
		// dashboard-column width is reserved, so the plain transcript text
		// gets the full body width instead.
	} else {
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

// fixedH is the non-viewport vertical budget: textarea(+border) + belowBar,
// plus optional strips. P74.2 removed the title-bar row this used to add.
// The completion popup (P33.18) is composited over the finished layout
// instead of reserved here — see renderCompletionPopup and render()'s
// anchored-overlay compositing.
func (m *model) fixedH() int {
	h := m.ta.Height() + 2 + 1
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
		case m.phase.firstTokenAt.IsZero():
			phrase = statusWaiting
			hint = formatStreamHint(m.streamStats())
		case !m.phase.modelWaitAt.IsZero():
			// P33.19: post-tool-round wait — the round's tools have returned and
			// the model is re-evaluating the enlarged prompt, no output yet. Like
			// the first-token wait this is honest dead air, not a flavor phrase;
			// its clock runs from the last tool result (modelWaitAt), so it times
			// this wait rather than the whole turn.
			phrase = statusReeval
			if secs := int(time.Since(m.phase.modelWaitAt).Seconds()); secs > 0 {
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
		work := shimmerText("● "+phrase, m.animStep, colTextMuted, stallRampColor(m.stallElapsed(), m.maxTurnStall))
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

// toggleLastToolBlock flips the expand/collapse state of the most recently
// resolved tool result or read/search group (P75.1) — the keyboard path's
// notion of "the block currently addressed," independent of every other
// block and of the session-wide /tools full|compact default new results
// start from. A no-op when nothing has resolved yet.
func (m *model) toggleLastToolBlock() {
	if len(m.toolState.toolBlocks) == 0 {
		return
	}
	m.toolState.toolBlocks[len(m.toolState.toolBlocks)-1].toggleFull(m)
}

// trackToolBlock registers b as a P75.1 keyboard-addressable tool block,
// replacing any earlier entry for the same transcript item instead of
// duplicating it — a solo card upgrading into a two-member group
// (model.foldIntoReadGroup) reuses its own blk, and the group should take
// over addressing it rather than sit behind a now-stale card entry.
func (m *model) trackToolBlock(b toolBlock) {
	blk := b.blkItem()
	for i, existing := range m.toolState.toolBlocks {
		if existing.blkItem() == blk {
			m.toolState.toolBlocks[i] = b
			return
		}
	}
	m.toolState.toolBlocks = append(m.toolState.toolBlocks, b)
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
	s = renderMathUnicode(s)   // P40.8: LaTeX math → Unicode before glamour sees it
	s = renderMermaidBlocks(s) // P40.9: ```mermaid fences → inline ASCII diagrams
	if m.renderer != nil {
		if rendered, err := m.renderer.Render(s); err == nil {
			return strings.TrimRight(rendered, "\n") + "\n"
		}
	}
	return wrap(s, m.transcript.Width())
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
		tag, tagStyle := "•", m.th.tool
		switch tm.Status {
		case "failed":
			tag, tagStyle = "✗", m.th.toolErr
		case "done":
			tag = "✓"
		}
		// P74.13: the agent id renders in its stable hashed colour (same as
		// the sidebar), so a teammate reads as the same colour everywhere it
		// appears; the tag keeps its status colour so failures still stand out.
		idStyle := lipgloss.NewStyle().Foreground(agentColor(tm.AgentID))
		rest := fmt.Sprintf(" [%s] %s", tm.Status, oneLine(tm.Summary))
		line := "  " + tagStyle.Render(tag) + " " + idStyle.Render(tm.AgentID) + m.th.tool.Render(truncate(rest, m.width-1))
		b.WriteString(line + "\n")
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
