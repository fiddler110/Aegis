//go:build windows

package fsguard

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestRestrictToOwnerLocksDownACL verifies RestrictToOwner replaces a file's
// (inherited) DACL with a single, protected ACE granting access only to the
// owner — the same property daemon.token relied on before this package
// existed. It reads the on-disk DACL back and confirms both that exactly
// one ACE is present and that it names the well-known "owner rights" SID
// rather than a broader principal like Everyone/Authenticated Users, which
// stands in for "a different simulated principal" being denied.
func TestRestrictToOwnerLocksDownACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := RestrictToOwner(path); err != nil {
		t.Fatalf("RestrictToOwner: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("sd.DACL: %v", err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("expected exactly one ACE after RestrictToOwner, got %d", dacl.AceCount)
	}

	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatalf("GetAce: %v", err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))

	if !sid.IsWellKnown(windows.WinCreatorOwnerRightsSid) {
		t.Errorf("sole ACE grants access to %s, want the owner-rights SID", sid.String())
	}

	// Simulated different principal: the well-known "Everyone" SID must not
	// be the grantee of the ACE — i.e. a process running as any other
	// account has nothing in this DACL granting it access.
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid(WinWorldSid): %v", err)
	}
	if sid.Equals(everyone) {
		t.Errorf("ACE unexpectedly grants access to the Everyone SID")
	}
}

// The missing-file no-op contract (used by callers hardening SQLite's
// -wal/-shm sidecars, which may not exist yet at session-open time) is
// covered cross-platform by TestRestrictToOwnerMissingFile in
// fsguard_test.go.
