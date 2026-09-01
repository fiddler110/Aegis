//go:build windows

package workspacetrust

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestSaveRestrictsACL pins the ACL on the trust store file (P81.24/P81.27).
//
// The store is what decides whether a cloned repository's `.aegis/config.yaml`
// may widen the agent's posture — permission.*, sandbox.*, mcp.servers,
// notify.webhook, hooks. save() writes it 0o600 inside a 0o700 directory,
// which is the whole story on POSIX and cosmetic on Windows, where a new file
// inherits its parent directory's ACL and the mode argument sets nothing; the
// fsguard.RestrictToOwner call there is what makes the restriction real, the
// same reasoning generateAndWriteToken writes down for daemon.token.
//
// The threat model (FIND-27) read this file as having no fsguard call at all.
// It has had one since the 519efbd hardening sweep; what it did not have was a
// test, so a refactor could drop the line and nothing would notice. This is
// that test. It does not address FIND-27's other half — the store is still
// unauthenticated, and a same-user process can still insert a grant — which is
// the MAC work deliberately left out of this item.
func TestSaveRestrictsACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust", "workspace_trust.json")
	s := Open(path)
	if err := s.Trust(t.TempDir(), "sha256:aaaa"); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("trust store was not written: %v", err)
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
		t.Fatalf("trust store has %d ACEs, want exactly 1 (more than one means the parent directory's ACL was inherited)", dacl.AceCount)
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

// TestKeyFileRestrictsACL is TestSaveRestrictsACL's sibling for the MAC
// secret introduced by FIND-27/P81.27: the key is exactly as sensitive as the
// store it authenticates (anything that can read it can forge a valid grant),
// so it needs the identical owner-only ACL, not just the store file.
func TestKeyFileRestrictsACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust", "workspace_trust.json")
	s := Open(path)
	if err := s.TrustWithOrigin(t.TempDir(), "sha256:aaaa", "cli"); err != nil {
		t.Fatalf("TrustWithOrigin: %v", err)
	}
	kp := keyPath(path)
	if _, err := os.Stat(kp); err != nil {
		t.Fatalf("key file was not written: %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(kp, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("sd.DACL: %v", err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("key file has %d ACEs, want exactly 1 (more than one means the parent directory's ACL was inherited)", dacl.AceCount)
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
