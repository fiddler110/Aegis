// Package engine implements the core agent loop: call the model, dispatch any
// tool calls it requests, append the results, and repeat until the model
// produces a final answer or the run is interrupted.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/toolshim"
	"github.com/fiddler110/aegis/internal/trace"
)

// redactSecretsFn is a seam over security.RedactText so tests can stub in
// findings without needing the real gitleaks binary on PATH — mirrors
// gitpr.go's scanPRTextForSecrets seam over security.ScanText (P24.6 /
// FIND-13).
var redactSecretsFn = security.RedactText

// Conversation is the mutable transcript the engine drives.
type Conversation struct {
	System   string
	Messages []provider.Message

	// Persisted is the number of leading Messages already durably saved by
	// the caller (e.g. the session store); callers that persist
	// incrementally (P8.1) save only Messages[Persisted:] each turn. -1
	// means the leading messages were rewritten in place (repair,
	// compaction) rather than just appended to, so the caller must fall
	// back to a full re-save instead of appending.
	Persisted int

	tokenEstimate      int // cached estimatedTokens() total (P8.4)
	tokenEstimateValid bool
}

// Append adds a message to the conversation.
func (c *Conversation) Append(m provider.Message) {
	c.Messages = append(c.Messages, m)
	if c.tokenEstimateValid {
		c.tokenEstimate += tokenest.Message(m)
	}
}

// invalidate marks the conversation as rewritten in place: cached token
// totals are stale and Persisted no longer reflects a safe append point.
func (c *Conversation) invalidate() {
	c.tokenEstimateValid = false
	c.Persisted = -1
}

// Invalidate is the exported form of invalidate, for callers that rewrite a
// message's content in place outside the engine (P39.5: the `aegis chat
// --skill` drive loop compacts the one-time SKILL.md preamble out of the first
// user message after the opening turn so it stops riding every request). After
// mutating Messages, call this so the cached token estimate is recomputed and
// the caller re-persists rather than blindly appends.
func (c *Conversation) Invalidate() { c.invalidate() }

// estimatedTokens returns the total estimated token count across system +
// messages, recomputing only when the conversation has been rewritten since
// the last call (P8.4 — avoids double-scanning the full conversation every
// turn).
func (c *Conversation) estimatedTokens() int {
	if !c.tokenEstimateValid {
		c.tokenEstimate = tokenest.Estimate(c.System)
		for _, m := range c.Messages {
			c.tokenEstimate += tokenest.Message(m)
		}
		c.tokenEstimateValid = true
	}
	return c.tokenEstimate
}

// EventKind classifies engine events delivered to consumers (TUI, CLI, logs).
type EventKind string

const (
	KindText     EventKind = "text"     // incremental assistant text
	KindThinking EventKind = "thinking" // incremental extended-thinking text
	// KindToolCallStart announces a tool call the model has begun streaming,
	// while its arguments are still being generated (P33.3). ToolName is set;
	// ToolInput is not, and ToolID only when the provider named the call and
	// assigned its ID together. The KindToolCall for the same call still
	// follows unchanged, so a consumer that ignores this kind is unaffected —
	// as is a provider adapter that never emits provider.EventToolUseStart.
	KindToolCallStart EventKind = "tool_call_start"
	KindToolCall      EventKind = "tool_call"   // a tool is about to run
	KindToolResult    EventKind = "tool_result" // a tool finished
	KindTurnDone      EventKind = "turn_done"   // one model turn completed
	KindTrace         EventKind = "trace"       // per-turn structured trace (server-internal)
	KindDone          EventKind = "done"        // the run finished (final answer)
	KindError         EventKind = "error"       // the run failed
	KindSteer         EventKind = "steer"       // mid-run steering instruction injected
	KindGuard         EventKind = "guard"       // output validation result (warning)
	KindNotice        EventKind = "notice"      // advisory for the user (context fill, compaction)
)

// Event is emitted to the consumer-provided sink as the run progresses.
type Event struct {
	Kind      EventKind
	Text      string          // KindText
	ToolName  string          // KindToolCallStart / KindToolCall / KindToolResult
	ToolInput json.RawMessage // KindToolCall
	// ToolID is the provider-assigned tool_use ID (provider.ToolUseBlock.ID),
	// carried on both the KindToolCall and its matching KindToolResult so a
	// consumer can correlate the two exactly instead of guessing from
	// same-name ordering — required once tools can run concurrently
	// (runTools below), where results don't necessarily arrive in call order
	// (P21.2).
	ToolID      string
	ToolResult  string // KindToolResult
	ToolIsError bool   // KindToolResult
	// Usage carries per-turn counts on KindTurnDone and the run-cumulative
	// total (IsEstimated set if any contributing turn lacked real
	// provider-reported usage) on the terminal KindDone (P25.5).
	Usage       *provider.Usage
	CostUSD     float64          // KindTurnDone: cumulative run cost (0 if untracked)
	Trace       *trace.TurnTrace // KindTrace: per-turn observability record
	Err         error            // KindError
	GuardReason string           // KindGuard: why validation failed
	GuardPassed bool             // KindGuard: whether the guard ultimately passed
	GuardStatus string           // KindGuard: guard.Status value — "passed" | "failed" | "skipped_transport_error" — distinguishes a genuine pass/fail verdict from a fail-open skip that GuardPassed alone can't
	// GuardRetrying marks a failed-guard event whose answer is about to be
	// replaced by a corrective retry (P25.3). Consumers should withdraw the
	// answer they just rendered rather than leaving it on screen — the retry's
	// text replaces it, it doesn't follow it.
	GuardRetrying bool
	// GuardFilesRestored is set on the terminal (retries-exhausted) KindGuard
	// failure event to the number of files rolled back to their pre-turn
	// state via the run's checkpoint Snapshotter (P27.16 quarantine-on-FAIL).
	// Zero means either nothing was written this turn or no checkpoint store
	// was wired in, in which case the bad write is left on disk as before.
	GuardFilesRestored int
}

// EmitFunc receives engine events. It must not block for long.
type EmitFunc func(Event)

// Gate decides whether a tool call may proceed. A denied call is reported to
// the model as an error result rather than aborting the run.
type Gate interface {
	Check(ctx context.Context, t tool.Tool, input json.RawMessage) (allowed bool, reason string)
}

// Compactor optionally shortens a conversation (e.g. by summarizing old turns)
// when it grows too large. It returns the possibly-rewritten message list and
// whether a change was made.
type Compactor interface {
	Compact(ctx context.Context, system string, msgs []provider.Message) (out []provider.Message, changed bool, err error)
}

// FallbackCompactor is an optional capability of a Compactor: a
// deterministic, non-LLM shortening pass used when Compact has failed twice
// in a row for the same run (P28.4) — e.g. a local-model summarizer that
// intermittently returns empty output. The engine type-asserts for this on
// e.compactor, so a Compactor that only implements Compact keeps today's
// warn-and-skip behavior on repeated failure.
type FallbackCompactor interface {
	FallbackCompact(msgs []provider.Message) (out []provider.Message, changed bool)
}

// CalibratedCompactor is an optional capability of a Compactor: it accepts the
// correction the engine has learned for the shared token estimate (P62.4), as
// an additive overhead plus a multiplicative scale.
//
// It exists because the engine and the Compactor run *two* gates against the
// same conversation, and a correction applied to only one of them re-creates
// P41.1 in a new form. Before that fix the engine estimated script-aware while
// compaction estimated chars/4, so a conversation the engine had decided to
// compact could be silently no-op'd by the cruder gate; they were unified on
// tokenest to close it. A calibration the engine applied alone would split them
// again — the engine would call Compact() believing the conversation is over
// budget, the summarizer would price the same messages uncorrected, decide it
// is not, and return changed=false. The engine would read that as "nothing left
// to compact" and emit the context-full notice, which is precisely the symptom
// P62.4 was filed about.
//
// The engine type-asserts for this, so a Compactor that only implements Compact
// keeps its uncorrected estimate and today's behavior.
type CalibratedCompactor interface {
	SetEstimateCorrection(overhead int, scale float64)
}

// Hooks observe and can veto tool calls. PreToolUse runs after the permission
// gate but before execution; returning an error blocks the call (the error is
// reported to the model). PostToolUse runs after execution. This is the
// in-process hook surface (cf. Hermes/opencode plugin lifecycle hooks).
type Hooks interface {
	PreToolUse(ctx context.Context, toolName string, input json.RawMessage) error
	PostToolUse(ctx context.Context, toolName string, input json.RawMessage, result string, isError bool)
}

// PrepareStepFunc is called before each model turn. It receives the current
// message list and may return a modified copy (e.g. to inject dynamic context
// or refresh ephemeral tool metadata). Returning nil leaves messages unchanged.
type PrepareStepFunc func(ctx context.Context, msgs []provider.Message) []provider.Message

// Options configures an Engine.
type Options struct {
	Adapter               provider.Adapter
	Tools                 *tool.Registry
	Gate                  Gate            // optional; nil means all tool calls are allowed
	Compactor             Compactor       // optional; nil disables context compaction
	Hooks                 Hooks           // optional; nil disables hooks
	Cost                  *cost.Tracker   // optional; nil disables cost tracking
	PrepareStep           PrepareStepFunc // optional; called before every model turn
	OutputGuard           guard.Func      // optional; validates the final answer (and any files written this turn)
	OutputGuardMaxRetries int             // corrective retries on guard failure; 0 -> 1 when a guard is set
	// OutputGuardFormat is a JSON Schema describing the shape OutputGuard
	// requires of the final answer (guard.SchemaFormat, P59.8). When set, the
	// corrective retry after a guard failure is sent with decoding constrained
	// to it on backends that can (Ollama), rather than being asked again in
	// prose and checked again afterwards.
	//
	// Only the retry is constrained, never the ordinary turns. A first turn is
	// where the model does the actual work — reading files, calling tools — and
	// a grammar that forces a JSON object out of it would forbid exactly that;
	// by the time a schema guard has rejected an answer, the remaining task
	// genuinely is "emit this object", which is the one moment the constraint
	// describes the intent instead of fighting it. Tools are suppressed on that
	// turn for the same reason. Empty (the default, and every non-schema guard)
	// leaves the retry as free generation.
	OutputGuardFormat json.RawMessage
	// ZeroToolNudgeMaxRetries (P28.3) bounds the corrective-nudge retries fired
	// when the model's first response to a task produces zero tool calls even
	// though the request plainly reads as actionable (looksActionable) and
	// tools are available — the deepseek-r1:8b live-eval failure mode where a
	// model's reasoning gets dumped as the final answer instead of being
	// followed by a real tool call. 0 -> default of 1; negative disables the
	// nudge entirely.
	ZeroToolNudgeMaxRetries int
	BudgetUSD               float64 // optional; >0 aborts the run past this cost
	MaxTokensPerRun         int     // optional; >0 aborts the run past this cumulative token count (P10.5) — always enforceable, unlike BudgetUSD which is a no-op for unpriced/estimated usage
	// MaxGeneratedTokensPerRun (P59.4) aborts a run past this cumulative
	// *output* token count, checked at the same two gates as the other three
	// budgets. 0 (the default) disables it.
	//
	// MaxTokensPerRun above is a context budget: it sums the whole prompt every
	// turn, which is exactly the billed quantity on a priced provider and a
	// ~O(N²)-in-conversation-length number on a local one, where the prompt is
	// re-counted in full each turn. This is the work budget — what the model
	// actually produced — so "stop after roughly this much output" is
	// expressible without either key changing meaning by provider.
	MaxGeneratedTokensPerRun int
	// MaxWallClockPerRun (P52.15) aborts a run that has been going longer than
	// this, checked at the same two gates as the cost/token budgets. 0 (the
	// default) disables it.
	//
	// It exists because none of the other three budgets bound *time*, which is
	// the dimension that actually hurts on local hardware: BudgetUSD is a no-op
	// for unpriced local usage, MaxTokensPerRun defaults to 0, and MaxIterations
	// defaults to 40 — which on a model measured at ~7 tok/s is potentially
	// hours before any safety valve trips. "Don't spend more than N minutes on
	// this" is the constraint users actually have, and nothing else expresses it.
	//
	// Deliberately off by default. A wall-clock cap cannot tell a stalled run
	// from a slow one that is making real progress, so a non-zero default would
	// guillotine legitimate long work — the same regression shape the P52.3
	// reconcile caught when the tool-failure breaker met the phased drive.
	// Opt-in only, via cost.max_wall_clock_per_run.
	MaxWallClockPerRun  time.Duration
	Model               string
	MaxTokens           int
	Temperature         *float64
	MaxIterations       int           // safety cap on tool-call rounds; 0 -> default
	LoopThreshold       int           // identical tool-call turns before aborting; 0 -> default, <0 disables
	ContextWindowTokens int           // model context window size; >0 enables proactive per-turn compaction at 85% fill
	SteerChan           <-chan string // optional; steering messages injected between tool rounds
	// ContextWindowFloor, when set, is consulted before every turn for a
	// serving window the adapter has been escalated to at runtime (P47.5b), and
	// the larger of it and ContextWindowTokens is what the proactive-compaction
	// trigger measures against (P59.7).
	//
	// It is a func rather than a value because ContextWindowTokens is captured
	// at New and the escalation happens mid-run: an engine holding the
	// pre-escalation number keeps compacting a conversation that now has room,
	// which on a local model costs minutes of summarizer calls during the very
	// overflow recovery that raised the window. Nil (every non-escalating
	// backend) leaves ContextWindowTokens as the only source, unchanged.
	ContextWindowFloor func() int
	// RedactSecrets opts in to running a read-capability tool's output through
	// gitleaks-backed secret detection (security.RedactText) before it's
	// appended to the conversation sent to the model provider (P24.12 /
	// FIND-09) — mitigates a cloud provider seeing a secret that a file read
	// happened to pick up. Off by default; never blocks the tool call, only
	// scrubs detected secret patterns in place.
	RedactSecrets bool
	Logger        *slog.Logger
	// Workdir, when set, overrides the working directory workspace-confined
	// tools (file ops, shell, git, ...) operate against for this run, via
	// tool.WithWorkdir — without it they fall back to their own
	// construction-time root (P25.1: per-session workdir).
	Workdir string
	// ExtraRoots names directories outside Workdir that workspace-confined
	// tools may additionally resolve paths into (P52.13,
	// workspace.additional_roots), carried to tools via tool.WithExtraRoots.
	// Empty — the usual case — leaves confinement exactly as it was: the
	// single Workdir root.
	//
	// The engine only ferries these to the tool call's context; the decisions
	// about which roots exist, whether each is writable, and whether it has
	// been trusted are all made before Options is built (see
	// config.ResolveAdditionalRoots).
	ExtraRoots []sandbox.Root
	// ToolCallShim (P53.6, provider.tool_call_shim) switches this run to the
	// non-native tool-calling fallback: tool schemas go into the system prompt
	// instead of the request's tools field, and the model's tagged JSON is
	// parsed back into tool calls (internal/toolshim). Off by default and
	// never engaged automatically — it is reached only by explicit config,
	// because a shim that quietly starts executing parsed prose is a safety
	// surface, not a convenience.
	//
	// It changes only how calls *arrive*. Parsed calls are ordinary
	// ToolUseBlocks by the time they reach runTools, so the gate, capability
	// check, hooks, and workspace confinement all apply unchanged — there is
	// deliberately no separate dispatch path for them.
	ToolCallShim bool
}

// Engine runs the agent loop.
type Engine struct {
	adapter          provider.Adapter
	tools            *tool.Registry
	gate             Gate
	compactor        Compactor
	hooks            Hooks
	cost             *cost.Tracker
	prepareStep      PrepareStepFunc
	outputGuard      guard.Func
	outputGuardMax   int
	outputGuardFmt   json.RawMessage
	zeroToolNudgeMax int
	budgetUSD        float64
	maxTokensPerRun  int
	maxGenTokens     int // P59.4: cumulative output-token cap; 0 = unbounded
	maxWallClock     time.Duration
	// now supplies the clock the wall-clock budget reads, defaulting to
	// time.Now. Unexported and injected only by same-package tests, because the
	// alternative is a test that depends on the host's monotonic-clock
	// resolution: on Windows two back-to-back time.Now calls return the *same*
	// instant (Go's nanotime there is ~0.5ms-granular), so a "budget already
	// spent" assertion written as a 1ns limit passes on macOS and silently never
	// fires on Windows. Real budgets are configured in whole seconds, so this
	// changes nothing in production — it just stops the tests from encoding a
	// POSIX-only assumption.
	now                 func() time.Time
	model               string
	maxTokens           int
	temperature         *float64
	maxIterations       int
	loopThreshold       int
	contextWindowTokens int
	contextWindowFloor  func() int
	steerChan           <-chan string
	redactSecrets       bool
	logger              *slog.Logger
	workdir             string
	extraRoots          []sandbox.Root
	toolShim            bool

	// writtenFiles tracks workspace-relative paths touched by a successful
	// write-capability tool call during the current Run, so the output guard
	// can validate the actual file content instead of only the assistant's
	// chat summary of it. Reset at the start of each Run; guarded by mu since
	// parallel tool rounds write to it from multiple goroutines.
	writtenFilesMu sync.Mutex
	writtenFiles   map[string]struct{}
}

// ErrInterrupted is returned when the run is cancelled via context.
var ErrInterrupted = errors.New("engine: interrupted")

// ErrWallClockLimit is wrapped by the P52.15 abort so a caller can distinguish
// "ran out of time" from "ran out of iterations" (which returns a plain error)
// or from a cost/token budget, rather than pattern-matching a message — the
// same reason ErrToolFailureLimit exists.
//
// Unlike ErrToolFailureLimit, the phased drive does *not* treat this as a
// resumable phase reset. A tool-failure stall is a model reasoning from a
// context full of its own failures, which a fresh context genuinely clears; a
// wall-clock limit says the operator asked for a hard time bound, and silently
// resetting and continuing past it would defeat the point of setting one.
var ErrWallClockLimit = errors.New("engine: wall-clock budget reached")

// ErrLoopDetected is wrapped by every loop-guard abort below so a caller driving
// the engine can classify the stop without matching on the message text — the
// same reason ErrToolFailureLimit exists.
//
// Like ErrToolFailureLimit and unlike ErrWallClockLimit, the phased drive treats
// this as a resumable reset rather than a terminal error (P57.1): the loop guard
// fires when a model keeps re-deriving one wrong theory of the data, and it is
// precisely the context holding that theory which makes the next turn repeat it.
// Dropping the context and re-reading from disk is the remedy — a second
// invocation with a fresh context is what resolved the 2026-08-03 stall by hand.
var ErrLoopDetected = errors.New("engine: aborting suspected loop")

// compactionTrigger returns the estimated prompt size at which proactive
// compaction fires (P59.1).
//
// It used to be a flat 85% of the window. That number reserves headroom for
// *prompt growth* and was never sized against generation — but on Ollama
// num_ctx covers prompt and completion out of one budget, so the completion has
// to fit in whatever the prompt leaves. At a 4096 window (Ollama's own server
// default, and a routinely detected one) a flat 85% leaves ~614 tokens for a
// max_tokens configured at 32768, and the run then hits the ceiling mid-answer,
// takes the "continue from where you left off" path, and grows the context
// again on every retry until it burns to maxIterations.
//
// So the trigger is sized against the generation the request may actually ask
// for: window - min(maxTokens, window/2) - a small margin, floored at half the
// window and capped at the old 85%. The min() is what keeps a large max_tokens
// from reserving the entire window and compacting on an empty conversation —
// past the halfway point, reserving more space for output than for the
// conversation is never the right trade. The 85% cap means a generous window
// with a modest max_tokens (a cloud model, say) behaves exactly as before.
func compactionTrigger(window, maxTokens int) int {
	if window <= 0 {
		return 0
	}
	trigger := window * 85 / 100
	if maxTokens > 0 {
		reserve := maxTokens
		if half := window / 2; reserve > half {
			reserve = half
		}
		// A margin over the reservation itself: the prompt figure this is
		// compared against is an estimate, not a token count.
		if sized := window - reserve - window/20; sized < trigger {
			trigger = sized
		}
	}
	if floor := window / 2; trigger < floor {
		trigger = floor
	}
	return trigger
}

// effectiveContextWindow is the window the proactive-compaction trigger
// measures against this turn: the larger of the window resolved at
// construction and any runtime escalation the adapter reports (P59.7).
//
// The larger wins because the escalation is a floor on the adapter's side
// (resolveNumCtx in the Ollama adapter serves it in preference to a
// per-request value), so treating it as anything else here would let the two
// disagree about the same request. A missing or zero floor leaves the
// constructed window untouched, which is every backend that cannot escalate.
func (e *Engine) effectiveContextWindow() int {
	win := e.contextWindowTokens
	if e.contextWindowFloor != nil {
		if floor := e.contextWindowFloor(); floor > win {
			return floor
		}
	}
	return win
}

// New constructs an Engine.
func New(opts Options) (*Engine, error) {
	if opts.Adapter == nil {
		return nil, errors.New("engine: nil adapter")
	}
	if opts.Model == "" {
		return nil, errors.New("engine: empty model")
	}
	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = 40
	}
	maxTok := opts.MaxTokens
	if maxTok <= 0 {
		maxTok = 32768
	}
	loopThreshold := opts.LoopThreshold
	if loopThreshold == 0 {
		loopThreshold = 5
	}
	zeroToolNudgeMax := opts.ZeroToolNudgeMaxRetries
	if zeroToolNudgeMax == 0 {
		zeroToolNudgeMax = 1
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		adapter:             opts.Adapter,
		tools:               opts.Tools,
		gate:                opts.Gate,
		compactor:           opts.Compactor,
		hooks:               opts.Hooks,
		cost:                opts.Cost,
		prepareStep:         opts.PrepareStep,
		outputGuard:         opts.OutputGuard,
		outputGuardMax:      opts.OutputGuardMaxRetries,
		outputGuardFmt:      opts.OutputGuardFormat,
		zeroToolNudgeMax:    zeroToolNudgeMax,
		budgetUSD:           opts.BudgetUSD,
		maxTokensPerRun:     opts.MaxTokensPerRun,
		maxGenTokens:        opts.MaxGeneratedTokensPerRun,
		maxWallClock:        opts.MaxWallClockPerRun,
		model:               opts.Model,
		maxTokens:           maxTok,
		temperature:         opts.Temperature,
		maxIterations:       maxIter,
		loopThreshold:       loopThreshold,
		contextWindowTokens: opts.ContextWindowTokens,
		contextWindowFloor:  opts.ContextWindowFloor,
		steerChan:           opts.SteerChan,
		redactSecrets:       opts.RedactSecrets,
		logger:              logger,
		workdir:             opts.Workdir,
		extraRoots:          opts.ExtraRoots,
		toolShim:            opts.ToolCallShim,
	}, nil
}

// Run drives the conversation to a final answer, executing tools as requested.
// It mutates conv in place (appending assistant and tool-result messages) and
// streams progress to emit. Cancelling ctx interrupts the run.
func (e *Engine) Run(ctx context.Context, conv *Conversation, emit EmitFunc) error {
	if emit == nil {
		emit = func(Event) {}
	}

	// P63.9: the four run budgets — cost, context tokens, generated tokens,
	// wall clock — and the run-start instant they share. See budget.go for why
	// this concern was the one lifted first.
	budget := e.newRunBudget()
	ctx, cancelBudget := budget.deadline(ctx)
	defer cancelBudget()

	// Repair any tool_use blocks left without a matching tool_result by a
	// previous interrupted run. Without this, most providers reject the
	// conversation with a validation error that permanently locks the session.
	if repaired := repairOrphanedToolUses(conv.Messages); len(repaired) != len(conv.Messages) {
		e.logger.Info("repaired orphaned tool calls", "added", len(repaired)-len(conv.Messages))
		conv.Messages = repaired
		conv.invalidate()
	}

	// compact is the P2.7/P28.4/P39.8 compaction concern (P63.9): the headroom
	// trigger, the summarizer's failure and latch bookkeeping, the shim's prompt
	// cost, and the one context-full notice a run may emit. Never nil — the
	// no-compactor case is still its business. See compact.go.
	compact := e.newCompactionGuard()
	compact.compactOnEntry(ctx, conv)

	// guardRetry is the output guard's verdict handling and its bounded
	// corrective retry (P63.9), including the P59.8 schema the retry re-asks
	// under — carried across exactly one turn boundary by the gate rather than by
	// a variable here. nil when no guard is configured; every method tolerates
	// that. See guardretry.go.
	guardRetry := e.newGuardGate()

	// loop is the P53.2 loop gate plus the window it decides on (P63.9). nil when
	// loop detection is disabled; every method tolerates that, so the gate below
	// carries no nil check. The per-turn half of the concern is the loopVerdict it
	// returns, declared inside the turn loop.
	loop := e.newLoopGuard()

	// P53.6: announce the shim once per run. What the catalog *costs* the
	// headroom estimate is the compaction guard's business and is measured
	// there; this is only the user-facing notice.
	if e.toolShim && e.tools != nil {
		emit(Event{Kind: KindNotice, Text: "tool-call shim active (provider.tool_call_shim) — tools are described in the prompt and called as tagged JSON, not via the provider's tool protocol"})
	}

	e.writtenFilesMu.Lock()
	e.writtenFiles = make(map[string]struct{})
	e.writtenFilesMu.Unlock()

	// nudges tracks the corrective/nudge scaffolding injected this run (guard
	// retries, the P28.3 zero-tool nudge, the P34.1 empty-answer nudge, and the
	// P52.3 tool-failure nudge — the last two bounded to one attempt per run) so
	// it can all be retracted before Run returns — see nudgeState.retractAll
	// (P40.6).
	var nudges nudgeState
	// toolFailures is the P52.3 circuit breaker: it aggregates the per-round
	// IsError signal (previously emitted and then dropped on the floor) so a run
	// whose every tool call fails gets a corrective nudge and, if it keeps
	// failing, ends with a named error instead of burning to maxIterations.
	// Per-Run by construction, like every other counter here.
	var toolFailures toolFailureTracker
	toolRoundsCompleted := 0
	// toolCallAsTextWarned bounds the P34.2 notice to one per run: the check
	// sits on a path a guard retry can re-enter.
	toolCallAsTextWarned := false
	// runUsage accumulates token counts across every turn of this run so the
	// terminal KindDone event can carry a total (P25.5) — previously it was
	// emitted bare, so API/eval-harness clients that only look at the final
	// event (unlike the TUI, which reads the per-turn KindTurnDone events)
	// always saw zero, even though a local/Ollama run's per-turn estimate was
	// computed and displayed live in the TUI status bar the whole time.
	// runUsageEstimated is true when any contributing turn had no real
	// provider-reported usage, so the total is flagged honestly rather than
	// implying every turn's count came straight from the provider.
	var (
		runUsage          provider.Usage
		runUsageSeen      bool
		runUsageEstimated bool
	)

	for iter := 0; iter < e.maxIterations; iter++ {
		select {
		case <-ctx.Done():
			return budget.override(ErrInterrupted)
		default:
		}

		// Budget gate: stop before spending on another paid model call. See
		// runBudget.exceeded for why this gate exists separately from the
		// pre-tool-round one below.
		if err := budget.exceeded(); err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}

		// Allow callers to inject dynamic context or refresh tool metadata
		// before each model turn (e.g. re-read a file, update memory state).
		if e.prepareStep != nil {
			if updated := e.prepareStep(ctx, conv.Messages); updated != nil {
				conv.Messages = updated
				conv.invalidate()
			}
		}

		// P2.6: On the final iteration, if any tool rounds have already completed,
		// inject a step-limit summary instruction and suppress tool schemas so the
		// model produces a plain-text progress summary rather than aborting with an
		// error. If no tools ran yet, skip the injection (model is in its first turn
		// and should simply answer).
		// P59.8: a schema-guard corrective retry is sent with decoding
		// constrained to the required shape, and with tools off — the remaining
		// task at that point is "emit this object", and a grammar plus a tool
		// schema pull the same turn in two directions. takeFormat empties the
		// carry as it reads it, so a *second* failure re-asks under the constraint
		// again rather than latching it on for the rest of the run.
		//
		// Decided before the compaction gate below purely so the gate can be told
		// whether this turn carries tool schemas — that changes the prompt size it
		// is estimating, and a turn without them is not a valid calibration sample
		// (P62.4). The step-limit *injection* still happens after the gate, so the
		// order of side effects on conv is unchanged.
		format := guardRetry.takeFormat()
		suppressTools := format != nil
		stepLimited := iter == e.maxIterations-1 && toolRoundsCompleted > 0
		if stepLimited {
			suppressTools = true
		}

		// Compaction gate: measure headroom and shorten the conversation before
		// spending a turn on a prompt that will not fit. Every decision this
		// concern makes — trigger, summarizer latch, deterministic fallback, the
		// once-per-run context-full notice — is inside. See compact.go.
		compact.beforeTurn(ctx, conv, emit, suppressTools)

		if stepLimited {
			emit(Event{Kind: KindNotice, Text: fmt.Sprintf("step limit reached (%d tool rounds) — asking the model to summarize; raise provider.max_iterations for longer tasks", e.maxIterations)})
			conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: "[Step limit reached. Summarize what you have accomplished, what constraints were met, and what work remains. Do not call any tools.]"},
			}})
		}

		turnStart := time.Now()
		assistant, toolUses, usage, stopReason, err := e.turn(ctx, conv, emit, suppressTools, format)
		if err != nil {
			// P59.2: when the run deadline above is what killed the turn, the
			// adapter reports whatever cancellation looked like from its side —
			// typically a transport error, which callers classify as
			// backend-unavailable and would then *wait for and resume*, turning a
			// deliberate budget abort into a retry loop. Re-derive the real
			// reason first so the wall-clock error (fatal by design, unlike a
			// context overflow or a tool-failure trip) is what propagates.
			err = budget.override(err)
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		conv.Append(assistant)

		// P62.4: the provider just reported how many prompt tokens that request
		// really was. Fold it into the estimate's correction before the next
		// turn's headroom check, which is the only thing standing between a long
		// run and a local server silently dropping its oldest turns.
		compact.afterTurn(usage, e.effectiveContextWindow())

		var runCost float64
		if e.cost != nil && usage != nil {
			if usage.IsEstimated {
				// Tokens still count toward MaxTokensPerRun even when the
				// provider gave no real usage (local/Ollama models) — only the
				// dollar figure is skipped, since pricing an estimate would be
				// misleading (P10.5).
				e.cost.AddTokens(*usage)
			} else {
				runCost = e.cost.Add(e.model, *usage)
			}
		}
		emit(Event{Kind: KindTurnDone, Usage: usage, CostUSD: runCost})
		if usage != nil {
			runUsageSeen = true
			runUsage.InputTokens += usage.InputTokens
			runUsage.OutputTokens += usage.OutputTokens
			runUsage.CacheCreationTokens += usage.CacheCreationTokens
			runUsage.CacheReadTokens += usage.CacheReadTokens
			if usage.IsEstimated {
				runUsageEstimated = true
			}
		}

		// Assemble a structured trace for this turn. Tool calls (if any) are
		// filled in after they run below; for a final turn it is emitted now.
		tr := e.newTrace(iter, usage, turnStart)

		// P2.6: If we suppressed tools but the model hallucinated tool calls,
		// discard them so the turn is treated as a final text answer.
		if suppressTools && len(toolUses) > 0 {
			toolUses = nil
		}

		// nativeCalls is the count of tool calls that arrived through the
		// provider's own protocol, captured before the shim parse below can add
		// to toolUses — the P59.6 checks need to tell "the model emitted a real
		// call" from "we recovered one from its prose".
		nativeCalls := len(toolUses)

		// P53.6: under the shim the request carried no tool schemas, so any call
		// this turn made is sitting in the reply text. Parse it here — before the
		// zero-tool-calls branch below — so the whole rest of the loop (loop
		// detector, budget gates, runTools, failure breaker) sees an ordinary
		// tool round and needs no knowledge of how the calls arrived.
		shimParseErr := ""
		if e.toolShim && !suppressTools && len(toolUses) == 0 {
			calls, err := toolshim.Parse(assistantText(assistant), e.exposedToolNames(), fmt.Sprintf("shim-%d", iter))
			switch {
			case errors.Is(err, toolshim.ErrNoCalls):
				// No attempt at all — an ordinary final answer.
			case err != nil:
				// An attempt that doesn't meet the contract. Nothing runs: a
				// reply the parser had to guess at is not an input a permission
				// prompt should be built from.
				shimParseErr = err.Error()
			default:
				toolUses = calls
			}
		}

		// P59.6: both prose-tool-call checks used to sit inside the zero-call
		// branch below, which asks "can this model produce the protocol at all".
		// The question that actually matters for the 14-27B class is how
		// *often* — a model that emits one real call and prints two more as
		// prose in the same reply is the common shape, and the zero-call gate
		// made it invisible: no notice, and under the shim the printed calls
		// were silently dropped rather than parsed or declined. Run the checks
		// on any turn whose reply text carries the shape.
		//
		// shimMixedPending carries the mixed-round correction past the tool
		// round, for the same reason loopVerdict.nudge does: the native calls
		// this turn made are real and must still be answered with tool_result
		// blocks, so nothing may be appended between them and their results.
		shimMixedPending := false
		if e.toolShim && !suppressTools && nativeCalls > 0 {
			// The parsed calls are deliberately *not* dispatched. Both readings
			// of a mixed round are defensible — parsed calls are unprivileged
			// and pass the same permission gate — but declining is the safer
			// default and the one consistent with the parser's existing
			// decline-rather-than-repair posture: a turn that half-speaks two
			// protocols is a turn whose intent is genuinely ambiguous, and
			// running both halves would double-execute a model that wrote the
			// same call twice in two dialects.
			_, err := toolshim.Parse(assistantText(assistant), e.exposedToolNames(), fmt.Sprintf("shim-mixed-%d", iter))
			if !errors.Is(err, toolshim.ErrNoCalls) {
				shimMixedPending = true
			}
		}
		//
		// The zero-call gate is dropped; the "no tool call has succeeded all
		// run" gate is deliberately kept for the zero-native-call case, because
		// there it is still doing real work: a model that has already made a
		// structured call and then quotes JSON in its answer is quoting, not
		// failing, and warning about that is the false positive
		// TestToolCallAsTextNoticeSkippedAfterRealToolCall exists to prevent.
		// A *mixed* turn is different in kind — the printed call was written to
		// be executed and silently wasn't — so it warns regardless of history.
		if !toolCallAsTextWarned && !suppressTools && (nativeCalls > 0 || toolRoundsCompleted == 0) {
			if names := e.exposedToolNames(); len(names) > 0 && looksLikeToolCallJSON(assistantText(assistant), names) {
				toolCallAsTextWarned = true
				if nativeCalls > 0 {
					// A partial-protocol turn. The P34.2 claim below would be
					// wrong here — this model demonstrably *can* call a tool —
					// so name what was actually lost instead.
					emit(Event{Kind: KindNotice, Text: fmt.Sprintf(
						"model wrote a tool call into its prose alongside %d real tool call(s) — the written one did not run", nativeCalls)})
				} else {
					// P34.2: the qwen2.5-coder:1.5b signature — a model whose
					// Ollama manifest claims tool support simply cannot speak
					// the protocol, then fabricates the results it never
					// fetched. Name it once; never block, since a prose-only
					// session with such a model is still legitimate and the user
					// may not care.
					emit(Event{Kind: KindNotice, Text: "model emitted a tool call as text — it may not support tool calling; run `aegis doctor` to check this model"})
				}
			}
		}

		if len(toolUses) == 0 {
			tr.WallMS = time.Since(turnStart).Milliseconds()
			emit(Event{Kind: KindTrace, Trace: &tr})
			// If the model was cut off by the token limit, inject a continuation
			// prompt and loop rather than silently returning a truncated response.
			if stopReason == provider.StopMaxTokens {
				conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
					provider.TextBlock{Text: "[Your response was cut off at the token limit. Continue from where you left off, completing any remaining task steps.]"},
				}})
				continue
			}
			// P53.6: the model tried to call a tool under the shim and got the
			// format wrong. Hand back the specific reason and the contract, and
			// let it try again — bounded, because a model that cannot produce
			// the shape after two corrections is not going to on the third, and
			// its prose answer is still worth surfacing.
			if shimParseErr != "" {
				if nudges.shimFormatNudges < shimFormatNudgeMax {
					nudges.shimFormatNudges++
					emit(Event{Kind: KindNotice, Text: "tool-call shim: could not parse the model's tool call (" + shimParseErr + ") — asking it to reformat"})
					conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
						provider.TextBlock{Text: shimFormatNudgeText(shimParseErr)},
					}})
					continue
				}
				emit(Event{Kind: KindNotice, Text: "tool-call shim: the model never produced a parsable tool call — answering from its text instead"})
			}
			// P28.3: nothing has been done yet this run (no tool round has
			// completed), tools are actually available, retries remain, and the
			// triggering request plainly reads as an actionable task — the
			// deepseek-r1:8b live-eval failure mode, where the model's
			// reasoning gets dumped as a text-only final answer instead of
			// being followed by a real tool call. Ask it to reconsider and act
			// rather than silently accepting the text-only turn as done.
			if e.zeroToolNudgeMax >= 0 && toolRoundsCompleted == 0 && nudges.zeroToolNudges < e.zeroToolNudgeMax &&
				e.tools != nil && len(e.tools.Schemas()) > 0 &&
				looksActionable(lastUserText(conv.Messages)) {
				nudges.zeroToolNudges++
				emit(Event{Kind: KindNotice, Text: "model answered in text only on what looks like an actionable task — asking it to reconsider and act"})
				conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
					provider.TextBlock{Text: zeroToolNudgeText},
				}})
				continue
			}
			// P34.1: the model ended its turn without error but produced no
			// user-visible text at all, so the user is about to receive an
			// empty reply (observed live with gpt-oss:20b, which emits its
			// conclusion into the thinking channel and stops). Ask once for a
			// plain-text answer. Bounded to a single attempt so a model that
			// simply won't speak can't spin the loop; if the nudge also comes
			// back empty, say so rather than returning silence.
			if assistantText(assistant) == "" {
				if nudges.emptyAnswerNudges == 0 {
					nudges.emptyAnswerNudges++
					emit(Event{Kind: KindNotice, Text: "model ended its turn with no text — asking it for a plain-text answer"})
					conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
						provider.TextBlock{Text: emptyAnswerNudgeText},
					}})
					continue
				}
				emit(Event{Kind: KindNotice, Text: "model produced no text even after being asked for a plain-text answer — the reply is empty"})
			}
			// The P34.2 prose-tool-call notice used to live here, gated on a
			// zero-call turn; P59.6 hoisted it above so a mixed round reaches it
			// too.
			// Guard gate: validate the final answer and, if it fails with retries
			// left, append a corrective and arm the next turn's schema. The
			// retry count stays here in nudgeState — retractAll reads it — and is
			// passed in rather than duplicated. See guardretry.go.
			if guardRetry.review(ctx, conv, emit, assistantText(assistant), toolRoundsCompleted, nudges.guardRetries) {
				nudges.guardRetries++
				continue
			}
			nudges.retractAll(conv)
			doneEv := Event{Kind: KindDone}
			if runUsageSeen {
				u := runUsage
				u.IsEstimated = runUsageEstimated
				doneEv.Usage = &u
			}
			emit(doneEv)
			return nil
		}

		// Loop guard: stop if the model keeps requesting the same tool calls.
		// The decision (abort / nudge / neither) and whether this turn entered the
		// detector's window are the verdict's, not Run's — see loopGuard.check for
		// P53.2(a)'s poll exemption and P53.2(b)'s outcome-decides-the-ending rule.
		// loopVerdict is per-turn state and is scoped to exactly this iteration.
		loopV := loop.check(toolUses, nudges.loopNudges)
		if loopV.abort != nil {
			emit(Event{Kind: KindError, Err: loopV.abort})
			return loopV.abort
		}
		if loopV.notice != "" {
			nudges.loopNudges++
			emit(Event{Kind: KindNotice, Text: loopV.notice})
		}

		// Budget gate: stop before launching another (paid) tool round, and
		// before its side effects, rather than one iteration later.
		if err := budget.exceeded(); err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}

		results, toolTraces, err := e.runTools(ctx, toolUses, emit)
		if err != nil {
			// P59.2: the run deadline cancels tool execution too, so the same
			// re-attribution the model turn needs applies here.
			err = budget.override(err)
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		// P53.6: a shimmed assistant message holds only text — the calls were
		// parsed out of it, never emitted as tool_use blocks — so tool_result
		// blocks here would be orphaned and rejected by the provider. Render
		// them as text instead. Native rounds are unchanged.
		if e.toolShim {
			conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: toolshim.RenderResults(toolUses, results)},
			}})
		} else {
			conv.Append(provider.Message{Role: provider.RoleUser, Content: results})
		}
		toolRoundsCompleted++
		// P59.10: retract the zero-tool nudge here rather than at run end. The
		// nudge is now spent — the gate at the injection site only fires it while
		// toolRoundsCompleted == 0, so a completed round makes it permanently
		// ineligible this run — and retracting a message from the middle of the
		// conversation invalidates a local runner's KV prefix cache from that
		// point on. Doing it now, when only this round sits after it, costs one
		// small re-prefill; doing it at run end costs a re-prefill of everything
		// the run produced in between. See prefixcache_test.go for both halves of
		// the measurement.
		nudges.retractSpentZeroTool(conv)

		tr.ToolCalls = toolTraces
		tr.WallMS = time.Since(turnStart).Milliseconds()
		emit(Event{Kind: KindTrace, Trace: &tr})

		// P53.2(b): the loop gate records a turn *before* its tools run, so the
		// outcome that classifies a cycle as recoverable or fatal is only knowable
		// here.
		loop.noteOutcome(loopV, results)
		// Inject the recoverable-loop corrective now that the round's tool_result
		// blocks are in place, keeping the transcript well-formed — the same
		// injection point the P52.3 nudge below uses.
		if loopV.nudge != "" {
			conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: loopV.nudge},
			}})
		}
		// P59.6: the same injection point, for the mixed-round correction. The
		// native calls of this turn have just run and been answered; now tell
		// the model the ones it *printed* did not, and how to emit them. Bounded
		// by the same counter as the P53.6 format corrective — a model that
		// keeps mixing dialects after two corrections is not going to stop, and
		// its real calls are still working.
		if shimMixedPending {
			if nudges.shimFormatNudges < shimFormatNudgeMax {
				nudges.shimFormatNudges++
				emit(Event{Kind: KindNotice, Text: "tool-call shim: the model printed a tool call in its prose alongside a real one — the printed call was not run; asking it to emit all calls in the shim format"})
				conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
					provider.TextBlock{Text: shimMixedNudgeText()},
				}})
			} else {
				emit(Event{Kind: KindNotice, Text: "tool-call shim: the model is still printing tool calls in its prose — those did not run"})
			}
		}

		// P52.3: consecutive-tool-failure circuit breaker. loopDetector cannot
		// see this stall — it matches on tool name + canonicalized input, and the
		// common local-model failure is a model whose arguments legitimately
		// differ every round (retry edit_file with a slightly different
		// old_string after each "not found"), so every signature is distinct.
		// Count failing rounds instead: nudge at the first threshold, end the run
		// at the second rather than letting it burn to maxIterations.
		toolFailures.record(toolUses, results)
		if toolFailures.shouldAbort() {
			err := toolFailures.abortError()
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		// P59.11: an outstanding corrective becomes spent the moment the failure
		// streak it was correcting ends, so retract it here rather than at run
		// end. Retraction is a mid-history edit and everything after it must be
		// re-prefilled by a local runner; doing it now leaves only the rounds
		// between the nudge and the recovery downstream of the break, instead of
		// the entire rest of the run (measured at 25.9x — see prefixcache_test.go).
		// The correction is not lost: the injection gate below can fire again if
		// failures recur, which is an append and costs nothing.
		if toolFailures.cleared() {
			nudges.retractSpentToolFailure(conv)
		}
		// P63.12: ask the conversation whether a corrective is still standing
		// rather than a flag set when one was injected. Compaction can have
		// deleted it since, and suppressing re-injection on the strength of a
		// message that is no longer there loses the correction silently — no
		// notice, no log line, and a counter that still looks healthy.
		if toolFailures.shouldNudge() && !hasNudge(conv, toolFailureNudgePrefix) &&
			nudges.toolFailureNudges < toolFailureNudgeMax {
			nudges.toolFailureNudges++
			emit(Event{Kind: KindNotice, Text: fmt.Sprintf(
				"%d tool round(s) in a row failed (%s: %s) — asking the model to re-inspect state instead of retrying",
				toolFailures.rounds(), toolFailures.toolLabel(), toolFailures.lastErrorText)})
			conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: toolFailures.nudgeText()},
			}})
		}

		// Drain one pending steer message (if any) between tool rounds, injecting
		// it as a user message so the model adjusts its plan on the next turn.
		if e.steerChan != nil {
			select {
			case steer, ok := <-e.steerChan:
				if ok && len([]rune(steer)) > 0 {
					conv.Append(provider.Message{
						Role:    provider.RoleUser,
						Content: []provider.Block{provider.TextBlock{Text: steer}},
					})
					emit(Event{Kind: KindSteer, Text: steer})
				}
			default:
			}
		}
	}

	err := fmt.Errorf("engine: exceeded max iterations (%d)", e.maxIterations)
	emit(Event{Kind: KindError, Err: err})
	return err
}

// nudgeState counts the corrective/nudge scaffolding a single engine.Run
// injected into the conversation: guard-retry correctives, the P28.3 zero-tool
// nudge, and the P34.1 empty-answer nudge. It exists so the terminal-turn
// bookkeeping (retract everything the run added) lives in one place instead of
// three parallel counters and a matching trio of if-blocks inline in Run
// (P40.6). Behavior is unchanged — retractAll runs the same three retractions,
// each guarded by its own count, in the same order.
type nudgeState struct {
	guardRetries   int
	zeroToolNudges int
	// zeroToolRetracted records that retractSpentZeroTool already stripped the
	// zero-tool nudge mid-run (P59.10), so retractAll does not repeat the walk.
	zeroToolRetracted bool
	emptyAnswerNudges int
	// toolFailureNudges counts the P52.3 consecutive-tool-failure nudge, bounded
	// at toolFailureNudgeMax. A model that ignores it is handled by the abort
	// threshold, not by nagging it every subsequent failing round — that job
	// belongs to the outstanding check, not to the count.
	//
	// P63.12: "is one still outstanding" used to be a bool set here at injection
	// time. It is now asked of the conversation (hasNudge), because the two could
	// disagree: compaction deletes anything before its keep-recent tail, so a
	// corrective could be summarized away while the flag went on claiming it was
	// present — suppressing re-injection of the one nudge whose entire purpose is
	// to be *visible* to the model. The count stays, because a count of what this
	// run has injected is not a claim about the transcript's current contents.
	toolFailureNudges int
	// loopNudges counts the P53.2 recoverable-loop nudge, also bounded to one per
	// run: a second trigger means the corrective didn't work, and the run is
	// aborted instead of nudged again.
	loopNudges int
	// shimFormatNudges counts the P53.6 tool-call-shim format corrective. Bounded
	// at shimFormatNudgeMax rather than one, because unlike the nudges above this
	// one is teaching a syntax rather than redirecting a decision, and a small
	// local model landing it on the second try is a normal outcome.
	shimFormatNudges int
}

// retractAll strips every corrective/nudge prompt this run injected from conv,
// so the scaffolding never leaks into the surfaced transcript or a later run's
// context. Only the families actually used (count > 0) are touched.
// retractSpentZeroTool retracts the P28.3 zero-tool nudge as soon as it can no
// longer fire — i.e. the moment the run's first tool round completes — instead
// of waiting for retractAll (P59.10).
//
// Retraction is a mid-history edit, so a local runner re-prefills everything
// after it on the next request. Measured on Ollama 0.30.10 / qwen3:14b, a run
// whose zero-tool nudge was retracted at run end made the *next* run's first
// turn cost 3604ms of prefill against 71ms for the same run unretracted — a 51x
// regression, and within noise of a cold prefill of the whole conversation
// (3711ms), i.e. the cache was not merely dented but wholly lost. The engine
// already treats the nudge as spent after a tool round (see the
// toolRoundsCompleted == 0 gate at the injection site), so retracting it there
// removes no corrective pressure the run could still have used, and leaves only
// that one round downstream of the break rather than the entire run.
//
// Idempotent: retractAll still runs at the end and must not redo this.
func (n *nudgeState) retractSpentZeroTool(conv *Conversation) {
	if n.zeroToolNudges == 0 || n.zeroToolRetracted {
		return
	}
	n.zeroToolRetracted = true
	retractNudges(conv, zeroToolNudgePrefix)
}

// retractSpentToolFailure retracts the P52.3 tool-failure nudge as soon as the
// failure streak it was correcting ends, instead of waiting for retractAll
// (P59.11).
//
// This is the same trade retractSpentZeroTool makes, but it needed a different
// definition of "spent". The zero-tool nudge is spent by construction — its
// injection gate can never fire again once a tool round completes — whereas the
// tool-failure nudge was only *bounded* to one per run, so retracting it early
// would have silently removed a correction whose failures could still recur.
// The measurement that motivated this (67ms of next-run prefill unretracted
// against 1745ms retracted, 25.9x) was therefore left standing at P59.10.
//
// What makes it safe is pairing the early retraction with re-injectability: the
// nudge is dropped only on an observed recovery (toolFailureTracker.cleared),
// and if failures start again the injection gate fires a fresh nudge quoting the
// *new* error — an append, which no prefix cache minds. So the corrective is
// never absent while it is needed, and the break costs the recovery window
// rather than the whole remainder of the run.
//
// Idempotent, and retractAll still runs at the end for a nudge outstanding when
// the run finished mid-streak.
func (n *nudgeState) retractSpentToolFailure(conv *Conversation) {
	if n.toolFailureNudges == 0 {
		return
	}
	retractNudges(conv, toolFailureNudgePrefix)
}

func (n *nudgeState) retractAll(conv *Conversation) {
	if n.guardRetries > 0 {
		retractGuardCorrectives(conv)
	}
	if n.zeroToolNudges > 0 && !n.zeroToolRetracted {
		retractNudges(conv, zeroToolNudgePrefix)
	}
	if n.emptyAnswerNudges > 0 {
		retractNudges(conv, emptyAnswerNudgePrefix)
	}
	if n.toolFailureNudges > 0 {
		retractNudges(conv, toolFailureNudgePrefix)
	}
	if n.loopNudges > 0 {
		retractNudges(conv, loopNudgePrefix)
	}
	if n.shimFormatNudges > 0 {
		retractNudges(conv, shimFormatNudgePrefix)
	}
}

// retractGuardCorrectives removes guard-retry scaffolding — each corrective
// prompt and the failed assistant answer it was scolding — from the
// conversation after the run has settled on a final surfaced answer (P25.3),
// so the durable transcript (and the model's context on later turns) contains
// only the answer the user actually saw, with no validation meta-text for a
// future turn to echo back. The scaffolding must stay in place *during* the
// retry (the model corrects against it); this runs only once the outcome is
// decided. Messages are identified by content rather than by indices recorded
// at retry time so a compaction or prepare-step rewrite mid-run can't shift
// the bookkeeping onto the wrong messages. Tool-use rounds a retry ran are
// untouched — only the failed text answer and the corrective prompt go.
func retractGuardCorrectives(conv *Conversation) {
	kept := make([]provider.Message, 0, len(conv.Messages))
	removed := false
	for _, m := range conv.Messages {
		if isGuardCorrective(m) {
			if n := len(kept); n > 0 && kept[n-1].Role == provider.RoleAssistant && !hasToolUse(kept[n-1]) {
				kept = kept[:n-1] // the failed answer this corrective was scolding
			}
			removed = true
			continue
		}
		kept = append(kept, m)
	}
	if removed {
		conv.Messages = kept
		conv.invalidate()
	}
}

func isGuardCorrective(m provider.Message) bool {
	if m.Role != provider.RoleUser || len(m.Content) != 1 {
		return false
	}
	tb, ok := m.Content[0].(provider.TextBlock)
	return ok && strings.HasPrefix(tb.Text, guardCorrectivePrefix)
}

func hasToolUse(m provider.Message) bool {
	for _, b := range m.Content {
		if _, ok := b.(provider.ToolUseBlock); ok {
			return true
		}
	}
	return false
}

// zeroToolNudgePrefix opens the P28.3 corrective-nudge prompt; besides framing
// the retry, it is the marker retractZeroToolNudges keys on to strip the
// scaffolding from the durable transcript once the run settles — the same
// pattern guardCorrectivePrefix/retractGuardCorrectives already use.
const zeroToolNudgePrefix = "[Your previous response didn't call any tools"

const zeroToolNudgeText = zeroToolNudgePrefix + ", but this task plainly reads as one that" +
	" requires taking action (editing a file, running a command, searching the repo, etc.), not" +
	" just describing or promising it. If you already know what needs to be done, call the" +
	" appropriate tool now to actually do it, then give a final answer once the action is" +
	" complete. Do not just explain what you would do — do it.]"

// emptyAnswerNudgePrefix opens the P34.1 corrective-nudge prompt and, like
// zeroToolNudgePrefix above, doubles as the marker its retraction keys on.
const emptyAnswerNudgePrefix = "[Your previous response contained no text"

const emptyAnswerNudgeText = emptyAnswerNudgePrefix + " at all — the user saw an empty reply." +
	" Reply now with your final answer as plain text. Put the answer itself in your visible" +
	" response, not in your reasoning.]"

// shimFormatNudgeMax bounds the P53.6 tool-call-shim format corrective per run.
// Two, not one: the first correction is often enough for a model that simply
// fenced its JSON or mistyped a tool name, and a second gives it one honest
// retry — but a model still wrong on the third attempt is not learning the
// syntax, and continuing to nudge would burn the whole iteration budget on
// format instruction instead of surfacing the prose answer it can produce.
const shimFormatNudgeMax = 2

// shimFormatNudgePrefix opens the P53.6 corrective and, like the prefixes
// above, doubles as the marker its retraction keys on. Retraction matters more
// here than elsewhere: the failed attempt and its correction are pure protocol
// scaffolding, and leaving them in the durable transcript would teach a later
// turn that malformed tool calls are part of the conversation.
const shimFormatNudgePrefix = "[Your previous response contained a tool call that could not be parsed"

func shimFormatNudgeText(reason string) string {
	return shimFormatNudgePrefix + ": " + reason + ".\n\n" + toolshim.FormatReminder + "]"
}

// shimMixedNudgeText is the P59.6 mixed-round correction: the turn carried both
// a real tool call and one written out in prose, and only the first ran. It
// deliberately reuses shimFormatNudgePrefix so it retracts with the rest of the
// shim scaffolding and needs no second marker — the two say the same thing to
// the model ("that call did not happen, here is the shape") and differ only in
// why the parser refused.
func shimMixedNudgeText() string {
	return shimFormatNudgePrefix +
		": you also wrote a tool call directly in your reply text, and only the calls you made properly were executed. " +
		"The written one did NOT run and produced no result — do not assume its output.\n\n" +
		toolshim.FormatReminder + "]"
}

// retractNudges removes corrective-nudge scaffolding — each nudge prompt
// opening with prefix, and the tool-call-less assistant answer it was reacting
// to — from the conversation once the run has settled, mirroring
// retractGuardCorrectives: the scaffolding must stay in place *during* the
// retry (the model reconsiders against it) but has no business surviving into
// the durable transcript or a future turn's context. Used for the P28.3
// zero-tool nudge, the P34.1 empty-answer nudge, and the P52.3 tool-failure
// nudge. Only a tool-call-less *assistant* message immediately preceding the
// nudge is dropped with it, so the tool-failure nudge (which follows a user
// tool-results message) removes just itself, leaving the failing round intact.
func retractNudges(conv *Conversation, prefix string) {
	kept := make([]provider.Message, 0, len(conv.Messages))
	removed := false
	for _, m := range conv.Messages {
		if isNudge(m, prefix) {
			if n := len(kept); n > 0 && kept[n-1].Role == provider.RoleAssistant && !hasToolUse(kept[n-1]) {
				kept = kept[:n-1] // the answer this nudge was reacting to
			}
			removed = true
			continue
		}
		kept = append(kept, m)
	}
	if removed {
		conv.Messages = kept
		conv.invalidate()
	}
}

func isNudge(m provider.Message, prefix string) bool {
	if m.Role != provider.RoleUser || len(m.Content) != 1 {
		return false
	}
	tb, ok := m.Content[0].(provider.TextBlock)
	return ok && strings.HasPrefix(tb.Text, prefix)
}

// hasNudge reports whether a corrective with this prefix is *actually* still in
// the conversation (P63.12).
//
// It exists because a stored "I injected one" flag and the transcript can
// disagree: compaction summarizes everything before its keep-recent tail, so a
// corrective older than that tail is deleted outright, and a flag set at
// injection time keeps claiming it is there. retractGuardCorrectives and
// retractNudges already avoid the equivalent trap by identifying messages by
// content rather than by indices recorded at injection time — this is the same
// rule applied to the one place that was still asking a variable instead of the
// conversation.
//
// Linear in conversation length, called at most once per tool round. The
// conversations where this matters are the ones long enough to have been
// compacted, and a scan of a few hundred messages against a tool round that
// just made a model call is not a cost worth trading correctness for.
func hasNudge(conv *Conversation, prefix string) bool {
	for _, m := range conv.Messages {
		if isNudge(m, prefix) {
			return true
		}
	}
	return false
}

// leadingPolitenessRe strips a leading politeness/indirection wrapper so the
// imperative-verb check in looksActionable sees "fix the bug" instead of
// "could you please fix the bug" — polite phrasing is near-universal for task
// requests and shouldn't defeat the check.
var leadingPolitenessRe = regexp.MustCompile(`(?i)^(?:please|can you|could you|would you|will you|` +
	`i need you to|i want you to|i'd like you to|i would like you to)[\s,]+`)

// actionVerbRe matches a leading verb from the vocabulary of imperative
// coding-agent tasks — a cheap, purely local signal (same "regex, never an
// extra model call" philosophy as routing.go's classifyTurn) that a message is
// asking the model to *do* something, which almost always requires a tool
// call, rather than answer a question in prose. Biased toward missing a real
// task (safe: today's no-nudge behavior) over firing on a genuine question —
// the nudge would waste a turn but not corrupt anything.
var actionVerbRe = regexp.MustCompile(`(?i)^(?:fix|implement|add|create|write|edit|refactor|delete|remove|` +
	`rename|move|run|execute|install|update|upgrade|downgrade|generate|build|debug|apply|commit|push|` +
	`revert|merge|rebase|search|find|grep|list|read|check|test|call|configure|deploy|migrate|scan|` +
	`review|clean up|set up|change|modify|rewrite|convert|extract|replace|optimize|document)\b`)

// looksActionable reports whether userText reads like a request that plainly
// requires the model to take action via tools rather than a question or
// discussion prompt a prose answer legitimately satisfies (P28.3).
func looksActionable(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
	}
	text = leadingPolitenessRe.ReplaceAllString(text, "")
	return actionVerbRe.MatchString(text)
}

// toolCallProbe is the shape a model prints when it means to make a tool call
// but cannot emit one: a name plus an argument object. Both spellings of the
// argument key seen in the wild are accepted, and the value is left raw
// because OpenAI-style output encodes it as a JSON *string* rather than an
// object.
type toolCallProbe struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	Parameters json.RawMessage `json:"parameters"`
}

// maxToolCallJSONCandidates bounds the brace scan below so that a long answer
// full of code (every `{` is a candidate start) can't turn this into a
// quadratic scan of the whole text.
const maxToolCallJSONCandidates = 64

// looksLikeToolCallJSON reports whether text contains a JSON object shaped
// like a structured tool call naming one of the tools actually offered to the
// model (P34.2) — the qwen2.5-coder:1.5b signature, where a model whose
// manifest claims tool support prints `{"name": "shell", "arguments": {...}}`
// into its prose instead of emitting a tool call.
//
// Requiring a *known* tool name, rather than any name/arguments pair, is what
// keeps this off ordinary JSON in an answer: the model has to have named
// something it was actually handed. Since the caller only warns, a false
// positive costs a wrong notice, so the check is allowed to be approximate —
// but it is deliberately anchored on the observed shape rather than widened to
// every tool-call dialect, because a notice that fires on prose the user can
// see is *not* a tool call would be worse than silence.
func looksLikeToolCallJSON(text string, names map[string]struct{}) bool {
	// Cheap pre-check: no argument key, no candidate — this exits on
	// essentially every normal answer without scanning anything.
	if !strings.Contains(text, `"arguments"`) && !strings.Contains(text, `"parameters"`) {
		return false
	}
	tried := 0
	for i, r := range text {
		if r != '{' {
			continue
		}
		if tried++; tried > maxToolCallJSONCandidates {
			return false
		}
		// Decode rather than brace-match: the decoder stops at the first
		// complete value and ignores the trailing text around it, which is
		// exactly the "JSON embedded in prose" case, and it gets string
		// escaping right for free.
		var probe toolCallProbe
		if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&probe); err != nil {
			continue // brace opened something that isn't JSON (prose, code)
		}
		if probe.Arguments == nil && probe.Parameters == nil {
			continue // e.g. the wrapper object around a nested tool call
		}
		if _, ok := names[probe.Name]; ok {
			return true
		}
	}
	return false
}

// exposedToolNames returns the set of tool names the model was actually
// offered this run — the same Schemas() the request carried, so the check
// above can't be fooled by a name the model never saw.
func (e *Engine) exposedToolNames() map[string]struct{} {
	if e.tools == nil {
		return nil
	}
	schemas := e.tools.Schemas()
	names := make(map[string]struct{}, len(schemas))
	for _, s := range schemas {
		names[s.Name] = struct{}{}
	}
	return names
}

// pollExempt reports whether a single tool call is a legitimate poll and so
// should be left out of the loop signature (P53.2a). Unknown tools and a nil
// registry answer false, which is the pre-P53.2 behavior.
func (e *Engine) pollExempt(tu provider.ToolUseBlock) bool {
	if e.tools == nil {
		return false
	}
	t, ok := e.tools.Get(tu.Name)
	if !ok {
		return false
	}
	return tool.IsPollExempt(t, tu.Input)
}

// resultsHadError reports whether any tool result in a completed round was an
// error — the signal that classifies a detected cycle as an error loop (fatal)
// rather than a succeeding one (nudgeable). Deliberately "any", not "all": a
// cycle that fails even part of the time is not one a nudge about repeating
// successful work should be describing.
func resultsHadError(results []provider.Block) bool {
	for _, b := range results {
		if res, ok := b.(provider.ToolResultBlock); ok && res.IsError {
			return true
		}
	}
	return false
}

// lastUserText returns the text content of the most recent user message in
// msgs that carries a text block — the triggering request for the current
// turn — skipping any trailing tool-result-only messages.
func lastUserText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != provider.RoleUser {
			continue
		}
		var sb strings.Builder
		for _, b := range msgs[i].Content {
			if tb, ok := b.(provider.TextBlock); ok {
				sb.WriteString(tb.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return ""
}

// coldLoadNoticeThresholdMS is the load_duration below which a KindNotice
// cold-load callout would just be noise from an already-warm model's own
// bookkeeping overhead (Ollama reports a small nonzero load_duration even
// when nothing was actually loaded).
const coldLoadNoticeThresholdMS = 1000

// turn performs a single model call, accumulating the assistant message and any
// tool-use blocks from the stream.
func (e *Engine) turn(ctx context.Context, conv *Conversation, emit EmitFunc, suppressTools bool, format json.RawMessage) (provider.Message, []provider.ToolUseBlock, *provider.Usage, provider.StopReason, error) {
	req := provider.Request{
		Model:       e.model,
		System:      conv.System,
		Messages:    conv.Messages,
		MaxTokens:   e.maxTokens,
		Temperature: e.temperature,
		// P59.8: non-empty only on a schema-guard corrective retry. Adapters
		// that cannot constrain decoding ignore it, and the guard's own check
		// still decides — so this never becomes a correctness dependency on a
		// backend feature.
		Format: format,
	}
	if e.tools != nil {
		req.Tools = e.tools.Schemas()
	}
	if suppressTools {
		req.Tools = nil
	}
	// P53.6: under the shim the schemas move from the request's tools field
	// into the system prompt, and the reply is parsed back into calls by the
	// caller. Appended to conv.System rather than written into it so the
	// durable transcript (and everything that reads it — compaction, session
	// persistence, checkpoints) stays free of harness scaffolding.
	if e.toolShim && !suppressTools {
		if shim := toolshim.Prompt(req.Tools); shim != "" {
			req.System = strings.TrimRight(req.System, "\n") + "\n\n" + shim
		}
		req.Tools = nil
	}

	stream, err := e.adapter.Stream(ctx, req)
	if err != nil {
		return provider.Message{}, nil, nil, provider.StopOther, err
	}

	var (
		text       []byte
		thinking   []provider.ThinkingBlock
		toolUses   []provider.ToolUseBlock
		usage      *provider.Usage
		stopReason provider.StopReason
	)
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			text = append(text, ev.Text...)
			emit(Event{Kind: KindText, Text: ev.Text})
		case provider.EventThinkingDelta:
			emit(Event{Kind: KindThinking, Text: ev.Text})
		case provider.EventThinking:
			if ev.Thinking != nil {
				thinking = append(thinking, *ev.Thinking)
			}
		case provider.EventToolUseStart:
			if ev.ToolUse != nil {
				emit(Event{Kind: KindToolCallStart, ToolName: ev.ToolUse.Name, ToolID: ev.ToolUse.ID})
			}
		case provider.EventToolUse:
			if ev.ToolUse != nil {
				toolUses = append(toolUses, *ev.ToolUse)
			}
		case provider.EventDone:
			usage = ev.Usage
			stopReason = ev.Stop
		case provider.EventError:
			return provider.Message{}, nil, nil, provider.StopOther, ev.Err
		}
	}

	// Providers that don't report usage (common with local/Ollama models) return
	// zero counts. Estimate from the script-aware heuristic (tokenest.Estimate)
	// so compaction thresholds and token-count display remain meaningful.
	if usage != nil && usage.InputTokens == 0 && usage.OutputTokens == 0 {
		usage.InputTokens = conv.estimatedTokens()
		usage.OutputTokens = tokenest.Estimate(string(text))
		usage.IsEstimated = true
	}

	// The native Ollama adapter (P33.9) reports how long this call spent
	// loading the model into memory before inference began. Below the
	// threshold that's just the server's own bookkeeping overhead on an
	// already-warm model; above it, it's the tens-of-seconds cold-load wait
	// P33.4's phase split made visible but couldn't yet name.
	if usage != nil && usage.LoadDurationMS >= coldLoadNoticeThresholdMS {
		emit(Event{Kind: KindNotice, Text: fmt.Sprintf("model cold-loaded (%.1fs)", float64(usage.LoadDurationMS)/1000)})
	}

	// P35.7 diagnostic: Ollama's native adapter reports prompt_eval_count
	// (prefill token count) and prompt_eval_duration alongside load_duration.
	// Logged every turn (not gated on a threshold, unlike the cold-load
	// notice) so a comparison across turns N and N+1 is possible after the
	// fact. Read prompt_eval_duration_ms, NOT the count, for cache reuse: on
	// current Ollama the count stays at the full context size every turn even
	// on a prefix-cache hit (P35.13), so it's the duration collapsing (e.g.
	// 15s->0.1s for a similar-sized prompt) that shows KV-cache reuse is
	// sparing prefill under keep_alive residency (P35.4); a duration that
	// stays proportional to the count means a full reprocess. Zero on every
	// non-Ollama provider, so this is a no-op log line elsewhere.
	if usage != nil && usage.PromptEvalDurationMS > 0 {
		e.logger.Debug("prefill (prompt_eval)",
			"prompt_eval_count", usage.InputTokens,
			"prompt_eval_duration_ms", usage.PromptEvalDurationMS,
		)
	}

	// The conversation must record exactly what the model produced, in order:
	// thinking blocks first (required by Anthropic for tool use), then text,
	// then tool-use blocks.
	var content []provider.Block
	for _, tb := range thinking {
		content = append(content, tb)
	}
	if len(text) > 0 {
		content = append(content, provider.TextBlock{Text: string(text)})
	}
	for _, tu := range toolUses {
		content = append(content, tu)
	}
	return provider.Message{Role: provider.RoleAssistant, Content: content}, toolUses, usage, stopReason, nil
}

// newTrace seeds a TurnTrace from a turn's usage. Per-turn cost is computed
// directly from the pricing catalog (not the cumulative tracker, which remains
// the source of truth for budget enforcement). Estimated usage contributes no
// cost. ToolCalls and WallMS are filled in by the caller.
func (e *Engine) newTrace(index int, usage *provider.Usage, startedAt time.Time) trace.TurnTrace {
	tr := trace.TurnTrace{Index: index, Model: e.model, StartedAt: startedAt}
	if usage != nil {
		tr.InputTokens = usage.InputTokens
		tr.OutputTokens = usage.OutputTokens
		tr.CacheReadTokens = usage.CacheReadTokens
		tr.CacheCreationTokens = usage.CacheCreationTokens
		tr.Estimated = usage.IsEstimated
		if !usage.IsEstimated {
			if p, ok := cost.PricingFor(e.model); ok {
				tr.CostUSD = p.CostUSD(*usage)
			}
		}
	}
	return tr
}

// maxParallelTools bounds how many tool calls run concurrently in one round.
const maxParallelTools = 8

// runTools executes the requested tools and returns tool-result blocks in the
// same order they were requested (as required for tool-use/result pairing).
//
// When the model requests several tools, read/network calls run fully
// concurrently with everything else; write/execute calls take a shared
// exclusive lock so they never race with each other (P8.6 — reads are no
// longer blocked behind a concurrent write/execute call in the same round).
// Event emission is serialized so streamed output is never interleaved
// mid-write. A single tool call takes the simple sequential path.
func (e *Engine) runTools(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, []trace.ToolCall, error) {
	if len(toolUses) <= 1 {
		return e.runToolsSequential(ctx, toolUses, emit)
	}

	results := make([]provider.Block, len(toolUses))
	traces := make([]trace.ToolCall, len(toolUses))

	// Per-call metadata for same-path ordering. Without it, a model that emits
	// "write X, then read X back" as two calls in one round hits a
	// read-before-write race: the read runs concurrently with the write (reads
	// aren't serialized) and can observe the pre-write — often absent — file.
	// serialize[i] marks write/exec calls (they take execLock); paths[i] is the
	// call's filesystem target, if any.
	serialize := make([]bool, len(toolUses))
	paths := make([]string, len(toolUses))
	done := make([]chan struct{}, len(toolUses))
	for i, tu := range toolUses {
		serialize[i] = e.serializeTool(tu.Name, tu.Input)
		paths[i] = toolTargetPath(tu.Input)
		done[i] = make(chan struct{})
	}
	// waitFor[i] lists earlier calls whose completion call i must await: any
	// prior write/exec call targeting the same non-empty path. It applies
	// whether call i reads or writes that path, so both write→read and
	// write→write pairs preserve the model-emitted order for a shared path;
	// calls on distinct paths (or with no path) never gate one another and
	// stay fully concurrent.
	waitFor := make([][]int, len(toolUses))
	for i := range toolUses {
		if paths[i] == "" {
			continue
		}
		for j := 0; j < i; j++ {
			if serialize[j] && paths[j] == paths[i] {
				waitFor[i] = append(waitFor[i], j)
			}
		}
	}

	var (
		emitMu   sync.Mutex // serializes emit across goroutines
		execLock sync.Mutex // exclusive among write/exec calls only
		wg       sync.WaitGroup
		sem      = make(chan struct{}, maxParallelTools)
	)
	safeEmit := func(ev Event) {
		emitMu.Lock()
		emit(ev)
		emitMu.Unlock()
	}

	for i, tu := range toolUses {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tu provider.ToolUseBlock) {
			defer wg.Done()
			defer func() { <-sem }()
			// Signal dependents that this call is complete — even on panic or
			// interrupt below — so a waiter on done[i] can never hang.
			defer close(done[i])
			// A panic in one tool call (a buggy MCP tool, malformed builtin
			// input) must not cross the goroutine boundary: unrecovered, it
			// takes down the whole daemon process — every concurrent
			// session, not just the one that triggered it. Recover here and
			// report it back as an ordinary tool error instead.
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("recovered panic in tool call", "tool", tu.Name, "panic", r, "stack", string(debug.Stack()))
					content := fmt.Sprintf("tool %q panicked: %v", tu.Name, r)
					traces[i] = trace.ToolCall{Name: tu.Name, IsError: true}
					safeEmit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolID: tu.ID, ToolResult: content, ToolIsError: true})
					results[i] = provider.ToolResultBlock{ToolUseID: tu.ID, Content: content, IsError: true}
				}
			}()

			// Wait for earlier same-path writes in this round to finish before
			// touching the path. Deadlock-free: writes never wait on reads, and
			// every awaited call has a lower index — so it was already spawned
			// (holding its own semaphore slot) and runs to completion, closing
			// done[j]. Give up if the run is interrupted.
			for _, j := range waitFor[i] {
				select {
				case <-done[j]:
				case <-ctx.Done():
					return
				}
			}
			if ctx.Err() != nil {
				return
			}

			if serialize[i] {
				execLock.Lock()
				defer execLock.Unlock()
			}

			safeEmit(Event{Kind: KindToolCall, ToolName: tu.Name, ToolID: tu.ID, ToolInput: tu.Input})
			start := time.Now()
			content, isErr := e.executeTool(ctx, tu)
			traces[i] = trace.ToolCall{Name: tu.Name, DurationMS: time.Since(start).Milliseconds(), IsError: isErr}
			safeEmit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolID: tu.ID, ToolResult: content, ToolIsError: isErr})
			results[i] = provider.ToolResultBlock{ToolUseID: tu.ID, Content: content, IsError: isErr}
		}(i, tu)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil, nil, ErrInterrupted
	}
	return results, traces, nil
}

// toolTargetPath extracts a tool call's filesystem target from its JSON input,
// used to order same-path writes and reads within one tool round. Builtin file
// tools (read_file, write_file, edit_file, multiedit, ls, …) all name their
// target "path"; the value is cleaned so equivalent spellings ("f.py",
// "./f.py") compare equal. Returns "" when the input carries no "path" string,
// so non-file tools never gate one another. Matching is exact after cleaning:
// on a case-insensitive filesystem two differently-cased spellings of one path
// won't be ordered, but a model emitting a write→read pair reuses the same
// string, so this is a non-issue in practice and avoids wrongly serializing
// distinct paths on a case-sensitive filesystem.
func toolTargetPath(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var probe struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &probe); err != nil || probe.Path == "" {
		return ""
	}
	return filepath.Clean(probe.Path)
}

// runToolsSequential is the simple in-order path used for a single tool call.
func (e *Engine) runToolsSequential(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, []trace.ToolCall, error) {
	results := make([]provider.Block, 0, len(toolUses))
	traces := make([]trace.ToolCall, 0, len(toolUses))
	for _, tu := range toolUses {
		select {
		case <-ctx.Done():
			return nil, nil, ErrInterrupted
		default:
		}

		emit(Event{Kind: KindToolCall, ToolName: tu.Name, ToolID: tu.ID, ToolInput: tu.Input})
		start := time.Now()
		content, isErr := e.executeTool(ctx, tu)
		traces = append(traces, trace.ToolCall{Name: tu.Name, DurationMS: time.Since(start).Milliseconds(), IsError: isErr})
		emit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolID: tu.ID, ToolResult: content, ToolIsError: isErr})

		results = append(results, provider.ToolResultBlock{
			ToolUseID: tu.ID,
			Content:   content,
			IsError:   isErr,
		})
	}
	return results, traces, nil
}

// serializeTool reports whether a tool call must run exclusively (write/
// execute capabilities), preventing it from racing other tool calls in the
// same round. Capability is evaluated per-call (tool.EffectiveCapability,
// P25.4c) so a call a tool reclassifies as narrower than its usual
// capability — e.g. a read-only shell command — isn't serialized behind
// concurrent writes/execs for no reason. Unknown tools are treated as serial
// out of caution.
func (e *Engine) serializeTool(name string, input json.RawMessage) bool {
	if e.tools == nil {
		return true
	}
	t, ok := e.tools.Get(name)
	if !ok {
		return true
	}
	switch tool.EffectiveCapability(t, input) {
	case tool.CapWrite, tool.CapExecute:
		return true
	default:
		return false
	}
}

// repairOrphanedToolUses scans the conversation for tool_use blocks in assistant
// messages that have no matching tool_result in a subsequent user message, and
// injects synthetic error results. This prevents providers from rejecting a
// conversation that was interrupted mid-tool-round (e.g. by context cancel).
func repairOrphanedToolUses(msgs []provider.Message) []provider.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Collect all resolved tool_use IDs.
	resolved := make(map[string]bool, len(msgs))
	for _, msg := range msgs {
		if msg.Role != provider.RoleUser {
			continue
		}
		for _, b := range msg.Content {
			if tr, ok := b.(provider.ToolResultBlock); ok {
				resolved[tr.ToolUseID] = true
			}
		}
	}

	// Check whether any assistant message has unresolved tool_use blocks.
	hasOrphans := false
	for _, msg := range msgs {
		if msg.Role != provider.RoleAssistant {
			continue
		}
		for _, b := range msg.Content {
			if tu, ok := b.(provider.ToolUseBlock); ok && !resolved[tu.ID] {
				hasOrphans = true
				break
			}
		}
		if hasOrphans {
			break
		}
	}
	if !hasOrphans {
		return msgs
	}

	// Rebuild the message list, inserting synthetic error results after each
	// assistant message that has orphaned tool_use blocks.
	out := make([]provider.Message, 0, len(msgs)+1)
	skip := make(map[int]bool) // next-user-message indices already merged
	for i, msg := range msgs {
		if skip[i] {
			continue
		}
		out = append(out, msg)
		if msg.Role != provider.RoleAssistant {
			continue
		}

		var synth []provider.Block
		for _, b := range msg.Content {
			if tu, ok := b.(provider.ToolUseBlock); ok && !resolved[tu.ID] {
				synth = append(synth, provider.ToolResultBlock{
					ToolUseID: tu.ID,
					Content:   fmt.Sprintf("tool call interrupted; %s did not run", tu.Name),
					IsError:   true,
				})
			}
		}
		if len(synth) == 0 {
			continue
		}

		nextIdx := i + 1
		if nextIdx < len(msgs) && msgs[nextIdx].Role == provider.RoleUser {
			// Merge synthetic results into the existing user message.
			combined := make([]provider.Block, len(msgs[nextIdx].Content)+len(synth))
			copy(combined, msgs[nextIdx].Content)
			copy(combined[len(msgs[nextIdx].Content):], synth)
			out = append(out, provider.Message{Role: provider.RoleUser, Content: combined})
			skip[nextIdx] = true
		} else {
			out = append(out, provider.Message{Role: provider.RoleUser, Content: synth})
		}
	}
	return out
}

// registeredToolNames lists every tool registered on reg (regardless of
// exposure/deferred state — the model should be told every real name, not
// just what's currently exposed), sorted and comma-joined for use in a
// model-visible error message (P39.2): a small local model that invents a
// tool name can self-correct from this list instead of spending a turn
// guessing at a name that doesn't exist.
func registeredToolNames(reg *tool.Registry) string {
	if reg == nil {
		return "(none)"
	}
	all := reg.All()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// executeTool looks up and runs a single tool, converting failures into
// model-visible error results rather than aborting the whole run.
func (e *Engine) executeTool(ctx context.Context, tu provider.ToolUseBlock) (string, bool) {
	if e.tools == nil {
		return fmt.Sprintf("no tools available (requested %q)", tu.Name), true
	}
	// Let a meta-tool (tool_search) discover the registry actually governing
	// this run — which, when the caller scopes exposure per session, is a
	// clone rather than the tool's own construction-time reference.
	ctx = tool.WithRegistry(ctx, e.tools)
	if e.workdir != "" {
		ctx = tool.WithWorkdir(ctx, e.workdir)
	}
	ctx = tool.WithExtraRoots(ctx, e.extraRoots)
	t, ok := e.tools.Get(tu.Name)
	if !ok {
		return fmt.Sprintf("unknown tool %q; registered tools: %s", tu.Name, registeredToolNames(e.tools)), true
	}
	if e.gate != nil {
		if allowed, reason := e.gate.Check(ctx, t, tu.Input); !allowed {
			e.logger.Info("tool call blocked by gate", "tool", tu.Name, "reason", reason)
			return reason, true
		}
	}
	if e.hooks != nil {
		if err := e.hooks.PreToolUse(ctx, tu.Name, tu.Input); err != nil {
			e.logger.Info("tool call blocked by hook", "tool", tu.Name, "err", err)
			return fmt.Sprintf("blocked by hook: %v", err), true
		}
	}
	res, err := t.Execute(ctx, tu.Input)
	content, isErr := res.Content, res.IsError
	if err != nil {
		e.logger.Warn("tool execution error", "tool", tu.Name, "err", err)
		content, isErr = fmt.Sprintf("tool error: %v", err), true
	}
	// P63.8: effective capability (P25.4c), not the static one — a tool that
	// reclassifies into CapWrite for a specific call would otherwise have its
	// written paths go unrecorded, costing that call its output-guard file
	// validation and its quarantine-on-fail rollback. Same reasoning that moved
	// ContextualGate (P32.2) and ScopeGate (P63.3) off the static capability,
	// and it makes this branch agree with the redaction branch below, which
	// reads the same tool and the same input.
	if !isErr && tool.EffectiveCapability(t, tu.Input) == tool.CapWrite {
		paths := writtenPathsFromInput(tu.Input)
		if len(paths) == 0 {
			// P32.6: writtenPathsFromInput only recognizes "path"/"file_path"/
			// "edits[].path". A write-capability tool using a different input
			// shape (an MCP tool, or a future builtin) silently gets no
			// output-guard file validation and no quarantine-on-fail rollback —
			// surface that gap instead of letting it degrade silently.
			e.logger.Warn("write-capability tool call yielded no paths for output-guard coverage", "tool", tu.Name)
		}
		e.recordWrittenPaths(paths)
	}
	if !isErr && e.redactSecrets && tool.EffectiveCapability(t, tu.Input) == tool.CapRead {
		// P24.12 / FIND-09: opt-in scrub of tool-read file content for secret
		// patterns before it's appended to the conversation sent to whichever
		// provider is configured (a cloud API by default has no visibility
		// restriction on what a file read surfaces). Strictly best-effort and
		// never blocking — a scan failure or gitleaks being absent leaves
		// content untouched, since the tool result must still reach the model
		// either way. Effective capability (P25.4c) so a `cat` of a
		// secrets-bearing file gets the same scrub a read_file call would.
		if redacted, findings, scanErr := redactSecretsFn(ctx, content); scanErr == nil && len(findings) > 0 {
			content = redacted
			e.logger.Info("redacted secret pattern(s) from tool output", "tool", tu.Name, "count", len(findings))
		}
	}
	if e.hooks != nil {
		e.hooks.PostToolUse(ctx, tu.Name, tu.Input, content, isErr)
	}
	return content, isErr
}

// writtenPathsFromInput extracts workspace-relative file paths from a
// write-capability tool call's input, recognizing the "path"/"file_path"
// fields used by write_file/edit_file/diagram, and the "edits[].path" shape
// used by multi_edit. Unrecognized shapes (e.g. an MCP or custom write tool
// with different field names) yield no paths — the guard simply won't see
// that tool's output, matching the existing subjectFor limitation in
// internal/permission/rules.go rather than guessing.
func writtenPathsFromInput(input json.RawMessage) []string {
	var args struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		Edits    []struct {
			Path string `json:"path"`
		} `json:"edits"`
	}
	if json.Unmarshal(input, &args) != nil {
		return nil
	}
	var paths []string
	if args.Path != "" {
		paths = append(paths, args.Path)
	}
	if args.FilePath != "" {
		paths = append(paths, args.FilePath)
	}
	for _, e := range args.Edits {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// recordWrittenPaths adds paths to the current run's written-files set.
func (e *Engine) recordWrittenPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	e.writtenFilesMu.Lock()
	defer e.writtenFilesMu.Unlock()
	if e.writtenFiles == nil {
		// Run resets this per run; the lazy init keeps a tool call outside a
		// Run (or before its reset) from panicking on a nil map write.
		e.writtenFiles = make(map[string]struct{})
	}
	for _, p := range paths {
		e.writtenFiles[p] = struct{}{}
	}
}

// maxGuardFiles caps how many written files are read back for guard
// validation, so a task that touches dozens of files doesn't balloon the
// validator prompt or issue that many extra reads.
const maxGuardFiles = 5

// collectWrittenFiles reads back the current content of every file written
// or edited so far this run via the registered read_file tool (so path
// resolution/sandboxing matches whatever wrote it), for the output guard to
// validate against the actual deliverable rather than only the assistant's
// chat summary. Best-effort: a tool registry without read_file, or a read
// failure for a given path, silently yields no content for that path rather
// than failing the run — the guard still gets the chat text either way.
func (e *Engine) collectWrittenFiles(ctx context.Context) []guard.FileContent {
	e.writtenFilesMu.Lock()
	paths := make([]string, 0, len(e.writtenFiles))
	for p := range e.writtenFiles {
		paths = append(paths, p)
	}
	e.writtenFilesMu.Unlock()
	if len(paths) == 0 || e.tools == nil {
		return nil
	}
	reader, ok := e.tools.Get("read_file")
	if !ok {
		return nil
	}
	sort.Strings(paths) // deterministic order for reproducible prompts/tests
	if len(paths) > maxGuardFiles {
		paths = paths[:maxGuardFiles]
	}
	var out []guard.FileContent
	for _, p := range paths {
		input, err := json.Marshal(map[string]string{"path": p})
		if err != nil {
			continue
		}
		res, err := reader.Execute(ctx, input)
		if err != nil || res.IsError {
			continue
		}
		out = append(out, guard.FileContent{Path: p, Content: res.Content})
	}
	return out
}

// assistantText concatenates the text blocks of an assistant message.
func assistantText(m provider.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if t, ok := b.(provider.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

func itoa(n int) string { return strconv.Itoa(n) }
