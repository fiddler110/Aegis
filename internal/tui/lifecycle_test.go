package tui

import (
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdPruneInvalidDaysIsUsageError checks the argument-parsing fast path
// that returns before ever touching the daemon client — the client is nil
// here specifically to prove this branch never calls it.
func TestCmdPruneInvalidDaysIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "prune", Args: []string{"not-a-number"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/prune not-a-number`")
	}
}

// TestCmdBGUnknownSubcommand checks an unrecognized /bg subcommand is
// rejected before calling the daemon client.
func TestCmdBGUnknownSubcommand(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "bg", Args: []string{"bogus"}})
	if !res.IsError {
		t.Fatal("expected an error result for an unknown /bg subcommand")
	}
}
