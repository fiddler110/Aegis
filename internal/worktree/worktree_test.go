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

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestAddCarriesDirtyState(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)

	// f.txt is committed by initRepo. Add more committed files so we can dirty
	// and delete them.
	if err := os.WriteFile(filepath.Join(repo, "keep.txt"), []byte("committed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gone.txt"), []byte("committed"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "more")

	// A .gitignore so an ignored untracked file is excluded from the carry.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".gitignore")
	gitRun(t, repo, "commit", "-m", "gitignore")

	// Now dirty the working tree in several ways:
	// - unstaged modification of a tracked file
	if err := os.WriteFile(filepath.Join(repo, "keep.txt"), []byte("MODIFIED"), 0o644); err != nil {
		t.Fatal(err)
	}
	// - staged new file (in a subdir to exercise parent-dir creation)
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "staged.txt"), []byte("STAGED"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", filepath.Join("sub", "staged.txt"))
	// - untracked (non-ignored) file
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("UNTRACKED"), 0o644); err != nil {
		t.Fatal(err)
	}
	// - gitignored untracked file (must NOT be carried)
	if err := os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("IGNORED"), 0o644); err != nil {
		t.Fatal(err)
	}
	// - deleted tracked file
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}
	dest, carried, err := m.AddCarry("wt", "")
	if err != nil {
		t.Fatalf("AddCarry: %v", err)
	}
	if len(carried) == 0 {
		t.Fatal("expected carried files, got none")
	}

	// Modified tracked file reflects the working-tree contents, not HEAD.
	if b, err := os.ReadFile(filepath.Join(dest, "keep.txt")); err != nil || string(b) != "MODIFIED" {
		t.Errorf("keep.txt = %q, %v; want MODIFIED", b, err)
	}
	// Staged new file (with its parent dir) landed.
	if b, err := os.ReadFile(filepath.Join(dest, "sub", "staged.txt")); err != nil || string(b) != "STAGED" {
		t.Errorf("sub/staged.txt = %q, %v; want STAGED", b, err)
	}
	// Untracked non-ignored file landed.
	if b, err := os.ReadFile(filepath.Join(dest, "untracked.txt")); err != nil || string(b) != "UNTRACKED" {
		t.Errorf("untracked.txt = %q, %v; want UNTRACKED", b, err)
	}
	// Gitignored file must NOT be carried.
	if _, err := os.Stat(filepath.Join(dest, "ignored.txt")); !os.IsNotExist(err) {
		t.Errorf("ignored.txt should not be carried, stat err=%v", err)
	}
	// Deleted tracked file must be removed from the fresh checkout.
	if _, err := os.Stat(filepath.Join(dest, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("gone.txt should be deleted in worktree, stat err=%v", err)
	}
}

func TestAddCarriesRename(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "orig.txt"), []byte("content-body-long-enough"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "add orig")
	// Rename via git so it's a staged rename.
	gitRun(t, repo, "mv", "orig.txt", "renamed.txt")

	m, _ := NewManager(repo)
	dest, _, err := m.AddCarry("wt", "")
	if err != nil {
		t.Fatalf("AddCarry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "renamed.txt")); err != nil {
		t.Errorf("renamed.txt missing in worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "orig.txt")); !os.IsNotExist(err) {
		t.Errorf("orig.txt should be gone after rename carry, stat err=%v", err)
	}
}

func TestAddCleanTreeCarriesNothing(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)
	m, _ := NewManager(repo)
	_, carried, err := m.AddCarry("wt", "")
	if err != nil {
		t.Fatalf("AddCarry: %v", err)
	}
	if len(carried) != 0 {
		t.Errorf("clean tree should carry nothing, got %v", carried)
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
