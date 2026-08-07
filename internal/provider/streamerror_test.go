package provider

import (
	"errors"
	"strings"
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

// TestStreamClassifiersAgree is the P61.7(b) regression. Retryable() and
// IsBackendUnavailableError read the same free-form server string, and before
// this fix they read it through different tables in different orders:
// classifyStreamError scanned terminalStreamSignals first and returned "do not
// retry" on a hit, while IsBackendUnavailableError scanned
// backendDeadStreamSignals with no terminal check at all. A crash message with
// a generation fragment or a driver note appended therefore came back as
// terminal-and-final AND as a dead backend at once — two verdicts whose
// recoveries contradict each other, reachable with no prompt injection
// whatsoever, since any server that appends detail to a crash message produces
// it.
//
// The rule pinned here is that exactly one class wins, and the same one for
// both functions. The three strings in the middle group are the measured
// defect; the rest fix the ladder's boundaries so a later re-ordering has to
// argue with the table rather than slip through.
func TestStreamClassifiersAgree(t *testing.T) {
	cases := []struct {
		name          string
		msg           string
		wantRetryable bool
		wantDead      bool
		wantOverflow  bool
	}{
		// --- the measured defect: a crash message carrying extra text ---
		// The bare crash is the control; appending a terminal-vocabulary word
		// must not change any verdict, because it says nothing about whether
		// the runner process is still there.
		{"bare runner crash (control)",
			"model runner has unexpectedly stopped", true, true, false},
		{"runner crash + echoed 'unsupported'",
			"model runner has unexpectedly stopped (last output: the tool is unsupported)", true, true, false},
		{"runner crash + echoed 'malformed'",
			"model runner has unexpectedly stopped (prompt: describe a malformed cert)", true, true, false},

		// --- the one terminal class that outranks a crash: size ---
		// An oversized prefill explains the death; waiting for the backend and
		// re-sending it unchanged kills it again, so this stays terminal and
		// routes to P47.2's fresh-context reset instead.
		{"crash caused by context overflow",
			"worker crashed: context length exceeded", false, false, true},
		{"runner stopped, prompt too long",
			"model runner has unexpectedly stopped: prompt is too long", false, false, true},

		// A raw truncated-tool-call signature is an overflow too, and stays one
		// even when the server appends a crash phrase. Found by probing the
		// invariants below against inputs outside this table: the truncation
		// signature used to be recognized only in IsContextOverflowError's tail,
		// outside the ladder, so this row returned dead AND overflow at once.
		{"raw truncated tool call",
			"invalid tool call arguments: unexpected end of JSON input", false, false, true},
		{"raw truncated tool call + appended crash phrase",
			"invalid tool call arguments: unexpected end of JSON input; connection reset by peer", false, false, true},

		// --- unchanged boundaries ---
		{"terminal only", "this model does not support tools", false, false, false},
		{"backend dead only", "connection reset by peer", true, true, false},
		{"live-but-slow is retryable, not dead", "i/o timeout reading response", true, false, false},
		{"overflow only", "input length exceeds the context window", false, false, true},
		{"unrecognized is terminal and nothing else", "something nobody has classified yet", false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewStreamError("ollama", tc.msg)
			if got := err.Retryable(); got != tc.wantRetryable {
				t.Errorf("Retryable() = %v, want %v for %q", got, tc.wantRetryable, tc.msg)
			}
			if got := IsBackendUnavailableError(err); got != tc.wantDead {
				t.Errorf("IsBackendUnavailableError() = %v, want %v for %q", got, tc.wantDead, tc.msg)
			}
			if got := IsContextOverflowError(err); got != tc.wantOverflow {
				t.Errorf("IsContextOverflowError() = %v, want %v for %q", got, tc.wantOverflow, tc.msg)
			}
			// The coherence invariants, stated independently of the table so
			// they hold for any row added later:
			//
			//  - "the backend is dead" implies "worth another attempt". The
			//    attempt fails at connect for pennies while the server is down,
			//    and it is what lets the retry decorator cover a blip P50.1's
			//    wait would otherwise have to.
			//  - backend-dead and context-overflow are mutually exclusive,
			//    because drive.recoverBackendDown runs BEFORE the overflow
			//    check and would swallow an overflow into a liveness wait.
			if IsBackendUnavailableError(err) && !err.Retryable() {
				t.Errorf("%q: a dead backend must not also be terminal-and-final", tc.msg)
			}
			if IsBackendUnavailableError(err) && IsContextOverflowError(err) {
				t.Errorf("%q: backend-dead and context-overflow recoveries contradict; only one may claim it", tc.msg)
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

// TestModelAuthoredTextDoesNotSteerClassification is the P61.7 regression: the
// signal tables in errors.go are substring matches over Message, so any
// model-authored text spliced into Message hands a generation control over
// whether a failure is retried, treated as a dead backend, or treated as a
// context overflow. The tool name in a malformed-tool-call error is exactly
// such text, and it needed no adversarial prompt — a plainly-named
// `crash_report` tool was enough.
//
// The rule this pins is INVARIANCE, not any particular verdict: for a fixed
// failure class every verdict must be identical whatever the model named its
// tool, while the name still reaches the user via Detail. Stating it as
// invariance keeps the test about P61.7 and leaves the underlying policy free
// to change — notably IsContextOverflowError, which answers true here for
// every name including the honest control, because the message carries P47.x's
// raw truncated-tool-call signature. That is a policy question about malformed
// vs. truncated calls, unrelated to who authored the text, and deliberately
// out of scope for this fix.
func TestModelAuthoredTextDoesNotSteerClassification(t *testing.T) {
	names := []string{
		"crash_report",       // plausible real tool; hits "crash"
		"analyze_crash_dump", // ditto
		"timed out",          // hallucinated, hits a retryable signal
		"out of memory",      // ditto
		"connection reset",   // ditto
		"unsupported",        // hits a terminal signal
		"context length",     // hits a context-overflow signal
		"model runner has unexpectedly stopped",
	}
	// The control is an honest name matching no signal in any table.
	const control = "write_file"
	base := NewMalformedToolCallError("openai", control)
	wantRetry, wantDead, wantOverflow :=
		base.Retryable(), IsBackendUnavailableError(base), IsContextOverflowError(base)

	// Whatever the policy is, a malformed call must not be retried or mistaken
	// for a dead backend — pin those two absolutely, since they are what the
	// measured defect flipped.
	if wantRetry {
		t.Fatal("a malformed tool call must be terminal")
	}
	if wantDead {
		t.Fatal("a malformed tool call must not read as a dead backend")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			err := NewMalformedToolCallError("openai", name)
			if got := err.Retryable(); got != wantRetry {
				t.Errorf("Retryable() = %v, want %v (control %q) — tool name steered retry", got, wantRetry, control)
			}
			if got := IsBackendUnavailableError(err); got != wantDead {
				t.Errorf("IsBackendUnavailableError() = %v, want %v (control %q) — tool name steered liveness", got, wantDead, control)
			}
			if got := IsContextOverflowError(err); got != wantOverflow {
				t.Errorf("IsContextOverflowError() = %v, want %v (control %q) — tool name steered recovery", got, wantOverflow, control)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the tool name must still reach the user: %q omits %q", err.Error(), name)
			}
		})
	}
}

// TestDetailIsNotClassified pins the field's contract directly: Detail is
// rendered and never matched. Without this, a later change that folds Detail
// back into Message for convenience would silently reopen P61.7.
func TestDetailIsNotClassified(t *testing.T) {
	err := NewStreamError("openai", "invalid tool call arguments: unexpected end of JSON input")
	err.Detail = "tool \"crash_report\", connection reset, out of memory"
	if err.Retryable() {
		t.Error("Detail must not make a terminal error retryable")
	}
	if IsBackendUnavailableError(err) {
		t.Error("Detail must not make an error read as a dead backend")
	}
	if !strings.Contains(err.Error(), "crash_report") {
		t.Errorf("Detail must render: %q", err.Error())
	}
}
