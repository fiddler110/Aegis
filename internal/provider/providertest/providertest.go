// Package providertest offers a deterministic provider.Adapter for tests and
// harnesses that need a model-shaped stream without a model server.
//
// It exists because every consumer that needed one wrote its own (M9): the
// engine's tests, the guard's, the eval harness, the provider decorators'.
// Four near-identical scripted adapters is four places for the *stream shape* —
// which events arrive, in what order, with what timing — to drift from what a
// real adapter produces, and stream shape is exactly what the decorators
// between the engine and the backend are made of. Nothing outside `go test`
// could use one at all, since they all lived in _test.go files.
//
// It is deliberately a non-test package so an eval scenario, a benchmark or a
// diagnostic command can build one.
package providertest

import (
	"context"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
)

// Adapter replays a fixed script: one []provider.Event per turn, in order.
// The zero value is unusable; build one with New.
type Adapter struct {
	name  string
	turns [][]provider.Event
	delay time.Duration

	// requests records every request the adapter was asked to serve, so a test
	// can assert on what the decorator chain above it actually sent.
	requests []provider.Request
	calls    int
}

// New returns an Adapter that replays turns, one per Stream call. A Stream call
// past the end of the script repeats the final turn rather than panicking, so a
// test that provokes an unexpected extra turn fails on its own assertion rather
// than on an index panic.
func New(turns ...[]provider.Event) *Adapter {
	return &Adapter{name: "providertest", turns: turns}
}

// WithName overrides the adapter name reported to decorators that key on it.
func (a *Adapter) WithName(name string) *Adapter {
	a.name = name
	return a
}

// WithEventDelay makes Stream sleep between events instead of filling a
// buffered channel and closing it immediately (M10).
//
// The difference is not cosmetic. A script delivered all at once cannot
// distinguish a decorator that forwards events as they arrive from one that
// buffers the whole turn and replays it at the end — which is precisely how
// CRIT-4 stayed invisible to a green suite while disabling the engine's stall
// heartbeat and its early tool dispatch for every local model. A delay makes
// observation *time* an assertable property.
func (a *Adapter) WithEventDelay(d time.Duration) *Adapter {
	a.delay = d
	return a
}

// Name implements provider.Adapter.
func (a *Adapter) Name() string { return a.name }

// Stream implements provider.Adapter.
func (a *Adapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.requests = append(a.requests, req)
	events := a.turns[min(a.calls, len(a.turns)-1)]
	a.calls++

	if a.delay <= 0 {
		ch := make(chan provider.Event, len(events))
		for _, ev := range events {
			ch <- ev
		}
		close(ch)
		return ch, nil
	}

	ch := make(chan provider.Event)
	go func() {
		defer close(ch)
		for _, ev := range events {
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.delay):
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

// Requests returns every request served so far, in order.
func (a *Adapter) Requests() []provider.Request { return a.requests }

// Calls reports how many Stream calls the adapter has served.
func (a *Adapter) Calls() int { return a.calls }

// Text builds a one-turn script: some text deltas and a normal end-of-turn.
func Text(chunks ...string) []provider.Event {
	evs := make([]provider.Event, 0, len(chunks)+1)
	for _, c := range chunks {
		evs = append(evs, provider.Event{Type: provider.EventTextDelta, Text: c})
	}
	return append(evs, provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn})
}

// ToolCall builds a one-turn script that issues a single structured tool call,
// in the two-event shape every real adapter emits: the announcement the engine
// dispatches on (P67.7), then the assembled call.
func ToolCall(id, name, inputJSON string) []provider.Event {
	call := &provider.ToolUseBlock{ID: id, Name: name, Input: []byte(inputJSON)}
	return []provider.Event{
		{Type: provider.EventToolUseStart, ToolUse: &provider.ToolUseBlock{ID: id, Name: name}},
		{Type: provider.EventToolUse, ToolUse: call},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
}
