package builtin

import (
	"context"
	"testing"
)

// TestShellThreadsReadOnlyMountForClassifiedCommands is the P81.10/FIND-10
// regression: a command classifyShellCommand recognizes as read-only must
// reach the sandbox backend as ExecOpts.ReadOnly=true, so a container
// backend can mount the workspace read-only for that one call; an
// unclassified (potentially mutating) command must not.
func TestShellThreadsReadOnlyMountForClassifiedCommands(t *testing.T) {
	root := t.TempDir()
	be := &recordingBackend{}
	sh := newShellTool(root, 30, nil, be)

	if _, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{
		"command": "ls",
	})); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !be.lastOpts.ReadOnly {
		t.Error("expected ReadOnly=true for a classified read-only command")
	}

	if _, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{
		"command": "some-unrecognized-binary --flag",
	})); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if be.lastOpts.ReadOnly {
		t.Error("expected ReadOnly=false for an unclassified command")
	}
}
