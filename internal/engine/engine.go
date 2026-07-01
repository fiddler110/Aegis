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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scottymacleod/aegis/internal/cost"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/tool"
	"github.com/scottymacleod/aegis/internal/trace"
)

// Conversation is the mutable transcript the engine drives.
type Conversation struct {
	System   string
	Messages []provider.Message
}

// Append adds a message to the conversation.
func (c *Conversation) Append(m provider.Message) { c.Messages = append(c.Messages, m) }

// EventKind classifies engine events delivered to consumers (TUI, CLI, logs).
type EventKind string

const (
	KindText       EventKind = "text"        // incremental assistant text
	KindThinking   EventKind = "thinking"    // incremental extended-thinking text
	KindToolCall   EventKind = "tool_call"   // a tool is about to run
	KindToolResult EventKind = "tool_result" // a tool finished
	KindTurnDone   EventKind = "turn_done"   // one model turn completed
	KindTrace      EventKind = "trace"       // per-turn structured trace (server-internal)
	KindDone       EventKind = "done"        // the run finished (final answer)
	KindError      EventKind = "error"       // the run failed
	KindSteer      EventKind = "steer"       // mid-run steering instruction injected
	KindGuard      EventKind = "guard"       // output validation result (warning)
)

// Event is emitted to the consumer-provided sink as the run progresses.
type Event struct {
	Kind        EventKind
	Text        string           // KindText
	ToolName    string           // KindToolCall / KindToolResult
	ToolInput   json.RawMessage  // KindToolCall
	ToolResult  string           // KindToolResult
	ToolIsError bool             // KindToolResult
	Usage       *provider.Usage  // KindTurnDone
	CostUSD     float64          // KindTurnDone: cumulative run cost (0 if untracked)
	Trace       *trace.TurnTrace // KindTrace: per-turn observability record
	Err         error            // KindError
	GuardReason string           // KindGuard: why validation failed
	GuardPassed bool             // KindGuard: whether the guard ultimately passed
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
	Gate                  Gate                                                            // optional; nil means all tool calls are allowed
	Compactor             Compactor                                                       // optional; nil disables context compaction
	Hooks                 Hooks                                                           // optional; nil disables hooks
	Cost                  *cost.Tracker                                                   // optional; nil disables cost tracking
	PrepareStep           PrepareStepFunc                                                 // optional; called before every model turn
	OutputGuard           func(ctx context.Context, text string) (ok bool, reason string) // optional; validates the final answer
	OutputGuardMaxRetries int                                                             // corrective retries on guard failure; 0 -> 1 when a guard is set
	BudgetUSD             float64                                                         // optional; >0 aborts the run past this cost
	Model                 string
	MaxTokens             int
	Temperature           *float64
	MaxIterations         int           // safety cap on tool-call rounds; 0 -> default
	LoopThreshold         int           // identical tool-call turns before aborting; 0 -> default, <0 disables
	ContextWindowTokens   int           // model context window size; >0 enables proactive per-turn compaction at 85% fill
	SteerChan             <-chan string // optional; steering messages injected between tool rounds
	Logger                *slog.Logger
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
	outputGuard         func(ctx context.Context, text string) (bool, string)
	outputGuardMax      int
	budgetUSD           float64
	model               string
	maxTokens           int
	temperature         *float64
	maxIterations       int
	loopThreshold       int
	contextWindowTokens int
	steerChan           <-chan string
	logger              *slog.Logger
}

// ErrInterrupted is returned when the run is cancelled via context.
var ErrInterrupted = errors.New("engine: interrupted")

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
		budgetUSD:           opts.BudgetUSD,
		model:               opts.Model,
		maxTokens:           maxTok,
		temperature:         opts.Temperature,
		maxIterations:       maxIter,
		loopThreshold:       loopThreshold,
		contextWindowTokens: opts.ContextWindowTokens,
		steerChan:           opts.SteerChan,
		logger:              logger,
	}, nil
}

// Run drives the conversation to a final answer, executing tools as requested.
// It mutates conv in place (appending assistant and tool-result messages) and
// streams progress to emit. Cancelling ctx interrupts the run.
func (e *Engine) Run(ctx context.Context, conv *Conversation, emit EmitFunc) error {
	if emit == nil {
		emit = func(Event) {}
	}

	// Repair any tool_use blocks left without a matching tool_result by a
	// previous interrupted run. Without this, most providers reject the
	// conversation with a validation error that permanently locks the session.
	if repaired := repairOrphanedToolUses(conv.Messages); &repaired != &conv.Messages {
		if len(repaired) != len(conv.Messages) {
			e.logger.Info("repaired orphaned tool calls", "added", len(repaired)-len(conv.Messages))
		}
		conv.Messages = repaired
	}

	if e.compactor != nil {
		if out, changed, err := e.compactor.Compact(ctx, conv.System, conv.Messages); err != nil {
			e.logger.Warn("context compaction failed", "err", err)
		} else if changed {
			e.logger.Info("compacted conversation", "before", len(conv.Messages), "after", len(out))
			conv.Messages = out
		}
	}

	var loop *loopDetector
	if e.loopThreshold > 0 {
		loop = newLoopDetector(e.loopThreshold)
	}

	guardRetries := 0
	toolRoundsCompleted := 0

	for iter := 0; iter < e.maxIterations; iter++ {
		select {
		case <-ctx.Done():
			return ErrInterrupted
		default:
		}

		// Allow callers to inject dynamic context or refresh tool metadata
		// before each model turn (e.g. re-read a file, update memory state).
		if e.prepareStep != nil {
			if updated := e.prepareStep(ctx, conv.Messages); updated != nil {
				conv.Messages = updated
			}
		}

		// P2.7: Proactive per-turn compaction — check token headroom before every
		// turn so context-limit errors never interrupt a run mid-flight.
		if e.compactor != nil && e.contextWindowTokens > 0 {
			est := estimateTokens(conv.System, conv.Messages)
			if est > e.contextWindowTokens*85/100 {
				if out, changed, compErr := e.compactor.Compact(ctx, conv.System, conv.Messages); compErr != nil {
					e.logger.Warn("proactive compaction failed", "err", compErr)
				} else if changed {
					e.logger.Info("proactive compaction", "before", len(conv.Messages), "after", len(out))
					conv.Messages = out
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
		if e.cost != nil && usage != nil && !usage.IsEstimated {
			runCost = e.cost.Add(e.model, *usage)
		}
		emit(Event{Kind: KindTurnDone, Usage: usage, CostUSD: runCost})

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
			if e.outputGuard != nil {
				maxRetries := e.outputGuardMax
				if maxRetries <= 0 {
					maxRetries = 1
				}
				if final := assistantText(assistant); final != "" {
					ok, reason := e.outputGuard(ctx, final)
					if !ok && guardRetries < maxRetries {
						guardRetries++
						emit(Event{Kind: KindGuard, GuardPassed: false, GuardReason: reason})
						conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
							provider.TextBlock{Text: "[Your previous response did not pass output validation: " + reason +
								". Revise and produce a corrected final answer.]"},
						}})
						continue
					}
					if !ok {
						emit(Event{Kind: KindGuard, GuardPassed: false,
							GuardReason: "surfacing the response after " + itoa(maxRetries) + " failed validation attempt(s): " + reason})
					}
				}
			}
			emit(Event{Kind: KindDone})
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
	// zero counts. Estimate from character length so compaction thresholds and
	// token-count display remain meaningful.
	if usage != nil && usage.InputTokens == 0 && usage.OutputTokens == 0 {
		usage.InputTokens = estimateTokens(conv.System, conv.Messages)
		usage.OutputTokens = len(text) / 4
		usage.IsEstimated = true
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
// When the model requests several tools, read/network calls run concurrently
// while write/execute calls are serialized (an exclusive lock) so side effects
// never race. Event emission is serialized so streamed output is never
// interleaved mid-write. A single tool call takes the simple sequential path.
func (e *Engine) runTools(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, []trace.ToolCall, error) {
	if len(toolUses) <= 1 {
		return e.runToolsSequential(ctx, toolUses, emit)
	}

	results := make([]provider.Block, len(toolUses))
	traces := make([]trace.ToolCall, len(toolUses))
	var (
		emitMu   sync.Mutex   // serializes emit across goroutines
		execLock sync.RWMutex // shared for read/net, exclusive for write/exec
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

			if e.serializeTool(tu.Name) {
				execLock.Lock()
				defer execLock.Unlock()
			} else {
				execLock.RLock()
				defer execLock.RUnlock()
			}

			safeEmit(Event{Kind: KindToolCall, ToolName: tu.Name, ToolInput: tu.Input})
			start := time.Now()
			content, isErr := e.executeTool(ctx, tu)
			traces[i] = trace.ToolCall{Name: tu.Name, DurationMS: time.Since(start).Milliseconds(), IsError: isErr}
			safeEmit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolResult: content, ToolIsError: isErr})
			results[i] = provider.ToolResultBlock{ToolUseID: tu.ID, Content: content, IsError: isErr}
		}(i, tu)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil, nil, ErrInterrupted
	}
	return results, traces, nil
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

		emit(Event{Kind: KindToolCall, ToolName: tu.Name, ToolInput: tu.Input})
		start := time.Now()
		content, isErr := e.executeTool(ctx, tu)
		traces = append(traces, trace.ToolCall{Name: tu.Name, DurationMS: time.Since(start).Milliseconds(), IsError: isErr})
		emit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolResult: content, ToolIsError: isErr})

		results = append(results, provider.ToolResultBlock{
			ToolUseID: tu.ID,
			Content:   content,
			IsError:   isErr,
		})
	}
	return results, traces, nil
}

// serializeTool reports whether a tool must run exclusively (write/execute
// capabilities), preventing it from racing other tool calls in the same round.
// Unknown tools are treated as serial out of caution.
func (e *Engine) serializeTool(name string) bool {
	if e.tools == nil {
		return true
	}
	t, ok := e.tools.Get(name)
	if !ok {
		return true
	}
	switch t.Capability() {
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

// estimateTokens approximates token count using a 4-chars-per-token heuristic.
// Used when the provider returns zero usage counts (e.g. local/Ollama models).
func estimateTokens(system string, msgs []provider.Message) int {
	chars := len(system)
	for _, m := range msgs {
		for _, b := range m.Content {
			switch v := b.(type) {
			case provider.TextBlock:
				chars += len(v.Text)
			case provider.ToolUseBlock:
				chars += len(v.Name) + len(v.Input)
			case provider.ToolResultBlock:
				chars += len(v.Content)
			}
		}
	}
	return chars / 4
}

// executeTool looks up and runs a single tool, converting failures into
// model-visible error results rather than aborting the whole run.
func (e *Engine) executeTool(ctx context.Context, tu provider.ToolUseBlock) (string, bool) {
	if e.tools == nil {
		return fmt.Sprintf("no tools available (requested %q)", tu.Name), true
	}
	t, ok := e.tools.Get(tu.Name)
	if !ok {
		return fmt.Sprintf("unknown tool %q", tu.Name), true
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
	if e.hooks != nil {
		e.hooks.PostToolUse(ctx, tu.Name, tu.Input, content, isErr)
	}
	return content, isErr
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
