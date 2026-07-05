package tui

import (
	"testing"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdKnowledgeBareArgsIsUsageError checks the no-subcommand fast path
// returns a usage error before ever touching the daemon client — the client
// is nil here specifically to prove this branch never calls it.
func TestCmdKnowledgeBareArgsIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "knowledge"})
	if !res.IsError {
		t.Fatal("expected an error result for bare `/knowledge`")
	}
}

// TestCmdKnowledgeQueryMissingTextIsUsageError checks `/knowledge query` with
// no search text is rejected before calling the daemon client.
func TestCmdKnowledgeQueryMissingTextIsUsageError(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "knowledge", Args: []string{"query"}})
	if !res.IsError {
		t.Fatal("expected an error result for `/knowledge query` with no text")
	}
}

// TestCmdKnowledgeUnknownSubcommand checks an unrecognized subcommand is
// rejected before calling the daemon client.
func TestCmdKnowledgeUnknownSubcommand(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "knowledge", Args: []string{"bogus"}})
	if !res.IsError {
		t.Fatal("expected an error result for an unknown /knowledge subcommand")
	}
}
