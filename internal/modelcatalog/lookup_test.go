package modelcatalog

import "testing"

func TestLookupClaude(t *testing.T) {
	c := Lookup("claude-opus-5-5")
	if c.ContextWindow != 1_000_000 || !c.Priced || c.Pricing.Input != 4 || c.Thinking != "adaptive, always on" {
		t.Errorf("Lookup(opus 5.5) = %+v", c)
	}
	if got := c.Annotation(); got != "1M ctx · $4/$20 per Mtok · thinking adaptive, always on" {
		t.Errorf("annotation = %q", got)
	}
}

func TestLookupUnknownIsEmpty(t *testing.T) {
	if c := Lookup("some-local-model:7b"); c.ContextWindow != 0 || c.Priced || c.Annotation() != "" {
		t.Errorf("unknown model = %+v", c)
	}
}

func TestCuratedClaudeModelsAreAllKnown(t *testing.T) {
	// A curated id the capability table does not recognize is a stale entry.
	for _, m := range Curated() {
		if m.Provider != "anthropic" {
			continue
		}
		if c := Lookup(m.ID); c.ContextWindow == 0 || !c.Priced {
			t.Errorf("curated %s is missing from the capability or price table: %+v", m.ID, c)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	for n, want := range map[int]string{1_000_000: "1M", 200_000: "200K", 1_500_000: "1.5M", 512: "512"} {
		if got := FormatTokens(n); got != want {
			t.Errorf("FormatTokens(%d) = %q want %q", n, got, want)
		}
	}
}
