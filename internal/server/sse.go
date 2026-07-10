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
// mcp-serve client) falls: once the queue is full, send drops the oldest
// queued event to make room for the newest rather than growing without
// bound or blocking the caller.
type sseWriter struct {
	ch      chan api.Event
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
	sw := &sseWriter{
		ch:      make(chan api.Event, bufSize),
		done:    make(chan struct{}),
		writeMu: writeMu,
		w:       w,
		flusher: flusher,
		onDrop:  onDrop,
	}
	go sw.run()
	return sw
}

func (sw *sseWriter) run() {
	defer close(sw.done)
	for ev := range sw.ch {
		data, _ := json.Marshal(ev)
		sw.writeMu.Lock()
		fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", ev.Kind, data)
		sw.flusher.Flush()
		sw.writeMu.Unlock()
	}
}

// send enqueues ev without blocking. If the queue is already full, the
// oldest queued event is dropped to make room and onDrop (if set) fires.
func (sw *sseWriter) send(ev api.Event) {
	select {
	case sw.ch <- ev:
		return
	default:
	}
	select {
	case <-sw.ch:
	default:
	}
	select {
	case sw.ch <- ev:
	default:
	}
	if sw.onDrop != nil {
		sw.onDrop()
	}
}

// Close stops accepting further sends and blocks until the writer goroutine
// has flushed everything still queued, so a caller can rely on every send
// before Close having at least been attempted before Close returns.
func (sw *sseWriter) Close() {
	close(sw.ch)
	<-sw.done
}
