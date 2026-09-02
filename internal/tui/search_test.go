package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestTranscriptPaneSearch checks the content grep that backs P40.3: it is
// case-insensitive, ANSI-agnostic, returns item indices in transcript order,
// and matches nothing for a blank query.
func TestTranscriptPaneSearch(t *testing.T) {
	p := newTranscriptPane(80, 20)
	p.Append("Please fix the login BUG in auth.go\n")
	p.Append(strings.Join([]string{
		"\x1b[32massistant\x1b[0m: I patched the parser.\n",
	}, ""))
	p.Append("Now write a test for the bug fix\n")

	got := p.Search("bug")
	want := []int{0, 2}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Search(bug) = %v, want %v", got, want)
	}

	if m := p.Search("PARSER"); len(m) != 1 || m[0] != 1 {
		t.Fatalf("Search(PARSER) should match the ANSI-styled item 1, got %v", m)
	}
	if m := p.Search("   "); m != nil {
		t.Fatalf("blank query should match nothing, got %v", m)
	}
	if m := p.Search("nonexistent"); m != nil {
		t.Fatalf("no-match query should return nil, got %v", m)
	}
}

// TestSearchStateNavigation checks match stepping wraps and that run() keeps
// focus on the nearest surviving match as the query narrows.
func TestSearchStateNavigation(t *testing.T) {
	p := newTranscriptPane(80, 20)
	p.Append("alpha bug one\n")   // 0
	p.Append("beta clean\n")      // 1
	p.Append("gamma bug two\n")   // 2
	p.Append("delta bug three\n") // 3

	s := &searchState{current: -1}
	s.query = "bug"
	s.run(p)
	if len(s.matches) != 3 || s.current != 0 {
		t.Fatalf("run: matches=%v current=%d, want 3 matches at 0", s.matches, s.current)
	}
	if s.currentItem() != 0 {
		t.Fatalf("currentItem = %d, want 0", s.currentItem())
	}

	s.step(1)
	if s.currentItem() != 2 {
		t.Fatalf("after step(1) currentItem = %d, want 2", s.currentItem())
	}
	s.step(1)
	s.step(1) // wrap
	if s.currentItem() != 0 {
		t.Fatalf("step(1) past the end should wrap to 0, got %d", s.currentItem())
	}
	s.step(-1) // wrap backwards
	if s.currentItem() != 3 {
		t.Fatalf("step(-1) at the start should wrap to the last match, got %d", s.currentItem())
	}

	// Narrowing the query to something only item 3 has keeps focus there rather
	// than snapping back to the top.
	s.query = "three"
	s.run(p)
	if len(s.matches) != 1 || s.currentItem() != 3 {
		t.Fatalf("narrowed query: matches=%v current item=%d, want only item 3", s.matches, s.currentItem())
	}
}

// TestTranscriptSearchOverlayFlow drives the real Update path: ctrl+f opens the
// overlay, typed runes build the query, and esc closes it and restores the
// composer.
func TestTranscriptSearchOverlayFlow(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.transcript.Reset() // drop the seeded welcome item so indices are deterministic
	m.transcript.Append("first turn about widgets\n")
	m.transcript.Append("second turn about the parser bug\n")
	m.transcript.Append("third turn about deployment\n")

	// Open search.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.overlays.search == nil {
		t.Fatal("ctrl+f did not open the search overlay")
	}

	// Type "parser".
	for _, r := range "parser" {
		m = driveUpdate(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.overlays.search.query != "parser" {
		t.Fatalf("query = %q, want %q", m.overlays.search.query, "parser")
	}
	if len(m.overlays.search.matches) != 1 || m.overlays.search.currentItem() != 1 {
		t.Fatalf("matches=%v current item=%d, want only item 1", m.overlays.search.matches, m.overlays.search.currentItem())
	}

	// The search bar and (highlighted) match text are on screen.
	view := ansi.Strip(m.renderContent())
	if !strings.Contains(view, "SEARCH") {
		t.Fatalf("search bar not visible in view:\n%s", view)
	}
	if !strings.Contains(view, "1/1") {
		t.Fatalf("match count not shown in view:\n%s", view)
	}

	// Backspace shortens the query and widens the result set.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.overlays.search.query != "parse" {
		t.Fatalf("after backspace query = %q, want %q", m.overlays.search.query, "parse")
	}

	// Esc closes and refocuses the composer.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.overlays.search != nil {
		t.Fatal("esc did not close the search overlay")
	}
	if m.focusedIdx != -1 {
		t.Fatalf("focusedIdx = %d after closing search, want -1", m.focusedIdx)
	}
}

// TestHighlightSearchMatches confirms the substring highlighter marks matches,
// leaves non-matching lines byte-identical, and preserves visible width.
func TestHighlightSearchMatches(t *testing.T) {
	style := lipgloss.NewStyle().Reverse(true)

	line := "the quick brown fox"
	out := highlightSearchMatches(line, "quick", style)
	if out == line {
		t.Fatal("expected the matched line to be restyled")
	}
	if got := ansi.Strip(out); got != line {
		t.Fatalf("highlight changed visible text: %q != %q", got, line)
	}

	// Case-insensitive, multiple occurrences.
	multi := highlightSearchMatches("Bug bug BUG", "bug", style)
	if ansi.Strip(multi) != "Bug bug BUG" {
		t.Fatalf("multi-match highlight changed visible text: %q", ansi.Strip(multi))
	}

	// No match: returned unchanged.
	if same := highlightSearchMatches(line, "zzz", style); same != line {
		t.Fatal("non-matching line should be returned unchanged")
	}
	// Blank query: unchanged.
	if same := highlightSearchMatches(line, "   ", style); same != line {
		t.Fatal("blank query should be returned unchanged")
	}
}
