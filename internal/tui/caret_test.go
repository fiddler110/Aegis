package tui

import (
	"strings"
	"testing"
)

// TestStreamingCaretBlinksAtWriteHead is the P21.3 regression guard: once
// real text has started streaming, a blinking caret glyph must appear at the
// live write-head, driven by the existing animStep tick (no new ticker) —
// present on the "on" half of the blink cycle, absent on the "off" half.
func TestStreamingCaretBlinksAtWriteHead(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("hi", nil)
	m.streamState.streaming = true
	m.streamState.followBottom = true
	m.streamState.liveText.WriteString("hello world")

	m.streamState.animStep = 0 // blink-on frame
	m.refresh()
	on := plainView(m)
	if !strings.Contains(on, caretChar) {
		t.Fatalf("expected caret glyph present on blink-on frame, got:\n%s", on)
	}

	m.streamState.animStep = caretBlinkPeriod / 2 // blink-off frame
	m.refresh()
	off := plainView(m)
	if strings.Contains(off, caretChar) {
		t.Fatalf("expected caret glyph absent on blink-off frame, got:\n%s", off)
	}
}

// TestStreamingCaretAbsentWhenNotStreaming covers the edge case of leftover
// live text with streaming already cleared (e.g. mid-teardown): the caret
// must not render once m.streamState.streaming is false, regardless of live text.
func TestStreamingCaretAbsentWhenNotStreaming(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("hi", nil)
	m.streamState.streaming = false
	m.streamState.followBottom = true
	m.streamState.liveText.WriteString("hello world")

	m.streamState.animStep = 0 // would be a blink-on frame if streaming were true
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, caretChar) {
		t.Fatalf("expected no caret glyph when not streaming, got:\n%s", got)
	}
}

// TestStreamingCaretAbsentDuringThinkingPhase covers the pre-text "thinking"
// phase: m.streamState.streaming is true but no tokens have arrived yet (liveText is
// empty), so refresh() takes the shimmer-phrase branch, not the live-tail
// branch — the caret belongs only to the latter.
func TestStreamingCaretAbsentDuringThinkingPhase(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("hi", nil)
	m.streamState.streaming = true
	m.streamState.followBottom = true
	// liveText intentionally left empty.

	m.streamState.animStep = 0
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, caretChar) {
		t.Fatalf("expected no caret glyph during pure thinking phase (no live text yet), got:\n%s", got)
	}
}

// TestStreamingCaretNotPersistedAfterFlush ensures the caret is purely a
// tail-rendering artifact: once a turn's live text flushes into a real
// transcript item, the caret glyph must never have been baked into it.
func TestStreamingCaretNotPersistedAfterFlush(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("hi", nil)
	m.streamState.streaming = true
	m.streamState.followBottom = true
	m.streamState.liveText.WriteString("hello world")
	m.streamState.animStep = 0
	m.refresh()
	if !strings.Contains(plainView(m), caretChar) {
		t.Fatal("expected caret present while streaming, precondition for this test failed")
	}

	m.flushLiveText()
	m.streamState.streaming = false
	m.refresh()

	got := plainView(m)
	if strings.Contains(got, caretChar) {
		t.Fatalf("expected caret glyph gone from persisted transcript after flush, got:\n%s", got)
	}
}
