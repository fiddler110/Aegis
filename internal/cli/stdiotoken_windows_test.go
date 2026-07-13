//go:build windows

package cli

import (
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestResolveStdioAuthTokenRestrictsACL is the Windows counterpart to
// TestResolveStdioAuthTokenFileOwnerRestricted: on this platform POSIX mode
// bits on os.WriteFile are cosmetic, so the real "token file is
// owner-restricted" guarantee is the non-inherited, owner-only DACL
// config.GenerateAndWriteToken applies via fsguard.RestrictToOwner
// (P27.4/FIND-06).
func TestResolveStdioAuthTokenRestrictsACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.token")

	if _, err := resolveStdioAuthToken("AEGIS_MCP_TOKEN_TEST_ACL", path, "mcp-serve", discardLoggerCLI()); err != nil {
		t.Fatalf("resolveStdioAuthToken: %v", err)
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
}
