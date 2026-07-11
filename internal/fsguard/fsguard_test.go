package fsguard

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRestrictToOwnerExistingFile is a cross-platform smoke test: on
// POSIX it exercises the no-op stub, on Windows it exercises the real ACL
// path exercised more thoroughly by fsguard_windows_test.go. Either way it
// must succeed on an ordinary file the test itself just created.
func TestRestrictToOwnerExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := RestrictToOwner(path); err != nil {
		t.Fatalf("RestrictToOwner: %v", err)
	}
}

// TestRestrictToOwnerMissingFile confirms a missing path is a no-op on every
// platform — callers harden SQLite's -wal/-shm sidecars best-effort and must
// not treat "doesn't exist yet" as a failure.
func TestRestrictToOwnerMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := RestrictToOwner(filepath.Join(dir, "does-not-exist")); err != nil {
		t.Errorf("RestrictToOwner on missing file: %v, want nil", err)
	}
}
