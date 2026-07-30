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
// case-insensitively and checked before retryableStreamSignals, so a message
// carrying both classes is treated as terminal (the safe, cost-avoiding default
// this classification exists to serve).
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
// an overflow are recognized.
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
	m := strings.ToLower(apiErr.Message)
	for _, s := range contextOverflowSignals {
		if strings.Contains(m, s) {
			return true
		}
	}
	// Also catch a truncated tool call whose RAW server text — "invalid tool
	// call arguments … unexpected end of JSON input" — surfaced un-rewritten,
	// rather than an adapter's NewContextTruncationError message (whose marker
	// contextOverflowSignals already covers). Both adapters normally convert it
	// (ollama.go / openai.go), but any path that surfaces the raw signature
	// describes the same context-ceiling overflow, and a fresh, smaller context
	// recovers from it identically — so a whole-file write_file that truncated
	// resets and resumes instead of killing the drive. IsTruncatedToolCallError
	// keys on the premature-end-of-input signature (AND of both markers), so a
	// genuinely malformed call — a syntax error, not a truncation — is still not
	// misread as an overflow. (P47.x)
	return IsTruncatedToolCallError(apiErr.Message)
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
// describes a transient/infrastructural failure worth retrying. Terminal
// signals win over retryable ones, and an unrecognized message is terminal:
// retrying it risks wasting a full prompt-eval on a slow local model for a
// result that will not change (P33.16).
func classifyStreamError(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range terminalStreamSignals {
		if strings.Contains(m, s) {
			return false
		}
	}
	for _, s := range retryableStreamSignals {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
