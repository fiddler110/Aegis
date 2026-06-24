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
