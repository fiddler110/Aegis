package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// benchTranscript builds a pane with n realistic multi-line items (a mix of
// short user turns and longer assistant paragraphs, matching typical
// transcript content) at a typical terminal width/height, scrolled to the
// middle of the history — mimicking a user who has scrolled up to read past
// output, the scenario P18.2 complains about.
//
// It also warms every render cache once (View + ScrollbarThumb), just as a
// real session would have already painted a frame before the user starts
// scrolling. Without this warmup, the very first call to TotalHeight() below
// pays a one-time cost of wrapping every not-yet-rendered item in the whole
// transcript, which swamps a b.N-averaged benchmark and looks like a
// per-tick cost when it isn't one — that one-time cost is real (see
// BenchmarkColdFirstRender) but it is not what "scroll feels janky" is about.
func benchTranscript(n int) *transcriptPane {
	p := newTranscriptPane(100, 40)
	short := "user: what's the status of the deploy?\n\n"
	long := strings.Repeat("This is a streamed assistant paragraph with enough words to wrap across "+
		"several terminal lines once rendered, simulating a realistic reply body. ", 8) + "\n\n"
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			p.Append(short)
		} else {
			p.Append(long)
		}
	}
	p.ScrollToItem(n / 2)
	_ = p.View()
	_, _, _ = p.ScrollbarThumb()
	return p
}

// BenchmarkColdFirstRender measures the one-time cost of the very first
// TotalHeight() computation over a never-before-rendered transcript (every
// item's wrap() pays its cost once). This happens on session load / history
// replay, not on a per-scroll-tick basis, so it is not itself candidate (a)
// — but it is a genuine O(n) cost worth recording since it will recur
// whenever an untouched item enters the sum (e.g. right after a resize
// invalidates every item's cache).
func BenchmarkColdFirstRender(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				p := newTranscriptPane(100, 40)
				for j := 0; j < n; j++ {
					p.Append("line of transcript content that wraps to a couple of terminal rows\n\n")
				}
				b.StartTimer()
				_, _, _ = p.ScrollbarThumb()
			}
		})
	}
}

// BenchmarkScrollTick_ViewOnly simulates one HandleMouseWheel + View() pass
// per bubbletea message with no concurrent content change (idle scrolling,
// not mid-stream) — the baseline "scroll-only" path candidate (a) describes,
// isolated from the scrollbar. Per-item wrap output is cached by width
// (transcriptItem.rendered) and View() only walks the visible window
// (P16.4), so this is expected to stay flat as history grows.
func BenchmarkScrollTick_ViewOnly(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			p := benchTranscript(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				} else {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
				}
				_ = p.View()
			}
		})
	}
}

// BenchmarkScrollTick_WithScrollbar reproduces what happens once per
// bubbletea message: a scroll tick (HandleMouseWheel) followed by the full
// frame render, which includes renderScrollbar -> ScrollbarThumb ->
// offsetLines. Before the P18.2 fix, offsetLines() re-summed every segment
// from the top of the transcript down to the current scroll offset on every
// single call — a cost that grew with how far the user had scrolled into
// history (see BenchmarkScrollTick_WithScrollbar_NoTrimCap, which isolates
// that scaling unambiguously). offsetLines() is now maintained incrementally
// by ScrollBy/GotoTop/GotoBottom, so this should stay flat as n grows.
func BenchmarkScrollTick_WithScrollbar(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			p := benchTranscript(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				} else {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
				}
				_ = p.View()
				_, _, _ = p.ScrollbarThumb()
			}
		})
	}
}

// BenchmarkScrollTick_DuringStreaming is the realistic worst case the P18.2
// complaint describes: the user scrolls while a reply is actively streaming,
// so SetTail changes on effectively every rendered frame (every token).
// Before the fix, that invalidated TotalHeight's single combined cache on
// every call, forcing a full items-plus-tail resum each time. TotalHeight is
// now itemsHeight (invalidated only by real content/width changes) plus
// tailHeight (the tail's own independent, already-cheap per-item cache), so
// a growing tail no longer touches the items sum at all.
func BenchmarkScrollTick_DuringStreaming(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			p := benchTranscript(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// A streamed token growing the live tail by a few bytes —
				// changes tailRaw every call, exactly like refresh() does
				// while m.streamState.liveText is growing.
				p.SetTail(fmt.Sprintf("● thinking… (%d bytes streamed so far, this is the live tail text growing token by token)\n", i))
				if i%2 == 0 {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				} else {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
				}
				_ = p.View()
				_, _, _ = p.ScrollbarThumb()
			}
		})
	}
}

// tinyBenchTranscript uses minimal-byte items ("x\n", one line each) so item
// count isn't capped by the 1MB trim budget (maxTranscriptBytes) at large n
// the way benchTranscript's realistic mix is — isolating pure scroll-depth
// scaling from that unrelated eviction behavior. Single-line segments are
// also the pathological case for offsetLines()'s incremental cache: offsetIdx
// changes on every scroll tick (no multi-line segment to absorb offsetLine
// deltas), so a plain memo keyed on offsetIdx would miss every time. The
// actual fix maintains the running sum inside ScrollBy itself, which handles
// this case too — see BenchmarkScrollTick_WithScrollbar_NoTrimCap below.
func tinyBenchTranscript(n int) *transcriptPane {
	p := newTranscriptPane(100, 40)
	for i := 0; i < n; i++ {
		p.Append("x\n")
	}
	p.ScrollToItem(n / 2)
	_ = p.View()
	_, _, _ = p.ScrollbarThumb()
	return p
}

// BenchmarkScrollTick_WithScrollbar_NoTrimCap is the benchmark that most
// directly confirmed P18.2's candidate (a): before the fix this scaled
// linearly with item count (21µs @ 1000 items -> 336µs @ 200000 items, a
// ~16x cost growth over a 200x item-count range) because offsetLines() (via
// ScrollbarThumb, called on every render) walked from segment 0 to offsetIdx
// on every call. After the fix it should stay flat regardless of n.
func BenchmarkScrollTick_WithScrollbar_NoTrimCap(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000, 200000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			p := tinyBenchTranscript(n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				} else {
					p.HandleMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
				}
				_ = p.View()
				_, _, _ = p.ScrollbarThumb()
			}
		})
	}
}
