package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/fiddler110/aegis/internal/cost"
)

// runBudget is the first concern lifted out of Engine.Run under P63.9: the four
// independent stop conditions (cost, context tokens, generated tokens, wall
// clock) and the one piece of state they share.
//
// The extraction is not code motion. Run is a 725-line scope where budget
// enforcement, compaction, nudge retraction, loop detection and guard retries
// all interact, and the question P63.9 exists to answer for each concern is
// *what state does it actually own*. For budgets the answer is unusually clean,
// which is why this one goes first:
//
//   - Three of the four bounds (budgetUSD, the two token caps) are pure reads of
//     the cost tracker. They own nothing.
//   - The fourth owns exactly one thing — the run's start instant. Before this
//     it was a bare local in Run, `runStart`, threaded by hand into five calls
//     across 600 lines. That thread is the whole of the concern's run-scoped
//     state, and once it is a field there is nothing left in Run to interact
//     with.
//
// The bound is deliberately per-Run rather than per-Engine: in the phased drive
// (internal/drive) a Run is one phase turn, which is the unit the drive resets
// around. One global cap would guillotine a long build mid-phase.
//
// A sub-agent inherits the parent's bound whole rather than a divided share,
// since elapsed time is not additive across concurrent teammates the way spend
// is — that decision lives in the caller, not here.
type runBudget struct {
	usd       float64
	tokens    int
	genTokens int
	wallClock time.Duration
	tracker   *cost.Tracker
	now       func() time.Time
	start     time.Time
}

// newRunBudget captures the bounds configured on e and starts the clock.
//
// The baseline is taken here — before compaction, not at the loop — because a
// compaction pass is itself a model call that can take real time on a local
// backend, and that is time the operator's bound was meant to cover.
func (e *Engine) newRunBudget() *runBudget {
	clock := e.now
	if clock == nil {
		clock = time.Now
	}
	return &runBudget{
		usd:       e.budgetUSD,
		tokens:    e.maxTokensPerRun,
		genTokens: e.maxGenTokens,
		wallClock: e.maxWallClock,
		tracker:   e.cost,
		now:       clock,
		start:     clock(),
	}
}

// deadline attaches the wall-clock bound to ctx as a real deadline (P59.2),
// returning ctx unchanged and a no-op cancel when no bound is set.
//
// The polls in exceeded cannot fire *during* a turn, which is exactly where a
// local backend hangs — so "don't spend more than N minutes on this" was
// structurally unable to hold on the one class of stall it was asked to cover.
// The polls stay: they produce the explanatory abort error, and they are what a
// fake clock in tests drives. This only ensures a wedged call actually returns
// so a poll can run.
func (b *runBudget) deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if b.wallClock <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, b.wallClock)
}

// spendBounded reports whether this run has a bound that only the *end* of a
// turn can decide: a USD cap or either token cap, all three of which are read
// off usage the provider does not report until the turn is done.
//
// P67.7 needs it. Dispatching a tool call while the turn is still streaming
// moves that call to *before* the pre-tool-round gate, and the gate's whole
// reason for existing (see exceeded) is that a turn which itself pushes spend
// over the cap must stop before its tool calls run, not one iteration later. A
// call started mid-stream cannot honor a bound that is not yet decidable, so
// when one is configured the engine does not start calls mid-stream at all.
//
// The wall-clock bound is deliberately absent: it is attached to the context as
// a real deadline (see deadline), so it already reaches a running tool round and
// needs no gate to be honored. Same for the P39.17 stall watch.
func (b *runBudget) spendBounded() bool {
	return b.usd > 0 || b.tokens > 0 || b.genTokens > 0
}

// exceeded reports the first bound this run has crossed, or nil.
//
// Both gates in Run — before each model turn and before each tool round — call
// this same method, which is the property the old inline form kept losing. The
// pre-turn gate exists because a guard corrective retry, a max-token
// continuation and a plain text-only turn are all billed exactly like any other
// turn, so gating only the tool-round path (the P9 dead zone) let a run cycling
// through guard failures burn all the way to the iteration cap without the
// budget ever aborting it. The pre-tool-round gate exists because a turn that
// itself pushes spend over the cap must stop before its tool calls — and their
// side effects — run, not one iteration later.
//
// Check order is observable and is preserved: a run past two bounds at once
// reports cost, then tokens, then time. Cost first because it is the one bound
// denominated in the units the operator set it in.
func (b *runBudget) exceeded() error {
	if err := b.costExceeded(); err != nil {
		return err
	}
	if err := b.tokensExceeded(); err != nil {
		return err
	}
	return b.wallClockExceeded()
}

// costExceeded checks the USD bound. It is a no-op for unpriced local usage,
// which is why it is not the only budget.
func (b *runBudget) costExceeded() error {
	if b.usd <= 0 || b.tracker == nil {
		return nil
	}
	if spent := b.tracker.TotalUSD(); spent >= b.usd {
		return fmt.Errorf("engine: cost budget reached: spent $%.4f of $%.2f limit", spent, b.usd)
	}
	return nil
}

// tokensExceeded checks both token bounds. They are checked together because
// the message for the first only makes sense alongside the second (P59.4): a
// user who set max_tokens_per_run as a *work* budget needs to be told, at the
// moment it fires, that the number it reports is not the quantity they were
// thinking of and which key is.
func (b *runBudget) tokensExceeded() error {
	if b.tracker == nil {
		return nil
	}
	if b.genTokens > 0 {
		if gen := b.tracker.TotalGeneratedTokens(); gen >= b.genTokens {
			return fmt.Errorf("engine: generation budget reached: the model generated %d of %d tokens (cost.max_generated_tokens_per_run)",
				gen, b.genTokens)
		}
	}
	if b.tokens > 0 {
		if total := b.tracker.TotalTokens(); total >= b.tokens {
			return fmt.Errorf("engine: token budget reached: counted %d of %d tokens (cost.max_tokens_per_run). "+
				"That count includes the whole prompt on every turn, so it grows with conversation length as well as with work done — "+
				"to bound generated output instead, set cost.max_generated_tokens_per_run",
				total, b.tokens)
		}
	}
	return nil
}

// wallClockExceeded reports whether this run has outlived its time bound
// (P52.15).
func (b *runBudget) wallClockExceeded() error {
	if b.wallClock <= 0 {
		return nil
	}
	elapsed := b.now().Sub(b.start)
	if elapsed < b.wallClock {
		return nil
	}
	return fmt.Errorf("%w: ran %s of a %s limit — raise cost.max_wall_clock_per_run for longer tasks",
		ErrWallClockLimit, elapsed.Round(time.Second), b.wallClock)
}

// override re-attributes an error produced by cancellation when this budget's
// own deadline is what fired. Without it the abort would surface as whatever the
// cancelled call happened to return — a bare ErrInterrupted, or an adapter
// transport error a caller classifies as backend-unavailable and would then wait
// for and resume, turning a deliberate budget stop into a retry loop. err is
// returned unchanged when the run is still inside its budget, so an ordinary
// interrupt keeps its own identity.
func (b *runBudget) override(err error) error {
	if wcErr := b.wallClockExceeded(); wcErr != nil {
		return wcErr
	}
	return err
}
