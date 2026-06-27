package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func gitInput(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGitReadStatus(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	tl := &gitTool{root: dir}
	res, err := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"subcommand": "status",
		"args":       []string{"--short"},
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("status returned error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello.txt") {
		t.Errorf("status output missing file: %q", res.Content)
	}
}

func TestGitCommitAndLog(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	commit := &gitCommitTool{root: dir}
	res, err := commit.Execute(context.Background(), gitInput(t, map[string]any{
		"message": "add a.txt",
	}))
	if err != nil {
		t.Fatalf("commit Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("commit failed: %s", res.Content)
	}
	if !strings.Contains(res.Content, "committed") {
		t.Errorf("commit output = %q", res.Content)
	}

	// The commit should appear in the log.
	tl := &gitTool{root: dir}
	logRes, err := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"subcommand": "log",
		"args":       []string{"--oneline"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logRes.Content, "add a.txt") {
		t.Errorf("log missing commit: %q", logRes.Content)
	}
}

func TestGitCommitNothingToCommit(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	commit := &gitCommitTool{root: dir}
	res, err := commit.Execute(context.Background(), gitInput(t, map[string]any{"message": "empty"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content, "nothing to commit") {
		t.Errorf("expected 'nothing to commit' error, got isErr=%v %q", res.IsError, res.Content)
	}
}

func TestGitReadRejectsDisallowed(t *testing.T) {
	dir := t.TempDir()
	tl := &gitTool{root: dir}

	// Subcommand not on the allowlist.
	res, _ := tl.Execute(context.Background(), gitInput(t, map[string]any{"subcommand": "checkout"}))
	if !res.IsError {
		t.Error("expected error for disallowed subcommand 'checkout'")
	}

	// Mutating branch flag.
	res, _ = tl.Execute(context.Background(), gitInput(t, map[string]any{
		"subcommand": "branch", "args": []string{"-D", "main"},
	}))
	if !res.IsError {
		t.Error("expected error for 'branch -D'")
	}

	// Denied global-ish arg.
	res, _ = tl.Execute(context.Background(), gitInput(t, map[string]any{
		"subcommand": "log", "args": []string{"--output=/tmp/x"},
	}))
	if !res.IsError {
		t.Error("expected error for '--output='")
	}

	// stash without list.
	res, _ = tl.Execute(context.Background(), gitInput(t, map[string]any{"subcommand": "stash"}))
	if !res.IsError {
		t.Error("expected error for bare 'stash'")
	}
}

func TestGitCommitSpecificPaths(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("b"), 0o644)

	commit := &gitCommitTool{root: dir}
	res, err := commit.Execute(context.Background(), gitInput(t, map[string]any{
		"message": "only tracked",
		"paths":   []string{"tracked.txt"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("commit failed: %s", res.Content)
	}

	// ignored.txt must still be untracked.
	tl := &gitTool{root: dir}
	st, _ := tl.Execute(context.Background(), gitInput(t, map[string]any{
		"subcommand": "status", "args": []string{"--short"},
	}))
	if !strings.Contains(st.Content, "ignored.txt") {
		t.Errorf("expected ignored.txt to remain uncommitted, status: %q", st.Content)
	}
}
