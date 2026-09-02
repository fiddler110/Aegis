package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/session"
)

// --- messages ---
//
// Every tea.Msg type the TUI's Update handler switches on lives here,
// grouped together (P77.5) instead of scattered across tui.go's former
// "messages" and "commands" comment banners.

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

type slashResultMsg SlashResult
type editorDoneMsg struct {
	content string
	err     error
}

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

// cronJobsUpdateMsg is a silent ListCronJobs poll result (P<dashboard>),
// mirroring teammatesUpdateMsg's shape.
type cronJobsUpdateMsg struct{ items []api.CronJobInfo }

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
