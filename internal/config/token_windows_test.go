//go:build windows

package config

import (
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestGenerateAndWriteTokenRestrictsACL verifies GenerateAndWriteToken
// applies fsguard.RestrictToOwner's non-inherited, owner-only DACL to the
// token file it writes — the same property daemon.token has always relied
// on (see internal/fsguard/fsguard_windows_test.go), now required of the
// mcp-serve/acp stdio token files too (P27.4/FIND-06).
func TestGenerateAndWriteTokenRestrictsACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.token")

	if _, err := GenerateAndWriteToken(path); err != nil {
		t.Fatalf("GenerateAndWriteToken: %v", err)
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
		t.Fatalf("expected exactly one ACE after GenerateAndWriteToken, got %d", dacl.AceCount)
	}

	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatalf("GetAce: %v", err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))

	if !sid.IsWellKnown(windows.WinCreatorOwnerRightsSid) {
		t.Errorf("sole ACE grants access to %s, want the owner-rights SID", sid.String())
	}

	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid(WinWorldSid): %v", err)
	}
	if sid.Equals(everyone) {
		t.Errorf("ACE unexpectedly grants access to the Everyone SID")
	}
}
