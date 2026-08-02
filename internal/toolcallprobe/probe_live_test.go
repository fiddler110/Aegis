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

	// Run as the P53.4 aggregate rather than a hand-rolled loop, so the tier
	// exercises the shipped conformance accounting against real streams too:
	// the no-verdict exclusion is only ever interesting when a real model
	// actually truncates.
	const runs = DefaultTrials
	conf, err := RunTrials(context.Background(), a, model, runs)
	if err != nil {
		t.Fatalf("probe could not run (is %s serving %s?): %v", baseURL, model, err)
	}
	if conf.Err != nil {
		t.Fatalf("sample cut short after %d trial(s): %v", conf.Trials, conf.Err)
	}
	for i, res := range conf.Results {
		t.Logf("run %d: tool_calls=%d truncated=%v", i, res.ToolCalls, res.Truncated)
	}
	t.Logf("conformance: %s", conf.Summary())
	rate, ok := conf.Rate()
	if !ok {
		t.Fatalf("all %d runs hit the %d-token cap — no verdict at all; SmokeMaxTokens is too tight for %s's reasoning preamble", conf.Trials, SmokeMaxTokens, model)
	}
	if rate < 1 {
		t.Errorf("%s called the tool on only %d of %d verdict-reaching runs — either the model is genuinely inconsistent (the conformance gap P53.4 exists to surface) or the probe is accusing a capable one (the P34.2 false positive)", model, conf.ToolCallTrials, conf.Denominator())
	}
	if conf.NoVerdict > 0 {
		t.Errorf("%d/%d runs hit the %d-token cap — SmokeMaxTokens is too tight for %s's reasoning preamble; those runs are excluded from the rate rather than counted as misses, but a truncated probe reaches no verdict at all", conf.NoVerdict, conf.Trials, SmokeMaxTokens, model)
	}
	if w := NewGate().Warning(context.Background(), a, model); w != "" {
		t.Errorf("Gate warns about a model that calls tools: %q", w)
	}
}
