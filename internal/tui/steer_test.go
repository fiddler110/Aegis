package tui

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
)

// altEnterKeyMsg is the alt+enter press that steers a running turn (P33.8).
func altEnterKeyMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
}

// steerModel returns a model parked mid-run with the given draft typed, ready
// for the alt+enter-during-streaming steer path.
func steerModel(t *testing.T, draft string) model {
	t.Helper()
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.appendUser("first question", nil)
	m.streamState.streaming = true
	m.streamState.cancel = func() {}
	m.applyEvent(api.Event{Kind: api.KindText, Text: "answering…"})
	m.ta.SetValue(draft)
	return m
}

// TestEnterWhileStreamingQueues is the P33.8 swap seen from the reflex key:
// Enter mid-run holds the draft as the next turn instead of injecting it into
// the run already in flight, and it auto-sends once that run closes.
func TestEnterWhileStreamingQueues(t *testing.T) {
	m := steerModel(t, "and update the README afterwards")

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.composer.pendingSteers) != 0 {
		t.Fatalf("pendingSteers = %v, want Enter to queue rather than steer", m.composer.pendingSteers)
	}
	if len(m.composer.queued) != 1 || m.composer.queued[0] != "and update the README afterwards" {
		t.Fatalf("queued = %v, want the draft Enter just queued", m.composer.queued)
	}
	if got := plainView(m); !strings.Contains(got, "queued ▸ and update the README afterwards") {
		t.Fatalf("expected the draft rendered as a queued block, got:\n%s", got)
	}

	m = driveUpdate(t, m, streamClosedMsg{})
	if len(m.composer.queued) != 0 {
		t.Fatalf("queued = %v, want drained into the next turn", m.composer.queued)
	}
	if !m.streamState.streaming {
		t.Fatal("expected a new stream to have started for the queued message")
	}
	if got := plainView(m); !strings.Contains(got, "and update the README afterwards") {
		t.Fatalf("expected the queued message sent as a user turn, got:\n%s", got)
	}
}

// TestQueueModeSignalMatchesEnterAction is the honesty half of P33.8: the
// composer's mode signal has to describe what Enter is about to do. Enter
// queues while a run streams, so the amber border that used to warn "this
// injects into the live run" must not be what the composer wears by default —
// that warning belonged to the steer action, which moved to alt+enter.
func TestQueueModeSignalMatchesEnterAction(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.setQueueMode(true)
	if !strings.Contains(m.ta.Placeholder, "Queue") {
		t.Errorf("placeholder = %q, want it to name the queue Enter performs", m.ta.Placeholder)
	}
	if got := m.ta.Styles().Focused.Base.GetBorderTopForeground(); got == colWarning {
		t.Error("composer still wears the amber steer border while Enter only queues")
	}

	m.setQueueMode(false)
	if m.ta.Placeholder != "Message Aegis…" {
		t.Errorf("placeholder = %q, want the idle composer restored", m.ta.Placeholder)
	}
}

// TestSteerEchoedThenResolvedOnInjection covers the P33.2 local echo: the
// steer shows up as a dimmed pending block the moment it's sent, and the
// KindSteer event that reports it landed replaces the echo with the real user
// block rather than leaving both on screen.
func TestSteerEchoedThenResolvedOnInjection(t *testing.T) {
	m := steerModel(t, "use the other file")

	m = driveUpdate(t, m, altEnterKeyMsg())
	if len(m.composer.pendingSteers) != 1 || m.composer.pendingSteers[0].text != "use the other file" {
		t.Fatalf("pendingSteers = %v, want the steer just sent", m.composer.pendingSteers)
	}
	if m.composer.pendingSteers[0].origin != steerOriginUser {
		t.Errorf("pendingSteers[0].origin = %v, want steerOriginUser", m.composer.pendingSteers[0].origin)
	}
	if got := plainView(m); !strings.Contains(got, "steer ▸ use the other file") {
		t.Fatalf("expected the steer echoed as a pending block, got:\n%s", got)
	}

	m.applyEvent(api.Event{Kind: api.KindSteer, Text: "use the other file"})
	m.refresh()
	if len(m.composer.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want empty once the steer was injected", m.composer.pendingSteers)
	}
	got := plainView(m)
	if strings.Contains(got, "steer ▸") {
		t.Errorf("the pending echo outlived the injected steer:\n%s", got)
	}
	if !strings.Contains(got, "use the other file") {
		t.Errorf("expected the injected steer rendered as a user turn, got:\n%s", got)
	}
}

// TestSteerUnconsumedRequeues is the P33.2 regression: a steer the run ended
// without ever injecting comes back as KindSteerUnconsumed and lands in the
// TQ8 queue, so it auto-sends as the next user turn instead of vanishing.
func TestSteerUnconsumedRequeues(t *testing.T) {
	m := steerModel(t, "actually, stop and explain")
	m = driveUpdate(t, m, altEnterKeyMsg())

	m.applyEvent(api.Event{Kind: api.KindSteerUnconsumed, Text: "actually, stop and explain"})
	m.refresh()
	if len(m.composer.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want empty once the daemon reported back", m.composer.pendingSteers)
	}
	if len(m.composer.queued) != 1 || m.composer.queued[0] != "actually, stop and explain" {
		t.Fatalf("queued = %v, want the unconsumed steer", m.composer.queued)
	}
	if got := plainView(m); !strings.Contains(got, "queued ▸ actually, stop and explain") {
		t.Fatalf("expected the requeued steer rendered as a pending block, got:\n%s", got)
	}

	m = driveUpdate(t, m, streamClosedMsg{})
	if len(m.composer.queued) != 0 {
		t.Fatalf("queued = %v, want drained into the next turn", m.composer.queued)
	}
	if !m.streamState.streaming {
		t.Fatal("expected a new stream to have started for the requeued steer")
	}
	if got := plainView(m); !strings.Contains(got, "actually, stop and explain") {
		t.Fatalf("expected the requeued steer sent as a user turn, got:\n%s", got)
	}
}

// TestSteerUnconsumedAfterInterruptIsNoted checks the cancel path: an
// interrupt discards the TQ8 queue, so an unconsumed steer arriving after one
// must not sneak a turn in behind the user's brakes — it's surfaced as a note
// instead, which still keeps the typed text on screen.
func TestSteerUnconsumedAfterInterruptIsNoted(t *testing.T) {
	m := steerModel(t, "actually, stop and explain")
	m = driveUpdate(t, m, altEnterKeyMsg())
	m = driveUpdate(t, m, escKeyMsg())
	if !m.composer.interrupted {
		t.Fatal("expected Esc while streaming to mark the run interrupted")
	}

	m.applyEvent(api.Event{Kind: api.KindSteerUnconsumed, Text: "actually, stop and explain"})
	m.refresh()
	if len(m.composer.queued) != 0 {
		t.Errorf("queued = %v, want empty after an interrupt", m.composer.queued)
	}
	got := plainView(m)
	if !strings.Contains(got, "steer not delivered (interrupted)") {
		t.Errorf("expected an undelivered-steer note, got:\n%s", got)
	}
	if !strings.Contains(got, "actually, stop and explain") {
		t.Errorf("the note dropped the steer text, got:\n%s", got)
	}
}

// TestSteerWithoutVerdictRequeuedOnStreamClose covers a daemon that never
// emits KindSteerUnconsumed (an older one, or an event the SSE buffer dropped):
// the echo must not dangle in the tail forever, and the text is still
// recovered as the next turn rather than lost.
func TestSteerWithoutVerdictRequeuedOnStreamClose(t *testing.T) {
	m := steerModel(t, "one more thing")
	m = driveUpdate(t, m, altEnterKeyMsg())

	m = driveUpdate(t, m, streamClosedMsg{})
	if len(m.composer.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want cleared when the stream closed", m.composer.pendingSteers)
	}
	if !m.streamState.streaming {
		t.Fatal("expected the leftover steer to be sent as the next turn")
	}
	if got := plainView(m); !strings.Contains(got, "one more thing") {
		t.Fatalf("expected the leftover steer sent as a user turn, got:\n%s", got)
	}
}

// TestSteerFailed404DoesNotTearDownRunAndRequeues is the P33.15 #1/#2
// regression: a steer POST that 404s (errSteerClosed — the run already ended
// before it arrived) is not retryable and not a real failure. It must not be
// shown as a scary error, and — critically, #2 — must not tear the run's UI
// state down: the main SSE stream this steer targeted may still be live.
func TestSteerFailed404DoesNotTearDownRunAndRequeues(t *testing.T) {
	m := steerModel(t, "actually, stop and explain")
	m = driveUpdate(t, m, altEnterKeyMsg())
	if len(m.composer.pendingSteers) != 1 {
		t.Fatalf("pendingSteers = %v, want the steer just sent", m.composer.pendingSteers)
	}

	m = driveUpdate(t, m, steerFailedMsg{
		text:   "actually, stop and explain",
		origin: steerOriginUser,
		err:    fmt.Errorf("steer: %w", &client.StatusError{Code: http.StatusNotFound, Msg: "no active run for session"}),
	})

	if !m.streamState.streaming {
		t.Error("expected streaming to stay true — the main stream never ended, only the steer POST failed")
	}
	if len(m.composer.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want the failed steer resolved", m.composer.pendingSteers)
	}
	if len(m.composer.queued) != 1 || m.composer.queued[0] != "actually, stop and explain" {
		t.Fatalf("queued = %v, want the 404'd steer requeued as the next turn", m.composer.queued)
	}
	got := plainView(m)
	if strings.Contains(got, "error:") {
		t.Errorf("a 404 steer failure must not render as a scary generic error, got:\n%s", got)
	}
}

// TestSteerFailed429IsRetryableAndDoesNotTearDown is the P33.15 #1/#2
// regression for the other status code: a full steer buffer (errSteerFull,
// 429) is retryable and must say so, distinct from the 404 text — and, like
// the 404 case, must leave the still-live run's UI state alone.
func TestSteerFailed429IsRetryableAndDoesNotTearDown(t *testing.T) {
	m := steerModel(t, "use the other approach")
	m = driveUpdate(t, m, altEnterKeyMsg())

	m = driveUpdate(t, m, steerFailedMsg{
		text:   "use the other approach",
		origin: steerOriginUser,
		err:    fmt.Errorf("steer: %w", &client.StatusError{Code: http.StatusTooManyRequests, Msg: "steer buffer full"}),
	})

	if !m.streamState.streaming {
		t.Error("expected streaming to stay true after a retryable steer failure")
	}
	if len(m.composer.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want the failed steer resolved", m.composer.pendingSteers)
	}
	// Unlike the 404 case, a 429 is not requeued as a fresh turn — it's the
	// same instruction still worth trying again as a steer, not a new message.
	if len(m.composer.queued) != 0 {
		t.Errorf("queued = %v, want a 429 NOT requeued as a new user turn", m.composer.queued)
	}
	got := plainView(m)
	if !strings.Contains(got, "try again") {
		t.Errorf("expected a retry hint distinct from the 404 case, got:\n%s", got)
	}
}

// TestSteerFailedLeavesOtherPendingSteersAlone checks the "other in-flight
// m.composer.pendingSteers alone" half of P33.15 #2: when two steers are in flight and
// only one fails, the other's pending echo must survive untouched.
func TestSteerFailedLeavesOtherPendingSteersAlone(t *testing.T) {
	m := steerModel(t, "first steer")
	m = driveUpdate(t, m, altEnterKeyMsg())
	m.ta.SetValue("second steer")
	m = driveUpdate(t, m, altEnterKeyMsg())
	if len(m.composer.pendingSteers) != 2 {
		t.Fatalf("pendingSteers = %v, want both in flight", m.composer.pendingSteers)
	}

	m = driveUpdate(t, m, steerFailedMsg{
		text:   "first steer",
		origin: steerOriginUser,
		err:    fmt.Errorf("steer: %w", &client.StatusError{Code: http.StatusTooManyRequests}),
	})

	if len(m.composer.pendingSteers) != 1 || m.composer.pendingSteers[0].text != "second steer" {
		t.Fatalf("pendingSteers = %v, want only the failed one removed", m.composer.pendingSteers)
	}
	if !m.streamState.streaming {
		t.Error("expected streaming to stay true")
	}
}

// TestSteerFailedAlreadyResolvedIsANoOp guards the race steerFailedMsg's
// found check exists for: if the main stream resolves a steer (KindSteer, or
// the streamClosedMsg safety net) before this async POST-failure response
// arrives, the handler must not re-touch state — in particular it must not
// double-queue the text.
func TestSteerFailedAlreadyResolvedIsANoOp(t *testing.T) {
	m := steerModel(t, "already handled")
	m = driveUpdate(t, m, altEnterKeyMsg())

	// The main stream reports the steer landed before the POST's own
	// response (steerFailedMsg) arrives.
	m.applyEvent(api.Event{Kind: api.KindSteer, Text: "already handled"})
	m.refresh()
	if len(m.composer.pendingSteers) != 0 {
		t.Fatalf("pendingSteers = %v, want resolved by KindSteer", m.composer.pendingSteers)
	}

	m = driveUpdate(t, m, steerFailedMsg{
		text:   "already handled",
		origin: steerOriginUser,
		err:    fmt.Errorf("steer: %w", &client.StatusError{Code: http.StatusNotFound}),
	})
	if len(m.composer.queued) != 0 {
		t.Errorf("queued = %v, want no duplicate requeue for an already-resolved steer", m.composer.queued)
	}
	if !m.streamState.streaming {
		t.Error("expected streaming to stay true")
	}
}

// TestDenialFeedbackSteerTaggedAndNotRequeuedAsUserTurn is the P33.15 #3
// regression: approval.go's deny-with-feedback steer is system-phrased text
// ("The user denied the X call. Feedback: ..."), not something the user
// typed. If the run ends before it's consumed, it must not be requeued as
// the next user turn the way TestSteerUnconsumedRequeues expects for a
// genuine user-typed steer.
func TestDenialFeedbackSteerTaggedAndNotRequeuedAsUserTurn(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streamState.streaming = true
	m.applyEvent(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           "shell",
		ToolInput:      []byte(`{"command":"rm -rf /tmp/x"}`),
		ApprovalReason: "execute capability requires approval",
		ApprovalID:     "run-1",
	})

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	for _, r := range "not needed" {
		m = driveUpdate(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(m.composer.pendingSteers) != 1 {
		t.Fatalf("pendingSteers = %v, want the denial-feedback steer registered", m.composer.pendingSteers)
	}
	if m.composer.pendingSteers[0].origin != steerOriginDenialFeedback {
		t.Errorf("origin = %v, want steerOriginDenialFeedback", m.composer.pendingSteers[0].origin)
	}
	steerText := m.composer.pendingSteers[0].text
	if !strings.Contains(steerText, "The user denied the shell call. Feedback: not needed") {
		t.Fatalf("unexpected steer text: %q", steerText)
	}

	// The run ends without ever consuming it.
	m.applyEvent(api.Event{Kind: api.KindSteerUnconsumed, Text: steerText})
	m.refresh()

	if len(m.composer.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want resolved", m.composer.pendingSteers)
	}
	if len(m.composer.queued) != 0 {
		t.Errorf("queued = %v, want the denial-feedback text NOT requeued as a user turn", m.composer.queued)
	}
	if got := plainView(m); !strings.Contains(got, "feedback not delivered") {
		t.Errorf("expected a not-delivered note instead, got:\n%s", got)
	}
}
