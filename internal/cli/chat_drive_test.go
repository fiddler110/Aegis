package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
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
	write("security/threat-model/run/notes.bin", "<!-- PENDING -->")      // non-text ext, ignored

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

// The P39.7 act-now nudge must be forceful and name edit_file so a stalled
// local model breaks out of "announce then yield" and mutates a file.
func TestActNowNudge(t *testing.T) {
	n := actNowNudge()
	for _, want := range []string{"ACT NOW", "edit_file", "PENDING", "one section"} {
		if !strings.Contains(n, want) {
			t.Errorf("actNowNudge missing %q in:\n%s", want, n)
		}
	}
}

// sameStrings is the P39.7 progress oracle for the PENDING set: equal sorted
// slices mean the turn changed nothing.
func TestSameStrings(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{nil, []string{"a"}, false},                // first fill after scaffold: 0 → some markers
		{[]string{"a", "b"}, []string{"a"}, false}, // a marker resolved
		{[]string{"a"}, []string{"b"}, false},      // same count, different set
	}
	for _, c := range cases {
		if got := sameStrings(c.a, c.b); got != c.want {
			t.Errorf("sameStrings(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// mutatingTools must classify exactly the file-writing tools as progress for
// the P39.7 guard — a read or narration turn is not progress.
func TestMutatingTools(t *testing.T) {
	for _, name := range []string{"write_file", "edit_file", "multi_edit"} {
		if !mutatingTools[name] {
			t.Errorf("expected %q to count as a mutation", name)
		}
	}
	for _, name := range []string{"read_file", "grep", "shell", "skill", "ls"} {
		if mutatingTools[name] {
			t.Errorf("%q must not count as a file mutation", name)
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

// compactSkillPreamble (P39.5) must drop the skill body, keep the task, and
// point at the on-disk SKILL.md to re-read.
func TestCompactSkillPreamble(t *testing.T) {
	p := compactSkillPreamble("threat-modeling", "Model the app under /repo.", "/data/builtin-skills/threat-modeling")
	for _, want := range []string{"threat-modeling", "Model the app under /repo.", "SKILL.md", "remain in effect"} {
		if !strings.Contains(p, want) {
			t.Errorf("compactSkillPreamble missing %q in:\n%s", want, p)
		}
	}
	// Must NOT carry a full skill body / <skill> wrapper.
	if strings.Contains(p, "<skill name=") {
		t.Errorf("compact preamble must not re-embed the skill body:\n%s", p)
	}
}

// compactFirstSkillMessage (P39.5) rewrites the first user message once, only
// while it still carries the full preamble, and shrinks it.
func TestCompactFirstSkillMessage(t *testing.T) {
	full := skillPreamble("threat-modeling", strings.Repeat("INSTRUCTIONS ", 2000)) + "Do the task."
	conv := &engine.Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: full}}})

	if !compactFirstSkillMessage(conv, "threat-modeling", "Do the task.", "/skills/threat-modeling") {
		t.Fatal("expected first-message compaction to fire")
	}
	got := conv.Messages[0].Content[0].(provider.TextBlock).Text
	if strings.Contains(got, "<skill name=") {
		t.Errorf("compaction left the skill body in place:\n%s", got[:200])
	}
	if !strings.Contains(got, "Do the task.") {
		t.Error("compaction dropped the task text")
	}
	if len(got) >= len(full) {
		t.Errorf("compaction did not shrink the message (%d >= %d)", len(got), len(full))
	}

	// Idempotent: a second call is a no-op because the body is gone.
	if compactFirstSkillMessage(conv, "threat-modeling", "Do the task.", "/skills/threat-modeling") {
		t.Error("second compaction should be a no-op")
	}

	// A non-skill message is never touched.
	plain := &engine.Conversation{}
	plain.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "just a normal prompt"}}})
	if compactFirstSkillMessage(plain, "threat-modeling", "just a normal prompt", "") {
		t.Error("a message without the preamble must not be rewritten")
	}
}

// compatDriveWindowNotice (P39.9) warns only when the served window is too small
// for a skill drive, and includes the modelfile recipe.
func TestCompatDriveWindowNotice(t *testing.T) {
	// Served window large enough: no notice.
	if n := compatDriveWindowNotice("qwen3.6:35b-a3b-fast", 40000, 32768); n != "" {
		t.Errorf("large served window should not warn, got:\n%s", n)
	}
	// Too small: warns, names num_ctx and the derived model recipe.
	n := compatDriveWindowNotice("qwen3.6:35b-a3b-fast", 16384, 32768)
	for _, want := range []string{"16384 tokens", "num_ctx", "ollama create", "provider.default: ollama"} {
		if !strings.Contains(n, want) {
			t.Errorf("small-window notice missing %q in:\n%s", want, n)
		}
	}
	// Undetectable served window (0): still warns, describes it as unknown.
	if n := compatDriveWindowNotice("qwen3.6:35b-a3b-fast", 0, 32768); !strings.Contains(n, "unknown") {
		t.Errorf("undetectable window should warn as unknown, got:\n%s", n)
	}
}
