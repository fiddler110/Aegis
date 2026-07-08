package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func diffGitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func diffInitRepo(t *testing.T) string {
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

func diffCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestCmdDiff_NoChanges(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")

	d := &SlashDispatcher{workDir: dir}
	res := d.cmdDiff(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if res.Output != "No changes." {
		t.Errorf("Output = %q, want %q", res.Output, "No changes.")
	}
}

func TestCmdDiff_TrackedAndUntracked(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")

	// Modify the tracked file and add a new untracked one.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &SlashDispatcher{workDir: dir}
	res := d.cmdDiff(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.HasPrefix(res.Output, "\x00diff\n") {
		t.Fatalf("expected \\x00diff marker prefix, got %q", res.Output[:min(40, len(res.Output))])
	}
	body := strings.TrimPrefix(res.Output, "\x00diff\n")
	if !strings.Contains(body, "a.txt") {
		t.Errorf("expected diff to mention a.txt (tracked change):\n%s", body)
	}
	if !strings.Contains(body, "b.txt") {
		t.Errorf("expected diff to mention b.txt (untracked file):\n%s", body)
	}
	if !strings.Contains(body, "+hi there") {
		t.Errorf("expected diff to show the modified line:\n%s", body)
	}
	if !strings.Contains(body, "+new") {
		t.Errorf("expected diff to show the untracked file's content:\n%s", body)
	}
}

func TestCmdDiff_Staged(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "a.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &SlashDispatcher{workDir: dir}
	res := d.cmdDiff([]string{"--staged"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	body := strings.TrimPrefix(res.Output, "\x00diff\n")
	if !strings.Contains(body, "staged change") {
		t.Errorf("expected staged diff to show the staged change:\n%s", body)
	}
	if strings.Contains(body, "c.txt") {
		t.Errorf("expected --staged to exclude the untracked file:\n%s", body)
	}
}

func TestCmdDiff_NotAGitRepo(t *testing.T) {
	diffGitAvailable(t)
	dir := t.TempDir()
	d := &SlashDispatcher{workDir: dir}
	res := d.cmdDiff(nil)
	if !res.IsError {
		t.Error("expected an error for a non-git directory")
	}
}

// reviewDispatcher builds a SlashDispatcher already in plan mode, so cmdReview
// never takes its mode-switch branch (which would otherwise call
// d.client.UpdateSession on a nil client and panic) — these tests exercise
// the diff-gathering and prompt-building logic, not the mode-switch itself.
func reviewDispatcher(dir string) *SlashDispatcher {
	return &SlashDispatcher{workDir: dir, mode: "plan"}
}

func TestCmdReview_NoChanges(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")

	d := reviewDispatcher(dir)
	res := d.cmdReview(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if res.Output != "No changes to review." {
		t.Errorf("Output = %q, want %q", res.Output, "No changes to review.")
	}
	if res.Message != "" {
		t.Errorf("expected no Message when there's nothing to review, got %q", res.Message)
	}
}

func TestCmdReview_WorkingTree(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi there\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := reviewDispatcher(dir)
	res := d.cmdReview(nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Message, "content-review skill") {
		t.Errorf("expected the message to invoke the content-review skill:\n%s", res.Message)
	}
	if !strings.Contains(res.Message, "uncommitted working-tree changes") {
		t.Errorf("expected the message to describe the working-tree scope:\n%s", res.Message)
	}
	if !strings.Contains(res.Message, "+hi there") {
		t.Errorf("expected the message to inline the diff:\n%s", res.Message)
	}
	if res.Output != "" {
		t.Errorf("expected no mode-switch note when already in plan mode, got %q", res.Output)
	}
}

func TestCmdReview_Staged(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "a.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	d := reviewDispatcher(dir)
	res := d.cmdReview([]string{"--staged"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Message, "staged change") {
		t.Errorf("expected the message to inline the staged diff:\n%s", res.Message)
	}
	if !strings.Contains(res.Message, "the staged changes") {
		t.Errorf("expected the message to describe the staged scope:\n%s", res.Message)
	}
}

func TestCmdReview_Branch(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")

	cmd := exec.Command("git", "branch", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("main change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "main change")

	d := reviewDispatcher(dir)
	res := d.cmdReview([]string{"feature"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Message, "merge-base") {
		t.Errorf("expected the message to describe a merge-base diff:\n%s", res.Message)
	}
	if !strings.Contains(res.Message, "+main change") {
		t.Errorf("expected the message to inline the diff against the branch:\n%s", res.Message)
	}
}

func TestCmdReview_Commit(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "second commit")

	d := reviewDispatcher(dir)
	res := d.cmdReview([]string{"HEAD"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Message, "commit HEAD") {
		t.Errorf("expected the message to describe a single-commit review:\n%s", res.Message)
	}
	if !strings.Contains(res.Message, "+second") {
		t.Errorf("expected the message to inline the commit's diff:\n%s", res.Message)
	}
}

func TestCmdReview_InvalidRef(t *testing.T) {
	diffGitAvailable(t)
	dir := diffInitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffCommitAll(t, dir, "init")

	d := reviewDispatcher(dir)
	res := d.cmdReview([]string{"no-such-ref"})
	if !res.IsError {
		t.Error("expected an error for an invalid ref")
	}
}

func TestCmdReview_UsageErrorOnConflictingArgs(t *testing.T) {
	d := &SlashDispatcher{workDir: t.TempDir(), mode: "plan"}
	res := d.cmdReview([]string{"--staged", "some-branch"})
	if !res.IsError {
		t.Error("expected a usage error when --staged and a ref are both given")
	}
}

func TestCmdReview_NotAGitRepo(t *testing.T) {
	diffGitAvailable(t)
	dir := t.TempDir()
	d := reviewDispatcher(dir)
	res := d.cmdReview(nil)
	if !res.IsError {
		t.Error("expected an error for a non-git directory")
	}
}
