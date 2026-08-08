package filetracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadThenWriteAllowed(t *testing.T) {
	tr := New()
	path := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	tr.RecordRead(path)
	if err := tr.CheckWrite(path); err != nil {
		t.Errorf("write should be allowed after read: %v", err)
	}
}

func TestWriteWithoutReadAllowed(t *testing.T) {
	tr := New()
	path := filepath.Join(t.TempDir(), "new.txt")
	os.WriteFile(path, []byte("new"), 0o644)

	if err := tr.CheckWrite(path); err != nil {
		t.Errorf("write to untracked file should be allowed: %v", err)
	}
}

func TestWriteAfterExternalModification(t *testing.T) {
	tr := New()
	path := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	tr.RecordRead(path)

	// Simulate external modification.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(path, []byte("externally modified"), 0o644)

	if err := tr.CheckWrite(path); err == nil {
		t.Error("write should be rejected after external modification")
	}
}

func TestWriteAfterOwnWrite(t *testing.T) {
	tr := New()
	path := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	tr.RecordRead(path)

	// Simulate the agent writing (updates tracked mtime).
	os.WriteFile(path, []byte("agent wrote this"), 0o644)
	tr.RecordWrite(path)

	// A second write should be allowed since the agent itself wrote last.
	if err := tr.CheckWrite(path); err != nil {
		t.Errorf("write after own write should be allowed: %v", err)
	}
}

func TestDeletedFileAllowsWrite(t *testing.T) {
	tr := New()
	path := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(path, []byte("temp"), 0o644)

	tr.RecordRead(path)
	os.Remove(path)

	if err := tr.CheckWrite(path); err != nil {
		t.Errorf("write to deleted file should be allowed: %v", err)
	}
}

func TestClear(t *testing.T) {
	tr := New()
	path := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	tr.RecordRead(path)
	if tr.TrackedFiles() != 1 {
		t.Errorf("expected 1 tracked file, got %d", tr.TrackedFiles())
	}

	tr.Clear()
	if tr.TrackedFiles() != 0 {
		t.Errorf("expected 0 tracked files after clear, got %d", tr.TrackedFiles())
	}
}

func TestNonexistentFileRead(t *testing.T) {
	tr := New()
	tr.RecordRead("/nonexistent/file.txt")
	if tr.TrackedFiles() != 0 {
		t.Error("reading nonexistent file should not track it")
	}
}

// TestPartialReadGatesFullOverwrite covers the readState.partial flag: the
// mtime guard cannot distinguish a capped read from a complete one, so
// CheckWrite stays silent in both cases while CheckFullOverwrite refuses only
// the partial one.
func TestPartialReadGatesFullOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := New()
	tr.RecordPartialRead(p)
	if err := tr.CheckWrite(p); err != nil {
		t.Errorf("CheckWrite must not fire on an unmodified file: %v", err)
	}
	if err := tr.CheckFullOverwrite(p); err == nil {
		t.Error("CheckFullOverwrite must refuse after a partial read")
	}

	// An anchored edit records a write but leaves the flag standing.
	tr.RecordWrite(p)
	if err := tr.CheckFullOverwrite(p); err == nil {
		t.Error("RecordWrite must not clear the partial-read flag")
	}

	// A whole-file write does clear it: every byte is now the agent's own.
	tr.RecordOverwrite(p)
	if err := tr.CheckFullOverwrite(p); err != nil {
		t.Errorf("RecordOverwrite should clear the flag: %v", err)
	}

	// So does a complete re-read.
	tr.RecordPartialRead(p)
	tr.RecordRead(p)
	if err := tr.CheckFullOverwrite(p); err != nil {
		t.Errorf("a complete read should clear the flag: %v", err)
	}
}

// TestFullOverwriteUntrackedAndMissing keeps the guard narrow: it must not
// disturb the "first write to a file the agent never read" allowance, nor a
// path that no longer exists (a write there re-creates rather than overwrites).
func TestFullOverwriteUntrackedAndMissing(t *testing.T) {
	dir := t.TempDir()
	tr := New()
	unread := filepath.Join(dir, "unread.txt")
	if err := os.WriteFile(unread, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckFullOverwrite(unread); err != nil {
		t.Errorf("untracked file must be writable: %v", err)
	}

	gone := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(gone, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.RecordPartialRead(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckFullOverwrite(gone); err != nil {
		t.Errorf("a deleted file must be re-creatable: %v", err)
	}
}
