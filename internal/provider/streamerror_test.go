package provider

import (
	"errors"
	"testing"
)

// TestNewStreamErrorClassification pins P33.16's per-class retry policy: a
// mid-stream {"error":...} envelope from a transient/infrastructural failure is
// retryable, a deterministic one (and anything unrecognized) is terminal.
func TestNewStreamErrorClassification(t *testing.T) {
	cases := []struct {
		name          string
		msg           string
		wantRetryable bool
	}{
		// --- retryable: transient / infrastructural ---
		{"worker crash (Ollama's own wording)", "model runner has unexpectedly stopped", true},
		{"worker crashed", "worker crashed", true},
		{"runner process terminated", "llama runner process has terminated: signal: aborted", true},
		{"model load failure", "error loading model: failed to create context", true},
		{"unable to load model", "unable to load model llama3.2", true},
		{"OOM via cudaMalloc", "cudaMalloc failed: out of memory", true},
		{"OOM allocate", "failed to allocate CUDA0 buffer", true},
		{"connection reset mid-stream", "read tcp 127.0.0.1: connection reset by peer", true},
		{"connection refused", "dial tcp: connection refused", true},
		{"unknown runtime error", "an unknown error was encountered while running the model", true},

		// --- terminal: deterministic, retry wastes a prompt-eval ---
		{"context length exceeded", "context length exceeded", false},
		{"input exceeds context window", "input length exceeds the context window", false},
		{"too many tokens", "the prompt has too many tokens for this model", false},
		{"invalid request", "invalid request: messages must not be empty", false},
		{"model not found", `model "llama3" not found, try pulling it first`, false},
		{"malformed", "malformed function-call arguments", false},
		{"unsupported", "this model does not support tools", false},

		// --- terminal: unrecognized defaults to terminal (avoid wasted eval) ---
		{"raw json fallback", `{"code":500}`, false},
		{"totally unknown", "something nobody has classified yet", false},

		// --- terminal wins over retryable when both signals present ---
		{"crash text but context-overflow cause", "worker crashed: context length exceeded", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewStreamError("ollama", tc.msg)

			// The classification must be reachable through the same seam the
			// retry layer uses: errors.As(&APIError) then Retryable().
			var apiErr *APIError
			if !errors.As(error(err), &apiErr) {
				t.Fatalf("NewStreamError should produce an *APIError")
			}
			if got := apiErr.Retryable(); got != tc.wantRetryable {
				t.Errorf("Retryable() = %v, want %v for %q", got, tc.wantRetryable, tc.msg)
			}
			// retryable() (the retry loop's helper) must agree.
			if got := retryable(err); got != tc.wantRetryable {
				t.Errorf("retryable(err) = %v, want %v for %q", got, tc.wantRetryable, tc.msg)
			}
		})
	}
}

// TestNewStreamErrorRendersVerbatim guards the user-facing message: a stream
// error prints "<provider>: <msg>" exactly, so classification does not change
// what the TUI shows (and the adapter TestMidStreamError golden strings hold).
func TestNewStreamErrorRendersVerbatim(t *testing.T) {
	err := NewStreamError("openai", "model runner has unexpectedly stopped")
	if got, want := err.Error(), "openai: model runner has unexpectedly stopped"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// A terminal one renders identically — only retryability differs.
	term := NewStreamError("ollama", "context length exceeded")
	if got, want := term.Error(), "ollama: context length exceeded"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if term.Retryable() {
		t.Errorf("context overflow must be terminal")
	}
}
