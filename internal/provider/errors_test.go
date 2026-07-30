package provider

import (
	"errors"
	"fmt"
	"testing"
)

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
