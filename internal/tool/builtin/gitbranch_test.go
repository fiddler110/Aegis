package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// commitSomething gives the repo a HEAD, since git refuses most branch
// operations in a repository with no commits.
func commitSomething(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "seed " + name}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitBranchCreateSwitchListDelete(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	commitSomething(t, dir, "a.txt")
	tl := &gitBranchTool{root: dir}
	start := currentBranch(t, dir)

	// create without switching stays put.
	res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": "create", "name": "feature/x"}))
	if err != nil || res.IsError {
		t.Fatalf("create: err=%v result=%s", err, res.Content)
	}
	if got := currentBranch(t, dir); got != start {
		t.Errorf("create switched branches on its own: now on %q, wanted %q", got, start)
	}

	// switch moves.
	if res, err = tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": "switch", "name": "feature/x"})); err != nil || res.IsError {
		t.Fatalf("switch: err=%v result=%s", err, res.Content)
	}
	if got := currentBranch(t, dir); got != "feature/x" {
		t.Errorf("after switch on %q, wanted feature/x", got)
	}

	// list shows it.
	res, err = tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": "list"}))
	if err != nil || res.IsError {
		t.Fatalf("list: err=%v result=%s", err, res.Content)
	}
	if !strings.Contains(res.Content, "feature/x") {
		t.Errorf("list missing the branch: %q", res.Content)
	}

	// delete a merged branch works, from another branch.
	if res, err = tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": "switch", "name": start})); err != nil || res.IsError {
		t.Fatalf("switch back: err=%v result=%s", err, res.Content)
	}
	if res, err = tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": "delete", "name": "feature/x"})); err != nil || res.IsError {
		t.Fatalf("delete merged: err=%v result=%s", err, res.Content)
	}
}

func TestGitBranchCreateSwitchTrueMovesOntoIt(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	commitSomething(t, dir, "a.txt")
	tl := &gitBranchTool{root: dir}

	res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"operation": "create", "name": "topic", "switch": true,
	}))
	if err != nil || res.IsError {
		t.Fatalf("create+switch: err=%v result=%s", err, res.Content)
	}
	if got := currentBranch(t, dir); got != "topic" {
		t.Errorf("on %q, wanted topic", got)
	}
}

func TestGitBranchCreateFromStartPoint(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	commitSomething(t, dir, "a.txt")
	commitSomething(t, dir, "b.txt")
	tl := &gitBranchTool{root: dir}

	res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"operation": "create", "name": "from-prev", "from": "HEAD~1", "switch": true,
	}))
	if err != nil || res.IsError {
		t.Fatalf("create from HEAD~1: err=%v result=%s", err, res.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Errorf("branch was not cut from HEAD~1: b.txt is present")
	}
}

// The refusal that gives this tool its reason to exist: it must not become a
// way to discard committed work without the operator seeing a command.
func TestGitBranchRefusesToDeleteUnmergedWork(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	commitSomething(t, dir, "a.txt")
	tl := &gitBranchTool{root: dir}
	start := currentBranch(t, dir)

	if res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"operation": "create", "name": "unmerged", "switch": true,
	})); err != nil || res.IsError {
		t.Fatalf("create: err=%v result=%s", err, res.Content)
	}
	commitSomething(t, dir, "only-here.txt")
	if res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"operation": "switch", "name": start,
	})); err != nil || res.IsError {
		t.Fatalf("switch back: err=%v result=%s", err, res.Content)
	}

	res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": "delete", "name": "unmerged"}))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !res.IsError {
		t.Fatalf("deleting an unmerged branch succeeded; -D must never be reachable from this tool: %s", res.Content)
	}
	if !strings.Contains(res.Content, "shell tool") {
		t.Errorf("refusal should name the escape hatch, got %q", res.Content)
	}
	// The branch must still exist.
	out, _ := runGit(context.Background(), dir, "branch", "--list")
	if !strings.Contains(out, "unmerged") {
		t.Errorf("branch was deleted despite the refusal: %q", out)
	}
}

func TestGitBranchRejectsUnsupportedOperation(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	tl := &gitBranchTool{root: dir}
	for _, op := range []string{"merge", "rebase", "reset", "", "DELETE"} {
		res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{"operation": op, "name": "x"}))
		if err != nil {
			t.Fatalf("operation %q: %v", op, err)
		}
		if !res.IsError {
			t.Errorf("operation %q was accepted; the enum is advisory and must be enforced here", op)
		}
	}
}

func TestValidateBranchNameRejectsDangerousShapes(t *testing.T) {
	bad := []string{
		"", "-f", "--force", "-D", "a b", "a\tb", "feat..x", "a@{0}", "a//b",
		"a\\b", "a~1", "a^", "a:b", "a?", "a*", "a[", "/lead", "trail/",
		".hidden", "trail.", "x.lock", "@", "a\x00b", "a\nb",
	}
	for _, name := range bad {
		if err := validateBranchName(name); err == nil {
			t.Errorf("validateBranchName(%q) = nil, wanted rejection", name)
		}
	}
	good := []string{"main", "feature/x", "release-1.2", "user/thing_2", "a.b.c"}
	for _, name := range good {
		if err := validateBranchName(name); err != nil {
			t.Errorf("validateBranchName(%q) = %v, wanted accept", name, err)
		}
	}
}

func TestValidateStartPointAllowsRevisionsButNotFlags(t *testing.T) {
	good := []string{"HEAD~1", "origin/main^", "abc1234", "v1.0", "main@{u}"}
	for _, rev := range good {
		if err := validateStartPoint(rev); err != nil {
			t.Errorf("validateStartPoint(%q) = %v, wanted accept", rev, err)
		}
	}
	bad := []string{"-f", "--force", "a b", "a\\b", "a:b", "a..b", "a\x00"}
	for _, rev := range bad {
		if err := validateStartPoint(rev); err == nil {
			t.Errorf("validateStartPoint(%q) = nil, wanted rejection", rev)
		}
	}
}

// git_branch rewrites the working tree but cannot run an arbitrary binary, so
// it must be CapWrite — Deny in plan mode, Ask in build mode. If this ever
// becomes CapExecute the tool has lost the reason it was added (GAP-04).
func TestGitBranchIsWriteCapability(t *testing.T) {
	tl := &gitBranchTool{root: "."}
	if got := tl.Capability(); got != tool.CapWrite {
		t.Errorf("Capability() = %v, want %v", got, tool.CapWrite)
	}
}
