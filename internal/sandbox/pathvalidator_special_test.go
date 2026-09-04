package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// VULN-08. Two Windows path shapes pass every confinement check while naming
// something other than a file under root, so they have to be refused by name.
// Measured on Windows before the fix: os.WriteFile(filepath.Join(root, "NUL"))
// returns nil and reads back empty, and filepath.Join(root, "C:notes.txt")
// creates an alternate data stream that os.ReadDir reports only as the file
// "C" — invisible to glob, grep, the repo map and git.
//
// The rule itself is tested through windowsSpecialNameError, which carries no
// platform gate, so the table runs on every host; ValidatePath is then checked
// on Windows only, where the gate lets the rule through.

func TestWindowsSpecialNameRule(t *testing.T) {
	cases := []struct {
		path      string
		writeOnly bool // refused for writes; still readable
		reject    bool
		want      string // substring of the rejection reason
	}{
		// Rejected.
		{path: `C:notes.txt`, reject: true, want: "drive-relative"},
		{path: `c:x/y.go`, reject: true, want: "drive-relative"},
		{path: `README.md:payload`, reject: true, writeOnly: true, want: "alternate data stream"},
		{path: `docs/README.md:hidden:$DATA`, reject: true, writeOnly: true, want: "alternate data stream"},
		{path: `NUL`, reject: true, want: "reserved Windows device"},
		{path: `nul`, reject: true, want: "reserved Windows device"},
		{path: `CON.txt`, reject: true, want: "reserved Windows device"},
		{path: `sub/dir/aux.log`, reject: true, want: "reserved Windows device"},
		{path: `COM1`, reject: true, want: "reserved Windows device"},
		{path: `lpt9.dat`, reject: true, want: "reserved Windows device"},
		{path: `prn `, reject: true, want: "reserved Windows device"},
		{path: `con/x.go`, reject: true, want: "reserved Windows device"},

		// Accepted. The volume prefix of a legitimate absolute path contains a
		// colon and must survive, as must UNC and extended-length prefixes; and
		// a reserved name is only reserved as a whole segment, so "console.go"
		// and "nulls.txt" are ordinary files.
		{path: `D:\repo\file.go`},
		{path: `D:/repo/file.go`},
		{path: `\\server\share\file.go`},
		{path: `\\?\C:\repo\file.go`},
		{path: `src/main.go`},
		{path: `/etc/shadow`}, // rooted-no-volume: absCandidate's P32.1 case, not this one
		{path: `..\..\etc\passwd`},
		{path: `console.go`},
		{path: `nulls.txt`},
		{path: `com10.txt`},
		{path: `my.con`},
		{path: `a/CONTRIBUTING.md`},
	}
	for _, tc := range cases {
		err := windowsSpecialNameError(tc.path, AccessWrite)
		switch {
		case tc.reject && err == nil:
			t.Errorf("%q: expected rejection, got nil", tc.path)
		case tc.reject && !strings.Contains(err.Error(), tc.want):
			t.Errorf("%q: expected reason containing %q, got: %v", tc.path, tc.want, err)
		case !tc.reject && err != nil:
			t.Errorf("%q: expected acceptance, got: %v", tc.path, err)
		}
		// The stream rule is write-only; everything else applies in both
		// directions. See windowsSpecialNameError on why the read direction
		// stays open.
		readErr := windowsSpecialNameError(tc.path, AccessRead)
		switch {
		case tc.writeOnly && readErr != nil:
			t.Errorf("%q: a read should still be allowed, got: %v", tc.path, readErr)
		case tc.reject && !tc.writeOnly && readErr == nil:
			t.Errorf("%q: expected the read direction to be refused too", tc.path)
		case !tc.reject && readErr != nil:
			t.Errorf("%q: expected acceptance on read, got: %v", tc.path, readErr)
		}
	}
}

// The gate is Windows-only: a colon is a legal filename character on Linux and
// macOS, and a repo checked out there may legitimately hold a file named NUL.
func TestSpecialNameRuleIsPlatformGated(t *testing.T) {
	err := rejectSpecialName(`README.md:payload`, AccessWrite)
	if runtime.GOOS == "windows" && err == nil {
		t.Error("windows: expected the stream form to be rejected")
	}
	if runtime.GOOS != "windows" && err != nil {
		t.Errorf("%s: colon-bearing names are legal here, got: %v", runtime.GOOS, err)
	}
}

// Both validator entry points must apply the rule, since either one alone
// leaves the other's callers reachable.
func TestValidatePathRejectsWindowsSpecialNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("reserved device names and NTFS streams only apply on Windows")
	}
	root := tempRoot(t)
	roots := []Root{{Path: root, Writable: true}}
	for _, p := range []string{`NUL`, `sub/CON.txt`, `C:notes.txt`} {
		if _, err := ValidatePath(root, p); err == nil {
			t.Errorf("ValidatePath accepted %q", p)
		}
		if _, err := ValidatePathIn(roots, p, AccessWrite); err == nil {
			t.Errorf("ValidatePathIn accepted %q", p)
		}
	}
	// The stream form is refused for writes — the direction that hides content
	// — and left alone for reads.
	if _, err := ValidatePathIn(roots, `README.md:payload`, AccessWrite); err == nil {
		t.Error("ValidatePathIn accepted a write to an alternate data stream")
	}
	if _, err := ValidatePathIn(roots, `README.md:payload`, AccessRead); err != nil {
		t.Errorf("a read of a stream inside root should still resolve: %v", err)
	}
}

// The guard must not cost a legitimate Windows path. A drive-lettered absolute
// path inside root, and an ordinary relative one, both still validate.
func TestValidatePathKeepsLegitimateWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter paths only apply on Windows")
	}
	root := tempRoot(t)
	inner := filepath.Join(root, "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(inner), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inner, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{inner, `src/main.go`, `src\main.go`, filepath.ToSlash(inner)} {
		got, err := ValidatePath(root, p)
		if err != nil {
			t.Errorf("ValidatePath(%q): %v", p, err)
			continue
		}
		if got != inner {
			t.Errorf("ValidatePath(%q) = %q, want %q", p, got, inner)
		}
	}
}
