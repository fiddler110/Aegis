package eval

import (
	"strings"
	"testing"
)

// TestSaturationReason covers the P63.11 guard: the live tier's prompt-size
// comparison must be able to say "the instrument saturated" instead of blaming
// the P25.6 prompt profile. These cases are the ones actually observed or
// plausibly reachable on a live run, not an exhaustive grid.
func TestSaturationReason(t *testing.T) {
	tests := []struct {
		name                         string
		localTokens, localWindow     int
		defaultTokens, defaultWindow int
		wantSaturated                bool
	}{
		{
			// The 2026-08-08 observation: qwen3:14b at Ollama's default 4096,
			// both profiles reporting num_ctx-1.
			name: "both clamped at num_ctx-1", localTokens: 4095, localWindow: 4096,
			defaultTokens: 4095, defaultWindow: 4096, wantSaturated: true,
		},
		{
			// The local profile fits and the default one does not: still
			// unusable, and in the one direction that would otherwise *pass*
			// the assertion for the wrong reason.
			name: "only the default profile clamped", localTokens: 2100, localWindow: 16384,
			defaultTokens: 16383, defaultWindow: 16384, wantSaturated: true,
		},
		{
			name: "both well inside the window", localTokens: 2100, localWindow: 16384,
			defaultTokens: 3400, defaultWindow: 16384, wantSaturated: false,
		},
		{
			// A real no-reduction result must reach the caller's own assertion
			// rather than being swallowed as an instrument problem.
			name: "no reduction, no clamp", localTokens: 3400, localWindow: 16384,
			defaultTokens: 3400, defaultWindow: 16384, wantSaturated: false,
		},
		{
			name: "identical counts with no window known", localTokens: 4095, localWindow: 0,
			defaultTokens: 4095, defaultWindow: 0, wantSaturated: true,
		},
		{
			name: "differing counts with no window known", localTokens: 2100, localWindow: 0,
			defaultTokens: 3400, defaultWindow: 0, wantSaturated: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := saturationReason(tc.localTokens, tc.localWindow, tc.defaultTokens, tc.defaultWindow)
			if (got != "") != tc.wantSaturated {
				t.Errorf("saturationReason(%d, %d, %d, %d) = %q, want saturated=%v",
					tc.localTokens, tc.localWindow, tc.defaultTokens, tc.defaultWindow, got, tc.wantSaturated)
			}
		})
	}
}

// The window guard has to distinguish three cases, and the middle one is the
// whole reason it exists: a window that is *plausible* but too small produces a
// run that looks like a confused model rather than a truncated prompt.
func TestInsufficientWindowReason(t *testing.T) {
	if why := insufficientWindowReason(32768, "modelfile"); why != "" {
		t.Errorf("a 32k window should be fine, got %q", why)
	}
	if why := insufficientWindowReason(workflowMinContextWindow, "config"); why != "" {
		t.Errorf("the floor itself must pass, got %q", why)
	}
	// An undetermined window is a non-Ollama backend or a failed detection.
	// Skipping the tier on it would be a guess dressed as a check.
	if why := insufficientWindowReason(0, ""); why != "" {
		t.Errorf("an unknown window must not block the run, got %q", why)
	}
	why := insufficientWindowReason(4096, "ollama:default")
	if why == "" {
		t.Fatal("Ollama's 4096 default is below what the task needs and must be reported")
	}
	// The message is the whole value of the skip: it has to name the number, the
	// source, and the fix that actually works before the model is loaded.
	for _, want := range []string{"4096", "ollama:default", "num_ctx", "provider.context_window", "OLLAMA_CONTEXT_LENGTH"} {
		if !strings.Contains(why, want) {
			t.Errorf("the skip message does not mention %q: %s", want, why)
		}
	}
}
