package filetracker

// Hunk-level agent-vs-external change attribution (P45.2). Where the mtime map
// (tracker.go) answers "was this file touched externally since the agent last
// saw it", the hunk store answers the finer question "which *lines* currently
// in this file did the agent author". Each successful agent write records the
// changed line ranges as agent-attributed hunks; a later external edit
// invalidates only the hunks whose lines it disturbed, leaving the agent's
// other contributions attributed.

import (
	"os"
	"sort"
	"strings"
	"time"
)

// A Hunk is a contiguous run of agent-authored lines within a file, addressed
// by 1-based inclusive line numbers (Start <= End). Ranges are expressed in the
// coordinate space of the file's current on-disk content.
type Hunk struct {
	Start int
	End   int
}

// Diffing bounds. LCS is O(n*m) in time and space, so above these limits the
// tracker degrades to whole-file attribution rather than allocate a huge table.
const (
	maxDiffLines = 50000
	maxDiffCells = 4_000_000
)

// fileHunks is the agent-attribution state for one file: a snapshot of the
// content the agent last left on disk, and the agent-authored ranges within it.
// Reconciliation diffs a fresh read against lines to decide which hunks survive.
type fileHunks struct {
	lines   []string
	hunks   []Hunk
	updated time.Time // for prune ordering, independent of the mtime map
}

// RecordAgentWrite records that the agent produced newContent from oldContent
// for the file at path (the full file text immediately before and after the
// write; an empty oldContent means the file did not previously exist). It
// computes the changed line ranges as freshly agent-authored hunks, remaps any
// previously recorded agent hunks through the same edit, merges the two, and
// stores the result against a snapshot of newContent.
//
// It never touches the mtime map, so CheckWrite's read-before-write guard is
// unaffected.
func (t *Tracker) RecordAgentWrite(path, oldContent, newContent string) {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	t.mu.Lock()
	defer t.mu.Unlock()

	var priorInOld []Hunk
	if prior := t.hunks[path]; prior != nil {
		// Remap prior hunks from their own snapshot space into oldContent's
		// space first. This is the identity map on the normal path (nothing
		// external happened, so the snapshot equals oldContent), but stays
		// correct if AgentHunks was never called to reconcile an interim edit.
		priorInOld = reconcileHunks(prior.lines, oldLines, prior.hunks)
	}

	changed, survive := diffLines(oldLines, newLines)
	merged := mergeHunks(append(remapHunks(priorInOld, survive), changed...))

	if t.hunks == nil {
		t.hunks = make(map[string]*fileHunks)
	}
	t.hunks[path] = &fileHunks{lines: newLines, hunks: merged, updated: time.Now()}
	if len(t.hunks) > maxTrackedFiles {
		t.pruneOldestHunkLocked()
	}
}

// AgentHunks returns the agent-attributed line ranges currently valid for path,
// reconciled against the file's present on-disk content: hunks whose lines an
// external edit changed are dropped, surviving hunks are remapped to their new
// line positions. Returns nil if the file was never written by the agent, or no
// longer exists. The reconciled result is persisted so repeated queries are
// cheap and later external edits reconcile against current content; this does
// not touch the mtime map.
func (t *Tracker) AgentHunks(path string) []Hunk {
	t.mu.Lock()
	st := t.hunks[path]
	t.mu.Unlock()
	if st == nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil // file gone: nothing attributable now
	}
	curLines := splitLines(string(data))
	reconciled := reconcileHunks(st.lines, curLines, st.hunks)

	t.mu.Lock()
	// Only persist if no concurrent RecordAgentWrite replaced the entry.
	if cur := t.hunks[path]; cur == st {
		t.hunks[path] = &fileHunks{lines: curLines, hunks: reconciled, updated: st.updated}
	}
	t.mu.Unlock()
	return reconciled
}

// pruneOldestHunkLocked evicts the least-recently-updated hunk entry. Caller
// holds t.mu.
func (t *Tracker) pruneOldestHunkLocked() {
	var oldestPath string
	var oldestTime time.Time
	for p, fh := range t.hunks {
		if oldestPath == "" || fh.updated.Before(oldestTime) {
			oldestPath = p
			oldestTime = fh.updated
		}
	}
	if oldestPath != "" {
		delete(t.hunks, oldestPath)
	}
}

// splitLines splits file text into lines. An empty string yields no lines (an
// empty file has nothing to attribute); a trailing newline yields a trailing
// empty line, symmetric on both sides of a diff.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// diffLines diffs oldLines into newLines, returning the changed (inserted or
// modified) line ranges in newLines as agent-authored hunks, plus a map from
// surviving oldLines indices to their newLines indices (0-based). When the
// inputs share no common subsequence or are too large to diff, the whole new
// file is reported as one changed hunk and nothing is reported as surviving.
func diffLines(oldLines, newLines []string) (changed []Hunk, survive map[int]int) {
	matches := lcs(oldLines, newLines)
	if matches == nil {
		if len(newLines) == 0 {
			return nil, map[int]int{}
		}
		return []Hunk{{Start: 1, End: len(newLines)}}, map[int]int{}
	}
	return changedRanges(matches, len(newLines)), surviveMap(matches)
}

// reconcileHunks maps hunks recorded against snapLines into curLines' coordinate
// space, dropping any hunk whose lines curLines changed. Used both to validate
// stored hunks against a fresh disk read and to carry prior hunks across an
// agent write.
func reconcileHunks(snapLines, curLines []string, hunks []Hunk) []Hunk {
	if len(hunks) == 0 {
		return nil
	}
	if linesEqual(snapLines, curLines) {
		return append([]Hunk(nil), hunks...)
	}
	matches := lcs(snapLines, curLines)
	if matches == nil {
		return nil // too large, or no lines in common: can't safely attribute
	}
	return mergeHunks(remapHunks(hunks, surviveMap(matches)))
}

// changedRanges returns the 1-based inclusive ranges of b's lines that are not
// part of the common subsequence described by matches (i.e. inserted or
// changed relative to the other side).
func changedRanges(matches [][2]int, bLen int) []Hunk {
	matched := make([]bool, bLen)
	for _, m := range matches {
		matched[m[1]] = true
	}
	var out []Hunk
	start := -1
	for j := range bLen {
		if !matched[j] {
			if start < 0 {
				start = j
			}
			continue
		}
		if start >= 0 {
			out = append(out, Hunk{Start: start + 1, End: j})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, Hunk{Start: start + 1, End: bLen})
	}
	return out
}

func surviveMap(matches [][2]int) map[int]int {
	m := make(map[int]int, len(matches))
	for _, mm := range matches {
		m[mm[0]] = mm[1]
	}
	return m
}

// remapHunks translates hunks (1-based, over the "old" side of a diff) into the
// "new" side using survive (old index -> new index). A hunk survives only if
// every one of its lines survived and they stayed contiguous on the new side —
// so an external insertion or change anywhere inside the hunk drops it.
func remapHunks(hunks []Hunk, survive map[int]int) []Hunk {
	var out []Hunk
	for _, h := range hunks {
		s0, e0 := h.Start-1, h.End-1
		ns, ok := survive[s0]
		if !ok {
			continue
		}
		contiguous := true
		for oi := s0; oi <= e0; oi++ {
			ni, has := survive[oi]
			if !has || ni != ns+(oi-s0) {
				contiguous = false
				break
			}
		}
		if contiguous {
			out = append(out, Hunk{Start: ns + 1, End: ns + (e0 - s0) + 1})
		}
	}
	return out
}

// mergeHunks sorts and coalesces overlapping or directly adjacent ranges.
func mergeHunks(hunks []Hunk) []Hunk {
	if len(hunks) <= 1 {
		return hunks
	}
	sort.Slice(hunks, func(i, j int) bool { return hunks[i].Start < hunks[j].Start })
	out := make([]Hunk, 0, len(hunks))
	out = append(out, hunks[0])
	for _, h := range hunks[1:] {
		last := &out[len(out)-1]
		if h.Start <= last.End+1 { // overlapping or adjacent
			if h.End > last.End {
				last.End = h.End
			}
			continue
		}
		out = append(out, h)
	}
	return out
}

// lcs returns the matched index pairs (aIdx, bIdx), ascending, of a maximal
// common subsequence of a and b. Returns nil when either side is empty or the
// inputs are too large to diff within the bounded table.
func lcs(a, b []string) [][2]int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 || n > maxDiffLines || m > maxDiffLines || n*m > maxDiffCells {
		return nil
	}
	// dp[i][j] = LCS length of a[i:] and b[j:].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			out = append(out, [2]int{i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}
