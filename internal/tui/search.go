package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// searchState backs the P40.3 in-transcript full-text search: an incremental
// find over the open session's rendered transcript, entered with the
// TranscriptSearch key (ctrl+f). While it is non-nil the composer's status
// line becomes a search bar and every keystroke is captured for the search —
// typing edits the query and re-runs the match live, enter / ↓ / ctrl+n (and
// ↑ / ctrl+p) step between matches, esc closes it. It is a view-only concern:
// nothing about the session or the transcript's stored content changes, so it
// needs no persistence and resets on close.
//
// The pickers in dialog.go all fuzzy-filter *lists of turns*; none greps the
// message *content*, which is what "find the earlier message where I asked
// about X" needs. This fills that gap the way lnav's incremental `/`-search
// with n/N-next-match does, adapted to a composer-always-focused TUI by making
// search a short-lived modal input mode rather than a persistent keybinding.
type searchState struct {
	query   string
	matches []int // transcript item indices containing the query, in order
	current int   // index into matches of the focused match; -1 when none
}

// run recomputes matches for the current query against the pane, keeping the
// viewport steady: it re-focuses the same item if it still matches, else the
// first match at or after the previously focused one, so incremental typing
// doesn't yank the scroll position around. An empty query clears the matches.
func (s *searchState) run(p *transcriptPane) {
	prev := -1
	if s.current >= 0 && s.current < len(s.matches) {
		prev = s.matches[s.current]
	}
	s.matches = p.Search(s.query)
	if len(s.matches) == 0 {
		s.current = -1
		return
	}
	s.current = 0
	for i, idx := range s.matches {
		if idx >= prev {
			s.current = i
			break
		}
	}
}

// step advances the focused match by delta (positive = later in the
// transcript), wrapping at both ends so n/N never dead-ends.
func (s *searchState) step(delta int) {
	if len(s.matches) == 0 {
		return
	}
	s.current = (s.current + delta) % len(s.matches)
	if s.current < 0 {
		s.current += len(s.matches)
	}
}

// currentItem returns the transcript item index of the focused match, or -1
// when there is no current match.
func (s *searchState) currentItem() int {
	if s.current < 0 || s.current >= len(s.matches) {
		return -1
	}
	return s.matches[s.current]
}

// Search returns the indices (into items) of every real item whose visible,
// ANSI-stripped text contains query, case-insensitively, in transcript order.
// A blank query matches nothing. The trim marker and the ephemeral streaming
// tail are deliberately excluded — neither is a navigable conversation turn,
// and the tail is rebuilt every frame so an index into it would be unstable.
func (p *transcriptPane) Search(query string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []int
	for i, it := range p.items {
		if strings.Contains(strings.ToLower(ansi.Strip(it.raw)), q) {
			out = append(out, i)
		}
	}
	return out
}

// ItemSegment translates a public item index (as used by Search/ScrollToItem)
// into the internal segment index the focused-item accent bar addresses,
// accounting for a trim marker occupying segment 0.
func (p *transcriptPane) ItemSegment(idx int) int { return idx + p.markerOffset() }

// openSearch enters transcript-search mode, blurring the composer so the
// search bar owns keyboard input. A no-op if search is already open.
func (m *model) openSearch() {
	if m.search != nil {
		return
	}
	m.search = &searchState{current: -1}
	m.ta.Blur()
	m.refresh()
}

// closeSearch leaves search mode, clearing the match highlight and returning
// keyboard focus to the composer.
func (m *model) closeSearch() {
	m.search = nil
	m.focusedIdx = -1
	m.ta.Focus()
	m.refresh()
}

// jumpToSearchMatch scrolls the transcript so the focused match sits at the top
// of the viewport and marks it with the focused-item accent bar. With no
// current match it just clears the marker.
func (m *model) jumpToSearchMatch() {
	idx := m.search.currentItem()
	if idx < 0 {
		m.focusedIdx = -1
		m.refresh()
		return
	}
	m.transcript.ScrollToItem(idx)
	m.focusedIdx = m.transcript.ItemSegment(idx)
	m.followBottom = false
	m.refresh()
}

// handleSearchKey processes a keypress while search mode is active: navigation
// keys step between matches, printable text extends the query (re-running the
// search live), backspace trims it, and esc/ctrl+c close.
func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeSearch()
		return m, nil
	case "enter", "down", "ctrl+n", "ctrl+f":
		m.search.step(1)
		m.jumpToSearchMatch()
		return m, nil
	case "up", "ctrl+p":
		m.search.step(-1)
		m.jumpToSearchMatch()
		return m, nil
	case "backspace":
		if r := []rune(m.search.query); len(r) > 0 {
			m.search.query = string(r[:len(r)-1])
			m.search.run(m.transcript)
			m.jumpToSearchMatch()
		}
		return m, nil
	}
	// Printable text (kp.Text is empty for control/navigation keys) extends the
	// query and re-runs the incremental search.
	if kp, ok := msg.(tea.KeyPressMsg); ok && kp.Text != "" {
		m.search.query += kp.Text
		m.search.run(m.transcript)
		m.jumpToSearchMatch()
	}
	return m, nil
}

// renderSearchStatus renders the search bar shown in place of the composer's
// status line while search mode is active.
func (m model) renderSearchStatus() string {
	label := lipgloss.NewStyle().
		Background(colBrandBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1).
		Render("SEARCH")
	cursor := lipgloss.NewStyle().Reverse(true).Render(" ")

	var count string
	switch {
	case strings.TrimSpace(m.search.query) == "":
		count = m.th.statusDim.Render("type to search · esc close")
	case len(m.search.matches) == 0:
		count = lipgloss.NewStyle().Foreground(colWarning).Render("no matches")
	default:
		count = m.th.statusDim.Render(fmt.Sprintf("%d/%d · ⏎/↑↓ next · esc close",
			m.search.current+1, len(m.search.matches)))
	}
	return label + " " + m.search.query + cursor + "  " + count
}

// highlightSearchMatches reverse-highlights every occurrence of query within a
// single already-ANSI-styled transcript line, preserving the surrounding
// styling. The matched substring is re-rendered as plain styled text (its
// original foreground is dropped in favor of the highlight), which keeps the
// composited SGR runs clean rather than layering a background over an
// unterminated color run. Cell width is unchanged, so downstream overlays
// (the focused-item bar, mouse selection) keep working on the same offsets.
func highlightSearchMatches(line, query string, style lipgloss.Style) string {
	q := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(q) == 0 {
		return line
	}
	plain := []rune(ansi.Strip(line))
	lower := []rune(strings.ToLower(string(plain)))
	if len(lower) < len(q) {
		return line
	}

	var ranges [][2]int
	for i := 0; i+len(q) <= len(lower); {
		if string(lower[i:i+len(q)]) == string(q) {
			ranges = append(ranges, [2]int{i, i + len(q)})
			i += len(q)
		} else {
			i++
		}
	}
	if len(ranges) == 0 {
		return line
	}

	var b strings.Builder
	last := 0
	for _, rg := range ranges {
		b.WriteString(ansi.Cut(line, last, rg[0]))
		b.WriteString(style.Render(string(plain[rg[0]:rg[1]])))
		last = rg[1]
	}
	b.WriteString(ansi.Cut(line, last, len(plain)))
	return b.String()
}
