package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/fiddler110/aegis/internal/api"
)

// sseWriter serializes api.Event values as SSE frames onto w through a
// bounded queue, decoupling the producer (the engine's own goroutine, via
// send) from how fast the HTTP client actually reads (P21.5). This keeps a
// single connection's queued-but-unflushed event count fixed regardless of
// how far behind a slow or stalled SSE consumer (TUI, web UI, or an
// mcp-serve client) falls: send never blocks the run and the queue never
// grows past its configured depth.
//
// What overflow costs depends on the event (P66.20/PERF-08). For a tool_call
// or a turn_done, recency wins and the oldest queued event is dropped — the
// client is behind, and the newest state is the one worth delivering. A `text`
// delta is not like that: dropping one leaves a silent hole in the middle of
// the model's answer that no client can detect, let alone repair. So text
// deltas are *folded* instead — an overflowing delta is appended to the text
// already queued ahead of it, which costs a frame boundary (invisible to a
// reader, since a client concatenates deltas anyway) rather than the words.
// Only when there is no text to fold does a queue at depth drop an event, and
// it drops the oldest non-text one first.
type sseWriter struct {
	mu     sync.Mutex
	cond   *sync.Cond
	queue  []api.Event
	max    int
	closed bool

	done    chan struct{}
	writeMu *sync.Mutex
	w       io.Writer
	flusher http.Flusher
	onDrop  func()
}

// newSSEWriter starts the background goroutine that drains the queue onto w
// and returns immediately. writeMu must be the same mutex used for any other
// direct writes to w (e.g. a heartbeat) so frames never interleave.
func newSSEWriter(w io.Writer, flusher http.Flusher, writeMu *sync.Mutex, bufSize int, onDrop func()) *sseWriter {
	if bufSize <= 0 {
		bufSize = 1
	}
	sw := &sseWriter{
		max:     bufSize,
		done:    make(chan struct{}),
		writeMu: writeMu,
		w:       w,
		flusher: flusher,
		onDrop:  onDrop,
	}
	sw.cond = sync.NewCond(&sw.mu)
	go sw.run()
	return sw
}

func (sw *sseWriter) run() {
	defer close(sw.done)
	for {
		sw.mu.Lock()
		for len(sw.queue) == 0 && !sw.closed {
			sw.cond.Wait()
		}
		if len(sw.queue) == 0 {
			sw.mu.Unlock()
			return
		}
		ev := sw.queue[0]
		copy(sw.queue, sw.queue[1:])
		sw.queue = sw.queue[:len(sw.queue)-1]
		sw.mu.Unlock()

		data, _ := json.Marshal(ev)
		sw.writeMu.Lock()
		fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", ev.Kind, data)
		sw.flusher.Flush()
		sw.writeMu.Unlock()
	}
}

// foldable reports whether ev is a plain text delta — the only event kind that
// can be concatenated with its neighbour without losing anything. toAPIEvent
// sets nothing but Kind and Text on a KindText event; the rest of the guard is
// here so a future field on one can never be silently swallowed by a fold.
func foldable(ev api.Event) bool {
	return ev.Kind == api.KindText && ev.Text != "" &&
		ev.Tool == "" && ev.ToolID == "" && ev.ToolResult == "" && ev.Error == "" &&
		ev.ToolInput == nil && ev.ToolPresentation == nil &&
		ev.InputTokens == 0 && ev.OutputTokens == 0 && ev.CostUSD == 0 && ev.EgressBytes == 0
}

// makeRoomLocked frees one queue slot for ev. It first tries to fold ev into
// the text already at the tail, then to fold an adjacent pair of queued text
// deltas together; both preserve the byte stream and its order exactly.
// Returns folded when ev was absorbed (the caller must not enqueue it) and
// dropped when an event was discarded instead. Callers must hold sw.mu.
func (sw *sseWriter) makeRoomLocked(ev api.Event) (folded, dropped bool) {
	last := len(sw.queue) - 1
	if foldable(ev) && foldable(sw.queue[last]) {
		sw.queue[last].Text += ev.Text
		return true, false
	}
	for i := 0; i < last; i++ {
		if foldable(sw.queue[i]) && foldable(sw.queue[i+1]) {
			sw.queue[i].Text += sw.queue[i+1].Text
			copy(sw.queue[i+1:], sw.queue[i+2:])
			sw.queue = sw.queue[:len(sw.queue)-1]
			return false, false
		}
	}
	// Nothing to fold — a queue alternating text with tool calls reaches this
	// even though it holds text. Drop the oldest event that is *not* a text
	// delta first: for a tool_call or a turn_done the newest is the one worth
	// having (P21.5's recency rule), and spending it here is what keeps the
	// token stream whole. Only a queue too short to hold two adjacent deltas
	// (max == 1) falls through to dropping text.
	victim := 0
	for i, queued := range sw.queue {
		if !foldable(queued) {
			victim = i
			break
		}
	}
	copy(sw.queue[victim:], sw.queue[victim+1:])
	sw.queue = sw.queue[:len(sw.queue)-1]
	return false, true
}

// send enqueues ev without blocking, folding or dropping as makeRoomLocked
// decides when the queue is already at depth. onDrop (if set) fires only for a
// real drop — a fold loses nothing and is not worth warning about.
func (sw *sseWriter) send(ev api.Event) {
	sw.mu.Lock()
	if sw.closed {
		sw.mu.Unlock()
		return
	}
	var folded, dropped bool
	if len(sw.queue) >= sw.max {
		folded, dropped = sw.makeRoomLocked(ev)
	}
	if !folded {
		sw.queue = append(sw.queue, ev)
	}
	sw.cond.Signal()
	sw.mu.Unlock()

	if dropped && sw.onDrop != nil {
		sw.onDrop()
	}
}

// queueLen reports how many events are queued but not yet written. Test hook
// for the boundedness assertion; production code never needs it.
func (sw *sseWriter) queueLen() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return len(sw.queue)
}

// Close stops accepting further sends and blocks until the writer goroutine
// has flushed everything still queued, so a caller can rely on every send
// before Close having at least been attempted before Close returns. Idempotent.
func (sw *sseWriter) Close() {
	sw.mu.Lock()
	if !sw.closed {
		sw.closed = true
		sw.cond.Broadcast()
	}
	sw.mu.Unlock()
	<-sw.done
}
