package config

import "time"

// CostConfig configures spend tracking.
type CostConfig struct {
	BudgetUSD float64 `koanf:"budget_usd"` // abort a run past this estimated cost; 0 = unlimited

	// MaxTokensPerRun aborts a run past this cumulative token count (input +
	// output + cache, across every turn); 0 = unlimited. The primary spend
	// guardrail (P10.5): unlike BudgetUSD, it is always enforceable because
	// token counts are present even for unpriced or local/Ollama models where
	// BudgetUSD silently never fires (estimated usage carries no dollar cost).
	// Note this is a *context* budget on any backend that reports the full
	// prompt each turn rather than a delta — see MaxGeneratedTokensPerRun.
	MaxTokensPerRun int `koanf:"max_tokens_per_run"`

	// MaxGeneratedTokensPerRun aborts a run past this cumulative *output* token
	// count; 0 = unlimited (P59.4). The work budget, where MaxTokensPerRun is
	// the context/billing budget.
	//
	// The two are separate keys on purpose. MaxTokensPerRun sums the whole
	// prompt every turn, which is precisely what a priced provider bills — but
	// on Ollama prompt_eval_count is the full prompt each turn rather than a
	// delta, so the sum grows ~O(N²) in conversation length and a cap set with
	// "how much work should this do" in mind aborts far earlier than intended.
	// Overloading one key to mean output tokens on unpriced backends and total
	// tokens elsewhere would make a single config value mean two things
	// depending on the provider; a second key says what it counts in its name
	// and leaves MaxTokensPerRun's meaning intact for everyone already using it.
	MaxGeneratedTokensPerRun int `koanf:"max_generated_tokens_per_run"`

	// MaxWallClockPerRunSec aborts a run that has been going longer than this
	// many seconds; 0 = unlimited (P52.15). The time dimension the other two
	// budgets miss entirely: BudgetUSD is a no-op for unpriced local usage and
	// MaxTokensPerRun defaults to 0, so on a slow local model a run's only real
	// bound is provider.max_iterations (40), which at ~7 tok/s can be hours.
	//
	// Off by default and deliberately so — a wall-clock cap cannot distinguish a
	// stalled run from a slow one making real progress, so any non-zero default
	// would eventually kill legitimate long work. Read via
	// MaxWallClockPerRun(), never this field directly.
	MaxWallClockPerRunSec int `koanf:"max_wall_clock_per_run"`

	// MaxTurnStallSec aborts a run whose current turn has produced no provider
	// stream event and no tool activity for this many seconds; 0 = disabled
	// (P39.17). Default DefaultMaxTurnStallSec. Read via MaxTurnStall(), never
	// this field directly.
	//
	// Unlike MaxWallClockPerRunSec above this one is ON by default, and the
	// difference is not a change of mind about time bounds — it is that the two
	// measure different things. A wall-clock cap cannot distinguish a stalled
	// run from a slow one making real progress, so it must stay opt-in. This
	// measures *silence*: no token streamed, no tool started or finished. There
	// is no legitimate long-running work that looks like that, which is why it
	// can carry a default at all.
	//
	// It is also the only bound covering the tool-execution half of a turn;
	// provider.stream_idle_timeout covers the model half at the transport, and
	// only for adapters that speak HTTP.
	MaxTurnStallSec int `koanf:"max_turn_stall"`

	// SessionCapUSD refuses to start a new turn once a session's cumulative
	// (persisted) cost reaches this amount; 0 = unlimited (P9.5).
	SessionCapUSD float64 `koanf:"session_cap_usd"`
	// DailyCapUSD refuses to start a new turn once total spend across all
	// sessions for the current UTC day reaches this amount; 0 = unlimited (P9.5).
	DailyCapUSD float64 `koanf:"daily_cap_usd"`
	// SessionTokenCap refuses to start a new turn once a session's cumulative
	// (persisted) token count reaches this amount; 0 = unlimited (P10.5). The
	// token-denominated counterpart to SessionCapUSD — always enforceable.
	SessionTokenCap int `koanf:"session_token_cap"`
	// DailyTokenCap refuses to start a new turn once total tokens across all
	// sessions for the current UTC day reaches this amount; 0 = unlimited (P10.5).
	DailyTokenCap int `koanf:"daily_token_cap"`
	// AlertThreshold is the fraction (0-1) of SessionCapUSD/DailyCapUSD/
	// SessionTokenCap/DailyTokenCap at which a warning event is surfaced to
	// the client instead of a hard stop. Only takes effect for whichever cap
	// is non-zero. Default 0.8 (P9.5).
	AlertThreshold float64 `koanf:"alert_threshold"`
}

// MaxWallClockPerRun returns the configured cost.max_wall_clock_per_run as a
// time.Duration, or 0 when unset or non-positive — which the engine reads as
// "no wall-clock bound" (P52.15).
func (c CostConfig) MaxWallClockPerRun() time.Duration {
	if c.MaxWallClockPerRunSec <= 0 {
		return 0
	}
	return time.Duration(c.MaxWallClockPerRunSec) * time.Second
}

// DefaultMaxTurnStallSec is the shipped cost.max_turn_stall (P39.17): fifteen
// minutes of complete silence before a turn is called hung.
//
// The number is chosen to be a *backstop*, not a competitor. Layer-specific
// timeouts already bound pieces of a turn, and the stall detector must not fire
// before any of them has had its chance, or it would convert a precise,
// locally-reported failure into a vague one:
//
//   - provider.stream_idle_timeout — 10 minutes (sse.DefaultStreamIdleTimeout),
//     the gap between two streamed chunks.
//   - the shell tool's per-call ceiling — 600s = 10 minutes, the longest a
//     single tool call can legitimately block with no output.
//   - the cron job timeout — 10 minutes.
//   - every other per-call tool bound, enumerated by
//     builtin.TestToolTimeoutsStayUnderTheStallBound.
//
// Fifteen minutes clears them all with margin. It is also far below the hours a
// legitimate phased drive runs for, which is the gap P39.17 was filed about: the
// 2026-08-09 hang had been silent 14 minutes when it was noticed by hand, and
// nothing in the harness would ever have noticed it.
//
// **The relation is "above every *per-call* bound", not "above every timeout",
// and the distinction is load-bearing** (P66.8 / ARCH-04). Two bounds in
// internal/tool/builtin are deliberately larger — the agent tool's workflow
// batch (up to 9 teammates) and its debate (up to 2*rounds+2 roles) — and until
// P66.8 that made this comment false rather than nuanced: 40 and 80 minutes of
// silence against a 900s bound, aborting a healthy fan-out as a fatal
// ErrTurnStalled. What makes them admissible now is that each decomposes into
// per-teammate waits capped at 10 minutes with a heartbeat between them, so an
// aggregate bound can never be *reached* without observable activity. A future
// timeout above 900s needs the same treatment or it is a regression; the
// enumerating test above is what enforces the choice.
const DefaultMaxTurnStallSec = 900

// MaxTurnStall returns cost.max_turn_stall as a time.Duration, or 0 when set to
// zero or negative — which the engine reads as "no stall detection" (P39.17).
//
// Note the asymmetry with MaxWallClockPerRun: there, 0 is the shipped state; here
// 0 is an explicit opt-out, and the default is applied by the defaults layer
// rather than here, so a user who writes `max_turn_stall: 0` genuinely gets it
// off rather than silently getting the default back.
func (c CostConfig) MaxTurnStall() time.Duration {
	if c.MaxTurnStallSec <= 0 {
		return 0
	}
	return time.Duration(c.MaxTurnStallSec) * time.Second
}
