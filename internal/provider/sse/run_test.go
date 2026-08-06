package sse

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// testDecoder is a minimal wire format standing in for the three real ones:
// a line is text unless it is one of the sentinels below. It exercises every
// Status without dragging any adapter's JSON into this package's tests.
type testDecoder struct {
	stop     provider.StopReason
	usage    *provider.Usage
	flush    Status // what Flush reports, for the flushDecoder below
	finishOK bool
	finished bool
}

func newTestDecoder() *testDecoder {
	return &testDecoder{stop: provider.StopEndTurn, usage: &provider.Usage{}, finishOK: true}
}

func (d *testDecoder) Line(line string, emit func(provider.Event) bool) Status {
	switch line {
	case "":
		return StatusContinue
	case "TERMINAL":
		return StatusTerminal
	case "END":
		return StatusEnd
	case "ABORT":
		emit(provider.Event{Type: provider.EventError, Err: errors.New("decoder said so")})
		return StatusAbort
	}
	if !emit(provider.Event{Type: provider.EventTextDelta, Text: line}) {
		return StatusAbort
	}
	return StatusContinue
}

func (d *testDecoder) Finish(emit func(provider.Event) bool) (provider.StopReason, *provider.Usage, bool) {
	d.finished = true
	if !d.finishOK {
		emit(provider.Event{Type: provider.EventError, Err: errors.New("finish failed")})
	}
	return d.stop, d.usage, d.finishOK
}

// flushDecoder adds sse.Flusher, the way the anthropic adapter does.
type flushDecoder struct {
	*testDecoder
	flushed bool
}

func (d *flushDecoder) Flush(func(provider.Event) bool) Status {
	d.flushed = true
	return d.flush
}

// closeCounter is the response body: a reader plus the Close accounting Run's
// lifecycle ownership depends on (the idle watchdog closes it to break a
// blocked read, and Run closes it on the way out).
type closeCounter struct {
	mu     sync.Mutex
	r      io.Reader
	closed bool
}

func (c *closeCounter) Read(p []byte) (int, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, errors.New("http: read on closed response body")
	}
	return c.r.Read(p)
}

func (c *closeCounter) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *closeCounter) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// runCollect drives Run to completion and returns everything it emitted. The
// channel closing is itself part of the contract — a caller ranges over it.
func runCollect(t *testing.T, ctx context.Context, body io.ReadCloser, opts Options, dec Decoder) []provider.Event {
	t.Helper()
	out := make(chan provider.Event)
	go Run(ctx, body, out, opts, dec)
	var got []provider.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range out {
			got = append(got, ev)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not finish (or never closed the output channel)")
	}
	return got
}

func stringBody(s string) *closeCounter { return &closeCounter{r: strings.NewReader(s)} }

func lastEvent(t *testing.T, evs []provider.Event) provider.Event {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("no events emitted")
	}
	return evs[len(evs)-1]
}

// TestRun_TerminalChunkPresent: the ordinary path. A stream that states it
// finished ends in EventDone carrying the decoder's stop reason and usage.
func TestRun_TerminalChunkPresent(t *testing.T) {
	dec := newTestDecoder()
	dec.usage.OutputTokens = 7
	body := stringBody("hello\nTERMINAL\n")
	evs := runCollect(t, context.Background(), body, Options{Provider: "test", MissingTerminal: "no terminator"}, dec)

	if len(evs) != 2 || evs[0].Type != provider.EventTextDelta {
		t.Fatalf("events = %+v, want a text delta then done", evs)
	}
	done := lastEvent(t, evs)
	if done.Type != provider.EventDone || done.Stop != provider.StopEndTurn || done.Usage.OutputTokens != 7 {
		t.Errorf("final event = %+v, want EventDone with the decoder's stop/usage", done)
	}
	if !dec.finished {
		t.Error("Finish was not called on a cleanly terminated stream")
	}
	if !body.isClosed() {
		t.Error("Run did not close the response body")
	}
}

// TestRun_TerminalChunkAbsent is P61.2 lifted into the skeleton: a stream that
// ends cleanly without a terminator is a truncated generation, not a finished
// turn, and must surface as a transport error so IsBackendUnavailableError
// routes it into waitForBackend/resume-from-disk instead of being reported as
// a complete (short) answer.
func TestRun_TerminalChunkAbsent(t *testing.T) {
	dec := newTestDecoder()
	evs := runCollect(t, context.Background(), stringBody("hello\n"),
		Options{Provider: "test", MissingTerminal: "read stream: the response ended without a terminator"}, dec)

	last := lastEvent(t, evs)
	if last.Type != provider.EventError {
		t.Fatalf("final event = %+v, want EventError", last)
	}
	if !provider.IsBackendUnavailableError(last.Err) {
		t.Errorf("err = %v, want a transport error IsBackendUnavailableError classifies", last.Err)
	}
	if !strings.Contains(last.Err.Error(), "without a terminator") {
		t.Errorf("err = %v, want Options.MissingTerminal's message", last.Err)
	}
	if dec.finished {
		t.Error("Finish ran on a truncated stream; accumulated fragments must not be flushed as if whole")
	}
	for _, ev := range evs {
		if ev.Type == provider.EventDone {
			t.Error("EventDone emitted for a stream that never terminated")
		}
	}
}

// TestRun_TerminalRequirementOptional: an empty MissingTerminal disables the
// requirement rather than failing every stream.
func TestRun_TerminalRequirementOptional(t *testing.T) {
	dec := newTestDecoder()
	evs := runCollect(t, context.Background(), stringBody("hello\n"), Options{Provider: "test"}, dec)
	if last := lastEvent(t, evs); last.Type != provider.EventDone {
		t.Errorf("final event = %+v, want EventDone when no terminator is required", last)
	}
}

// TestRun_StatusEndStopsReading: only the OpenAI "[DONE]" sentinel says no
// further lines follow, and Run must honor that rather than draining to EOF —
// otherwise a trailing line would still be decoded.
func TestRun_StatusEndStopsReading(t *testing.T) {
	dec := newTestDecoder()
	evs := runCollect(t, context.Background(), stringBody("hello\nEND\nafter\n"),
		Options{Provider: "test", MissingTerminal: "no terminator"}, dec)
	for _, ev := range evs {
		if ev.Type == provider.EventTextDelta && ev.Text == "after" {
			t.Error("Run kept reading past StatusEnd")
		}
	}
	if last := lastEvent(t, evs); last.Type != provider.EventDone {
		t.Errorf("final event = %+v, want EventDone (StatusEnd is also terminal)", last)
	}
}

// TestRun_AbortEmitsNothingFurther: when the decoder has already emitted the
// event that ends the stream, Run must not append an EventDone behind it.
func TestRun_AbortEmitsNothingFurther(t *testing.T) {
	dec := newTestDecoder()
	evs := runCollect(t, context.Background(), stringBody("hello\nABORT\nTERMINAL\n"),
		Options{Provider: "test", MissingTerminal: "no terminator"}, dec)
	last := lastEvent(t, evs)
	if last.Type != provider.EventError || last.Err.Error() != "decoder said so" {
		t.Errorf("final event = %+v, want the decoder's own error", last)
	}
	if dec.finished {
		t.Error("Finish ran after StatusAbort")
	}
}

// TestRun_FlushRunsAndCanTerminate covers the anthropic framing case: the last
// event of a stream that does not end with a blank line exists only in the
// decoder's buffer, and flushing it is what makes the stream count as terminal.
func TestRun_FlushRunsAndCanTerminate(t *testing.T) {
	dec := &flushDecoder{testDecoder: newTestDecoder()}
	dec.flush = StatusTerminal
	evs := runCollect(t, context.Background(), stringBody("hello\n"),
		Options{Provider: "test", MissingTerminal: "no terminator"}, dec)
	if !dec.flushed {
		t.Error("Flush was not called")
	}
	if last := lastEvent(t, evs); last.Type != provider.EventDone {
		t.Errorf("final event = %+v, want EventDone — the flushed event terminated the stream", last)
	}
}

// TestRun_FlushAbortWins: a Flush that ends the stream itself stops Run there.
func TestRun_FlushAbortWins(t *testing.T) {
	dec := &flushDecoder{testDecoder: newTestDecoder()}
	dec.flush = StatusAbort
	evs := runCollect(t, context.Background(), stringBody("hello\nTERMINAL\n"),
		Options{Provider: "test", MissingTerminal: "no terminator"}, dec)
	if last := lastEvent(t, evs); last.Type == provider.EventDone {
		t.Errorf("final event = %+v, want no EventDone after an aborting Flush", last)
	}
	if dec.finished {
		t.Error("Finish ran after an aborting Flush")
	}
}

// TestRun_FinishMayEndTheStream: a decoder that fails while assembling what it
// accumulated (openai's truncated tool-call arguments) reports false, and Run
// must not follow its error with an EventDone.
func TestRun_FinishMayEndTheStream(t *testing.T) {
	dec := newTestDecoder()
	dec.finishOK = false
	evs := runCollect(t, context.Background(), stringBody("TERMINAL\n"),
		Options{Provider: "test", MissingTerminal: "no terminator"}, dec)
	last := lastEvent(t, evs)
	if last.Type != provider.EventError || last.Err.Error() != "finish failed" {
		t.Errorf("final event = %+v, want the decoder's Finish error", last)
	}
}

// errReader fails the read partway through, the shape of a server that goes
// away mid-stream.
type errReader struct {
	data string
	err  error
	done bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, nil
}

// TestRun_ScannerErrorIsTransport is P50.1/P61.3 in the shared skeleton: a
// mid-stream read failure must be a transport APIError so the backend-recovery
// path fires. Before P61.6 only the ollama adapter did this; the openai and
// anthropic adapters wrapped the identical condition in a bare fmt.Errorf that
// nothing could classify.
func TestRun_ScannerErrorIsTransport(t *testing.T) {
	body := &closeCounter{r: &errReader{data: "hello\n", err: errors.New("connection reset by peer")}}
	evs := runCollect(t, context.Background(), body,
		Options{Provider: "test", MissingTerminal: "no terminator"}, newTestDecoder())

	last := lastEvent(t, evs)
	if last.Type != provider.EventError {
		t.Fatalf("final event = %+v, want EventError", last)
	}
	var apiErr *provider.APIError
	if !errors.As(last.Err, &apiErr) {
		t.Fatalf("err = %v (%T), want a *provider.APIError", last.Err, last.Err)
	}
	if !provider.IsBackendUnavailableError(last.Err) {
		t.Errorf("err = %v, want IsBackendUnavailableError to classify it", last.Err)
	}
	if !strings.Contains(last.Err.Error(), "connection reset by peer") {
		t.Errorf("err = %v, want the underlying read failure preserved", last.Err)
	}
}

// TestRun_OversizedLineIsNamed is P35.12 lifted: a single line past the 4MiB
// scanner cap must name the cause (an oversized tool-call payload) instead of
// surfacing bufio's opaque "token too long". Only the ollama adapter had this
// before P61.6.
func TestRun_OversizedLineIsNamed(t *testing.T) {
	huge := strings.Repeat("x", maxBufSize+1)
	evs := runCollect(t, context.Background(), stringBody(huge+"\n"),
		Options{Provider: "test", MissingTerminal: "no terminator"}, newTestDecoder())

	last := lastEvent(t, evs)
	if last.Type != provider.EventError {
		t.Fatalf("final event = %+v, want EventError", last)
	}
	if !errors.Is(last.Err, bufio.ErrTooLong) {
		t.Errorf("err = %v, want it to wrap bufio.ErrTooLong", last.Err)
	}
	msg := last.Err.Error()
	if !strings.Contains(msg, "4MiB line limit") || !strings.Contains(msg, "tool-call argument payload") {
		t.Errorf("err = %v, want the oversized-line cause named (P35.12)", msg)
	}
	if !strings.HasPrefix(msg, "test: ") {
		t.Errorf("err = %v, want it prefixed with the provider name", msg)
	}
}

// stallingBody delivers one line and then blocks forever, which is the wedged
// runner P59.2 exists for: nothing else bounds it (Client.Timeout is zero by
// design and ResponseHeaderTimeout stopped applying when the headers arrived).
type stallingBody struct {
	mu     sync.Mutex
	first  bool
	closed chan struct{}
	once   sync.Once
}

func newStallingBody() *stallingBody { return &stallingBody{closed: make(chan struct{})} }

func (b *stallingBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	sent := b.first
	b.first = true
	b.mu.Unlock()
	if !sent {
		return copy(p, "hello\n"), nil
	}
	<-b.closed // unblocks only when the watchdog closes the body
	return 0, errors.New("http: read on closed response body")
}

func (b *stallingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// TestRun_IdleTimeoutFires: the watchdog closes the body to break the blocked
// read (there is no way to interrupt a bufio.Scanner otherwise), and the result
// is a transport error naming the stall and the config knob — not the raw
// "read on closed response body", which reads like a harness bug.
func TestRun_IdleTimeoutFires(t *testing.T) {
	evs := runCollect(t, context.Background(), newStallingBody(),
		Options{Provider: "test", IdleTimeout: 50 * time.Millisecond, MissingTerminal: "no terminator"},
		newTestDecoder())

	last := lastEvent(t, evs)
	if last.Type != provider.EventError {
		t.Fatalf("final event = %+v, want EventError", last)
	}
	if !provider.IsBackendUnavailableError(last.Err) {
		t.Errorf("err = %v, want a transport error so a stalled runner takes the crashed-server recovery path", last.Err)
	}
	msg := last.Err.Error()
	if !strings.Contains(msg, "stalled mid-generation") || !strings.Contains(msg, "provider.stream_idle_timeout") {
		t.Errorf("err = %v, want the stall and the knob named (P59.2)", msg)
	}
}

// TestRun_IdleTimeoutDisabled: <= 0 means no bound at all, which is how a
// caller opts out (ollama's negative-disables config mapping) — the stream must
// then run to its natural end rather than being guillotined.
func TestRun_IdleTimeoutDisabled(t *testing.T) {
	evs := runCollect(t, context.Background(), stringBody("hello\nTERMINAL\n"),
		Options{Provider: "test", IdleTimeout: 0, MissingTerminal: "no terminator"}, newTestDecoder())
	if last := lastEvent(t, evs); last.Type != provider.EventDone {
		t.Errorf("final event = %+v, want EventDone", last)
	}
}

// TestRun_IdleTimeoutResetsPerLine: the bound is on the server sending
// *anything*, so a stream that keeps delivering lines slower than the timeout
// in total — but faster than it per line — must not trip the watchdog.
func TestRun_IdleTimeoutResetsPerLine(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(pw, "tick\n")
		}
		_, _ = io.WriteString(pw, "TERMINAL\n")
		_ = pw.Close()
	}()
	evs := runCollect(t, context.Background(), pr,
		Options{Provider: "test", IdleTimeout: 200 * time.Millisecond, MissingTerminal: "no terminator"},
		newTestDecoder())
	if last := lastEvent(t, evs); last.Type != provider.EventDone {
		t.Errorf("final event = %+v, want EventDone — the watchdog must reset per delivered line", last)
	}
}

// TestRun_ContextCancellationStops: a cancelled ctx aborts the send and Run
// returns without blocking on a channel nobody is reading, closing the body and
// the output channel on the way out.
func TestRun_ContextCancellationStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body := stringBody("hello\nTERMINAL\n")
	out := make(chan provider.Event)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, body, out, Options{Provider: "test", MissingTerminal: "no terminator"}, newTestDecoder())
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run blocked on a cancelled context")
	}
	if _, open := <-out; open {
		t.Error("an event was delivered after the context was cancelled")
	}
	if !body.isClosed() {
		t.Error("Run did not close the response body")
	}
}
