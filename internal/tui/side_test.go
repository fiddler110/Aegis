package tui

import (
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdSideMissingQuestionIsUsageError checks the argument-parsing fast
// path that returns before ever touching the daemon client — the client is
// nil here specifically to prove this branch never calls it, same
// convention as TestCmdDebateMissingClaimIsUsageError and
// TestCmdForkInvalidNumberIsUsageError.
func TestCmdSideMissingQuestionIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "side", Args: nil})
	if !res.IsError {
		t.Fatal("expected an error result for `/side` with no question")
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "side", Args: []string{"   "}})
	if !res.IsError {
		t.Fatal("expected an error result for `/side` with only whitespace")
	}
}

// TestSideRegisteredInCommandDefs guards against /side silently falling out
// of the completion popup / command palette / dispatch table the way
// /rollback, /scan, and others did before P14.1 folded them all into one
// source of truth (commandDefs) — see commands.go's doc comment.
func TestSideRegisteredInCommandDefs(t *testing.T) {
	for _, c := range commandDefs() {
		if c.name == "side" {
			if c.handler == nil {
				t.Fatal("side commandDef has a nil handler")
			}
			return
		}
	}
	t.Fatal("side not found in commandDefs")
}

// TestCmdSideNeverSwitchesOrReloadsMainSession documents the isolation
// contract via the SlashResult shape: a successful /side must never set
// SwitchToSession or ReloadSession, since either would pull the TUI's view
// away from (or mutate) the main session it left untouched. This only
// exercises the usage-error path (no daemon in tests), but pins the
// invariant so a future edit to cmdSide that starts wiring those fields in
// gets caught by inspection/review, not just by looking at the diff.
func TestCmdSideNeverSwitchesOrReloadsMainSession(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "side", Args: nil})
	if res.SwitchToSession != "" {
		t.Fatal("/side must never switch the TUI's active session")
	}
	if res.ReloadSession {
		t.Fatal("/side must never reload the main session")
	}
}
