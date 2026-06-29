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
	"sync"

	"github.com/scottymacleod/aegis/internal/cost"
	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/tool"
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
	KindDone       EventKind = "done"        // the run finished (final answer)
	KindError      EventKind = "error"       // the run failed
	KindSteer      EventKind = "steer"       // mid-run steering instruction injected
)

// Event is emitted to the consumer-provided sink as the run progresses.
type Event struct {
	Kind        EventKind
	Text        string          // KindText
	ToolName    string          // KindToolCall / KindToolResult
	ToolInput   json.RawMessage // KindToolCall
	ToolResult  string          // KindToolResult
	ToolIsError bool            // KindToolResult
	Usage       *provider.Usage // KindTurnDone
	CostUSD     float64         // KindTurnDone: cumulative run cost (0 if untracked)
	Err         error           // KindError
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

// Options configures an Engine.
type Options struct {
	Adapter       provider.Adapter
	Tools         *tool.Registry
	Gate          Gate          // optional; nil means all tool calls are allowed
	Compactor     Compactor     // optional; nil disables context compaction
	Hooks         Hooks         // optional; nil disables hooks
	Cost          *cost.Tracker // optional; nil disables cost tracking
	BudgetUSD     float64       // optional; >0 aborts the run past this cost
	Model         string
	MaxTokens     int
	Temperature   *float64
	MaxIterations int           // safety cap on tool-call rounds; 0 -> default
	LoopThreshold int           // identical tool-call turns before aborting; 0 -> default, <0 disables
	SteerChan     <-chan string  // optional; steering messages injected between tool rounds
	Logger        *slog.Logger
}

// Engine runs the agent loop.
type Engine struct {
	adapter       provider.Adapter
	tools         *tool.Registry
	gate          Gate
	compactor     Compactor
	hooks         Hooks
	cost          *cost.Tracker
	budgetUSD     float64
	model         string
	maxTokens     int
	temperature   *float64
	maxIterations int
	loopThreshold int
	steerChan     <-chan string
	logger        *slog.Logger
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
		maxIter = 25
	}
	maxTok := opts.MaxTokens
	if maxTok <= 0 {
		maxTok = 8192
	}
	loopThreshold := opts.LoopThreshold
	if loopThreshold == 0 {
		loopThreshold = 3
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		adapter:       opts.Adapter,
		tools:         opts.Tools,
		gate:          opts.Gate,
		compactor:     opts.Compactor,
		hooks:         opts.Hooks,
		cost:          opts.Cost,
		budgetUSD:     opts.BudgetUSD,
		model:         opts.Model,
		maxTokens:     maxTok,
		temperature:   opts.Temperature,
		maxIterations: maxIter,
		loopThreshold: loopThreshold,
		steerChan:     opts.SteerChan,
		logger:        logger,
	}, nil
}

// Run drives the conversation to a final answer, executing tools as requested.
// It mutates conv in place (appending assistant and tool-result messages) and
// streams progress to emit. Cancelling ctx interrupts the run.
func (e *Engine) Run(ctx context.Context, conv *Conversation, emit EmitFunc) error {
	if emit == nil {
		emit = func(Event) {}
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

	for iter := 0; iter < e.maxIterations; iter++ {
		select {
		case <-ctx.Done():
			return ErrInterrupted
		default:
		}

		assistant, toolUses, usage, err := e.turn(ctx, conv, emit)
		if err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		conv.Append(assistant)

		var runCost float64
		if e.cost != nil && usage != nil {
			runCost = e.cost.Add(e.model, *usage)
		}
		emit(Event{Kind: KindTurnDone, Usage: usage, CostUSD: runCost})

		if len(toolUses) == 0 {
			emit(Event{Kind: KindDone})
			return nil
		}

		// Mid-run compaction: re-check after every 5 tool rounds to prevent
		// unbounded conversation growth during long multi-tool runs.
		if e.compactor != nil && iter > 0 && iter%5 == 0 {
			if out, changed, err := e.compactor.Compact(ctx, conv.System, conv.Messages); err != nil {
				e.logger.Warn("mid-run compaction failed", "err", err)
			} else if changed {
				e.logger.Info("mid-run compaction", "before", len(conv.Messages), "after", len(out))
				conv.Messages = out
			}
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

		results, err := e.runTools(ctx, toolUses, emit)
		if err != nil {
			emit(Event{Kind: KindError, Err: err})
			return err
		}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: results})

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
func (e *Engine) turn(ctx context.Context, conv *Conversation, emit EmitFunc) (provider.Message, []provider.ToolUseBlock, *provider.Usage, error) {
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

	stream, err := e.adapter.Stream(ctx, req)
	if err != nil {
		return provider.Message{}, nil, nil, err
	}

	var (
		text     []byte
		thinking []provider.ThinkingBlock
		toolUses []provider.ToolUseBlock
		usage    *provider.Usage
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
		case provider.EventError:
			return provider.Message{}, nil, nil, ev.Err
		}
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
	return provider.Message{Role: provider.RoleAssistant, Content: content}, toolUses, usage, nil
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
func (e *Engine) runTools(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, error) {
	if len(toolUses) <= 1 {
		return e.runToolsSequential(ctx, toolUses, emit)
	}

	results := make([]provider.Block, len(toolUses))
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
			content, isErr := e.executeTool(ctx, tu)
			safeEmit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolResult: content, ToolIsError: isErr})
			results[i] = provider.ToolResultBlock{ToolUseID: tu.ID, Content: content, IsError: isErr}
		}(i, tu)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil, ErrInterrupted
	}
	return results, nil
}

// runToolsSequential is the simple in-order path used for a single tool call.
func (e *Engine) runToolsSequential(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, error) {
	results := make([]provider.Block, 0, len(toolUses))
	for _, tu := range toolUses {
		select {
		case <-ctx.Done():
			return nil, ErrInterrupted
		default:
		}

		emit(Event{Kind: KindToolCall, ToolName: tu.Name, ToolInput: tu.Input})
		content, isErr := e.executeTool(ctx, tu)
		emit(Event{Kind: KindToolResult, ToolName: tu.Name, ToolResult: content, ToolIsError: isErr})

		results = append(results, provider.ToolResultBlock{
			ToolUseID: tu.ID,
			Content:   content,
			IsError:   isErr,
		})
	}
	return results, nil
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
