//go:build windows

package builtin

import (
	"context"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestSpillWriteAppliesPermissionHardening pins GAP-3.2: spill files are raw,
// unredacted dumps of tool output (by design, for recoverability) and are
// realistically credential-bearing — a model running `cat .env` or a shell
// command whose output embeds a bearer token can spill it verbatim once
// large enough to exceed the inline result cap. spillText must apply
// fsguard.RestrictToOwner the same way every other security-sensitive file
// in this codebase does, since 0o600 alone is cosmetic on Windows.
func TestSpillWriteAppliesPermissionHardening(t *testing.T) {
	root := t.TempDir()
	rel, ok := spillText(context.Background(), root, "shell", "secret content")
	if !ok {
		t.Fatal("spillText reported failure")
	}
	path := filepath.Join(root, filepath.FromSlash(rel))

	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo(%s): %v", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("sd.DACL: %v", err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("expected exactly one ACE, got %d", dacl.AceCount)
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
