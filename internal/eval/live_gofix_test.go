//go:build live_workflow

package eval

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveGoFixRepeated (P88.7) runs GoFixTask through Aegis N times against
// one model and reports pass rate, mean score, calls, tokens and elapsed —
// the measurement P88.1–P88.6 were filed without. One run per configuration
// is what the 2026-10-01 trial had, and it could not separate a sampling
// effect from luck.
//
// Compare configurations by running this once per configuration with the
// same N and reading the summary lines side by side:
//
//	AEGIS_EVAL_MODEL=aegis-qwen35-9b:32k AEGIS_EVAL_RUNS=5 \
//	  go test -tags live_workflow -count=1 ./internal/eval/... -run TestLiveGoFixRepeated -v
//
// It reports and never fails on a weak model: the numbers are the result.
// It fails only when the harness itself could not run.
func TestLiveGoFixRepeated(t *testing.T) {
	requireGo(t)
	baseURL := os.Getenv("AEGIS_EVAL_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("AEGIS_EVAL_MODEL")
	if model == "" {
		t.Skip("set AEGIS_EVAL_MODEL to the model under test")
	}
	runs := 3
	if v, err := strconv.Atoi(os.Getenv("AEGIS_EVAL_RUNS")); err == nil && v > 0 {
		runs = v
	}

	task := GoFixTask()
	var passed, earned, possible, calls, in, out, empty int
	var elapsed time.Duration
	for i := 0; i < runs; i++ {
		dir := taskDir(t, fmt.Sprintf("gofix-%d", i))
		o := runTaskUnderAegis(t, task, dir, "", baseURL, model)
		if o.Err != nil && o.Score.Possible == 0 {
			t.Fatalf("run %d could not execute: %v", i+1, o.Err)
		}
		t.Logf("run %d/%d: %s\n    calls %d · tokens in %d out %d · empty reply %v\n    %s",
			i+1, runs, o, o.ToolCalls, o.InputTokens, o.OutputTokens, o.EmptyReply, o.Score.Table())
		if o.Passed {
			passed++
		}
		if o.EmptyReply {
			empty++
		}
		earned += o.Score.Earned
		possible += o.Score.Possible
		calls += o.ToolCalls
		in += o.InputTokens
		out += o.OutputTokens
		elapsed += o.Elapsed
	}
	n := time.Duration(runs)
	t.Logf("SUMMARY model=%s runs=%d pass=%d/%d score=%d/%d mean_calls=%.1f mean_tokens_in=%d mean_tokens_out=%d empty_replies=%d mean_elapsed=%s",
		model, runs, passed, runs, earned, possible, float64(calls)/float64(runs), in/runs, out/runs, empty, (elapsed / n).Round(time.Second))
}
