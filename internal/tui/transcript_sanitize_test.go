package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// escSeq assembles a clear-screen + cursor-home pair from pieces, for the same
// reason approval_sanitize_test.go does: this source file carries no control
// byte of its own.
func escSeq() string { return escByte + "[2J" + escByte + "[H" }

// noSmuggledESC fails when out still contains an ESC byte after the TUI's own
// SGR styling is removed — lipgloss colours the transcript, so SGR is expected;
// anything else arrived with the event.
func noSmuggledESC(t *testing.T, label, out string) {
	t.Helper()
	if left := sgrOnly.ReplaceAllString(out, ""); strings.ContainsRune(left, 0x1b) {
		t.Errorf("%s: an escape sequence reached the terminal:\n%q", label, left)
	}
}

// TestTranscriptStripsControlSequencesFromToolCalls is P66.15's answer to the
// question P66.6 left open: the approval dialog was not the only ingestion
// point. Every tool call is *also* drawn into the transcript — before approval
// on an auto-approved tool, and again on every history reload — through
// renderToolCall, whose diff previews hand model-supplied file content to
// renderLinesBlock, which strips nothing. In build/auto mode a write to an
// already-allowed path never opens the dialog at all, so the transcript is the
// only place those bytes are rendered.
func TestTranscriptStripsControlSequencesFromToolCalls(t *testing.T) {
	clear := escSeq()
	cases := []struct {
		name  string
		tool  string
		input string
	}{
		{"write_file content", "write_file", `{"path":"a.go","content":"package a\n` + escWire + `[2Jgone\n"}`},
		{"write_file raw ESC in path", "write_file", `{"path":"` + clear + `a.go","content":"x"}`},
		{"write_file OSC clipboard", "write_file", `{"path":"a.go","content":"` + escWire + `]52;c;cHduZWQ=` + belWire + `"}`},
		{"edit_file both diff sides", "edit_file", `{"path":"a.go","old_string":"one` + escWire + `[2J","new_string":"two` + escWire + `[2J"}`},
		{"multi_edit nested edit", "multi_edit", `{"edits":[{"path":"a.go","old_string":"one` + escWire + `[2J","new_string":"two` + escWire + `[2J"}]}`},
		{"shell command", "shell", `{"command":"echo ` + escWire + `]0;pwned` + belWire + `"}`},
		{"generic JSON excerpt", "mcp__evil__" + clear + "do_thing", `{"` + clear + `k":"` + clear + `v"}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
			m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
			m.streamState.streaming = true
			m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: c.tool, ToolID: "t1"})
			m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: c.tool, ToolID: "t1", ToolInput: []byte(c.input)})
			m.refresh()
			noSmuggledESC(t, "pending tool card", m.renderContent())

			m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: c.tool, ToolID: "t1", ToolResult: "done"})
			m.refresh()
			noSmuggledESC(t, "finished tool card", m.renderContent())
		})
	}
}

// TestTranscriptToolCallSanitizationKeepsThePreview is the other side of the
// fix above: the diff preview is the reason the card is worth drawing, and it
// must survive sanitization intact.
func TestTranscriptToolCallSanitizationKeepsThePreview(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streamState.streaming = true
	m.applyEvent(api.Event{
		Kind:      api.KindToolCall,
		Tool:      "write_file",
		ToolID:    "t1",
		ToolInput: []byte(`{"path":"src/a.go","content":"package a\n` + escWire + `[2Jconst X = 1\n"}`),
	})
	m.refresh()
	view := plainView(m)
	for _, want := range []string{"write_file", "src/a.go", "package a", "const X = 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected the transcript preview to still contain %q, got:\n%s", want, view)
		}
	}
}

// TestTranscriptStripsControlSequencesFromGuardAndError covers the two other
// event fields that reach the terminal through lipgloss alone. A guard reason
// is the judge *model's* own words (guard.parseVerdict returns whatever it
// wrote after the verdict keyword), and an error string routinely quotes a
// provider or a subprocess.
func TestTranscriptStripsControlSequencesFromGuardAndError(t *testing.T) {
	clear := escSeq()
	for _, c := range []struct {
		name string
		ev   api.Event
	}{
		{"guard failure reason", api.Event{Kind: api.KindGuard, Text: "answer is incomplete " + clear}},
		{"guard retry reason", api.Event{Kind: api.KindGuard, Text: "rubric miss " + clear, GuardRetrying: true}},
		{"stream error", api.Event{Kind: api.KindError, Error: "provider said " + clear}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
			m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
			m.streamState.streaming = true
			m.applyEvent(c.ev)
			m.refresh()
			noSmuggledESC(t, c.name, m.renderContent())
		})
	}
}

// TestSlashOutputStripsDangerousSequences covers the third ingestion point:
// a slash command's plain-text tail. /scan network prints nmap's service
// banners — bytes a scanned host chose — and /scan path prints whatever a
// scanner wrote into a finding title, both straight into this field.
//
// The policy here is stripDangerousSeqs rather than stripControlSeqs: a diff
// or a colourized report legitimately carries SGR, and SGR cannot move the
// cursor, retitle the window or write the clipboard.
func TestSlashOutputStripsDangerousSequences(t *testing.T) {
	hostile := "PORT 22 open " + escByte + "]52;c;cHduZWQ=" + escByte + "\\" + escByte + "[2J"
	for _, c := range []struct {
		name string
		msg  slashResultMsg
	}{
		{"transcript tail", slashResultMsg{Output: hostile}},
		{"error tail", slashResultMsg{Output: hostile, IsError: true}},
		{"transient panel", slashResultMsg{Output: hostile, Transient: true, TransientTitle: "scan"}},
		{"diff sentinel", slashResultMsg{Output: "\x00diff\n--- a\n+++ b\n+" + hostile}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
			m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
			m = driveUpdate(t, m, c.msg)
			noSmuggledESC(t, c.name, m.renderContent())
			if !strings.Contains(plainView(m), "PORT 22 open") {
				t.Errorf("%s: the report text itself should survive, got:\n%s", c.name, plainView(m))
			}
		})
	}
}

// TestBangOutputStripsDangerousSequences: the user chose the `!` command, but
// not what it printed. The shell *tool*'s output already gets this strip on the
// way to the same pane (P28.1).
func TestBangOutputStripsDangerousSequences(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = driveUpdate(t, m, bangMsg{cmd: "cat notes.txt", output: "hello " + escByte + "]0;pwned" + escByte + "\\"})
	noSmuggledESC(t, "bang output", m.renderContent())
	if !strings.Contains(plainView(m), "hello") {
		t.Errorf("expected the command's own output to survive, got:\n%s", plainView(m))
	}
}
