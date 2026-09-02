package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// bookkeepingTool is a tool.SignatureTransparent whose arguments differ on every
// call — the shape of a todo write, a memory note, or a task status update. It
// is the mechanism P64.2 exists for: nothing about its payload says whether the
// agent is making progress, but before P64.2 that payload was concatenated into
// the turn signature and made every turn look new.
type bookkeepingTool struct{ called int }

func (b *bookkeepingTool) Name() string                              { return "todo_write" }
func (b *bookkeepingTool) Description() string                       { return "records a plan" }
func (b *bookkeepingTool) InputSchema() json.RawMessage              { return json.RawMessage(`{"type":"object"}`) }
func (b *bookkeepingTool) Capability() tool.Capability               { return tool.CapWrite }
func (b *bookkeepingTool) SignatureTransparent(json.RawMessage) bool { return true }
func (b *bookkeepingTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	b.called++
	return tool.Result{Content: "noted"}, nil
}

var _ tool.SignatureTransparent = (*bookkeepingTool)(nil)

// TestTurnSignatureDropsTransparentArgumentsButKeepsTheName is the P64.2 unit,
// and it pins both halves of the asymmetry against PollExempt. Dropping the
// arguments is what closes the laundering gap; keeping the *name* is what stops
// the fix from silently becoming an exemption, which would disable detection for
// the tool wholesale — the blast radius PollExempter's own doc warns about.
func TestTurnSignatureDropsTransparentArgumentsButKeepsTheName(t *testing.T) {
	transparent := func(tu provider.ToolUseBlock) bool { return tu.Name == "todo_write" }

	turn := func(payload string) []provider.ToolUseBlock {
		return []provider.ToolUseBlock{
			{Name: "grep", Input: json.RawMessage(`{"pattern":"X"}`)},
			{Name: "todo_write", Input: json.RawMessage(payload)},
		}
	}

	a, recA := turnSignatureExcludingPolls(turn(`{"note":"step 1 of 9"}`), nil, transparent, nil)
	b, recB := turnSignatureExcludingPolls(turn(`{"note":"step 2 of 9"}`), nil, transparent, nil)
	if !recA || !recB {
		t.Fatal("a turn with a transparent call must still be recorded")
	}
	if a != b {
		t.Errorf("a varying transparent payload must not change the signature:\n %q\n %q", a, b)
	}
	if !strings.Contains(a, "todo_write") {
		t.Errorf("the transparent tool's name must survive: %q", a)
	}
	if strings.Contains(a, "step 1") {
		t.Errorf("the transparent tool's arguments must not survive: %q", a)
	}

	// The turn is still distinguished by the non-transparent call, or the fix
	// would have flattened real work into one signature.
	other := []provider.ToolUseBlock{
		{Name: "grep", Input: json.RawMessage(`{"pattern":"Y"}`)},
		{Name: "todo_write", Input: json.RawMessage(`{"note":"step 1 of 9"}`)},
	}
	if c, _ := turnSignatureExcludingPolls(other, nil, transparent, nil); c == a {
		t.Error("a genuinely different accompanying call must still change the signature")
	}

	// And a turn of nothing *but* transparent calls is still recorded, which is
	// the concrete difference from exemption: five turns of pure bookkeeping is
	// a stuck agent, and exemption would have let it run to the iteration cap.
	only := []provider.ToolUseBlock{{Name: "todo_write", Input: json.RawMessage(`{"note":"again"}`)}}
	if sig, rec := turnSignatureExcludingPolls(only, nil, transparent, nil); !rec || sig == "" {
		t.Errorf("an all-transparent turn must still be recorded, got rec=%v sig=%q", rec, sig)
	}
}

// TestEngineDetectsLoopLaunderedByVaryingBookkeepingCall is the P64.2
// regression, and it is the item's own stated first step: demonstrate the defeat
// by construction rather than from a reading of the code.
//
// The model repeats one failing `fail_edit` every turn — a plain error loop the
// detector has caught since P53.2 — while writing a todo whose payload differs
// each time. Before P64.2 the whole-turn signature was fresh every turn and the
// run rode to the iteration cap with the detector never firing.
func TestEngineDetectsLoopLaunderedByVaryingBookkeepingCall(t *testing.T) {
	turn := func(n int) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: fmt.Sprintf("f%d", n), Name: "fail_edit", Input: json.RawMessage(`{"old":"x"}`)}},
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID:    fmt.Sprintf("b%d", n),
				Name:  "todo_write",
				Input: json.RawMessage(fmt.Sprintf(`{"note":"attempt %d, still working on it"}`, n))}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		}
	}
	turns := make([][]provider.Event, 10)
	for i := range turns {
		turns[i] = turn(i)
	}

	reg := tool.NewRegistry()
	book := &bookkeepingTool{}
	if err := reg.Register(&failEditTool{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(book); err != nil {
		t.Fatal(err)
	}
	eng, _ := New(Options{
		Adapter: &scriptedAdapter{turns: turns}, Tools: reg, Model: "test", LoopThreshold: 3,
	})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	var gotErr error
	err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindError {
			gotErr = ev.Err
		}
	})
	if err == nil {
		t.Fatal("run should have aborted: a failing call repeated every turn is an error loop")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "loop") {
		t.Fatalf("expected a loop abort, got err=%v event=%v", err, gotErr)
	}
	// The abort must arrive on the detector's schedule, not at the iteration
	// cap — a count assertion alone cannot tell those apart (P63.9's finding),
	// and riding to MaxIterations is precisely the pre-P64.2 behaviour.
	if book.called > 5 {
		t.Errorf("detector fired late: %d bookkeeping calls ran, want it caught within a few turns of the threshold", book.called)
	}
}

// TestTransparencyDoesNotExemptAToolFromDetection is the counterpart guard, and
// it is the mutation the fix is most likely to acquire later: someone "simplifies"
// SignatureTransparent into another PollExempter. A model that does nothing but
// rewrite its bookkeeping every turn is stuck, and must still be caught.
func TestTransparencyDoesNotExemptAToolFromDetection(t *testing.T) {
	turn := func(n int) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID:    fmt.Sprintf("b%d", n),
				Name:  "todo_write",
				Input: json.RawMessage(fmt.Sprintf(`{"note":"revision %d"}`, n))}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		}
	}
	turns := make([][]provider.Event, 12)
	for i := range turns {
		turns[i] = turn(i)
	}

	reg := tool.NewRegistry()
	book := &bookkeepingTool{}
	if err := reg.Register(book); err != nil {
		t.Fatal(err)
	}
	eng, _ := New(Options{
		Adapter: &scriptedAdapter{turns: turns}, Tools: reg, Model: "test", LoopThreshold: 3,
	})

	conv := &Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "go"}}})
	nudged := false
	_ = eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "repeated the same succeeding tool calls") {
			nudged = true
		}
	})
	// These calls succeed, so the first trigger is the P53.2(b) nudge rather
	// than an abort — either way the detector must have seen it.
	if !nudged && countNudges(conv.Messages, loopNudgePrefix) == 0 {
		t.Error("a turn of nothing but transparent bookkeeping calls, repeated, must still reach the detector")
	}
}
