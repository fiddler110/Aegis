package workspacetrust

import (
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
