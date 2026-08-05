package engine

import "testing"

// TestCompactionTrigger (P59.1): the trigger has to reserve room for the
// *completion*, because on Ollama num_ctx is one budget covering prompt and
// completion both. The flat 85% it replaced reserved headroom for prompt growth
// only, so at a small window a large max_tokens left the model no room to
// answer — and the "continue from where you left off" recovery then grew the
// context on every retry.
func TestCompactionTrigger(t *testing.T) {
	for _, tc := range []struct {
		name      string
		window    int
		maxTokens int
		want      int
	}{
		{
			// The shipped default pair on a stock Ollama install. The old flat
			// 85% let the prompt reach 3481 of 4096, leaving ~600 tokens for a
			// 32768-token request; half the window is the floor.
			name: "shipped default on a stock window", window: 4096, maxTokens: 32768, want: 2048,
		},
		{
			// A generous window with a modest cap is unchanged: the completion
			// reservation is smaller than the old prompt-growth headroom, so
			// the 85% ceiling still binds.
			name: "large window, modest cap", window: 131072, maxTokens: 8192, want: 111411,
		},
		{
			// Equal window and cap: the min(maxTokens, window/2) clamp is what
			// stops the reservation from consuming the entire window.
			name: "cap equals window", window: 32768, maxTokens: 32768, want: 16384,
		},
		{
			// Between the two regimes the sizing — not either bound — decides.
			name: "sizing binds", window: 16384, maxTokens: 4096, want: 11469,
		},
		{
			name: "no cap configured falls back to the old ceiling", window: 8192, maxTokens: 0, want: 6963,
		},
		{
			name: "no window means no trigger", window: 0, maxTokens: 32768, want: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compactionTrigger(tc.window, tc.maxTokens); got != tc.want {
				t.Errorf("compactionTrigger(%d, %d) = %d, want %d", tc.window, tc.maxTokens, got, tc.want)
			}
		})
	}
}

// TestCompactionTriggerNeverExceedsTheOldCeiling is the regression guard: the
// change may only ever compact *earlier*, never later. Compacting later than
// 85% would be a new way to overrun a window, which is the failure the whole
// proactive-compaction path exists to prevent.
func TestCompactionTriggerNeverExceedsTheOldCeiling(t *testing.T) {
	for _, window := range []int{2048, 4096, 8192, 32768, 131072, 262144} {
		for _, maxTokens := range []int{0, 512, 4096, 32768, 131072} {
			got := compactionTrigger(window, maxTokens)
			if ceiling := window * 85 / 100; got > ceiling {
				t.Errorf("window=%d maxTokens=%d: trigger %d exceeds the old %d ceiling", window, maxTokens, got, ceiling)
			}
			if got >= window {
				t.Errorf("window=%d maxTokens=%d: trigger %d must stay inside the window", window, maxTokens, got)
			}
		}
	}
}
