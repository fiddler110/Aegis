package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
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
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// A worktree needs at least one commit to branch from.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestAddListRemove(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	path, err := m.Add("feature-x", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range list {
		if filepath.Base(w.Path) == "feature-x" {
			found = true
		}
	}
	if !found {
		t.Errorf("feature-x not in list: %+v", list)
	}

	if err := m.Remove("feature-x", true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be gone, stat err=%v", err)
	}
}

func TestAddWithBranch(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)
	m, _ := NewManager(repo)
	if _, err := m.Add("wt", "my-branch"); err != nil {
		t.Fatalf("Add with branch: %v", err)
	}
	list, _ := m.List()
	found := false
	for _, w := range list {
		if w.Branch == "my-branch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a worktree on branch my-branch: %+v", list)
	}
}

func TestInvalidName(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)
	m, _ := NewManager(repo)
	for _, bad := range []string{"../escape", "a/b", "", ".."} {
		if _, err := m.Add(bad, ""); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}

func TestNewManagerNonRepo(t *testing.T) {
	gitAvailable(t)
	if _, err := NewManager(t.TempDir()); err == nil {
		t.Error("expected error outside a git repo")
	}
}
