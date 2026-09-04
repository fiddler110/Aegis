package sse

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// Status is what a Decoder reports back to Run about the line it just decoded.
// It exists so the *lifecycle* decisions — stop reading, count the stream as
// finished, abandon it — stay in Run while the wire format stays in the
// adapter (P61.6).
type Status int

const (
	// StatusContinue: nothing about the stream's lifecycle changed. Keep
	// reading. This is the zero value, so a decoder that falls off the end of
	// its switch keeps the stream alive rather than ending it.
	StatusContinue Status = iota

	// StatusTerminal: this line was the stream's own statement that the
	// generation finished — an Ollama chunk with done:true, an OpenAI choice
	// carrying a finish_reason, an Anthropic message_stop or a message_delta
	// with a stop_reason. It satisfies Options.MissingTerminal (P61.2) but does
	// not stop the read: on every wire format except OpenAI's sentinel there
	// may legitimately be more lines after it.
	StatusTerminal

	// StatusEnd: as StatusTerminal, plus the wire has stated that no further
	// lines follow — only OpenAI's "[DONE]" sentinel does this. Run leaves the
	// read loop and proceeds to the same post-loop path a clean EOF takes.
	StatusEnd

	// StatusAbort: the decoder has already emitted the event that ends this
	// stream (an EventError it classified itself), or an emit failed because
	// ctx was cancelled. Run returns immediately, emitting nothing further —
	// in particular no EventDone.
	StatusAbort
)

// Decoder is the per-adapter half of a streamed response: the part that
// legitimately differs between wire formats (NDJSON with native tool calls,
// SSE with "[DONE]", SSE with indexed content blocks). Everything around it —
// the idle watchdog, the read loop, the terminal-chunk requirement,
// scanner.Err() classification and the final EventDone — belongs to Run, which
// is the whole point of P61.6: those five had independently drifted across the
// three adapters, each toward whichever one was in front of whoever was
// debugging a local model at the time.
//
// A Decoder is single-use and inherently stateful (accumulated tool-call
// fragments, a running stop reason, usage counters), so adapters construct a
// fresh one per Stream call.
type Decoder interface {
	// Line decodes exactly one line as delivered by the scanner — untrimmed,
	// without its newline — and emits whatever events it carries. emit reports
	// false when ctx was cancelled mid-send, in which case the decoder must
	// stop and return StatusAbort.
	Line(line string, emit func(provider.Event) bool) Status

	// Finish runs after the stream ended cleanly *and* satisfied the
	// terminal-chunk requirement. It is where a decoder that accumulates
	// fragments across lines emits the assembled result (OpenAI's tool-call
	// arguments), which is deliberately after the terminal check: a stream cut
	// off mid-tool-call must be reported as truncated rather than flushed as
	// if it were whole (P61.2).
	//
	// It returns the stop reason and usage Run puts on the final EventDone,
	// and false if it emitted the stream's ending event itself (a truncated
	// tool call, or a cancelled emit) — in which case Run emits nothing more.
	Finish(emit func(provider.Event) bool) (provider.StopReason, *provider.Usage, bool)
}

// Flusher is the optional half of Decoder for a wire format that buffers
// across lines and therefore may still be holding a complete, undelivered
// event when the reader stops. Anthropic's SSE framing is the case: an event
// is dispatched on the blank line that ends it, so the last event of a stream
// that does not end with a blank line only exists in the buffer.
//
// Run calls Flush after the read loop and *before* classifying scanner.Err(),
// matching the order anthropic.consume used: a buffered final message_delta is
// what makes an otherwise complete stream count as terminal.
type Flusher interface {
	Flush(emit func(provider.Event) bool) Status
}

// Options carries the per-adapter facts Run needs that are not wire format.
type Options struct {
	// Provider names the adapter in every error Run constructs.
	Provider string

	// IdleTimeout bounds the gap between two consecutive delivered lines
	// (P59.2). <= 0 disables the watchdog. Callers map their configured value
	// onto this — 0-means-default is a config-layer decision, not this one's.
	IdleTimeout time.Duration

	// MissingTerminal is the message reported (as a transport APIError) when
	// the stream ended cleanly without any line having returned StatusTerminal
	// or StatusEnd (P61.2). Empty disables the requirement, which no shipped
	// adapter does — it exists so a future wire format with no terminator at
	// all does not have to lie about one.
	MissingTerminal string
}

// Run owns the lifecycle of one streamed response: it closes body and out when
// it returns, drives the idle watchdog, reads the body line by line through
// dec, classifies a read failure, enforces the terminal-chunk requirement and
// emits the final EventDone. It is meant to be started as a goroutine, exactly
// as each adapter's consume was.
//
// The split is the P61.6 fix: before it, three adapters each re-derived the
// watchdog, the terminal requirement, the scanner.Err() classification, the
// bufio.ErrTooLong naming and the final EventDone, and all five had drifted.
func Run(ctx context.Context, body io.ReadCloser, out chan<- provider.Event, opts Options, dec Decoder) {
	defer close(out)
	defer body.Close()

	emit := NewEmitter(ctx, out).Emit

	// P59.2: the idle watchdog. Closing the body is what breaks the blocked
	// read — there is no way to interrupt a bufio.Scanner otherwise — and the
	// resulting scanner error lands on the P50.1 branch below, which already
	// classifies it as a transport failure and therefore routes into the
	// waitForBackend/resume-from-disk path built for a crashed server. A wedged
	// runner and a dead one are the same problem from the harness's side, so
	// they get the same recovery; idleFired only sharpens the message.
	var idleFired atomic.Bool
	resetIdle, stopIdle := func() {}, func() {}
	if opts.IdleTimeout > 0 {
		timer := time.AfterFunc(opts.IdleTimeout, func() {
			idleFired.Store(true)
			_ = body.Close()
		})
		defer timer.Stop()
		resetIdle = func() { timer.Reset(opts.IdleTimeout) }
		stopIdle = func() { timer.Stop() }
	}

	sawTerminal := false
	scanner := NewScanner(body)
readLoop:
	for {
		// The watchdog is armed only across the read itself. The bound is on the
		// server sending *anything* — and a line the decoder skips (blank, a
		// keepalive, JSON it can't unmarshal) is still evidence the runner is
		// alive, so it resets per delivered line rather than per parsed chunk.
		//
		// LLM-17: it is disarmed again the moment a line lands, because dec.Line
		// blocks inside emit while the consumer reads the event. Leaving it armed
		// there measured the *consumer's* processing time against the server's
		// idle bound, so a caller slower than IdleTimeout per event (a TUI
		// redrawing, a tool result being persisted) had the body closed
		// underneath it and was told the model runner had stalled. A wedged
		// consumer is not a dead backend; the engine's MaxTurnStall is the bound
		// that covers it.
		resetIdle()
		ok := scanner.Scan()
		stopIdle()
		if !ok {
			break
		}
		st := dec.Line(scanner.Text(), emit)
		if st == StatusTerminal || st == StatusEnd {
			sawTerminal = true
		}
		switch st {
		case StatusAbort:
			return
		case StatusEnd:
			break readLoop
		}
	}

	if f, ok := dec.(Flusher); ok {
		switch st := f.Flush(emit); st {
		case StatusAbort:
			return
		case StatusTerminal, StatusEnd:
			sawTerminal = true
		}
	}

	if err := scanner.Err(); err != nil {
		// P59.2: our own watchdog closed the body. Say so plainly — the raw
		// error is "http: read on closed response body", which reads like a
		// harness bug rather than a stalled runner. Still a transport error, so
		// it takes the same recovery path as a crashed server (see the watchdog
		// comment above).
		if idleFired.Load() {
			emit(provider.Event{Type: provider.EventError, Err: provider.NewTransportError(opts.Provider, fmt.Errorf(
				"read stream: no output for %s — the model runner appears to have stalled mid-generation "+
					"(raise or disable provider.stream_idle_timeout if this was a legitimately slow run): %w",
				opts.IdleTimeout, err))})
			return
		}
		// P35.12: a tool call can arrive whole on a single line (an NDJSON
		// chunk, one SSE data payload), so an oversized tool-call argument
		// payload trips bufio.ErrTooLong against the 4MiB scanner cap
		// (maxBufSize). The generic wrap surfaces it as the opaque "token too
		// long"; name the actual cause instead so the reader can act on it.
		if errors.Is(err, bufio.ErrTooLong) {
			emit(provider.Event{Type: provider.EventError, Err: fmt.Errorf(
				"%s: read stream: a single stream line exceeded the 4MiB line limit "+
					"(most likely a tool-call argument payload the model emitted is too large): %w",
				opts.Provider, err)})
			return
		}
		// P50.1: a mid-stream read failure — connection reset / unexpected EOF —
		// is the signature of the server going away *while streaming*, exactly
		// what happens when `ollama serve` is killed or crashes mid-response. Wrap
		// it as a transport APIError (not a bare error) so IsBackendUnavailableError
		// classifies it and the phased drive waits for the backend to return and
		// resumes from disk, rather than aborting on an unclassifiable error. A
		// synchronous connection failure is already a NewTransportError; this
		// closes the mid-stream counterpart, which is the more common one on a
		// slow local model whose per-turn stream is long.
		emit(provider.Event{Type: provider.EventError, Err: provider.NewTransportError(opts.Provider, fmt.Errorf("read stream: %w", err))})
		return
	}

	// P61.2/P59.3: the stream ended with no read error and no terminator. That
	// is not a finished turn — the body was closed mid-generation (a runner
	// that exited, a proxy that cut the response, a server shutting down
	// between chunks). Neither the P50.1 mid-stream-read-failure path nor the
	// P35.12 line-limit path fires here, because scanner.Err() is nil.
	//
	// Emitting EventDone regardless was the bug: usage stays zeroed, so the
	// engine substitutes estimates and flags IsEstimated exactly as it does for
	// a legitimately usage-free provider, and the stop reason stays at whatever
	// "the model chose to end its turn" value the decoder started with — so a
	// truncated response was surfaced as a complete short answer. Classifying
	// it as a transport error instead routes it into the existing
	// waitForBackend/resume-from-disk path via IsBackendUnavailableError rather
	// than into a silent wrong answer.
	//
	// What counts as a terminator is the decoder's call, and it is deliberately
	// looser than one sentinel per format: the OpenAI decoder accepts a
	// finish_reason as well as "[DONE]" because Ollama's /v1 compat endpoint
	// omits the sentinel, and the Anthropic decoder accepts a message_delta
	// carrying a stop_reason as well as message_stop because that is where the
	// completion semantics actually land. Requiring the envelope's last event
	// alone would report legitimate streams as transport failures — a worse
	// regression than the bug.
	if opts.MissingTerminal != "" && !sawTerminal {
		emit(provider.Event{Type: provider.EventError,
			Err: provider.NewTransportError(opts.Provider, errors.New(opts.MissingTerminal))})
		return
	}

	stop, usage, ok := dec.Finish(emit)
	if !ok {
		return
	}
	emit(provider.Event{Type: provider.EventDone, Stop: stop, Usage: usage})
}
