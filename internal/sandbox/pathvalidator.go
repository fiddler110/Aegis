package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	abs = filepath.Clean(abs)

	// Fast check before symlink resolution: reject obvious escapes.
	if escapesRoot(root, abs) {
		return "", fmt.Errorf("path %q escapes the workspace root %q", path, root)
	}

	// Resolve symlinks on the real filesystem. If the full path exists, resolve
	// it directly. Otherwise, walk up to the nearest existing ancestor and
	// resolve that, then re-append the remaining segments.
	resolved, tail := resolveExisting(abs)

	// Check the resolved real path is still within root.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}

	full := filepath.Join(resolved, tail)
	if escapesRoot(realRoot, full) {
		return "", fmt.Errorf("path %q resolves outside the workspace root %q (symlink escape)", path, root)
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

// resolveExisting walks up from path until it finds an existing directory,
// resolves symlinks on that ancestor, and returns (resolvedAncestor,
// remainingTail). For a fully existing path, tail is empty.
func resolveExisting(path string) (resolved, tail string) {
	// Try the full path first.
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, ""
	}

	// Walk up until we find something that exists.
	dir := path
	var segments []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding an existing path.
			return path, ""
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
	return real, filepath.Join(segments...)
}
