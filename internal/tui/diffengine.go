package tui

import (
	"regexp"
	"strings"
)

// wordSplitRe splits text into alternating whitespace/non-whitespace runs so
// intralineDiff can rejoin them without losing the original spacing.
var wordSplitRe = regexp.MustCompile(`\s+|\S+`)

// intralineDiff computes a word-level diff between a replaced line pair and
// renders each side with the changed span emphasized (bold+underline) and
// the unchanged span in the softer tinted tone — "this word changed" rather
// than a wholesale red/green line swap (P16.3). Chroma coloring is
// intentionally not layered on these two lines: token boundaries and word-diff
// boundaries don't align, so combining them accurately isn't worth the
// complexity for a single-line emphasis feature.
func intralineDiff(th theme, oldText, newText string) (oldRendered, newRendered string) {
	oldW := wordSplitRe.FindAllString(oldText, -1)
	newW := wordSplitRe.FindAllString(newText, -1)
	wedits := buildEdits(oldW, newW)
	var ob, nb strings.Builder
	for _, we := range wedits {
		switch we.op {
		case opEqual:
			ob.WriteString(th.diffDelBg.Render(we.text))
			nb.WriteString(th.diffAddBg.Render(we.text))
		case opDel:
			ob.WriteString(th.diffIntraDel.Render(we.text))
		case opAdd:
			nb.WriteString(th.diffIntraAdd.Render(we.text))
		}
	}
	return ob.String(), nb.String()
}

// splitDiffLines splits s into trimmed lines without a trailing newline.
func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// editOp classifies a single line in an edit sequence.
type editOp byte

const (
	opEqual editOp = iota
	opDel
	opAdd
)

type editLine struct {
	op   editOp
	text string
}

// lcsIndices returns matched (ia, ib) index pairs for the LCS of a and b.
// Uses O(m·n) DP — acceptable for the small strings found in tool inputs.
func lcsIndices(a, b []string) [][2]int {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return nil
	}
	dp := make([][]int32, m+1)
	for i := range dp {
		dp[i] = make([]int32, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	pairs := make([][2]int, 0, int(dp[m][n]))
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			pairs = append([][2]int{{i - 1, j - 1}}, pairs...)
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	return pairs
}

// buildEdits turns an LCS into an ordered equal/del/add edit sequence.
func buildEdits(oldL, newL []string) []editLine {
	pairs := lcsIndices(oldL, newL)
	edits := make([]editLine, 0, len(oldL)+len(newL))
	ia, ib := 0, 0
	for _, p := range pairs {
		for ; ia < p[0]; ia++ {
			edits = append(edits, editLine{opDel, oldL[ia]})
		}
		for ; ib < p[1]; ib++ {
			edits = append(edits, editLine{opAdd, newL[ib]})
		}
		edits = append(edits, editLine{opEqual, oldL[ia]})
		ia++
		ib++
	}
	for ; ia < len(oldL); ia++ {
		edits = append(edits, editLine{opDel, oldL[ia]})
	}
	for ; ib < len(newL); ib++ {
		edits = append(edits, editLine{opAdd, newL[ib]})
	}
	return edits
}
