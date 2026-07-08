package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// maxTranscriptBytes bounds the transcript's total raw (pre-wrap) size.
// Trimming drops whole items from the front rather than severing content
// mid-line (TQ1) — the previous cappedBuffer implementation trimmed at the
// byte level and could truncate inside a styled ANSI run.
const maxTranscriptBytes = 1 << 20

const trimmedMarker = "[earlier output trimmed]\n\n"

// transcriptItem is one independently-cached, independently-addressable unit
// of the conversation: a user turn, an assistant reply, a tool call, a tool
// result, a system notice, and so on. raw is pre-styled ANSI content (the
// existing renderX helpers already produce this); rendered() lazily
// word-wraps it to the current pane width and caches the result — along with
// its terminated-line count — until the width changes.
//
// Per-item caching is what makes resize, theme change, and future
// retroactive re-render (e.g. /tools full applying to history) cheap: only
// the items whose wrapped output is stale get redone, instead of the whole
// transcript. Callers must end each item's raw content on a line boundary
// (trailing "\n" or "\n\n") — every call site in this package already writes
// complete lines, which is what makes concatenation across item boundaries
// safe (see transcriptPane.View).
type transcriptItem struct {
	raw string

	// noWrap marks content that must reach the terminal byte-for-byte
	// instead of going through wrap()'s lipgloss word-wrap — image
	// thumbnails (P16.9), whose SGR-styled lines are already sized to a
	// fixed cell box and must not be reflowed at an arbitrary pane width.
	noWrap bool

	cacheW      int
	cacheOut    string
	cacheHeight int
	cached      bool
}

func newItem(raw string) *transcriptItem { return &transcriptItem{raw: raw} }

func newRawItem(raw string) *transcriptItem { return &transcriptItem{raw: raw, noWrap: true} }

// rendered returns the wrapped ANSI string for width w, from cache when the
// width hasn't changed since the last call. noWrap items are returned as-is
// (still cached, since they need no per-width recomputation).
func (it *transcriptItem) rendered(w int) string {
	if it.noWrap {
		if !it.cached {
			it.cacheOut = it.raw
			it.cacheHeight = strings.Count(it.cacheOut, "\n")
			it.cached = true
		}
		return it.cacheOut
	}
	if !it.cached || it.cacheW != w {
		it.cacheOut = wrap(it.raw, w)
		it.cacheHeight = strings.Count(it.cacheOut, "\n")
		it.cacheW = w
		it.cached = true
	}
	return it.cacheOut
}

// height is the item's terminated-line count at width w (i.e. the number of
// "\n" in its wrapped output), used for scroll bookkeeping without needing to
// split the string into a line slice.
func (it *transcriptItem) height(w int) int {
	it.rendered(w)
	return it.cacheHeight
}

func (it *transcriptItem) invalidate() { it.cached = false }

// transcriptPane is the transcript's content store AND its scroll/display
// engine in one: a virtualized list of transcriptItems (crush's
// internal/ui/list/list.go model — see research/roadmap.md P16.4) that
// renders only the segments needed to fill the visible window, instead of
// concatenating the entire history on every frame. It replaces both the old
// transcript (content) and the bubbles/viewport.Model (scroll/display) that
// used to sit in front of it.
//
// Addressable "segments" are, in order: the non-evictable trim marker (if
// any), the real items, and an ephemeral trailing "tail" segment (streaming
// preview text set fresh every refresh() via SetTail — never cached,
// addressable, or evictable). Segment indexing lets scroll position be
// expressed as (segment index, line offset within that segment) rather than
// a byte/line offset into a flat buffer, which is what makes both
// ScrollToItem and the O(visible) View() possible.
type transcriptPane struct {
	items    []*transcriptItem
	rawBytes int

	// trimmed is true once old items have been evicted. The marker item
	// lives outside items so trim() can never evict it in a later pass.
	trimmed bool
	marker  *transcriptItem

	// tailRaw is the ephemeral trailing content (streaming thinking preview,
	// live assistant tail, queued-message previews) rebuilt by refresh()
	// every frame. tailCache avoids re-allocating a transcriptItem when the
	// text hasn't changed since the last View()/TotalHeight() call.
	tailRaw   string
	tailCache *transcriptItem

	width, height int

	// offsetIdx/offsetLine is the scroll position: offsetIdx is the segment
	// index of the first (possibly partially) visible segment, offsetLine is
	// how many of its lines are scrolled past above the viewport top.
	offsetIdx  int
	offsetLine int

	// itemsHeightCache/itemsHeightValid cache the summed height of the
	// marker + real items (everything except the ephemeral tail). Deliberately
	// separate from the tail's own contribution (see TotalHeight) so a
	// streaming tail changing on every token (SetTail) never forces an
	// O(items) resum — only mutations that can change a real item's height
	// (append, trim, edit, resize) invalidate this.
	itemsHeightCache int
	itemsHeightValid bool

	// offsetLinesCacheSum/-W/-Valid track the sum of segment heights strictly
	// before offsetIdx. Rather than a plain memo keyed on offsetIdx (which
	// would miss every tick for single-line-item transcripts, where offsetIdx
	// changes on every scroll step), ScrollBy/GotoTop/GotoBottom maintain this
	// sum incrementally as they move offsetIdx — each step adds or removes
	// exactly the one segment's height that crossed the offset boundary,
	// using heights those functions already compute for their own bookkeeping.
	// Anything that can change a pre-offset segment's own height
	// (invalidateItemsHeight) or jump offsetIdx non-incrementally
	// (ScrollToItem) invalidates instead, falling back to a full recompute on
	// next access — this is what keeps offsetLines() correct, not just fast.
	//
	// Before this, offsetLines() (called by ScrollbarThumb/ScrollPercent on
	// every render, for the scrollbar) re-walked every segment from the top
	// of the transcript on every single call — an O(scroll depth) cost paid
	// on every scroll tick and every streamed token while auto-follow is
	// active. See BenchmarkScrollTick_WithScrollbar_NoTrimCap in
	// transcript_bench_test.go for the confirming profile (P18.2).
	offsetLinesCacheSum   int
	offsetLinesCacheW     int
	offsetLinesCacheValid bool
}

func newTranscriptPane(width, height int) *transcriptPane {
	return &transcriptPane{width: width, height: height}
}

// invalidateItemsHeight marks the items-only height sum (and the dependent
// offsetLines memo, since it sums a prefix of the same segments) stale.
// Called by mutations that can change a real item's rendered height:
// append/trim/edit/resize. NOT called by SetTail — the tail is summed
// separately in TotalHeight, and it never contributes to offsetLines since
// it is always the last segment.
func (p *transcriptPane) invalidateItemsHeight() {
	p.itemsHeightValid = false
	p.offsetLinesCacheValid = false
}

// Append adds one rendered item. Empty strings are ignored (mirrors the old
// WriteString("") no-op behavior).
func (p *transcriptPane) Append(raw string) {
	if raw == "" {
		return
	}
	p.items = append(p.items, newItem(raw))
	p.rawBytes += len(raw)
	p.invalidateItemsHeight()
	p.trim()
}

// AppendRaw is Append, but the item bypasses wrap() entirely (see
// transcriptItem.noWrap) — for content, like image thumbnails (P16.9),
// that is already laid out to a fixed size and must not be reflowed.
func (p *transcriptPane) AppendRaw(raw string) {
	if raw == "" {
		return
	}
	p.items = append(p.items, newRawItem(raw))
	p.rawBytes += len(raw)
	p.invalidateItemsHeight()
	p.trim()
}

// AppendBlock is Append, but returns the created item so the caller can
// later swap its content in place (collapsible thinking blocks, TQ9).
// Returns nil for an empty raw string.
func (p *transcriptPane) AppendBlock(raw string) *transcriptItem {
	if raw == "" {
		return nil
	}
	it := newItem(raw)
	p.items = append(p.items, it)
	p.rawBytes += len(raw)
	p.invalidateItemsHeight()
	p.trim()
	// trim may have evicted the item we just appended (pathological budget);
	// report it only if it survived.
	if len(p.items) > 0 && p.items[len(p.items)-1] == it {
		return it
	}
	return nil
}

// SetItemRaw replaces a live item's content, keeping the byte accounting
// exact and invalidating its render cache. The caller must ensure it is
// still part of this pane.
func (p *transcriptPane) SetItemRaw(it *transcriptItem, raw string) {
	p.rawBytes += len(raw) - len(it.raw)
	it.raw = raw
	it.invalidate()
	p.invalidateItemsHeight()
	p.trim()
}

// trim drops the oldest items until the pane is back under budget. The first
// time this happens, a "[earlier output trimmed]" marker is recorded
// (rendered ahead of the remaining items) — it is never itself subject to
// eviction, unlike the old cappedBuffer's inline byte-trim.
func (p *transcriptPane) trim() {
	for p.rawBytes > maxTranscriptBytes && len(p.items) > 1 {
		p.rawBytes -= len(p.items[0].raw)
		p.items = p.items[1:]
		p.trimmed = true
		// Item 0 has been evicted from underneath the current scroll
		// position; shift the offset back so it still refers to the same
		// logical item (clamped by the segment walk elsewhere if it
		// overshoots).
		if p.offsetIdx > 0 {
			p.offsetIdx--
		}
	}
	if p.trimmed && p.marker == nil {
		p.marker = newItem(trimmedMarker)
		p.offsetIdx++ // keep pointing at the same item now that marker occupies segment 0
	}
	p.invalidateItemsHeight()
}

// Reset clears the pane (session switch, /clear).
func (p *transcriptPane) Reset() {
	p.items = nil
	p.rawBytes = 0
	p.trimmed = false
	p.marker = nil
	p.tailRaw = ""
	p.tailCache = nil
	p.offsetIdx = 0
	p.offsetLine = 0
	p.invalidateItemsHeight()
}

// Len returns the current item count (excluding the trim marker and the
// ephemeral tail), used as a stable position marker (the timeline picker's
// scroll-to-turn) instead of a byte/line offset into a flat buffer.
func (p *transcriptPane) Len() int { return len(p.items) }

// SetSize updates the pane's viewport dimensions. Item render caches
// self-invalidate lazily on next access when their cached width no longer
// matches (transcriptItem.rendered), so no global invalidation is needed
// here — only the cached height sums, which depend on every item's height at
// the current width.
func (p *transcriptPane) SetSize(w, h int) {
	if p.width != w {
		p.invalidateItemsHeight()
	}
	p.width = w
	p.height = h
}

func (p *transcriptPane) Width() int  { return p.width }
func (p *transcriptPane) Height() int { return p.height }

// SetTail sets the ephemeral trailing content (streaming thinking preview,
// live assistant tail, queued-message previews) rendered after every real
// item — the caller builds this by direct string concatenation exactly like
// refresh() used to build the whole viewport content, just scoped to the
// non-persisted trailing pieces. Called fresh every refresh(); never cached
// per-item since it changes every frame during streaming. A trailing
// newline is enforced (if raw is non-empty) so this segment's last visual
// line isn't silently dropped by View's height accounting, which — like
// every other segment — counts newlines to know how many lines a segment
// contributes.
//
// Deliberately does NOT invalidate itemsHeightCache/offsetLinesCache: the
// tail is always the last segment, so changing it can never affect the
// height of any earlier segment or the offsetLines() prefix sum before
// offsetIdx. TotalHeight adds the tail's own (independently cached, O(tail
// length) not O(history)) height on top of itemsHeight — see TotalHeight.
// Without this split, a streaming reply that calls SetTail on every token
// would force a full O(history) resum on every single token (P18.2).
func (p *transcriptPane) SetTail(raw string) {
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	if raw == p.tailRaw {
		return
	}
	p.tailRaw = raw
	p.tailCache = nil
}

// segmentCount is the number of addressable segments: marker (if any) +
// items + ephemeral tail (if any).
func (p *transcriptPane) segmentCount() int {
	n := len(p.items)
	if p.marker != nil {
		n++
	}
	if p.tailRaw != "" {
		n++
	}
	return n
}

// segmentAt returns the segment at logical index idx in O(1), without ever
// materializing a combined slice.
func (p *transcriptPane) segmentAt(idx int) *transcriptItem {
	if idx < 0 {
		return nil
	}
	if p.marker != nil {
		if idx == 0 {
			return p.marker
		}
		idx--
	}
	if idx < len(p.items) {
		return p.items[idx]
	}
	idx -= len(p.items)
	if idx == 0 && p.tailRaw != "" {
		if p.tailCache == nil {
			p.tailCache = newItem(p.tailRaw)
		}
		return p.tailCache
	}
	return nil
}

// markerOffset is 1 if a trim marker occupies segment 0, else 0 — the
// translation between the public "item index" address space (Len/
// ScrollToItem, unaware of the marker, matching the old blockIndex
// semantics) and the internal segment address space.
func (p *transcriptPane) markerOffset() int {
	if p.marker != nil {
		return 1
	}
	return 0
}

// TotalHeight returns the total number of rendered lines across every
// segment at the current width: the marker+items sum (itemsHeight, cached —
// see invalidateItemsHeight) plus the ephemeral tail's own height (tailHeight
// — cheap on its own, since the tail is a single item with its own wrap
// cache, O(tail length) not O(history)). Splitting these two sums is what
// lets a streaming tail change on every token (SetTail) without forcing a
// full re-walk of the whole transcript on every frame (P18.2).
func (p *transcriptPane) TotalHeight() int {
	return p.itemsHeight() + p.tailHeight()
}

// itemsHeight sums the marker (if any) + every real item's height, cached
// until invalidateItemsHeight() is called (append/trim/edit/resize —
// anything that can change a real item's contribution).
func (p *transcriptPane) itemsHeight() int {
	if !p.itemsHeightValid {
		total := 0
		if p.marker != nil {
			total += p.marker.height(p.width)
		}
		for _, it := range p.items {
			total += it.height(p.width)
		}
		p.itemsHeightCache = total
		p.itemsHeightValid = true
	}
	return p.itemsHeightCache
}

// tailHeight returns the ephemeral tail segment's height, or 0 if there is
// none. Needs no cache of its own beyond transcriptItem's per-item wrap
// cache (tailCache) — SetTail already invalidates that when the text
// actually changes.
func (p *transcriptPane) tailHeight() int {
	if p.tailRaw == "" {
		return 0
	}
	if p.tailCache == nil {
		p.tailCache = newItem(p.tailRaw)
	}
	return p.tailCache.height(p.width)
}

func (p *transcriptPane) TotalLineCount() int   { return p.TotalHeight() }
func (p *transcriptPane) VisibleLineCount() int { return p.height }

// lastOffsetSegment returns the segment index and line offset that puts the
// last segment's last line at the bottom of the viewport (crush's
// lastOffsetItem, list.go:203-225, generalized over segments). Bounded to
// the handful of segments needed to fill one viewport from the end — it
// deliberately never touches (or wrap-caches) segments further back than
// that, which is what keeps GotoBottom() cheap on a huge transcript.
func (p *transcriptPane) lastOffsetSegment() (idx, lineOffset int) {
	n := p.segmentCount()
	var windowHeight int
	for idx = n - 1; idx >= 0; idx-- {
		windowHeight += p.segmentAt(idx).height(p.width)
		if windowHeight > p.height {
			break
		}
	}
	lineOffset = max(windowHeight-p.height, 0)
	idx = max(idx, 0)
	return idx, lineOffset
}

// AtBottom reports whether the pane is showing the last segment at the
// bottom. Bounded by the number of segments between the current offset and
// the end, not the whole history (crush's AtBottom, list.go:123-144).
func (p *transcriptPane) AtBottom() bool {
	n := p.segmentCount()
	if n == 0 {
		return true
	}
	var totalHeight int
	for idx := p.offsetIdx; idx < n; idx++ {
		if totalHeight > p.height {
			return false
		}
		totalHeight += p.segmentAt(idx).height(p.width)
	}
	return totalHeight-p.offsetLine <= p.height
}

func (p *transcriptPane) AtTop() bool {
	return p.offsetIdx == 0 && p.offsetLine == 0
}

// offsetLines returns the total number of content lines scrolled past above
// the viewport top — the same quantity ScrollPercent and ScrollbarThumb both
// need, factored out so they can't drift apart.
//
// The sum of segment heights strictly before offsetIdx is normally kept
// correct incrementally by ScrollBy/GotoTop/GotoBottom as offsetIdx moves
// (see the offsetLinesCache* field docs), so the common scroll-tick path
// never re-walks anything here. This is the fallback full recompute for
// when the cache has been invalidated (content/width change, or a
// non-incremental jump like ScrollToItem).
func (p *transcriptPane) offsetLines() int {
	if !p.offsetLinesCacheValid || p.offsetLinesCacheW != p.width {
		sum := 0
		for i := 0; i < p.offsetIdx; i++ {
			sum += p.segmentAt(i).height(p.width)
		}
		p.offsetLinesCacheSum = sum
		p.offsetLinesCacheW = p.width
		p.offsetLinesCacheValid = true
	}
	return p.offsetLinesCacheSum + p.offsetLine
}

// ScrollPercent mirrors bubbles/viewport's: how far the current offset is
// through the total scrollable range.
func (p *transcriptPane) ScrollPercent() float64 {
	total := p.TotalHeight()
	if total <= p.height {
		return 1
	}
	maxOffset := total - p.height
	if maxOffset <= 0 {
		return 1
	}
	return float64(p.offsetLines()) / float64(maxOffset)
}

// ScrollbarThumb returns the [start, end) row range (within [0, height)) that
// a scrollbar thumb should occupy for the current scroll position, sized
// proportionally to the visible fraction of the total content. ok is false
// when everything fits without scrolling — callers should draw no thumb (and
// typically no track) in that case, mirroring the old title-bar indicator's
// "only shown when content overflows" behavior.
func (p *transcriptPane) ScrollbarThumb() (start, end int, ok bool) {
	total := p.TotalHeight()
	if total <= p.height || p.height <= 0 {
		return 0, 0, false
	}
	thumbH := max(1, p.height*p.height/total)
	thumbH = min(thumbH, p.height)
	track := p.height - thumbH
	maxOffset := total - p.height
	frac := 0.0
	if maxOffset > 0 {
		frac = float64(p.offsetLines()) / float64(maxOffset)
	}
	start = int(frac * float64(track))
	start = max(0, min(start, track))
	return start, start + thumbH, true
}

// VisibleLines splits the current View() into per-row strings (still
// ANSI-styled), used by mouse selection to hit-test and extract text without
// recomputing View() once per row.
func (p *transcriptPane) VisibleLines() []string {
	v := p.View()
	if v == "" {
		return nil
	}
	return strings.Split(v, "\n")
}

// GotoTop resets the scroll position to the very top, where by definition no
// segment lies above the offset — an exact, O(1) offsetLines cache reset,
// not just an invalidation.
func (p *transcriptPane) GotoTop() {
	p.offsetIdx = 0
	p.offsetLine = 0
	p.offsetLinesCacheSum = 0
	p.offsetLinesCacheW = p.width
	p.offsetLinesCacheValid = true
}

// GotoBottom scrolls to the end. Deliberately does NOT try to derive the
// offsetLines prefix sum here: doing so would need the height of every
// segment before the new offset, which — the first time it's asked for on a
// long transcript — means wrap-caching every one of them (see
// BenchmarkColdFirstRender), defeating View()'s O(visible) guarantee for
// callers that only ever call GotoBottom+View() and never touch the
// scrollbar (TestTranscriptPaneViewIsWindowed). So this just invalidates;
// offsetLines() recomputes lazily, only when something (ScrollbarThumb/
// ScrollPercent) actually asks for it.
func (p *transcriptPane) GotoBottom() {
	if p.segmentCount() == 0 {
		return
	}
	p.offsetIdx, p.offsetLine = p.lastOffsetSegment()
	p.offsetLinesCacheValid = false
}

// ScrollToItem scrolls so that the item recorded via Len() at index idx
// becomes the first visible segment (replaces the old renderUpTo +
// SetYOffset(strings.Count(prefix, "\n")) dance with an O(1) index set).
// This is a non-incremental jump (the timeline picker's scroll-to-turn), so
// it simply invalidates the offsetLines cache rather than trying to derive
// it — offsetLines() will pay one full recompute lazily on next access,
// which is fine for an infrequent, deliberate jump rather than a per-tick
// scroll.
func (p *transcriptPane) ScrollToItem(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx > len(p.items) {
		idx = len(p.items)
	}
	p.offsetIdx = idx + p.markerOffset()
	p.offsetLine = 0
	p.offsetLinesCacheValid = false
}

// ScrollBy scrolls the pane by the given number of lines; positive scrolls
// down, negative scrolls up (crush's ScrollBy, list.go:414-474).
//
// As offsetIdx steps across a segment boundary, the segment that was just
// crossed moves into (scrolling down) or out of (scrolling up) the "before
// offsetIdx" prefix that offsetLines() sums — so its height is added to or
// subtracted from offsetLinesCacheSum right here, using the height value
// this loop already computed for its own bookkeeping. That keeps the cache
// exactly in sync at zero extra asymptotic cost, including for
// single-line-tall segments where offsetIdx changes on every tick (see
// BenchmarkScrollTick_WithScrollbar_NoTrimCap).
func (p *transcriptPane) ScrollBy(lines int) {
	n := p.segmentCount()
	if n == 0 || lines == 0 {
		return
	}
	if lines > 0 {
		if p.AtBottom() {
			return
		}
		p.offsetLine += lines
		current := p.segmentAt(p.offsetIdx)
		for current != nil && p.offsetLine >= current.height(p.width) {
			h := current.height(p.width)
			p.offsetLine -= h
			if p.offsetLinesCacheValid && p.offsetLinesCacheW == p.width {
				p.offsetLinesCacheSum += h
			}
			p.offsetIdx++
			if p.offsetIdx > n-1 {
				p.GotoBottom()
				return
			}
			current = p.segmentAt(p.offsetIdx)
		}
		lastIdx, lastLine := p.lastOffsetSegment()
		if p.offsetIdx > lastIdx || (p.offsetIdx == lastIdx && p.offsetLine > lastLine) {
			// A jump-clamp, not a step — the incremental accounting above
			// doesn't apply to it, so fall back to invalidating.
			p.offsetIdx, p.offsetLine = lastIdx, lastLine
			p.offsetLinesCacheValid = false
		}
	} else {
		p.offsetLine += lines // lines is negative
		for p.offsetLine < 0 {
			p.offsetIdx--
			if p.offsetIdx < 0 {
				p.GotoTop()
				return
			}
			h := p.segmentAt(p.offsetIdx).height(p.width)
			if p.offsetLinesCacheValid && p.offsetLinesCacheW == p.width {
				p.offsetLinesCacheSum -= h
			}
			p.offsetLine += h
		}
	}
}

func (p *transcriptPane) ScrollDown(n int) { p.ScrollBy(n) }
func (p *transcriptPane) ScrollUp(n int)   { p.ScrollBy(-n) }

func (p *transcriptPane) PageDown() {
	if p.AtBottom() {
		return
	}
	p.ScrollDown(p.height)
}

func (p *transcriptPane) PageUp() {
	if p.AtTop() {
		return
	}
	p.ScrollUp(p.height)
}

func (p *transcriptPane) HalfPageDown() {
	if p.AtBottom() {
		return
	}
	p.ScrollDown(p.height / 2)
}

func (p *transcriptPane) HalfPageUp() {
	if p.AtTop() {
		return
	}
	p.ScrollUp(p.height / 2)
}

// ItemIndexAtY returns the segment at viewport-relative line y and the line
// offset within it, or (-1, -1) if y falls outside the visible range. Ported
// from crush's findItemAtY (list.go:880-908); not wired to any input
// handling yet — P16.5 will use this for mouse click/drag hit-testing — but
// exercised directly by tests now since it's cheap to get right while the
// windowing code is fresh.
func (p *transcriptPane) ItemIndexAtY(y int) (idx, lineWithin int) {
	if y < 0 || y >= p.height {
		return -1, -1
	}
	n := p.segmentCount()
	currentIdx := p.offsetIdx
	currentLine := -p.offsetLine
	for currentIdx < n && currentLine < p.height {
		seg := p.segmentAt(currentIdx)
		segEnd := currentLine + seg.height(p.width)
		if y >= currentLine && y < segEnd {
			return currentIdx, y - currentLine
		}
		currentLine = segEnd
		currentIdx++
	}
	return -1, -1
}

// View renders the visible window: it walks segments from offsetIdx/
// offsetLine, accumulating just enough wrapped content to fill height, then
// slices exactly the visible lines out of that (bounded) buffer. Cost is
// O(segments touching the viewport), not O(total transcript) — crush's
// list.go:517-587, generalized over segments and adapted to reuse each
// item's existing whole-string wrap cache (rendered) instead of a per-item
// line-slice cache, since every item's raw content is documented to end on a
// line boundary (see transcriptItem's doc comment) and concatenating whole
// wrapped strings is exactly what the old flat-transcript render did —
// bounding it to a small window preserves that proven-correct behavior
// instead of reassembling from split line arrays, which would double-count
// or drop item-boundary blank lines depending on trailing-newline count.
func (p *transcriptPane) View() string {
	budget := max(p.height, 0)
	n := p.segmentCount()
	if budget == 0 || n == 0 {
		return ""
	}

	var buf strings.Builder
	linesBuffered := 0
	idx := p.offsetIdx
	for idx < n && linesBuffered-p.offsetLine < budget {
		seg := p.segmentAt(idx)
		buf.WriteString(seg.rendered(p.width))
		linesBuffered += seg.height(p.width)
		idx++
	}
	if linesBuffered <= p.offsetLine {
		return ""
	}

	all := strings.Split(buf.String(), "\n")
	// Every segment's wrapped output ends in "\n" (documented invariant), so
	// the element after the last counted newline is always the empty
	// artifact of that trailing terminator, not a real line — drop it.
	content := all
	if len(content) > linesBuffered {
		content = content[:linesBuffered]
	}
	start := min(p.offsetLine, len(content))
	end := min(start+budget, len(content))
	return strings.Join(content[start:end], "\n")
}

// HandleKey reproduces bubbles/viewport's DefaultKeyMap scroll bindings
// (viewport/keymap.go:23-58): pgup/b, pgdown/space/f, u/ctrl+u, d/ctrl+d,
// up/k, down/j. Left/right/horizontal scrolling are dropped — SoftWrap
// already made horizontal scrolling dead code for this pane. Returns true if
// the key was consumed as a scroll command (informational only; callers keep
// forwarding every key to the textarea regardless, matching today's
// behavior — see the "known existing quirk" note in transcript.go's package
// docs / research/roadmap.md P16.4 plan notes).
func (p *transcriptPane) HandleKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "pgdown", "space", "f":
		p.PageDown()
	case "pgup", "b":
		p.PageUp()
	case "u", "ctrl+u":
		p.HalfPageUp()
	case "d", "ctrl+d":
		p.HalfPageDown()
	case "down", "j":
		p.ScrollDown(1)
	case "up", "k":
		p.ScrollUp(1)
	default:
		return false
	}
	return true
}

// HandleMouseWheel reproduces bubbles/viewport's mouse-wheel handling
// (viewport.go:696-722) at its default MouseWheelDelta of 3.
func (p *transcriptPane) HandleMouseWheel(msg tea.MouseWheelMsg) bool {
	switch msg.Button {
	case tea.MouseWheelDown:
		p.ScrollDown(3)
	case tea.MouseWheelUp:
		p.ScrollUp(3)
	default:
		return false
	}
	return true
}

// liveBlock renders actively-streaming text. Unlike transcriptItem it keeps
// a boundary-caching trick: only the "settled" prefix up to the last safe
// markdown paragraph break is cached, and the short growing suffix is
// re-rendered every token. This keeps a long streaming reply O(tail) per
// token instead of O(n), the same guarantee the old model-level liveWrapCache
// fields gave, just encapsulated per-block instead of scattered on model.
//
// Rendering goes through md, the caller-supplied markdown renderer (TQ3), so
// streamed text is styled identically to the flushed transcript block and the
// old end-of-turn restyle "pop" no longer happens. The settled prefix always
// ends at a blank line outside a code fence, so rendering it separately from
// the tail produces the same output as one whole-source render.
type liveBlock struct {
	raw string

	prefixCache   string
	prefixCacheTo int
	prefixCacheW  int
}

func (b *liveBlock) setText(s string) { b.raw = s }

func (b *liveBlock) render(w int, md func(string) string) string {
	if b.raw == "" {
		return ""
	}
	boundary := findSafeMarkdownBoundary(b.raw)
	if boundary != b.prefixCacheTo || w != b.prefixCacheW {
		if boundary > 0 {
			b.prefixCache = md(b.raw[:boundary])
		} else {
			b.prefixCache = ""
		}
		b.prefixCacheTo = boundary
		b.prefixCacheW = w
	}
	return b.prefixCache + md(b.raw[boundary:])
}

func (b *liveBlock) reset() {
	b.raw = ""
	b.prefixCache, b.prefixCacheTo, b.prefixCacheW = "", 0, 0
}
