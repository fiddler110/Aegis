package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testKeyMsg builds a tea.KeyMsg whose String() matches the given key
// string, for the small fixed set of keys transcriptPane.HandleKey matches.
func testKeyMsg(s string) tea.KeyMsg {
	switch s {
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+f":
		return tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
	case "ctrl+b":
		return tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

func TestTranscriptAppendAndRender(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	// Each item ends on its own line — the invariant documented on
	// transcriptPane.Append — so per-item width padding never merges into the
	// next item's content.
	tr.Append("hello\n")
	tr.Append("world\n")

	got := tr.View()
	helloAt := strings.Index(got, "hello")
	worldAt := strings.Index(got, "world")
	if helloAt < 0 || worldAt < 0 || helloAt > worldAt {
		t.Fatalf("expected %q then %q in order, got %q", "hello", "world", got)
	}
	if tr.Len() != 2 {
		t.Fatalf("got %d items, want 2", tr.Len())
	}
}

func TestTranscriptAppendEmptyIsNoop(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	tr.Append("")
	if tr.Len() != 0 {
		t.Fatalf("expected empty append to be a no-op, got %d items", tr.Len())
	}
}

func TestTranscriptAppendRawBypassesWrap(t *testing.T) {
	tr := newTranscriptPane(10, 100)
	// A line far wider than the pane — wrap() would reflow this at width 10,
	// but AppendRaw content (P16.9 image thumbnails) must reach View()
	// byte-for-byte since it's pre-sized to a fixed cell box.
	wide := strings.Repeat("x", 40) + "\n"
	tr.AppendRaw(wide)

	got := tr.View()
	if !strings.Contains(got, strings.Repeat("x", 40)) {
		t.Fatalf("expected unwrapped 40-char run in output, got %q", got)
	}
	if got := tr.items[0].height(tr.width); got != 1 {
		t.Fatalf("expected the item to report height 1, got %d", got)
	}
}

func TestTranscriptAppendRawEmptyIsNoop(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	tr.AppendRaw("")
	if tr.Len() != 0 {
		t.Fatalf("expected empty AppendRaw to be a no-op, got %d items", tr.Len())
	}
}

func TestTranscriptAppendRawStableAcrossWidthChange(t *testing.T) {
	tr := newTranscriptPane(10, 100)
	raw := strings.Repeat("y", 40) + "\n"
	tr.AppendRaw(raw)
	before := tr.View()

	tr.SetSize(5, 100)
	after := tr.View()

	if before != after {
		t.Fatalf("noWrap item content changed across a width resize:\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestScrollbarThumbNoScrollWhenContentFits(t *testing.T) {
	tr := newTranscriptPane(80, 10)
	tr.Append("one\ntwo\nthree\n")
	if _, _, ok := tr.ScrollbarThumb(); ok {
		t.Fatalf("expected no thumb when content fits within the viewport")
	}
}

func TestScrollbarThumbTracksScrollPosition(t *testing.T) {
	tr := newTranscriptPane(80, 10)
	for i := 0; i < 50; i++ {
		tr.Append("line\n")
	}
	tr.GotoTop()
	topStart, topEnd, ok := tr.ScrollbarThumb()
	if !ok {
		t.Fatalf("expected a thumb once content overflows the viewport")
	}
	if topStart != 0 {
		t.Fatalf("expected thumb to start at row 0 when scrolled to top, got %d", topStart)
	}
	if topEnd <= topStart {
		t.Fatalf("expected a non-empty thumb range, got [%d,%d)", topStart, topEnd)
	}

	tr.GotoBottom()
	botStart, botEnd, ok := tr.ScrollbarThumb()
	if !ok {
		t.Fatalf("expected a thumb at the bottom too")
	}
	if botEnd != tr.Height() {
		t.Fatalf("expected thumb to reach the last row (%d) when scrolled to bottom, got end %d", tr.Height(), botEnd)
	}
	if botStart <= topStart {
		t.Fatalf("expected thumb to move down between top (%d) and bottom (%d)", topStart, botStart)
	}
}

func TestVisibleLinesMatchesView(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	tr.Append("hello\n")
	tr.Append("world\n")
	lines := tr.VisibleLines()
	if strings.Join(lines, "\n") != tr.View() {
		t.Fatalf("expected VisibleLines joined with \\n to equal View(), got %q vs %q", strings.Join(lines, "\n"), tr.View())
	}
}

func TestTranscriptReset(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	tr.Append("some content")
	tr.Reset()
	if tr.Len() != 0 || tr.rawBytes != 0 || tr.View() != "" {
		t.Fatalf("expected reset pane to be empty, got len=%d rawBytes=%d view=%q",
			tr.Len(), tr.rawBytes, tr.View())
	}
}

func TestTranscriptRenderCachesPerItemWidth(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	tr.Append("first item content that is reasonably long for wrapping purposes")

	out80 := tr.items[0].rendered(80)
	if !tr.items[0].cached || tr.items[0].cacheW != 80 {
		t.Fatalf("expected item to cache at width 80")
	}
	// Re-rendering at the same width must reuse the cache (same output, no panic/mutation issue).
	if again := tr.items[0].rendered(80); again != out80 {
		t.Fatalf("expected identical cached output, got %q vs %q", again, out80)
	}
	// A different width must invalidate and rewrap.
	out40 := tr.items[0].rendered(40)
	if tr.items[0].cacheW != 40 {
		t.Fatalf("expected cache to track the new width 40, got %d", tr.items[0].cacheW)
	}
	if out40 == out80 && len(out80) > 40 {
		t.Fatalf("expected rewrap at a narrower width to change output")
	}
}

func TestTranscriptTrimDropsWholeItemsNotMidLine(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	// Push well past the budget with many distinct items, each terminated by
	// a newline, so we can assert no item is ever cut mid-content.
	item := strings.Repeat("x", 1024) + "\n"
	itemsNeeded := (maxTranscriptBytes / len(item)) + 10
	for i := 0; i < itemsNeeded; i++ {
		tr.Append(item)
	}

	if tr.rawBytes > maxTranscriptBytes {
		t.Fatalf("rawBytes %d exceeds budget %d after trim", tr.rawBytes, maxTranscriptBytes)
	}
	if !tr.trimmed {
		t.Fatal("expected trimmed to be true after exceeding budget")
	}
	if tr.marker == nil || tr.marker.raw != trimmedMarker {
		t.Fatalf("expected a trimmed marker, got %+v", tr.marker)
	}
	// Every remaining item must be byte-identical to the original unit —
	// i.e. trimming dropped whole items, never sliced one.
	for _, it := range tr.items {
		if it.raw != item {
			t.Fatalf("expected an untouched whole item, got %q", it.raw)
		}
	}
}

func TestTranscriptTrimMarkerInsertedOnce(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	item := strings.Repeat("y", 1024) + "\n"
	itemsNeeded := (maxTranscriptBytes / len(item)) + 10
	for i := 0; i < itemsNeeded; i++ {
		tr.Append(item)
	}
	if tr.marker == nil {
		t.Fatal("expected a trimmed marker after repeated trims")
	}
	// The marker itself is never part of the evictable items slice, so it
	// can't be duplicated or dropped by a later trim pass.
	for _, it := range tr.items {
		if it.raw == trimmedMarker {
			t.Fatal("the trimmed marker must not appear inside the evictable items slice")
		}
	}
}

func TestTranscriptScrollToItem(t *testing.T) {
	tr := newTranscriptPane(80, 100)
	tr.Append("a\n")
	tr.Append("b\n")
	tr.Append("c\n")

	tr.ScrollToItem(2)
	got := tr.View()
	if strings.Contains(got, "a") || strings.Contains(got, "b") {
		t.Fatalf("ScrollToItem(2) should skip items a,b, got %q", got)
	}
	if !strings.Contains(got, "c") {
		t.Fatalf("ScrollToItem(2) missing expected content, got %q", got)
	}

	tr.ScrollToItem(0)
	got0 := tr.View()
	if !strings.Contains(got0, "a") || !strings.Contains(got0, "b") || !strings.Contains(got0, "c") {
		t.Fatalf("ScrollToItem(0) should show everything, got %q", got0)
	}

	// An index beyond the item count clamps rather than panicking.
	tr.ScrollToItem(99)
	_ = tr.View()
}

func TestTranscriptPaneViewIsWindowed(t *testing.T) {
	tr := newTranscriptPane(80, 10)
	for i := 0; i < 5000; i++ {
		tr.Append("line\n")
	}
	tr.GotoBottom()
	_ = tr.View()

	// Item 0 sits far outside the visible window at the bottom of a 5000-item
	// pane with a 10-line viewport; if View() were still O(total) it would have
	// touched (and cached) item 0 along the way. It shouldn't have.
	if tr.items[0].cached {
		t.Fatal("expected View() to leave far-off-screen items uncached (O(visible), not O(total))")
	}
}

// bruteOffsetLines recomputes the "lines scrolled past above the viewport
// top" quantity from scratch, independent of transcriptPane's incrementally
// maintained offsetLinesCache — the ground truth a differential test checks
// the maintained cache against.
func bruteOffsetLines(tr *transcriptPane) int {
	sum := 0
	for i := 0; i < tr.offsetIdx; i++ {
		sum += tr.segmentAt(i).height(tr.width)
	}
	return sum + tr.offsetLine
}

// TestOffsetLinesCacheMatchesBruteForce exercises a long, varied sequence of
// scroll/content operations (ScrollBy in both directions across segment
// boundaries, GotoTop/GotoBottom, resize, append, trim-triggering growth,
// SetItemRaw, ScrollToItem) and checks the incrementally maintained
// offsetLines() value against a from-scratch recomputation after every step.
// This is the correctness backstop for P18.2's scroll-tick perf fix
// (transcript.go: offsetLinesCacheSum maintained by ScrollBy/GotoTop/
// GotoBottom instead of recomputed on every call) — a fast but wrong cache
// would be worse than the O(n) walk it replaces.
func TestOffsetLinesCacheMatchesBruteForce(t *testing.T) {
	tr := newTranscriptPane(40, 8)
	var blocks []*transcriptItem
	rng := rand.New(rand.NewSource(1))

	check := func(step string) {
		t.Helper()
		got := tr.offsetLines()
		want := bruteOffsetLines(tr)
		if got != want {
			t.Fatalf("after %s: offsetLines() = %d, want %d (offsetIdx=%d offsetLine=%d)",
				step, got, want, tr.offsetIdx, tr.offsetLine)
		}
	}

	for i := 0; i < 400; i++ {
		switch rng.Intn(9) {
		case 0, 1:
			tr.Append(fmt.Sprintf("line %d\n", i))
			check("Append")
		case 2:
			tr.Append(strings.Repeat(fmt.Sprintf("word%d ", i), 20) + "\n\n")
			check("Append (multi-line)")
		case 3:
			tr.ScrollBy(rng.Intn(7) - 3) // -3..3, including 0 (no-op)
			check("ScrollBy")
		case 4:
			tr.GotoTop()
			check("GotoTop")
		case 5:
			tr.GotoBottom()
			check("GotoBottom")
		case 6:
			tr.SetSize(30+rng.Intn(40), tr.Height())
			check("SetSize (width change)")
		case 7:
			if len(blocks) > 0 {
				b := blocks[rng.Intn(len(blocks))]
				tr.SetItemRaw(b, fmt.Sprintf("edited %d\n", i))
				check("SetItemRaw")
			}
		case 8:
			b := tr.AppendBlock(fmt.Sprintf("block %d\n", i))
			if b != nil {
				blocks = append(blocks, b)
			}
			check("AppendBlock")
		}
	}
}

func TestTranscriptItemIndexAtY(t *testing.T) {
	tr := newTranscriptPane(80, 10)
	tr.Append("one\n")       // item 0: 1 line
	tr.Append("two\ntwo2\n") // item 1: 2 lines
	tr.Append("three\n")     // item 2: 1 line
	tr.GotoTop()

	cases := []struct {
		y            int
		wantIdx      int
		wantLineWith int
	}{
		{0, 0, 0}, // first line of item 0
		{1, 1, 0}, // first line of item 1
		{2, 1, 1}, // second line of item 1
		{3, 2, 0}, // item 2
		{99, -1, -1},
	}
	for _, c := range cases {
		idx, lineWith := tr.ItemIndexAtY(c.y)
		if idx != c.wantIdx || lineWith != c.wantLineWith {
			t.Fatalf("ItemIndexAtY(%d) = (%d, %d), want (%d, %d)", c.y, idx, lineWith, c.wantIdx, c.wantLineWith)
		}
	}
}

func TestTranscriptHandleKeyMatchesViewportDefaults(t *testing.T) {
	newPane := func() *transcriptPane {
		tr := newTranscriptPane(80, 5)
		for i := 0; i < 50; i++ {
			tr.Append("line\n")
		}
		tr.GotoTop()
		return tr
	}

	cases := []struct {
		key    string
		action func(tr *transcriptPane) // expected equivalent direct call
	}{
		{"pgdown", func(tr *transcriptPane) { tr.PageDown() }},
		{"space", func(tr *transcriptPane) { tr.PageDown() }},
		{"f", func(tr *transcriptPane) { tr.PageDown() }},
		{"pgup", func(tr *transcriptPane) { tr.PageUp() }},
		{"b", func(tr *transcriptPane) { tr.PageUp() }},
		{"u", func(tr *transcriptPane) { tr.HalfPageUp() }},
		{"ctrl+u", func(tr *transcriptPane) { tr.HalfPageUp() }},
		{"d", func(tr *transcriptPane) { tr.HalfPageDown() }},
		{"ctrl+d", func(tr *transcriptPane) { tr.HalfPageDown() }},
		{"down", func(tr *transcriptPane) { tr.ScrollDown(1) }},
		{"j", func(tr *transcriptPane) { tr.ScrollDown(1) }},
		{"up", func(tr *transcriptPane) { tr.ScrollUp(1) }},
		{"k", func(tr *transcriptPane) { tr.ScrollUp(1) }},
		{"ctrl+f", func(tr *transcriptPane) { tr.PageDown() }},
		{"ctrl+b", func(tr *transcriptPane) { tr.PageUp() }},
		// P40.2: g/G and home/end jump to top/bottom.
		{"g", func(tr *transcriptPane) { tr.GotoTop() }},
		{"home", func(tr *transcriptPane) { tr.GotoTop() }},
		{"G", func(tr *transcriptPane) { tr.GotoBottom() }},
		{"end", func(tr *transcriptPane) { tr.GotoBottom() }},
	}
	// Start a few pages down so top/bottom jumps are observable, not no-ops from
	// an already-top pane; both the key-driven and direct panes share it so
	// relative-scroll cases still compare like-for-like.
	prescroll := func(tr *transcriptPane) { tr.PageDown(); tr.PageDown() }
	for _, c := range cases {
		viaKey := newPane()
		prescroll(viaKey)
		if !viaKey.HandleKey(testKeyMsg(c.key)) {
			t.Fatalf("HandleKey(%q) returned false, want true (consumed)", c.key)
		}
		viaDirect := newPane()
		prescroll(viaDirect)
		c.action(viaDirect)
		if viaKey.offsetIdx != viaDirect.offsetIdx || viaKey.offsetLine != viaDirect.offsetLine {
			t.Fatalf("key %q: offset (%d,%d), want (%d,%d) matching direct call",
				c.key, viaKey.offsetIdx, viaKey.offsetLine, viaDirect.offsetIdx, viaDirect.offsetLine)
		}
	}

	unmatched := newPane()
	if unmatched.HandleKey(testKeyMsg("x")) {
		t.Fatal("HandleKey should report false for a key it doesn't consume")
	}
}

func TestTranscriptHandleMouseWheelMatchesViewportDefaults(t *testing.T) {
	newPane := func() *transcriptPane {
		tr := newTranscriptPane(80, 5)
		for i := 0; i < 50; i++ {
			tr.Append("line\n")
		}
		tr.GotoTop()
		return tr
	}

	down := newPane()
	if !down.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown}) {
		t.Fatal("expected wheel-down to be consumed")
	}
	wantDown := newPane()
	wantDown.ScrollDown(3)
	if down.offsetIdx != wantDown.offsetIdx || down.offsetLine != wantDown.offsetLine {
		t.Fatalf("wheel-down offset (%d,%d), want (%d,%d)", down.offsetIdx, down.offsetLine, wantDown.offsetIdx, wantDown.offsetLine)
	}

	up := newPane()
	up.GotoBottom()
	if !up.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp}) {
		t.Fatal("expected wheel-up to be consumed")
	}
	wantUp := newPane()
	wantUp.GotoBottom()
	wantUp.ScrollUp(3)
	if up.offsetIdx != wantUp.offsetIdx || up.offsetLine != wantUp.offsetLine {
		t.Fatalf("wheel-up offset (%d,%d), want (%d,%d)", up.offsetIdx, up.offsetLine, wantUp.offsetIdx, wantUp.offsetLine)
	}
}

// mdPlain is a stand-in markdown renderer for liveBlock tests: it wraps like
// the real glamour path but stays deterministic and dependency-free.
func mdPlain(s string) string { return wrap(s, 80) }

func TestLiveBlockRenderAndReset(t *testing.T) {
	lb := &liveBlock{}
	if got := lb.render(80, mdPlain); got != "" {
		t.Fatalf("empty live block should render empty, got %q", got)
	}

	lb.setText("hello")
	if got := lb.render(80, mdPlain); !strings.Contains(got, "hello") {
		t.Fatalf("expected rendered text to contain %q, got %q", "hello", got)
	}

	lb.reset()
	if lb.raw != "" || lb.prefixCache != "" || lb.prefixCacheTo != 0 {
		t.Fatalf("expected reset to clear all liveBlock state, got %+v", lb)
	}
}

func TestLiveBlockPrefixCacheStableAcrossGrowth(t *testing.T) {
	lb := &liveBlock{}
	lb.setText("paragraph one.\n\npar")
	first := lb.render(80, mdPlain)
	cachedPrefixLen := len(lb.prefixCache)

	// Growing the tail (without crossing a new blank-line boundary) must not
	// shrink the cached settled prefix.
	lb.setText("paragraph one.\n\nparagraph two continues here")
	second := lb.render(80, mdPlain)
	if len(lb.prefixCache) != cachedPrefixLen {
		t.Fatalf("expected the settled prefix cache to remain stable, got len %d vs %d", len(lb.prefixCache), cachedPrefixLen)
	}
	if first == second {
		t.Fatalf("expected rendered output to reflect the grown tail")
	}
}

func TestLiveBlockRendersThroughMarkdownRenderer(t *testing.T) {
	lb := &liveBlock{}
	calls := 0
	md := func(s string) string {
		calls++
		return "R[" + s + "]"
	}
	lb.setText("settled paragraph.\n\ntail")
	out := lb.render(80, md)
	// Both the settled prefix and the live tail must pass through the markdown
	// renderer (TQ3) — no raw-wrapped text on screen while streaming.
	if !strings.Contains(out, "R[settled paragraph.\n\n]") || !strings.Contains(out, "R[tail]") {
		t.Fatalf("expected prefix and tail rendered via md callback, got %q", out)
	}
	if calls != 2 {
		t.Fatalf("expected 2 renderer calls (prefix + tail), got %d", calls)
	}
	// The settled prefix is cached: rendering again re-renders only the tail.
	lb.setText("settled paragraph.\n\ntail grows")
	_ = lb.render(80, md)
	if calls != 3 {
		t.Fatalf("expected cached prefix to skip re-render, got %d calls", calls)
	}
}

// TestCapLineLengthsTruncatesOnlyOverlongLines is the P81.33 regression: a
// single pathological line (one multi-megabyte line with no newlines) must
// be bounded, while normal multi-line content — including lines individually
// under the cap — passes through unchanged.
func TestCapLineLengthsTruncatesOnlyOverlongLines(t *testing.T) {
	short := "line one\nline two\nline three"
	if got := capLineLengths(short); got != short {
		t.Errorf("short content should pass through unchanged, got %q", got)
	}

	huge := strings.Repeat("x", maxLineRunes+5000)
	got := capLineLengths("before\n" + huge + "\nafter")
	if strings.Contains(got, huge) {
		t.Fatal("expected the overlong line to be truncated, found it verbatim")
	}
	if !strings.Contains(got, "before\n") || !strings.Contains(got, "\nafter") {
		t.Errorf("expected the surrounding short lines to survive untouched, got a %d-byte result", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected an explicit truncation marker, got %q", got[:200])
	}
	// The reset sequence must precede the marker so a cut mid-ANSI-run can't
	// leak an open style into the rest of the transcript.
	if !strings.Contains(got, "\x1b[0m […truncated") {
		t.Errorf("expected an SGR reset immediately before the truncation marker")
	}
}

// TestAppendEnforcesLineLengthCap proves the cap is actually applied at the
// transcript's write path, not just by the helper in isolation.
func TestAppendEnforcesLineLengthCap(t *testing.T) {
	p := newTranscriptPane(80, 24)
	huge := strings.Repeat("y", maxLineRunes*2)
	p.Append(huge + "\n")
	if len(p.items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(p.items))
	}
	if utf8RuneCount := len([]rune(p.items[0].raw)); utf8RuneCount > maxLineRunes*2 {
		t.Errorf("expected Append to cap the line length, stored item has %d runes", utf8RuneCount)
	}
}
