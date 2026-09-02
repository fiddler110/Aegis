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
	got := repairOrphanedToolUses(msgs, nil, nil)
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
	got := repairOrphanedToolUses(msgs, nil, nil)
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
	if _, isErr, _ := eng.executeTool(context.Background(), provider.ToolUseBlock{
		ID: "tu_denied", Name: "ok", Input: json.RawMessage(`{}`),
	}); !isErr {
		t.Fatal("expected the gate to refuse the call")
	}
	if _, ok := eng.startedToolSet()["tu_denied"]; ok {
		t.Error("a gate-refused call was recorded as started")
	}

	// An unknown tool never reaches a gate at all — same reasoning, other branch.
	if _, isErr, _ := eng.executeTool(context.Background(), provider.ToolUseBlock{
		ID: "tu_unknown", Name: "nope", Input: json.RawMessage(`{}`),
	}); !isErr {
		t.Fatal("expected an unknown tool to error")
	}
	if _, ok := eng.startedToolSet()["tu_unknown"]; ok {
		t.Error("an unknown-tool call was recorded as started")
	}
}

// idempotentReadTool implements tool.ReplayClassifier and reports
// tool.ReplaySafe unconditionally — a stand-in for the pure-read tools P65.4
// classifies explicitly (read_file, grep, glob, ...).
type idempotentReadTool struct{}

func (idempotentReadTool) Name() string                 { return "idempotent_read" }
func (idempotentReadTool) Description() string          { return "a tool safe to reissue" }
func (idempotentReadTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (idempotentReadTool) Capability() tool.Capability  { return tool.CapRead }
func (idempotentReadTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "read"}, nil
}
func (idempotentReadTool) Replay(json.RawMessage) tool.ReplayClass { return tool.ReplaySafe }

// TestRepairOrphanUsesSafeWordingForReplaySafeTool covers the second half of
// P65.4: a "may have run" orphan whose tool declares tool.ReplaySafe gets
// told it's fine to just reissue the call, instead of the universally
// cautious "verify before assuming" wording every tool got before replay
// classification existed.
func TestRepairOrphanUsesSafeWordingForReplaySafeTool(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(idempotentReadTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&neverReachedTool{}); err != nil {
		t.Fatal(err)
	}

	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_safe", Name: "idempotent_read", Input: json.RawMessage(`{}`)},
			provider.ToolUseBlock{ID: "tu_unsafe", Name: "second_write", Input: json.RawMessage(`{}`)},
		}},
	}
	started := map[string]struct{}{"tu_safe": {}, "tu_unsafe": {}}
	got := repairOrphanedToolUses(msgs, started, reg)

	results := map[string]string{}
	for _, b := range got[2].Content {
		tr, ok := b.(provider.ToolResultBlock)
		if !ok {
			t.Fatalf("block is not a ToolResultBlock: %#v", b)
		}
		results[tr.ToolUseID] = tr.Content
	}

	if safe := results["tu_safe"]; !strings.Contains(safe, "safe to simply retry") {
		t.Errorf("ReplaySafe orphan result = %q, want the safe-to-retry wording", safe)
	}
	if unsafe := results["tu_unsafe"]; !strings.Contains(unsafe, "Verify before assuming") {
		t.Errorf("default-classified orphan result = %q, want the conservative wording unchanged", unsafe)
	}
	if strings.Contains(results["tu_unsafe"], "safe to simply retry") {
		t.Errorf("a tool with no ReplayClassifier was treated as replay-safe: %q", results["tu_unsafe"])
	}
}

// TestInitialStartedToolsSurvivesAcrossEngineInstances covers P65.4's actual
// point: a brand-new Engine (no in-process history at all — the shape a fresh
// daemon process resuming a session from disk has) still classifies an orphan
// as "may have run" when Options.InitialStartedTools seeds it, rather than
// falling back to "did not run" the way a truly historyless Engine must.
// InitialStartedTools stands in for what a caller would read from
// internal/opregister's durable Pending() before constructing the engine.
func TestInitialStartedToolsSurvivesAcrossEngineInstances(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.ToolUseBlock{ID: "tu_from_dead_process", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		}},
	}

	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{{Type: provider.EventTextDelta, Text: "recovered"}, {Type: provider.EventDone, Stop: provider.StopEndTurn}},
	}}
	eng, err := New(Options{
		Adapter:             adapter,
		Model:               "test",
		InitialStartedTools: []string{"tu_from_dead_process"},
	})
	if err != nil {
		t.Fatal(err)
	}

	conv := &Conversation{Messages: msgs}
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("Run err = %v", err)
	}

	var found bool
	for _, msg := range conv.Messages {
		for _, b := range msg.Content {
			tr, ok := b.(provider.ToolResultBlock)
			if !ok || tr.ToolUseID != "tu_from_dead_process" {
				continue
			}
			found = true
			if !strings.Contains(tr.Content, "may have partially completed") {
				t.Errorf("seeded orphan result = %q, want the uncertain wording", tr.Content)
			}
			if strings.Contains(tr.Content, "did not run") {
				t.Errorf("seeded orphan wrongly reported as never started: %q", tr.Content)
			}
		}
	}
	if !found {
		t.Fatal("no synthetic result injected for the seeded orphan")
	}
}
