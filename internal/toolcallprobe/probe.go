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
type Gate struct {
	sf singleflight.Group

	mu       sync.RWMutex
	verdicts map[string]Verdict
}

func NewGate() *Gate { return &Gate{verdicts: make(map[string]Verdict)} }

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
	return fmt.Sprintf("model %q made no tool call on a trivial tool-calling probe — it likely can't use tools,"+
		" so it may answer from guesswork instead of reading your files; see docs/providers.md", model)
}
