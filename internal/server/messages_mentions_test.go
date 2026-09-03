package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExpandFileMentionsConfinement verifies that @path#L1-2 mentions can only
// read files inside the workspace: a "../" escape must be left as-is and its
// contents must never be spliced into the returned text (CWE-22, gosec G703).
func TestExpandFileMentionsConfinement(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "in.txt"), []byte("alpha\nbravo\ncharlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A secret file one level above the workspace.
	outside := filepath.Join(filepath.Dir(ws), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOPSECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	t.Run("in-workspace mention expands", func(t *testing.T) {
		got := expandFileMentions("show @in.txt#L1-2", ws)
		if !strings.Contains(got, "alpha") || !strings.Contains(got, "bravo") {
			t.Fatalf("expected file contents to be expanded, got %q", got)
		}
	})

	t.Run("traversal is blocked", func(t *testing.T) {
		in := "leak @../secret.txt#L1-1"
		got := expandFileMentions(in, ws)
		if strings.Contains(got, "TOPSECRET") {
			t.Fatalf("path traversal leaked file outside workspace: %q", got)
		}
		// Unresolved tokens are left verbatim.
		if got != in {
			t.Fatalf("expected token left as-is, got %q", got)
		}
	})

	t.Run("symlink escape is blocked", func(t *testing.T) {
		// VULN-07: a workspace symlink used to bypass expandFileMentions'
		// purely lexical ".." check, since the mention text itself never
		// names a traversal — the check now runs through sandbox.ValidatePath
		// the same way every other read path does.
		link := filepath.Join(ws, "escape.txt")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlinks unavailable in this environment (%v); on Windows enable Developer Mode or run elevated to exercise this test", err)
		}
		in := "leak @escape.txt#L1-1"
		got := expandFileMentions(in, ws)
		if strings.Contains(got, "TOPSECRET") {
			t.Fatalf("symlink escape leaked file outside workspace: %q", got)
		}
		if got != in {
			t.Fatalf("expected token left as-is, got %q", got)
		}
	})
}
