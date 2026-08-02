// Package toolcallprobe holds the single definition of Aegis's tool-calling
// smoke test: send one obviously-actionable prompt with one trivial tool
// schema and see whether the model answers with a structured tool call or
// just talks about it.
//
// It exists because two callers need the same verdict and must not drift
// apart: `aegis doctor`'s tool-calling check (P28.2), which reports it as a
// diagnostic row, and the daemon's per-model gate (P34.2 lever 1), which warns
// at run start. A model is only ever "unsupported" here in the sense that
// *this* probe couldn't get a tool call out of it — see Run's contract for why
// that distinction matters to callers.
package toolcallprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"golang.org/x/sync/singleflight"
)

// SmokePrompt names a concrete tool and leaves no ambiguity that calling it,
// rather than describing it, is the expected response. A model that answers
// this in prose is not exercising judgment; it is failing to speak the
// protocol.
const SmokePrompt = "Call the `list_files` tool now to list the files in the current directory. Do not describe what you would do — call the tool."

// SmokeSystem biases the model toward acting rather than explaining, so a
// prose answer isn't just an artifact of a missing system prompt.
const SmokeSystem = "You are a coding agent. When a task requires a tool, call it immediately instead of describing it in prose."

// SmokeTool is the one trivial tool offered to the model during the probe.
var SmokeTool = provider.ToolSchema{
	Name:        "list_files",
	Description: "List the files in a directory.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"directory to list"}},"required":["path"]}`),
}

// SmokeMaxTokens caps the probe generously, for the same reason ProbeTimeout
// does: the cap is a bound, not a target — the stream ends the moment the tool
// call lands — so a terse model pays nothing for headroom, while a tight cap
// truncates a verbose one mid-reasoning and buys a false accusation.
//
// It was measured, not guessed. At the original 256 the probe was a coin flip
// on a reasoning model: `qwen3:14b` needed 124-825 completion tokens across
// five runs of this exact prompt (most of it thinking preamble before the
// call), so 256 truncated the majority of them and reported a model that calls
// tools reliably as one that cannot. Truncation past even this cap is still
// handled — see Result.Truncated — but it should be rare enough not to matter.
const SmokeMaxTokens = 2048

// Result is what one probe run observed.
type Result struct {
	// ToolCalls is how many structured tool calls the model emitted.
	ToolCalls int
	// Truncated reports that the model hit the token cap before it finished.
	// A truncated run that made no tool call is not evidence of anything: the
	// call may simply have been the next token the model was going to emit.
	// Callers must treat ToolCalls == 0 && Truncated as "no verdict", never as
	// failure.
	Truncated bool
}

// Run sends the smoke test to model via adapter and reports what came back.
//
// A non-nil error means the probe could not reach a verdict (transport
// failure, mid-stream provider error, cancelled context) — it never means the
// model failed. Callers must treat that case as "unknown" and stay silent
// rather than accusing a model of not supporting tool calls when the truth is
// the server was unreachable. A zero ToolCalls with a nil error is the only
// negative verdict this probe can justify, and only when !Truncated.
func Run(ctx context.Context, adapter provider.Adapter, model string) (Result, error) {
	events, err := adapter.Stream(ctx, provider.Request{
		Model:  model,
		System: SmokeSystem,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: []provider.Block{provider.TextBlock{Text: SmokePrompt}},
		}},
		Tools:     []provider.ToolSchema{SmokeTool},
		MaxTokens: SmokeMaxTokens,
	})
	if err != nil {
		return Result{}, err
	}
	// The stream must be drained even after an error event so the adapter's
	// goroutine can finish rather than block on an unread channel.
	var res Result
	var stop provider.StopReason
	var streamErr error
	for ev := range events {
		switch ev.Type {
		case provider.EventToolUse:
			res.ToolCalls++
		case provider.EventDone:
			stop = ev.Stop
		case provider.EventError:
			streamErr = ev.Err
		}
	}
	if streamErr != nil {
		return Result{}, streamErr
	}
	res.Truncated = stop == provider.StopMaxTokens
	return res, nil
}

// Verdict is what the probe learned about one model.
type Verdict int

const (
	// Unknown means the probe hasn't run or couldn't reach a verdict. It is
	// never cached and never warned about: an unreachable model server must not
	// be reported to the user as a model that can't call tools.
	Unknown Verdict = iota
	OK
	Unsupported
)

// ProbeTimeout bounds the probe generously rather than tightly: on local
// hardware a cold model load alone was measured at ~28s for a 24b model, and
// timing out mid-load would report Unknown for exactly the slow-to-load models
// most worth checking. Exceeding it costs nothing but the warning — callers
// proceed either way.
const ProbeTimeout = 90 * time.Second

// Gate caches one verdict per model name and collapses concurrent probes of the
// same model into a single call.
//
// The cache is deliberately in-memory and never persisted: an Ollama tag is
// mutable (`ollama pull` can replace what "qwen3:14b" means without the name
// changing), so a verdict written to disk could outlive the model it describes
// and warn about a model since replaced by a capable one. A process-lifetime
// cache is short enough for that to stay unlikely, and the probe is cheap
// enough to repay on restart.
// The conformance rate (P53.4) is layered on top of that cache rather than
// beside it: the *first* trial is what the blocking caller waits for and is
// what produces the verdict, exactly as before, and the remaining trials run
// in the background against the gate's own lifetime context, refining the
// stored Conformance so later notices can carry a rate. That shape is forced
// by where Gate is consulted from — the daemon's message path, before the
// user's first reply — where running five sequential generations inline would
// be minutes of dead air on a model that generates at single-digit tokens/sec.
type Gate struct {
	sf singleflight.Group

	// trials is the full sample size; 1 disables background refinement
	// entirely and reduces the gate to its pre-P53.4 behavior.
	trials int

	// bgCtx is the gate's own lifetime context, the parent of every background
	// refinement. Deliberately NOT the per-request context a probe is first
	// triggered from: that one is cancelled the moment the message completes,
	// which is roughly when the refinement would be getting started. Close
	// cancels it, so refinements die with the server rather than outliving it.
	bgCtx    context.Context
	bgCancel context.CancelFunc
	wg       sync.WaitGroup

	mu       sync.RWMutex
	verdicts map[string]Verdict
	conf     map[string]Conformance
	refining map[string]bool
	// onRefined, when set, fires after a background refinement finishes. Test
	// seam only: it makes the background path assertable without sleeps.
	onRefined func(model string, c Conformance)
}

// GateOption configures a Gate at construction.
type GateOption func(*Gate)

// WithTrials sets how many probe trials make up a model's conformance sample.
// n < 1 is clamped to 1, which reproduces the single-trial behavior exactly:
// no background work is scheduled at all.
func WithTrials(n int) GateOption {
	return func(g *Gate) {
		if n < 1 {
			n = 1
		}
		g.trials = n
	}
}

// withRefinedHook installs the test seam described on Gate.onRefined.
func withRefinedHook(fn func(model string, c Conformance)) GateOption {
	return func(g *Gate) { g.onRefined = fn }
}

func NewGate(opts ...GateOption) *Gate {
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gate{
		trials:   1,
		bgCtx:    ctx,
		bgCancel: cancel,
		verdicts: make(map[string]Verdict),
		conf:     make(map[string]Conformance),
		refining: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Close cancels any in-flight background refinement and waits briefly for the
// goroutines to unwind, so a daemon shutdown doesn't leave probes generating
// against a model server nobody is listening to. Safe to call more than once
// and safe never to call — a Gate with trials == 1 never starts a goroutine.
func (g *Gate) Close() {
	g.bgCancel()
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Cancellation has already been delivered; the adapter unwinds on its
		// own. Blocking shutdown on a slow model server would be the worse
		// trade.
	}
}

// wait blocks until every background refinement has finished. Test helper —
// production code uses Close.
func (g *Gate) wait() { g.wg.Wait() }

// Conformance returns what the trials so far observed for model, if any have.
// Refinement runs in the background, so this may report a partial sample (one
// trial right after the blocking probe, the full count once refinement lands).
func (g *Gate) Conformance(model string) (Conformance, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.conf[model]
	return c, ok
}

// addResult folds one trial into model's stored conformance.
func (g *Gate) addResult(model string, res Result) {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.conf[model]
	c.Add(res)
	g.conf[model] = c
}

func (g *Gate) setConformanceErr(model string, err error) {
	if err == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.conf[model]
	c.Err = err
	g.conf[model] = c
}

// refine runs the sample's remaining trials in the background, publishing each
// one into the stored conformance as it lands so a cancelled refinement still
// contributes what it measured.
//
// Called from inside the singleflight flight that just ran trial 1, so at most
// one refinement per model is ever in flight; the refining map keeps a later
// re-probe (a model whose verdict wasn't cacheable) from starting a second.
func (g *Gate) refine(adapter provider.Adapter, model string) {
	if g.trials <= 1 {
		return
	}
	g.mu.Lock()
	if g.refining[model] || g.conf[model].Trials >= g.trials {
		g.mu.Unlock()
		return
	}
	remaining := g.trials - g.conf[model].Trials
	g.refining[model] = true
	g.mu.Unlock()

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		defer func() {
			g.mu.Lock()
			delete(g.refining, model)
			c := g.conf[model]
			g.mu.Unlock()
			if g.onRefined != nil {
				g.onRefined(model, c)
			}
		}()
		var scratch Conformance
		// Each trial is bounded by the same ProbeTimeout the blocking probe
		// uses, and the whole thing dies with g.bgCtx.
		err := runTrialsInto(g.bgCtx, adapter, model, remaining, ProbeTimeout, &scratch, func(res Result) {
			g.addResult(model, res)
		})
		if err != nil {
			g.setConformanceErr(model, err)
			return
		}
		g.setConformanceErr(model, scratch.Err)
	}()
}

// Cached reports the stored verdict for model, if the probe has reached one.
func (g *Gate) Cached(model string) (Verdict, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	v, ok := g.verdicts[model]
	return v, ok
}

func (g *Gate) store(model string, v Verdict) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.verdicts[model] = v
}

// Verdict returns model's tool-calling verdict, probing once and caching the
// result. Probe failures are reported as Unknown and are not cached, so a
// transient outage doesn't poison a model's verdict for the process's life.
func (g *Gate) Verdict(ctx context.Context, adapter provider.Adapter, model string) Verdict {
	if v, ok := g.Cached(model); ok {
		return v
	}
	res, err, _ := g.sf.Do(model, func() (any, error) {
		// Re-check under the flight: the fast path above can miss, then lose
		// the race to a probe that finishes and caches before this one gets the
		// slot. Without this, that caller probes a second time — a wasted model
		// load, which is the whole cost this cache exists to avoid.
		if v, ok := g.Cached(model); ok {
			return v, nil
		}
		pctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
		defer cancel()
		res, err := Run(pctx, adapter, model)
		if err != nil {
			return Unknown, err
		}
		// Trial 1 of the conformance sample: recorded, then the rest handed to
		// the background so this caller's latency is one probe, as it has
		// always been.
		g.addResult(model, res)
		g.refine(adapter, model)
		if res.ToolCalls == 0 && res.Truncated {
			// The model ran out of tokens before it finished, so silence here
			// is the cap's doing, not the model's. Unknown, and deliberately
			// not cached: a verdict this run couldn't justify must not be the
			// one every later session in this process inherits.
			return Unknown, nil
		}
		v := OK
		if res.ToolCalls == 0 {
			v = Unsupported
		}
		g.store(model, v)
		return v, nil
	})
	if err != nil {
		return Unknown
	}
	return res.(Verdict)
}

// Warning returns the user-facing notice text for a model that can't drive the
// tool-calling loop, or "" when the model is fine, the verdict is unknown, or
// the model name isn't resolved yet.
//
// Callers are responsible for the provider gate: this must only be asked about
// local Ollama-style providers, which is where the observed variance lives (an
// Ollama manifest can claim tool support the weights don't deliver) and which
// keeps the probe off the paid-API path entirely.
func (g *Gate) Warning(ctx context.Context, adapter provider.Adapter, model string) string {
	if adapter == nil {
		return ""
	}
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		return ""
	}
	if g.Verdict(ctx, adapter, model) != Unsupported {
		return ""
	}
	warn := fmt.Sprintf("model %q made no tool call on a trivial tool-calling probe — it likely can't use tools,"+
		" so it may answer from guesswork instead of reading your files; see docs/providers.md", model)
	// The rate, when a background refinement has produced one worth naming.
	// The first notice on a fresh model necessarily carries only the blocking
	// trial (Trials == 1, nothing to add); a later session on the same model
	// gets the sample. Deliberately additive: the verdict itself still comes
	// from the probe's own contract, not from a threshold on the rate.
	if c, ok := g.Conformance(model); ok && c.Trials > 1 {
		warn += fmt.Sprintf(" (conformance over %d trials: %s)", c.Trials, c.Summary())
	}
	return warn
}
