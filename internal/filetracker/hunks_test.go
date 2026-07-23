package filetracker

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeFile is a helper that fails the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRecordAgentWrite_NewFile(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "a\nb\nc"
	writeFile(t, path, content)
	tr.RecordAgentWrite(path, "", content)

	got := tr.AgentHunks(path)
	want := []Hunk{{Start: 1, End: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("new file hunks = %v, want %v", got, want)
	}
}

func TestRecordAgentWrite_EditMiddleLine(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	old := "a\nb\nc\nd\ne"
	updated := "a\nb\nCHANGED\nd\ne"
	writeFile(t, path, updated)
	tr.RecordAgentWrite(path, old, updated)

	got := tr.AgentHunks(path)
	want := []Hunk{{Start: 3, End: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("edit-middle hunks = %v, want %v", got, want)
	}
}

func TestRecordAgentWrite_MergesAcrossWrites(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	// First agent write: change line 2.
	v1old := "a\nb\nc\nd\ne"
	v1 := "a\nB\nc\nd\ne"
	writeFile(t, path, v1)
	tr.RecordAgentWrite(path, v1old, v1)

	// Second agent write: change line 4. Prior hunk (line 2) must survive and
	// remap, and the new hunk (line 4) is added — two disjoint hunks.
	v2 := "a\nB\nc\nD\ne"
	writeFile(t, path, v2)
	tr.RecordAgentWrite(path, v1, v2)

	got := tr.AgentHunks(path)
	want := []Hunk{{Start: 2, End: 2}, {Start: 4, End: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cumulative hunks = %v, want %v", got, want)
	}
}

func TestRecordAgentWrite_AdjacentHunksMerge(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	// Change line 2, then line 3 (adjacent) — should coalesce to 2..3.
	v0 := "a\nb\nc\nd"
	v1 := "a\nB\nc\nd"
	writeFile(t, path, v1)
	tr.RecordAgentWrite(path, v0, v1)

	v2 := "a\nB\nC\nd"
	writeFile(t, path, v2)
	tr.RecordAgentWrite(path, v1, v2)

	got := tr.AgentHunks(path)
	want := []Hunk{{Start: 2, End: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("adjacent hunks = %v, want %v", got, want)
	}
}

func TestAgentHunks_ExternalEditDropsOnlyOverlapping(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	// Agent authors two separate lines: 2 and 5.
	v0 := "a\nb\nc\nd\ne\nf"
	v1 := "a\nB\nc\nd\nE\nf"
	writeFile(t, path, v1)
	tr.RecordAgentWrite(path, v0, v1)
	if got := tr.AgentHunks(path); !reflect.DeepEqual(got, []Hunk{{2, 2}, {5, 5}}) {
		t.Fatalf("precondition hunks = %v", got)
	}

	// External edit changes line 2 (the agent's first hunk) but leaves line 5
	// intact. The agent's line-2 hunk must drop; line 5 must survive.
	external := "a\nEXTERNAL\nc\nd\nE\nf"
	writeFile(t, path, external)

	got := tr.AgentHunks(path)
	want := []Hunk{{Start: 5, End: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after external edit hunks = %v, want %v", got, want)
	}
}

func TestAgentHunks_ExternalInsertShiftsSurvivors(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	// Agent authors line 3.
	v0 := "a\nb\nc\nd"
	v1 := "a\nb\nC\nd"
	writeFile(t, path, v1)
	tr.RecordAgentWrite(path, v0, v1)

	// External edit inserts two lines at the top; the agent's line moves to 5.
	external := "x\ny\na\nb\nC\nd"
	writeFile(t, path, external)

	got := tr.AgentHunks(path)
	want := []Hunk{{Start: 5, End: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after external insert hunks = %v, want %v", got, want)
	}
}

func TestAgentHunks_ExternalInsertInsideHunkDropsIt(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	// Agent authors a 2-line hunk (lines 2..3).
	v0 := "a\nd"
	v1 := "a\nB\nC\nd"
	writeFile(t, path, v1)
	tr.RecordAgentWrite(path, v0, v1)
	if got := tr.AgentHunks(path); !reflect.DeepEqual(got, []Hunk{{2, 3}}) {
		t.Fatalf("precondition hunks = %v", got)
	}

	// External edit inserts a line *between* the agent's two lines; the hunk is
	// no longer contiguous agent content, so it drops entirely.
	external := "a\nB\nMIDDLE\nC\nd"
	writeFile(t, path, external)

	if got := tr.AgentHunks(path); got != nil {
		t.Errorf("hunk should drop when split by external insert, got %v", got)
	}
}

func TestAgentHunks_Untracked(t *testing.T) {
	tr := New()
	if got := tr.AgentHunks(filepath.Join(t.TempDir(), "nope.txt")); got != nil {
		t.Errorf("untracked file should have no hunks, got %v", got)
	}
}

func TestAgentHunks_DeletedFile(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "a\nb")
	tr.RecordAgentWrite(path, "", "a\nb")
	os.Remove(path)
	if got := tr.AgentHunks(path); got != nil {
		t.Errorf("deleted file should have no hunks, got %v", got)
	}
}

func TestClearResetsHunks(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "a")
	tr.RecordAgentWrite(path, "", "a")
	tr.Clear()
	if got := tr.AgentHunks(path); got != nil {
		t.Errorf("Clear should drop hunk state, got %v", got)
	}
}

func TestHunkPruning(t *testing.T) {
	tr := New()
	dir := t.TempDir()
	// Exceed the cap; the map must not grow unbounded.
	for i := range maxTrackedFiles + 50 {
		p := filepath.Join(dir, "f")
		// distinct virtual paths (files need not exist for RecordAgentWrite)
		vp := p + string(rune('a'+i%26)) + itoa(i)
		tr.RecordAgentWrite(vp, "", "x")
	}
	tr.mu.Lock()
	n := len(tr.hunks)
	tr.mu.Unlock()
	if n > maxTrackedFiles {
		t.Errorf("hunk map grew past cap: %d > %d", n, maxTrackedFiles)
	}
}

// itoa avoids importing strconv for one call in a test.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestDiffLines_Primitives(t *testing.T) {
	tests := []struct {
		name        string
		old, new    string
		wantChanged []Hunk
	}{
		{"append", "a\nb", "a\nb\nc", []Hunk{{3, 3}}},
		{"prepend", "b\nc", "a\nb\nc", []Hunk{{1, 1}}},
		{"replace-all", "a\nb", "x\ny", []Hunk{{1, 2}}},
		{"no-change", "a\nb", "a\nb", nil},
		{"empty-to-content", "", "a\nb", []Hunk{{1, 2}}},
		{"content-to-empty", "a\nb", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := diffLines(splitLines(tc.old), splitLines(tc.new))
			if !reflect.DeepEqual(got, tc.wantChanged) {
				t.Errorf("diffLines changed = %v, want %v", got, tc.wantChanged)
			}
		})
	}
}
