//go:build windows

package swarm

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestWriteSpecRestrictsACL is the C1 regression test for the subprocess
// backend's worker spec file.
//
// The spec is created in the system temp directory and chmod'ed 0o600, which
// is the whole story on POSIX and cosmetic on Windows: a new file inherits its
// parent directory's ACL regardless of the mode argument, and that parent is
// shared. The file carries the teammate's task and system prompts, and — the
// part that makes it a permission boundary rather than a privacy one — its
// Mode. The parent process clamps Mode against its own before writing it
// (clampMode in internal/tool/builtin/agent.go); `aegis worker` then trusts
// spec.Config.Mode as it finds it, because the clamp already happened in a
// process it cannot see. So another local account that can rewrite this file
// between the write and the child's read promotes the teammate to `auto`.
//
// The mailbox root has had this hardening since FIND-20/P27.11 and the stdio
// tokens since FIND-06/P27.4; the spec file was the one carrier of the same
// class that never got it.
func TestWriteSpecRestrictsACL(t *testing.T) {
	path, err := writeSpec(WorkerSpec{
		Identity: Identity{Name: "teammate"},
		Config:   SpawnConfig{Prompt: "do the thing", Mode: "plan"},
	})
	if err != nil {
		t.Fatalf("writeSpec: %v", err)
	}
	defer os.Remove(path)

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
		t.Fatalf("spec file has %d ACEs, want exactly 1 (an inherited ACL means the temp dir's, not the owner's)", dacl.AceCount)
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
