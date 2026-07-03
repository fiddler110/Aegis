package tui

import "strings"

// maxTranscriptBytes bounds the transcript's total raw (pre-wrap) size.
// Trimming drops whole blocks from the front rather than severing content
// mid-line (TQ1) — the previous cappedBuffer implementation trimmed at the
// byte level and could truncate inside a styled ANSI run.
const maxTranscriptBytes = 1 << 20

const trimmedMarker = "[earlier output trimmed]\n\n"

// transcriptBlock is one independently-wrapped unit of the conversation: a
// user turn, an assistant reply, a tool call, a tool result, a system
// notice, and so on. raw is pre-styled ANSI content (the existing renderX
// helpers already produce this); render() lazily word-wraps it to the
// current viewport width and caches the result until the width changes.
//
// Per-block caching is what makes resize, theme change, and future
// retroactive re-render (e.g. /tools full applying to history) cheap: only
// the blocks whose wrapped output is stale get redone, instead of the whole
// transcript.
type transcriptBlock struct {
	raw string

	cacheW   int
	cacheOut string
	cached   bool
}

func newBlock(raw string) *transcriptBlock { return &transcriptBlock{raw: raw} }

func (b *transcriptBlock) render(w int) string {
	if !b.cached || b.cacheW != w {
		b.cacheOut = wrap(b.raw, w)
		b.cacheW = w
		b.cached = true
	}
	return b.cacheOut
}

// transcript is an ordered, size-capped list of blocks. It replaces the old
// cappedBuffer + whole-buffer wrapCache pair: appends are O(1), a resize or
// theme change invalidates lazily (each block notices its cached width no
// longer matches on next render), and trimming never cuts a block in half.
//
// Callers must end each appended block's raw content on a line boundary
// (trailing "\n" or "\n\n") unless it is the very last thing appended before
// the next block starts a new visual line anyway. Blocks are wrapped
// independently and then concatenated, so a block left mid-line would have
// its continuation padded onto its own line instead of joining the next
// block's text — every call site in this package already writes complete
// lines, which is what makes this safe.
type transcript struct {
	blocks   []*transcriptBlock
	rawBytes int

	// trimmed is true once old blocks have been evicted. The marker block
	// lives outside blocks so trim() can never evict it in a later pass.
	trimmed bool
	marker  *transcriptBlock
}

// append adds one rendered block. Empty strings are ignored (mirrors the old
// WriteString("") no-op behavior).
func (t *transcript) append(raw string) {
	if raw == "" {
		return
	}
	t.blocks = append(t.blocks, newBlock(raw))
	t.rawBytes += len(raw)
	t.trim()
}

// trim drops the oldest blocks until the transcript is back under budget.
// The first time this happens, a "[earlier output trimmed]" marker is
// recorded (rendered ahead of the remaining blocks) — it is never itself
// subject to eviction, unlike the old cappedBuffer's inline byte-trim.
func (t *transcript) trim() {
	for t.rawBytes > maxTranscriptBytes && len(t.blocks) > 1 {
		t.rawBytes -= len(t.blocks[0].raw)
		t.blocks = t.blocks[1:]
		t.trimmed = true
	}
	if t.trimmed && t.marker == nil {
		t.marker = newBlock(trimmedMarker)
	}
}

// reset clears the transcript (session switch, /clear).
func (t *transcript) reset() {
	t.blocks = nil
	t.rawBytes = 0
	t.trimmed = false
	t.marker = nil
}

// len returns the current block count, used as a stable position marker
// (e.g. the timeline picker's scroll-to-turn) instead of a byte offset into
// a flat buffer.
func (t *transcript) len() int { return len(t.blocks) }

// render joins the trim marker (if any) and every block's wrapped output for
// width w.
func (t *transcript) render(w int) string {
	return t.renderUpTo(len(t.blocks), w)
}

// renderUpTo joins the trim marker (if any) and the first n blocks' wrapped
// output for width w, used to compute a scroll position for a turn recorded
// at block index n.
func (t *transcript) renderUpTo(n, w int) string {
	if n > len(t.blocks) {
		n = len(t.blocks)
	}
	if n == 0 && t.marker == nil {
		return ""
	}
	var b strings.Builder
	if t.marker != nil {
		b.WriteString(t.marker.render(w))
	}
	for _, blk := range t.blocks[:n] {
		b.WriteString(blk.render(w))
	}
	return b.String()
}

// liveBlock renders actively-streaming text. Unlike transcriptBlock it keeps
// a boundary-caching trick: only the "settled" prefix up to the last safe
// markdown paragraph break is cached, and the short growing suffix is
// rewrapped every token. This keeps a long streaming reply O(tail) per token
// instead of O(n), the same guarantee the old model-level liveWrapCache
// fields gave, just encapsulated per-block instead of scattered on model.
type liveBlock struct {
	raw string

	prefixCache   string
	prefixCacheTo int
	prefixCacheW  int
}

func (b *liveBlock) setText(s string) { b.raw = s }

func (b *liveBlock) render(w int) string {
	if b.raw == "" {
		return ""
	}
	boundary := findSafeMarkdownBoundary(b.raw)
	if boundary > b.prefixCacheTo || w != b.prefixCacheW {
		if boundary > 0 {
			b.prefixCache = wrap(b.raw[:boundary], w)
		} else {
			b.prefixCache = ""
		}
		b.prefixCacheTo = boundary
		b.prefixCacheW = w
	}
	return b.prefixCache + wrap(b.raw[boundary:], w)
}

func (b *liveBlock) reset() {
	b.raw = ""
	b.prefixCache, b.prefixCacheTo, b.prefixCacheW = "", 0, 0
}
