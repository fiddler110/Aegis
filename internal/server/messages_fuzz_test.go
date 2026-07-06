package server

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzExpandFileMentions targets the @path#L10-40 file-mention parser
// (P5.5): it runs on every user message before the model sees it, so it
// must never panic on attacker- or accident-supplied text, regardless of
// how malformed the "@...#..." token looks (missing range, non-numeric
// bounds, huge line numbers, path traversal attempts, unicode).
func FuzzExpandFileMentions(f *testing.F) {
	dir := f.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		f.Fatal(err)
	}

	f.Add("read @a.go#L1-2 please")
	f.Add("@a.go#1-3")
	f.Add("@a.go#L0-0")
	f.Add("@a.go#L999999-1000000")
	f.Add("@../../../etc/passwd#L1-1")
	f.Add("@a.go#")
	f.Add("@#L1-2")
	f.Add("no mentions here")
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		_ = expandFileMentions(text, dir)
	})
}
