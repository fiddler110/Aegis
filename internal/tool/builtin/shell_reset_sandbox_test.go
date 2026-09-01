package builtin

import (
	"context"
	"testing"
)

// TestShellResetSandboxThreadsFreshContainer is the P81.22/FIND-22 regression
// for the per-command sandbox reset: the shell tool's reset_sandbox input
// must reach the backend as ExecOpts.FreshContainer, and default to false
// when omitted. Uses the recordingBackend fake declared in workdir_test.go.
func TestShellResetSandboxThreadsFreshContainer(t *testing.T) {
	root := t.TempDir()
	be := &recordingBackend{}
	sh := newShellTool(root, 30, nil, be)

	if _, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{
		"command": "exit 0",
	})); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if be.lastOpts.FreshContainer {
		t.Error("expected FreshContainer=false when reset_sandbox is omitted")
	}

	if _, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{
		"command":       "exit 0",
		"reset_sandbox": true,
	})); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !be.lastOpts.FreshContainer {
		t.Error("expected FreshContainer=true when reset_sandbox is set")
	}
}
