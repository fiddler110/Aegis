package toolcallprobe

import (
	"context"
	"fmt"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// DefaultTrials is the sample size the aggregate probe uses when no trial
// count is configured. Five is not arbitrary: it is the sample
// SmokeMaxTokens's own calibration used (qwen3:14b needed 124-825 completion
// tokens across five runs of this exact prompt), and it is small enough that
// the cost stays a handful of short generations rather than a benchmark run.
//
// A value of 1 reduces the aggregate to exactly one Run — the single-trial
// behavior every caller had before P53.4.
const DefaultTrials = 5

// Conformance is what N repetitions of the smoke probe observed about one
// model: not "can it ever emit a tool call" but "how often does it", which is
// the question that decides whether an unattended run survives. A model that
// complies 60% of the time passes a single-trial probe cleanly and then fails
// a long drive in a way that reads like a harness bug.
//
// The no-verdict accounting is the part that matters. Run's contract — a
// truncated trial with zero tool calls is *no verdict*, never failure — is
// preserved per trial: such trials land in NoVerdict and are excluded from the
// rate's denominator rather than counted as misses. Aggregating them as
// failures would silently re-introduce the exact P34.2 false positive the
// contract exists to prevent.
type Conformance struct {
	// Trials is how many trials produced a Result (i.e. reached the model and
	// came back). A trial aborted by a transport error is not counted here.
	Trials int
	// ToolCallTrials is how many of those trials emitted at least one tool
	// call. A trial that made a call and *then* hit the token cap still counts
	// as a call — truncation only clouds a trial that produced nothing.
	ToolCallTrials int
	// NoVerdict is how many trials hit the token cap before emitting anything:
	// zero tool calls with Truncated set. These are excluded from the rate's
	// denominator (see Rate).
	NoVerdict int
	// Results is the per-trial detail, in trial order — the same Result the
	// single-trial Run has always returned, kept so callers can report
	// truncation per trial as they do today.
	Results []Result
	// Err is the transport error that ended the aggregate early, when at least
	// one trial had already produced a Result. A partial rate from 3 of 5
	// trials is more useful than nothing, so aggregation aborts rather than
	// fails: RunTrials only returns a non-nil error when *no* trial succeeded.
	// Callers that need to know the sample was cut short read this.
	Err error
}

// Add folds one trial's Result into c, applying the per-trial no-verdict rule.
func (c *Conformance) Add(res Result) {
	c.Trials++
	c.Results = append(c.Results, res)
	switch {
	case res.ToolCalls > 0:
		c.ToolCallTrials++
	case res.Truncated:
		// Zero calls because the model ran out of tokens mid-answer: the call
		// may simply have been the next token it was going to emit. Not a
		// miss, and not part of the denominator.
		c.NoVerdict++
	}
}

// Denominator is the number of trials that reached a verdict either way —
// trials minus the ones truncated before they could say anything.
func (c Conformance) Denominator() int { return c.Trials - c.NoVerdict }

// Rate reports the fraction of verdict-reaching trials that emitted a tool
// call, and whether there is a rate at all.
//
// The bool is not decoration. When every trial was truncated there is no rate
// — reporting 0.0 would accuse a model the probe never got an answer out of,
// which is precisely the failure mode Run's contract forbids for a single
// trial. Returning (rate, ok) instead of a bare float makes "no verdict"
// impossible to mistake for "never called a tool".
func (c Conformance) Rate() (float64, bool) {
	d := c.Denominator()
	if d <= 0 {
		return 0, false
	}
	return float64(c.ToolCallTrials) / float64(d), true
}

// Verdict collapses c to the same three-way verdict a single probe reaches:
// Unknown when no trial reached a verdict, OK when at least one trial called
// the tool, Unsupported when every verdict-reaching trial answered in prose.
//
// Deliberately not a threshold on Rate: "made a tool call at least once" is
// the claim this probe can justify, and turning a partial rate into an
// Unsupported verdict is a policy decision that belongs to whatever engages a
// fallback, not to the measurement.
func (c Conformance) Verdict() Verdict {
	if c.Denominator() <= 0 {
		return Unknown
	}
	if c.ToolCallTrials > 0 {
		return OK
	}
	return Unsupported
}

// Summary is the one-line user-facing description of c: the rate when there is
// one, an explicit no-verdict note when there isn't, plus the truncation and
// cut-short detail that would otherwise be invisible.
func (c Conformance) Summary() string {
	if c.Trials == 0 {
		return "no trials ran"
	}
	var s string
	rate, ok := c.Rate()
	if !ok {
		s = fmt.Sprintf("no verdict — all %d trial(s) hit the %d-token cap before answering", c.Trials, SmokeMaxTokens)
	} else {
		s = fmt.Sprintf("%d/%d trials made a tool call (%.0f%%)", c.ToolCallTrials, c.Denominator(), rate*100)
		if c.NoVerdict > 0 {
			s += fmt.Sprintf("; %d further trial(s) reached no verdict (truncated at the %d-token cap) and are excluded", c.NoVerdict, SmokeMaxTokens)
		}
	}
	if c.Err != nil {
		s += fmt.Sprintf("; sample cut short after %d trial(s): %v", c.Trials, c.Err)
	}
	return s
}

// RunTrials runs the smoke probe trials times, sequentially, and aggregates
// the results.
//
// Sequential on purpose: a local model server serializes generation anyway, so
// concurrent probes would only distort latency and, on a memory-tight box,
// the results themselves. trials < 1 is treated as 1, so the aggregate always
// reflects at least one real probe.
//
// Transport errors: the first trial's failure is returned as an error, exactly
// as Run would — no trial succeeded, so there is nothing to report. A failure
// after at least one successful trial instead stops the loop and returns the
// partial Conformance with Err set, because a rate over 3 of 5 trials is worth
// more than discarding the sample.
func RunTrials(ctx context.Context, adapter provider.Adapter, model string, trials int) (Conformance, error) {
	var c Conformance
	if err := runTrialsInto(ctx, adapter, model, trials, 0, &c, nil); err != nil {
		return Conformance{}, err
	}
	return c, nil
}

// runTrialsInto is the shared loop behind RunTrials and the Gate's background
// refinement. perTrial, when non-zero, bounds each individual trial (the gate
// needs that; a direct caller bounds the whole call with its own context).
// each, when non-nil, is called after every successful trial so a caller can
// publish partial progress instead of waiting for the whole sample.
func runTrialsInto(ctx context.Context, adapter provider.Adapter, model string, trials int, perTrial time.Duration, c *Conformance, each func(Result)) error {
	if trials < 1 {
		trials = 1
	}
	for i := 0; i < trials; i++ {
		if err := ctx.Err(); err != nil {
			if c.Trials == 0 {
				return err
			}
			c.Err = err
			return nil
		}
		res, err := runOnce(ctx, adapter, model, perTrial)
		if err != nil {
			if c.Trials == 0 {
				return err
			}
			c.Err = err
			return nil
		}
		c.Add(res)
		if each != nil {
			each(res)
		}
	}
	return nil
}

func runOnce(ctx context.Context, adapter provider.Adapter, model string, perTrial time.Duration) (Result, error) {
	if perTrial <= 0 {
		return Run(ctx, adapter, model)
	}
	tctx, cancel := context.WithTimeout(ctx, perTrial)
	defer cancel()
	return Run(tctx, adapter, model)
}
