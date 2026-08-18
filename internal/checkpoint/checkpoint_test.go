package checkpoint

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st
}

func TestSnapshotAndRestore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	existing := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(existing, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "b.txt") // does not exist yet

	cp, err := st.Create(ctx, "s1", 2, "do the thing", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	snap := st.NewSnapshotter(cp.ID)
	snap.Capture(existing) // captures "original"
	snap.Capture(created)  // records as newly created

	// Simulate the turn's writes.
	if err := os.WriteFile(existing, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capturing again after modification must not overwrite the first snapshot.
	snap.Capture(existing)

	n, err := st.RestoreFiles(ctx, cp.ID)
	if err != nil {
		t.Fatalf("RestoreFiles: %v", err)
	}
	if n != 2 {
		t.Fatalf("restored %d files, want 2", n)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Errorf("existing file = %q, want %q", got, "original")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("created file should have been removed, stat err = %v", err)
	}
}

func TestListAndGet(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	os.WriteFile(f, []byte("v1"), 0o644)

	cp, err := st.Create(ctx, "s1", 0, "first turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(f)
	// A checkpoint for a different session must not appear in s1's list.
	if _, err := st.Create(ctx, "s2", 0, "other", dir); err != nil {
		t.Fatal(err)
	}

	list, err := st.List(ctx, "s1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", list[0].FileCount)
	}
	if list[0].Label != "first turn" {
		t.Errorf("Label = %q", list[0].Label)
	}

	got, err := st.Get(ctx, cp.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SessionID != "s1" || got.Seq != 0 {
		t.Errorf("Get returned %+v", got)
	}

	if _, err := st.Get(ctx, "missing"); err != ErrNotFound {
		t.Errorf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestDeleteForSession(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	os.WriteFile(f, []byte("v1"), 0o644)

	cp, _ := st.Create(ctx, "s1", 0, "t", dir)
	st.NewSnapshotter(cp.ID).Capture(f)

	if err := st.DeleteForSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteForSession: %v", err)
	}
	list, _ := st.List(ctx, "s1")
	if len(list) != 0 {
		t.Errorf("after delete, len(list) = %d, want 0", len(list))
	}
	// Files for the checkpoint should be gone too.
	files, _ := st.files(ctx, cp.ID)
	if len(files) != 0 {
		t.Errorf("orphan files remain: %d", len(files))
	}
}

func TestNilSnapshotterCapture(t *testing.T) {
	var s *Snapshotter
	s.Capture("/nonexistent") // must not panic
}

// TestSnapshotterRestoreFiles is the P27.16 regression: Snapshotter.RestoreFiles
// must delegate to Store.RestoreFiles for its own checkpoint ID, giving callers
// (the engine's guard-FAIL quarantine path) a way to roll back a turn's writes
// without needing direct access to the underlying Store.
func TestSnapshotterRestoreFiles(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	existing := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(existing, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "b.txt")

	cp, err := st.Create(ctx, "s1", 0, "do the thing", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	snap := st.NewSnapshotter(cp.ID)
	snap.Capture(existing)
	snap.Capture(created)

	if err := os.WriteFile(existing, []byte("bad write"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("bad new file"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := snap.RestoreFiles(ctx)
	if err != nil {
		t.Fatalf("RestoreFiles: %v", err)
	}
	if n != 2 {
		t.Fatalf("restored %d files, want 2", n)
	}
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Errorf("existing file = %q, want %q", got, "original")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("created file should have been removed, stat err = %v", err)
	}
}

// TestNilSnapshotterRestoreFiles mirrors TestNilSnapshotterCapture: a nil
// Snapshotter (no checkpoint store wired in) must not panic and must report
// zero files restored, so a caller can invoke it unconditionally.
func TestNilSnapshotterRestoreFiles(t *testing.T) {
	var s *Snapshotter
	n, err := s.RestoreFiles(context.Background())
	if err != nil || n != 0 {
		t.Errorf("nil Snapshotter.RestoreFiles = (%d, %v), want (0, nil)", n, err)
	}
}

// --- P70.1: the restore boundary ---------------------------------------
//
// RestoreFiles used to replay every BLOB row with a bare os.WriteFile, so the
// invariant "captured paths are inside the workspace" was held only by every
// capture site getting it right — and P66.15 found one that did not. These
// tests pin the boundary itself: the root recorded on the checkpoint row, the
// pre-write validation of every path, and the all-or-nothing refusal.

// TestRestoreRefusesPathOutsideRootAndWritesNothing is the core regression: one
// bad row must abort the entire restore, leaving the *good* files untouched
// too. A half-rewound tree is exactly what /rewind exists to prevent.
func TestRestoreRefusesPathOutsideRootAndWritesNothing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	outsideDir := t.TempDir()

	inside := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(inside, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(outsideDir, "victim.txt")
	if err := os.WriteFile(outside, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}

	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(inside)
	// A capture site that resolved outside the workspace — the class of bug
	// P66.15 found. Recorded directly, because no current capture site does
	// this any more.
	if err := st.recordFile(cp.ID, outside, true, []byte("clobbered"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(inside, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := st.RestoreFiles(ctx, cp.ID)
	if !errors.Is(err, ErrRestoreRefused) {
		t.Fatalf("RestoreFiles err = %v, want it to wrap ErrRestoreRefused", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%q", outside)) {
		t.Errorf("error %q does not name the offending path %q", err, outside)
	}
	if n != 0 {
		t.Errorf("restored = %d, want 0", n)
	}
	if got, _ := os.ReadFile(outside); string(got) != "untouched" {
		t.Errorf("out-of-root file = %q, want it never written", got)
	}
	// The in-root file must NOT have been restored: the refusal is wholesale.
	if got, _ := os.ReadFile(inside); string(got) != "modified" {
		t.Errorf("in-root file = %q, want %q (nothing may be written on a refusal)", got, "modified")
	}
}

// TestRestoreRefusesTraversalAndSiblingPrefix covers the two cases a string
// prefix check gets wrong: "..", and a sibling directory whose name starts
// with the root's ("/work" vs "/work-other").
func TestRestoreRefusesTraversalAndSiblingPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work")
	sibling := filepath.Join(base, "work-other")
	for _, d := range []string{root, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]string{
		"sibling with the root as a string prefix": filepath.Join(sibling, "x.txt"),
		"parent traversal":                         filepath.Join(root, "sub", "..", "..", "escape.txt"),
		"the root itself":                          root,
		"a relative path":                          filepath.Join("relative", "x.txt"),
	}
	for name, p := range cases {
		if withinRoot(root, p) {
			t.Errorf("%s: withinRoot(%q, %q) = true, want false", name, root, p)
		}
	}
	// ...and the legitimate shapes still pass.
	for _, p := range []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "deep", "nested", "not-yet-created.txt"),
		filepath.Join(root, "sub", "..", "b.txt"),
	} {
		if !withinRoot(root, p) {
			t.Errorf("withinRoot(%q, %q) = false, want true", root, p)
		}
	}
}

// TestRestoreRefusesSymlinkEscape pins that validation is a real filesystem
// check, not a lexical one: a symlinked directory *inside* the root that
// points out of it must not be a way through.
func TestRestoreRefusesSymlinkEscape(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	outsideDir := t.TempDir()

	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Skipf("symlinks unavailable on this platform/account: %v", err)
	}
	victim := filepath.Join(outsideDir, "victim.txt")
	if err := os.WriteFile(victim, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}

	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	// Lexically under the root; actually outside it.
	if err := st.recordFile(cp.ID, filepath.Join(link, "victim.txt"), true, []byte("clobbered"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := st.RestoreFiles(ctx, cp.ID); !errors.Is(err, ErrRestoreRefused) {
		t.Fatalf("RestoreFiles err = %v, want it to wrap ErrRestoreRefused", err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "untouched" {
		t.Errorf("file behind the symlink = %q, want it never written", got)
	}
}

// TestRestoreHandlesPathsMissingAtRestoreTime is the false-positive guard on
// the boundary: a captured path legitimately may not exist when restore runs.
// A file deleted during the turn is recreated; a file created during the turn
// is deleted. Neither may be rejected for not existing.
func TestRestoreHandlesPathsMissingAtRestoreTime(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	deleted := filepath.Join(dir, "sub", "gone.txt")
	if err := os.MkdirAll(filepath.Dir(deleted), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deleted, []byte("was here"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "sub", "new.txt")

	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := st.NewSnapshotter(cp.ID)
	snap.Capture(deleted)
	snap.Capture(created)

	// The turn deletes one file and creates the other.
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("brand new"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := st.RestoreFiles(ctx, cp.ID)
	if err != nil {
		t.Fatalf("RestoreFiles: %v", err)
	}
	if n != 2 {
		t.Fatalf("restored = %d, want 2", n)
	}
	if got, err := os.ReadFile(deleted); err != nil || string(got) != "was here" {
		t.Errorf("deleted-then-restored file = %q, err = %v; want %q", got, err, "was here")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("created file should have been removed, stat err = %v", err)
	}
}

// TestRestorePreservesFileMode is the secondary half of P70.1: mode was never
// captured, so a file recreated by restore came back at a flat 0o644 and one
// whose mode the turn changed kept the new mode.
func TestRestorePreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not carry Unix permission bits")
	}
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	chmodded := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(chmodded, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := st.NewSnapshotter(cp.ID)
	snap.Capture(script)
	snap.Capture(chmodded)

	// The turn deletes the executable outright and relaxes the other's mode.
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(chmodded, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := st.RestoreFiles(ctx, cp.ID); err != nil {
		t.Fatalf("RestoreFiles: %v", err)
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatalf("recreated script: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Errorf("recreated file mode = %04o, want %04o", got, 0o750)
	}
	info, err = os.Stat(chmodded)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("rewritten file mode = %04o, want %04o", got, 0o600)
	}
}

// TestRestoreRefusesCheckpointWithNoRecordedRoot pins the legacy-row decision:
// a checkpoint written before P70.1 has no root, so its paths cannot be
// validated against anything. Restore refuses it rather than trusting them —
// fail closed, since trusting the recorded paths is precisely the behavior
// being removed. The user's fallback is the git rollback the same rewind
// request already offers.
func TestRestoreRefusesCheckpointWithNoRecordedRoot(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(f)
	// Age the row back to what an older binary would have written.
	if _, err := st.db.Exec(`UPDATE checkpoints SET workspace_root = ? WHERE id = ?`, "", cp.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := st.RestoreFiles(ctx, cp.ID)
	if !errors.Is(err, ErrRestoreRefused) {
		t.Fatalf("RestoreFiles err = %v, want it to wrap ErrRestoreRefused", err)
	}
	if n != 0 {
		t.Errorf("restored = %d, want 0", n)
	}
	if got, _ := os.ReadFile(f); string(got) != "modified" {
		t.Errorf("file = %q, want it untouched by a refused restore", got)
	}
}

// TestRestoreWithoutCapturedFilesNeedsNoRoot keeps the refusal from firing on
// the common empty case: a turn that wrote nothing has nothing to validate,
// and must still report a clean (0, nil) — including for a legacy row.
func TestRestoreWithoutCapturedFilesNeedsNoRoot(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	cp, err := st.Create(ctx, "s1", 0, "turn", "")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := st.RestoreFiles(ctx, cp.ID); n != 0 || err != nil {
		t.Errorf("RestoreFiles on an empty checkpoint = (%d, %v), want (0, nil)", n, err)
	}
}

// TestCreateRecordsWorkspaceRoot pins that the root survives the round trip
// through the row — the whole mechanism depends on it, and Get/List are how
// clients see it.
func TestCreateRecordsWorkspaceRoot(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	cp, err := st.Create(ctx, "s1", 0, "turn", dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(ctx, cp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceRoot != dir {
		t.Errorf("Get(...).WorkspaceRoot = %q, want %q", got.WorkspaceRoot, dir)
	}
	list, err := st.List(ctx, "s1")
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if list[0].WorkspaceRoot != dir {
		t.Errorf("List()[0].WorkspaceRoot = %q, want %q", list[0].WorkspaceRoot, dir)
	}
}
