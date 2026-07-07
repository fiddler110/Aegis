package tui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

func ctrlKeyMsg(r rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func TestExitCodeFromErr(t *testing.T) {
	if got := exitCodeFromErr(nil); got != 0 {
		t.Errorf("nil error: got %d, want 0", got)
	}
	if got := exitCodeFromErr(context.Canceled); got != 1 {
		t.Errorf("generic error: got %d, want 1", got)
	}

	// A real *exec.ExitError, the shape sandbox.LocalBackend actually wraps.
	name, args := "sh", []string{"-c", "exit 7"}
	if err := exec.Command(name, args...).Run(); err == nil {
		t.Skip("sh not available on this platform")
	}
	err := exec.Command(name, args...).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	wrapped := errWrap(err)
	if got := exitCodeFromErr(wrapped); got != 7 {
		t.Errorf("wrapped ExitError: got %d, want 7", got)
	}
}

// errWrap mimics the sandbox backend's "exit error: %w" wrapping.
func errWrap(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(errors.New("exit error"), err)
}

func TestTermPaneTracksRunOutcome(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)

	tp.beginRun("false")
	tp.handleOutput("some output\n")
	tp.handleOutput("more output\n")
	tp.handleDone(errWrap(errors.New("exit status 1")))

	if !tp.lastFailed {
		t.Fatal("expected lastFailed = true after a failing command")
	}
	if tp.lastCmd != "false" {
		t.Errorf("lastCmd = %q, want %q", tp.lastCmd, "false")
	}
	if tp.lastOutput != "some output\nmore output\n" {
		t.Errorf("lastOutput = %q, want the two appended chunks", tp.lastOutput)
	}

	// A successful run afterward must clear the failure state and reset
	// runOutput so a stale failure isn't diagnosed again.
	tp.beginRun("true")
	tp.handleOutput("ok\n")
	tp.handleDone(nil)

	if tp.lastFailed {
		t.Fatal("expected lastFailed = false after a successful command")
	}
	if tp.lastOutput != "ok\n" {
		t.Errorf("lastOutput after success = %q, want %q", tp.lastOutput, "ok\n")
	}
}

func TestTermPaneCanceledRunIsNotAFailure(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	tp.beginRun("sleep 100")
	tp.handleDone(context.Canceled)

	if tp.lastFailed {
		t.Fatal("an interrupted (canceled) run should not be flagged as a failure to diagnose")
	}
}

func TestDiagnoseLastFailureSendsPromptAndClearsState(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.lastFailure = &shellFailure{source: "terminal", command: "go build ./...", output: "undefined: foo", code: 2}
	m.termFocused = true

	before := m.transcript.Len()
	cmd := m.diagnoseLastFailureCmd()
	if cmd == nil {
		t.Fatal("expected a non-nil tea.Cmd to start the diagnostic turn")
	}
	if m.lastFailure != nil {
		t.Error("expected lastFailure to be cleared after triggering diagnose")
	}
	if m.termFocused {
		t.Error("expected diagnose to return focus to the chat input")
	}
	if !m.streaming {
		t.Error("expected diagnose to start a streaming turn")
	}
	if m.transcript.Len() <= before {
		t.Fatal("expected a new user-turn item in the transcript")
	}
	got := m.transcript.View()
	if !strings.Contains(got, "go build ./...") || !strings.Contains(got, "undefined: foo") {
		t.Errorf("expected the failed command and its output in the diagnostic prompt, got:\n%s", got)
	}
}

func TestDiagnoseLastFailureNoopWithoutFailure(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	if cmd := m.diagnoseLastFailureCmd(); cmd != nil {
		t.Error("expected nil cmd when there is no pending failure")
	}
}

func TestDiagnoseKeybindingRoutesThroughTerminalPane(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.termFocused = true
	m.term.lastFailed = true
	m.term.lastCmd = "npm test"
	m.term.lastOutput = "1 failing"
	m.term.lastExitCode = 1
	m.lastFailure = &shellFailure{source: "terminal", command: "npm test", output: "1 failing", code: 1}

	m = driveUpdate(t, m, ctrlKeyMsg('g'))

	if m.termFocused {
		t.Error("expected ctrl+g to return focus to chat after diagnosing")
	}
	if m.lastFailure != nil {
		t.Error("expected lastFailure cleared after ctrl+g")
	}
	got := m.transcript.View()
	if !strings.Contains(got, "npm test") {
		t.Errorf("expected the failed command in the transcript, got:\n%s", got)
	}
}

func TestDiagnoseKeybindingIgnoredWhenNoFailure(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.slash.customs = []api.CommandInfo{} // skip refreshCustoms' daemon call (nil client here)
	before := m.transcript.Len()

	m = driveUpdate(t, m, ctrlKeyMsg('g'))

	if m.streaming {
		t.Error("ctrl+g with no pending failure should not start a turn")
	}
	if m.transcript.Len() != before {
		t.Error("ctrl+g with no pending failure should not touch the transcript")
	}
}

func TestBangMsgFailureIsDiagnosable(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.slash.customs = []api.CommandInfo{} // skip refreshCustoms' daemon call (nil client here)

	m = driveUpdate(t, m, bangMsg{cmd: "go build ./bad", output: "syntax error", code: 2})

	if m.lastFailure == nil {
		t.Fatal("expected a ! command failure to be recorded for diagnosis")
	}
	if m.lastFailure.command != "go build ./bad" || m.lastFailure.code != 2 {
		t.Errorf("lastFailure = %+v, want command=%q code=2", m.lastFailure, "go build ./bad")
	}
	got := m.transcript.View()
	if !strings.Contains(got, "diagnose") {
		t.Errorf("expected a diagnose hint in the transcript after a failed ! command, got:\n%s", got)
	}

	m = driveUpdate(t, m, ctrlKeyMsg('g'))
	if m.lastFailure != nil {
		t.Error("expected lastFailure cleared after ctrl+g diagnoses the ! command failure")
	}
	if !m.streaming {
		t.Error("expected ctrl+g to start a diagnostic turn for the ! command failure")
	}
}

func TestBangMsgSuccessLeavesNoFailureToDiagnose(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m = driveUpdate(t, m, bangMsg{cmd: "echo hi", output: "hi", code: 0})

	if m.lastFailure != nil {
		t.Error("a successful ! command should not set lastFailure")
	}
}
