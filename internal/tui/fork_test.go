package tui

import (
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdForkInvalidNumberIsUsageError checks the argument-parsing fast path
// that returns before ever touching the daemon client — the client is nil
// here specifically to prove this branch never calls it, same convention as
// TestCmdScanImageMissingRefIsUsageError.
func TestCmdForkInvalidNumberIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "fork", Args: []string{"abc"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/fork abc`")
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "fork", Args: []string{"0"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/fork 0` (checkpoints are 1-indexed)")
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "fork", Args: []string{"-1"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/fork -1`")
	}
}

// TestForkRegisteredInCommandDefs guards against /fork silently falling out
// of the completion popup / command palette / dispatch table the way
// /rollback, /scan, and others did before P14.1 folded them all into one
// source of truth (commandDefs) — see commands.go's doc comment.
func TestForkRegisteredInCommandDefs(t *testing.T) {
	for _, c := range commandDefs() {
		if c.name == "fork" {
			if c.handler == nil {
				t.Fatal("fork commandDef has a nil handler")
			}
			return
		}
	}
	t.Fatal("fork not found in commandDefs")
}
