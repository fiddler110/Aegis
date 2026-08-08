package toolcallprobe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// trial scripts one probe outcome for sequenceAdapter.
type trial struct {
	toolCalls int
	stop      provider.StopReason
	err       error // transport failure for this trial
}

// sequenceAdapter replays a different outcome per probe call, which is what
// separates a conformance sample from a single verdict: the interesting models
// are the ones that answer differently on different runs.
type sequenceAdapter struct {
	calls  atomic.Int32
	trials []trial
}

func (a *sequenceAdapter) Name() string { return "sequence" }

func (a *sequenceAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	n := int(a.calls.Add(1)) - 1
	if n >= len(a.trials) {
		return nil, errors.New("probed more times than the script has trials")
	}
	tr := a.trials[n]
	if tr.err != nil {
		return nil, tr.err
	}
	ch := make(chan provider.Event, tr.toolCalls+2)
	go func() {
		defer close(ch)
		for i := 0; i < tr.toolCalls; i++ {
			ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu", Name: "list_files", Input: json.RawMessage(`{"path":"."}`),
			}}
		}
		ch <- provider.Event{Type: provider.EventDone, Stop: tr.stop}
	}()
	return ch, nil
}

func called(n int) trial    { return trial{toolCalls: n, stop: provider.StopToolUse} }
func refused() trial        { return trial{toolCalls: 0, stop: provider.StopEndTurn} }
func truncated() trial      { return trial{toolCalls: 0, stop: provider.StopMaxTokens} }
func failed(e string) trial { return trial{err: errors.New(e)} }

// TestRunTrialsRate is the headline of P53.4: a model that complies some of
// the time reads as a rate, not as a pass.
func TestRunTrialsRate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		script   []trial
		wantRate float64
		wantCall int
	}{
		{"perfect", []trial{called(1), called(1), called(1), called(2), called(1)}, 1, 5},
		{"three of five", []trial{called(1), refused(), called(1), refused(), called(1)}, 0.6, 3},
		{"never", []trial{refused(), refused(), refused()}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &sequenceAdapter{trials: tc.script}
			c, err := RunTrials(context.Background(), a, "m", len(tc.script))
			if err != nil {
				t.Fatalf("RunTrials: %v", err)
			}
			if c.Trials != len(tc.script) {
				t.Errorf("Trials = %d, want %d", c.Trials, len(tc.script))
			}
			if len(c.Results) != len(tc.script) {
				t.Errorf("per-trial Results = %d, want %d", len(c.Results), len(tc.script))
			}
			if c.ToolCallTrials != tc.wantCall {
				t.Errorf("ToolCallTrials = %d, want %d", c.ToolCallTrials, tc.wantCall)
			}
			rate, ok := c.Rate()
			if !ok {
				t.Fatal("Rate reported no verdict on a sample where every trial finished its turn")
			}
			if rate != tc.wantRate {
				t.Errorf("Rate = %v, want %v", rate, tc.wantRate)
			}
			if got := int(a.calls.Load()); got != len(tc.script) {
				t.Errorf("adapter probed %d times, want %d (trials run sequentially, once each)", got, len(tc.script))
			}
		})
	}
}

// TestRunTrialsExcludesNoVerdictFromDenominator is the accounting P34.2's
// contract turns on: a truncated trial proves nothing, so it must leave the
// sample rather than count as a miss. Two calls out of three trials with one
// truncation is 2/2, not 2/3 — counting it the other way would report a
// perfectly reliable model as 67% conformant.
func TestRunTrialsExcludesNoVerdictFromDenominator(t *testing.T) {
	a := &sequenceAdapter{trials: []trial{called(1), truncated(), called(1)}}
	c, err := RunTrials(context.Background(), a, "m", 3)
	if err != nil {
		t.Fatalf("RunTrials: %v", err)
	}
	if c.Trials != 3 || c.NoVerdict != 1 || c.ToolCallTrials != 2 {
		t.Fatalf("got %+v, want Trials=3 NoVerdict=1 ToolCallTrials=2", c)
	}
	if d := c.Denominator(); d != 2 {
		t.Errorf("Denominator = %d, want 2 (the truncated trial is excluded)", d)
	}
	rate, ok := c.Rate()
	if !ok || rate != 1 {
		t.Errorf("Rate = (%v, %v), want (1, true) — 2 of 2 verdict-reaching trials called the tool", rate, ok)
	}
	if c.Verdict() != OK {
		t.Errorf("Verdict = %v, want OK", c.Verdict())
	}
	// A trial that made its call and then hit the cap is a call, not a
	// no-verdict: truncation only clouds a trial that produced nothing.
	var late Conformance
	late.Add(Result{ToolCalls: 1, Truncated: true})
	if late.NoVerdict != 0 || late.ToolCallTrials != 1 {
		t.Errorf("truncation after a tool call = %+v, want it counted as a call", late)
	}
}

// TestRunTrialsAllNoVerdictHasNoRate keeps the aggregate from inventing the
// one number the single-trial probe is forbidden to report: a model that was
// cut off every time never said anything, and 0% would be an accusation.
func TestRunTrialsAllNoVerdictHasNoRate(t *testing.T) {
	a := &sequenceAdapter{trials: []trial{truncated(), truncated(), truncated()}}
	c, err := RunTrials(context.Background(), a, "m", 3)
	if err != nil {
		t.Fatalf("RunTrials: %v", err)
	}
	if rate, ok := c.Rate(); ok {
		t.Errorf("Rate = (%v, true), want no rate at all — every trial was truncated", rate)
	}
	if c.Verdict() != Unknown {
		t.Errorf("Verdict = %v, want Unknown", c.Verdict())
	}
	if c.NoVerdict != 3 || c.Denominator() != 0 {
		t.Errorf("got %+v, want NoVerdict=3 and an empty denominator", c)
	}
}

// TestRunTrialsSingleTrialMatchesRun pins the compatibility floor: trials == 1
// (and anything below it) must be exactly one Run, reporting exactly what Run
// reported.
func TestRunTrialsSingleTrialMatchesRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		tr   trial
	}{
		{"tool call", called(1)},
		{"clean refusal", refused()},
		{"truncated", truncated()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := Run(context.Background(), &sequenceAdapter{trials: []trial{tc.tr}}, "m")
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			for _, trials := range []int{1, 0, -3} {
				a := &sequenceAdapter{trials: []trial{tc.tr}}
				c, err := RunTrials(context.Background(), a, "m", trials)
				if err != nil {
					t.Fatalf("RunTrials(%d): %v", trials, err)
				}
				if c.Trials != 1 || len(c.Results) != 1 || c.Results[0] != want {
					t.Errorf("RunTrials(%d) = %+v, want one trial holding %+v", trials, c, want)
				}
				if got := int(a.calls.Load()); got != 1 {
					t.Errorf("RunTrials(%d) probed %d times, want 1", trials, got)
				}
			}
		})
	}
}

// TestRunTrialsTransportError documents the decided policy: a failure before
// any trial succeeded is an error (nothing was measured); a failure after one
// keeps the partial sample, because a rate over 3 of 5 trials beats nothing.
func TestRunTrialsTransportError(t *testing.T) {
	t.Run("first trial fails", func(t *testing.T) {
		c, err := RunTrials(context.Background(), &sequenceAdapter{trials: []trial{failed("connection refused")}}, "m", 5)
		if err == nil {
			t.Fatalf("want an error when no trial ran, got %+v", c)
		}
		if c.Trials != 0 {
			t.Errorf("Conformance = %+v, want the zero value", c)
		}
	})
	t.Run("fails midway", func(t *testing.T) {
		a := &sequenceAdapter{trials: []trial{called(1), called(1), failed("model server exploded"), called(1), called(1)}}
		c, err := RunTrials(context.Background(), a, "m", 5)
		if err != nil {
			t.Fatalf("a partial sample must not be reported as a failed probe: %v", err)
		}
		if c.Trials != 2 || c.ToolCallTrials != 2 {
			t.Errorf("got %+v, want the 2 trials that completed", c)
		}
		if c.Err == nil {
			t.Error("Conformance.Err is nil — the sample was cut short and callers can't tell")
		}
		if rate, ok := c.Rate(); !ok || rate != 1 {
			t.Errorf("Rate = (%v, %v), want the partial rate (1, true)", rate, ok)
		}
		if got := int(a.calls.Load()); got != 3 {
			t.Errorf("adapter probed %d times, want 3 (aggregation stops at the error)", got)
		}
	})
}

// TestGateRefinesInBackground is the daemon-path constraint: the caller waits
// for exactly one probe (the verdict it needs before the user's first reply)
// and the rest of the sample lands afterwards.
func TestGateRefinesInBackground(t *testing.T) {
	a := &sequenceAdapter{trials: []trial{called(1), refused(), called(1), refused(), called(1)}}
	g := NewGate(WithTrials(5))
	defer g.Close()

	if v := g.Verdict(context.Background(), a, "m"); v != OK {
		t.Fatalf("verdict = %v, want OK from the first trial alone", v)
	}
	// Whatever the background has done so far, the blocking call cost one probe
	// and produced a verdict; the sample completes behind it.
	g.wait()

	c, ok := g.Conformance("m")
	if !ok {
		t.Fatal("no conformance recorded")
	}
	if c.Trials != 5 {
		t.Fatalf("Trials = %d, want 5 (1 blocking + 4 refined in the background)", c.Trials)
	}
	if rate, ok := c.Rate(); !ok || rate != 0.6 {
		t.Errorf("Rate = (%v, %v), want (0.6, true)", rate, ok)
	}
	if got := int(a.calls.Load()); got != 5 {
		t.Errorf("adapter probed %d times, want 5", got)
	}
	// The cached verdict is still the probe's own contract, untouched by the
	// rate: this model does call tools, it just doesn't always.
	if v, cached := g.Cached("m"); !cached || v != OK {
		t.Errorf("Cached = (%v, %v), want (OK, true)", v, cached)
	}
}

// TestGateSingleTrialStartsNoBackgroundWork pins the "1 reproduces today"
// requirement at the gate: no goroutine, no extra probe.
func TestGateSingleTrialStartsNoBackgroundWork(t *testing.T) {
	for _, trials := range []int{1, 0} {
		a := &sequenceAdapter{trials: []trial{called(1)}}
		g := NewGate(WithTrials(trials))
		if v := g.Verdict(context.Background(), a, "m"); v != OK {
			t.Fatalf("trials=%d: verdict = %v, want OK", trials, v)
		}
		g.wait()
		if got := int(a.calls.Load()); got != 1 {
			t.Errorf("trials=%d: adapter probed %d times, want 1", trials, got)
		}
		if c, _ := g.Conformance("m"); c.Trials != 1 {
			t.Errorf("trials=%d: Trials = %d, want 1", trials, c.Trials)
		}
		g.Close()
	}
}

// TestGateWarningCarriesTheRate: the first notice on a fresh model has only
// the blocking trial to report, but once the sample lands the notice says how
// often the model actually complied.
func TestGateWarningCarriesTheRate(t *testing.T) {
	a := &sequenceAdapter{trials: []trial{refused(), refused(), refused()}}
	g := NewGate(WithTrials(3))
	defer g.Close()

	w := g.Warning(context.Background(), a, "m")
	if w == "" {
		t.Fatal("no warning for a model that finished its turn without calling the tool")
	}
	g.wait()
	w2 := g.Warning(context.Background(), a, "m")
	if !strings.Contains(w2, "0/3") {
		t.Errorf("refined warning = %q, want it to carry the 0-of-3 conformance sample", w2)
	}

	// This deliberately does NOT assert w2 != w. Whether the *first* warning
	// already carries the sample is a race, not a contract: Warning renders the
	// rate whenever Trials > 1, and the background refinement this same call
	// kicks off can finish between its own Verdict call and the Conformance read
	// a few lines later. Under a loaded `go test ./...` that happens, and the
	// inequality failed roughly one run in ten while the product was behaving
	// correctly either way — an earlier-than-usual sample is a better warning,
	// not a worse one. The substring above is what the test was actually for; the
	// inequality was a proxy for it that could disagree with it.
}

// blockingAdapter answers the first probe immediately and holds every later
// one until either the test releases it or the probe's context dies — the
// deterministic stand-in for a slow local model still generating in the
// background after the request that triggered it has finished.
type blockingAdapter struct {
	calls   atomic.Int32
	release chan struct{}
	ctxErrs atomic.Int32
}

func (a *blockingAdapter) Name() string { return "blocking" }

func (a *blockingAdapter) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Event, error) {
	if a.calls.Add(1) > 1 {
		select {
		case <-a.release:
			if err := ctx.Err(); err != nil {
				a.ctxErrs.Add(1)
				return nil, err
			}
		case <-ctx.Done():
			a.ctxErrs.Add(1)
			return nil, ctx.Err()
		}
	}
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu", Name: "list_files"}}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse}
	close(ch)
	return ch, nil
}

// TestGateRefinementSurvivesTheRequestContext is the bug this shape exists to
// avoid: the gate is first consulted from a per-request context that is
// cancelled the moment the message completes — roughly when the background
// sample would be getting started. Refinement must run against the gate's own
// lifetime context instead.
func TestGateRefinementSurvivesTheRequestContext(t *testing.T) {
	a := &blockingAdapter{release: make(chan struct{})}
	g := NewGate(WithTrials(3))
	defer g.Close()

	reqCtx, cancelReq := context.WithCancel(context.Background())
	if v := g.Verdict(reqCtx, a, "m"); v != OK {
		t.Fatalf("verdict = %v, want OK", v)
	}
	// The request ends; its context dies. The refinement is parked in the
	// adapter and has not yet run a trial.
	cancelReq()
	close(a.release)
	g.wait()

	if n := a.ctxErrs.Load(); n != 0 {
		t.Errorf("%d background trial(s) died with the request context — refinement must use the gate's lifetime context", n)
	}
	if c, _ := g.Conformance("m"); c.Trials != 3 {
		t.Errorf("Trials = %d, want 3 (the sample completed after the request ended)", c.Trials)
	}
}

// TestGateCloseCancelsRefinement: shutdown must stop the background sample
// rather than leave probes generating against a model server nobody is
// listening to. The partial sample it did measure is kept.
func TestGateCloseCancelsRefinement(t *testing.T) {
	a := &blockingAdapter{release: make(chan struct{})}
	refined := make(chan Conformance, 1)
	g := NewGate(WithTrials(4), withRefinedHook(func(_ string, c Conformance) { refined <- c }))

	if v := g.Verdict(context.Background(), a, "m"); v != OK {
		t.Fatalf("verdict = %v, want OK", v)
	}
	g.Close() // cancels the gate context the parked trial is waiting on

	// Close waits for the refinement goroutine, which fires the hook on its way
	// out, so this receive is already satisfied — no sleep, no timing race.
	<-refined
	c, _ := g.Conformance("m")
	if c.Trials != 1 {
		t.Errorf("Trials = %d, want 1 — the background trials were cancelled, not run", c.Trials)
	}
	if c.Err == nil {
		t.Error("a cancelled refinement left no trace on the sample")
	}
	if rate, ok := c.Rate(); !ok || rate != 1 {
		t.Errorf("Rate = (%v, %v), want the one measured trial's (1, true)", rate, ok)
	}
}
