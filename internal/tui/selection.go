package tui

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// doubleClickWindow is the max gap between clicks at the same cell for them
// to count toward a double/triple click, matching typical desktop terminal
// defaults (crush and most GUI terminals sit in the 300-500ms range).
const doubleClickWindow = 400 * time.Millisecond

// selection tracks a mouse-driven text selection over the transcript pane, in
// pane-relative screen coordinates (row 0 = the pane's first visible line,
// col 0 = its first visible column — see model.paneOrigin/toPaneCoord).
// Screen space rather than item/offset space is deliberate: a selection is a
// snapshot of what's currently on screen, and — like a real terminal's native
// selection — scrolling during a drag isn't specially handled, since the
// coordinates would no longer refer to the same content anyway.
type selection struct {
	active bool // true from mouse-down until release (drag in progress)
	have   bool // true once a selection exists (post-release/double/triple-click)

	anchorRow, anchorCol int
	curRow, curCol       int

	// Click-counting state for double-click-word / triple-click-line.
	clickCount   int
	lastClickAt  time.Time
	lastClickRow int
	lastClickCol int
}

// normalized returns the selection's two endpoints in reading order
// (top-to-bottom, left-to-right), regardless of drag direction.
func (s selection) normalized() (r1, c1, r2, c2 int) {
	r1, c1, r2, c2 = s.anchorRow, s.anchorCol, s.curRow, s.curCol
	if r2 < r1 || (r2 == r1 && c2 < c1) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	return
}

// paneOrigin returns the screen (col,row) of the transcript pane's top-left
// content cell, translating from absolute terminal coordinates (what
// tea.Mouse reports) to pane-relative ones. Kept in sync with render()'s
// layout by construction: PaddingLeft(1) on the main panel, nothing else —
// P74.2 removed the title-bar row (row 0 now) and the sidebar no longer
// shifts this origin, since it composites as an overlay instead of being
// joined into the layout. See sidebarOccludes for the overlay's own bounds.
func (m *model) paneOrigin() (col, row int) {
	return 1, 0
}

// sidebarOccludes reports whether screen column x falls under the P74.2
// sidebar overlay (drawn full-height, 0..sidebarW+1, when open) — the pane
// underneath it is still there geometrically (the sidebar no longer
// reserves layout width), but visually covered, so mouse coordinates in
// that band must not resolve to transcript content.
func (m *model) sidebarOccludes(x int) bool {
	return m.chrome.sidebarOpen && m.chrome.width >= sidebarMinTermW && x < m.chrome.sidebarW+1
}

// toPaneCoord translates absolute mouse coordinates to pane-relative
// (col,row); ok is false when the point falls outside the transcript pane
// (over the sidebar overlay, textarea, terminal pane, or the scrollbar
// column to its right).
func (m *model) toPaneCoord(x, y int) (col, row int, ok bool) {
	if m.sidebarOccludes(x) {
		return 0, 0, false
	}
	ox, oy := m.paneOrigin()
	col, row = x-ox, y-oy
	if col < 0 || row < 0 || col >= m.transcript.Width() || row >= m.transcript.Height() {
		return 0, 0, false
	}
	return col, row, true
}

// clampPaneCoord is toPaneCoord without the bounds rejection — used while
// dragging so a selection extends to the nearest visible row/col instead of
// freezing the instant the pointer crosses the pane edge. A drag that
// crosses under the sidebar overlay clamps to its right edge rather than
// resolving to the (occluded) transcript content beneath it.
func (m *model) clampPaneCoord(x, y int) (col, row int) {
	ox, oy := m.paneOrigin()
	if m.sidebarOccludes(x) {
		x = ox + m.chrome.sidebarW + 1
	}
	col = max(0, min(x-ox, m.transcript.Width()-1))
	row = max(0, min(y-oy, m.transcript.Height()-1))
	return
}

// registerClick updates click-counting state for the cell at (col,row) and
// returns the resulting click count (1, 2, or 3 — a 4th click within the
// window wraps back to 1, so a click train longer than triple just restarts
// the cycle rather than doing nothing).
func (m *model) registerClick(col, row int) int {
	now := time.Now()
	if m.sel.clickCount > 0 && now.Sub(m.sel.lastClickAt) < doubleClickWindow &&
		m.sel.lastClickRow == row && m.sel.lastClickCol == col {
		m.sel.clickCount++
		if m.sel.clickCount > 3 {
			m.sel.clickCount = 1
		}
	} else {
		m.sel.clickCount = 1
	}
	m.sel.lastClickAt = now
	m.sel.lastClickRow, m.sel.lastClickCol = row, col
	return m.sel.clickCount
}

// isWordRune reports whether r participates in a "word" for double-click
// selection purposes.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// wordBounds returns the [start,end) rune-index range of the word at column
// col in plain (already ANSI-stripped) text: a run of isWordRune characters,
// or a single non-space rune treated as its own one-character "word" (so
// punctuation like "->" or a lone "." is selectable), or the point itself
// when it lands on whitespace.
func wordBounds(plain string, col int) (start, end int) {
	r := []rune(plain)
	if col < 0 {
		col = 0
	}
	if col >= len(r) {
		return len(r), len(r)
	}
	if unicode.IsSpace(r[col]) {
		return col, col + 1
	}
	if !isWordRune(r[col]) {
		return col, col + 1
	}
	start, end = col, col+1
	for start > 0 && isWordRune(r[start-1]) {
		start--
	}
	for end < len(r) && isWordRune(r[end]) {
		end++
	}
	return start, end
}

// selectedText extracts the plain-text selection spanning the normalized
// range [r1,c1]-[r2,c2] from lines — the pane's currently visible ANSI-styled
// rows (transcriptPane.VisibleLines). Column coordinates are rune indices
// into each row's ANSI-stripped plain text; treating that as a cell-column
// index into the original (styled) line for ansi.Cut is an approximation
// that assumes single-cell-width runes, matching the simplification the rest
// of this package already makes for wrap/truncate.
func selectedText(lines []string, r1, c1, r2, c2 int) string {
	if r1 < 0 {
		r1, c1 = 0, 0
	}
	if r2 >= len(lines) {
		r2 = len(lines) - 1
	}
	if r1 >= len(lines) || r2 < r1 {
		return ""
	}
	out := make([]string, 0, r2-r1+1)
	for row := r1; row <= r2; row++ {
		line := lines[row]
		plainLen := len([]rune(ansi.Strip(line)))
		left, right := 0, plainLen
		if row == r1 {
			left = c1
		}
		if row == r2 {
			right = c2
		}
		left = max(0, min(left, plainLen))
		right = max(left, min(right, plainLen))
		out = append(out, ansi.Strip(ansi.Cut(line, left, right)))
	}
	return strings.Join(out, "\n")
}

// toolBlockAt returns the toolBlock tracked for a transcript item, or nil if
// it isn't a resolved tool card/group (pending, replayed, or not a tool
// block at all) — the mouse-click half of P75.1's per-block toggle, sharing
// the same model.toolsUI.state.toolBlocks registry the keyboard path
// (toggleLastToolBlock) already addresses by "last resolved block".
func (m *model) toolBlockAt(it *transcriptItem) toolBlock {
	if it == nil {
		return nil
	}
	for _, b := range m.toolsUI.state.toolBlocks {
		if b.blkItem() == it {
			return b
		}
	}
	return nil
}

// toolToggleIconCols is the clickable width of a toggleIcon glyph
// (internal/tui/toolview.go): the glyph cell itself plus its trailing space.
const toolToggleIconCols = 2

// clickedToggleIcon reports whether pane-relative col falls on line's own
// disclosure icon — the narrow click target toggleIcon renders at the very
// start of a toggleable tool-result/group header, rather than the whole row
// being clickable. line is one row of VisibleLines() (still ANSI-styled).
func clickedToggleIcon(line string, col int) bool {
	if col < 0 || col >= toolToggleIconCols {
		return false
	}
	plain := ansi.Strip(line)
	if plain == "" {
		return false
	}
	r := []rune(plain)[0]
	return r == '▸' || r == '▾'
}

// handleMouseClick processes a mouse button press over the transcript pane:
// a single click on a resolved tool result/read-group's own disclosure icon
// (toggleIcon, internal/tui/toolview.go — not anywhere else on the row)
// toggles its expand/collapse state in place (P75.1) and consumes the click
// rather than arming a drag-select; a single click anywhere else focuses the
// message under the cursor and arms a drag; a double-click selects the word
// under the cursor and copies it immediately; a triple-click does the same
// for the whole line. Clicks outside the pane (the sidebar overlay,
// textarea, terminal pane, scrollbar) are ignored.
func (m *model) handleMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	col, row, ok := m.toPaneCoord(msg.X, msg.Y)
	if !ok {
		return nil
	}
	count := m.registerClick(col, row)
	idx, _ := m.transcript.ItemIndexAtY(row)
	if idx >= 0 {
		m.focusedIdx = idx
	}

	if count == 1 {
		lines := m.transcript.VisibleLines()
		if row < len(lines) && clickedToggleIcon(lines[row], col) {
			if blk := m.toolBlockAt(m.transcript.segmentAt(idx)); blk != nil {
				blk.toggleFull(m)
				m.sel.active, m.sel.have = false, false
				m.refresh()
				return nil
			}
		}
	}

	switch count {
	case 2:
		lines := m.transcript.VisibleLines()
		if row >= len(lines) {
			return nil
		}
		startCol, endCol := wordBounds(ansi.Strip(lines[row]), col)
		m.sel.active, m.sel.have = false, true
		m.sel.anchorRow, m.sel.anchorCol = row, startCol
		m.sel.curRow, m.sel.curCol = row, endCol
		m.refresh()
		return copyIfNonEmpty(selectedText(lines, row, startCol, row, endCol))
	case 3:
		lines := m.transcript.VisibleLines()
		if row >= len(lines) {
			return nil
		}
		plainLen := len([]rune(ansi.Strip(lines[row])))
		m.sel.active, m.sel.have = false, true
		m.sel.anchorRow, m.sel.anchorCol = row, 0
		m.sel.curRow, m.sel.curCol = row, plainLen
		m.refresh()
		return copyIfNonEmpty(selectedText(lines, row, 0, row, plainLen))
	default: // 1 (or a wrapped-around 4th+ click)
		m.sel.active, m.sel.have = true, false
		m.sel.anchorRow, m.sel.anchorCol = row, col
		m.sel.curRow, m.sel.curCol = row, col
		m.refresh()
		return nil
	}
}

// handleMouseMotion extends the in-progress drag selection. Motion outside
// the pane clamps to the nearest edge rather than being ignored, so dragging
// past the transcript's bottom/top still extends the selection.
func (m *model) handleMouseMotion(msg tea.MouseMotionMsg) {
	if !m.sel.active {
		return
	}
	col, row, ok := m.toPaneCoord(msg.X, msg.Y)
	if !ok {
		col, row = m.clampPaneCoord(msg.X, msg.Y)
	}
	if col == m.sel.curCol && row == m.sel.curRow {
		return
	}
	m.sel.curRow, m.sel.curCol = row, col
	m.refresh()
}

// handleMouseRelease ends a drag: a release at the same cell as the press
// leaves only the click-to-focus effect (no text to copy); otherwise the
// dragged range is copied to the clipboard.
func (m *model) handleMouseRelease(msg tea.MouseReleaseMsg) tea.Cmd {
	if !m.sel.active {
		return nil
	}
	m.sel.active = false
	if m.sel.anchorRow == m.sel.curRow && m.sel.anchorCol == m.sel.curCol {
		m.sel.have = false
		return nil
	}
	m.sel.have = true
	r1, c1, r2, c2 := m.sel.normalized()
	m.refresh()
	return copyIfNonEmpty(selectedText(m.transcript.VisibleLines(), r1, c1, r2, c2))
}

func copyIfNonEmpty(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return copyToClipboardCmd(text)
}

// renderTranscriptContent returns the transcript pane's visible content with
// the focused-item indicator and any active/held selection composited in.
// Both are screen-space overlays applied fresh every render — neither is
// baked into transcriptItem's own cache, so they need no invalidation of
// their own and never go stale across scroll/resize.
func (m model) renderTranscriptContent() string {
	lines := m.transcript.VisibleLines()
	if len(lines) == 0 {
		return ""
	}

	// P40.3: reverse-highlight the active search query wherever it appears in
	// the visible window. Applied before the focused-item bar and selection
	// overlays below, all of which preserve each line's cell width so the
	// offsets they compute stay valid after this pass.
	if m.overlays.search != nil && strings.TrimSpace(m.overlays.search.query) != "" {
		hl := lipgloss.NewStyle().Foreground(colBrandFg).Background(colBrandBg).Bold(true)
		for row := range lines {
			lines[row] = highlightSearchMatches(lines[row], m.overlays.search.query, hl)
		}
	}

	if m.focusedIdx >= 0 {
		bar := lipgloss.NewStyle().Foreground(colAccent).Render("▎")
		for row := range lines {
			if idx, _ := m.transcript.ItemIndexAtY(row); idx == m.focusedIdx {
				plainLen := len([]rune(ansi.Strip(lines[row])))
				// Overwrite column 0 rather than prepending, so the line's
				// total cell width is unchanged — the pane's word-wrap
				// already sized every line to exactly m.transcript.Width().
				lines[row] = bar + ansi.Cut(lines[row], min(1, plainLen), plainLen)
			}
		}
	}

	if m.sel.active || m.sel.have {
		r1, c1, r2, c2 := m.sel.normalized()
		// P74.18: a solid background fill, not SGR-7 Reverse. Reverse swaps
		// fg/bg per cell, which fragments over chroma-highlighted diffs and
		// read_file output — every differently-colored token inverts to a
		// different background. A fill preserves each cell's foreground and
		// only replaces the background, matching how a real terminal
		// selection reads.
		sel := lipgloss.NewStyle().Background(colSelectionBg)
		for row := max(r1, 0); row <= r2 && row < len(lines); row++ {
			line := lines[row]
			plainLen := len([]rune(ansi.Strip(line)))
			left, right := 0, plainLen
			if row == r1 {
				left = c1
			}
			if row == r2 {
				right = c2
			}
			left = max(0, min(left, plainLen))
			right = max(left, min(right, plainLen))
			if left == right {
				continue
			}
			prefix := ansi.Cut(line, 0, left)
			mid := ansi.Cut(line, left, right)
			suffix := ansi.Cut(line, right, plainLen)
			lines[row] = prefix + sel.Render(mid) + suffix
		}
	}

	return strings.Join(lines, "\n")
}

// renderScrollbar draws a single-column glyph track for the transcript pane,
// replacing the old title-bar "62% ·" text: a thumb (accent-colored) sized
// and positioned proportionally to the visible fraction of content, over a
// dim track. P74.2: it carries no information while pinned to the bottom —
// the normal state — so it renders as a blank column there, the way a GUI
// overlay scrollbar auto-hides; it only draws once the user has scrolled
// away (m.streamState.followBottom false), or when nothing is scrollable at all.
func (m model) renderScrollbar() string {
	h := m.transcript.Height()
	if h <= 0 {
		return ""
	}
	start, end, ok := m.transcript.ScrollbarThumb()
	ok = ok && !m.streamState.followBottom
	track := lipgloss.NewStyle().Foreground(colBorder)
	thumb := lipgloss.NewStyle().Foreground(colAccent)

	var b strings.Builder
	for row := 0; row < h; row++ {
		switch {
		case ok && row >= start && row < end:
			b.WriteString(thumb.Render("┃"))
		case ok:
			b.WriteString(track.Render("│"))
		default:
			b.WriteString(" ")
		}
		if row < h-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
