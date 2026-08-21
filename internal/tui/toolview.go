package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// toolMaxLinesCompact is the default line cap for tool results in compact mode.
const toolMaxLinesCompact = 10

// Rendering budgets for tool activity in the transcript. These keep large
// outputs from flooding the scrollback while still showing meaningfully more
// than a single truncated line.
const (
	maxDiffLines         = 24 // diff lines shown for an edit/write before collapsing
	maxToolResultLines   = 16 // body lines shown for generic multi-line tool output
	maxSearchResultLines = 32 // web_search returns ~3 lines per result × 10 results
)

// renderToolCall renders the header (and, for file-mutating tools, an inline
// diff) shown when a tool is about to run. File edits are diffed from the tool
// input — old_string/new_string for edits, content for writes — so the user
// sees exactly what is changing without waiting for the result.
//
// P66.15: the name and the arguments are model-controlled, and every branch
// below styles them with lipgloss/chroma *after* this point — nothing
// downstream strips an escape sequence out of a diff body (renderLinesBlock
// doesn't, and diffLines feeds it directly). P66.6 made exactly this fix for
// the approval dialog, which is one branch of the same event stream; the
// transcript is the other, and it renders the same bytes. Sanitizing here
// rather than in each preview renderer is the same choice for the same
// reason: seven branches plus a generic fallback, and renderShellCall's
// per-renderer strip (P28.1) is the evidence that the per-branch form leaves
// the next branch uncovered.
//
// This is also the choke point for replayed history (tui.go's loadHistory
// calls it with a stored tool_use block), so a poisoned transcript reloaded
// from the session store is covered by the same line.
func renderToolCall(th theme, name string, input json.RawMessage, width int) string {
	name = stripControlSeqs(name)
	input = json.RawMessage(sanitizeToolInputJSON(string(input)))
	switch name {
	case "edit_file":
		if s, ok := renderEditDiff(th, name, input, width); ok {
			return s
		}
	case "multi_edit":
		if s, ok := renderMultiEditDiff(th, name, input, width); ok {
			return s
		}
	case "write_file":
		if s, ok := renderWriteDiff(th, name, input, width); ok {
			return s
		}
	case "shell":
		if s, ok := renderShellCall(th, name, input, width); ok {
			return s
		}
	case "read_file":
		if s, ok := renderReadFileCall(th, name, input, width); ok {
			return s
		}
	case "glob":
		if s, ok := renderGlobCall(th, name, input, width); ok {
			return s
		}
	case "grep":
		if s, ok := renderGrepCall(th, name, input, width); ok {
			return s
		}
	}
	// Generic: tool name plus a compact one-line view of the input.
	budget := max(width-len(name)-4, 20)
	return th.tool.Render(fmt.Sprintf("● %s  %s", name, truncate(oneLine(string(input)), budget)))
}

// renderToolCardPending renders the "pending" state of a combined tool-call
// card (P21.2): the already-rendered call block (whatever renderToolCall /
// renderEditDiff / renderShellCall / etc. produced — unchanged, so the
// specialized diff-preview renderers keep working verbatim) plus a dim,
// shimmering "running…" line underneath. step drives the shimmer exactly
// like the existing "● thinking…" status-line animation (see shimmerText
// and tui.go's animStep) — reused rather than inventing a second animation
// primitive. Call is cheap to re-concatenate every animation tick since it
// is computed once and cached by the caller (tui.go's toolCard.call); only
// this trailing line actually changes per tick.
func renderToolCardPending(th theme, call string, step int) string {
	return call + "\n  " + shimmerText("⧗ running…", step, colTextMuted, colAccent)
}

// renderToolCardStart renders the provisional state of a card appended the
// moment the model names a tool it is calling, while the call's arguments are
// still streaming (P33.3). call here is just the name header — the real
// renderToolCall block replaces it in place once the arguments finish — and
// the shimmering status line is renderToolCardPending's, one word apart, so
// the card the user is already watching only gains detail rather than being
// swapped for a different-looking one.
func renderToolCardStart(th theme, call string, step int) string {
	return call + "\n  " + shimmerText("⧗ preparing…", step, colTextMuted, colAccent)
}

// renderToolCardStartCall renders the name-only call block of a card in the
// provisional state above, matching renderToolCall's generic header so the
// two line up when one replaces the other.
func renderToolCardStartCall(th theme, name string) string {
	// The name arrives from the model, and this card goes up before any
	// arguments exist to route through renderToolCall (P66.15).
	return th.tool.Render("● " + stripControlSeqs(name))
}

// renderToolCardDone renders the finished ("ok"/"err") state of a combined
// tool-call card: the same call block (see renderToolCardPending) plus the
// normal finished-result rendering underneath, so what used to be two
// independent transcript items (one appended at KindToolCall, one at
// KindToolResult) becomes one item's two possible renderings.
func renderToolCardDone(th theme, call, name, result string, isErr bool, width, maxBodyLines int, path string) string {
	return call + "\n" + renderToolResult(th, name, result, isErr, width, maxBodyLines, path)
}

// renderToolCardStuck renders the terminal state applied to a tool card that
// is still pending when its run ends without ever producing a matching
// KindToolResult (turn abort, budget/loop error, or a client-initiated
// cancel — see tui.go's resolveStuckToolCards, P21.2).
func renderToolCardStuck(th theme, call string) string {
	return call + "\n" + th.toolErr.Render("  ⚠ interrupted — run ended before a result arrived")
}

// renderToolResult renders a finished tool call as a continuation of the call
// block already printed by renderToolCall (P74.3) — it never repeats the tool
// name, which the call header already carries, and hangs off a "⎿" gutter
// instead. Short, single-line results are shown inline; multi-line output
// that already fits within maxBodyLines is shown as a gutter-marked block
// (the specialized read_file/chroma path included); multi-line output that
// would need truncating collapses to a single one-line summary instead of a
// chopped body, with "/tools full" — which raises maxBodyLines past the
// result's line count — as the expand path. maxBodyLines controls that cap;
// pass a very large number (e.g. 9999) to disable truncation (full mode).
// path is the file path associated with the call that produced this result
// (read_file only, for chroma syntax highlighting — P16.2); empty when
// unknown or not applicable.
func renderToolResult(th theme, name, result string, isErr bool, width, maxBodyLines int, path string) string {
	tag, style := "✓", th.tool
	if isErr {
		tag, style = "×", th.toolErr
	}
	// Untrusted raw tool output (P28.1) — strip anything beyond SGR colour
	// before it reaches the real terminal, covering every branch below
	// (single-line, read_file, and the generic renderBlock path). The name is
	// model-controlled too (P66.15); unlike the body it carries no legitimate
	// styling, so it gets the stricter strip even though it's no longer
	// echoed in the header.
	name = stripControlSeqs(name)
	result = stripDangerousSeqs(strings.TrimRight(result, "\n"))
	header := fmt.Sprintf("%s %s", tag, th.toolGut.Render("⎿"))

	if !strings.Contains(result, "\n") {
		budget := max(width-6, 20)
		return style.Render(fmt.Sprintf("%s %s", tag, th.toolGut.Render("⎿"))) + " " + style.Render(truncate(oneLine(result), budget))
	}

	maxLines := maxBodyLines
	if maxLines <= 0 {
		maxLines = maxToolResultLines
	}
	if name == "web_search" && maxLines < maxSearchResultLines {
		maxLines = maxSearchResultLines
	}

	lineCount := strings.Count(result, "\n") + 1
	if lineCount > maxLines {
		summary := fmt.Sprintf("%d lines  (/tools full to expand)", lineCount)
		return style.Render(header) + " " + style.Render(summary)
	}

	if !isErr && name == "read_file" && path != "" {
		if body, ok := renderReadFileResult(th, path, result, maxLines, width); ok {
			return strings.TrimRight(style.Render(header)+"\n"+body, "\n")
		}
	}

	var b strings.Builder
	b.WriteString(style.Render(header) + "\n")
	b.WriteString(renderBlock(th, result, maxLines, width))
	return strings.TrimRight(b.String(), "\n")
}

// renderBlock renders text as an indented, gutter-marked block capped at max
// lines, with a "… N more lines" footer when truncated.
func renderBlock(th theme, text string, maxLines, width int) string {
	// Strip anything beyond SGR colour before it reaches the terminal (P28.1;
	// idempotent, so this is a no-op for text renderToolResult already
	// sanitized — this also covers the renderShellCall call site, which
	// renders the model's own tool-input command and doesn't route through
	// renderToolResult).
	text = stripDangerousSeqs(text)
	// Raw terminal output (shell tools) may carry ANSI-16 colour codes. Remap
	// them onto the on-brand palette and preserve them: in that case we skip the
	// uniform toolBody foreground so the command's own colours show through.
	colored := strings.IndexByte(text, 0x1b) >= 0
	if colored {
		text = remapANSI16(text, ansiPalette)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return renderLinesBlock(th, lines, maxLines, width, !colored)
}

// renderLinesBlock renders pre-split lines as an indented, gutter-marked
// block capped at maxLines, with a "… N more lines" footer when truncated.
// styleBody applies the uniform toolBody foreground; pass false when lines
// already carry their own styling (ANSI passthrough or chroma highlighting,
// P16.2).
func renderLinesBlock(th theme, lines []string, maxLines, width int, styleBody bool) string {
	hidden := 0
	if len(lines) > maxLines {
		hidden = len(lines) - maxLines
		lines = lines[:maxLines]
	}
	gutter := th.toolGut.Render("│ ")
	var b strings.Builder
	for _, ln := range lines {
		body := truncate(ln, max(width-6, 16))
		if styleBody {
			body = th.toolBody.Render(body)
		}
		b.WriteString("  " + gutter + body + "\n")
	}
	if hidden > 0 {
		b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("▶ %d more lines  (/tools full to expand)", hidden)) + "\n")
	}
	return b.String()
}

// renderReadFileResult renders a read_file tool result — content lines
// carrying the tool's own "N\t<code>" line-number prefix — as a chroma
// syntax-highlighted, gutter-numbered block (P16.2). ok is false when the
// result doesn't match that format or no lexer matches path, so the caller
// falls back to the generic renderBlock path unchanged.
func renderReadFileResult(th theme, path, result string, maxLines, width int) (string, bool) {
	raw := strings.Split(strings.TrimRight(result, "\n"), "\n")
	nums := make([]string, len(raw))
	codes := make([]string, len(raw))
	for i, ln := range raw {
		n, rest, ok := strings.Cut(ln, "\t")
		if !ok {
			return "", false
		}
		nums[i] = n
		codes[i] = rest
	}

	var hi []string
	if lexers.Match(path) != nil {
		hi, _ = highlightSource(th, path, strings.Join(codes, "\n"), nil)
	}

	numW := 0
	for _, n := range nums {
		if len(n) > numW {
			numW = len(n)
		}
	}
	hidden := 0
	if len(raw) > maxLines {
		hidden = len(raw) - maxLines
		nums = nums[:maxLines]
		codes = codes[:maxLines]
	}

	bodyW := max(width-numW-9, 12)
	var b strings.Builder
	for i, code := range codes {
		body := th.toolBody.Render(code)
		if i < len(hi) {
			body = hi[i]
		}
		b.WriteString("  " + th.toolGut.Render(fmt.Sprintf("%*s │ ", numW, nums[i])) + truncate(body, bodyW) + "\n")
	}
	if hidden > 0 {
		b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("▶ %d more lines  (/tools full to expand)", hidden)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// isGroupableTool reports whether name is one of the read-capability tools
// the P74.4 grouping folds into a single card: read_file, grep, glob. Any
// other tool — and any of these three that errors — breaks a group rather
// than joining one (see model.foldIntoReadGroup in stream.go).
func isGroupableTool(name string) bool {
	switch name {
	case "read_file", "grep", "glob":
		return true
	}
	return false
}

// groupEntry is one call folded into a P74.4 read/search group: enough to
// both tally the collapsed summary line and, in expanded ("/tools full")
// mode, list each call's target underneath it.
type groupEntry struct {
	tool  string
	label string
}

// groupEntryLabel returns a short target descriptor for one call folded into
// a group's expanded view — the same identifying detail its own
// renderReadFileCall/renderGrepCall/renderGlobCall header would show (path,
// or pattern plus location), without the bullet/name chrome those carry for
// a standalone card.
func groupEntryLabel(name string, input json.RawMessage) string {
	switch name {
	case "read_file":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(input, &a) == nil && a.Path != "" {
			return a.Path
		}
	case "glob":
		var a struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(input, &a) == nil && a.Pattern != "" {
			if a.Path != "" {
				return a.Pattern + "  in " + a.Path
			}
			return a.Pattern
		}
	case "grep":
		var a struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(input, &a) == nil && a.Pattern != "" {
			if a.Path != "" {
				return "/" + a.Pattern + "/  in " + a.Path
			}
			return "/" + a.Pattern + "/"
		}
	}
	return name
}

// renderToolGroup renders a P74.4 group card: a single collapsed summary
// line ("● searched 5 patterns, read 6 files") by default, or one line per
// call underneath it in full ("/tools full") mode — the same expand
// convention renderToolResult already uses for an over-cap single result.
// entries is never empty (a group starts life with two members — see
// model.foldIntoReadGroup).
func renderToolGroup(th theme, entries []groupEntry, full bool) string {
	reads, searches := 0, 0
	for _, e := range entries {
		if e.tool == "read_file" {
			reads++
		} else {
			searches++
		}
	}
	var parts []string
	if searches > 0 {
		parts = append(parts, fmt.Sprintf("searched %d pattern%s", searches, plural(searches)))
	}
	if reads > 0 {
		parts = append(parts, fmt.Sprintf("read %d file%s", reads, plural(reads)))
	}
	summary := strings.Join(parts, ", ")
	head := th.tool.Render("● " + strings.ToUpper(summary[:1]) + summary[1:])
	if !full {
		return head + "  " + th.diffMeta.Render("(/tools full to expand)")
	}
	var b strings.Builder
	b.WriteString(head + "\n")
	for _, e := range entries {
		b.WriteString("  " + th.toolGut.Render("⎿") + " " + th.diffMeta.Render(e.label) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// plural returns "s" unless n is exactly 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// diffLines computes a proper unified diff between oldS and newS and returns a
// list of pre-styled, ready-to-print lines (line-number gutter + context/
// removed/added rows, hunk headers with real ranges) capped at budget. Hidden
// reports how many lines were omitted. The LCS core is unchanged from the
// original implementation; P16.3 adds the gutter, real hunk ranges, and
// word-level intraline emphasis for single-line replacements, and P16.2
// layers chroma syntax coloring under the +/- background tint wherever path's
// extension matches a lexer.
func diffLines(th theme, path, oldS, newS string, width, budget int) (lines []string, hidden int) {
	oldL := splitDiffLines(oldS)
	newL := splitDiffLines(newS)
	edits := buildEdits(oldL, newL)

	// Mark positions within ±diffCtxLines of any change.
	const diffCtxLines = 3
	show := make([]bool, len(edits))
	anyChange := false
	for i, e := range edits {
		if e.op != opEqual {
			anyChange = true
			for j := max(i-diffCtxLines, 0); j < min(i+diffCtxLines+1, len(edits)); j++ {
				show[j] = true
			}
		}
	}
	if !anyChange {
		return nil, 0
	}

	// 1-based old/new line numbers per edit, for the gutter and hunk ranges.
	// onBefore/nnBefore snapshot the running counters *before* each edit, so a
	// hunk that opens with a pure addition (no old-numbered line of its own)
	// can still fall back to "old line right before this hunk, plus one".
	oldNos := make([]int, len(edits))
	newNos := make([]int, len(edits))
	onBefore := make([]int, len(edits))
	nnBefore := make([]int, len(edits))
	on, nn := 0, 0
	for i, e := range edits {
		onBefore[i], nnBefore[i] = on, nn
		switch e.op {
		case opEqual:
			on++
			nn++
			oldNos[i], newNos[i] = on, nn
		case opDel:
			on++
			oldNos[i] = on
		case opAdd:
			nn++
			newNos[i] = nn
		}
	}
	numW := len(strconv.Itoa(max(on, max(nn, 1))))

	// Detect singleton del→add "replace" pairs (not part of a longer run) for
	// word-level intraline emphasis instead of whole-line chroma coloring.
	isPair := make([]bool, len(edits))
	for i := 0; i+1 < len(edits); i++ {
		if edits[i].op == opDel && edits[i+1].op == opAdd &&
			(i == 0 || edits[i-1].op != opDel) &&
			(i+2 >= len(edits) || edits[i+2].op != opAdd) {
			isPair[i], isPair[i+1] = true, true
		}
	}

	var newBg, oldBg []color.Color
	if len(newL) > 0 {
		newBg = make([]color.Color, len(newL))
	}
	if len(oldL) > 0 {
		oldBg = make([]color.Color, len(oldL))
	}
	for i, e := range edits {
		switch e.op {
		case opAdd:
			if !isPair[i] {
				newBg[newNos[i]-1] = colDiffAddBg
			}
		case opDel:
			if !isPair[i] {
				oldBg[oldNos[i]-1] = colDiffDelBg
			}
		}
	}

	var newHi, oldHi []string
	if lexers.Match(path) != nil {
		newHi, _ = highlightSource(th, path, newS, func(l int) color.Color {
			if l >= 0 && l < len(newBg) {
				return newBg[l]
			}
			return nil
		})
		oldHi, _ = highlightSource(th, path, oldS, func(l int) color.Color {
			if l >= 0 && l < len(oldBg) {
				return oldBg[l]
			}
			return nil
		})
	}

	gutter := func(oldNo, newNo int) string {
		o, n := "", ""
		if oldNo > 0 {
			o = strconv.Itoa(oldNo)
		}
		if newNo > 0 {
			n = strconv.Itoa(newNo)
		}
		return th.diffGutter.Render(fmt.Sprintf("%*s %*s", numW, o, numW, n))
	}
	// plainOr picks the chroma-highlighted line at idx from hi if available,
	// else renders raw text with the given fallback (tint) style.
	plainOr := func(hi []string, idx int, raw string, fallback lipgloss.Style) string {
		if idx >= 0 && idx < len(hi) {
			return hi[idx]
		}
		return fallback.Render(raw)
	}
	contentW := max(width-2*numW-6, 8)

	// Precompute contiguous shown ranges (hunks) so each header — which needs
	// the hunk's full old/new line-count span — can be emitted *before* its
	// content instead of after (the original single-pass version got this
	// backwards: it only knew a hunk's extent once the loop reached its end).
	type hunkRange struct{ start, end int }
	var hunks []hunkRange
	for i := 0; i < len(edits); {
		if !show[i] {
			i++
			continue
		}
		start := i
		for i < len(edits) && show[i] {
			i++
		}
		hunks = append(hunks, hunkRange{start, i})
	}
	hunkHeader := make(map[int]string, len(hunks))
	for _, h := range hunks {
		oStart, nStart, oCount, nCount := 0, 0, 0, 0
		for i := h.start; i < h.end; i++ {
			if edits[i].op != opAdd {
				if oStart == 0 {
					oStart = oldNos[i]
				}
				oCount++
			}
			if edits[i].op != opDel {
				if nStart == 0 {
					nStart = newNos[i]
				}
				nCount++
			}
		}
		if oStart == 0 {
			oStart = onBefore[h.start] + 1
		}
		if nStart == 0 {
			nStart = nnBefore[h.start] + 1
		}
		hunkHeader[h.start] = fmt.Sprintf("@@ -%d,%d +%d,%d @@", oStart, oCount, nStart, nCount)
	}

	for i := 0; i < len(edits); i++ {
		if !show[i] {
			continue
		}
		if hdr, ok := hunkHeader[i]; ok {
			lines = append(lines, th.diffMeta.Render(hdr))
		}

		e := edits[i]
		if isPair[i] && e.op == opDel {
			addI := i + 1
			oldSeg, newSeg := intralineDiff(th, e.text, edits[addI].text)
			lines = append(lines, gutter(oldNos[i], 0)+" "+th.diffMarkDel.Render("- ")+truncate(oldSeg, contentW))
			lines = append(lines, gutter(0, newNos[addI])+" "+th.diffMarkAdd.Render("+ ")+truncate(newSeg, contentW))
			i++ // consumed the paired opAdd too
			continue
		}

		switch e.op {
		case opDel:
			body := plainOr(oldHi, oldNos[i]-1, e.text, th.diffDelBg)
			lines = append(lines, gutter(oldNos[i], 0)+" "+th.diffMarkDel.Render("- ")+truncate(body, contentW))
		case opAdd:
			body := plainOr(newHi, newNos[i]-1, e.text, th.diffAddBg)
			lines = append(lines, gutter(0, newNos[i])+" "+th.diffMarkAdd.Render("+ ")+truncate(body, contentW))
		default:
			body := plainOr(newHi, newNos[i]-1, e.text, th.diffMeta)
			lines = append(lines, gutter(oldNos[i], newNos[i])+"   "+truncate(body, contentW))
		}
	}

	if len(lines) > budget {
		hidden = len(lines) - budget
		lines = lines[:budget]
	}
	return lines, hidden
}

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

// assembleDiff builds a tool header followed by an indented diff body.
func assembleDiff(th theme, name, path string, lines []string, hidden int) string {
	var b strings.Builder
	b.WriteString(th.tool.Render("● "+name+" ") + th.diffMeta.Render(path) + "\n")
	for _, ln := range lines {
		b.WriteString("  " + ln + "\n")
	}
	if hidden > 0 {
		b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("▶ %d more diff lines", hidden)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderEditDiff(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if json.Unmarshal(input, &a) != nil || a.Path == "" {
		return "", false
	}
	lines, hidden := diffLines(th, a.Path, a.OldString, a.NewString, width, maxDiffLines)
	return assembleDiff(th, name, a.Path, lines, hidden), true
}

func renderMultiEditDiff(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Edits []struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if json.Unmarshal(input, &a) != nil || len(a.Edits) == 0 {
		return "", false
	}
	var b strings.Builder
	budget := maxDiffLines
	for i, e := range a.Edits {
		if budget <= 0 {
			b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("… %d more edit(s)", len(a.Edits)-i)) + "\n")
			break
		}
		lines, hidden := diffLines(th, e.Path, e.OldString, e.NewString, width, budget)
		budget -= len(lines)
		if i == 0 {
			b.WriteString(th.tool.Render("● "+name+" ") + th.diffMeta.Render(e.Path) + "\n")
		} else {
			b.WriteString("  " + th.diffMeta.Render(e.Path) + "\n")
		}
		for _, ln := range lines {
			b.WriteString("  " + ln + "\n")
		}
		if hidden > 0 {
			b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("▶ %d more diff lines", hidden)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n"), true
}

func renderWriteDiff(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if json.Unmarshal(input, &a) != nil || a.Path == "" {
		return "", false
	}
	lines, hidden := diffLines(th, a.Path, "", a.Content, width, maxDiffLines)
	return assembleDiff(th, name, a.Path, lines, hidden), true
}

func renderShellCall(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if json.Unmarshal(input, &a) != nil || a.Command == "" {
		return "", false
	}
	header := th.tool.Render("● " + name)
	if a.Background {
		header += " " + th.diffMeta.Render("(background)")
	}
	// P28.1: sanitize before highlighting — chroma tokenizes the raw command
	// text verbatim, so a dangerous sequence embedded in it would otherwise
	// reach renderLinesBlock (which doesn't itself sanitize) unfiltered.
	a.Command = stripDangerousSeqs(a.Command)
	var b strings.Builder
	b.WriteString(header + "\n")
	// P16.2: highlight the command as shell syntax ("fenced code in tool
	// output") — a fake ".sh" filename is enough to steer chroma's
	// extension-based lexer match, since the command itself carries no path.
	if hi, ok := highlightSource(th, "cmd.sh", a.Command, nil); ok {
		lines := strings.Split(strings.TrimRight(a.Command, "\n"), "\n")
		if len(hi) == len(lines) {
			b.WriteString(renderLinesBlock(th, hi, maxToolResultLines, width, false))
		} else {
			b.WriteString(renderBlock(th, a.Command, maxToolResultLines, width))
		}
	} else {
		b.WriteString(renderBlock(th, a.Command, maxToolResultLines, width))
	}
	return strings.TrimRight(b.String(), "\n"), true
}

func renderReadFileCall(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if json.Unmarshal(input, &a) != nil || a.Path == "" {
		return "", false
	}
	label := th.tool.Render("● "+name+" ") + th.diffMeta.Render(truncate(a.Path, max(width-len(name)-12, 20)))
	if a.Offset > 0 || a.Limit > 0 {
		var rang string
		if a.Limit > 0 {
			rang = fmt.Sprintf("  lines %d–%d", a.Offset+1, a.Offset+a.Limit)
		} else {
			rang = fmt.Sprintf("  from line %d", a.Offset+1)
		}
		label += th.diffMeta.Render(rang)
	}
	return label, true
}

func renderGlobCall(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(input, &a) != nil || a.Pattern == "" {
		return "", false
	}
	loc := a.Path
	if loc == "" {
		loc = "."
	}
	label := th.tool.Render("● "+name+" ") + th.diffMeta.Render(truncate(a.Pattern, max(width-20, 12)))
	label += "  " + th.diffMeta.Render("in "+truncate(loc, max(width-len(a.Pattern)-20, 8)))
	return label, true
}

func renderGrepCall(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if json.Unmarshal(input, &a) != nil || a.Pattern == "" {
		return "", false
	}
	loc := a.Path
	if loc == "" {
		loc = "."
	}
	filter := ""
	if a.Glob != "" {
		filter = " [" + a.Glob + "]"
	}
	label := th.tool.Render("● "+name+" ") + th.diffMeta.Render("/"+truncate(a.Pattern, max(width-24, 12))+"/")
	label += "  " + th.diffMeta.Render("in "+truncate(loc, max(width-len(a.Pattern)-24, 8))+filter)
	return label, true
}

// renderApprovalPreview returns a compact one-line preview string for the
// approval banner, derived from the raw tool-input JSON. The preview is
// tool-type-specific: shell shows the command, file tools show the path, network
// tools show the URL. Falls back to a trimmed JSON excerpt for unknown tools.
func renderApprovalPreview(th theme, toolName, inputJSON string, width int) string {
	maxW := max(width-4, 20)
	prefix := "  "
	var raw json.RawMessage
	if json.Unmarshal([]byte(inputJSON), &raw) != nil || inputJSON == "" {
		return prefix + th.diffMeta.Render("(no preview)")
	}
	switch toolName {
	case "shell":
		var a struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(raw, &a) == nil && a.Command != "" {
			cmd := strings.Join(strings.Fields(a.Command), " ")
			return prefix + th.diffMeta.Render("❯ "+truncate(cmd, maxW-2))
		}
	case "write_file":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if json.Unmarshal(raw, &a) == nil && a.Path != "" {
			note := a.Path
			if a.Content != "" {
				note = fmt.Sprintf("%s (%d lines)", a.Path, strings.Count(a.Content, "\n")+1)
			}
			return prefix + th.diffMeta.Render("✎ "+truncate(note, maxW-2))
		}
	case "edit_file", "multi_edit":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &a) == nil && a.Path != "" {
			return prefix + th.diffMeta.Render("✎ "+truncate(a.Path, maxW-2))
		}
	case "read_file":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &a) == nil && a.Path != "" {
			return prefix + th.diffMeta.Render("▸ "+truncate(a.Path, maxW-2))
		}
	case "web_fetch":
		var a struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(raw, &a) == nil && a.URL != "" {
			return prefix + th.diffMeta.Render("⬡ "+truncate(a.URL, maxW-2))
		}
	case "web_search":
		var a struct {
			Query string `json:"query"`
		}
		if json.Unmarshal(raw, &a) == nil && a.Query != "" {
			return prefix + th.diffMeta.Render("⬡ "+truncate(a.Query, maxW-2))
		}
	}
	// Generic: compact single-line JSON excerpt.
	compact := strings.Join(strings.Fields(inputJSON), " ")
	return prefix + th.diffMeta.Render(truncate(compact, maxW))
}
