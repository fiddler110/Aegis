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

	"github.com/scottymacleod/agentharness/internal/cost"
	"github.com/scottymacleod/agentharness/internal/provider"
	"github.com/scottymacleod/agentharness/internal/tool"
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
	KindToolCall   EventKind = "tool_call"   // a tool is about to run
	KindToolResult EventKind = "tool_result" // a tool finished
	KindTurnDone   EventKind = "turn_done"   // one model turn completed
	KindDone       EventKind = "done"        // the run finished (final answer)
	KindError      EventKind = "error"       // the run failed
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
	MaxIterations int // safety cap on tool-call rounds; 0 -> default
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
		toolUses []provider.ToolUseBlock
		usage    *provider.Usage
	)
	for ev := range stream {
		switch ev.Type {
		case provider.EventTextDelta:
			text = append(text, ev.Text...)
			emit(Event{Kind: KindText, Text: ev.Text})
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
	// text first, then tool-use blocks.
	var content []provider.Block
	if len(text) > 0 {
		content = append(content, provider.TextBlock{Text: string(text)})
	}
	for _, tu := range toolUses {
		content = append(content, tu)
	}
	return provider.Message{Role: provider.RoleAssistant, Content: content}, toolUses, usage, nil
}

// runTools executes each requested tool and returns tool-result blocks in order.
func (e *Engine) runTools(ctx context.Context, toolUses []provider.ToolUseBlock, emit EmitFunc) ([]provider.Block, error) {
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
