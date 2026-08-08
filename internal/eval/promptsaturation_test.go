package eval

import "testing"

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
