package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/cost"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// P67.7 tests. The property under test is a *timing* one — a tool call starts
// before the turn that requested it has finished streaming — so these need an
// adapter that can hold a stream open, which scriptedAdapter (which buffers
// everything and closes immediately) cannot.

// heldAdapter streams a turn's events and then blocks before EventDone until
// release is closed, so a test can observe what the engine did with the tool
// calls it has already received while the model is still "generating".
type heldAdapter struct {
	pre     []provider.Event // emitted immediately
	post    []provider.Event // emitted after release closes
	release chan struct{}
	// streamErr, when set, is delivered instead of post — the failed-stream case.
	streamErr error

	mu    sync.Mutex
	calls int
	// later turns, if any, are answered from this script and never held.
	then [][]provider.Event
}

func (h *heldAdapter) Name() string { return "held" }

func (h *heldAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	h.mu.Lock()
	n := h.calls
	h.calls++
	h.mu.Unlock()

	ch := make(chan provider.Event)
	if n > 0 {
		events := []provider.Event{{Type: provider.EventDone, Stop: provider.StopEndTurn}}
		if n-1 < len(h.then) {
			events = h.then[n-1]
		}
		go func() {
			defer close(ch)
			for _, ev := range events {
				select {
				case ch <- ev:
				case <-ctx.Done():
					return
				}
			}
		}()
		return ch, nil
	}

	go func() {
		defer close(ch)
		send := func(ev provider.Event) bool {
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for _, ev := range h.pre {
			if !send(ev) {
				return
			}
		}
		select {
		case <-h.release:
		case <-ctx.Done():
			return
		}
		if h.streamErr != nil {
			send(provider.Event{Type: provider.EventError, Err: h.streamErr})
			return
		}
		for _, ev := range h.post {
			if !send(ev) {
				return
			}
		}
	}()
	return ch, nil
}

// signalTool records when each call started and blocks until told to finish, so
// a test can assert "this started" without waiting on wall-clock time.
type signalTool struct {
	name string
	cap  tool.Capability

	mu      sync.Mutex
	started map[string]chan struct{} // per-msg start signal
	hold    chan struct{}            // nil means "return immediately"
	order   []string                 // completion order, for the ordering tests
}

func newSignalTool(name string, c tool.Capability) *signalTool {
	return &signalTool{name: name, cap: c, started: map[string]chan struct{}{}}
}

func (s *signalTool) Name() string                 { return s.name }
func (s *signalTool) Description() string          { return "test tool" }
func (s *signalTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *signalTool) Capability() tool.Capability  { return s.cap }

func (s *signalTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Msg   string `json:"msg"`
		Delay string `json:"delay"`
	}
	_ = json.Unmarshal(input, &args)

	s.mu.Lock()
	ch, ok := s.started[args.Msg]
	if !ok {
		ch = make(chan struct{})
		s.started[args.Msg] = ch
	}
	hold := s.hold
	s.mu.Unlock()
	close(ch)

	if d, err := time.ParseDuration(args.Delay); err == nil && d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return tool.Result{}, ctx.Err()
		}
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return tool.Result{}, ctx.Err()
		}
	}
	s.mu.Lock()
	s.order = append(s.order, args.Msg)
	s.mu.Unlock()
	return tool.Result{Content: "ran:" + args.Msg}, nil
}

// startedCh returns the channel closed when the call carrying msg begins.
func (s *signalTool) startedCh(msg string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.started[msg]; ok {
		return ch
	}
	ch := make(chan struct{})
	s.started[msg] = ch
	return ch
}

func toolUseEvent(id, name, input string) provider.Event {
	return provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
		ID: id, Name: name, Input: json.RawMessage(input),
	}}
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestToolDispatchesBeforeTheTurnFinishesStreaming is the item itself. The first
// tool call of a round must not wait for the last one to finish generating.
func TestToolDispatchesBeforeTheTurnFinishesStreaming(t *testing.T) {
	st := newSignalTool("peek", tool.CapRead)
	reg := tool.NewRegistry()
	if err := reg.Register(st); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	ad := &heldAdapter{
		release: release,
		pre: []provider.Event{
			toolUseEvent("a", "peek", `{"msg":"first"}`),
		},
		post: []provider.Event{
			toolUseEvent("b", "peek", `{"msg":"second"}`),
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
	}
	eng, err := New(Options{Adapter: ad, Tools: reg, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, func(Event) {}) }()

	// The whole claim: the first call runs while the turn is still streaming.
	waitClosed(t, st.startedCh("first"), "the first tool call to start before the stream ended")
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not finish")
	}
}

// TestResultOrderIsReceiptOrder is P67.7's first named constraint. Execution
// order was never deterministic and still is not; the wire order of results must
// remain the order the provider emitted the calls, or every replay and eval
// fixture moves.
func TestResultOrderIsReceiptOrder(t *testing.T) {
	st := newSignalTool("peek", tool.CapRead)
	reg := tool.NewRegistry()
	if err := reg.Register(st); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	// The first call sleeps; the later ones do not, so completion order is the
	// reverse of receipt order.
	ad := &heldAdapter{
		release: release,
		pre: []provider.Event{
			toolUseEvent("a", "peek", `{"msg":"one","delay":"150ms"}`),
			toolUseEvent("b", "peek", `{"msg":"two"}`),
		},
		post: []provider.Event{
			toolUseEvent("c", "peek", `{"msg":"three"}`),
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
	}
	eng, err := New(Options{Adapter: ad, Tools: reg, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, func(Event) {}) }()
	waitClosed(t, st.startedCh("one"), "the first call to start")
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got []string
	for _, m := range conv.Messages {
		for _, blk := range m.Content {
			if tr, ok := blk.(provider.ToolResultBlock); ok {
				got = append(got, tr.ToolUseID)
			}
		}
	}
	want := []string{"a", "b", "c"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("result order = %v, want %v (receipt order)", got, want)
	}
	// And the fixture has to have actually completed out of order, or the
	// assertion above proves nothing.
	st.mu.Lock()
	order := append([]string(nil), st.order...)
	st.mu.Unlock()
	if len(order) > 0 && order[0] == "one" {
		t.Fatalf("fixture completed in receipt order (%v); it cannot distinguish the two", order)
	}
}

// TestFailedStreamAbandonsInFlightCalls is P67.7's third named constraint: a
// cancelled or failed stream must abandon in-flight calls, and their results must
// not be appended to a turn whose assistant message never completed.
func TestFailedStreamAbandonsInFlightCalls(t *testing.T) {
	st := newSignalTool("peek", tool.CapRead)
	st.hold = make(chan struct{}) // the call blocks until the test lets it go
	reg := tool.NewRegistry()
	if err := reg.Register(st); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	ad := &heldAdapter{
		release:   release,
		streamErr: errors.New("stream exploded"),
		pre:       []provider.Event{toolUseEvent("a", "peek", `{"msg":"doomed"}`)},
	}
	eng, err := New(Options{Adapter: ad, Tools: reg, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	before := len(conv.Messages)
	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, func(Event) {}) }()

	waitClosed(t, st.startedCh("doomed"), "the doomed call to start")
	close(release) // the stream now fails
	// Let the in-flight call finish on its own, so the test proves the result is
	// discarded rather than merely never produced.
	time.Sleep(50 * time.Millisecond)
	close(st.hold)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stream exploded") {
			t.Fatalf("Run err = %v, want the stream error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not finish; abort() likely did not wait for the in-flight call")
	}
	if len(conv.Messages) != before {
		t.Fatalf("a failed turn appended %d message(s); results must not reach the conversation",
			len(conv.Messages)-before)
	}
}

// TestEarlyDispatchStopsAtAWriteCall pins the prefix rule, which is load-bearing
// twice over: runTools relies on the dispatched set being a prefix of the round,
// and the pre-round gates rule on the complete round, so a write must never run
// before they have.
func TestEarlyDispatchStopsAtAWriteCall(t *testing.T) {
	read := newSignalTool("peek", tool.CapRead)
	write := newSignalTool("poke", tool.CapWrite)
	reg := tool.NewRegistry()
	for _, tl := range []*signalTool{read, write} {
		if err := reg.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	release := make(chan struct{})
	ad := &heldAdapter{
		release: release,
		pre: []provider.Event{
			toolUseEvent("a", "peek", `{"msg":"early"}`),
			toolUseEvent("b", "poke", `{"msg":"held"}`),
			toolUseEvent("c", "peek", `{"msg":"after-the-write"}`),
		},
		post: []provider.Event{{Type: provider.EventDone, Stop: provider.StopToolUse}},
	}
	eng, err := New(Options{Adapter: ad, Tools: reg, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, func(Event) {}) }()

	waitClosed(t, read.startedCh("early"), "the leading read to start")
	// The write, and the read *after* it, must both still be waiting.
	select {
	case <-write.startedCh("held"):
		t.Fatal("a write/execute call was dispatched before the turn completed")
	case <-read.startedCh("after-the-write"):
		t.Fatal("a read after a write was dispatched early, breaking the prefix rule")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	// All three still ran, and every slot is filled.
	if n := countToolResults(conv); n != 3 {
		t.Fatalf("got %d tool results, want 3 — every tool_use must be answered", n)
	}
}

// TestSpendBoundDisablesEarlyDispatch pins the budget carve-out. The
// pre-tool-round gate exists so a turn whose own usage crosses the cap stops
// before its tool calls run, and that usage is not known until the turn ends —
// so a run with a spend cap must keep the batch behaviour.
func TestSpendBoundDisablesEarlyDispatch(t *testing.T) {
	st := newSignalTool("peek", tool.CapRead)
	reg := tool.NewRegistry()
	if err := reg.Register(st); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	ad := &heldAdapter{
		release: release,
		pre:     []provider.Event{toolUseEvent("a", "peek", `{"msg":"gated"}`)},
		post:    []provider.Event{{Type: provider.EventDone, Stop: provider.StopToolUse}},
	}
	eng, err := New(Options{
		Adapter: ad, Tools: reg, Model: "m",
		Cost:      cost.NewTracker(),
		BudgetUSD: 100, // any spend bound at all
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	done := make(chan error, 1)
	go func() { done <- eng.Run(context.Background(), conv, func(Event) {}) }()

	select {
	case <-st.startedCh("gated"):
		t.Fatal("a tool ran before the turn ended on a run with a spend bound; the pre-tool-round budget gate was bypassed")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := countToolResults(conv); n != 1 {
		t.Fatalf("got %d tool results, want 1", n)
	}
}

// TestSuppressedToolsNeverDispatchEarly covers P2.6's half of the exclusion: a
// turn sent with no schemas may still hallucinate tool_use blocks, and those are
// discarded. Running one before deciding that would execute a call the engine is
// about to declare never happened.
func TestSuppressedToolsNeverDispatchEarly(t *testing.T) {
	// Run suppresses tools on the last permitted iteration of a run that has
	// already completed a tool round (the step limit), so the fixture needs a
	// real round first and the hallucinated call second.
	st := newSignalTool("peek", tool.CapRead)
	reg := tool.NewRegistry()
	if err := reg.Register(st); err != nil {
		t.Fatal(err)
	}
	ad := &scriptedAdapter{turns: [][]provider.Event{
		{
			toolUseEvent("a", "peek", `{"msg":"real"}`),
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "here is the summary"},
			toolUseEvent("b", "peek", `{"msg":"hallucinated"}`),
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}
	eng, err := New(Options{Adapter: ad, Tools: reg, Model: "m", MaxIterations: 2})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st.mu.Lock()
	order := append([]string(nil), st.order...)
	st.mu.Unlock()
	if fmt.Sprint(order) != fmt.Sprint([]string{"real"}) {
		t.Fatalf("executed %v; a suppressed-tools turn's calls must be discarded, not run", order)
	}
}

// countToolResults totals the tool_result blocks in a conversation.
func countToolResults(conv *Conversation) int {
	n := 0
	for _, m := range conv.Messages {
		for _, blk := range m.Content {
			if _, ok := blk.(provider.ToolResultBlock); ok {
				n++
			}
		}
	}
	return n
}
