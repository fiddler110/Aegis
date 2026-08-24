package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/trace"
)

// maxTraceToolError bounds the tool-error body kept in a trace (P68.1). Before
// this the SSE stream logged only a character count for a failing call, so
// P62.9's edit_section failure — and anything like it — was unreadable once
// the run ended, even on a daemon whose data survived.
const maxTraceToolError = 2000

func boundToolError(s string) string {
	if len(s) <= maxTraceToolError {
		return s
	}
	return s[:maxTraceToolError] + "…"
}

// P67.7. This file is `runTools` restructured so the scheduler can be fed one
// call at a time, as that call's block completes in the stream, instead of being
// handed a finished slice after the whole model turn has drained.
//
// The scheduler itself is deliberately unchanged in substance: per-call
// tool.EffectiveCapability, the read/write gating, the waitFor dependency graph
// and the same-path ordering were all correct and are a better design than
// partitioning a round into contiguous runs. What changed is only *where its
// input comes from*.
//
// # Why a slot, and not the index arithmetic this replaced
//
// The old code sized results/traces/serialize/paths/done from len(toolUses) up
// front and had each goroutine write results[i]. That cannot survive incremental
// arrival: appending to those slices reallocates their backing arrays while
// goroutines already spawned are holding indices into them, which is a data race
// with a completely silent failure mode. Each call therefore owns a *toolSlot
// whose address never moves, and writes only into that. The round's slice of
// slot pointers may reallocate as freely as it likes.
//
// # What still must hold
//
//   - **Result order is receipt order.** Slots are appended in the order the
//     provider emitted the tool_use blocks, and the result list is read back in
//     slot order, so execution order — which was never deterministic — stays
//     invisible to the transcript, every replay and every eval fixture.
//   - **Every slot is filled.** An unanswered tool_use is a protocol error, so
//     abandonment writes a result rather than leaving a hole (P67.4).
//   - **A call that started is never told it did not run** (P65.1).
//     markToolStarted is recorded inside executeTool, at dispatch, which is
//     already where P67.7 requires it and is why that constraint needed no code
//     change here.
//   - **Cancelling siblings never cancels the turn** (P67.4). roundCtx derives
//     from the turn's ctx; nothing here ever cancels ctx.

// toolSlot is one call's private state. Only that call's goroutine writes
// result/trace, and only after wg.Wait() does anyone else read them — which is
// what makes them safe without a lock of their own.
type toolSlot struct {
	tu        provider.ToolUseBlock
	serialize bool          // write/execute: takes execLock, and its failure cancels the round
	path      string        // filesystem target, for same-path ordering; "" if none
	done      chan struct{} // closed when this call is finished, however it finished
	waitFor   []*toolSlot   // earlier same-path writes this call must follow

	result provider.Block
	trace  trace.ToolCall
}

// toolRound schedules one parallel tool round. Calls may be added while the
// model turn that produced them is still streaming; wait() closes the round.
type toolRound struct {
	e    *Engine
	emit EmitFunc

	// ctx is the turn's context and roundCtx is derived from it (P67.4). The
	// split is the whole point and the thing to preserve through any later
	// change here: cancelling siblings must never cancel the turn. ctx stays the
	// only source of "the run is over" — wait() checks ctx, not roundCtx.
	ctx         context.Context
	roundCtx    context.Context
	cancelRound context.CancelFunc

	mu    sync.Mutex // guards slots
	slots []*toolSlot

	execLock sync.Mutex // exclusive among write/execute calls only
	wg       sync.WaitGroup
	sem      chan struct{}

	failMu     sync.Mutex
	failedTool string // the call that cancelled the round, "" if none did

	// aborted marks a round whose turn came apart — a failed or cancelled
	// stream. Its results must never reach the conversation: they would be
	// tool_result blocks answering an assistant message that never completed.
	aborted atomic.Bool
}

// newToolRound opens a round against the turn's context.
func (e *Engine) newToolRound(ctx context.Context, emit EmitFunc) *toolRound {
	roundCtx, cancel := context.WithCancel(ctx)
	return &toolRound{
		e:           e,
		emit:        emit,
		ctx:         ctx,
		roundCtx:    roundCtx,
		cancelRound: cancel,
		sem:         make(chan struct{}, maxParallelTools),
	}
}

// add appends a call and dispatches it. Safe to call from the stream-reading
// goroutine: it never blocks on the concurrency semaphore, which is acquired
// inside the spawned goroutine instead.
//
// That placement is a change from the pre-P67.7 code, which acquired the
// semaphore in the dispatch loop, and it is forced rather than incidental. The
// old caller had nothing better to do than block; the new one is draining a
// provider stream, and blocking it would stop the turn's text deltas, stop the
// heartbeat that rides every stream event (P39.17), and make a saturated round
// look exactly like a stalled model.
func (r *toolRound) add(tu provider.ToolUseBlock) {
	r.mu.Lock()
	s := &toolSlot{
		tu:        tu,
		serialize: r.e.serializeTool(tu.Name, tu.Input),
		path:      toolTargetPath(tu.Input),
		done:      make(chan struct{}),
	}
	// waitFor lists earlier calls whose completion this one must await: any
	// prior write/execute call targeting the same non-empty path. It applies
	// whether this call reads or writes that path, so both write→read and
	// write→write pairs preserve the model-emitted order for a shared path;
	// calls on distinct paths (or with no path) never gate one another and stay
	// fully concurrent.
	//
	// Only earlier calls are consulted, which is exactly why the graph survives
	// incremental arrival: a dependency can only point backwards, and everything
	// it could point at has already been added.
	if s.path != "" {
		for _, prev := range r.slots {
			if prev.serialize && prev.path == s.path {
				s.waitFor = append(s.waitFor, prev)
			}
		}
	}
	r.slots = append(r.slots, s)
	r.mu.Unlock()

	r.wg.Add(1)
	go r.run(s)
}

// pending reports how many calls have been added. Used by the caller to tell a
// round that was fed from the stream from one that was not.
func (r *toolRound) pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.slots)
}

// safeEmit emits one event. The serialization it used to provide now lives one
// level up, in Run's serializedEmit, because the round is no longer the only
// concurrent producer — the turn that is still streaming is another. The method
// stays as the round's single emission point so the reasoning has somewhere to
// live and so a future producer inside the round has one thing to call.
func (r *toolRound) safeEmit(ev Event) { r.emit(ev) }

// failRound cancels the round on behalf of the first qualifying failure. Later
// failures are recorded by nobody: the first one is the one the siblings'
// results should name, and re-cancelling an already-cancelled context would only
// race over the reason.
func (r *toolRound) failRound(name string) {
	r.failMu.Lock()
	defer r.failMu.Unlock()
	if r.failedTool != "" {
		return
	}
	r.failedTool = name
	r.e.logger.Debug("cancelling parallel tool round after a failure", "tool", name, "round_size", r.pending())
	r.cancelRound()
}

func (r *toolRound) roundFailure() string {
	r.failMu.Lock()
	defer r.failMu.Unlock()
	return r.failedTool
}

// abandon fills the result slot for a call the round cancelled. Every slot must
// be filled or the conversation goes back to the provider with a tool_use that
// has no tool_result — so this is not optional bookkeeping, it is what makes
// cancelling safe at all.
//
// It is only ever reached *before* a call has entered Execute — every path that
// has already executed reports the tool's own result instead, with
// siblingCancelledSuffix appended. That is what makes the plain "nothing was
// executed" wording honest here, and it is the P65.1 rule applied rather than
// relaxed: a call that may have landed effects is never told it did not run,
// because a model told that re-runs it.
func (r *toolRound) abandon(s *toolSlot) {
	if r.ctx.Err() != nil || r.aborted.Load() {
		// The turn itself is over: results are discarded wholesale by wait()
		// and repairOrphanedToolUses writes the synthetic ones instead.
		return
	}
	content := siblingCancelledText(s.tu.Name, r.roundFailure())
	s.trace = trace.ToolCall{Name: s.tu.Name, IsError: true, ErrorText: boundToolError(content)}
	r.safeEmit(Event{Kind: KindToolCall, ToolName: s.tu.Name, ToolID: s.tu.ID, ToolInput: s.tu.Input})
	r.safeEmit(Event{Kind: KindToolResult, ToolName: s.tu.Name, ToolID: s.tu.ID, ToolResult: content, ToolIsError: true})
	s.result = provider.ToolResultBlock{ToolUseID: s.tu.ID, Content: content, IsError: true}
}

// run executes one call. It is the body of the goroutine the old dispatch loop
// spawned, moved verbatim in substance onto the slot.
func (r *toolRound) run(s *toolSlot) {
	defer r.wg.Done()
	// Signal dependents that this call is complete — even on panic or interrupt
	// below — so a waiter on s.done can never hang.
	defer close(s.done)
	// A panic in one tool call (a buggy MCP tool, malformed builtin input) must
	// not cross the goroutine boundary: unrecovered, it takes down the whole
	// daemon process — every concurrent session, not just the one that triggered
	// it. Recover here and report it back as an ordinary tool error instead.
	defer func() {
		if rec := recover(); rec != nil {
			r.e.logger.Error("recovered panic in tool call", "tool", s.tu.Name, "panic", rec, "stack", string(debug.Stack()))
			content := fmt.Sprintf("tool %q panicked: %v", s.tu.Name, rec)
			s.trace = trace.ToolCall{Name: s.tu.Name, IsError: true, ErrorText: boundToolError(content)}
			r.safeEmit(Event{Kind: KindToolResult, ToolName: s.tu.Name, ToolID: s.tu.ID, ToolResult: content, ToolIsError: true})
			s.result = provider.ToolResultBlock{ToolUseID: s.tu.ID, Content: content, IsError: true}
		}
	}()

	if r.ctx.Err() != nil {
		r.abandon(s)
		return
	}
	// The concurrency bound, held for the whole of this call. Acquired here
	// rather than at add() so a saturated round never blocks the stream that is
	// still feeding it.
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-r.roundCtx.Done():
		r.abandon(s)
		return
	}

	// Wait for earlier same-path writes in this round to finish before touching
	// the path. Deadlock-free: writes never wait on reads, and every awaited call
	// was added earlier — so its goroutine is already running and will close its
	// done channel on every exit path, including abandonment. Give up if the run
	// is interrupted or the round was cancelled by a sibling's failure (P67.4).
	//
	// Under incremental dispatch the awaited write may not even have been added
	// yet when this read starts — it has, since waitFor only points backwards,
	// but it may not have *run* yet, and it may be queued behind the semaphore.
	// That is the correct behaviour and the reason the semaphore is acquired
	// above rather than below: a read holding a slot while waiting on a write
	// that cannot get one would deadlock the round.
	for _, prev := range s.waitFor {
		select {
		case <-prev.done:
		case <-r.roundCtx.Done():
			r.abandon(s)
			return
		}
	}
	if r.roundCtx.Err() != nil {
		r.abandon(s)
		return
	}

	if s.serialize {
		r.execLock.Lock()
		defer r.execLock.Unlock()
		// The wait for execLock is unbounded — it can sit behind a long-running
		// write or shell — so a round cancelled while this call queued must not
		// start it now (P67.4). This is the check that makes cancellation
		// actually shorten a doomed round of serialized calls rather than only
		// its concurrent tail.
		if r.roundCtx.Err() != nil {
			r.abandon(s)
			return
		}
	}

	r.safeEmit(Event{Kind: KindToolCall, ToolName: s.tu.Name, ToolID: s.tu.ID, ToolInput: s.tu.Input})
	// P39.17: same both-edges beat as the sequential path. The detector's state
	// is mutex-guarded precisely so it can be beaten from these goroutines.
	//
	// P67.7 makes this beat matter more, not less. A tool now runs concurrently
	// with generation, so the stall window covers both — but both beat the same
	// watch, and they beat it from two independent sources instead of one. The
	// overlap therefore cannot make a live turn look stalled; it can only make a
	// stalled one look live for as long as a tool is still working, which is the
	// direction the stall bound is supposed to err in.
	beat(r.ctx)
	start := time.Now()
	// P67.4: roundCtx, so a sibling's failure reaches this call's subprocess.
	// Everything that ends the *turn* still reaches it too, since roundCtx is
	// derived from ctx.
	content, isErr := r.e.executeTool(r.roundCtx, s.tu)
	// Read the round's state before failRound below, so a call cannot annotate
	// its own result with a cancellation it is about to cause. A call that was
	// genuinely running while a *sibling* failed does get the annotation, which
	// is the honest reading: its error may be the cancellation rather than the
	// tool's own verdict.
	cutShort := r.roundCtx.Err() != nil && r.ctx.Err() == nil
	beat(r.ctx)
	if isErr && cutShort {
		content = content + "\n\n" + siblingCancelledSuffix(r.roundFailure())
	}
	s.trace = trace.ToolCall{Name: s.tu.Name, DurationMS: time.Since(start).Milliseconds(), IsError: isErr}
	if isErr {
		s.trace.ErrorText = boundToolError(content)
	}
	r.safeEmit(Event{Kind: KindToolResult, ToolName: s.tu.Name, ToolID: s.tu.ID, ToolResult: content, ToolIsError: isErr})
	s.result = provider.ToolResultBlock{ToolUseID: s.tu.ID, Content: content, IsError: isErr}
	// Which failures qualify (P67.4). A read that fails is a normal negative
	// result — `read_file` on a path the model guessed wrong is how it learns the
	// path is wrong, and killing its siblings for that would make speculative
	// parallel reads useless. A failing write or execute is different: the round
	// was a plan, a step of it did not happen, and the steps after it are being
	// carried out against a state that no longer matches.
	//
	// s.serialize is exactly that classification, already computed for
	// scheduling (serializeTool → tool.EffectiveCapability), so the policy hangs
	// off it rather than off a second, drifting copy — including its per-call
	// refinement, which is what keeps a shell call the tool reclassified as
	// read-only from cancelling a round.
	if isErr && s.serialize && !cutShort {
		r.failRound(s.tu.Name)
	}
}

// abort tears the round down for a turn that came apart — a failed or cancelled
// stream — and discards whatever the in-flight calls produce.
//
// P67.7's third constraint. Once tool calls can start before the assistant
// message is complete, a stream that then errors leaves work running whose
// results would answer a message the conversation never received. Cancelling is
// the easy half; the load-bearing half is that abort blocks until the goroutines
// are gone, so a call cannot still be writing to a slot the next turn is about
// to reuse — and that nothing here touches the conversation.
func (r *toolRound) abort() {
	if r == nil {
		return
	}
	r.aborted.Store(true)
	r.cancelRound()
	r.wg.Wait()
}

// wait closes the round: it waits for every dispatched call, applies the P67.1
// aggregate bound, and returns results in receipt order.
func (r *toolRound) wait() ([]provider.Block, []trace.ToolCall, error) {
	defer r.cancelRound()
	r.wg.Wait()

	if r.ctx.Err() != nil {
		return nil, nil, ErrInterrupted
	}
	if r.aborted.Load() {
		return nil, nil, ErrInterrupted
	}

	r.mu.Lock()
	slots := r.slots
	r.mu.Unlock()

	results := make([]provider.Block, len(slots))
	traces := make([]trace.ToolCall, len(slots))
	for i, s := range slots {
		results[i], traces[i] = s.result, s.trace
	}
	r.e.capRound(r.ctx, results)
	return results, traces, nil
}

// serializedEmit wraps an EmitFunc so it is safe for concurrent use (P67.7).
//
// The lock is the whole implementation, and the reason it is worth a named
// function is the invariant it carries: a run's emit is wrapped exactly once, at
// the top of Run, and everything downstream uses that value. Wrapping twice
// would be harmless; wrapping zero times is a data race in whatever consumer the
// caller happened to pass, which is code this package does not own and cannot
// test.
func serializedEmit(emit EmitFunc) EmitFunc {
	var mu sync.Mutex
	return func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		emit(ev)
	}
}
