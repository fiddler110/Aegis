//go:build windows

package sqlitestore

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// assertOwnerOnlyACL fails unless path carries exactly one ACE granting the
// owner-rights SID. More than one ACE means the file is still carrying its
// parent directory's inherited entries (SYSTEM / Administrators / the user),
// which is what an unhardened file looks like on Windows — the 0o600 and
// 0o700 mode bits elsewhere in this package set no ACL at all.
//
// Follows internal/swarm/specperm_windows_test.go, the first test of this
// class in the tree.
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
		t.Fatalf("%s has %d ACEs, want exactly 1 (more than one means the parent directory's ACL was inherited)", filepath.Base(path), dacl.AceCount)
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

// openProbeDB opens a WAL-mode store under dir and writes to it, so the -wal
// and -shm sidecars are on disk when the caller inspects them.
func openProbeDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "probe.db")
	db, err := Open(dbPath, "probe")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE t (a TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err != nil {
			t.Fatalf("expected sidecar %s to exist after a write: %v", suffix, err)
		}
	}
	return dbPath
}

// TestHardenPermissionsCoversTheSidecars is the P81.24 regression test for the
// FIND-24 asymmetry: daemon.token gets 0o600 *and* a real non-inherited ACL
// precisely because the mode bit is cosmetic on Windows, and the store holding
// the conversation those credentials guard must not be protected less well.
//
// It also pins the half of the claim that had never been measured — that the
// driver-created -wal/-shm companions inherit the parent's ACL, so hardening
// only the file named in the DSN would leave the recent conversation pages
// readable by every other account the parent directory admits.
func TestHardenPermissionsCoversTheSidecars(t *testing.T) {
	dbPath := openProbeDB(t, t.TempDir())
	if err := HardenPermissions(dbPath, "probe"); err != nil {
		t.Fatalf("HardenPermissions: %v", err)
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		assertOwnerOnlyACL(t, p)
	}
}

// TestHardenedDirMakesSidecarsInheritOwnerOnly measures the property
// HardenPermissions' doc comment names as the durable answer to the sidecar
// timing problem: -wal and -shm are created (and, after a checkpoint, *re*-
// created) by the driver rather than by Aegis, so no single call at open time
// can cover every one that will ever exist. A protected inheritable DACL on
// the containing directory does, because Windows applies it to each file the
// driver creates there afterwards.
//
// The test is what licenses the spill directory being hardened as a directory
// (internal/tool/builtin/spill.go) rather than only per file, and what makes
// "harden the data dir" a coherent follow-up rather than a guess.
func TestHardenedDirMakesSidecarsInheritOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fsguard.RestrictToOwner(dir); err != nil {
		t.Fatalf("RestrictToOwner(dir): %v", err)
	}
	// No HardenPermissions call anywhere below: the point is that files the
	// driver creates after the directory is hardened need no per-file call.
	dbPath := openProbeDB(t, dir)
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		assertOwnerOnlyACL(t, p)
	}
}

// TestUnhardenedStoreInheritsTheParentACL is the negative control. Without it
// the two tests above would still pass on a host whose temp directory happened
// to carry a single owner-only ACE already, and would be measuring nothing.
func TestUnhardenedStoreInheritsTheParentACL(t *testing.T) {
	dbPath := openProbeDB(t, t.TempDir())
	sd, err := windows.GetNamedSecurityInfo(dbPath, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("sd.DACL: %v", err)
	}
	if dacl.AceCount < 2 {
		t.Skipf("this host's temp directory grants %d ACE(s); the inheritance control needs a shared parent to be meaningful", dacl.AceCount)
	}
}
