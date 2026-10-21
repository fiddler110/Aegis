package modelcatalog

import (
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/provider/anthropic"
)

// Capabilities is what Aegis knows about one model id from its own tables:
// the context window, the price, and how it thinks. It is the one place a
// picker, the setup wizard and the TUI's context meter ask, so the three stop
// disagreeing — the meter assumed 200k for every Claude model after the 1M
// generation shipped, while the wizard still offered Opus 4.8 as current.
//
// Static on purpose. A live listing from the provider (provider.ListModels)
// is fresher for *which* models exist and, on Anthropic, for the window; the
// caller overlays it. Price and thinking behaviour come only from here.
type Capabilities struct {
	// ContextWindow is the model's input window in tokens; 0 when unknown.
	ContextWindow int
	Pricing       cost.Pricing
	Priced        bool
	// Thinking describes how the model's thinking behaves ("" when unknown).
	Thinking string
	// EffortLevels lists accepted provider.reasoning_effort values; nil when
	// the model takes none or is unknown.
	EffortLevels []string
}

// Lookup returns what Aegis knows about model.
func Lookup(model string) Capabilities {
	var c Capabilities
	c.Pricing, c.Priced = cost.PricingFor(model)
	if s := anthropic.Summarize(model); s.Known {
		c.ContextWindow = s.ContextWindow
		c.Thinking = s.Thinking
		c.EffortLevels = s.EffortLevels
	}
	return c
}

// Annotation renders c as a short picker suffix, e.g.
// "1M ctx · $4/$20 per Mtok · adaptive, always on". Empty when nothing is known.
func (c Capabilities) Annotation() string {
	var parts []string
	if c.ContextWindow > 0 {
		parts = append(parts, FormatTokens(c.ContextWindow)+" ctx")
	}
	if c.Priced {
		parts = append(parts, fmt.Sprintf("$%s/$%s per Mtok", trimFloat(c.Pricing.Input), trimFloat(c.Pricing.Output)))
	}
	if c.Thinking != "" {
		parts = append(parts, "thinking "+c.Thinking)
	}
	return strings.Join(parts, " · ")
}

// FormatTokens renders a token count the way model cards do: 1M, 200K, 32K.
func FormatTokens(n int) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.3f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}
