package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mustSymlink creates link -> target, skipping the test when the platform
// refuses rather than failing it.
//
// Windows grants SeCreateSymbolicLinkPrivilege only to elevated processes or
// with Developer Mode enabled, so `os.Symlink` fails there with "a required
// privilege is not held by the client" on an ordinary developer account. That
// is an environment limitation, not a defect in the validator, and reporting it
// as a failure trains people to ignore a red suite. Deliberately a runtime
// check on the *error* rather than a blanket `GOOS == "windows"` skip: a
// Windows box with Developer Mode on can create symlinks fine, and those users
// should get the real coverage — the symlink-escape rules this file pins are
// exactly the ones worth verifying on the OS whose path semantics differ most.
func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable in this environment (%v); on Windows enable Developer Mode or run elevated to exercise this test", err)
	}
}

// mustDir creates dir under parent and returns its symlink-resolved path, so
// tests compare against the same namespace the validator returns (on macOS a
// t.TempDir() lives under a /var -> /private/var symlink).
func mustDir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestValidatePathInSingleRootMatchesValidatePath pins the degenerate case:
// with one writable root, ValidatePathIn must behave exactly like the
// single-root ValidatePath it generalizes — that equivalence is what lets
// every builtin route through the multi-root form unconditionally (P52.13).
func TestValidatePathInSingleRootMatchesValidatePath(t *testing.T) {
	base := t.TempDir()
	root := mustDir(t, base, "root")
	mustWrite(t, filepath.Join(root, "in.txt"), "x")

	for _, p := range []string{"in.txt", "new.txt", "../outside.txt", "/etc/passwd", "sub/../in.txt"} {
		wantPath, wantErr := ValidatePath(root, p)
		gotPath, gotErr := ValidatePathIn([]Root{{Path: root, Writable: true}}, p, AccessRead)
		if (wantErr == nil) != (gotErr == nil) {
			t.Errorf("%q: ValidatePath err=%v but ValidatePathIn err=%v", p, wantErr, gotErr)
			continue
		}
		if wantErr == nil && gotPath != wantPath {
			t.Errorf("%q: ValidatePathIn = %q, want %q", p, gotPath, wantPath)
		}
	}
}

// TestValidatePathInReadsAdditionalRoot is the workflow the item exists for:
// research artifacts live in repo A, the document is written into repo B, and
// A is reachable for reads without widening confinement to a common parent.
func TestValidatePathInReadsAdditionalRoot(t *testing.T) {
	base := t.TempDir()
	primary := mustDir(t, base, "docs")
	research := mustDir(t, base, "research")
	note := filepath.Join(research, "notes.md")
	mustWrite(t, note, "findings")

	roots := []Root{{Path: primary, Writable: true}, {Path: research}}

	got, err := ValidatePathIn(roots, note, AccessRead)
	if err != nil {
		t.Fatalf("read from additional root: %v", err)
	}
	if got != note {
		t.Errorf("resolved to %q, want %q", got, note)
	}

	// A sibling of both roots is still out of bounds — admitting research must
	// not admit its parent.
	outside := filepath.Join(base, "secret.txt")
	mustWrite(t, outside, "nope")
	if _, err := ValidatePathIn(roots, outside, AccessRead); err == nil {
		t.Error("path outside every root validated")
	}
}

// TestValidatePathInWriteRefusedInReadOnlyRoot covers the default that makes
// additional roots cheap to grant: they are readable, not writable, so the
// model cannot scribble into the repo it was only meant to read from.
func TestValidatePathInWriteRefusedInReadOnlyRoot(t *testing.T) {
	base := t.TempDir()
	primary := mustDir(t, base, "docs")
	research := mustDir(t, base, "research")
	target := filepath.Join(research, "out.md")

	roots := []Root{{Path: primary, Writable: true}, {Path: research}}

	if _, err := ValidatePathIn(roots, target, AccessRead); err != nil {
		t.Fatalf("read should be allowed: %v", err)
	}
	_, err := ValidatePathIn(roots, target, AccessWrite)
	if err == nil {
		t.Fatal("write into a read-only additional root was allowed")
	}
	// The error has to name the reason, or an operator sees a confinement
	// escape for a path that is in fact configured and just isn't writable.
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("error %q does not explain the read-only refusal", err)
	}

	// Marking it writable is the documented escape hatch.
	roots[1].Writable = true
	if _, err := ValidatePathIn(roots, target, AccessWrite); err != nil {
		t.Errorf("write into an explicitly writable additional root: %v", err)
	}
}

// TestValidatePathInSymlinkEscapeCheckedPerRoot is the trap the item names by
// hand: the escape check must run against each candidate root separately, not
// once against something that covers them all. A symlink out of root A landing
// in root B's *parent* has to be refused — under a merged-prefix check the
// shared parent would swallow it and validate.
func TestValidatePathInSymlinkEscapeCheckedPerRoot(t *testing.T) {
	base := t.TempDir()
	primary := mustDir(t, base, "a")
	other := mustDir(t, base, "b")
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(baseReal, "secret.txt"), "shared-parent secret")

	// primary/escape -> the parent both roots live under.
	mustSymlink(t, baseReal, filepath.Join(primary, "escape"))

	roots := []Root{{Path: primary, Writable: true}, {Path: other}}
	if _, err := ValidatePathIn(roots, "escape/secret.txt", AccessRead); err == nil {
		t.Error("symlink into the roots' shared parent validated; the escape check is not per-root")
	}
}

// TestValidatePathInSymlinkIntoAnotherRootIsAllowed is the other half of the
// same rule: a symlink whose target genuinely lands inside a configured root
// must validate. Refusing it would make the escape check root-order-dependent
// rather than target-dependent.
func TestValidatePathInSymlinkIntoAnotherRootIsAllowed(t *testing.T) {
	base := t.TempDir()
	primary := mustDir(t, base, "docs")
	research := mustDir(t, base, "research")
	note := filepath.Join(research, "notes.md")
	mustWrite(t, note, "findings")

	mustSymlink(t, research, filepath.Join(primary, "link"))

	roots := []Root{{Path: primary, Writable: true}, {Path: research}}
	got, err := ValidatePathIn(roots, "link/notes.md", AccessRead)
	if err != nil {
		t.Fatalf("symlink into a configured root refused: %v", err)
	}
	if got != note {
		t.Errorf("resolved to %q, want %q", got, note)
	}

	// ...but it is still read-only, reached through the primary root or not:
	// writability is a property of the root the path resolves *into*.
	if _, err := ValidatePathIn(roots, "link/new.md", AccessWrite); err == nil {
		t.Error("write through a symlink into a read-only root was allowed")
	}
}

// TestValidatePathInRejectsEmptyRootSet guards the wiring: an empty set must
// be an error, never a silent "everything validates".
func TestValidatePathInRejectsEmptyRootSet(t *testing.T) {
	if _, err := ValidatePathIn(nil, "/etc/passwd", AccessRead); err == nil {
		t.Error("empty root set validated a path")
	}
}

// TestIsRootedCoversWindowsSpellings pins the rule that filepath.IsAbs alone
// gets wrong. The POSIX-rooted and volume-relative spellings must be reported
// as rooted on Windows — that is the whole reason this helper exists, since
// callers that pre-join such a path produce something that looks confined while
// the OS resolves it from a drive root.
func TestIsRootedCoversWindowsSpellings(t *testing.T) {
	// Rooted on every platform.
	for _, p := range []string{"/etc/passwd", "/"} {
		if !IsRooted(p) {
			t.Errorf("IsRooted(%q) = false, want true on every platform", p)
		}
	}

	// Never rooted anywhere: the ordinary relative forms. Calling these rooted
	// would refuse perfectly legitimate paths.
	for _, p := range []string{"", "notes.md", "sub/notes.md", "../escape.md", "./x"} {
		if IsRooted(p) {
			t.Errorf("IsRooted(%q) = true, want false on every platform", p)
		}
	}

	// Windows-only spellings. filepath.IsAbs answers false for the first two
	// even on Windows, which is precisely the gap this helper closes; on POSIX
	// all three are just unusual relative filenames, and reading them as rooted
	// there would be wrong.
	windowsRooted := []string{`\Windows\System32`, `C:notes.txt`, `C:\Windows`}
	for _, p := range windowsRooted {
		want := runtime.GOOS == "windows"
		if got := IsRooted(p); got != want {
			t.Errorf("IsRooted(%q) = %v on %s, want %v", p, got, runtime.GOOS, want)
		}
	}
}
