package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/api"
)

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

// startDrive begins a phased skill drive (P52.12), the unattended counterpart
// to startStream. It streams over the same SSE seam, so every event the
// transcript already knows how to render arrives unchanged and nothing
// downstream of here has to distinguish a drive from a turn.
//
// Two things differ from a message. The run is marked resumable, because a
// phased build is the case that most wants to outlive a dropped connection —
// a multi-hour local-model run should not die because a terminal did. That
// choice would otherwise break interrupt (a resumable run keeps executing
// server-side when its request context is cancelled), so the returned cancel
// stops the run through the daemon *first* and only then closes the stream.
// Every existing cancel site — ESC, Ctrl+C, /quit — calls that same handle, so
// they all keep meaning "stop this run" without knowing a drive is behind it.
func (m model) startDrive(req api.DriveRequest) tea.Cmd {
	cl, id := m.cfg.Client, m.cfg.SessionID
	req.GuardEnabled = m.slash.guardEnabled
	req.Resumable = true
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := cl.Drive(ctx, id, req)
		if err != nil {
			cancel()
			return errMsg{err}
		}
		stop := func() {
			sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer scancel()
			// Best-effort: a run that already finished has nothing to stop, and
			// the cancel below is what actually ends this client's stream.
			_ = cl.StopRun(sctx, id)
			cancel()
		}
		return streamStartedMsg{ch: ch, cancel: stop}
	}
}

// setQueueMode switches the textarea between normal input and queue mode.
// In queue mode the placeholder and border colour signal that Enter holds the
// draft back as the next user turn instead of sending it now; injecting into
// the running turn is alt+enter, which the placeholder names because it is the
// deliberate action rather than the reflex one (P33.8).

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
	case m.streamState.phase.firstTokenAt.IsZero():
		return statusWaiting
	case !m.streamState.phase.modelWaitAt.IsZero():
		return statusReeval
	default:
		return statusGenerating
	}
}

// stallElapsed is how long the current wait phase has been running: since
// streamStart while waiting for the first token, since modelWaitAt during a
// post-tool-round re-eval, and zero once the model is actively producing
// output — tokens arriving is forward progress, not a stall, and P74.11's
// ramp exists to flag the *absence* of that progress. Feeds stallRampColor.
func (m model) stallElapsed() time.Duration {
	switch {
	case m.streamState.phase.firstTokenAt.IsZero():
		if m.streamState.phase.streamStart.IsZero() {
			return 0
		}
		return time.Since(m.streamState.phase.streamStart)
	case !m.streamState.phase.modelWaitAt.IsZero():
		return time.Since(m.streamState.phase.modelWaitAt)
	default:
		return 0
	}
}

// beginStream marks the start of a run and resets the per-run phase state.
// streamStart is zeroed too so the elapsed readout can't briefly quote the
// previous run's clock in the frames before streamStartedMsg lands.
func (m *model) beginStream() {
	m.streamState.streaming = true
	// P33.10: re-arm the first-keystroke pre-warm for the next message. The run
	// starting now loads the model itself, but it may have unloaded again by the
	// time the user composes their next turn.
	m.composer.warmPinged = false
	m.streamState.phase.streamStart = time.Time{}
	m.streamState.phase.firstTokenAt = time.Time{}
	m.streamState.phase.modelWaitAt = time.Time{}
	m.streamState.phase.outBytes = 0
	m.status = statusWaiting
	// P33.17: the previous turn's inputTokens is now stale for this turn's
	// prompt size — streamStats() must hide the ↑ segment rather than quote it
	// until KindTurnDone reports this turn's real usage.
	m.usage.inputTokensKnown = false
}

// markModelOutput ends the waiting phase and accumulates n output bytes. Any
// evidence of model output counts, not just prose: a run whose first act is a
// tool call reaches this through KindToolCallStart, so the phase line never
// claims the run is still waiting while P33.3's provisional "preparing <tool>…"
// card is on screen saying otherwise.
func (m *model) markModelOutput(n int) {
	if m.streamState.phase.firstTokenAt.IsZero() {
		m.streamState.phase.firstTokenAt = time.Now()
		m.status = statusGenerating
	}
	// The model has resumed producing output, so any post-tool-round wait
	// (P33.19) has ended — clear it unconditionally, since a later round sets
	// it afresh from its own last tool result.
	if !m.streamState.phase.modelWaitAt.IsZero() {
		m.streamState.phase.modelWaitAt = time.Time{}
		m.status = statusGenerating
	}
	m.streamState.phase.outBytes += n
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
	if m.usage.inputTokensKnown {
		st.inputToks = m.usage.inputTokens
	}
	if !m.streamState.phase.streamStart.IsZero() {
		st.elapsedSecs = int(time.Since(m.streamState.phase.streamStart).Seconds())
	}
	st.outputToks = m.streamState.phase.outBytes / bytesPerTokenEstimate
	// Rate over the generation window only. The wait for the first token runs
	// to a minute on a cold local model; averaging it in would report a
	// throughput the model never ran at.
	if !m.streamState.phase.firstTokenAt.IsZero() && st.outputToks > 0 {
		if secs := time.Since(m.streamState.phase.firstTokenAt).Seconds(); secs >= 1 {
			st.tokPerSec = float64(st.outputToks) / secs
		}
	}
	return st
}

// sendUserMessage appends text as a user turn and starts the stream. Shared by
// the enter/alt+enter key paths and the queued-message drain (TQ8).
func (m *model) sendUserMessage(text string) tea.Cmd {
	m.composer.history = append(m.composer.history, text)
	m.composer.histIdx = -1
	m.composer.draftInput = ""
	cleanText, images := extractImageRefs(text, m.cfg.WorkDir)
	cleanText = extractShellRefs(cleanText, m.splitTerm.term)
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
	m.streamState.followBottom = true // jump to the freshly sent message
	// The callers reset the textarea just before this; with DynamicHeight
	// that changes fixedH, so resync the pane height (which also re-pins).
	// Skipped before the first WindowSizeMsg, when m.chrome.height is still zero.
	if m.chrome.height > 0 {
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
	f := m.sessionMeta.lastFailure
	if f == nil || m.streamState.streaming {
		return nil
	}
	m.sessionMeta.lastFailure = nil
	if m.splitTerm.termFocused {
		m.splitTerm.termFocused = false
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
	for i, st := range m.composer.pendingSteers {
		if st.text == text {
			origin := st.origin
			m.composer.pendingSteers = append(m.composer.pendingSteers[:i], m.composer.pendingSteers[i+1:]...)
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
	if m.composer.interrupted {
		m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered (interrupted): "+oneLine(text)) + "\n\n")
		return
	}
	m.composer.queued = append(m.composer.queued, text)
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

// flushLiveText renders accumulated assistant text through glamour and appends
// it to the transcript. Called at KindTurnDone, KindToolCall, and KindError.
func (m *model) flushLiveText() {
	if m.streamState.liveText.Len() == 0 {
		return
	}
	raw := m.streamState.liveText.String()
	m.streamState.liveText.Reset()
	m.streamState.live.reset()
	m.streamState.lastAssistantText = raw // TQ4: capture for /copy
	// AppendBlock rather than Append so a guard-retry event can withdraw this
	// answer in place (P25.3); nil when the render was empty or the block was
	// immediately trimmed out.
	m.streamState.lastAnswerBlock = m.transcript.AppendBlock(m.mdRender(raw))
}
