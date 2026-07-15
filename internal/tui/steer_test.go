package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
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
	m.streaming = true
	m.cancel = func() {}
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
	if len(m.pendingSteers) != 0 {
		t.Fatalf("pendingSteers = %v, want Enter to queue rather than steer", m.pendingSteers)
	}
	if len(m.queued) != 1 || m.queued[0] != "and update the README afterwards" {
		t.Fatalf("queued = %v, want the draft Enter just queued", m.queued)
	}
	if got := plainView(m); !strings.Contains(got, "queued ▸ and update the README afterwards") {
		t.Fatalf("expected the draft rendered as a queued block, got:\n%s", got)
	}

	m = driveUpdate(t, m, streamClosedMsg{})
	if len(m.queued) != 0 {
		t.Fatalf("queued = %v, want drained into the next turn", m.queued)
	}
	if !m.streaming {
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
	if len(m.pendingSteers) != 1 || m.pendingSteers[0] != "use the other file" {
		t.Fatalf("pendingSteers = %v, want the steer just sent", m.pendingSteers)
	}
	if got := plainView(m); !strings.Contains(got, "steer ▸ use the other file") {
		t.Fatalf("expected the steer echoed as a pending block, got:\n%s", got)
	}

	m.applyEvent(api.Event{Kind: api.KindSteer, Text: "use the other file"})
	m.refresh()
	if len(m.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want empty once the steer was injected", m.pendingSteers)
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
	if len(m.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want empty once the daemon reported back", m.pendingSteers)
	}
	if len(m.queued) != 1 || m.queued[0] != "actually, stop and explain" {
		t.Fatalf("queued = %v, want the unconsumed steer", m.queued)
	}
	if got := plainView(m); !strings.Contains(got, "queued ▸ actually, stop and explain") {
		t.Fatalf("expected the requeued steer rendered as a pending block, got:\n%s", got)
	}

	m = driveUpdate(t, m, streamClosedMsg{})
	if len(m.queued) != 0 {
		t.Fatalf("queued = %v, want drained into the next turn", m.queued)
	}
	if !m.streaming {
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
	if !m.interrupted {
		t.Fatal("expected Esc while streaming to mark the run interrupted")
	}

	m.applyEvent(api.Event{Kind: api.KindSteerUnconsumed, Text: "actually, stop and explain"})
	m.refresh()
	if len(m.queued) != 0 {
		t.Errorf("queued = %v, want empty after an interrupt", m.queued)
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
	if len(m.pendingSteers) != 0 {
		t.Errorf("pendingSteers = %v, want cleared when the stream closed", m.pendingSteers)
	}
	if !m.streaming {
		t.Fatal("expected the leftover steer to be sent as the next turn")
	}
	if got := plainView(m); !strings.Contains(got, "one more thing") {
		t.Fatalf("expected the leftover steer sent as a user turn, got:\n%s", got)
	}
}
