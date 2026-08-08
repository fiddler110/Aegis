package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWritePreservesExistingFileMode covers P40.2: overwriting an existing
// mode-sensitive file must not silently reset its permission bits (drop the
// exec bit / widen to world-readable), while a newly created file still lands
// at newFileMode. Unix-only — Windows doesn't carry POSIX permission bits.
func TestWritePreservesExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not meaningful on Windows")
	}
	dir := t.TempDir()

	// Pre-existing 0o700 file (e.g. a private script): overwrite must keep 0o700.
	existing := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(existing, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	w := &writeTool{root: dir}
	res, err := w.Execute(context.Background(), mustJSON(t, map[string]any{"path": "script.sh", "content": "new"}))
	if err != nil || res.IsError {
		t.Fatalf("write overwrite: %v %+v", err, res)
	}
	if got := statMode(t, existing); got != 0o700 {
		t.Errorf("overwrite reset mode to %o, want 0700 preserved", got)
	}

	// New file: lands at newFileMode.
	created := filepath.Join(dir, "fresh.txt")
	res, err = w.Execute(context.Background(), mustJSON(t, map[string]any{"path": "fresh.txt", "content": "hi"}))
	if err != nil || res.IsError {
		t.Fatalf("write new: %v %+v", err, res)
	}
	if got := statMode(t, created); got != os.FileMode(newFileMode) {
		t.Errorf("new file mode = %o, want %o", got, newFileMode)
	}

	// edit_file must preserve mode too.
	res, err = (&editTool{root: dir}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "script.sh", "old_string": "new", "new_string": "edited",
	}))
	if err != nil || res.IsError {
		t.Fatalf("edit: %v %+v", err, res)
	}
	if got := statMode(t, existing); got != 0o700 {
		t.Errorf("edit reset mode to %o, want 0700 preserved", got)
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// TestReadBoundedMatchesFullSplit covers P40.3: the bufio.Scanner-based bounded
// read must produce byte-identical output to the previous whole-file
// strings.Split approach across the tricky cases (trailing newline, CRLF,
// empty file, offset past EOF, offset+limit windows).
//
// Two cases produce a diagnostic notice instead of line-numbered text, because
// an empty successful result is unreadable to a model — a zero-byte file and an
// overshot offset both used to come back as "" (or a bare "1\t"), which reads as
// "there is nothing here" rather than naming which of the two happened. Those
// carry wantExact; every other case must still match the reference render byte
// for byte, which is the invariant this test exists to hold.
func TestReadBoundedMatchesFullSplit(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		offset    int
		limit     int
		wantExact string // when set, replaces the reference render entirely
	}{
		{name: "trailing-newline", content: "a\nb\nc\n"},
		{name: "no-trailing-newline", content: "a\nb\nc"},
		{name: "empty", content: "", wantExact: "[read_file: f is empty (0 bytes).]\n"},
		{name: "single-line", content: "only"},
		{name: "crlf", content: "a\r\nb\r\nc\r\n"},
		{name: "offset-mid", content: "l1\nl2\nl3\nl4\nl5\n", offset: 3},
		{name: "offset-and-limit", content: "l1\nl2\nl3\nl4\nl5\n", offset: 2, limit: 2},
		{name: "limit-only", content: "l1\nl2\nl3\nl4\n", limit: 2},
		{
			name: "offset-past-eof", content: "l1\nl2\n", offset: 10,
			wantExact: "[read_file: offset 10 is past the end of f, which has 3 line(s). Re-read with a smaller offset.]\n",
		},
		{name: "blank-lines", content: "\n\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "f"), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			r := &readTool{root: dir}
			args := map[string]any{"path": "f"}
			if tc.offset > 0 {
				args["offset"] = tc.offset
			}
			if tc.limit > 0 {
				args["limit"] = tc.limit
			}
			res, err := r.Execute(context.Background(), mustJSON(t, args))
			if err != nil || res.IsError {
				t.Fatalf("read: %v %+v", err, res)
			}
			want := tc.wantExact
			if want == "" {
				want = referenceRender(tc.content, tc.offset, tc.limit)
			}
			if res.Content != want {
				t.Errorf("bounded read mismatch\n got: %q\nwant: %q", res.Content, want)
			}
		})
	}
}

// TestReadDefaultLineCap covers P38.1: an unbounded read of a file longer than
// defaultReadLines returns only the first window plus a paging notice (so one
// read cannot balloon a turn's context), while an explicit limit is honored
// verbatim with no notice.
func TestReadDefaultLineCap(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	total := defaultReadLines + 500
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &readTool{root: dir}

	// Unbounded read: capped to defaultReadLines, with a continue notice.
	res, err := r.Execute(context.Background(), mustJSON(t, map[string]any{"path": "big.txt"}))
	if err != nil || res.IsError {
		t.Fatalf("read: %v %+v", err, res)
	}
	if strings.Contains(res.Content, fmt.Sprintf("%d\tline %d", defaultReadLines+1, defaultReadLines+1)) {
		t.Errorf("unbounded read leaked past the %d-line cap", defaultReadLines)
	}
	if !strings.Contains(res.Content, fmt.Sprintf("%d\tline %d", defaultReadLines, defaultReadLines)) {
		t.Errorf("unbounded read did not reach the %d-line cap", defaultReadLines)
	}
	if !strings.Contains(res.Content, "read_file: showing lines 1-") || !strings.Contains(res.Content, fmt.Sprintf("offset=%d", defaultReadLines+1)) {
		t.Errorf("expected paging notice with next offset, got tail: %q", lastN(res.Content, 200))
	}

	// Explicit limit: honored verbatim, no notice injected.
	res, err = r.Execute(context.Background(), mustJSON(t, map[string]any{"path": "big.txt", "limit": 3}))
	if err != nil || res.IsError {
		t.Fatalf("read limit: %v %+v", err, res)
	}
	if strings.Contains(res.Content, "read_file: showing lines") {
		t.Errorf("explicit limit must not inject a paging notice, got: %q", res.Content)
	}
	if res.Content != "1\tline 1\n2\tline 2\n3\tline 3\n" {
		t.Errorf("explicit limit output = %q", res.Content)
	}
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// referenceRender reproduces the pre-P40.3 whole-file strings.Split rendering,
// serving as the oracle the new scanner must match.
func referenceRender(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")
	start := 1
	if offset > 0 {
		start = offset
	}
	var b strings.Builder
	count := 0
	for i := start - 1; i < len(lines); i++ {
		if limit > 0 && count >= limit {
			break
		}
		fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
		count++
	}
	return b.String()
}
