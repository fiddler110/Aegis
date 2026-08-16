package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSpillIsBestEffort covers P64.1's third load-bearing rule: no session, no
// store, or a failed write leaves the inline result exactly as it was and never
// converts a success into isError. A spill is an improvement on the notice, never
// a precondition for it — so every failure path has to degrade to the pre-P64.1
// behaviour rather than surface.
//
// The unwritable-root case is the one that matters in practice (a read-only
// checkout, a full disk, a container mount with no write permission), and it is
// forced here by pointing the spill at a path that cannot become a directory.
func TestSpillIsBestEffort(t *testing.T) {
	body := strings.Repeat("x", 40_000)

	t.Run("no root", func(t *testing.T) {
		// No workdir in ctx and no root: nothing to write to.
		out := SpillTail(context.Background(), "", "shell", body, 4096, "recover somehow")
		if len(out) > 4096 {
			t.Errorf("returned %d bytes, over the cap", len(out))
		}
		if !strings.Contains(out, "truncated") {
			t.Errorf("a failed spill must still truncate and say so:\n%s", out)
		}
		if strings.Contains(out, spillDirRel) {
			t.Errorf("a failed spill must not name a path it did not write:\n%s", out)
		}
		if !strings.Contains(out, "recover somehow") {
			t.Errorf("the caller's own recovery hint must survive a failed spill:\n%s", out)
		}
	})

	t.Run("unwritable root", func(t *testing.T) {
		root := t.TempDir()
		// Occupy `.aegis` with a regular file so MkdirAll of .aegis/spill fails.
		if err := os.WriteFile(filepath.Join(root, ".aegis"), []byte("not a dir"), 0o600); err != nil {
			t.Fatal(err)
		}
		out := SpillHead(context.Background(), root, "grep", body, 4096, "")
		if len(out) > 4096 {
			t.Errorf("returned %d bytes, over the cap", len(out))
		}
		if !strings.Contains(out, "truncated") {
			t.Errorf("an unwritable spill must still truncate and say so:\n%s", out)
		}
		if strings.Contains(out, spillDirRel) {
			t.Errorf("an unwritable spill must not name a path it did not write:\n%s", out)
		}
	})
}

// TestSpilledRemainderIsActuallyRecoverable is the end-to-end claim P64.1 is
// named for, and it is asserted as a round trip rather than as "a file was
// written": the locator in the notice must name a path that read_file opens and
// whose content holds the bytes the inline result dropped.
//
// A test that only checked the file exists would pass with a locator naming an
// absolute path, a path outside the workspace, or a stale one — every one of
// which is the failure this item exists to remove.
func TestSpilledRemainderIsActuallyRecoverable(t *testing.T) {
	root := t.TempDir()

	// A body whose *tail* is distinctive, spilled from a head-keeping tool, so
	// the recovered bytes are provably the ones the inline result lost.
	var b strings.Builder
	for i := 0; i < 4000; i++ {
		b.WriteString("line " + strconv.Itoa(i) + " padding padding padding\n")
	}
	body := b.String()
	const marker = "line 3999 padding"

	out := SpillHead(context.Background(), root, "git", body, 4096, "")
	if strings.Contains(out, marker) {
		t.Fatal("fixture is vacuous: the inline result still contains the tail marker")
	}

	rel := spillPathFromNotice(t, out)
	rd := &readTool{root: root}

	// A *default* read of the spill returns only its head — the spill file is
	// large, which is why it exists — so the one thing that must not happen is
	// the model reading it, seeing the same head it already had, and concluding
	// the remainder is gone. The default read has to say it is partial.
	res := execTool(t, rd.Execute, map[string]any{"path": rel})
	if res.IsError {
		t.Fatalf("the locator names a path read_file cannot open (%s):\n%s", rel, res.Content)
	}
	if strings.Contains(res.Content, marker) {
		t.Fatal("fixture is vacuous: the spill fits in one default read window")
	}
	if !strings.Contains(res.Content, "offset") {
		t.Errorf("a default read of the spill does not tell the model how to page for the rest:\n%s",
			lastLines(res.Content, 3))
	}

	// And the path the locator actually instructs — offset/limit — reaches the
	// bytes the inline result dropped. This is the whole claim of the item: the
	// remainder is recoverable, not merely stored.
	res = execTool(t, rd.Execute, map[string]any{"path": rel, "offset": 3990, "limit": 20})
	if res.IsError {
		t.Fatalf("paging the spill failed:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, marker) {
		t.Errorf("paging to the end of the spill did not reach the dropped tail; the remainder is still lost:\n%s",
			lastLines(res.Content, 3))
	}
}

// lastLines trims a result down for a failure message.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// TestItemSpillCarriesMatchesPastTheInlineCap is the item-level half — the one
// P64.1 says a careless port would miss, because by the time a byte policy sees
// a grep result the tail matches have already been discarded by the collector.
// So the assertion is specifically that a match *beyond* grepMaxMatches is
// recoverable, which no byte-level spill could deliver.
func TestItemSpillCarriesMatchesPastTheInlineCap(t *testing.T) {
	root := t.TempDir()
	items := make([]string, grepMaxMatches+200)
	for i := range items {
		items[i] = "file" + strconv.Itoa(i) + ".go:1:needle"
	}
	beyondCap := items[grepMaxMatches+150]

	out := spillItems(context.Background(), root, "grep", items, grepMaxMatches, true)
	if strings.Contains(out, beyondCap) {
		t.Fatal("fixture is vacuous: a past-the-cap match appeared inline")
	}

	rel := spillPathFromNotice(t, out)
	res := execTool(t, (&readTool{root: root}).Execute, map[string]any{"path": rel})
	if res.IsError {
		t.Fatalf("item spill locator unreadable (%s):\n%s", rel, res.Content)
	}
	if !strings.Contains(res.Content, beyondCap) {
		t.Error("a match past the inline cap was not recoverable — the item-level spill is not doing its job")
	}
}

// spillPathFromNotice pulls the workspace-relative spill path back out of a
// truncation notice, which is exactly what a model has to do with it.
func spillPathFromNotice(t *testing.T, notice string) string {
	t.Helper()
	i := strings.Index(notice, spillDirRel)
	if i < 0 {
		t.Fatalf("no spill locator in the notice:\n%s", notice)
	}
	rest := notice[i:]
	end := strings.IndexAny(rest, " \n]")
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSuffix(rest[:end], ".")
}
