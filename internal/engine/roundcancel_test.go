package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// failingCapTool fails immediately, with the capability it is given. The
// capability is the parameter under test: P67.4's policy is that a failing
// write/execute call cancels its siblings and a failing read does not, so the
// same fixture has to be runnable both ways.
type failingCapTool struct {
	name string
	cap  tool.Capability
}

func (f *failingCapTool) Name() string                 { return f.name }
func (f *failingCapTool) Description() string          { return "fails on purpose" }
func (f *failingCapTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *failingCapTool) Capability() tool.Capability  { return f.cap }
func (f *failingCapTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, errors.New("the build failed")
}

// cancelWatchTool stands in for the sibling a doomed round is paying
// wall-clock for: a build, a test run, a container pull. It blocks until its
// context is done or its own generous deadline expires, and records which of
// the two happened — that distinction is the measurement, since a round that
// finished quickly for some unrelated reason would still pass a timing
// assertion.
type cancelWatchTool struct {
	name  string
	capab tool.Capability
	// cancelled and ranToEnd are the measurement: which of the two ways out
	// this call took. A test that asserted on elapsed time instead would pass
	// on a machine merely slow to start the call and fail on one slow to
	// cancel it.
	cancelled atomic.Bool
	ranToEnd  atomic.Bool
}

func newCancelWatchTool(name string, capab tool.Capability) *cancelWatchTool {
	return &cancelWatchTool{name: name, capab: capab}
}

func (b *cancelWatchTool) Name() string                 { return b.name }
func (b *cancelWatchTool) Description() string          { return "blocks until cancelled" }
func (b *cancelWatchTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (b *cancelWatchTool) Capability() tool.Capability  { return b.capab }
func (b *cancelWatchTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	select {
	case <-ctx.Done():
		b.cancelled.Store(true)
		return tool.Result{}, ctx.Err()
	case <-time.After(10 * time.Second):
		b.ranToEnd.Store(true)
		return tool.Result{Content: "finished the whole thing"}, nil
	}
}

// recordingWriteTool records only whether it executed. Deliberately not
// blocking: the question it answers is whether a cancelled round started it at
// all.
type recordingWriteTool struct {
	name string
	ran  atomic.Bool
}

func (r *recordingWriteTool) Name() string        { return r.name }
func (r *recordingWriteTool) Description() string { return "records that it ran" }
func (r *recordingWriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (r *recordingWriteTool) Capability() tool.Capability { return tool.CapWrite }
func (r *recordingWriteTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	r.ran.Store(true)
	return tool.Result{Content: "ran"}, nil
}

// call names one tool invocation in a scripted parallel round. input is raw so
// a test can give two calls the same "path" and lean on runTools' same-path
// ordering for a deterministic sequence.
type roundCall struct {
	id    string
	name  string
	input string
}

// cancelRoundAdapter scripts one turn issuing calls in parallel, then a final
// answer on the turn after — so a test can assert that the turn *continued*
// after its round was cancelled.
func cancelRoundAdapter(calls ...roundCall) *scriptedAdapter {
	first := make([]provider.Event, 0, len(calls)+1)
	for _, c := range calls {
		in := c.input
		if in == "" {
			in = `{}`
		}
		first = append(first, provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: c.id, Name: c.name, Input: json.RawMessage(in),
		}})
	}
	first = append(first, provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{IsEstimated: true}})
	return &scriptedAdapter{turns: [][]provider.Event{
		first,
		{
			{Type: provider.EventTextDelta, Text: "recovered"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}},
		},
	}}
}

func cancelRoundRegistry(t *testing.T, tools ...tool.Tool) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func runCancelRound(t *testing.T, adapter provider.Adapter, reg *tool.Registry) (*Conversation, []Event, error) {
	t.Helper()
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}},
	}}
	var mu sync.Mutex
	var evs []Event
	runErr := eng.Run(context.Background(), conv, func(ev Event) {
		mu.Lock()
		evs = append(evs, ev)
		mu.Unlock()
	})
	return conv, evs, runErr
}

// roundResults returns every tool_result in the conversation keyed by the
// tool_use it answers, failing if any call went unanswered. That check is not
// cosmetic: a cancelled sibling with no result would be sent back to the
// provider as a tool_use with no tool_result, which providers reject.
func roundResults(t *testing.T, conv *Conversation) map[string]provider.ToolResultBlock {
	t.Helper()
	uses := map[string]bool{}
	out := map[string]provider.ToolResultBlock{}
	for _, m := range conv.Messages {
		for _, b := range m.Content {
			switch v := b.(type) {
			case provider.ToolUseBlock:
				uses[v.ID] = true
			case provider.ToolResultBlock:
				if _, dup := out[v.ToolUseID]; dup {
					t.Errorf("duplicate tool_result for %s", v.ToolUseID)
				}
				out[v.ToolUseID] = v
			}
		}
	}
	for id := range uses {
		if _, ok := out[id]; !ok {
			t.Errorf("tool_use %s has no tool_result — a provider would reject this conversation", id)
		}
	}
	return out
}

// TestFailedWriteCancelsItsSiblings is P67.4's headline behavior: a round of
// four where one call fails must stop paying wall-clock for the other three.
//
// The assertion is on each sibling's own record of *why* it returned rather
// than on elapsed time — a timing assertion would pass on a machine merely slow
// to start them and fail on one slow to cancel them.
func TestFailedWriteCancelsItsSiblings(t *testing.T) {
	failing := &failingCapTool{name: "build", cap: tool.CapExecute}
	siblings := []*cancelWatchTool{
		newCancelWatchTool("s1", tool.CapRead),
		newCancelWatchTool("s2", tool.CapRead),
		newCancelWatchTool("s3", tool.CapRead),
	}
	reg := cancelRoundRegistry(t, failing, siblings[0], siblings[1], siblings[2])
	adapter := cancelRoundAdapter(
		roundCall{id: "a", name: "build"},
		roundCall{id: "b", name: "s1"},
		roundCall{id: "c", name: "s2"},
		roundCall{id: "d", name: "s3"},
	)

	conv, _, err := runCancelRound(t, adapter, reg)
	if err != nil {
		t.Fatalf("run: %v — cancelling siblings must not fail the turn", err)
	}
	for _, s := range siblings {
		if s.ranToEnd.Load() {
			t.Errorf("%s ran to completion after a sibling failed", s.name)
		}
	}

	results := roundResults(t, conv)
	if len(results) != 4 {
		t.Fatalf("expected 4 tool results, got %d", len(results))
	}
	for id, res := range results {
		if id == "a" {
			continue // the failure itself
		}
		if !strings.Contains(res.Content, "build") {
			t.Errorf("result %s does not name the call that cancelled the round: %q", id, res.Content)
		}
		// P65.1's rule, applied here: a call is never told both that it ran and
		// that it did not.
		if strings.Contains(res.Content, "Nothing was executed") &&
			strings.Contains(res.Content, "may have partially completed") {
			t.Errorf("result %s claims both that it ran and that it did not: %q", id, res.Content)
		}
	}
}

// TestFailedReadDoesNotCancelTheRound is the other half of the policy, and the
// half a naive cancel-on-any-error gets wrong. A read on a path the model
// guessed wrong is how it learns the path is wrong; if that killed the round,
// speculative parallel reads — most of what the parallel round is for — would
// stop being usable.
func TestFailedReadDoesNotCancelTheRound(t *testing.T) {
	failing := &failingCapTool{name: "peek", cap: tool.CapRead}
	echo := &echoTool{}
	other := &namedFakeTool{name: "other"}
	reg := cancelRoundRegistry(t, failing, echo, other)
	adapter := cancelRoundAdapter(
		roundCall{id: "a", name: "peek"},
		roundCall{id: "b", name: "echo", input: `{"msg":"hi"}`},
		roundCall{id: "c", name: "other"},
	)

	conv, _, err := runCancelRound(t, adapter, reg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if echo.called != 1 {
		t.Errorf("echo ran %d times; a failing read must not cancel its siblings", echo.called)
	}
	results := roundResults(t, conv)
	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(results))
	}
	for id, res := range results {
		if strings.Contains(res.Content, "did not run") || strings.Contains(res.Content, "round was cancelled") {
			t.Errorf("result %s was cancelled by a failing *read*: %q", id, res.Content)
		}
	}
}

// TestCancelledSiblingWaitingOnAnEarlierCallIsAbandoned covers the path a
// naive implementation leaves out: a call already spawned but still *waiting* —
// on the same-path ordering here, on execLock in the serialized case — must be
// abandoned where it waits rather than started once the wait clears. Without
// that, cancellation shortens only a round's concurrent tail and not the
// serialized spine, which is usually the expensive part.
//
// Giving both calls the same "path" is what makes this deterministic rather
// than usually-right: queued_write provably waits on the failing call, so the
// cancellation provably lands while it is waiting instead of racing it.
func TestCancelledSiblingWaitingOnAnEarlierCallIsAbandoned(t *testing.T) {
	failing := &failingCapTool{name: "build", cap: tool.CapExecute}
	queued := &recordingWriteTool{name: "queued_write"}
	reg := cancelRoundRegistry(t, failing, queued)
	adapter := cancelRoundAdapter(
		roundCall{id: "a", name: "build", input: `{"path":"shared.txt"}`},
		roundCall{id: "b", name: "queued_write", input: `{"path":"shared.txt"}`},
	)

	conv, _, err := runCancelRound(t, adapter, reg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if queued.ran.Load() {
		t.Error("a write waiting on an earlier same-path call ran after the round was cancelled")
	}
	res, ok := roundResults(t, conv)["b"]
	if !ok {
		t.Fatal("no result for the waiting call")
	}
	if !strings.Contains(res.Content, "Nothing was executed") {
		t.Errorf("a call that never started must say so plainly: %q", res.Content)
	}
	if strings.Contains(res.Content, "may have partially completed") {
		t.Errorf("a call that never started must not be described as maybe-run: %q", res.Content)
	}
	if !strings.Contains(res.Content, "build") {
		t.Errorf("the result must name the call that cancelled the round: %q", res.Content)
	}
}

// TestRoundCancellationLeavesTheTurnRunning pins the parent/child split, which
// is the invariant most at risk from a later refactor here: the round context
// is derived from the turn's, and cancelling the child must not end the turn.
// The model still has to receive its results and take the next turn — that is
// the difference between shortening a doomed round and aborting the run.
func TestRoundCancellationLeavesTheTurnRunning(t *testing.T) {
	failing := &failingCapTool{name: "build", cap: tool.CapExecute}
	sibling := newCancelWatchTool("s1", tool.CapRead)
	reg := cancelRoundRegistry(t, failing, sibling)
	adapter := cancelRoundAdapter(roundCall{id: "a", name: "build"}, roundCall{id: "b", name: "s1"})

	conv, evs, err := runCancelRound(t, adapter, reg)
	if err != nil {
		t.Fatalf("run returned %v; a cancelled round must not abort the turn", err)
	}
	if adapter.calls != 2 {
		t.Errorf("model was called %d times; the turn after the cancelled round must still happen", adapter.calls)
	}
	var final string
	for _, ev := range evs {
		if ev.Kind == KindText {
			final += ev.Text
		}
	}
	if !strings.Contains(final, "recovered") {
		t.Errorf("the run did not reach its final answer: %q", final)
	}
	roundResults(t, conv) // asserts every call was answered
}

// TestSiblingCancelledWordingNamesTheCause covers the two wordings directly,
// including the empty-cause fallback — the branch that fires if a future change
// ever cancels a round without recording which call did it, where a message
// reading "because  failed" would be the visible symptom.
func TestSiblingCancelledWordingNamesTheCause(t *testing.T) {
	if got := siblingCancelledText("write_file", "build"); !strings.Contains(got, "build") ||
		!strings.Contains(got, "write_file") || !strings.Contains(got, "Nothing was executed") {
		t.Errorf("named cause: %q", got)
	}
	if got := siblingCancelledText("write_file", ""); strings.Contains(got, "  ") {
		t.Errorf("unnamed cause left a gap: %q", got)
	}
	if got := siblingCancelledSuffix("build"); !strings.Contains(got, "build") {
		t.Errorf("suffix does not name the cause: %q", got)
	}
	if got := siblingCancelledSuffix(""); strings.Contains(got, "  ") {
		t.Errorf("unnamed suffix left a gap: %q", got)
	}
	// The maybe-ran wording must stay P65.1's, not a new one: a cancelled call
	// that had started is never told it did not run.
	full := interruptedMaybeRanText("shell") + " " + siblingCancelledSuffix("build")
	if strings.Contains(full, "did not run") {
		t.Errorf("a maybe-ran result must not claim the call did not run: %q", full)
	}

}

// TestCancelledSiblingsDoNotFeedTheFailureBreaker guards the interaction
// P67.4 creates with P52.3. Cancelled siblings must be marked as errors — the
// model has to know they produced nothing — but they are the consequence of the
// one real failure, not independent evidence of it, and the breaker keys its
// nudge and its abort on the *first* error in the round. A sibling cancelled at
// a lower index than the call that cancelled it would otherwise become the
// error the breaker reports, so a model repeatedly failing the same build would
// be nudged about "tool call skipped" instead of about the build.
func TestCancelledSiblingsDoNotFeedTheFailureBreaker(t *testing.T) {
	uses := []provider.ToolUseBlock{
		{ID: "a", Name: "s1"},
		{ID: "b", Name: "build"},
	}
	// The cancelled sibling sits *before* the failure in the round, which is
	// the ordering that exposes the bug: a round is a set of concurrent calls,
	// so nothing says the one that fails has the lowest index.
	results := []provider.Block{
		provider.ToolResultBlock{ToolUseID: "a", Content: siblingCancelledText("s1", "build"), IsError: true},
		provider.ToolResultBlock{ToolUseID: "b", Content: "the build failed", IsError: true},
	}

	var tracker toolFailureTracker
	tracker.record(uses, results)
	if !strings.Contains(tracker.lastErrorText, "the build failed") {
		t.Errorf("breaker latched onto the cancellation, not the failure: %q", tracker.lastErrorText)
	}
	if got := tracker.toolLabel(); !strings.Contains(got, "build") {
		t.Errorf("breaker reports the wrong tool: %q", got)
	}

	// And a round whose *only* errors are cancellations — possible if the call
	// that cancelled it is not itself in this result set — must not read as a
	// failing round at all.
	var cancelOnly toolFailureTracker
	for i := 0; i < 5; i++ {
		cancelOnly.record(uses[:1], results[:1])
	}
	if cancelOnly.shouldAbort() {
		t.Error("cancellation artifacts alone tripped the tool-failure breaker")
	}

	if !isRoundCancelledResult(siblingCancelledText("s1", "build")) {
		t.Error("a skipped sibling is not recognized as a cancellation artifact")
	}
	if !isRoundCancelledResult("partial output\n\n" + siblingCancelledSuffix("build")) {
		t.Error("a cut-short sibling is not recognized as a cancellation artifact")
	}
	if isRoundCancelledResult("the build failed") {
		t.Error("a genuine tool error was mistaken for a cancellation artifact")
	}
}
