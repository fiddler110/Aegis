package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

// driveUpdate is a small helper that applies msg through the real Update
// method (the same path bubbletea uses) and returns the resulting model.
func driveUpdate(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	nm, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned unexpected type %T", next)
	}
	return nm
}

// plainView renders the full screen and strips ANSI styling so assertions
// can match on plain text.
func plainView(m model) string {
	return ansi.Strip(m.renderContent())
}

// TestTUIGuardRetryWithdrawsAnswer (P25.3): a KindGuard event flagged
// guard_retrying arrives after the failed answer has already been flushed to
// the transcript; it must withdraw that answer in place (leaving a dim note)
// so the corrected retry renders as *the* answer, not appended below the
// failed one.
func TestTUIGuardRetryWithdrawsAnswer(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.appendUser("fix the bug", nil)
	m.streaming = true
	m.applyEvent(api.Event{Kind: api.KindText, Text: "PASS. The fix is confirmed working."})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})
	m.refresh()
	if got := plainView(m); !strings.Contains(got, "fix is confirmed working") {
		t.Fatalf("expected the (soon-to-fail) answer on screen first, got:\n%s", got)
	}

	// Guard fails, engine retries: withdraw the failed answer, stream the
	// corrected one.
	m.applyEvent(api.Event{Kind: api.KindGuard, Text: "verdict not recognized", GuardRetrying: true})
	m.applyEvent(api.Event{Kind: api.KindText, Text: "temps.py now parses the CSV value as an int."})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})
	m.applyEvent(api.Event{Kind: api.KindDone})
	m.refresh()

	got := plainView(m)
	if strings.Contains(got, "fix is confirmed working") {
		t.Errorf("failed answer should have been withdrawn from the transcript, got:\n%s", got)
	}
	if !strings.Contains(got, "answer withdrawn") {
		t.Errorf("expected a withdrawal note in place of the failed answer, got:\n%s", got)
	}
	if !strings.Contains(got, "parses the CSV value") {
		t.Errorf("expected the corrected answer to render, got:\n%s", got)
	}
}

// TestTUIFullTurn_NoPTY exercises the exact runtime path a live terminal
// session drives — Update(WindowSizeMsg) -> layout/refresh, a user turn,
// a streamed reply with thinking + a tool call + tool result, turn
// completion, a resize, and a scrollback check — through the block-based
// transcript (TQ1) instead of the old cappedBuffer. This is a substitute for
// interactive PTY verification (tmux/winpty were unavailable in this
// sandbox): it drives the same model.Update/applyEvent/refresh/View methods
// bubbletea itself calls, so a panic or corrupted render here is exactly
// what a real session would hit.
func TestTUIFullTurn_NoPTY(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", Model: "test-model", WorkDir: t.TempDir()})

	// Initial layout, as bubbletea sends on startup.
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if !m.ready {
		t.Fatal("expected model to be ready after WindowSizeMsg")
	}
	if got := plainView(m); !strings.Contains(got, "Aegis") {
		t.Fatalf("expected welcome banner to mention Aegis, got:\n%s", got)
	}

	// Send a user message (mirrors the "enter" key path in Update).
	m.appendUser("what does this function do?", nil)
	m.streaming = true
	m.refresh()
	if got := plainView(m); !strings.Contains(got, "what does this function do?") {
		t.Fatalf("expected the user's message in the transcript, got:\n%s", got)
	}

	// Stream: thinking, then answer text in several chunks (as SSE would
	// deliver token-by-token), a tool call, a tool result, then turn done.
	m.applyEvent(api.Event{Kind: api.KindThinking, Text: "considering the code path"})
	m.applyEvent(api.Event{Kind: api.KindText, Text: "This function "})
	m.applyEvent(api.Event{Kind: api.KindText, Text: "reads a file and "})
	m.applyEvent(api.Event{Kind: api.KindText, Text: "returns its contents.\n\n"})

	toolInput, _ := json.Marshal(map[string]string{"path": "main.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolInput: toolInput})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolResult: "package main\n"})
	m.applyEvent(api.Event{Kind: api.KindText, Text: "Confirmed: it's the file reader."})
	m.applyEvent(api.Event{Kind: api.KindTurnDone, InputTokens: 42, OutputTokens: 17})
	m.refresh()

	got := plainView(m)
	for _, want := range []string{"✻ thought", "reads a file", "read_file", "Confirmed: it's the file reader"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected rendered transcript to contain %q, got:\n%s", want, got)
		}
	}
	// TQ9: thinking is collapsed to a one-line header by default; ctrl+o
	// (toggleThinking) reveals the full text in place, and toggling back
	// re-collapses it.
	if strings.Contains(got, "considering the code path") {
		t.Errorf("expected thinking text to be collapsed by default, got:\n%s", got)
	}
	m.toggleThinking()
	if expanded := plainView(m); !strings.Contains(expanded, "considering the code path") {
		t.Errorf("expected ctrl+o to expand thinking text, got:\n%s", expanded)
	}
	m.toggleThinking()
	if collapsed := plainView(m); strings.Contains(collapsed, "considering the code path") {
		t.Errorf("expected second toggle to re-collapse thinking text, got:\n%s", collapsed)
	}
	if m.transcript.Len() == 0 {
		t.Fatal("expected the transcript to have accumulated items after a full turn")
	}

	// End of stream, mirroring streamClosedMsg's handling.
	m.flushThinking()
	m.flushLiveText()
	m.streaming = false
	m.refresh()

	// Resize mid-conversation must not panic or lose content — each block
	// re-wraps itself lazily against the new width.
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})
	gotAfterResize := plainView(m)
	if !strings.Contains(gotAfterResize, "Confirmed: it's the file reader") {
		t.Fatalf("expected content to survive a resize, got:\n%s", gotAfterResize)
	}

	// A second, much wider resize (simulating a maximize) must also succeed
	// without panicking and without losing earlier content.
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	gotAfterWideResize := plainView(m)
	if !strings.Contains(gotAfterWideResize, "what does this function do?") {
		t.Fatalf("expected the original user turn to survive a second resize, got:\n%s", gotAfterWideResize)
	}
}

// TestTUIQueuedMessageDrain_NoPTY verifies TQ8: a message queued during
// streaming renders as a pending block and auto-sends when the stream closes.
func TestTUIQueuedMessageDrain_NoPTY(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.appendUser("first question", nil)
	m.streaming = true
	m.applyEvent(api.Event{Kind: api.KindText, Text: "answering…"})
	m.queued = append(m.queued, "follow-up question")
	m.refresh()

	if got := plainView(m); !strings.Contains(got, "queued ▸ follow-up question") {
		t.Fatalf("expected queued message rendered as pending block, got:\n%s", got)
	}

	// Stream closes → the queued message becomes the next user turn.
	m = driveUpdate(t, m, streamClosedMsg{})
	if len(m.queued) != 0 {
		t.Fatalf("expected queue drained, got %d", len(m.queued))
	}
	if !m.streaming {
		t.Fatal("expected a new stream to have started for the queued message")
	}
	if got := plainView(m); !strings.Contains(got, "follow-up question") {
		t.Fatalf("expected the queued text as a user turn, got:\n%s", got)
	}

	// An error discards any remaining queue rather than auto-sending into it.
	m.queued = append(m.queued, "never sent")
	m = driveUpdate(t, m, errMsg{err: context.DeadlineExceeded})
	if len(m.queued) != 0 {
		t.Fatal("expected queue cleared after a stream error")
	}
}

// TestTUITimelineSeek_NoPTY verifies the timeline picker's scroll-to-turn
// still works against block indices instead of the old byte offsets.
func TestTUITimelineSeek_NoPTY(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.appendUser("first turn", nil)
	m.applyEvent(api.Event{Kind: api.KindText, Text: "first reply"})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})

	m.appendUser("second turn", nil)
	m.applyEvent(api.Event{Kind: api.KindText, Text: "second reply"})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})
	m.refresh()

	if len(m.timelineEntries) != 2 {
		t.Fatalf("expected 2 timeline entries, got %d", len(m.timelineEntries))
	}
	first := m.timelineEntries[0]
	if first.blockIndex < 0 || first.blockIndex > m.transcript.Len() {
		t.Fatalf("first entry's blockIndex %d out of range [0, %d]", first.blockIndex, m.transcript.Len())
	}
	// The recorded position for turn 1 is captured before anything for that
	// turn is appended, so scrolling to it must land exactly on turn 1's own
	// content, not before or after it.
	m.transcript.ScrollToItem(first.blockIndex)
	view := m.transcript.View()
	if !strings.Contains(view, "first turn") {
		t.Fatalf("expected ScrollToItem(blockIndex) to land on turn 1's own content, got:\n%s", view)
	}
}

// followBottomTestModel builds a model with a small, exactly-known transcript
// pane size, with the welcome-banner content cleared out first so the tests
// below control every line in the pane instead of depending on the welcome
// banner's (unrelated, may-change) line count.
func followBottomTestModel(t *testing.T) model {
	t.Helper()
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.transcript.Reset()
	m.transcript.SetSize(60, 8) // pane height fixed directly, independent of layout()'s fixedH budget
	return m
}

// TestFollowBottomStaysPinnedDuringEventStream_NoPTY (P18.3a) drives a
// streaming reply token-by-token through the real eventMsg path (the same
// one waitForEvent delivers from the daemon's SSE channel) and checks the
// viewport stays pinned to the bottom of every token, not just at spinner-
// tick boundaries, as long as followBottom is true throughout.
func TestFollowBottomStaysPinnedDuringEventStream_NoPTY(t *testing.T) {
	m := followBottomTestModel(t)

	m.appendUser("please write a long answer", nil)
	m.streaming = true
	m.followBottom = true
	m.refresh()
	if !m.transcript.AtBottom() {
		t.Fatal("expected a freshly sent turn to start pinned to the bottom")
	}

	for i := 0; i < 20; i++ {
		m = driveUpdate(t, m, eventMsg(api.Event{
			Kind: api.KindText,
			Text: fmt.Sprintf("streamed line %d of the reply\n", i),
		}))
		if !m.followBottom {
			t.Fatalf("token %d: followBottom unexpectedly cleared mid-stream", i)
		}
		if !m.transcript.AtBottom() {
			t.Fatalf("token %d: expected viewport to stay pinned to the bottom while followBottom is true", i)
		}
	}
	if m.transcript.TotalHeight() <= 8 {
		t.Fatal("expected the streamed reply to exceed one page — otherwise pinned vs. unpinned isn't distinguishable")
	}
}

// TestFollowBottomResumesOnNextEvent_NoPTY (P18.3b) covers the fix's exact
// mechanism: the eventMsg case in Update always returns early, so before
// this fix it never reached the second switch's catch-all
// `m.followBottom = m.transcript.AtBottom()` re-derivation (tui.go, after
// the tea.KeyMsg/MouseWheelMsg cases) — only a spinner tick or another
// key/mouse message could resync followBottom. This reproduces the instant
// right after a user scrolls back down to the bottom mid-stream: the scroll
// position is already exactly at the bottom (AtBottom() is true), but
// followBottom itself hasn't been resynced from that position yet. The fix
// must resync it from the very next streamed token, not wait for a tick.
func TestFollowBottomResumesOnNextEvent_NoPTY(t *testing.T) {
	m := followBottomTestModel(t)

	m.appendUser("please write a long answer", nil)
	m.streaming = true
	m.followBottom = true
	for i := 0; i < 20; i++ {
		m.applyEvent(api.Event{Kind: api.KindText, Text: fmt.Sprintf("streamed line %d of the reply\n", i)})
	}
	m.refresh()
	if m.transcript.TotalHeight() <= 8 {
		t.Fatal("expected streamed content to exceed one page for scrolling to be meaningful")
	}

	// The user scrolls up mid-stream. Scroll directly at the pane level and
	// mirror the model's own catch-all re-derivation (the second switch's
	// `m.followBottom = m.transcript.AtBottom()` in Update) rather than
	// driving a real tea.KeyMsg through model.Update: with streaming active,
	// every keystroke also runs syncCompletion(), which calls out to the
	// (here, nil) daemon client for custom commands — an unrelated
	// pre-existing crash risk in a client-less test model, not anything this
	// fix touches, so it's sidestepped rather than worked around.
	for i := 0; i < 5; i++ {
		m.transcript.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	m.followBottom = m.transcript.AtBottom()
	if m.followBottom {
		t.Fatal("expected followBottom to clear once the user scrolls away from the bottom")
	}
	if m.transcript.AtBottom() {
		t.Fatal("expected the viewport to have actually moved off the bottom")
	}

	// Simulate the instant the user's scroll returns to exactly the bottom,
	// before followBottom has been resynced from that position (isolating
	// the eventMsg fix from the already-working key/mouse re-derivation
	// path, which would otherwise resync followBottom itself and mask the
	// very thing under test here).
	m.transcript.GotoBottom()
	m.followBottom = false
	if !m.transcript.AtBottom() {
		t.Fatal("setup error: expected GotoBottom to land exactly at the bottom")
	}

	m = driveUpdate(t, m, eventMsg(api.Event{Kind: api.KindText, Text: "one more streamed token\n"}))

	if !m.followBottom {
		t.Fatal("expected the next eventMsg to resync followBottom to true from the pre-event scroll position")
	}
	if !m.transcript.AtBottom() {
		t.Fatal("expected the viewport to follow the newly streamed token once followBottom resyncs")
	}

	// Conversely, a token arriving while genuinely scrolled away from the
	// bottom must not force followBottom back on.
	for i := 0; i < 5; i++ {
		m.transcript.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	m.followBottom = m.transcript.AtBottom()
	if m.followBottom || m.transcript.AtBottom() {
		t.Fatal("setup error: expected scrolling up again to leave the bottom")
	}
	m = driveUpdate(t, m, eventMsg(api.Event{Kind: api.KindText, Text: "yet another token\n"}))
	if m.followBottom {
		t.Fatal("expected a token arriving while scrolled away from the bottom to leave followBottom false")
	}
}
