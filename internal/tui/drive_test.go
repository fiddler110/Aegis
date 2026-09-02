package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/commands"
)

var driveReq = api.DriveRequest{Skill: "threat-modeling", Task: "threat model this repo"}

// TestCmdDriveBuildsRequest covers the general P52.12 entry point: /drive takes
// the skill name as the first token and everything after it as the task, since
// the drive builds each phase's prompt from the skill's own plan and needs the
// task rather than the skill body.
func TestCmdDriveBuildsRequest(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "drive", Args: []string{"threat-modeling", "threat", "model", "this", "repo"}})
	if res.Drive == nil {
		t.Fatal("expected /drive to produce a DriveRequest")
	}
	if res.Drive.Skill != "threat-modeling" {
		t.Errorf("Skill = %q, want %q", res.Drive.Skill, "threat-modeling")
	}
	if res.Drive.Task != "threat model this repo" {
		t.Errorf("Task = %q, want %q", res.Drive.Task, "threat model this repo")
	}
	if res.Message != "" {
		t.Errorf("expected no ordinary message alongside a drive, got: %q", res.Message)
	}
}

// TestCmdDriveRequiresSkillAndTask checks both halves are refused rather than
// sent half-formed: a drive with no task has nothing to build, and the daemon
// would reject it a round trip later.
func TestCmdDriveRequiresSkillAndTask(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	for _, args := range [][]string{nil, {"threat-modeling"}, {"threat-modeling", "   "}} {
		res := d.Dispatch(&commands.ParsedCommand{Name: "drive", Args: args})
		if res.Drive != nil {
			t.Errorf("args %v: expected no DriveRequest, got %+v", args, res.Drive)
		}
		if !res.IsError || !strings.Contains(res.Output, "usage:") {
			t.Errorf("args %v: expected a usage error, got %+v", args, res)
		}
	}
}

// TestCmdThreatModelUnattendedDrives covers the shorthand: with a framework
// already pinned, `unattended` turns /threat-model into a drive instead of the
// interactive skill-injection message. Interactive stays the default — P47.10's
// reasoning that review between phases is valuable still holds.
func TestCmdThreatModelUnattendedDrives(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "/repo/aegis")

	interactive := d.Dispatch(&commands.ParsedCommand{Name: "threat-model", Args: []string{"STRIDE"}})
	if interactive.Drive != nil {
		t.Fatalf("expected /threat-model to stay interactive by default, got a drive: %+v", interactive.Drive)
	}

	res := d.Dispatch(&commands.ParsedCommand{Name: "threat-model", Args: []string{"STRIDE", "unattended"}})
	if res.Drive == nil {
		t.Fatal("expected `unattended` to produce a DriveRequest")
	}
	if res.Drive.Skill != "threat-modeling" {
		t.Errorf("Skill = %q, want %q", res.Drive.Skill, "threat-modeling")
	}
	if !strings.Contains(res.Drive.Task, "Use the STRIDE framework") {
		t.Errorf("expected the pinned framework in the drive task, got: %q", res.Drive.Task)
	}
	if strings.Contains(res.Drive.Task, "unattended") {
		t.Errorf("expected the flag token to be stripped from the task text, got: %q", res.Drive.Task)
	}
	if res.Message != "" {
		t.Errorf("expected no interactive message alongside the drive, got: %q", res.Message)
	}
}

// TestExtractUnattendedFlagAnywhere checks the flag is accepted at either end
// and in each spelling, because the framework/target parsing that follows must
// never see it — a stray "unattended" token would otherwise be parsed as part
// of the scope text.
func TestExtractUnattendedFlagAnywhere(t *testing.T) {
	cases := []struct {
		args     []string
		wantRest string
		wantFlag bool
	}{
		{[]string{"stride", "unattended"}, "stride", true},
		{[]string{"unattended", "stride"}, "stride", true},
		{[]string{"stride", "--unattended", "the", "api"}, "stride the api", true},
		{[]string{"stride", "-u"}, "stride", true},
		{[]string{"stride", "the", "api"}, "stride the api", false},
	}
	for _, c := range cases {
		rest, flag := extractUnattendedFlag(c.args)
		if flag != c.wantFlag {
			t.Errorf("args %v: unattended = %v, want %v", c.args, flag, c.wantFlag)
		}
		if got := strings.Join(rest, " "); got != c.wantRest {
			t.Errorf("args %v: rest = %q, want %q", c.args, got, c.wantRest)
		}
	}
}

// TestThreatModelPickerPreservesUnattended is the regression test for the gap
// between parsing the flag and using it: with no framework named, the command
// opens the picker and returns before the drive branch is reached, so the flag
// has to survive the round trip in the model. The bare "/threat-model
// unattended" form — the common one, since the picker exists precisely so the
// framework need not be typed — silently ran interactively without this.
func TestThreatModelPickerPreservesUnattended(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	target := ""
	m = driveUpdate(t, m, slashResultMsg{ThreatModelTarget: &target, ThreatModelUnattended: true})
	if m.overlays.dialog == nil || m.overlays.dialog.kind != dialogThreatModelPicker {
		t.Fatal("expected the framework picker to open")
	}
	if !m.overlays.pendingThreatModelUnattended {
		t.Fatal("expected the unattended flag to be held while the picker is open")
	}

	next, cmd := m.Update(dialogSelectedMsg{kind: dialogThreatModelPicker, item: frameworkItem{name: "STRIDE"}})
	m = next.(model)
	if m.overlays.pendingThreatModelUnattended {
		t.Error("expected the pending flag to be cleared after selection")
	}
	if cmd == nil {
		t.Fatal("expected selecting a framework to re-dispatch the command")
	}
	res, ok := cmd().(slashResultMsg)
	if !ok {
		t.Fatalf("expected a slashResultMsg from the re-dispatch, got %T", cmd())
	}
	if res.Drive == nil {
		t.Fatalf("expected the re-dispatched command to drive, got message: %q", res.Message)
	}
	if !strings.Contains(res.Drive.Task, "Use the STRIDE framework") {
		t.Errorf("expected the picked framework in the drive task, got: %q", res.Drive.Task)
	}
}

// TestThreatModelPickerCancelClearsUnattended: dismissing the picker must not
// leave the flag armed for whatever opens it next.
func TestThreatModelPickerCancelClearsUnattended(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	target := ""
	m = driveUpdate(t, m, slashResultMsg{ThreatModelTarget: &target, ThreatModelUnattended: true})
	m = driveUpdate(t, m, dialogCancelMsg{kind: dialogThreatModelPicker})
	if m.overlays.pendingThreatModelUnattended {
		t.Error("expected the unattended flag to be cleared when the picker is dismissed")
	}
}

// TestDriveResultStartsStream checks the TUI treats a drive like a turn: the
// task lands in the transcript as the user's message and the model enters the
// streaming state, so the timeline, spinner, and interrupt path all behave as
// they do for an ordinary run.
func TestDriveResultStartsStream(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	next, cmd := m.Update(slashResultMsg{Drive: &driveReq})
	m = next.(model)
	if !m.streamState.streaming {
		t.Error("expected a drive to put the TUI into its streaming state")
	}
	if cmd == nil {
		t.Fatal("expected a command starting the drive")
	}
	if !strings.Contains(plainView(m), "threat model this repo") {
		t.Errorf("expected the drive task to appear in the transcript, got:\n%s", plainView(m))
	}
}
