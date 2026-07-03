package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

func TestSuggestRulePattern(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"shell two words", `{"command":"npm test -v --run foo"}`, "npm test*"},
		{"shell one word", `{"command":"make"}`, "make*"},
		{"file path", `{"path":"src/engine/loop.go"}`, "src/engine/*"},
		{"windows path", `{"path":"D:\\dev\\aegis\\main.go"}`, `D:\dev\aegis\*`},
		{"bare filename", `{"path":"main.go"}`, "main.go"},
		{"url", `{"url":"https://example.com/a/b"}`, "https://example.com/*"},
		{"unknown", `{"foo":1}`, "*"},
		{"empty", ``, "*"},
	}
	for _, c := range cases {
		if got := suggestRulePattern(c.input); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
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
