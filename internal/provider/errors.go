package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIError is a structured provider error. Adapters return it from Stream so the
// retry layer (see retry.go) can classify failures without parsing error
// strings. StatusCode == 0 denotes a transport/network failure (Err is set).
type APIError struct {
	Provider   string        // adapter name, e.g. "anthropic"
	StatusCode int           // HTTP status; 0 for transport-level errors
	Message    string        // response body or error detail
	RetryAfter time.Duration // parsed from a Retry-After header; 0 if absent
	Err        error         // underlying transport error, if any

	// Detail carries model-authored text — a tool name the model emitted, a
	// generation fragment — that belongs in what a human reads but must never
	// reach the substring classifiers below. Message is control flow: every
	// signal table in this file substring-matches it, so any model-influenceable
	// text spliced into it lets a generation steer retry and recovery decisions.
	// A tool call named `crash_report` failing to parse was enough to flip a
	// terminal malformed-call error to retryable AND to make
	// IsBackendUnavailableError report a dead backend, sending the phased
	// drive (P50.1) into a liveness-probe wait for a live server. Detail is
	// rendered by Error() and ignored by every classifier. (P61.7)
	Detail string

	// stream marks a mid-stream error surfaced inside the event stream (a
	// provider's {"error":...} envelope, not a synchronous Stream failure). It
	// renders Message verbatim rather than as an HTTP/transport failure, and
	// carries an explicit per-class retryability in streamRetryable instead of
	// deriving it from a status code — see NewStreamError / P33.16.
	stream          bool
	streamRetryable bool
}

// Error implements error.
func (e *APIError) Error() string {
	if e.stream {
		if e.Detail != "" {
			return fmt.Sprintf("%s: %s (%s)", e.Provider, e.Message, e.Detail)
		}
		return fmt.Sprintf("%s: %s", e.Provider, e.Message)
	}
	if e.StatusCode == 0 {
		if e.Err != nil {
			return fmt.Sprintf("%s: request failed: %v", e.Provider, e.Err)
		}
		return fmt.Sprintf("%s: request failed", e.Provider)
	}
	return fmt.Sprintf("%s: status %d: %s", e.Provider, e.StatusCode, e.Message)
}

// Unwrap exposes the underlying transport error to errors.Is/As.
func (e *APIError) Unwrap() error { return e.Err }

// Retryable reports whether the request may succeed if retried. Rate limits
// (429), request timeouts (408), conflicts (409) and 5xx server errors are
// transient; transport errors are retryable unless they stem from context
// cancellation. 4xx client errors (other than the above) are permanent.
func (e *APIError) Retryable() bool {
	if e.stream {
		return e.streamRetryable
	}
	if errors.Is(e.Err, context.Canceled) || errors.Is(e.Err, context.DeadlineExceeded) {
		return false
	}
	if e.StatusCode == 0 {
		return e.Err != nil // a transport error with no status
	}
	switch e.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	}
	return e.StatusCode >= 500 && e.StatusCode <= 599
}

// parseRetryAfter interprets a Retry-After header value, which is either a
// number of seconds or an HTTP date. It returns 0 when absent or unparseable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// NewHTTPError builds an APIError from a non-2xx HTTP response.
func NewHTTPError(providerName string, status int, retryAfter, body string) *APIError {
	return &APIError{
		Provider:   providerName,
		StatusCode: status,
		Message:    body,
		RetryAfter: parseRetryAfter(retryAfter),
	}
}

// NewTransportError builds an APIError from a transport-level failure (no HTTP
// status, e.g. connection refused or DNS error).
func NewTransportError(providerName string, err error) *APIError {
	return &APIError{Provider: providerName, Err: err}
}

// NewStreamError builds the APIError an adapter emits as a mid-stream
// EventError from a provider's {"error":...} envelope (P33.16). Unlike a
// synchronous transport/HTTP failure it renders msg verbatim ("<provider>:
// <msg>"), preserving exactly the text callers surfaced before classification
// existed, and it carries an explicit retryable/terminal verdict — derived from
// msg by classifyStreamError — that the retry layer and any future consumer can
// read through errors.As(&APIError)+Retryable() without re-parsing the string.
//
// Transient/infrastructural failures (worker crash, model-load failure, OOM,
// connection reset mid-stream) are retryable; deterministic ones (context
// overflow, invalid/malformed request, model-not-found) and anything
// unrecognized are terminal, because retrying them only burns another full
// prompt-eval on a slow local model for a result that cannot change — the waste
// this classification exists to avoid.
func NewStreamError(providerName, msg string) *APIError {
	return &APIError{
		Provider:        providerName,
		Message:         msg,
		stream:          true,
		streamRetryable: classifyStreamError(msg),
	}
}

// NewMalformedToolCallError builds the error for a tool call whose accumulated
// arguments are not valid JSON and that carries no truncation signal — the
// model produced a broken call rather than being cut off at the context
// ceiling (the sawLength case is NewContextTruncationError's). It is terminal:
// like the truncation error it will not classify differently on a retry.
//
// The tool name goes in Detail, never in Message. It is the only text on this
// path the model authors, and splicing it into Message put a generation in
// charge of the retry and backend-liveness verdicts — see Detail's comment and
// P61.7. Message is a fixed string here, so the classification is a property of
// the failure class rather than of what the model happened to name its tool.
func NewMalformedToolCallError(providerName, toolName string) *APIError {
	return &APIError{
		Provider:        providerName,
		Message:         "invalid tool call arguments: unexpected end of JSON input",
		Detail:          "tool " + strconv.Quote(toolName),
		stream:          true,
		streamRetryable: false,
	}
}

// NewContextTruncationError builds the actionable error surfaced when a
// generation is cut off at the model's context ceiling mid-tool-call (P35.2).
// A local model server (Ollama/llama-server) that runs out of context partway
// through emitting a tool call stops with the arguments JSON cut short and
// reports a bare "invalid tool call arguments ... unexpected end of JSON input"
// — indistinguishable, to a caller, from a genuinely malformed model call, and
// giving no hint that the fix is to raise provider.context_window. This error
// names that cause instead. It is terminal (stream, non-retryable): retrying an
// over-long prompt unchanged fails identically and only burns another
// prompt-eval on a slow local model. The underlying server message, when
// present, is preserved parenthetically so nothing is lost.
func NewContextTruncationError(providerName, underlying string) *APIError {
	msg := "response truncated at the context limit — raise provider.context_window or reduce session history"
	if u := strings.TrimSpace(underlying); u != "" {
		msg += " (server error: " + u + ")"
	}
	return &APIError{
		Provider:        providerName,
		Message:         msg,
		stream:          true,
		streamRetryable: false,
	}
}

// NewResponseHeaderTimeoutError builds the actionable error surfaced when a
// request on a native-Ollama/OpenAI-compat local backend fails with Go's bare
// transport string "net/http: timeout awaiting response headers" (P35.6).
// Ollama withholds the HTTP response header until prompt-eval (prefill)
// finishes (see provider.response_header_timeout, P35.5), so this fires when
// prefill for a large context takes longer than that budget — not when the
// server is unreachable or crashed, which the raw transport string is
// indistinguishable from. This error names that cause and the levers instead:
// raise provider.response_header_timeout, lower context_window, or reduce
// per-turn context growth. It is terminal (non-retryable): a blind retry
// re-processes the same oversized prefill and times out again, exactly as
// P35.2's context-truncation error is terminal for the same reason. The
// underlying transport error is preserved parenthetically so nothing is lost.
func NewResponseHeaderTimeoutError(providerName string, underlying error) *APIError {
	msg := "timed out waiting for the response header — the model is likely still processing " +
		"a large prompt (prefill) on a local backend and exceeded provider.response_header_timeout; " +
		"raise that setting, lower context_window, or reduce per-turn context growth"
	if underlying != nil {
		msg += " (" + underlying.Error() + ")"
	}
	return &APIError{
		Provider:        providerName,
		Message:         msg,
		Err:             underlying,
		stream:          true,
		streamRetryable: false,
	}
}

// IsResponseHeaderTimeoutError reports whether err is Go's transport-level
// error for a response-header wait that exceeded the HTTP client's configured
// ResponseHeaderTimeout — the bare string "net/http: timeout awaiting response
// headers", with no HTTP status and no server-side error envelope to key off
// (P35.6).
func IsResponseHeaderTimeoutError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout awaiting response headers")
}

// IsTruncatedToolCallError reports whether a mid-stream error message is the
// signature of a tool call cut off at the context ceiling rather than a model
// producing a syntactically malformed call. On the native Ollama path the
// server does the tool-call parsing itself and returns only this message, so
// the message shape is the only truncation signal available: an "invalid tool
// call arguments" failure whose cause is a *premature end* of the arguments
// JSON ("unexpected end of JSON input"). That premature-end-of-input — not a
// syntax error like "invalid character" — is what truncation produces, so this
// keys on the truncation signal, not on the generic parse failure. (P35.2)
func IsTruncatedToolCallError(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "invalid tool call arguments") &&
		strings.Contains(m, "unexpected end of json input")
}

// terminalStreamSignals are substrings marking a deterministic mid-stream
// failure — one that fails identically on every retry. Matched
// case-insensitively and ranked above retryableStreamSignals, so a message
// carrying both classes is treated as terminal (the safe, cost-avoiding default
// this classification exists to serve). They are ranked BELOW
// backendDeadStreamSignals and contextOverflowSignals, both of which name a
// specific cause where this table only names a broad word — see
// classifyStreamMessage.
var terminalStreamSignals = []string{
	"context length",
	"context window",
	"context size",
	"exceeds context",
	"exceed the context",
	"exceeded the context",
	"maximum context",
	"too many tokens",
	"prompt is too long",
	"input is too large",
	"invalid request",
	"invalid_request",
	"no such model",
	"model not found",
	"not found, try pulling",
	"does not support",
	"unsupported",
	"malformed",
}

// contextOverflowSignals mark a terminal error whose cause is the prompt
// exceeding the model's context window — the class of terminal failure a
// fresh, smaller context can recover from (P47.2). It is a deliberate subset
// of terminalStreamSignals: it excludes the size-independent terminal failures
// (model-not-found, malformed, unsupported, generic invalid-request) that a
// context reset cannot fix, and adds the P35.2 context-truncation error's own
// message marker so both the truncated-tool-call and the hard-reject shapes of
// an overflow are recognized. It ranks FIRST in classifyStreamMessage: an
// oversized prefill is a plausible cause of the runner dying, so when both a
// size signal and a crash signal are present the size one is the explanation
// and the reset — not a wait for the corpse to reanimate — is the recovery.
var contextOverflowSignals = []string{
	"context length",
	"context window",
	"context size",
	"exceeds context",
	"exceed the context",
	"exceeded the context",
	"maximum context",
	"too many tokens",
	"prompt is too long",
	"input is too large",
	"response truncated at the context limit", // NewContextTruncationError's marker
}

// IsContextOverflowError reports whether err is a terminal provider error whose
// cause is the prompt exceeding the model's context window — either the P35.2
// context-truncation error (NewContextTruncationError), a mid-stream
// {"error":...} envelope carrying a context-size signal (NewStreamError), or a
// truncated tool call that reached here with its RAW server text intact rather
// than an adapter's rewritten message. This is the subset of terminal errors a
// fresh, smaller context can recover from, which is what lets the phased skill
// drive treat an overflow as a resumable phase reset rather than a fatal abort
// (P47.2) — as distinct from size-independent terminal failures like
// model-not-found or a malformed request, where resetting and retrying would
// only loop. Response-header timeouts (P35.6) are deliberately excluded: their
// fix is a different lever.
func IsContextOverflowError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	// Read the same ordered ladder Retryable() and IsBackendUnavailableError do,
	// so all three answer from one classification (P61.7(b)). The ladder's first
	// rung covers both shapes of an overflow: the contextOverflowSignals
	// vocabulary, and a truncated tool call whose RAW server text — "invalid
	// tool call arguments … unexpected end of JSON input" — surfaced
	// un-rewritten rather than as an adapter's NewContextTruncationError. Both
	// adapters normally convert it (ollama.go / openai.go), but any path
	// surfacing the raw signature describes the same context-ceiling overflow
	// and a fresh, smaller context recovers from it identically — so a
	// whole-file write_file that truncated resets and resumes instead of killing
	// the drive. IsTruncatedToolCallError keys on the premature-end-of-input
	// signature (AND of both markers), so a genuinely malformed call — a syntax
	// error, not a truncation — is still not misread as an overflow. (P47.x)
	return classifyStreamMessage(apiErr.Message) == classContextOverflow
}

// IsBackendUnavailableError reports whether err means the model server itself
// is unreachable or has died — as distinct from a context overflow (a prompt
// problem) or a rate limit (a quota problem). Two shapes qualify: a
// transport-level failure with no HTTP status (connection refused/reset, EOF —
// the shape of `ollama serve` not listening or being killed), and a mid-stream
// {"error":...} envelope carrying one of the infrastructural signals
// (`model runner has unexpectedly stopped`, `connection reset`, a load/OOM
// crash). This is the class the phased drive (P50.1) recovers from by WAITING
// for the backend to come back and then resetting the phase to a fresh context
// that re-reads the on-disk suite — the same resumable treatment
// IsContextOverflowError gets, but gated on a liveness probe first. A
// context-cancelled transport error is excluded: that is the user aborting, not
// the backend dying. A message that ALSO carries a context-size signal is
// excluded too, so this and IsContextOverflowError can never both answer true —
// the drive checks backend-down before overflow, and a crash caused by an
// oversized prefill must take the reset path, not the wait path (P61.7b).
func IsBackendUnavailableError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if errors.Is(apiErr.Err, context.Canceled) || errors.Is(apiErr.Err, context.DeadlineExceeded) {
		return false
	}
	// Transport-level failure (no HTTP status): connection refused/reset, DNS,
	// EOF — the server is not answering. A response-header timeout is excluded:
	// its cause is slow prefill on a live server, not a dead one (P35.6).
	if !apiErr.stream && apiErr.StatusCode == 0 && apiErr.Err != nil {
		return !IsResponseHeaderTimeoutError(apiErr.Err)
	}
	// Mid-stream infrastructural failure: the model runner crashed/was killed
	// while streaming. This reads the SAME ordered ladder Retryable() does
	// (classifyStreamMessage), which is the P61.7(b) fix: this function used to
	// scan backendDeadStreamSignals directly while classifyStreamError scanned
	// terminalStreamSignals first, so one string could be "terminal, do not
	// retry" and "the backend is dead, wait for it" at the same time.
	return classifyStreamMessage(apiErr.Message) == classBackendDead
}

// streamClass is the verdict of the one ordered ladder every mid-stream message
// classifier in this file reads. Having a single ladder is the point: before
// P61.7(b) two functions matched overlapping tables in different orders, and
// `model runner has unexpectedly stopped (last output: … unsupported …)` came
// back as non-retryable AND backend-dead — a pair of verdicts no recovery path
// can act on coherently. That needed no prompt injection; any server that
// appends the tail of a generation to a crash message produces it.
type streamClass int

const (
	// classUnrecognized: no table matched. Terminal, per P33.16's default.
	classUnrecognized streamClass = iota
	// classContextOverflow: the prompt was too big — see contextOverflowSignals.
	classContextOverflow
	// classBackendDead: the model server/runner is gone — see
	// backendDeadStreamSignals.
	classBackendDead
	// classTerminal: a deterministic failure that is not about size and not
	// about liveness (model-not-found, malformed, unsupported, invalid request).
	classTerminal
	// classRetryable: transient but not proof the server died — the plain
	// timeout signals.
	classRetryable
)

// classifyStreamMessage assigns a mid-stream provider error message to exactly
// one class, in a fixed precedence order. The order is the whole design, so it
// is argued here rather than at the call sites:
//
//  1. Context overflow first. It is the one terminal class that also plausibly
//     *causes* the runner to die (a prefill that OOMs kills the process), so
//     without this tier an overflow-induced crash would be waited out and
//     re-sent unchanged, killing the server again — while the recovery that
//     actually works, P47.2's reset to a fresh, smaller context, never runs.
//     Size beats liveness because the size cause explains the death, not the
//     other way round. This keeps `worker crashed: context length exceeded`
//     terminal, as it has always been.
//
//  2. Backend-dead next — deliberately AHEAD of the generic terminal signals,
//     which reverses the older "terminal always wins". Two reasons. It is the
//     more specific and more actionable observation: "the runner process is
//     gone" is a claim about the state of the machine, whereas "the word
//     `unsupported` appeared somewhere in the text" is a claim about a substring
//     — and terminalStreamSignals is deliberately broad (`malformed`,
//     `unsupported`, `does not support`, `invalid request`), exactly the
//     vocabulary a server echoing a generation fragment or a driver message into
//     its crash text will hit by accident. Second, the cost that justified
//     terminal-wins does not exist here: P33.16 chose terminal-by-default
//     because retrying burns another full prompt-eval on a slow local model for
//     an unchanged result, and a *dead* backend cannot burn a prompt-eval —
//     the attempt fails at connect, in milliseconds. And on the recovery side
//     the asymmetry is stark: a false "dead" costs one cheap liveness probe
//     that answers immediately plus a context reset (P50.1's wait short-circuits
//     when the server is up), while a false "terminal" aborts the whole phased
//     drive on a half-built suite — the exact failure P50.1 was filed for.
//
//  3. Then the remaining terminal signals, and 4. the remaining retryable ones
//     (`timed out`, `i/o timeout` — a live-but-slow server), unchanged from
//     P33.16. Anything unmatched stays terminal.
func classifyStreamMessage(msg string) streamClass {
	m := strings.ToLower(msg)
	switch {
	// The raw truncated-tool-call signature belongs on this rung too, not only
	// in IsContextOverflowError's tail. It describes the same context-ceiling
	// overflow, and leaving it outside the ladder let one message be classified
	// backend-dead by the ladder AND overflow by that tail — the very
	// double-verdict this ladder exists to prevent, reachable whenever a server
	// appends a crash phrase to the truncation text.
	case containsAnySignal(m, contextOverflowSignals) || IsTruncatedToolCallError(msg):
		return classContextOverflow
	case containsAnySignal(m, backendDeadStreamSignals):
		return classBackendDead
	case containsAnySignal(m, terminalStreamSignals):
		return classTerminal
	case containsAnySignal(m, retryableStreamSignals):
		return classRetryable
	}
	return classUnrecognized
}

// containsAnySignal reports whether the already-lowercased message contains any
// of the signal substrings.
func containsAnySignal(lowered string, signals []string) bool {
	for _, s := range signals {
		if strings.Contains(lowered, s) {
			return true
		}
	}
	return false
}

// backendDeadStreamSignals are the subset of retryableStreamSignals that
// specifically mean the model server/runner is gone or crashed — the ones a
// liveness-gated wait-and-resume (P50.1) is the right recovery for. It excludes
// the plain "timed out" / "i/o timeout" signals, which can fire on a live but
// briefly-slow server and are already handled by the retry decorator. Ranked
// above terminalStreamSignals but below contextOverflowSignals
// (classifyStreamMessage): every phrase here names a specific mechanical event
// on the server, so it is far less likely to appear incidentally in echoed text
// than the broad words in the terminal table.
var backendDeadStreamSignals = []string{
	"unexpectedly stopped",
	"has terminated",
	"crash",
	"failed to load model",
	"error loading model",
	"unable to load model",
	"out of memory",
	"cudamalloc",
	"failed to allocate",
	"cannot allocate",
	"connection reset",
	"connection refused",
	"broken pipe",
	"unexpected eof",
	"unknown error was encountered while running the model",
}

// retryableStreamSignals are substrings marking a transient/infrastructural
// mid-stream failure that could plausibly succeed on retry.
var retryableStreamSignals = []string{
	"unexpectedly stopped",
	"has terminated",
	"crash",
	"failed to load model",
	"error loading model",
	"unable to load model",
	"out of memory",
	"cudamalloc",
	"failed to allocate",
	"cannot allocate",
	"connection reset",
	"connection refused",
	"broken pipe",
	"unexpected eof",
	"i/o timeout",
	"timed out",
	"unknown error was encountered while running the model",
}

// classifyStreamError reports whether a mid-stream provider error message
// describes a transient/infrastructural failure worth retrying. An unrecognized
// message is terminal: retrying it risks wasting a full prompt-eval on a slow
// local model for a result that will not change (P33.16). Terminal signals
// still win over the plain retryable ones (`timed out`), but a backend-dead
// signal now wins over both — see classifyStreamMessage for why the precedence
// is ordered that way and what it deliberately does not change.
func classifyStreamError(msg string) bool {
	switch classifyStreamMessage(msg) {
	case classBackendDead, classRetryable:
		return true
	}
	return false
}
