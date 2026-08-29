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
	// ColdCacheAfter is the P67.6 idle gap that triggers the cold-cache clear.
	// It rides here for the reason the three non-`cost:` fields above do: it is a
	// per-run bound an operator sets once and expects every entry point to
	// honor, and a field on this struct reaches all of them.
	ColdCacheAfter time.Duration
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
		ColdCacheAfter:           coldCacheAfter(cfg),
	}
}

// coldCacheAfter resolves compaction.cold_cache_after, falling back to the
// package default on an unparseable value. The *warning* for that case belongs
// at startup where there is a logger (see internal/server), not here — this
// function is called per engine construction and would repeat it every run.
func coldCacheAfter(cfg *config.Config) time.Duration {
	d, _ := cfg.Compaction.ColdCacheAfterOr()
	return d
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
	o.ColdCacheAfter = l.ColdCacheAfter
}

// WithBudgetOverride returns a copy whose spend and context-token bounds are
// replaced wholesale by a caller-computed per-agent allowance.
//
// Both values are taken as given, including zero: a swarm share is computed
// from what the parent has already spent, and "this teammate gets no dollar
// allowance because the provider is unpriced" is a real answer that must not
// silently fall back to the operator's whole-run figure. Callers that mean
// "use mine only if I have one" want WithRemainingAllowance below.
func (l Limits) WithBudgetOverride(usd float64, tokens int) Limits {
	l.BudgetUSD = usd
	l.MaxTokensPerRun = tokens
	return l
}

// WithRemainingAllowance returns a copy in which each bound is replaced only
// when the caller actually has a figure for it (> 0), leaving the configured
// value in place otherwise.
//
// This is the subprocess worker's shape: a WorkerSpec carries whatever the
// parent had left at spawn time, and an absent (zero) field means "the parent
// did not compute one", not "unbounded".
func (l Limits) WithRemainingAllowance(usd float64, tokens int) Limits {
	if usd > 0 {
		l.BudgetUSD = usd
	}
	if tokens > 0 {
		l.MaxTokensPerRun = tokens
	}
	return l
}
