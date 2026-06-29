package tui

import "strings"

// findSafeMarkdownBoundary returns the byte offset of the last "safe" paragraph
// break (blank line outside a code fence) in s. Re-wrapping only the suffix
// after this boundary keeps streaming render cost O(tail) instead of O(n²).
// Returns 0 when no safe boundary is found.
func findSafeMarkdownBoundary(s string) int {
	inFence := false
	boundary := 0

	i := 0
	for i < len(s) {
		end := strings.IndexByte(s[i:], '\n')
		if end < 0 {
			break // incomplete final line — not safe to treat as a boundary
		}
		lineEnd := i + end
		trimmed := strings.TrimSpace(s[i:lineEnd])

		if isFenceMarker(trimmed) {
			inFence = !inFence
		}

		// A blank line outside a fence is a safe wrapping boundary.
		// Point to the character immediately after the newline.
		if !inFence && trimmed == "" {
			next := lineEnd + 1
			if next <= len(s) {
				boundary = next
			}
		}

		i = lineEnd + 1
	}

	return boundary
}

// isFenceMarker reports whether line begins a code fence (3+ backticks or tildes).
func isFenceMarker(line string) bool {
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '`' && c != '~' {
		return false
	}
	return line[1] == c && line[2] == c
}
