package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// escByte is a real ESC; escWire is the six-character JSON escape a provider
// actually puts on the wire for one. Both are assembled from pieces rather
// than written out, so this source file carries no control byte of its own and
// no editor or terminal reading it is at risk of the thing under test.
const (
	escByte = "\x1b"
	escWire = `\u00` + "1b"
	belWire = `\u00` + "07"
)

// sgrOnly matches an SGR sequence — ESC '[' params 'm' — which is the only
// escape the TUI itself emits (lipgloss colours the dialog frame, the diff
// gutter and the option list). Removing those from a render leaves exactly the
// escapes that arrived with the event.
var sgrOnly = regexp.MustCompile(escByte + `\[[0-9;]*m`)

// TestApprovalDialogStripsControlSequencesFromToolInput is P66.6 (SEC-14): the
// approval prompt is the last line of defence a human answers, so a tool call
// whose arguments carry a clear-screen + cursor-home pair must not be able to
// blank or redraw the very question being asked. The fix is at ingestion
// (stream.go's KindApprovalRequest case), which is why one test covers every
// preview branch at once — the write/edit/multi_edit diff path, the shell
// command block, and the compact one-line/JSON-excerpt fallback that everything
// else falls through to.
//
// The closure condition is "no ESC byte anywhere in the approval dialog's
// rendered output". The dialog's own lipgloss styling *is* made of SGR escapes,
// so the assertion runs against the render with those — and only those —
// removed: any ESC still standing is one the event smuggled in.
func TestApprovalDialogStripsControlSequencesFromToolInput(t *testing.T) {
	// Two shapes reach the terminal by different routes. The JSON escape is
	// harmless ASCII on the wire and only becomes a control byte when a
	// renderer unmarshals the arguments — the realistic one. A raw ESC byte in
	// the wire text instead makes the JSON unparseable, dropping the preview
	// into the generic excerpt branch that prints the bytes verbatim.
	clearWire := escWire + "[2J" + escWire + "[H"
	clearRaw := escByte + "[2J" + escByte + "[H"

	cases := []struct {
		name  string
		tool  string
		input string
	}{
		{
			name:  "write_file json-escaped clear screen in content",
			tool:  "write_file",
			input: `{"path":"a.go","content":"package a\n` + clearWire + `harmless-looking\n"}`,
		},
		{
			name:  "write_file raw ESC bytes in content and path",
			tool:  "write_file",
			input: `{"path":"` + clearRaw + `a.go","content":"package a\n` + clearRaw + `\n"}`,
		},
		{
			name:  "write_file OSC title and clipboard strings",
			tool:  "write_file",
			input: `{"path":"a.go","content":"` + escWire + `]0;pwned` + belWire + escWire + `]52;c;cHduZWQ=` + belWire + `"}`,
		},
		{
			name:  "edit_file escape on both diff sides",
			tool:  "edit_file",
			input: `{"path":"a.go","old_string":"one` + clearWire + `","new_string":"two` + clearWire + `"}`,
		},
		{
			name:  "multi_edit escape inside a nested edit",
			tool:  "multi_edit",
			input: `{"edits":[{"path":"a.go","old_string":"one` + clearWire + `","new_string":"two` + clearWire + `"}]}`,
		},
		{
			name:  "shell escape in the command",
			tool:  "shell",
			input: `{"command":"echo ` + clearWire + `"}`,
		},
		{
			name:  "web_fetch escape in the one-line preview",
			tool:  "web_fetch",
			input: `{"url":"https://example.com/` + clearWire + `"}`,
		},
		{
			name:  "unknown tool falls through to the JSON excerpt",
			tool:  "mcp__evil__" + clearRaw + "do_thing",
			input: `{"` + clearWire + `key":"` + clearWire + `value"}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
			m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
			m.streaming = true
			m.applyEvent(api.Event{
				Kind:           api.KindApprovalRequest,
				Tool:           c.tool,
				ToolInput:      []byte(c.input),
				ApprovalReason: "approval required " + clearRaw,
				ApprovalID:     "run-1",
			})
			if m.approval == nil {
				t.Fatal("expected pending approval state")
			}

			for _, r := range []struct{ label, out string }{
				{"approval dialog", m.renderApprovalDialog()},
				{"composited view", m.renderContent()},
			} {
				if left := sgrOnly.ReplaceAllString(r.out, ""); strings.ContainsRune(left, 0x1b) {
					t.Errorf("%s: a model-supplied ESC byte reached the terminal:\n%q", r.label, left)
				}
			}

			// The stored state is sanitized too, so whatever reads it next —
			// the desktop notification body, a preview branch added later —
			// is covered by the same one fix.
			joined := m.approval.toolName + m.approval.input + m.approval.reason + m.approval.pattern
			if strings.ContainsRune(joined, 0x1b) {
				t.Errorf("approvalState still holds an ESC byte: %q", joined)
			}
		})
	}
}

// TestApprovalDialogSanitizationKeepsThePreviewIntact guards P66.6's other
// side: sanitizing at ingestion must not cost the dialog the preview that
// makes it worth answering. The write_file diff, the suggested "allow always"
// rule pattern and the header all still render off the sanitized input.
func TestApprovalDialogSanitizationKeepsThePreviewIntact(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true
	m.applyEvent(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           "write_file",
		ToolInput:      []byte(`{"path":"src/a.go","content":"package a\n` + escWire + `[2Jconst X = 1\n"}`),
		ApprovalReason: "write capability requires approval",
		ApprovalID:     "run-1",
	})
	if got := m.approval.pattern; got != "src/*" {
		t.Errorf("expected the suggested rule pattern to survive sanitization, got %q", got)
	}
	view := plainView(m)
	for _, want := range []string{"write_file", "src/a.go", "package a", "const X = 1", "Allow once"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected the preview to still contain %q, got:\n%s", want, view)
		}
	}
}
