package tui

import (
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdDebateMissingClaimIsUsageError checks the argument-parsing fast path
// that returns before ever touching the daemon client — the client is nil
// here specifically to prove this branch never calls it.
func TestCmdDebateMissingClaimIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "debate", Args: nil})
	if !res.IsError {
		t.Fatal("expected an error result for `/debate` with no claim")
	}
}
