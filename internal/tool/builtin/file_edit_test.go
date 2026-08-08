package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/tool"
)

// execTool runs a tool and fails the test on a transport error, returning the
// result so the caller can assert on IsError/Content.
func execTool(t *testing.T, run func(context.Context, json.RawMessage) (tool.Result, error), args any) tool.Result {
	t.Helper()
	res, err := run(context.Background(), mustJSON(t, args))
	if err != nil {
		t.Fatalf("tool returned a transport error: %v", err)
	}
	return res
}

func writeFileT(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestMultiEditRejectsAmbiguousMatch is the headline regression: multi_edit
// counted occurrences but only checked for zero, so an ambiguous old_string
// replaced the *first* match and reported success — while edit_file rejected
// the identical edit. An agent batching edits got a plausible confirmation and
// the wrong file.
func TestMultiEditRejectsAmbiguousMatch(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.txt")
	writeFileT(t, p, "x\nx\nx\n", 0o644)

	me := &multieditTool{root: root}
	res := execTool(t, me.Execute, map[string]any{"edits": []map[string]any{
		{"path": "f.txt", "old_string": "x", "new_string": "y"},
	}})
	if !res.IsError {
		t.Fatalf("expected an ambiguous-match error, got %+v", res)
	}
	if !strings.Contains(res.Content, "occurs 3 times") {
		t.Errorf("error should report the occurrence count, got %q", res.Content)
	}
	if got := readFileT(t, p); got != "x\nx\nx\n" {
		t.Errorf("file must be untouched after a rejected edit, got %q", got)
	}
}

// TestMultiEditReplaceAll covers the escape hatch the ambiguity check needs: a
// genuine repeated-token rename was previously inexpressible in multi_edit.
func TestMultiEditReplaceAll(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.txt")
	writeFileT(t, p, "x\nx\nx\n", 0o644)

	me := &multieditTool{root: root}
	res := execTool(t, me.Execute, map[string]any{"edits": []map[string]any{
		{"path": "f.txt", "old_string": "x", "new_string": "y", "replace_all": true},
	}})
	if res.IsError {
		t.Fatalf("replace_all edit failed: %+v", res)
	}
	if got := readFileT(t, p); got != "y\ny\ny\n" {
		t.Errorf("got %q, want all occurrences replaced", got)
	}
}

// TestMultiEditPreservesFileMode covers multi_edit's hardcoded 0o644, which
// widened a 0o600 file to world-readable and dropped the exec bit off a 0o700
// script — while write_file and edit_file both preserved the mode.
func TestMultiEditPreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not model POSIX permission bits")
	}
	root := t.TempDir()
	p := filepath.Join(root, "run.sh")
	writeFileT(t, p, "echo old\n", 0o700)

	me := &multieditTool{root: root}
	res := execTool(t, me.Execute, map[string]any{"edits": []map[string]any{
		{"path": "run.sh", "old_string": "old", "new_string": "new"},
	}})
	if res.IsError {
		t.Fatalf("edit failed: %+v", res)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %v, want 0700 preserved", got)
	}
}

// TestMultiEditAtomicAcrossFiles checks the all-or-nothing promise in the tool
// description: a batch whose later edit cannot apply must leave every file
// untouched, including ones whose own edits were valid.
func TestMultiEditAtomicAcrossFiles(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	writeFileT(t, a, "alpha\n", 0o644)
	writeFileT(t, b, "beta\n", 0o644)

	me := &multieditTool{root: root}
	res := execTool(t, me.Execute, map[string]any{"edits": []map[string]any{
		{"path": "a.txt", "old_string": "alpha", "new_string": "ALPHA"},
		{"path": "b.txt", "old_string": "nowhere", "new_string": "X"},
	}})
	if !res.IsError {
		t.Fatalf("expected failure on the unmatched edit, got %+v", res)
	}
	if got := readFileT(t, a); got != "alpha\n" {
		t.Errorf("a.txt was modified despite the batch failing: %q", got)
	}
	if got := readFileT(t, b); got != "beta\n" {
		t.Errorf("b.txt = %q, want untouched", got)
	}
}

// TestMultiEditCreatesFile covers the gap that forced a batch mixing new and
// existing files to be split across calls.
func TestMultiEditCreatesFile(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, filepath.Join(root, "old.txt"), "one\n", 0o644)

	me := &multieditTool{root: root}
	res := execTool(t, me.Execute, map[string]any{"edits": []map[string]any{
		{"path": "sub/new.txt", "old_string": "", "new_string": "created\n"},
		{"path": "old.txt", "old_string": "one", "new_string": "two"},
		{"path": "sub/new.txt", "old_string": "created", "new_string": "amended"},
	}})
	if res.IsError {
		t.Fatalf("create-and-edit batch failed: %+v", res)
	}
	if got := readFileT(t, filepath.Join(root, "sub", "new.txt")); got != "amended\n" {
		t.Errorf("new file = %q, want later edits in the same batch applied", got)
	}
	if got := readFileT(t, filepath.Join(root, "old.txt")); got != "two\n" {
		t.Errorf("old.txt = %q", got)
	}
}

// TestMultiEditMissingFileWithoutCreate keeps the create path opt-in: a typo'd
// path must not silently become a new file.
func TestMultiEditMissingFileWithoutCreate(t *testing.T) {
	root := t.TempDir()
	me := &multieditTool{root: root}
	res := execTool(t, me.Execute, map[string]any{"edits": []map[string]any{
		{"path": "typo.txt", "old_string": "a", "new_string": "b"},
	}})
	if !res.IsError {
		t.Fatalf("expected an error for a missing file, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "typo.txt")); err == nil {
		t.Error("a missing path must not be created implicitly")
	}
}

// TestEditRejectsNoOp guards against a retry loop: an edit whose old_string and
// new_string are identical used to report "1 replacement(s)", so a model
// re-issuing it forever believed it was making progress.
func TestEditRejectsNoOp(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, filepath.Join(root, "f.txt"), "hello\n", 0o644)
	e := &editTool{root: root}
	res := execTool(t, e.Execute, map[string]any{
		"path": "f.txt", "old_string": "hello", "new_string": "hello",
	})
	if !res.IsError {
		t.Fatalf("expected a no-op edit to be rejected, got %+v", res)
	}
	if !strings.Contains(res.Content, "identical") {
		t.Errorf("error should explain the no-op, got %q", res.Content)
	}
}

// TestEditRejectsEmptyOldString replaces a confusing occurrence-count error
// ("old_string occurs 4 times") with one that names the actual mistake.
func TestEditRejectsEmptyOldString(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, filepath.Join(root, "f.txt"), "abc", 0o644)
	e := &editTool{root: root}
	res := execTool(t, e.Execute, map[string]any{
		"path": "f.txt", "old_string": "", "new_string": "Z",
	})
	if !res.IsError || !strings.Contains(res.Content, "old_string is empty") {
		t.Fatalf("want an empty-old_string error, got %+v", res)
	}
	if got := readFileT(t, filepath.Join(root, "f.txt")); got != "abc" {
		t.Errorf("file changed: %q", got)
	}
}

// TestEditRejectsBinaryFile: substring replacement over arbitrary bytes is not
// meaningful, and writing the result back would mangle the file.
func TestEditRejectsBinaryFile(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, filepath.Join(root, "a.bin"), "\x89PNG\x00\x1a\n", 0o644)
	e := &editTool{root: root}
	res := execTool(t, e.Execute, map[string]any{
		"path": "a.bin", "old_string": "PNG", "new_string": "JPG",
	})
	if !res.IsError || !strings.Contains(res.Content, "binary") {
		t.Fatalf("want a binary-file rejection, got %+v", res)
	}
}

// TestReadRejectsBinaryFile: a numbered dump of PNG bytes is worse than an
// error — the model reasons over mojibake instead of stopping, and raw control
// bytes reach the transcript.
func TestReadRejectsBinaryFile(t *testing.T) {
	root := t.TempDir()
	writeFileT(t, filepath.Join(root, "a.png"), "\x89PNG\x00\x1a\n\xff\xfe", 0o644)
	r := &readTool{root: root}
	res := execTool(t, r.Execute, map[string]any{"path": "a.png"})
	if !res.IsError || !strings.Contains(res.Content, "binary") {
		t.Fatalf("want a binary-file rejection, got %+v", res)
	}
	if strings.Contains(res.Content, "\x00") {
		t.Error("rejection message must not echo the file's control bytes")
	}
}

// TestWriteRefusesOverwriteAfterPartialRead is the data-loss regression. P38.1
// caps an unbounded read at defaultReadLines, but the tracker recorded the file
// as fully seen, so a following write_file replaced content the agent never
// read — probed at 10000 bytes down to 10, reported as success.
func TestWriteRefusesOverwriteAfterPartialRead(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "big.txt")
	var sb strings.Builder
	for i := 0; i < defaultReadLines+100; i++ {
		sb.WriteString("line\n")
	}
	writeFileT(t, p, sb.String(), 0o644)

	ft := filetracker.New()
	r := &readTool{root: root, tracker: ft}
	if res := execTool(t, r.Execute, map[string]any{"path": "big.txt"}); res.IsError {
		t.Fatalf("read: %+v", res)
	}

	w := &writeTool{root: root, tracker: ft}
	res := execTool(t, w.Execute, map[string]any{"path": "big.txt", "content": "truncated\n"})
	if !res.IsError {
		t.Fatalf("expected the overwrite to be refused, got %+v", res)
	}
	if got := readFileT(t, p); got != sb.String() {
		t.Fatalf("file was clobbered: %d bytes left of %d", len(got), sb.Len())
	}

	// An anchored edit stays allowed — it can only change text it matched.
	e := &editTool{root: root, tracker: ft}
	if res := execTool(t, e.Execute, map[string]any{
		"path": "big.txt", "old_string": "line\n", "new_string": "LINE\n", "replace_all": true,
	}); res.IsError {
		t.Fatalf("edit after a partial read must still be allowed: %+v", res)
	}
	// ...and must not launder the partial read into a licence to overwrite.
	if res := execTool(t, w.Execute, map[string]any{
		"path": "big.txt", "content": "still truncating\n",
	}); !res.IsError {
		t.Error("an anchored edit must not clear the partial-read flag")
	}
}

// TestWriteAllowedAfterCompleteRead keeps the guard narrow: reading a short
// file whole, or re-reading with a limit that reaches the end, must leave
// write_file working normally.
func TestWriteAllowedAfterCompleteRead(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "small.txt")
	writeFileT(t, p, "a\nb\nc\n", 0o644)

	ft := filetracker.New()
	r := &readTool{root: root, tracker: ft}
	if res := execTool(t, r.Execute, map[string]any{"path": "small.txt"}); res.IsError {
		t.Fatalf("read: %+v", res)
	}
	w := &writeTool{root: root, tracker: ft}
	if res := execTool(t, w.Execute, map[string]any{"path": "small.txt", "content": "z\n"}); res.IsError {
		t.Fatalf("write after a complete read must be allowed: %+v", res)
	}
	if got := readFileT(t, p); got != "z\n" {
		t.Errorf("got %q", got)
	}
}

// TestWriteAllowedOnUnreadFile preserves the pre-existing allowance: a file the
// agent has never read is not subject to the partial-read guard.
func TestWriteAllowedOnUnreadFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "untouched.txt")
	writeFileT(t, p, "original\n", 0o644)

	ft := filetracker.New()
	w := &writeTool{root: root, tracker: ft}
	if res := execTool(t, w.Execute, map[string]any{"path": "untouched.txt", "content": "new\n"}); res.IsError {
		t.Fatalf("write to an unread file must be allowed: %+v", res)
	}
}
