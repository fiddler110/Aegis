package workspacetrust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrustPersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if s.IsTrusted(dir) {
		t.Fatal("fresh store should not trust anything")
	}
	if err := s.Trust(dir); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if !s.IsTrusted(dir) {
		t.Fatal("directory should be trusted after Trust")
	}

	reopened := Open(path)
	if !reopened.IsTrusted(dir) {
		t.Fatal("trust decision should persist across Open")
	}
}

func TestRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.Trust(dir); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if err := s.Revoke(dir); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if s.IsTrusted(dir) {
		t.Fatal("directory should not be trusted after Revoke")
	}
}

func TestMissingStoreFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	s := Open(path)
	if s.IsTrusted(t.TempDir()) {
		t.Fatal("missing store file should not trust anything")
	}
}

// A directory trusted through one path spelling must stay trusted (and be
// revocable) through a symlinked spelling of the same directory. This is the
// exact mismatch that broke `aegis trust --revoke` on macOS, where os.Getwd
// returns the fully symlink-resolved real path (e.g. /var → /private/var) while
// a caller may hand normalize the symlink spelling.
func TestNormalizeResolvesSymlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	s := Open(path)
	// Trust via the real path, look up / revoke via the symlink spelling.
	if err := s.Trust(real); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if !s.IsTrusted(link) {
		t.Fatal("symlink spelling of a trusted directory should also be trusted")
	}
	if err := s.Revoke(link); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if s.IsTrusted(real) {
		t.Error("revoking via the symlink spelling must revoke the real directory too")
	}
}

func TestNormalizeRelativeVsAbsolute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace_trust.json")
	dir := t.TempDir()

	s := Open(path)
	if err := s.Trust(dir); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	rel, err := filepath.Rel(".", dir)
	if err == nil {
		if !s.IsTrusted(rel) {
			t.Errorf("relative path %q for the same directory should also be trusted", rel)
		}
	}
}
