package builtin

import (
	"context"
	"testing"
)

// TestGitPRToolAlwaysDestructive: git_pr always pushes and opens a PR, an
// irreversible send visible to others — regardless of input. It is the
// seam's real demonstrator (P67.10); write_file deliberately does not
// implement tool.Destroyer — see the comment beside writeTool.Name in
// file.go for why marking every overwrite destructive turned out to trade
// real approval fatigue for protection /rewind already provides.
func TestGitPRToolAlwaysDestructive(t *testing.T) {
	pr := &gitPRTool{root: t.TempDir()}
	if !pr.Destructive(context.Background(), mustJSON(t, map[string]any{"title": "x"})) {
		t.Error("Destructive() = false, want true: git_pr always pushes and opens a PR")
	}
}
