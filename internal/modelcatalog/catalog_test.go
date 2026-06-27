package modelcatalog

import "testing"

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
