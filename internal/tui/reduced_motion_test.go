package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
)

// TestReducedMotionFreezesAnimStep is the P74.10 regression guard: with
// Config.ReducedMotion set, a streaming spinner tick must not advance
// animStep (which drives shimmerText, caretGlyph and thinkingPhrase) or call
// the per-tick pending-card refresh, while an unset config keeps advancing it
// as before.
func TestReducedMotionFreezesAnimStep(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir(), ReducedMotion: true})
	if !m.chrome.reducedMotion {
		t.Fatal("expected Config.ReducedMotion to set model.chrome.reducedMotion")
	}
	m.streamState.streaming = true
	m.streamState.followBottom = true

	before := m.streamState.animStep
	next, _ := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	if next.streamState.animStep != before {
		t.Errorf("expected animStep frozen at %d under reduced motion, got %d", before, next.streamState.animStep)
	}
}

// TestMotionDefaultAdvancesAnimStep is the control case for
// TestReducedMotionFreezesAnimStep: with reduced motion off (the default),
// a streaming spinner tick still advances animStep.
func TestMotionDefaultAdvancesAnimStep(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.streamState.streaming = true
	m.streamState.followBottom = true

	before := m.streamState.animStep
	next, _ := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	if next.streamState.animStep == before {
		t.Errorf("expected animStep to advance past %d with motion enabled", before)
	}
}

// TestReducedMotionStillPollsTeammates confirms the P2.5 sub-agent roster
// poll keeps its cadence under reduced motion — it must not ride the frozen
// animStep counter.
func TestReducedMotionStillPollsTeammates(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir(), ReducedMotion: true})
	m.streamState.streaming = true
	m.streamState.followBottom = true

	var sawPoll bool
	for i := 0; i < 20; i++ {
		next, cmds := m.updateSpinnerTick(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
		m = next
		if len(cmds) > 1 {
			sawPoll = true
		}
	}
	if !sawPoll {
		t.Error("expected the teammate poll to still fire every 20 ticks under reduced motion")
	}
}

// TestReducedMotionCaretStaysStaticOnce covers the caret specifically: under
// reduced motion, once live text is streaming the caret must render (it
// never blinks off) instead of following the blink-period parity of a
// frozen-at-zero animStep.
func TestReducedMotionCaretStaysStaticOnce(t *testing.T) {
	m := followBottomTestModel(t)
	m.cfg.ReducedMotion = true
	m.chrome.reducedMotion = true
	m.appendUser("hi", nil)
	m.streamState.streaming = true
	m.streamState.followBottom = true
	m.streamState.liveText.WriteString("hello world")

	m.streamState.animStep = 0
	m.refresh()
	if !strings.Contains(plainView(m), caretChar) {
		t.Fatal("expected the caret glyph present while streaming under reduced motion")
	}
}
