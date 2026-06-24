package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
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
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)

	// Fast check before symlink resolution: reject obvious escapes.
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", path)
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
	rel, err = filepath.Rel(realRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the workspace (symlink escape)", path)
	}

	return full, nil
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
