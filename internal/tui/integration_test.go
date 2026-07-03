package tui

import (
	"context"
	"encoding/json"
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
	return ansi.Strip(m.render())
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
	m.appendUser("what does this function do?")
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
	if m.transcript.len() == 0 {
		t.Fatal("expected the transcript to have accumulated blocks after a full turn")
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

	m.appendUser("first question")
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

	m.appendUser("first turn")
	m.applyEvent(api.Event{Kind: api.KindText, Text: "first reply"})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})

	m.appendUser("second turn")
	m.applyEvent(api.Event{Kind: api.KindText, Text: "second reply"})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})
	m.refresh()

	if len(m.timelineEntries) != 2 {
		t.Fatalf("expected 2 timeline entries, got %d", len(m.timelineEntries))
	}
	first := m.timelineEntries[0]
	if first.blockIndex < 0 || first.blockIndex > m.transcript.len() {
		t.Fatalf("first entry's blockIndex %d out of range [0, %d]", first.blockIndex, m.transcript.len())
	}
	// The recorded position for turn 1 must render only content up through
	// (not including) turn 1's own text — it's captured before anything for
	// that turn is appended.
	prefix := m.transcript.renderUpTo(first.blockIndex, m.vp.Width())
	if strings.Contains(prefix, "first turn") {
		t.Fatalf("expected renderUpTo(blockIndex) to precede turn 1's own content, got:\n%s", prefix)
	}
}
