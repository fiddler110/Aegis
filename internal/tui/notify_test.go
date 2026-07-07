package tui

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// TestCmdNotifyBareArgsShowsSentinel mirrors /theme's no-args fast path.
func TestCmdNotifyBareArgsShowsSentinel(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "notify"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if res.Output != "\x00notify-show" {
		t.Errorf("expected show sentinel, got %q", res.Output)
	}
}

func TestCmdNotifyRejectsUnknownMode(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "notify", Args: []string{"loud"}})
	if !res.IsError {
		t.Fatalf("expected an error for an unknown mode, got: %s", res.Output)
	}
}

func TestCmdNotifyAcceptsKnownModes(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	for _, mode := range []string{"off", "bell", "Desktop", "BOTH"} {
		res := d.Dispatch(&commands.ParsedCommand{Name: "notify", Args: []string{mode}})
		if res.IsError {
			t.Fatalf("unexpected error for %q: %s", mode, res.Output)
		}
		if !strings.HasPrefix(res.Output, "\x00notify ") {
			t.Errorf("expected switch sentinel for %q, got %q", mode, res.Output)
		}
	}
}

// TestNotifyModeSwitchLive drives the real Update path to check the
// "\x00notify " sentinel actually flips m.notifyMode and reports it back.
func TestNotifyModeSwitchLive(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	if m.notifyMode != notify.Both {
		t.Fatalf("expected default notify mode %q, got %q", notify.Both, m.notifyMode)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00notify off"})
	if m.notifyMode != notify.Off {
		t.Errorf("expected m.notifyMode to switch to %q, got %q", notify.Off, m.notifyMode)
	}
	if got := plainView(m); !strings.Contains(got, "Notify mode switched to off") {
		t.Errorf("expected confirmation message in transcript, got:\n%s", got)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00notify-show"})
	if got := plainView(m); !strings.Contains(got, "Current notify mode: off") {
		t.Errorf("expected current-mode message in transcript, got:\n%s", got)
	}
}

// TestFocusTrackingSuppressesNotify checks tea.FocusMsg/BlurMsg toggle
// m.focused, and that notifyCmd only fires while unfocused (P16.1).
func TestFocusTrackingSuppressesNotify(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m = driveUpdate(t, m, tea.FocusMsg{})
	if !m.focused {
		t.Fatalf("expected m.focused=true after FocusMsg")
	}
	if cmd := m.notifyCmd(notify.Event{Title: "Aegis", Body: "hi"}); cmd != nil {
		t.Errorf("expected no notify cmd while focused")
	}

	m = driveUpdate(t, m, tea.BlurMsg{})
	if m.focused {
		t.Fatalf("expected m.focused=false after BlurMsg")
	}
	if cmd := m.notifyCmd(notify.Event{Title: "Aegis", Body: "hi"}); cmd == nil {
		t.Errorf("expected a notify cmd while unfocused")
	}
}

// TestNotifyCmdRespectsOffMode checks Off mode never fires even unfocused.
func TestNotifyCmdRespectsOffMode(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m.notifyMode = notify.Off
	if cmd := m.notifyCmd(notify.Event{Title: "Aegis", Body: "hi"}); cmd != nil {
		t.Errorf("expected no notify cmd in Off mode")
	}
}

// TestWindowTitleReflectsState checks the OSC 0/2 title (P16.1) tracks
// streaming/approval/ready state.
func TestWindowTitleReflectsState(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	if got := m.windowTitle(); !strings.Contains(got, "ready") {
		t.Errorf("expected ready title, got %q", got)
	}

	m.streaming = true
	if got := m.windowTitle(); !strings.Contains(got, "working") {
		t.Errorf("expected working title while streaming, got %q", got)
	}
	m.streaming = false

	m.approval = &approvalState{toolName: "shell"}
	if got := m.windowTitle(); !strings.Contains(got, "approval") {
		t.Errorf("expected approval title, got %q", got)
	}
}
