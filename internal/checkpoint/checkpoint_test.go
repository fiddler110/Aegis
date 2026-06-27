package checkpoint

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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

	cp, err := st.Create(ctx, "s1", 2, "do the thing")
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

	cp, err := st.Create(ctx, "s1", 0, "first turn")
	if err != nil {
		t.Fatal(err)
	}
	st.NewSnapshotter(cp.ID).Capture(f)
	// A checkpoint for a different session must not appear in s1's list.
	if _, err := st.Create(ctx, "s2", 0, "other"); err != nil {
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

	cp, _ := st.Create(ctx, "s1", 0, "t")
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
