// Package sse holds the small pieces of SSE-consumption plumbing that are
// identical across provider adapters (internal/provider/anthropic,
// internal/provider/openai): the HTTP client both use for the streamed
// request, sizing the line scanner, the ctx-aware channel send used while
// streaming events out, and turning a non-200 HTTP response into the
// provider.APIError adapters return from Stream. It intentionally does not
// know anything about either provider's wire format — that parsing stays
// adapter-specific.
package sse

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

const (
	initialBufSize = 64 * 1024
	maxBufSize     = 4 * 1024 * 1024
	maxErrBodySize = 64 * 1024

	// DefaultResponseHeaderTimeout bounds only the wait for the response
	// headers, not the stream that follows. It is generous because a cold
	// local backend (Ollama pulling a model into VRAM) can take minutes to
	// reply at all, and — the load-bearing case — Ollama withholds the response
	// header until prompt-eval/prefill finishes, so a large local context
	// (a skill-driven threat-model turn that just read a multi-thousand-line
	// file) can legitimately prefill for many minutes before the first byte.
	// P38.1 raised this from 5m to 30m: the built-in threat-model drive aborted
	// mid-build at the 5m ceiling reading a 2845-line file on a local 35B model
	// (~7 tok/s), and 5m is too tight for any content-rich repo on modest local
	// hardware. Callers that want a different bound (a configured
	// provider.response_header_timeout, P35.5) pass it explicitly to
	// NewStreamingClient instead of relying on this default.
	DefaultResponseHeaderTimeout = 30 * time.Minute

	// DefaultStreamIdleTimeout bounds the gap between two consecutive streamed
	// chunks (P59.2) — not the stream as a whole, which must stay unbounded for
	// the reasons NewStreamingClient documents. Once headers arrive nothing else
	// is watching: a local runner that wedges mid-generation leaves the consumer
	// blocked on a read forever, and MaxWallClockPerRun is polled between turns,
	// never inside one.
	//
	// It is deliberately generous. Prefill happens *before* the headers on
	// Ollama, so every gap this bound sees is an inter-token gap; even a ~7 tok/s
	// local model on a large context emits something every few seconds. A tight
	// bound would guillotine a legitimately slow run, which is the regression
	// shape P52.15 avoided by leaving the wall clock off by default — so the
	// number is set where "nothing at all for this long" cannot be healthy.
	DefaultStreamIdleTimeout = 10 * time.Minute
)

// NewStreamingClient returns the http.Client an adapter uses for its streamed
// request. Client.Timeout stays zero on purpose: it bounds the whole request
// *including* reading the response body, so any non-zero value silently caps
// how long a turn may stream and kills a legitimately long agentic turn on a
// slow local model as a transport error mid-answer. Bounding the overall run
// is the caller's context's job; the transport still bounds a server that
// accepts the connection and then never sends headers. Mirrors the daemon
// client's split between its SSE and RPC clients (internal/client.New).
//
// headerTimeout <= 0 substitutes DefaultResponseHeaderTimeout.
func NewStreamingClient(headerTimeout time.Duration) *http.Client {
	if headerTimeout <= 0 {
		headerTimeout = DefaultResponseHeaderTimeout
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Timeout: 0, Transport: tr}
}

// NewScanner returns a bufio.Scanner over body, pre-sized the way both
// adapters need it: a 64KiB initial buffer growing up to 4MiB so a single
// large SSE line (e.g. a big tool-call JSON payload) doesn't hit the
// scanner's default 64KiB token limit.
func NewScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, initialBufSize), maxBufSize)
	return scanner
}

// Emitter sends events to an adapter's output channel, aborting the send
// (without blocking forever) if ctx is cancelled first.
type Emitter struct {
	ctx context.Context
	out chan<- provider.Event
}

// NewEmitter constructs an Emitter bound to ctx and out. Its Emit method is
// the shared body of the "emit" closure both adapters defined locally.
func NewEmitter(ctx context.Context, out chan<- provider.Event) *Emitter {
	return &Emitter{ctx: ctx, out: out}
}

// Emit sends ev on the output channel and reports whether it was delivered;
// it returns false without sending if ctx is done first.
func (e *Emitter) Emit(ev provider.Event) bool {
	select {
	case e.out <- ev:
		return true
	case <-e.ctx.Done():
		return false
	}
}

// HandleErrorResponse converts a non-200 HTTP response into the error an
// adapter's Stream method returns (paired with a nil channel). It reads and
// caps the response body, closes it, and wraps the result via
// provider.NewHTTPError.
func HandleErrorResponse(providerName string, resp *http.Response) error {
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodySize))
	return provider.NewHTTPError(providerName, resp.StatusCode,
		resp.Header.Get("Retry-After"), strings.TrimSpace(string(msg)))
}
