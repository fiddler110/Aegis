package enginecfg

import (
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/engine"
)

// Limits are the per-run bounds from the `cost:` config block. They travel as a
// struct rather than as five fields copied at each engine.New so that adding a
// bound reaches every entry point — ARCH-06 found `aegis chat` silently ignoring
// max_iterations, loop_threshold and redact_secrets for the same reason the gate
// stack had drifted: nobody revisited the CLI when the knob shipped.
//
// MaxIterations, LoopThreshold and RedactSecrets are not `cost:` settings, but
// they are the same kind of thing — a per-run bound an operator configured once
// and expects every path to honor — so they ride along here rather than being
// left as the next three fields to be forgotten.
type Limits struct {
	BudgetUSD                float64
	MaxTokensPerRun          int
	MaxGeneratedTokensPerRun int
	MaxWallClockPerRun       time.Duration
	MaxTurnStall             time.Duration
	MaxIterations            int
	LoopThreshold            int
	RedactSecrets            bool
}

// CostLimits reads the run bounds out of config.
func CostLimits(cfg *config.Config) Limits {
	if cfg == nil {
		return Limits{}
	}
	return Limits{
		BudgetUSD:                cfg.Cost.BudgetUSD,
		MaxTokensPerRun:          cfg.Cost.MaxTokensPerRun,
		MaxGeneratedTokensPerRun: cfg.Cost.MaxGeneratedTokensPerRun,
		MaxWallClockPerRun:       cfg.Cost.MaxWallClockPerRun(),
		MaxTurnStall:             cfg.Cost.MaxTurnStall(),
		MaxIterations:            cfg.Provider.MaxIterations,
		LoopThreshold:            cfg.Provider.LoopThreshold,
		RedactSecrets:            cfg.Security.RedactSecrets,
	}
}

// WithoutContextTokenCap returns a copy with MaxTokensPerRun cleared.
//
// The phased skill drive is the one caller that wants this (P59.4): its whole
// design is a fresh context per phase, so a cap denominated in cumulative
// context tokens fires on a run that is behaving exactly as intended. Every
// other bound — spend, generated tokens, wall clock, stall — still applies, and
// dropping this one is now a named, single-line decision at the call site rather
// than a field quietly missing from a 20-field literal.
func (l Limits) WithoutContextTokenCap() Limits {
	l.MaxTokensPerRun = 0
	return l
}

// Apply writes the limits into an engine.Options under construction. It only
// ever sets the fields it owns, so a caller can fill the rest of the struct in
// any order around it.
func (l Limits) Apply(o *engine.Options) {
	o.BudgetUSD = l.BudgetUSD
	o.MaxTokensPerRun = l.MaxTokensPerRun
	o.MaxGeneratedTokensPerRun = l.MaxGeneratedTokensPerRun
	o.MaxWallClockPerRun = l.MaxWallClockPerRun
	o.MaxTurnStall = l.MaxTurnStall
	o.MaxIterations = l.MaxIterations
	o.LoopThreshold = l.LoopThreshold
	o.RedactSecrets = l.RedactSecrets
}
