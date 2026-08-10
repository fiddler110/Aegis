package config

import (
	"strings"
	"testing"
	"time"
)

// TestMaxTurnStall covers the P39.17 accessor's contract. It mirrors
// MaxWallClockPerRun's — seconds in, duration out, non-positive means "no bound"
// — with one deliberate difference this test is here to pin: the *default* is
// applied by the defaults layer, not by the accessor, so a user who explicitly
// writes `max_turn_stall: 0` genuinely gets the detector off instead of silently
// getting 900 back.
func TestMaxTurnStall(t *testing.T) {
	for _, tc := range []struct {
		name string
		sec  int
		want time.Duration
	}{
		{"explicitly disabled", 0, 0},
		{"negative is not a bound", -30, 0},
		{"seconds become a duration", 900, 15 * time.Minute},
		{"a tighter operator value is honoured", 120, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CostConfig{MaxTurnStallSec: tc.sec}.MaxTurnStall()
			if got != tc.want {
				t.Errorf("MaxTurnStall() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTurnStallDefaultIsOn is the load-bearing half of P39.17's "unlike every
// other cost bound, this one ships enabled" claim. If the defaults map ever
// loses the key, the detector silently becomes opt-in again and the hang it
// exists to catch goes back to being invisible — with no test failing anywhere
// else, because every other test constructs CostConfig directly.
func TestTurnStallDefaultIsOn(t *testing.T) {
	got, ok := defaults()["cost.max_turn_stall"]
	if !ok {
		t.Fatal("cost.max_turn_stall has no default — the stall detector ships disabled")
	}
	if got != DefaultMaxTurnStallSec {
		t.Fatalf("cost.max_turn_stall default = %v, want %d", got, DefaultMaxTurnStallSec)
	}
	// The value has to clear every narrower timeout it is a backstop for
	// (provider.stream_idle_timeout, the shell tool's 600s ceiling, cron's
	// 10-minute bound — all 10 minutes), or it would pre-empt their precise,
	// locally-reported failures with a vague one.
	if d := (CostConfig{MaxTurnStallSec: DefaultMaxTurnStallSec}).MaxTurnStall(); d <= 10*time.Minute {
		t.Errorf("the default stall bound (%v) must sit above the 10-minute layer timeouts it backstops", d)
	}
}

// TestHardenPreservesTurnStallBound is the same footgun TestHardenPreservesWallClockBound
// guards, with the opposite failure direction. patchCost splices in a freshly
// built cost block, so a key buildCostBlock does not write is erased — but here
// erasure is *safe* (the default comes back) while writing a stray 0 is not, since
// 0 disables a safety default the caller never asked to touch. Both halves are
// asserted.
func TestHardenPreservesTurnStallBound(t *testing.T) {
	cfg := &Config{}
	cfg.Cost.MaxTurnStallSec = 300

	plan := ComputeHardenPlan(cfg)
	if plan.Cost.MaxTurnStallSec != 300 {
		t.Errorf("harden dropped the user's stall bound: got %d, want 300", plan.Cost.MaxTurnStallSec)
	}
	if block := buildCostBlock(plan.Cost); !strings.Contains(block, "max_turn_stall: 300") {
		t.Errorf("rewritten cost block lost the stall bound:\n%s", block)
	}

	// A patch that never populated the field must not emit `max_turn_stall: 0`
	// and quietly turn the detector off.
	if block := buildCostBlock(CostPatch{SessionCapUSD: 1.5}); strings.Contains(block, "max_turn_stall") {
		t.Errorf("an unpopulated patch must omit the key rather than disable the detector:\n%s", block)
	}
}
