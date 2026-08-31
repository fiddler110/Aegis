//go:build windows

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assertOwnerOnlyACL(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo(%s): %v", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("sd.DACL(%s): %v", path, err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("%s has %d ACEs, want exactly 1 (more than one means the workspace's ACL was inherited)", filepath.Base(path), dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatalf("GetAce(%s): %v", path, err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !sid.IsWellKnown(windows.WinCreatorOwnerRightsSid) {
		t.Errorf("%s: sole ACE grants access to %s, want the owner-rights SID", filepath.Base(path), sid.String())
	}
}

// TestSpillDirAndFileAreOwnerOnly is the P81.24 regression test for the spill
// path. A spill file holds the overflow of a truncated tool result — verbatim
// bytes of whatever the agent just read — and it is written into the user's
// *workspace*, whose ACL is typically the widest of any directory Aegis
// touches. The 0o750/0o600 modes on that path do nothing on Windows.
//
// Both halves are asserted because they are different controls: the per-file
// call (already present) covers a file written before this change, and the
// directory call (added by P81.24) covers the directory listing itself and
// makes every later file inherit the owner-only ACE at creation time rather
// than acquiring it a syscall after os.WriteFile returned.
func TestSpillDirAndFileAreOwnerOnly(t *testing.T) {
	root := t.TempDir()
	rel, ok := spillText(context.Background(), root, "shell", strings.Repeat("secret\n", 100))
	if !ok {
		t.Fatal("spillText declined to write")
	}
	assertOwnerOnlyACL(t, filepath.Join(root, filepath.FromSlash(spillDirRel)))
	assertOwnerOnlyACL(t, filepath.Join(root, filepath.FromSlash(rel)))
}

// TestSpillFileCreatedAfterHardeningInheritsOwnerOnly measures the reason the
// directory is hardened rather than only each file: a file created in the
// hardened directory is owner-only *before* any per-file call runs. The check
// removes the file's own explicit ACL first, so what it observes can only have
// come from inheritance.
func TestSpillFileCreatedAfterHardeningInheritsOwnerOnly(t *testing.T) {
	root := t.TempDir()
	if _, ok := spillText(context.Background(), root, "shell", "first"); !ok {
		t.Fatal("spillText declined to write")
	}
	dir := filepath.Join(root, filepath.FromSlash(spillDirRel))

	// A plain create in the now-hardened directory, with no fsguard call.
	plain := filepath.Join(dir, "inherited.txt")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertOwnerOnlyACL(t, plain)
}

// TestReapSpillDirRemovesEverything covers the session-end half of P81.24: the
// TTL reaper is a resource bound, not a retention policy, so a caller that
// knows a session ended needs a way to delete the remainder outright.
func TestReapSpillDirRemovesEverything(t *testing.T) {
	root := t.TempDir()
	if _, ok := spillText(context.Background(), root, "shell", strings.Repeat("secret\n", 100)); !ok {
		t.Fatal("spillText declined to write")
	}
	if err := ReapSpillDir(root); err != nil {
		t.Fatalf("ReapSpillDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(spillDirRel))); !os.IsNotExist(err) {
		t.Errorf("spill directory survived the reap: %v", err)
	}
	// Idempotent: reaping a workspace that never spilled is not an error.
	if err := ReapSpillDir(root); err != nil {
		t.Errorf("second ReapSpillDir: %v", err)
	}
}
