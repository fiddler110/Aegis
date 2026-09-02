package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// pinnedStreamingModel builds a follow-bottom test model mid-stream: a user
// turn plus enough streamed lines that the transcript overflows the pane, with
// the viewport pinned to the bottom and followBottom armed — the state every
// one of the P21.7 regressions below perturbs.
func pinnedStreamingModel(t *testing.T) model {
	t.Helper()
	m := followBottomTestModel(t)
	// Typed runes run syncCompletion → slash.Customs(); pre-seed the customs
	// cache so the client-less test model never calls the nil daemon client
	// (the same pre-existing crash risk integration_test.go sidesteps).
	m.slash.customs = []api.CommandInfo{}
	m.appendUser("please write a long answer", nil)
	m.streamState.streaming = true
	m.streamState.followBottom = true
	m.refresh()
	for i := 0; i < 20; i++ {
		m = driveUpdate(t, m, eventMsg(api.Event{
			Kind: api.KindText,
			Text: fmt.Sprintf("streamed line %d of the reply\n", i),
		}))
	}
	if m.transcript.TotalHeight() <= m.transcript.VisibleLineCount() {
		t.Fatal("setup: streamed reply must overflow the pane or pinned vs. unpinned is indistinguishable")
	}
	if !m.streamState.followBottom || !m.transcript.AtBottom() {
		t.Fatal("setup: expected the model to be pinned and following mid-stream")
	}
	return m
}

// TestTypingLettersMidStreamDoesNotUnfollow (P21.7) reproduces the reported
// "I have to manually scroll to find what new content has been written" bug:
// every KeyMsg used to be forwarded to BOTH the textarea (as typed text) and
// transcriptPane.HandleKey (as a vi scroll key), so typing any word containing
// 'u', 'k', or 'b' — or pressing the up arrow to edit the draft — while a
// response streamed scrolled the transcript up, and the geometry-derived
// catch-all then cleared followBottom for the rest of the turn. Plain typing
// must edit the draft only: the viewport stays pinned and follow stays armed.
func TestTypingLettersMidStreamDoesNotUnfollow(t *testing.T) {
	m := pinnedStreamingModel(t)

	for _, s := range []string{"u", "k", "b", "j", "d", "f"} {
		m = driveUpdate(t, m, tea.KeyPressMsg{Code: []rune(s)[0], Text: s})
		if !m.streamState.followBottom {
			t.Fatalf("typing %q cleared followBottom — plain letters must not scroll the transcript", s)
		}
		if !m.transcript.AtBottom() {
			t.Fatalf("typing %q moved the viewport off the bottom", s)
		}
	}
	for _, s := range []string{"up", "down", "space"} {
		m = driveUpdate(t, m, testKeyMsg(s))
		if !m.streamState.followBottom {
			t.Fatalf("pressing %q cleared followBottom — textarea editing keys must not scroll the transcript", s)
		}
	}

	// The keys still reached the textarea as typed input.
	if got := m.ta.Value(); !strings.Contains(got, "ukbjdf") {
		t.Fatalf("expected typed letters to reach the textarea draft, got %q", got)
	}

	// And the next streamed token still follows.
	m = driveUpdate(t, m, eventMsg(api.Event{Kind: api.KindText, Text: "one more line\n"}))
	if !m.streamState.followBottom || !m.transcript.AtBottom() {
		t.Fatal("expected the viewport to keep following streamed tokens after typing")
	}
}

// TestViewportShrinkMidStreamKeepsFollowing (P21.7) covers the other silent
// follow-killer: a mid-stream pane-height change (completion popup opening,
// approval dialog appearing, textarea growing a line) used to make the
// geometric AtBottom() read false with no user scroll having happened, and the
// next batch's pre-apply re-derivation then cleared followBottom permanently.
// Follow is user intent: only an explicit scroll-up may pause it.
func TestViewportShrinkMidStreamKeepsFollowing(t *testing.T) {
	m := pinnedStreamingModel(t)

	// Shrink the pane out from under the pinned offset (what applyViewportHeight
	// does when fixedH grows); the old offset no longer reaches the bottom.
	m.transcript.SetSize(m.transcript.Width(), 4)

	m = driveUpdate(t, m, eventMsg(api.Event{Kind: api.KindText, Text: "post-shrink line\n"}))
	if !m.streamState.followBottom {
		t.Fatal("a viewport height change must not clear followBottom — the user never scrolled")
	}
	if !m.transcript.AtBottom() {
		t.Fatal("expected the refresh after the height change to re-pin the viewport to the bottom")
	}
}

// TestApplyViewportHeightRePinsWhenFollowing (P21.7): the resize itself must
// keep the newest content visible while following, not wait for the next
// streamed token to arrive.
func TestApplyViewportHeightRePinsWhenFollowing(t *testing.T) {
	m := pinnedStreamingModel(t)

	// Force a real height change through applyViewportHeight (fixedH grows when
	// the approval dialog opens, the completion popup appears, or the textarea
	// wraps to another line).
	m.chrome.height = 12
	m.applyViewportHeight()
	if !m.transcript.AtBottom() {
		t.Fatal("applyViewportHeight must re-pin the bottom while followBottom is set")
	}
}

// TestPgUpPausesAndPgDownResumesFollow (P21.7) locks in the intended crush-like
// contract end to end: pgup pauses follow (tokens stream without moving the
// viewport), paging back down to the bottom resumes it.
func TestPgUpPausesAndPgDownResumesFollow(t *testing.T) {
	m := pinnedStreamingModel(t)

	m = driveUpdate(t, m, testKeyMsg("pgup"))
	if m.streamState.followBottom {
		t.Fatal("pgup must pause follow")
	}
	if m.transcript.AtBottom() {
		t.Fatal("setup: pgup should have scrolled the viewport off the bottom")
	}

	// Tokens keep arriving; the paused viewport must not move.
	before := m.transcript.View()
	for i := 0; i < 5; i++ {
		m = driveUpdate(t, m, eventMsg(api.Event{Kind: api.KindText, Text: fmt.Sprintf("while paused %d\n", i)}))
	}
	if m.streamState.followBottom {
		t.Fatal("streamed tokens must not re-arm follow while scrolled away from the bottom")
	}
	if got := m.transcript.View(); got != before {
		t.Fatalf("paused viewport moved while tokens streamed:\nbefore:\n%s\nafter:\n%s", before, got)
	}

	// Page back down to the bottom: follow resumes and the next token pins.
	for i := 0; i < 50 && !m.transcript.AtBottom(); i++ {
		m = driveUpdate(t, m, testKeyMsg("pgdown"))
	}
	if !m.streamState.followBottom {
		t.Fatal("paging back to the bottom must resume follow")
	}
	m = driveUpdate(t, m, eventMsg(api.Event{Kind: api.KindText, Text: "resumed line\n"}))
	if !m.transcript.AtBottom() {
		t.Fatal("expected the viewport to follow again after returning to the bottom")
	}
}

// TestMouseWheelPausesAndResumesFollow (P21.7): same contract for the wheel,
// which is how most users actually scroll back mid-stream.
func TestMouseWheelPausesAndResumesFollow(t *testing.T) {
	m := pinnedStreamingModel(t)

	m = driveUpdate(t, m, tea.MouseWheelMsg{X: 2, Y: 2, Button: tea.MouseWheelUp})
	if m.streamState.followBottom {
		t.Fatal("wheel-up must pause follow")
	}

	for i := 0; i < 50 && !m.transcript.AtBottom(); i++ {
		m = driveUpdate(t, m, tea.MouseWheelMsg{X: 2, Y: 2, Button: tea.MouseWheelDown})
	}
	if !m.streamState.followBottom {
		t.Fatal("wheeling back to the bottom must resume follow")
	}
}
