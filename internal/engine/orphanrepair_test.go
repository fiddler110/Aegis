package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// cancelOnStartTool declares CapWrite (so the same-path ordering in runTools
// gates a later call on it) and cancels the run the moment it is entered, then
// blocks until that cancel lands. It stands in for the class P65.1 is about: a
// long shell, a scanner, a container build — the tool most likely to be running
// when a stall or wall-clock bound fires, and therefore the one most likely to
// have had an effect the transcript is about to deny.
type cancelOnStartTool struct {
	cancel  context.CancelFunc
	entered chan struct{}
}

func (c *cancelOnStartTool) Name() string        { return "slow_write" }
func (c *cancelOnStartTool) Description() string { return "cancels the run once entered" }
func (c *cancelOnStartTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (c *cancelOnStartTool) Capability() tool.Capability { return tool.CapWrite }
func (c *cancelOnStartTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	close(c.entered)
	c.cancel()
	<-ctx.Done()
	return tool.Result{Content: "partially done"}, nil
}

// neverReachedTool records whether it ever ran. It targets the same path as
// cancelOnStartTool, so runTools' same-path ordering makes it wait on that
// call — and the wait is abandoned on ctx.Done, which is what keeps it
// deterministically unstarted rather than merely usually unstarted.
type neverReachedTool struct{ executed bool }

func (n *neverReachedTool) Name() string                 { return "second_write" }
func (n *neverReachedTool) Description() string          { return "must not be reached" }
func (n *neverReachedTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (n *neverReachedTool) Capability() tool.Capability  { return tool.CapWrite }
func (n *neverReachedTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	n.executed = true
	return tool.Result{Content: "ran"}, nil
}

// TestInterruptedToolRepairDistinguishesStartedFromUnreached covers P65.1.
//
// A run is cancelled mid-tool-round with two calls outstanding: one that had
// entered Execute and one that had not. The synthetic results the next run
// injects must say different things about them — and *both* halves are
// asserted, because a mutation that marks every orphan as started is as wrong
// as the pre-P65.1 code that marked none, in the opposite direction. Only a
// two-call fixture can tell those apart (P63.9's finding: a short fixture
// cannot distinguish adjacent behaviours).
func TestInterruptedToolRepairDistinguishesStartedFromUnreached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slow := &cancelOnStartTool{cancel: cancel, entered: make(chan struct{})}
	never := &neverReachedTool{}

	reg := tool.NewRegistry()
	for _, tl := range []tool.Tool{slow, never} {
		if err := reg.Register(tl); err != nil {
			t.Fatal(err)
		}
	}

	// Both calls name the same path, so waitFor gates the second on the first.
	toolRound := []provider.Event{
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: "tu_started", Name: "slow_write", Input: json.RawMessage(`{"path":"shared.txt"}`)}},
		{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
			ID: "tu_unreached", Name: "second_write", Input: json.RawMessage(`{"path":"shared.txt"}`)}},
		{Type: provider.EventDone, Stop: provider.StopToolUse},
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		toolRound,
		{{Type: provider.EventTextDelta, Text: "recovered"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})

	if err := eng.Run(ctx, conv, nil); err != ErrInterrupted {
		t.Fatalf("first Run err = %v, want ErrInterrupted", err)
	}
	select {
	case <-slow.entered:
	default:
		t.Fatal("fixture is vacuous: the cancelling tool never entered Execute")
	}
	if never.executed {
		t.Fatal("fixture is vacuous: the gated second call ran despite the cancel")
	}

	// The interrupted round left both tool_use blocks unresolved; the next Run
	// is where repairOrphanedToolUses fills them in.
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("second Run err = %v", err)
	}

	results := map[string]string{}
	for _, msg := range conv.Messages {
		for _, b := range msg.Content {
			if tr, ok := b.(provider.ToolResultBlock); ok {
				results[tr.ToolUseID] = tr.Content
			}
		}
	}

	started, ok := results["tu_started"]
	if !ok {
		t.Fatal("no synthetic result for the started call")
	}
	if !strings.Contains(started, "may have partially completed") {
		t.Errorf("started call result = %q, want it to say the effect is uncertain", started)
	}
	if strings.Contains(started, "did not run") {
		t.Errorf("started call result asserts it did not run: %q", started)
	}

	unreached, ok := results["tu_unreached"]
	if !ok {
		t.Fatal("no synthetic result for the unreached call")
	}
	if !strings.Contains(unreached, "did not run") {
		t.Errorf("unreached call result = %q, want the definite wording", unreached)
	}
	if strings.Contains(unreached, "may have partially completed") {
		t.Errorf("unreached call was wrongly reported as possibly-run: %q", unreached)
	}
}

// TestRepairOrphanUsesNotStartedWordingWithoutARecord pins the fallback: with
// no started-set (a session restored into a fresh process, where the in-process
// map is gone), an orphan with well-formed arguments keeps the pre-P65.1
// wording. That is not a claim the runtime can back — it is the only answer
// available without the durable record P65.4 would add — so it is asserted
// here rather than left to drift into the confident half by accident. The
// arguments are deliberately valid JSON so this exercises the "no record"
// branch rather than P74.14's malformed-arguments branch.
func TestRepairOrphanUsesNotStartedWordingWithoutARecord(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_1", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		}},
	}
	got := repairOrphanedToolUses(msgs, nil)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	tr, ok := got[2].Content[0].(provider.ToolResultBlock)
	if !ok {
		t.Fatal("injected block is not a ToolResultBlock")
	}
	if !strings.Contains(tr.Content, "did not run") {
		t.Errorf("content = %q, want the not-started wording", tr.Content)
	}
}

// TestRepairOrphanDistinguishesMalformedFromInterrupted covers P74.14: an
// orphaned call whose arguments never parsed as JSON at all must be reported
// differently from one that was simply cut off mid-flight, because the first
// was never going to dispatch regardless of the interruption and "did not
// run" invites the model to retry the identical, still-malformed call.
func TestRepairOrphanDistinguishesMalformedFromInterrupted(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_malformed", Name: "shell", Input: json.RawMessage(`{"command":`)},
			provider.ToolUseBlock{ID: "tu_clean", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		}},
	}
	got := repairOrphanedToolUses(msgs, nil)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	results := map[string]provider.ToolResultBlock{}
	for _, b := range got[2].Content {
		tr, ok := b.(provider.ToolResultBlock)
		if !ok {
			t.Fatalf("block is not a ToolResultBlock: %#v", b)
		}
		results[tr.ToolUseID] = tr
	}

	malformed, ok := results["tu_malformed"]
	if !ok {
		t.Fatal("no synthetic result for the malformed call")
	}
	if !strings.Contains(malformed.Content, "malformed") {
		t.Errorf("malformed call result = %q, want it to name malformed arguments", malformed.Content)
	}
	if strings.Contains(malformed.Content, "interrupted") {
		t.Errorf("malformed call result wrongly claims interruption: %q", malformed.Content)
	}
	if !malformed.IsError {
		t.Error("malformed call result should be an error")
	}

	clean, ok := results["tu_clean"]
	if !ok {
		t.Fatal("no synthetic result for the clean call")
	}
	if !strings.Contains(clean.Content, "did not run") {
		t.Errorf("clean call result = %q, want the not-started wording", clean.Content)
	}
	if strings.Contains(clean.Content, "malformed") {
		t.Errorf("clean call result wrongly reported as malformed: %q", clean.Content)
	}
}

// TestMarkToolStartedSkipsGateRefusals covers the placement decision in
// executeTool: the mark sits immediately before Execute, not at the function's
// top, so a call the gate or a hook refused is never described to the model as
// possibly-having-run. Those refusals are the one case where "did not run" is
// provably true, and over-warning there would cost the wording its meaning.
func TestMarkToolStartedSkipsGateRefusals(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(okTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{
		Adapter: &scriptedAdapter{},
		Tools:   reg,
		Model:   "test",
		Gate:    &denyGate{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, isErr := eng.executeTool(context.Background(), provider.ToolUseBlock{
		ID: "tu_denied", Name: "ok", Input: json.RawMessage(`{}`),
	}); !isErr {
		t.Fatal("expected the gate to refuse the call")
	}
	if _, ok := eng.startedToolSet()["tu_denied"]; ok {
		t.Error("a gate-refused call was recorded as started")
	}

	// An unknown tool never reaches a gate at all — same reasoning, other branch.
	if _, isErr := eng.executeTool(context.Background(), provider.ToolUseBlock{
		ID: "tu_unknown", Name: "nope", Input: json.RawMessage(`{}`),
	}); !isErr {
		t.Fatal("expected an unknown tool to error")
	}
	if _, ok := eng.startedToolSet()["tu_unknown"]; ok {
		t.Error("an unknown-tool call was recorded as started")
	}
}
