//go:build windows

package logging

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// assertOwnerOnlyACL mirrors internal/fsguard/fsguard_windows_test.go and
// internal/config/token_windows_test.go: it reads the file's on-disk DACL
// and confirms exactly one ACE is present, naming the well-known "owner
// rights" SID rather than a broader principal like Everyone.
func assertOwnerOnlyACL(t *testing.T, path string) {
	t.Helper()
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
		t.Fatalf("%s: expected exactly one ACE, got %d", path, dacl.AceCount)
	}

	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatalf("GetAce: %v", err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))

	if !sid.IsWellKnown(windows.WinCreatorOwnerRightsSid) {
		t.Errorf("%s: sole ACE grants access to %s, want the owner-rights SID", path, sid.String())
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid(WinWorldSid): %v", err)
	}
	if sid.Equals(everyone) {
		t.Errorf("%s: ACE unexpectedly grants access to the Everyone SID", path)
	}
}

// TestLogFileAppliesPermissionHardening pins GAP-3.1 across all three open
// paths: the initial (non-rotating) open, the rotating-writer's initial
// open, and its post-rotation reopen.
func TestLogFileAppliesPermissionHardening(t *testing.T) {
	t.Run("initial open", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "aegis.log")
		lg, closer, err := New(Options{Path: path})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		lg.Info("hello")
		if err := closer.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertOwnerOnlyACL(t, path)
	})

	t.Run("rotating-writer open", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "aegis.log")
		lg, closer, err := New(Options{Path: path, MaxSizeBytes: 1 << 20, MaxBackups: 1})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		lg.Info("hello")
		if err := closer.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertOwnerOnlyACL(t, path)
	})

	t.Run("post-rotation reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "aegis.log")
		w, err := NewRotatingWriter(path, 5, 1)
		if err != nil {
			t.Fatalf("NewRotatingWriter: %v", err)
		}
		defer w.Close()
		if _, err := w.Write([]byte("aaaaa")); err != nil {
			t.Fatalf("Write 1: %v", err)
		}
		if _, err := w.Write([]byte("bbbbb")); err != nil { // forces rotation + reopen
			t.Fatalf("Write 2: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected reopened file to exist: %v", err)
		}
		assertOwnerOnlyACL(t, path)
	})
}
