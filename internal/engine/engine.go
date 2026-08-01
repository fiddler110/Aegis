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

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/security"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
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
}

// Engine runs the agent loop.
type Engine struct {
	adapter             provider.Adapter
	tools               *tool.Registry
	gate                Gate
	compactor           Compactor
	hooks               Hooks
	cost                *cost.Tracker
	prepareStep         PrepareStepFunc
	outputGuard         guard.Func
	outputGuardMax      int
	zeroToolNudgeMax    int
	budgetUSD           float64
	maxTokensPerRun     int
	maxWallClock        time.Duration
	model               string
	maxTokens           int
	temperature         *float64
	maxIterations       int
	loopThreshold       int
	contextWindowTokens int
	steerChan           <-chan string
	redactSecrets       bool
	logger              *slog.Logger
	workdir             string
	extraRoots          []sandbox.Root

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

// wallClockExceeded reports whether this run has outlived maxWallClock, and
// returns the abort error if so. start is the run's own start time, so the
// bound is per-Run — which in the phased drive means per phase turn, the unit
// the drive actually resets around, rather than one global cap that would
// guillotine a long build mid-phase.
func (e *Engine) wallClockExceeded(start time.Time) error {
	if e.maxWallClock <= 0 {
		return nil
	}
	elapsed := time.Since(start)
	if elapsed < e.maxWallClock {
		return nil
	}
	return fmt.Errorf("%w: ran %s of a %s limit — raise cost.max_wall_clock_per_run for longer tasks",
		ErrWallClockLimit, elapsed.Round(time.Second), e.maxWallClock)
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
		zeroToolNudgeMax:    zeroToolNudgeMax,
		budgetUSD:           opts.BudgetUSD,
		maxTokensPerRun:     opts.MaxTokensPerRun,
		maxWallClock:        opts.MaxWallClockPerRun,
		model:               opts.Model,
		maxTokens:           maxTok,
		temperature:         opts.Temperature,
		maxIterations:       maxIter,
		loopThreshold:       loopThreshold,
		contextWindowTokens: opts.ContextWindowTokens,
		steerChan:           opts.SteerChan,
		redactSecrets:       opts.RedactSecrets,
		logger:              logger,
		workdir:             opts.Workdir,
		extraRoots:          opts.ExtraRoots,
	}, nil
}

// Run drives the conversation to a final answer, executing tools as requested.
// It mutates conv in place (appending assistant and tool-result messages) and
// streams progress to emit. Cancelling ctx interrupts the run.
func (e *Engine) Run(ctx context.Context, conv *Conversation, emit EmitFunc) error {
	if emit == nil {
		emit = func(Event) {}
	}

	// P52.15 wall-clock budget baseline. Taken before compaction rather than at
	// the loop, since a compaction pass is a model call that can itself take
	// real time on a local backend — time the operator's bound should cover.
	runStart := time.Now()

	// Repair any tool_use blocks left without a matching tool_result by a
	// previous interrupted run. Without this, most providers reject the
	// conversation with a validation error that permanently locks the session.
	if repaired := repairOrphanedToolUses(conv.Messages); len(repaired) != len(conv.Messages) {
		e.logger.Info("repaired orphaned tool calls", "added", len(repaired)-len(conv.Messages))
		conv.Messages = repaired
		conv.invalidate()
	}

	if e.compactor != nil {
		if out, changed, err := e.compactor.Compact(ctx, conv.System, conv.Messages); err != nil {
			e.logger.Warn("context compaction failed", "err", err)
		} else if changed {
			e.logger.Info("compacted conversation", "before", len(conv.Messages), "after", len(out))
			conv.Messages = out
			conv.invalidate()
		}
	}

	var loop *loopDetector
	if e.loopThreshold > 0 {
		loop = newLoopDetector(e.loopThreshold)
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
	ctxFullWarned := false // one context-nearly-full notice per run, not one per turn
	// toolCallAsTextWarned bounds the P34.2 notice to one per run: the check
	// sits on a path a guard retry can re-enter.
	toolCallAsTextWarned := false
	// compactionFailures counts consecutive proactive-compaction failures
	// within this run (P28.4). Reset to 0 on any successful compaction
	// (LLM-summarized or deterministic-fallback); never carries across runs,
	// since a single Run already loops through every tool round of a long
	// local-model task (the failure mode this guards against).
	compactionFailures := 0
	// compactionLLMFailuresTotal is the cumulative (non-reset) count of LLM
	// summarizer failures this run, and summarizerLatchedOff records that we have
	// given up on the LLM summarizer for the rest of the run (P39.8). A weak local
	// model that reliably returns empty output from the summarization prompt would
	// otherwise be re-tried on every compaction cycle — two wasted LLM calls each
	// time before the deterministic fallback fires (42× "summarizer returned empty
	// output" in one observed run). Once the total crosses the threshold, later
	// compactions skip straight to the deterministic fallback.
	compactionLLMFailuresTotal := 0
	summarizerLatchedOff := false

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
			return ErrInterrupted
		default:
		}

		// Budget gate: stop before spending on another paid model call. This
		// must run before every turn, not just before a tool round (P9 dead
		// zone) — a guard corrective retry, a max-token continuation, and a
		// plain text-only turn are all billed the same as any other turn via
		// cost.Add below, so gating only the tool-round path let a run cycling
		// through guard failures or token-limit continuations burn all the way
		// to the iteration cap without the budget ever aborting it. The
		// tool-round path also re-checks just before runTools further down,
		// so a turn that itself pushes spend over the cap still stops before
		// its tool calls (and their side effects) run, not one iteration late.
		if e.budgetUSD > 0 && e.cost != nil && e.cost.TotalUSD() >= e.budgetUSD {
			err := fmt.Errorf("engine: cost budget reached: spent $%.4f of $%.2f limit", e.cost.TotalUSD(), e.budgetUSD)
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		if e.maxTokensPerRun > 0 && e.cost != nil && e.cost.TotalTokens() >= e.maxTokensPerRun {
			err := fmt.Errorf("engine: token budget reached: used %d of %d token limit", e.cost.TotalTokens(), e.maxTokensPerRun)
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		// P52.15: same gate, the time dimension. Placed with the other two for
		// the same P9 dead-zone reason — a guard corrective retry or a
		// max-token continuation is just as much elapsed time as a tool round.
		if err := e.wallClockExceeded(runStart); err != nil {
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

		// P2.7: Proactive per-turn compaction — check token headroom before every
		// turn so context-limit errors never interrupt a run mid-flight. Cloud
		// providers reject an oversized prompt loudly; local servers (Ollama)
		// silently drop the oldest tokens instead — including the system prompt —
		// so when nothing can be compacted the user gets an explicit notice
		// rather than a model that quietly forgot its instructions.
		if e.contextWindowTokens > 0 {
			est := conv.estimatedTokens()
			if est > e.contextWindowTokens*85/100 {
				pct := est * 100 / e.contextWindowTokens
				compacted := false
				if e.compactor != nil {
					// P39.8: once the LLM summarizer has proven unreliable this run,
					// stop calling it — go straight to the deterministic fallback so
					// we don't burn two empty summary calls per compaction cycle on a
					// model that will only ever return empty. The latch is per-run.
					if summarizerLatchedOff {
						if fc, ok := e.compactor.(FallbackCompactor); ok {
							if out, changed := fc.FallbackCompact(conv.Messages); changed {
								e.logger.Info("proactive compaction: summarizer latched off, using deterministic fallback",
									"before", len(conv.Messages), "after", len(out))
								emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — compacted %d→%d messages (deterministic; summarizer disabled for this run)", pct, len(conv.Messages), len(out))})
								conv.Messages = out
								conv.invalidate()
								compacted = true
							}
						}
					} else if out, changed, compErr := e.compactor.Compact(ctx, conv.System, conv.Messages); compErr != nil {
						e.logger.Warn("proactive compaction failed", "err", compErr)
						compactionFailures++
						compactionLLMFailuresTotal++
						// P39.8: after enough cumulative LLM-summarizer failures this
						// run (not just consecutive), give up on it entirely — a weak
						// local model that reliably returns empty output would otherwise
						// be re-tried every compaction cycle (42× in one observed run).
						if compactionLLMFailuresTotal >= summarizerGiveUpThreshold && !summarizerLatchedOff {
							summarizerLatchedOff = true
							e.logger.Warn("proactive compaction: disabling LLM summarizer for the rest of this run after repeated failures", "failures", compactionLLMFailuresTotal)
						}
						// P28.4: the LLM summarizer has now failed twice in a row for
						// this run — a local model unreliably returning empty output
						// (the observed live-eval failure mode) would otherwise skip
						// compaction indefinitely and drift toward the hard
						// context-window ceiling with no safety valve. Fall back to a
						// deterministic, non-LLM shortening pass instead, if the
						// configured Compactor supports one.
						if compactionFailures >= 2 {
							if fc, ok := e.compactor.(FallbackCompactor); ok {
								if out, changed := fc.FallbackCompact(conv.Messages); changed {
									e.logger.Warn("proactive compaction: summarizer failed twice in a row, applied deterministic fallback",
										"before", len(conv.Messages), "after", len(out))
									emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — summarizer unavailable, applied deterministic fallback compaction (%d→%d messages)", pct, len(conv.Messages), len(out))})
									conv.Messages = out
									conv.invalidate()
									compacted = true
									compactionFailures = 0
								}
							}
						}
					} else if changed {
						e.logger.Info("proactive compaction", "before", len(conv.Messages), "after", len(out))
						emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full — compacted %d→%d messages", pct, len(conv.Messages), len(out))})
						conv.Messages = out
						conv.invalidate()
						compacted = true
						compactionFailures = 0
					}
				}
				if !compacted && !ctxFullWarned && pct >= 95 {
					ctxFullWarned = true
					emit(Event{Kind: KindNotice, Text: fmt.Sprintf("context ~%d%% full and nothing left to compact — the model server may silently drop older turns; consider /compact or a fresh session", pct)})
				}
			}
		}

		// P2.6: On the final iteration, if any tool rounds have already completed,
		// inject a step-limit summary instruction and suppress tool schemas so the
		// model produces a plain-text progress summary rather than aborting with an
		// error. If no tools ran yet, skip the injection (model is in its first turn
		// and should simply answer).
		suppressTools := false
		if iter == e.maxIterations-1 && toolRoundsCompleted > 0 {
			suppressTools = true
			emit(Event{Kind: KindNotice, Text: fmt.Sprintf("step limit reached (%d tool rounds) — asking the model to summarize; raise provider.max_iterations for longer tasks", e.maxIterations)})
			conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: "[Step limit reached. Summarize what you have accomplished, what constraints were met, and what work remains. Do not call any tools.]"},
			}})
		}

		turnStart := time.Now()
		assistant, toolUses, usage, stopReason, err := e.turn(ctx, conv, emit, suppressTools)
		if err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		conv.Append(assistant)

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
			// P34.2: the model wrote a tool call into its prose instead of
			// emitting one, and no tool call has succeeded all run — the
			// qwen2.5-coder:1.5b signature, where a model whose Ollama manifest
			// claims tool support simply cannot speak the protocol, then
			// fabricates the results it never fetched. Name it once; never
			// block, since a prose-only session with such a model is still
			// legitimate and the user may not care.
			if !toolCallAsTextWarned && !suppressTools && toolRoundsCompleted == 0 {
				if names := e.exposedToolNames(); len(names) > 0 && looksLikeToolCallJSON(assistantText(assistant), names) {
					toolCallAsTextWarned = true
					emit(Event{Kind: KindNotice, Text: "model emitted a tool call as text — it may not support tool calling; run `aegis doctor` to check this model"})
				}
			}
			if e.outputGuard != nil {
				maxRetries := e.outputGuardMax
				if maxRetries <= 0 {
					maxRetries = 1
				}
				if final := assistantText(assistant); final != "" {
					ok, reason, status := e.outputGuard(ctx, guard.Input{Text: final, Files: e.collectWrittenFiles(ctx)})
					e.logger.Debug("output guard result", "passed", ok, "guard_status", string(status))
					if !ok && nudges.guardRetries < maxRetries {
						nudges.guardRetries++
						emit(Event{Kind: KindGuard, GuardPassed: false, GuardReason: reason, GuardStatus: string(status), GuardRetrying: true})
						corrective := guardCorrectivePrefix + reason +
							". This means the actual deliverable is incomplete or unpolished, not just its" +
							" description. Do not reply with only an acknowledgment, a plan, or a promise to" +
							" fix it later — that will fail validation again."
						if toolRoundsCompleted > 0 {
							corrective += " If you already wrote or edited a file for this task, call your file" +
								" tools now (e.g. edit_file/write_file) to fix the real content directly."
						}
						corrective += " Finish the work now, then give a corrected final answer that reflects" +
							" the fixed result. Address the original request only — never mention this" +
							" validation step or include verdict words like PASS or FAIL in your answer.]"
						conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
							provider.TextBlock{Text: corrective},
						}})
						continue
					}
					if !ok {
						finalReason := "surfacing the response after " + itoa(maxRetries) + " failed validation attempt(s): " + reason
						// P27.16 (FIND-15): retries are exhausted and the failing
						// response is about to be surfaced anyway — quarantine any
						// file(s) this turn wrote rather than leaving the bad write
						// on disk. Reuses the same per-turn checkpoint Snapshotter
						// write_file/edit_file already capture pre-write content
						// into (internal/checkpoint), so this is a plain restore,
						// not a new mechanism. A nil Snapshotter (no checkpoint
						// store wired in, e.g. tests or an embedded engine without
						// one) makes RestoreFiles a no-op — skip rollback rather
						// than error, preserving today's retry-then-surface
						// behavior in that case.
						restored, rerr := checkpoint.SnapshotterFrom(ctx).RestoreFiles(ctx)
						if rerr != nil {
							e.logger.Warn("guard fail: rollback of files written this turn failed", "err", rerr)
						} else if restored > 0 {
							e.logger.Warn("guard fail: rolled back files written this turn", "files_restored", restored)
							finalReason += fmt.Sprintf(" — rolled back %d file(s) written this turn", restored)
						}
						emit(Event{Kind: KindGuard, GuardPassed: false, GuardStatus: string(status),
							GuardReason: finalReason, GuardFilesRestored: restored})
					} else {
						// Genuine pass and fail-open-skip both set ok=true, but the
						// caller can now tell them apart via GuardStatus — without
						// this, "the guard validated and passed" and "the guard
						// silently never ran" were byte-for-byte indistinguishable
						// (FIND-16).
						emit(Event{Kind: KindGuard, GuardPassed: true, GuardStatus: string(status)})
					}
				}
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
		if loop != nil && loop.record(turnSignature(toolUses)) {
			err := fmt.Errorf("engine: aborting suspected loop: identical tool calls repeated %d turns", e.loopThreshold)
			emit(Event{Kind: KindError, Err: err})
			return err
		}

		// Budget gate: stop before launching another (paid) tool round.
		if e.budgetUSD > 0 && e.cost != nil && e.cost.TotalUSD() >= e.budgetUSD {
			err := fmt.Errorf("engine: cost budget reached: spent $%.4f of $%.2f limit", e.cost.TotalUSD(), e.budgetUSD)
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		if e.maxTokensPerRun > 0 && e.cost != nil && e.cost.TotalTokens() >= e.maxTokensPerRun {
			err := fmt.Errorf("engine: token budget reached: used %d of %d token limit", e.cost.TotalTokens(), e.maxTokensPerRun)
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		// P52.15: stop before launching a tool round (and its side effects) once
		// the time bound is spent, not one iteration later.
		if err := e.wallClockExceeded(runStart); err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}

		results, toolTraces, err := e.runTools(ctx, toolUses, emit)
		if err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: results})
		toolRoundsCompleted++

		tr.ToolCalls = toolTraces
		tr.WallMS = time.Since(turnStart).Milliseconds()
		emit(Event{Kind: KindTrace, Trace: &tr})

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
		if toolFailures.shouldNudge() && nudges.toolFailureNudges == 0 {
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

// guardCorrectivePrefix opens every guard corrective message; besides framing
// the retry prompt, it is the marker retractGuardCorrectives keys on to strip
// retry scaffolding from the durable transcript once the run settles.
// summarizerGiveUpThreshold is the cumulative number of LLM-summarizer failures
// in one run after which the engine stops calling the summarizer and compacts
// deterministically for the rest of the run (P39.8). Set above the P28.4
// consecutive-failure fallback trigger (2) so a run gets a couple of real
// attempts — enough to ride out a transient error — before concluding the model
// simply can't summarize and latching the LLM call off.
const summarizerGiveUpThreshold = 4

const guardCorrectivePrefix = "[Your previous response did not pass output validation: "

// nudgeState counts the corrective/nudge scaffolding a single engine.Run
// injected into the conversation: guard-retry correctives, the P28.3 zero-tool
// nudge, and the P34.1 empty-answer nudge. It exists so the terminal-turn
// bookkeeping (retract everything the run added) lives in one place instead of
// three parallel counters and a matching trio of if-blocks inline in Run
// (P40.6). Behavior is unchanged — retractAll runs the same three retractions,
// each guarded by its own count, in the same order.
type nudgeState struct {
	guardRetries      int
	zeroToolNudges    int
	emptyAnswerNudges int
	// toolFailureNudges counts the P52.3 consecutive-tool-failure nudge, bounded
	// to one per run: a model that ignores it is handled by the abort threshold,
	// not by nagging it every subsequent failing round.
	toolFailureNudges int
}

// retractAll strips every corrective/nudge prompt this run injected from conv,
// so the scaffolding never leaks into the surfaced transcript or a later run's
// context. Only the families actually used (count > 0) are touched.
func (n *nudgeState) retractAll(conv *Conversation) {
	if n.guardRetries > 0 {
		retractGuardCorrectives(conv)
	}
	if n.zeroToolNudges > 0 {
		retractNudges(conv, zeroToolNudgePrefix)
	}
	if n.emptyAnswerNudges > 0 {
		retractNudges(conv, emptyAnswerNudgePrefix)
	}
	if n.toolFailureNudges > 0 {
		retractNudges(conv, toolFailureNudgePrefix)
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
func (e *Engine) turn(ctx context.Context, conv *Conversation, emit EmitFunc, suppressTools bool) (provider.Message, []provider.ToolUseBlock, *provider.Usage, provider.StopReason, error) {
	req := provider.Request{
		Model:       e.model,
		System:      conv.System,
		Messages:    conv.Messages,
		MaxTokens:   e.maxTokens,
		Temperature: e.temperature,
	}
	if e.tools != nil {
		req.Tools = e.tools.Schemas()
	}
	if suppressTools {
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
	if !isErr && t.Capability() == tool.CapWrite {
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
