package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Root is one confinement root a path may legitimately resolve inside, along
// with whether writes into it are permitted (P52.13). The session's own
// workdir is always the primary, writable root; `workspace.additional_roots`
// contributes the rest, read-only unless explicitly marked writable.
type Root struct {
	// Path is the directory paths must resolve inside.
	Path string
	// Writable allows AccessWrite validations to land here. Additional roots
	// default to false: the workflow the feature exists for is "read research
	// from repo A, write the document into repo B", so widening confinement
	// for reads should not also widen it for writes.
	Writable bool
}

// Access is the intent a caller has for a path — the distinction that lets a
// read-only additional root serve reads while rejecting writes.
type Access int

const (
	// AccessRead is a read, list, or scan of an existing path.
	AccessRead Access = iota
	// AccessWrite is a create, overwrite, edit, or output-directory use.
	AccessWrite
)

// ValidatePath resolves path against root and verifies the result stays within
// root, following symlinks. This hardens the basic filepath.Rel check against
// symlink escapes and ".." traversal.
//
// If the path does not exist yet (e.g. a write to a new file), the validator
// walks up to the nearest existing ancestor and resolves symlinks there,
// ensuring the ancestor is within root.
func ValidatePath(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	return validateAgainstRoot(root, absCandidate(root, path), path)
}

// ValidatePathIn is ValidatePath over a set of roots (P52.13). roots[0] is the
// primary root: relative paths resolve against it (so an unqualified name means
// the same file it always did), and it names the workspace in error messages.
// The resolved path validates if it lands inside *any* root permitting access.
//
// The confinement check runs per root, against each root's own EvalSymlinks
// identity — never once against a prefix covering the set. That distinction is
// the whole security property here: two roots under a shared parent must not
// make that parent reachable, so a symlink out of root A into root B's *parent*
// is refused even though it lands "between" two legitimate roots.
//
// Symlink resolution of the candidate itself happens once, before the loop,
// because it is a fact about the filesystem rather than about any root. That
// also means the single-root fast path's lexical pre-check is deliberately
// absent: it assumes the candidate was built by joining against the root being
// tested, which is false for every root but the primary — applying it per root
// would reject a relative path naming a file in an additional root before its
// symlinks were ever resolved. The post-resolution comparison is the
// authoritative check in both forms; ".." is already collapsed by
// absCandidate's Clean.
//
// A single-element writable root set is equivalent to ValidatePath, which is
// what a session with no additional roots configured gets.
func ValidatePathIn(roots []Root, path string, access Access) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("no workspace root configured")
	}
	primary := roots[0].Path
	abs := absCandidate(primary, path)

	// Resolve symlinks on the real filesystem. If the full path exists, resolve
	// it directly; otherwise walk up to the nearest existing ancestor, resolve
	// that, and re-append the remaining segments (a write to a new file).
	resolved, tail, err := resolveExisting(abs)
	if err != nil {
		// Fail closed: with nothing resolvable to compare against, the only
		// honest answer is "cannot confirm this is inside root" (M8).
		return "", fmt.Errorf("path %q cannot be resolved for confinement against %q: %w", path, primary, err)
	}
	full := filepath.Join(resolved, tail)

	// Track whether a read-only root would have matched, so the write case can
	// say *why* it was refused instead of reporting a confinement escape the
	// operator cannot act on.
	var readOnlyMatch string
	for _, r := range roots {
		realRoot, err := filepath.EvalSymlinks(r.Path)
		if err != nil {
			realRoot = r.Path
		}
		if escapesRoot(realRoot, full) {
			continue
		}
		if access == AccessWrite && !r.Writable {
			if readOnlyMatch == "" {
				readOnlyMatch = r.Path
			}
			continue
		}
		return full, nil
	}
	if readOnlyMatch != "" {
		return "", fmt.Errorf("path %q is inside the read-only additional root %q; writes are only allowed in the workspace root %q (set writable: true on that entry in workspace.additional_roots to change it)", path, readOnlyMatch, primary)
	}
	if len(roots) == 1 {
		return "", fmt.Errorf("path %q escapes the workspace root %q", path, primary)
	}
	return "", fmt.Errorf("path %q resolves outside the workspace root %q and all %d additional roots", path, primary, len(roots)-1)
}

// absCandidate turns path into the absolute path it names, resolving a
// relative path against root. Purely lexical — no filesystem access — so a
// multi-root validation can absolutize once and then test that one candidate
// against every root.
func absCandidate(root, path string) string {
	abs := path
	switch {
	case filepath.IsAbs(abs):
		// already absolute, use as-is.
	case isWindowsRootedNoVolume(abs):
		// A path like "/etc/shadow" or "\etc\shadow" has no drive letter, so
		// filepath.IsAbs reports false on Windows even though the OS treats
		// it as rooted at the current drive, not as relative to root — join
		// it against root's drive to match that real resolution instead of
		// filepath.Join's (relative-path) semantics, which would otherwise
		// fold the leading separator into a path that stays under root and
		// wrongly validate as confined (P32.1).
		abs = filepath.VolumeName(root) + abs
	default:
		abs = filepath.Join(root, abs)
	}
	return filepath.Clean(abs)
}

// validateAgainstRoot runs the confinement checks for one root against an
// already-absolutized candidate. orig is the caller's original path, used only
// in error messages so they name what the caller actually wrote.
func validateAgainstRoot(root, abs, orig string) (string, error) {
	// Fast check before symlink resolution: reject obvious escapes.
	if escapesRoot(root, abs) {
		return "", fmt.Errorf("path %q escapes the workspace root %q", orig, root)
	}

	// Resolve symlinks on the real filesystem. If the full path exists, resolve
	// it directly. Otherwise, walk up to the nearest existing ancestor and
	// resolve that, then re-append the remaining segments.
	resolved, tail, err := resolveExisting(abs)
	if err != nil {
		// Fail closed (M8): comparing an unresolved path against a resolved
		// root is a confinement answer computed across two namespaces.
		return "", fmt.Errorf("path %q cannot be resolved for confinement against %q: %w", orig, root, err)
	}

	// Check the resolved real path is still within root.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}

	full := filepath.Join(resolved, tail)
	if escapesRoot(realRoot, full) {
		return "", fmt.Errorf("path %q resolves outside the workspace root %q (symlink escape)", orig, root)
	}

	return full, nil
}

// isWindowsRootedNoVolume reports whether p is a Windows-style rooted path
// (starts with a path separator) that carries no drive letter/UNC volume —
// e.g. "/etc/shadow" or `\Windows\System32`. filepath.IsAbs returns false
// for these on Windows (it requires a volume), but the OS itself resolves
// them against the current drive, not against an arbitrary root directory.
func isWindowsRootedNoVolume(p string) bool {
	if runtime.GOOS != "windows" || p == "" {
		return false
	}
	return (p[0] == '/' || p[0] == '\\') && filepath.VolumeName(p) == ""
}

// IsRooted reports whether the OS will resolve p from a filesystem root rather
// than from the current directory — i.e. whether joining it onto a base
// directory would misrepresent where it actually points.
//
// It exists because `filepath.IsAbs` alone is the wrong test on Windows and
// gets this wrong in a direction that hides escapes: it requires a volume, so
// it answers false for `/etc/passwd`, `\Windows\System32`, and the
// drive-relative `C:notes.txt`, all of which the OS resolves against a drive
// root. Code that then does `filepath.Join(base, p)` folds the leading
// separator away and produces a path that looks perfectly confined while the
// real read goes elsewhere — the P32.1 trap absCandidate documents. Any caller
// that pre-joins a caller-supplied path before validating it needs this test
// first; use it rather than writing a fourth spelling of the rule.
func IsRooted(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	// Windows: rooted-without-volume (`/x`, `\x`) and volume-relative (`C:x`)
	// both escape a naive Join.
	return p[0] == '/' || p[0] == '\\' || filepath.VolumeName(p) != ""
}

// escapesRoot reports whether target lies outside root. On Windows the
// comparison is case-insensitive, since the filesystem treats "C:\Work" and
// "c:\work" as the same directory and a case difference must not be mistaken
// for (or used to disguise) a traversal escape.
func escapesRoot(root, target string) bool {
	base, tgt := root, target
	if runtime.GOOS == "windows" {
		base, tgt = strings.ToLower(root), strings.ToLower(target)
	}
	rel, err := filepath.Rel(base, tgt)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// errNoExistingAncestor reports that resolveExisting reached the filesystem
// root without finding any existing directory to resolve against. See M8 on
// resolveExisting for why this is an error rather than a fallback.
var errNoExistingAncestor = errors.New("no existing ancestor directory to resolve against")

// resolveExisting walks up from path until it finds an existing directory,
// resolves symlinks on that ancestor, and returns (resolvedAncestor,
// remainingTail). For a fully existing path, tail is empty.
//
// M8: it used to return the *unresolved* path when the walk reached the
// filesystem root without finding anything that exists. The caller then
// compared that unresolved path against a symlink-resolved root — the exact
// namespace mismatch ResolveForCompare's doc comment warns gives a wrong answer
// "in whichever direction the link points" — and the wrong answer here is a
// confinement verdict. It is a should-not-happen case (every real path has an
// existing ancestor; the root itself exists), so failing closed costs nothing
// and saying so is better than silently comparing across namespaces.
func resolveExisting(path string) (resolved, tail string, err error) {
	// Try the full path first.
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, "", nil
	}

	// Walk up until we find something that exists.
	dir := path
	var segments []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", errNoExistingAncestor
		}
		segments = append(segments, filepath.Base(dir))
		dir = parent
		if _, err := os.Stat(dir); err == nil {
			break
		}
	}

	real, err = filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}

	// Reverse the segments and rejoin.
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	return real, filepath.Join(segments...), nil
}

// --- shared path containment (CLN-1) ---
//
// "Is this path inside that root" had four implementations before this: the
// three below plus escapesRoot, and they did not agree. The server copy
// resolved no symlinks and treated root == target as inside; checkpoint
// resolved and case-folded but treated it as outside; drive resolved and
// treated it as inside but never case-folded. Three reviewers had to read
// three implementations to reason about one property, and the two production
// bugs this pass turned up (SEC-H and the shell-checkpoint capture) were both
// a resolved path compared against an unresolved root — the exact failure this
// spread invites.
//
// The variation that was real is kept as three named entry points rather than
// flattened away, because each caller genuinely wants a different question
// answered. What is no longer duplicated is the answer.

// WithinRoot reports whether target is root itself or nested beneath it,
// comparing lexically after case-folding on Windows (where "C:\Work" and
// "c:\work" are the same directory, so a case difference must not read as — or
// be used to disguise — an escape).
//
// It resolves no symlinks. Use it only where both arguments are already in the
// same namespace: either both unresolved, or both already resolved by the
// caller, which is what the ValidatePath* functions above do. When in doubt
// use WithinRootResolved — a resolved path compared against an unresolved root
// silently reports every path as an escape.
func WithinRoot(root, target string) bool {
	return !escapesRoot(root, target)
}

// WithinRootResolved is WithinRoot with both sides put through
// ResolveForCompare first, so a root or a target reached through a symlink is
// judged on the directory it actually names. This is the safe default.
func WithinRootResolved(root, target string) bool {
	return WithinRoot(ResolveForCompare(root), ResolveForCompare(target))
}

// StrictlyWithinRootResolved is WithinRootResolved with root itself excluded:
// target must name something *beneath* root, not root.
//
// The distinction matters where target is known to be a file rather than a
// directory — internal/checkpoint's restore validation, where a captured path
// equal to the workspace root is not a file that could have been captured, and
// refusing it is the conservative reading.
func StrictlyWithinRootResolved(root, target string) bool {
	rr, rt := ResolveForCompare(root), ResolveForCompare(target)
	if !WithinRoot(rr, rt) {
		return false
	}
	rel, err := filepath.Rel(foldPathForCompare(rr), foldPathForCompare(rt))
	return err == nil && rel != "."
}

// ResolveForCompare turns p into the path containment should be judged on:
// symlinks resolved as far as the filesystem can, the rest cleaned lexically.
//
// A path legitimately may not exist yet — a checkpoint captures a file that is
// about to be created, or one deleted during the turn that restore will
// recreate — so EvalSymlinks on the full path is not enough. Walk up to the
// deepest ancestor that does exist, resolve that, and re-append the components
// below it. That still catches a symlinked directory in the middle of the path
// (the classic escape) while letting a not-yet-existing leaf inside the root
// validate.
func ResolveForCompare(p string) string {
	if p == "" {
		return p
	}
	resolved, tail, err := resolveExisting(filepath.Clean(p))
	if err != nil {
		// Nothing on the path exists, so there is nothing to resolve: the
		// lexically cleaned path is the best available comparison form. Unlike
		// the validators above this is not a gate — it normalizes both sides of
		// a comparison a caller then makes — so there is no verdict to fail
		// closed on here.
		return filepath.Clean(p)
	}
	if tail == "" {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(filepath.Join(resolved, tail))
}

// foldPathForCompare normalizes case where the platform's filesystem does.
// EvalSymlinks on Windows can hand back a different casing (or the long form
// of an 8.3 name) than the one recorded earlier, so a case-sensitive
// comparison would reject legitimate paths.
func foldPathForCompare(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
