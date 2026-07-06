package tui

import (
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdScanImageMissingRefIsUsageError checks the argument-parsing fast
// path that returns before ever touching the daemon client — the client is
// nil here specifically to prove this branch never calls it.
func TestCmdScanImageMissingRefIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "scan", Args: []string{"image"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/scan image` with no ref")
	}
}

// TestCmdScanNetworkMissingTargetIsUsageError mirrors
// TestCmdScanImageMissingRefIsUsageError for the P13.5 `/scan network` branch:
// a bare `/scan network` with no target list must fail before ever touching
// the daemon client.
func TestCmdScanNetworkMissingTargetIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "scan", Args: []string{"network"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/scan network` with no target")
	}
}
