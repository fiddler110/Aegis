package modelcatalog

import (
	"testing"

	"github.com/fiddler110/aegis/internal/hwinfo"
)

func TestCuratedNonEmptyAndValid(t *testing.T) {
	all := Curated()
	if len(all) < 4 {
		t.Fatalf("curated list too small: %d", len(all))
	}
	for _, m := range all {
		if m.Provider == "" || m.ID == "" || m.Notes == "" {
			t.Errorf("incomplete entry: %+v", m)
		}
		switch m.Tier {
		case TierFrontier, TierBalanced, TierLocal:
		default:
			t.Errorf("invalid tier %q for %s", m.Tier, m.ID)
		}
	}
}

func TestForTier(t *testing.T) {
	if len(ForTier(TierLocal)) == 0 {
		t.Error("expected at least one local model")
	}
	if len(ForTier(TierFrontier)) == 0 {
		t.Error("expected at least one frontier model")
	}
	for _, m := range ForTier(TierLocal) {
		if m.Tier != TierLocal {
			t.Errorf("ForTier(local) returned %q", m.Tier)
		}
	}
}

func TestRecommendLocal(t *testing.T) {
	allLocal := ForTier(TierLocal)

	cases := []struct {
		name string
		hw   hwinfo.Info
		// want is checked as: every returned model's MinRAMGB must be <=
		// the hardware's RAM (when known), and the count must match exactly
		// so the test also catches over- or under-narrowing.
		wantCount int
		wantAll   bool // when true, expect every curated local entry back
	}{
		{
			name:    "RAM unknown returns full unnarrowed list",
			hw:      hwinfo.Info{CPUCores: 8, RAMSource: hwinfo.SourceUnknown},
			wantAll: true,
		},
		{
			name:      "very low RAM (2GB) excludes everything with a floor above it",
			hw:        hwinfo.Info{CPUCores: 4, RAMSource: hwinfo.SourceProcMeminfo, TotalRAMBytes: 2 << 30},
			wantCount: countAtOrBelow(allLocal, 2),
		},
		{
			name:      "6GB: small/quantized-friendly families only",
			hw:        hwinfo.Info{CPUCores: 8, RAMSource: hwinfo.SourceProcMeminfo, TotalRAMBytes: 6 << 30},
			wantCount: countAtOrBelow(allLocal, 6),
		},
		{
			name:      "12GB: most entries except the heaviest reasoning model",
			hw:        hwinfo.Info{CPUCores: 8, RAMSource: hwinfo.SourceSysctl, TotalRAMBytes: 12 << 30},
			wantCount: countAtOrBelow(allLocal, 12),
		},
		{
			name:    "32GB: full local catalog",
			hw:      hwinfo.Info{CPUCores: 16, RAMSource: hwinfo.SourceWinAPI, TotalRAMBytes: 32 << 30},
			wantAll: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RecommendLocal(tc.hw)
			wantCount := tc.wantCount
			if tc.wantAll {
				wantCount = len(allLocal)
			}
			if len(got) != wantCount {
				t.Fatalf("RecommendLocal() returned %d entries, want %d (got: %+v)", len(got), wantCount, got)
			}
			for _, m := range got {
				if m.Tier != TierLocal {
					t.Errorf("RecommendLocal returned non-local entry %+v", m)
				}
				if tc.hw.RAMKnown() && float64(m.MinRAMGB) > tc.hw.TotalRAMGB() {
					t.Errorf("RecommendLocal returned %s (MinRAMGB=%d) for %.0fGB RAM", m.ID, m.MinRAMGB, tc.hw.TotalRAMGB())
				}
			}
		})
	}
}

// TestRecommendLocalNeverExceedsRAMFloorAcrossKnownGBValues is a small sweep
// double-checking monotonicity: recommending at a higher RAM value should
// never return fewer models than a lower one.
func TestRecommendLocalMonotonic(t *testing.T) {
	prev := -1
	for _, gb := range []int{1, 2, 4, 6, 8, 12, 16, 24, 64} {
		hw := hwinfo.Info{RAMSource: hwinfo.SourceProcMeminfo, TotalRAMBytes: uint64(gb) << 30}
		n := len(RecommendLocal(hw))
		if n < prev {
			t.Errorf("RecommendLocal shrank from %d to %d models going from a lower to %dGB RAM", prev, n, gb)
		}
		prev = n
	}
}

func countAtOrBelow(models []Model, gb int) int {
	n := 0
	for _, m := range models {
		if m.MinRAMGB <= gb {
			n++
		}
	}
	return n
}
