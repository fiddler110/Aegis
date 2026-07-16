//go:build live_probe

// This tier exists because the probe's correctness is a claim about real model
// behavior, and no unit test can hold it: the P34.2 false positive lived
// through a fully green suite. The scripted tests assert what the code does
// with a given stream; only a real model says whether the cap fits the way a
// reasoning model actually thinks. Run it when changing SmokeMaxTokens, the
// smoke prompt, or the verdict rules.
package toolcallprobe

import (
	"context"
	"os"
	"testing"

	"github.com/fiddler110/aegis/internal/provider/openai"
)

// TestLiveProbeReachesAVerdict is the P34.2 false-positive regression, run
// against a real model. Default qwen3:14b — the reasoning model that produced
// the observation: it calls tools reliably, but thought past the original
// 256-token cap on 3 of 5 runs, so the probe cached "Unsupported" and warned
// on every session for the daemon's whole life. At the shipped cap it is 5/5
// clean; a run that does truncate must report Truncated rather than a verdict.
func TestLiveProbeReachesAVerdict(t *testing.T) {
	model := os.Getenv("AEGIS_LIVE_PROBE_MODEL")
	if model == "" {
		model = "qwen3:14b"
	}
	baseURL := os.Getenv("AEGIS_LIVE_PROBE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	a := openai.New("ollama", openai.WithBaseURL(baseURL))

	const runs = 5
	var truncated int
	for i := 0; i < runs; i++ {
		res, err := Run(context.Background(), a, model)
		if err != nil {
			t.Fatalf("run %d: probe could not run (is %s serving %s?): %v", i, baseURL, model, err)
		}
		t.Logf("run %d: tool_calls=%d truncated=%v", i, res.ToolCalls, res.Truncated)
		if res.ToolCalls == 0 && !res.Truncated {
			t.Errorf("run %d: %s finished its turn without a tool call — either the model genuinely can't call tools, or the probe is accusing a capable one (the P34.2 false positive)", i, model)
		}
		if res.Truncated {
			truncated++
		}
	}
	if truncated > 0 {
		t.Errorf("%d/%d runs hit the %d-token cap — SmokeMaxTokens is too tight for %s's reasoning preamble; the guard keeps this from becoming a false accusation, but a truncated probe reaches no verdict at all", truncated, runs, SmokeMaxTokens, model)
	}
	if w := NewGate().Warning(context.Background(), a, model); w != "" {
		t.Errorf("Gate warns about a model that calls tools: %q", w)
	}
}
