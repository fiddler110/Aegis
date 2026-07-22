package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// scanPendingMarkers is the completion oracle for P38.2 drive-to-completion:
// the drive loop keeps running while it returns a non-empty list. It must find
// the marker in nested generated files, ignore resolved/unrelated files, and
// return stable, sorted, forward-slash relative paths.
func TestScanPendingMarkers(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("security/threat-model/run/2-analysis.md", "# Analysis\n<!-- PENDING -->\n")
	write("security/threat-model/run/0.1-architecture.md", "# Architecture\nfully written, no marker\n")
	write("security/threat-model/run/1.1-model.mmd", "flowchart LR\n<!-- PENDING -->\n")
	// P38.7: scaffold.py emits section-keyed markers; scanPendingMarkers matches
	// the `<!-- PENDING` prefix, so a keyed marker must still count as unfinished.
	write("security/threat-model/run/0-assessment.md", "# Assessment\n<!-- PENDING: key-components -->\n")
	write("security/threat-model/run/inventory.yaml", "components: []\n") // resolved
	write("security/threat-model/run/notes.bin", "<!-- PENDING -->")       // non-text ext, ignored

	got := scanPendingMarkers(root)
	want := []string{
		"security/threat-model/run/0-assessment.md",
		"security/threat-model/run/1.1-model.mmd",
		"security/threat-model/run/2-analysis.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanPendingMarkers:\n got  %v\n want %v", got, want)
	}

	// A tree with no markers (a completed run) must report done.
	done := t.TempDir()
	if err := os.WriteFile(filepath.Join(done, "x.md"), []byte("all done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if h := scanPendingMarkers(done); len(h) != 0 {
		t.Fatalf("expected no markers in completed tree, got %v", h)
	}

	// A missing root is a no-op (not an error) — the skill may not have created
	// .aegis yet on the first turn.
	if h := scanPendingMarkers(filepath.Join(root, "does-not-exist")); len(h) != 0 {
		t.Fatalf("missing root should yield no markers, got %v", h)
	}
}

// suiteFileCount is the P38.6 floor check: it distinguishes "finished, every
// marker resolved" from "nothing was ever written" (a fabricated completion) —
// both leave scanPendingMarkers empty. It counts text-ish files anywhere under
// root and ignores non-suite extensions; a missing root is zero.
func TestSuiteFileCount(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// An empty tree (the fabrication signature) counts zero.
	if n := suiteFileCount(root); n != 0 {
		t.Fatalf("empty tree: got %d, want 0", n)
	}
	// A missing root is zero, not an error.
	if n := suiteFileCount(filepath.Join(root, "nope")); n != 0 {
		t.Fatalf("missing root: got %d, want 0", n)
	}

	write("security/threat-model/run/0-assessment.md", "x")
	write("security/threat-model/run/1.1-model.mmd", "y")
	write("security/threat-model/run/inventory.yaml", "z")
	write("security/threat-model/run/scratch.bin", "ignored") // non-suite ext
	if n := suiteFileCount(root); n != 3 {
		t.Fatalf("populated tree: got %d, want 3", n)
	}
}

func TestAppendUnique(t *testing.T) {
	base := []string{"a", "b"}
	// Case-insensitive dedupe, and the input slice is never mutated.
	if got := appendUnique(base, "A"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("appendUnique dup: got %v", got)
	}
	got := appendUnique(base, "c")
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("appendUnique new: got %v", got)
	}
	if !reflect.DeepEqual(base, []string{"a", "b"}) {
		t.Errorf("appendUnique mutated its input: %v", base)
	}
}

// The continuation turn must name every still-pending file and forbid a pause,
// so a small local model resumes the exact remaining work instead of yielding.
func TestContinuePrompt(t *testing.T) {
	p := continuePrompt([]string{"a/1.md", "a/2.md"})
	// P38.7: the continuation must warn off the replace_all file-nuke.
	for _, want := range []string{"a/1.md", "a/2.md", "PENDING", "do not stop", "dependency order", "replace_all"} {
		if !strings.Contains(p, want) {
			t.Errorf("continuePrompt missing %q in:\n%s", want, p)
		}
	}
}

// actNowPrompt (P39.7) must still name every pending file (it wraps
// continuePrompt) and additionally forbid narration, so a model that yielded
// with no tool call is pushed to act instead of describing its plan again.
func TestActNowPrompt(t *testing.T) {
	p := actNowPrompt([]string{"a/1.md", "a/2.md"})
	for _, want := range []string{"a/1.md", "a/2.md", "PENDING", "ACT NOW", "edit_file", "Do not describe or narrate"} {
		if !strings.Contains(p, want) {
			t.Errorf("actNowPrompt missing %q in:\n%s", want, p)
		}
	}
}

// skillPreamble frames the body as authoritative instructions the same way the
// TUI's /threat-model path does, so both surfaces present the skill identically.
func TestSkillPreamble(t *testing.T) {
	pre := skillPreamble("threat-modeling", "BODY-CONTENT")
	for _, want := range []string{"threat-modeling skill has been loaded", "<skill name=\"threat-modeling\">", "BODY-CONTENT"} {
		if !strings.Contains(pre, want) {
			t.Errorf("skillPreamble missing %q in:\n%s", want, pre)
		}
	}
}
