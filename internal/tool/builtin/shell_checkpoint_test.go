package builtin

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fiddler110/aegis/internal/checkpoint"
	_ "modernc.org/sqlite"
)

// echoToFile returns an OS-appropriate shell command that writes content to
// name in the shell tool's working directory — PowerShell on Windows (the
// shell tool's Windows backend, see shell.go), /bin/sh elsewhere.
func echoToFile(name, content string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("Set-Content -Path %q -Value %q", name, content)
	}
	return fmt.Sprintf("printf %%s %q > %s", content, name)
}

// newTestSnapshotter builds a checkpoint store backed by a temp SQLite file
// and returns a Snapshotter plus a ctx carrying it, mirroring the setup in
// agent_test.go's TestAgentToolPropagatesCheckpointIDToSpawn.
func newTestSnapshotter(t *testing.T) (*checkpoint.Snapshotter, *checkpoint.Store, context.Context) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := checkpoint.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	snap := store.NewSnapshotter("cp-shell-test")
	ctx := checkpoint.WithSnapshotter(context.Background(), snap)
	return snap, store, ctx
}

// TestShellCheckpointCapturesNewFile is the scaffold.py scenario: a shell
// subprocess creates a brand-new file directly, bypassing write_file's own
// Snapshotter.Capture call. Without shell_checkpoint.go's git-status
// bracketing, /rewind would have no record of the file and could not delete
// it on restore.
func TestShellCheckpointCapturesNewFile(t *testing.T) {
	gitAvailable(t)
	root := initRepo(t)
	// initRepo has no commits yet; give it one so `git status` has a HEAD to
	// diff against (mirrors any real workspace this fix targets).
	mustGit(t, root, "commit", "--allow-empty", "-m", "init")

	snap, store, ctx := newTestSnapshotter(t)

	sh := newShellTool(root, 30, nil, nil)
	res, err := sh.Execute(ctx, mustJSON(t, map[string]any{
		"command": echoToFile("scaffolded.md", "# stub"),
	}))
	if err != nil || res.IsError {
		t.Fatalf("shell: %v %+v", err, res)
	}

	newPath := filepath.Join(root, "scaffolded.md")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file to exist after shell command: %v", err)
	}

	if n, err := store.RestoreFiles(ctx, snap.CheckpointID()); err != nil || n == 0 {
		t.Fatalf("RestoreFiles() = %d, %v; want the new file captured and restored (deleted)", n, err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("scaffolded.md still exists after rewind, want it deleted (err=%v)", err)
	}
}

// TestShellCheckpointCapturesModifiedTrackedFile covers a shell command that
// overwrites an existing, clean, git-tracked file (e.g. a codegen/formatter
// step). The pre-image isn't on disk to read after the fact, so capture must
// come from `git show HEAD:<path>`.
func TestShellCheckpointCapturesModifiedTrackedFile(t *testing.T) {
	gitAvailable(t)
	root := initRepo(t)
	original := "original content\n"
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "add", "tracked.txt")
	mustGit(t, root, "commit", "-m", "add tracked.txt")

	snap, store, ctx := newTestSnapshotter(t)

	sh := newShellTool(root, 30, nil, nil)
	res, err := sh.Execute(ctx, mustJSON(t, map[string]any{
		"command": echoToFile("tracked.txt", "overwritten content"),
	}))
	if err != nil || res.IsError {
		t.Fatalf("shell: %v %+v", err, res)
	}

	trackedPath := filepath.Join(root, "tracked.txt")
	data, err := os.ReadFile(trackedPath)
	if err != nil || string(data) == original {
		t.Fatalf("expected tracked.txt to be overwritten before restore, got %q, err=%v", data, err)
	}

	if n, err := store.RestoreFiles(ctx, snap.CheckpointID()); err != nil || n == 0 {
		t.Fatalf("RestoreFiles() = %d, %v; want the modified file captured and restored", n, err)
	}
	data, err = os.ReadFile(trackedPath)
	if err != nil {
		t.Fatalf("read after restore: %v", err)
	}
	if string(data) != original {
		t.Errorf("tracked.txt after rewind = %q, want original %q", data, original)
	}
}

// TestShellCheckpointNoopOutsideGitRepo confirms the fix degrades safely to
// today's pre-existing behavior (no capture, no error) outside a git working
// tree, where there is no HEAD to recover a modified file's pre-image from.
func TestShellCheckpointNoopOutsideGitRepo(t *testing.T) {
	root := t.TempDir() // not a git repo

	_, store, ctx := newTestSnapshotter(t)
	snap := checkpoint.SnapshotterFrom(ctx)

	sh := newShellTool(root, 30, nil, nil)
	res, err := sh.Execute(ctx, mustJSON(t, map[string]any{
		"command": echoToFile("newfile.txt", "hello"),
	}))
	if err != nil || res.IsError {
		t.Fatalf("shell: %v %+v", err, res)
	}
	if _, statErr := os.Stat(filepath.Join(root, "newfile.txt")); statErr != nil {
		t.Fatalf("expected file to exist after shell command: %v", statErr)
	}

	n, err := store.RestoreFiles(ctx, snap.CheckpointID())
	if err != nil {
		t.Fatalf("RestoreFiles: %v", err)
	}
	if n != 0 {
		t.Errorf("RestoreFiles() = %d, want 0 (no git repo, nothing capturable)", n)
	}
	if _, statErr := os.Stat(filepath.Join(root, "newfile.txt")); statErr != nil {
		t.Errorf("newfile.txt should still exist (nothing was captured to restore): %v", statErr)
	}
}

// mustGit runs a git subcommand in dir, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestShellCheckpointWorkspaceInsideLargerRepo is P66.15: `git status
// --porcelain` reports paths relative to the *repository* root, not to the
// directory git ran in, so a workspace that is a subdirectory of a larger repo
// used to join those paths onto the wrong base. Two things went wrong at once
// — the command's real writes were never captured (so /rewind silently
// restored nothing), and a bogus path under the workspace could be recorded
// instead and written back on restore.
func TestShellCheckpointWorkspaceInsideLargerRepo(t *testing.T) {
	gitAvailable(t)
	repo := initRepo(t)
	root := filepath.Join(repo, "app") // the workspace: a subdirectory
	sibling := filepath.Join(repo, "other")
	for _, d := range []string{root, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	original := "original content\n"
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	siblingBefore := "sibling content\n"
	if err := os.WriteFile(filepath.Join(sibling, "keep.txt"), []byte(siblingBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-m", "init")

	snap, store, ctx := newTestSnapshotter(t)

	sh := newShellTool(root, 30, nil, nil)
	res, err := sh.Execute(ctx, mustJSON(t, map[string]any{
		"command": echoToFile("tracked.txt", "overwritten content"),
	}))
	if err != nil || res.IsError {
		t.Fatalf("shell: %v %+v", err, res)
	}

	if n, err := store.RestoreFiles(ctx, snap.CheckpointID()); err != nil || n == 0 {
		t.Fatalf("RestoreFiles() = %d, %v; want the subdirectory workspace's write captured", n, err)
	}
	got, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil {
		t.Fatalf("read after restore: %v", err)
	}
	if string(got) != original {
		t.Errorf("app/tracked.txt after rewind = %q, want original %q", got, original)
	}
	// Nothing may be created at the doubled-up path the wrong join addressed.
	if _, err := os.Stat(filepath.Join(root, "app")); !os.IsNotExist(err) {
		t.Errorf("rewind created app/app/... from a repo-root-relative path (err=%v)", err)
	}
	// A sibling directory in the same repo is outside the workspace and must
	// be left alone entirely — RestoreFiles has no root of its own to check.
	if data, err := os.ReadFile(filepath.Join(sibling, "keep.txt")); err != nil || string(data) != siblingBefore {
		t.Errorf("sibling file changed by a workspace rewind: %q, err=%v", data, err)
	}
}
