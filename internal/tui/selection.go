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
// layout by construction: one title-bar row, PaddingLeft(1) on the main
// panel, plus the sidebar's width when it's open and wide enough to show.
func (m *model) paneOrigin() (col, row int) {
	col, row = 1, 1
	if m.sidebarOpen && m.width >= sidebarMinTermW {
		col += m.sidebarW + 1 // inner width + right border (P40.1)
	}
	return col, row
}

// toPaneCoord translates absolute mouse coordinates to pane-relative
// (col,row); ok is false when the point falls outside the transcript pane
// (over the sidebar, title bar, textarea, terminal pane, or the scrollbar
// column to its right).
func (m *model) toPaneCoord(x, y int) (col, row int, ok bool) {
	ox, oy := m.paneOrigin()
	col, row = x-ox, y-oy
	if col < 0 || row < 0 || col >= m.transcript.Width() || row >= m.transcript.Height() {
		return 0, 0, false
	}
	return col, row, true
}

// clampPaneCoord is toPaneCoord without the bounds rejection — used while
// dragging so a selection extends to the nearest visible row/col instead of
// freezing the instant the pointer crosses the pane edge.
func (m *model) clampPaneCoord(x, y int) (col, row int) {
	ox, oy := m.paneOrigin()
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

// handleMouseClick processes a mouse button press over the transcript pane:
// a single click focuses the message under the cursor and arms a drag; a
// double-click selects the word under the cursor and copies it immediately;
// a triple-click does the same for the whole line. Clicks outside the pane
// (sidebar, title bar, textarea, scrollbar) are ignored.
func (m *model) handleMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	col, row, ok := m.toPaneCoord(msg.X, msg.Y)
	if !ok {
		return nil
	}
	count := m.registerClick(col, row)
	if idx, _ := m.transcript.ItemIndexAtY(row); idx >= 0 {
		m.focusedIdx = idx
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
	if m.search != nil && strings.TrimSpace(m.search.query) != "" {
		hl := lipgloss.NewStyle().Foreground(colBrandFg).Background(colBrandBg).Bold(true)
		for row := range lines {
			lines[row] = highlightSearchMatches(lines[row], m.search.query, hl)
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
		reverse := lipgloss.NewStyle().Reverse(true)
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
			lines[row] = prefix + reverse.Render(mid) + suffix
		}
	}

	return strings.Join(lines, "\n")
}

// renderScrollbar draws a single-column glyph track for the transcript pane,
// replacing the old title-bar "62% ·" text: a thumb (accent-colored) sized
// and positioned proportionally to the visible fraction of content, over a
// dim track — or a blank column when everything fits without scrolling.
func (m model) renderScrollbar() string {
	h := m.transcript.Height()
	if h <= 0 {
		return ""
	}
	start, end, ok := m.transcript.ScrollbarThumb()
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
