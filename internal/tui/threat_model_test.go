package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/commands"
)

func TestExtractThreatModelFramework(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantFW   string
		wantRest []string
	}{
		{"none", []string{"the", "auth", "service"}, "", []string{"the", "auth", "service"}},
		{"empty", nil, "", nil},
		{"bare framework", []string{"PASTA"}, "PASTA", []string{}},
		{"framework lowercase", []string{"pasta"}, "PASTA", []string{}},
		{"framework plus scope", []string{"PASTA", "the", "auth", "service"}, "PASTA", []string{"the", "auth", "service"}},
		{"nist alone", []string{"nist"}, "NIST 800-154", []string{}},
		{"nist plus number", []string{"nist", "800-154", "the", "api"}, "NIST 800-154", []string{"the", "api"}},
		{"nist hyphenated single token", []string{"NIST-800-154"}, "NIST 800-154", []string{}},
		{"trike", []string{"trike"}, "Trike", []string{}},
		{"vast", []string{"VAST", "billing"}, "VAST", []string{"billing"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fw, rest := extractThreatModelFramework(c.args)
			if fw != c.wantFW {
				t.Errorf("framework = %q, want %q", fw, c.wantFW)
			}
			if strings.Join(rest, " ") != strings.Join(c.wantRest, " ") {
				t.Errorf("rest = %v, want %v", rest, c.wantRest)
			}
		})
	}
}

func TestCmdThreatModelUsesGivenFramework(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "threat-model", Args: []string{"PASTA", "the", "auth", "service"}})
	if !strings.Contains(res.Message, "Use the PASTA framework") {
		t.Errorf("expected message to pin the PASTA framework, got: %q", res.Message)
	}
	if !strings.Contains(res.Message, "for the auth service") {
		t.Errorf("expected message to scope to the given target, got: %q", res.Message)
	}
	if strings.Contains(res.Message, "ask me which one to use") {
		t.Errorf("expected the clarifying-question text to be skipped when a framework is given, got: %q", res.Message)
	}
}

// TestCmdThreatModelDefaultsToWorkspace checks that omitting a target scopes
// to the workspace Aegis is actually running against — named explicitly, with
// its path when known — rather than the vague "this project" wording, since
// that's the common case (threat-model the app/codebase in front of you).
func TestCmdThreatModelDefaultsToWorkspace(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "/repo/aegis")
	res := d.Dispatch(&commands.ParsedCommand{Name: "threat-model", Args: []string{"STRIDE"}})
	if !strings.Contains(res.Message, "the application and codebase in the current workspace") {
		t.Errorf("expected message to name the workspace explicitly, got: %q", res.Message)
	}
	if !strings.Contains(res.Message, "/repo/aegis") {
		t.Errorf("expected message to include the workspace path, got: %q", res.Message)
	}
}

// TestCmdThreatModelScopedTargetStillAnchorsToWorkspace checks a named
// system/feature is still framed as being inside the current workspace, not
// an unscoped/external system.
func TestCmdThreatModelScopedTargetStillAnchorsToWorkspace(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "/repo/aegis")
	res := d.Dispatch(&commands.ParsedCommand{Name: "threat-model", Args: []string{"STRIDE", "the", "auth", "service"}})
	if !strings.Contains(res.Message, "the auth service, scoped to the current workspace") {
		t.Errorf("expected message to anchor the scoped target to the workspace, got: %q", res.Message)
	}
	if !strings.Contains(res.Message, "/repo/aegis") {
		t.Errorf("expected message to include the workspace path, got: %q", res.Message)
	}
}

func TestCmdThreatModelNoFrameworkOpensPicker(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "threat-model", Args: []string{"the", "auth", "service"}})
	if res.ThreatModelTarget == nil {
		t.Fatal("expected ThreatModelTarget to be set to open the framework picker when no framework is given")
	}
	if *res.ThreatModelTarget != "the auth service" {
		t.Errorf("ThreatModelTarget = %q, want %q", *res.ThreatModelTarget, "the auth service")
	}
	if res.Message != "" {
		t.Errorf("expected no message to be sent before a framework is chosen, got: %q", res.Message)
	}
}

// TestThreatModelPickerFlow drives the full TUI-level round trip: a
// slashResultMsg carrying ThreatModelTarget opens the picker with all six
// frameworks described, and selecting one re-dispatches /threat-model with
// the framework pinned and the original target preserved.
func TestThreatModelPickerFlow(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	target := "the auth service"
	m = driveUpdate(t, m, slashResultMsg{ThreatModelTarget: &target})
	if m.dialog == nil {
		t.Fatal("expected ThreatModelTarget to open a dialog")
	}
	if m.dialog.kind != dialogThreatModelPicker {
		t.Fatalf("expected dialogThreatModelPicker, got %v", m.dialog.kind)
	}
	items := m.dialog.list.Items()
	if len(items) != len(threatModelFrameworks) {
		t.Fatalf("expected %d framework items, got %d", len(threatModelFrameworks), len(items))
	}
	for _, it := range items {
		fi := it.(frameworkItem)
		if fi.Description() == "" {
			t.Errorf("framework %q has no description in the picker", fi.name)
		}
	}

	m = driveUpdate(t, m, dialogSelectedMsg{kind: dialogThreatModelPicker, item: frameworkItem{name: "NIST 800-154"}})
	if m.dialog != nil {
		t.Error("expected dialog to close after selection")
	}
	if m.pendingThreatModelTarget != "" {
		t.Error("expected pendingThreatModelTarget to be cleared after selection")
	}
}
