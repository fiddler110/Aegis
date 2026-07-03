package tui

import (
	"strings"
	"testing"
)

func TestTranscriptAppendAndRender(t *testing.T) {
	var tr transcript
	// Each block ends on its own line — the invariant documented on
	// transcript.append — so per-block width padding never merges into the
	// next block's content.
	tr.append("hello\n")
	tr.append("world\n")

	got := tr.render(80)
	helloAt := strings.Index(got, "hello")
	worldAt := strings.Index(got, "world")
	if helloAt < 0 || worldAt < 0 || helloAt > worldAt {
		t.Fatalf("expected %q then %q in order, got %q", "hello", "world", got)
	}
	if tr.len() != 2 {
		t.Fatalf("got %d blocks, want 2", tr.len())
	}
}

func TestTranscriptAppendEmptyIsNoop(t *testing.T) {
	var tr transcript
	tr.append("")
	if tr.len() != 0 {
		t.Fatalf("expected empty append to be a no-op, got %d blocks", tr.len())
	}
}

func TestTranscriptReset(t *testing.T) {
	var tr transcript
	tr.append("some content")
	tr.reset()
	if tr.len() != 0 || tr.rawBytes != 0 || tr.render(80) != "" {
		t.Fatalf("expected reset transcript to be empty, got len=%d rawBytes=%d render=%q",
			tr.len(), tr.rawBytes, tr.render(80))
	}
}

func TestTranscriptRenderCachesPerBlockWidth(t *testing.T) {
	var tr transcript
	tr.append("first block content that is reasonably long for wrapping purposes")

	out80 := tr.blocks[0].render(80)
	if !tr.blocks[0].cached || tr.blocks[0].cacheW != 80 {
		t.Fatalf("expected block to cache at width 80")
	}
	// Re-rendering at the same width must reuse the cache (same output, no panic/mutation issue).
	if again := tr.blocks[0].render(80); again != out80 {
		t.Fatalf("expected identical cached output, got %q vs %q", again, out80)
	}
	// A different width must invalidate and rewrap.
	out40 := tr.blocks[0].render(40)
	if tr.blocks[0].cacheW != 40 {
		t.Fatalf("expected cache to track the new width 40, got %d", tr.blocks[0].cacheW)
	}
	if out40 == out80 && len(out80) > 40 {
		t.Fatalf("expected rewrap at a narrower width to change output")
	}
}

func TestTranscriptTrimDropsWholeBlocksNotMidLine(t *testing.T) {
	var tr transcript
	// Push well past the budget with many distinct blocks, each terminated by
	// a newline, so we can assert no block is ever cut mid-content.
	block := strings.Repeat("x", 1024) + "\n"
	blocksNeeded := (maxTranscriptBytes / len(block)) + 10
	for i := 0; i < blocksNeeded; i++ {
		tr.append(block)
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
	// Every remaining block must be byte-identical to the original unit —
	// i.e. trimming dropped whole blocks, never sliced one.
	for _, b := range tr.blocks {
		if b.raw != block {
			t.Fatalf("expected an untouched whole block, got %q", b.raw)
		}
	}
}

func TestTranscriptTrimMarkerInsertedOnce(t *testing.T) {
	var tr transcript
	block := strings.Repeat("y", 1024) + "\n"
	blocksNeeded := (maxTranscriptBytes / len(block)) + 10
	for i := 0; i < blocksNeeded; i++ {
		tr.append(block)
	}
	if tr.marker == nil {
		t.Fatal("expected a trimmed marker after repeated trims")
	}
	// The marker itself is never part of the evictable blocks slice, so it
	// can't be duplicated or dropped by a later trim pass.
	for _, b := range tr.blocks {
		if b.raw == trimmedMarker {
			t.Fatal("the trimmed marker must not appear inside the evictable blocks slice")
		}
	}
}

func TestTranscriptRenderUpTo(t *testing.T) {
	var tr transcript
	tr.append("a\n")
	tr.append("b\n")
	tr.append("c\n")

	got2 := tr.renderUpTo(2, 80)
	if strings.Contains(got2, "c") {
		t.Fatalf("renderUpTo(2, ...) must not include block 3's content, got %q", got2)
	}
	if !strings.Contains(got2, "a") || !strings.Contains(got2, "b") {
		t.Fatalf("renderUpTo(2, ...) missing expected content, got %q", got2)
	}
	if got := tr.renderUpTo(0, 80); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	// n beyond the block count clamps to everything.
	got99 := tr.renderUpTo(99, 80)
	if !strings.Contains(got99, "a") || !strings.Contains(got99, "b") || !strings.Contains(got99, "c") {
		t.Fatalf("renderUpTo clamped beyond block count missing content, got %q", got99)
	}
}

func TestLiveBlockRenderAndReset(t *testing.T) {
	lb := &liveBlock{}
	if got := lb.render(80); got != "" {
		t.Fatalf("empty live block should render empty, got %q", got)
	}

	lb.setText("hello")
	if got := lb.render(80); !strings.Contains(got, "hello") {
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
	first := lb.render(80)
	cachedPrefixLen := len(lb.prefixCache)

	// Growing the tail (without crossing a new blank-line boundary) must not
	// shrink the cached settled prefix.
	lb.setText("paragraph one.\n\nparagraph two continues here")
	second := lb.render(80)
	if len(lb.prefixCache) != cachedPrefixLen {
		t.Fatalf("expected the settled prefix cache to remain stable, got len %d vs %d", len(lb.prefixCache), cachedPrefixLen)
	}
	if first == second {
		t.Fatalf("expected rendered output to reflect the grown tail")
	}
}
