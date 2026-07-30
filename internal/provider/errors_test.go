package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestIsBackendUnavailableError is the P50.1 classifier guard: the phased drive
// waits out and resumes from a dead/unreachable model server, but only for that
// class — not for a context overflow (a prompt problem) or a user cancel. A
// transport failure with no HTTP status (connection refused/reset) and a
// mid-stream runner-crash envelope both qualify; a header timeout (slow prefill
// on a LIVE server), a context-overflow, a rate limit, and a cancelled request
// must not.
func TestIsBackendUnavailableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"connection refused (transport)", NewTransportError("ollama", errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")), true},
		{"mid-stream runner crash", NewStreamError("ollama", "model runner has unexpectedly stopped"), true},
		{"mid-stream connection reset", NewStreamError("ollama", "connection reset by peer"), true},
		{"mid-stream OOM crash", NewStreamError("ollama", "failed to allocate: out of memory"), true},
		{"header timeout is a live-but-slow server, not down", NewResponseHeaderTimeoutError("ollama", errors.New("net/http: timeout awaiting response headers")), false},
		{"context overflow is not backend-down", NewContextTruncationError("ollama", ""), false},
		{"stream context reject is not backend-down", NewStreamError("ollama", "input exceeds context length"), false},
		{"rate limit is not backend-down", NewHTTPError("anthropic", 429, "", "rate limited"), false},
		{"cancelled transport is user abort, not down", NewTransportError("ollama", context.Canceled), false},
		{"deadline exceeded is not down", NewTransportError("ollama", context.DeadlineExceeded), false},
		{"non-provider error", errors.New("something else"), false},
		{"nil error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBackendUnavailableError(tc.err); got != tc.want {
				t.Errorf("IsBackendUnavailableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsBackendUnavailableError_Wrapped confirms the classifier looks through a
// wrapping error, matching how engine.Run surfaces provider errors.
func TestIsBackendUnavailableError_Wrapped(t *testing.T) {
	inner := NewTransportError("ollama", errors.New("connection refused"))
	if !IsBackendUnavailableError(fmt.Errorf("engine run failed: %w", inner)) {
		t.Error("IsBackendUnavailableError must see through a wrapped APIError")
	}
}

// TestIsContextOverflowError is the P47.2 classifier guard: the phased skill
// drive resets and retries a phase only for terminal errors whose cause is the
// prompt exceeding the context window — the class a fresh, smaller context can
// recover from. It must recognize both the P35.2 context-truncation error and a
// context-size stream envelope, while rejecting size-independent terminal
// failures (model-not-found, malformed) and non-provider errors, so a reset is
// never attempted where it could only loop.
func TestIsContextOverflowError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"context-truncation error", NewContextTruncationError("ollama", "invalid tool call arguments: unexpected end of JSON input"), true},
		{"truncation error, no underlying", NewContextTruncationError("openai", ""), true},
		{"raw truncated tool-call, un-converted", NewStreamError("ollama", `llama-server returned invalid tool call arguments for "write_file": unexpected end of JSON input`), true},
		{"stream context-length reject", NewStreamError("ollama", "input length exceeds context length of 131072"), true},
		{"stream maximum-context reject", NewStreamError("openai", "This model's maximum context length is 8192 tokens"), true},
		{"stream prompt-too-long", NewStreamError("anthropic", "prompt is too long"), true},
		{"model-not-found is not overflow", NewStreamError("ollama", "model not found, try pulling it"), false},
		{"malformed is not overflow", NewStreamError("ollama", "malformed request"), false},
		{"transient crash is not overflow", NewStreamError("ollama", "the model unexpectedly stopped"), false},
		{"header timeout is not overflow", NewResponseHeaderTimeoutError("ollama", errors.New("net/http: timeout awaiting response headers")), false},
		{"plain HTTP 500 is not overflow", NewHTTPError("anthropic", 500, "", "internal error"), false},
		{"non-provider error", errors.New("something else"), false},
		{"nil error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsContextOverflowError(tc.err); got != tc.want {
				t.Errorf("IsContextOverflowError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsContextOverflowError_Wrapped confirms the classifier looks through a
// wrapping error via errors.As, since engine.Run can return the provider error
// wrapped with context.
func TestIsContextOverflowError_Wrapped(t *testing.T) {
	inner := NewContextTruncationError("ollama", "unexpected end of JSON input")
	wrapped := fmt.Errorf("engine run failed: %w", inner)
	if !IsContextOverflowError(wrapped) {
		t.Error("IsContextOverflowError must see through a wrapped APIError")
	}
}
