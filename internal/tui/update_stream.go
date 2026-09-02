package tui

import (
	"errors"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// updateStreamStarted adopts the freshly opened event stream for a run.
func (m model) updateStreamStarted(msg streamStartedMsg) (tea.Model, tea.Cmd) {
	m.streamState.events = msg.ch
	m.streamState.cancel = msg.cancel
	m.streamState.phase.streamStart = time.Now()
	m.composer.backtrackArmed = false
	m.composer.interrupted = false
	m.setQueueMode(true)
	return m, tea.Batch(waitForEvent(m.streamState.events), m.sp.Tick)
}

// updateEvent applies a single stream event. This path is kept for direct
// drivers such as the integration tests; the live stream arrives as
// batchEventMsg. Both share applyStreamBatch so the follow-bottom and notify
// bookkeeping stay identical.
func (m model) updateEvent(msg eventMsg) (tea.Model, tea.Cmd) {
	notifyCmd := m.applyStreamBatch([]api.Event{api.Event(msg)})
	return m, tea.Batch(waitForEvent(m.streamState.events), notifyCmd)
}

// updateBatchEvent applies a drained batch of stream events.
func (m model) updateBatchEvent(msg batchEventMsg) (tea.Model, tea.Cmd) {
	notifyCmd := m.applyStreamBatch(msg.events)
	if msg.closed {
		// The stream closed within this same drain: run the exact
		// teardown the dedicated streamClosedMsg path does, on the
		// already-applied state, and carry any per-event notification
		// (e.g. a cost alert) alongside the closed path's own command.
		nm, closeCmd := m.Update(streamClosedMsg{})
		return nm, tea.Batch(notifyCmd, closeCmd)
	}
	return m, tea.Batch(waitForEvent(m.streamState.events), notifyCmd)
}

// updateStreamClosed tears down a finished run.
func (m model) updateStreamClosed(msg streamClosedMsg) (tea.Model, tea.Cmd) {
	m.flushThinking()
	m.flushLiveText() // safety: in case KindTurnDone wasn't the last event
	// P21.2: the universal safety net for a stuck-pending tool card — the
	// stream can close without KindError or KindDone at all on a
	// client-initiated cancel (engine.ErrInterrupted's callers return
	// before emitting anything), so this is the one place guaranteed to
	// run after every kind of run end. A no-op if KindError already
	// resolved everything.
	m.resolveStuckToolCards()
	m.streamState.streaming = false
	// P74.12: ticks stop firing once streaming is false, so snap the
	// displayed counters to the real values now rather than leaving them
	// short of a target nothing will ease them toward again.
	m.usage.displayedInputTokens = m.usage.inputTokens
	m.usage.displayedOutputTokens = m.usage.outputTokens
	m.streamState.events = nil
	m.streamState.cancel = nil
	m.status = "ready"
	m.composer.backtrackArmed = false
	m.setQueueMode(false)
	m.transcript.Append("\n")
	// P33.2: a steer the daemon never reported a verdict on — an older
	// daemon that doesn't emit KindSteerUnconsumed at all, or an event the
	// SSE buffer dropped — is unconsumed by definition once the stream is
	// gone, so treat it as such instead of leaving its echo dangling.
	for _, st := range m.composer.pendingSteers {
		m.requeueSteer(st.text, st.origin)
	}
	m.composer.pendingSteers = nil
	// TQ8: auto-send the next queued message, one per completed run. Don't
	// notify here — another run is about to start immediately.
	if len(m.composer.queued) > 0 {
		next := m.composer.queued[0]
		m.composer.queued = m.composer.queued[1:]
		return m, m.sendUserMessage(next)
	}
	m.refresh()
	return m, m.notifyCmd(notify.Event{Title: "Aegis", Body: "Run finished"})
}

// updateErr reports a failed run.
func (m model) updateErr(msg errMsg) (tea.Model, tea.Cmd) {
	m.streamState.streaming = false
	m.usage.displayedInputTokens = m.usage.inputTokens
	m.usage.displayedOutputTokens = m.usage.outputTokens
	m.composer.backtrackArmed = false
	m.setQueueMode(false)
	m.transcript.Append(m.th.errLine.Render("error: "+msg.err.Error()) + "\n\n")
	// TQ8: don't auto-send into a failing session.
	if len(m.composer.queued) > 0 {
		m.composer.queued = nil
		m.transcript.Append(m.th.statusDim.Render("⏳ queued messages discarded after error") + "\n\n")
	}
	for _, st := range m.composer.pendingSteers {
		m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered: "+oneLine(st.text)) + "\n\n")
	}
	m.composer.pendingSteers = nil
	m.status = "ready"
	m.refresh()
	return m, m.notifyCmd(notify.Event{Title: "Aegis", Body: "Error: " + truncate(msg.err.Error(), 100)})
}

// updateSteerFailed handles a steer POST that failed.
//
// P33.15 #2: a steer POST failing does NOT mean the stream it was meant to
// interrupt died — unlike updateErr above, this must not touch m.streamState.streaming,
// m.composer.queued, or any other in-flight m.composer.pendingSteers.
func (m model) updateSteerFailed(msg steerFailedMsg) (tea.Model, tea.Cmd) {
	if _, found := m.resolvePendingSteer(msg.text); !found {
		// Already resolved by the main stream itself — a KindSteer/
		// KindSteerUnconsumed event, or the streamClosedMsg safety net —
		// arrived before this async POST response did. Nothing left to do.
		return m, nil
	}
	var statusErr *client.StatusError
	switch {
	case errors.As(msg.err, &statusErr) && statusErr.Code == http.StatusNotFound:
		// errSteerClosed: the run had already ended before the steer
		// reached the daemon. Not a failure — the run legitimately
		// finished — so this isn't shown as an error at all; hand the
		// text to the same requeue path KindSteerUnconsumed uses so a
		// user-typed steer still isn't silently lost (P33.15 #1).
		m.requeueSteer(msg.text, msg.origin)
	case errors.As(msg.err, &statusErr) && statusErr.Code == http.StatusTooManyRequests:
		// errSteerFull: the run is still live, this is just retryable.
		m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered (server busy — try again): "+oneLine(msg.text)) + "\n\n")
	default:
		m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered: "+oneLine(msg.text)) + "\n\n")
	}
	m.refresh()
	return m, nil
}
