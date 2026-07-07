package tui

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdModelBareArgsShowsCurrentModel checks the no-args fast path returns
// the current model without touching the daemon client — the client is nil
// here specifically to prove this branch never calls it.
func TestCmdModelBareArgsShowsCurrentModel(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "model"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "test-model") {
		t.Errorf("expected current model in output, got: %s", res.Output)
	}
}
