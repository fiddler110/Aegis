package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

func TestSuggestRulePattern(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"shell two words, subcommand", `{"command":"npm test -v --run foo"}`, "npm test*"},
		{"shell one word", `{"command":"make"}`, "make*"},
		{"shell binary + script argument", `{"command":"python3 temps.py"}`, "python3 *"},
		{"file path", `{"path":"src/engine/loop.go"}`, "src/engine/*"},
		{"windows path", `{"path":"D:\\dev\\aegis\\main.go"}`, `D:\dev\aegis\*`},
		{"bare filename", `{"path":"main.go"}`, "main.go"},
		{"url", `{"url":"https://example.com/a/b"}`, "https://example.com/*"},
		{"unknown", `{"foo":1}`, "*"},
		{"empty", ``, "*"},

		// P25.4b: cd-prefix stripping — the working directory a command
		// happened to run in isn't part of what's being approved, and
		// keeping it produced rules narrow enough to never match again.
		{"cd prefix stripped", `{"command":"cd /private/tmp/x/demo-project && python3 temps.py"}`, "python3 *"},
		{"cd prefix, subcommand kept", `{"command":"cd /tmp && git status"}`, "git status*"},
		{"nested cd prefixes stripped", `{"command":"cd /a && cd /b && ls"}`, "ls*"},
		{"cd prefix only", `{"command":"cd /tmp &&"}`, "*"},

		// P25.4b: env-var prefix stripping.
		{"env prefix stripped", `{"command":"FOO=bar python3 script.py"}`, "python3 *"},
		{"env and cd prefix stripped", `{"command":"cd /tmp && FOO=bar npm test"}`, "npm test*"},

		// P25.4b: shell metacharacters — never suggest a pattern; the
		// dangerous "allow shell(cat >*)" and useless "allow shell(cd
		// ... && python3 temps.py*)" cases both come from here.
		{"redirection refused", `{"command":"cat file.txt > /etc/passwd"}`, ""},
		{"redirection-only refused", `{"command":"cat >out.txt"}`, ""},
		{"pipe refused", `{"command":"ls | grep foo"}`, ""},
		{"substitution refused", "{\"command\":\"echo $(whoami)\"}", ""},
		{"backtick refused", "{\"command\":\"echo `whoami`\"}", ""},
		{"chaining refused", `{"command":"ls; rm -rf /"}`, ""},
		{"background refused", `{"command":"npm test &"}`, ""},
		{"redirection after cd stripped", `{"command":"cd /tmp && cat f > /etc/x"}`, ""},
	}
	for _, c := range cases {
		if got := suggestRulePattern(c.input); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestBuildApproveRequestEmptyPatternNeverAllowsAlways covers P25.4b's other
// half: even if a caller selects "Allow always" for a call whose suggested
// pattern is "" (shell metacharacters present), the resulting request must
// not set AllowAlways/Pattern — sending AllowAlways with no pattern falls
// back to the daemon's whole-tool session cache (sseApprover.Approve),
// which is exactly the unbounded-whitelist hazard this guards against.
func TestBuildApproveRequestEmptyPatternNeverAllowsAlways(t *testing.T) {
	a := &approvalState{id: "run-1", toolName: "shell", pattern: ""}
	req := buildApproveRequest(a, apprAllowAlways)
	if !req.Approved {
		t.Error("expected the call itself to still be approved")
	}
	if req.AllowAlways {
		t.Error("expected AllowAlways to stay false when no safe pattern exists")
	}
	if req.Pattern != "" {
		t.Errorf("expected empty Pattern, got %q", req.Pattern)
	}

	// Sanity check the non-empty case still persists a rule as before.
	a2 := &approvalState{id: "run-2", toolName: "shell", pattern: "npm test*"}
	req2 := buildApproveRequest(a2, apprAllowAlways)
	if !req2.Approved || !req2.AllowAlways || req2.Pattern != "npm test*" {
		t.Errorf("expected allow-always with pattern, got %+v", req2)
	}
}

// TestApprovalCursorPosTracksSelectionAndFeedbackMode checks the P74.7
// cursor for the approval dialog: it sits on the selected option's "▸"
// arrow normally, and moves to the typed-feedback caret once feedbackMode is
// entered — so the real terminal cursor tracks whichever the user is
// actually interacting with.
func TestApprovalCursorPosTracksSelectionAndFeedbackMode(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true
	m.applyEvent(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           "shell",
		ToolInput:      []byte(`{"command":"npm test -v"}`),
		ApprovalReason: "execute capability requires approval",
		ApprovalID:     "run-1",
	})
	if m.approval == nil {
		t.Fatal("expected pending approval state")
	}

	fg := m.renderApprovalDialog()
	_, y1, ok := approvalCursorPos(fg, false)
	if !ok {
		t.Fatal("expected a cursor position on the selected option")
	}

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	fg = m.renderApprovalDialog()
	_, y2, ok := approvalCursorPos(fg, false)
	if !ok {
		t.Fatal("expected a cursor position after moving down")
	}
	if y2 <= y1 {
		t.Errorf("expected cursor row to move down (%d -> %d)", y1, y2)
	}

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !m.approval.feedbackMode {
		t.Fatal("expected feedback mode after 'f'")
	}
	fg = m.renderApprovalDialog()
	if _, _, ok := approvalCursorPos(fg, true); !ok {
		t.Error("expected a cursor position on the feedback caret")
	}

	// render() must translate this into full-frame coordinates.
	_, cur := m.render()
	if cur == nil {
		t.Fatal("expected render() to return a cursor while the approval dialog is open")
	}
	if cur.X < 0 || cur.Y < 0 {
		t.Errorf("expected a non-negative cursor position, got %#v", cur)
	}
}

// TestApprovalDialogFlow_NoPTY drives the option-list approval dialog (TQ6)
// through the real Update path: arrival, navigation, and each answer shape.
func TestApprovalDialogFlow_NoPTY(t *testing.T) {
	newApprovalModel := func() model {
		m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
		m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
		m.streaming = true
		m.applyEvent(api.Event{
			Kind:           api.KindApprovalRequest,
			Tool:           "shell",
			ToolInput:      []byte(`{"command":"npm test -v"}`),
			ApprovalReason: "execute capability requires approval",
			ApprovalID:     "run-1",
		})
		return m
	}

	m := newApprovalModel()
	if m.approval == nil {
		t.Fatal("expected pending approval state")
	}
	if m.approval.pattern != "npm test*" {
		t.Fatalf("expected suggested pattern %q, got %q", "npm test*", m.approval.pattern)
	}

	view := plainView(m)
	for _, want := range []string{"Allow once", "allow shell(npm test*)", "Deny with feedback", "npm test -v"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected approval dialog to contain %q, got:\n%s", want, view)
		}
	}

	// Arrow-key navigation moves the selection and wraps.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.approval.selected != apprAllowAlways {
		t.Fatalf("expected selection to move to allow-always, got %d", m.approval.selected)
	}
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.approval.selected != apprDenyFeedback {
		t.Fatalf("expected selection to wrap to deny-with-feedback, got %d", m.approval.selected)
	}

	// 'f' enters feedback mode; typed runes accumulate; esc backs out.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !m.approval.feedbackMode {
		t.Fatal("expected feedback mode after 'f'")
	}
	for _, r := range "bad" {
		m = driveUpdate(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.approval.feedback != "bad" {
		t.Fatalf("expected typed feedback %q, got %q", "bad", m.approval.feedback)
	}
	if fb := plainView(m); !strings.Contains(fb, "deny — why? bad") {
		t.Errorf("expected feedback prompt in view, got:\n%s", fb)
	}
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.approval.feedbackMode {
		t.Fatal("expected esc to leave feedback mode")
	}

	// 'y' resolves the dialog (the network cmd itself is not executed here).
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.approval != nil {
		t.Fatal("expected approval state cleared after answering")
	}
}

// TestApprovalOverlayLeavesTranscriptGeometryAlone is the P33.6 regression:
// the approval prompt is composited over the chat, so the transcript keeps the
// exact height it had before the engine asked. It used to be inserted between
// transcript and input, shrinking the pane by the whole dialog — preview,
// options and all — and pushing the conversation around mid-run.
func TestApprovalOverlayLeavesTranscriptGeometryAlone(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true

	beforeH, beforeFixed := m.transcript.Height(), m.fixedH()
	beforeChat := lipgloss.Height(m.renderChat())

	m.applyEvent(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           "write_file",
		ToolInput:      []byte(`{"file_path":"a.go","content":"package a\n"}`),
		ApprovalReason: "write capability requires approval",
		ApprovalID:     "run-1",
	})
	m.applyViewportHeight()

	if got := m.transcript.Height(); got != beforeH {
		t.Errorf("transcript height moved with the approval open: %d -> %d", beforeH, got)
	}
	if got := m.fixedH(); got != beforeFixed {
		t.Errorf("fixed vertical budget moved with the approval open: %d -> %d", beforeFixed, got)
	}
	if got := lipgloss.Height(m.renderChat()); got != beforeChat {
		t.Errorf("chat frame height moved with the approval open: %d -> %d", beforeChat, got)
	}

	// The prompt reaches the screen only through the overlay compositor: it is
	// absent from the chat frame itself, present in the composited view.
	if chat := ansi.Strip(m.renderChat()); strings.Contains(chat, "Allow once") {
		t.Error("expected the approval prompt out of the chat frame's vertical stack")
	}
	view := plainView(m)
	if !strings.Contains(view, "Allow once") {
		t.Errorf("expected the approval prompt composited over the chat, got:\n%s", view)
	}
	// Composited, not replacing: the frame behind it is still there.
	if !strings.Contains(view, "Aegis") {
		t.Errorf("expected the chat still visible behind the approval overlay, got:\n%s", view)
	}
	// Modality is unchanged (P25.4a): the composer stays blurred.
	if m.ta.Focused() {
		t.Error("expected the composer blurred while an approval is pending")
	}
	if !strings.Contains(view, "respond to the approval dialog") {
		t.Errorf("expected the status-bar approval hint, got:\n%s", view)
	}
}

// TestApprovalOverlayHeightIsStableAcrossOptionAndFeedbackModes checks the
// other half of P33.6: entering deny-with-feedback swaps the option list for
// the feedback line, which used to re-run applyViewportHeight and resize the
// transcript underneath. Now nothing below the overlay moves.
func TestApprovalOverlayHeightIsStableAcrossOptionAndFeedbackModes(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true
	m.applyEvent(api.Event{
		Kind:       api.KindApprovalRequest,
		Tool:       "shell",
		ToolInput:  []byte(`{"command":"npm test"}`),
		ApprovalID: "run-1",
	})

	before := m.transcript.Height()
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !m.approval.feedbackMode {
		t.Fatal("expected feedback mode after 'f'")
	}
	if got := m.transcript.Height(); got != before {
		t.Errorf("transcript height moved entering feedback mode: %d -> %d", before, got)
	}
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := m.transcript.Height(); got != before {
		t.Errorf("transcript height moved leaving feedback mode: %d -> %d", before, got)
	}
}

// TestApprovalDialogTakesKeyPriorityOverComposer verifies P25.4a: the
// approval dialog consumes hotkeys even while the composer is mid-draft and in
// its streaming queue mode — the state a live dialog opens on top of — and
// that the composer's focus/content is left untouched by the dialog's keys.
func TestApprovalDialogTakesKeyPriorityOverComposer(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true
	m.setQueueMode(true)

	// The composer has focus and a partial draft before the dialog opens —
	// the exact mid-run composer state the live bug report described. Set
	// the draft directly (SetValue) rather than driving a keypress through
	// Update, so the test doesn't depend on the completion popup's daemon
	// client, which is unavailable in this unit test.
	m.ta.SetValue("h")
	if !m.ta.Focused() {
		t.Fatal("expected composer focused before the approval dialog opens")
	}
	if m.ta.Value() != "h" {
		t.Fatalf("expected composer draft %q, got %q", "h", m.ta.Value())
	}

	m.applyEvent(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           "shell",
		ToolInput:      []byte(`{"command":"rm -rf /"}`),
		ApprovalReason: "execute capability requires approval",
		ApprovalID:     "run-2",
	})
	if m.ta.Focused() {
		t.Fatal("expected composer blurred once the approval dialog opens (P25.4a)")
	}

	// Repeated 'y' presses — the reported dead-hotkey symptom — must reach
	// the dialog, not accumulate as text in the still-blurred composer.
	for i := 0; i < 5 && m.approval != nil; i++ {
		m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	}
	if m.approval != nil {
		t.Fatal("expected 'y' to resolve the approval dialog")
	}
	if m.ta.Value() != "h" {
		t.Fatalf("expected composer draft untouched by dialog hotkeys, got %q", m.ta.Value())
	}
	if !m.ta.Focused() {
		t.Fatal("expected composer focus restored after the dialog resolves")
	}
}
