package config

import (
	"strings"
	"testing"
	"time"
)

// TestMaxWallClockPerRun covers the P52.15 accessor's contract: seconds in,
// duration out, and anything non-positive means "no bound" — the value the
// engine reads as disabled.
func TestMaxWallClockPerRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		sec  int
		want time.Duration
	}{
		{"unset", 0, 0},
		{"negative is not a bound", -30, 0},
		{"seconds become a duration", 600, 10 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CostConfig{MaxWallClockPerRunSec: tc.sec}.MaxWallClockPerRun()
			if got != tc.want {
				t.Errorf("MaxWallClockPerRun() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHardenPreservesWallClockBound guards a footgun that comes with adding any
// new cost key: patchCost splices in a freshly built cost block, so a key that
// buildCostBlock doesn't write is erased from the user's config. `aegis harden`
// sets no wall-clock bound of its own — it is an operator preference, not a
// security control — but it must not delete one the user set.
func TestHardenPreservesWallClockBound(t *testing.T) {
	cfg := &Config{}
	cfg.Cost.MaxWallClockPerRunSec = 900

	plan := ComputeHardenPlan(cfg)

	if plan.Cost.MaxWallClockPerRunSec != 900 {
		t.Errorf("harden dropped the user's wall-clock bound: got %d, want 900", plan.Cost.MaxWallClockPerRunSec)
	}
	for _, c := range plan.CostChanges {
		if strings.Contains(c, "wall_clock") {
			t.Errorf("harden must not report a wall-clock change it did not make: %q", c)
		}
	}
	block := buildCostBlock(plan.Cost)
	if !strings.Contains(block, "max_wall_clock_per_run: 900") {
		t.Errorf("rewritten cost block lost the wall-clock bound:\n%s", block)
	}
}
