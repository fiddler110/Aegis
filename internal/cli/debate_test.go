package cli

import (
	"encoding/json"
	"testing"
)

func TestDebateCLIResultRoundTrip(t *testing.T) {
	line, err := json.Marshal(debateCLIResult{
		Report: "=== Claim ===\nx", Verdict: "REVISE", Confidence: "high", CostUSD: 0.02, Turns: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	var res debateCLIResult
	if err := json.Unmarshal(line, &res); err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "REVISE" || res.Confidence != "high" || res.Turns != 3 {
		t.Errorf("roundtrip mismatch: %+v", res)
	}
}

// TestNewDebateCmdRequiresClaimArg checks the cobra arg validation (at least
// one positional claim argument) without executing RunE, which would need a
// live model provider.
func TestNewDebateCmdRequiresClaimArg(t *testing.T) {
	cmd := newDebateCmd()
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("expected an error for zero claim arguments")
	}
	if err := cmd.Args(cmd, []string{"a claim"}); err != nil {
		t.Errorf("unexpected error for one claim argument: %v", err)
	}
}
