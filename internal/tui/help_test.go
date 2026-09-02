package tui

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestSlashCommandsAreListedInHelp and TestSlashCommandsHaveDetailedHelp guard
// against the exact drift found and fixed here: /rollback, /timeline,
// /sidebar, and /copy were all real, dispatchable commands that had silently
// fallen out of sync with the help system (missing from the general listing
// and/or falling through builtinHelp's default "No help available" case).
// "quit" is a deliberate exception in both — it's a bare alias for "exit",
// documented and listed only under that name to avoid a duplicate line.

func TestSlashCommandsAreListedInHelp(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	d.customs = []api.CommandInfo{} // skip refreshCustoms' daemon call (nil client here)
	res := d.cmdHelp(nil)
	for name := range d.builtins {
		if name == "quit" {
			continue
		}
		if !strings.Contains(res.Output, "/"+name) {
			t.Errorf("/%s is missing from the general /help listing", name)
		}
	}
}

func TestSlashCommandsHaveDetailedHelp(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	for name := range d.builtins {
		if name == "quit" {
			continue
		}
		if got := builtinHelp(name); got == "No help available for /"+name {
			t.Errorf("/help %s falls through to the default case — add one to builtinHelp", name)
		}
	}
}

// TestRewindAndRollbackHelpMentionUndo guards the discoverability fix: a
// first-time user is far more likely to type /rewind or /rollback bare, or
// stumble on the Esc-Esc backtrack picker, than to connect the two — both
// detailed-help entries must call out that this is Aegis's undo and
// cross-reference the other path to the same checkpoints.
func TestRewindAndRollbackHelpMentionUndo(t *testing.T) {
	for _, name := range []string{"rewind", "rollback"} {
		got := builtinHelp(name)
		if !strings.Contains(got, "undo") {
			t.Errorf("/help %s should mention it is Aegis's undo, got:\n%s", name, got)
		}
		if !strings.Contains(got, "Esc twice") {
			t.Errorf("/help %s should cross-reference the Esc-Esc backtrack picker, got:\n%s", name, got)
		}
	}
}

// TestHelpListsKeyboardShortcuts guards P14.9: several features (terminal
// pane, sub-agent list, session switcher, thinking expand/collapse) are
// keybind-only with no slash-command equivalent, so /help's general listing
// must also surface the keymap, not just slash commands.
func TestHelpListsKeyboardShortcuts(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	d.customs = []api.CommandInfo{}
	res := d.cmdHelp(nil)
	for _, e := range defaultKeyMap().helpEntries() {
		if !strings.Contains(res.Output, e.Key) {
			t.Errorf("expected keybinding %q in /help output", e.Key)
		}
	}
}
