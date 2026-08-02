package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// recordingAdapter is scriptedAdapter plus the requests it was handed, so the
// shim tests can assert on what actually went over the wire — which is where
// the whole P53.6 behavior lives (schemas in the prompt, tools field empty).
type recordingAdapter struct {
	turns [][]provider.Event
	calls int
	reqs  []provider.Request
}

func (r *recordingAdapter) Name() string { return "recording" }

func (r *recordingAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	r.reqs = append(r.reqs, req)
	events := r.turns[r.calls]
	r.calls++
	ch := make(chan provider.Event, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func shimTurn(text string, stop provider.StopReason) []provider.Event {
	return []provider.Event{
		{Type: provider.EventTextDelta, Text: text},
		{Type: provider.EventDone, Stop: stop, Usage: &provider.Usage{IsEstimated: true}},
	}
}

func shimCall(name, args string) string {
	return "<tool_call>\n{\"name\": \"" + name + "\", \"arguments\": " + args + "}\n</tool_call>"
}

func newShimRegistry(t *testing.T) (*tool.Registry, *echoTool) {
	t.Helper()
	reg := tool.NewRegistry()
	echo := &echoTool{}
	if err := reg.Register(echo); err != nil {
		t.Fatal(err)
	}
	return reg, echo
}

func userConv(text string) *Conversation {
	conv := &Conversation{System: "You are a test agent."}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: text},
	}})
	return conv
}

// TestToolShimOffByDefault is the guard that matters most: nothing about the
// request or the transcript may change for a run that didn't ask for the shim.
func TestToolShimOffByDefault(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		shimTurn(shimCall("echo", `{"msg":"hi"}`), provider.StopEndTurn),
	}}
	reg, echo := newShimRegistry(t)

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	conv := userConv("say hi")
	if err := eng.Run(context.Background(), conv, nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	if echo.called != 0 {
		t.Errorf("tool ran %d times with the shim off — tagged JSON in prose must stay prose", echo.called)
	}
	req := adapter.reqs[0]
	if len(req.Tools) == 0 {
		t.Error("native tool schemas were not sent with the shim off")
	}
	if strings.Contains(req.System, "<tool_call>") {
		t.Error("shim prompt leaked into a non-shim run's system prompt")
	}
}

// TestToolShimExecutesParsedCall is the end-to-end path: schemas move into the
// prompt, the model's tagged JSON becomes a real tool call, the result comes
// back as text (there is no tool_use block to correlate a tool_result against),
// and the run finishes on the model's answer.
func TestToolShimExecutesParsedCall(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		shimTurn("I'll echo.\n"+shimCall("echo", `{"msg":"hi"}`), provider.StopEndTurn),
		shimTurn("Done: it echoed.", provider.StopEndTurn),
	}}
	reg, echo := newShimRegistry(t)

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ToolCallShim: true})
	if err != nil {
		t.Fatal(err)
	}
	conv := userConv("echo hi")

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if echo.called != 1 {
		t.Fatalf("echo tool called %d times, want 1", echo.called)
	}
	// First request: no native schemas, catalog in the system prompt.
	first := adapter.reqs[0]
	if len(first.Tools) != 0 {
		t.Errorf("shim run still sent %d native tool schemas", len(first.Tools))
	}
	if !strings.Contains(first.System, "<tool_call>") || !strings.Contains(first.System, "echo") {
		t.Errorf("shim prompt missing from system prompt:\n%s", first.System)
	}
	if !strings.Contains(first.System, "You are a test agent.") {
		t.Error("shim prompt replaced the caller's system prompt instead of appending to it")
	}
	// The conversation itself must stay free of the scaffolding — only the
	// request carries it, so compaction and session persistence see a normal
	// transcript.
	if strings.Contains(conv.System, "<tool_call>") {
		t.Error("shim prompt was written into the durable conversation")
	}
	// The tool result came back as text, not as an orphaned tool_result block.
	var sawResultText bool
	for _, m := range conv.Messages {
		for _, b := range m.Content {
			switch blk := b.(type) {
			case provider.ToolResultBlock:
				t.Errorf("shim run appended an orphaned tool_result block: %+v", blk)
			case provider.ToolUseBlock:
				t.Errorf("shim run appended a tool_use block the provider never emitted: %+v", blk)
			case provider.TextBlock:
				if strings.Contains(blk.Text, "echo:hi") {
					sawResultText = true
				}
			}
		}
	}
	if !sawResultText {
		t.Error("the tool result never reached the conversation as text")
	}
	// And the run announced that it was shimming, once.
	var announced int
	for _, n := range notices {
		if strings.Contains(n, "tool-call shim active") {
			announced++
		}
	}
	if announced != 1 {
		t.Errorf("expected exactly one shim-active notice, got %d of %v", announced, notices)
	}
}

// TestToolShimCallsPassThroughGate pins the security property: a call parsed
// out of prose is dispatched through the same permission gate a native call
// is, with no shortcut path. A denying gate must stop it.
func TestToolShimCallsPassThroughGate(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		shimTurn(shimCall("echo", `{"msg":"hi"}`), provider.StopEndTurn),
		shimTurn("I was not allowed to run that.", provider.StopEndTurn),
	}}
	reg, echo := newShimRegistry(t)

	gate := &denyGate{}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ToolCallShim: true, Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), userConv("echo hi"), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gate.checked != 1 {
		t.Errorf("gate consulted %d times for a shimmed call, want 1", gate.checked)
	}
	if echo.called != 0 {
		t.Errorf("a gate-denied shim call still executed %d times", echo.called)
	}
}

type denyGate struct{ checked int }

func (g *denyGate) Check(context.Context, tool.Tool, json.RawMessage) (bool, string) {
	g.checked++
	return false, "denied by test gate"
}

// TestToolShimMalformedCallIsCorrectedNotExecuted covers the decline-never-guess
// rule at the engine level: an attempt that doesn't parse runs nothing, earns a
// corrective naming the reason, and the corrective is retracted from the durable
// transcript once the run settles.
func TestToolShimMalformedCallIsCorrectedNotExecuted(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		// Names a tool that was never offered — a lenient parser might map it
		// onto the one tool that exists.
		shimTurn("<tool_call>\n{\"name\": \"exec\", \"arguments\": {}}\n</tool_call>", provider.StopEndTurn),
		shimTurn(shimCall("echo", `{"msg":"hi"}`), provider.StopEndTurn),
		shimTurn("Done.", provider.StopEndTurn),
	}}
	reg, echo := newShimRegistry(t)

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ToolCallShim: true})
	if err != nil {
		t.Fatal(err)
	}
	conv := userConv("run exec")

	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if echo.called != 1 {
		t.Errorf("echo called %d times, want 1 (the corrected second attempt only)", echo.called)
	}
	var corrected bool
	for _, n := range notices {
		if strings.Contains(n, "could not parse") && strings.Contains(n, "not one of the available tools") {
			corrected = true
		}
	}
	if !corrected {
		t.Errorf("no corrective notice naming the reason: %v", notices)
	}
	// The corrective is protocol scaffolding — the durable transcript must not
	// teach a later turn that malformed tool calls belong in the conversation.
	for _, m := range conv.Messages {
		for _, b := range m.Content {
			if tb, ok := b.(provider.TextBlock); ok && strings.Contains(tb.Text, shimFormatNudgePrefix) {
				t.Errorf("shim corrective survived into the transcript: %q", tb.Text)
			}
		}
	}
}

// TestToolShimGivesUpAfterBoundedCorrections: a model that never learns the
// format must not burn the run on format instruction — after the bound its
// prose answer is surfaced instead.
func TestToolShimGivesUpAfterBoundedCorrections(t *testing.T) {
	bad := shimTurn("<tool_call>\n{not json\n</tool_call>", provider.StopEndTurn)
	turns := make([][]provider.Event, 0, shimFormatNudgeMax+1)
	for i := 0; i < shimFormatNudgeMax+1; i++ {
		turns = append(turns, bad)
	}
	adapter := &recordingAdapter{turns: turns}
	reg, echo := newShimRegistry(t)

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ToolCallShim: true, ZeroToolNudgeMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}

	var gaveUp bool
	if err := eng.Run(context.Background(), userConv("do something"), func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "never produced a parsable tool call") {
			gaveUp = true
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != shimFormatNudgeMax+1 {
		t.Errorf("model was called %d times, want %d (one initial + %d corrections)",
			adapter.calls, shimFormatNudgeMax+1, shimFormatNudgeMax)
	}
	if !gaveUp {
		t.Error("run never announced that it stopped trying to correct the format")
	}
	if echo.called != 0 {
		t.Errorf("nothing may execute from an unparsable attempt; echo ran %d times", echo.called)
	}
}

// TestToolShimPlainAnswerIsNotACorrection: an ordinary answer under the shim
// must finish the run, not trip the corrective. A false positive here would
// inject protocol scaffolding into every answering turn.
func TestToolShimPlainAnswerIsNotACorrection(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		shimTurn("The answer is 4.", provider.StopEndTurn),
	}}
	reg, _ := newShimRegistry(t)

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ToolCallShim: true, ZeroToolNudgeMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), userConv("what is 2+2"), func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "could not parse") {
			t.Errorf("plain answer treated as a failed tool call: %q", ev.Text)
		}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if adapter.calls != 1 {
		t.Errorf("model called %d times for a one-turn answer, want 1", adapter.calls)
	}
}

// TestToolShimSuppressedOnStepLimitTurn: the final-iteration summary turn asks
// for prose and no tools (P2.6), so the shim must not hand the model a tool
// catalog and then parse a call out of the summary it asked for.
func TestToolShimSuppressedOnStepLimitTurn(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		shimTurn(shimCall("echo", `{"msg":"a"}`), provider.StopEndTurn),
		shimTurn("Summary: I echoed once. "+shimCall("echo", `{"msg":"b"}`), provider.StopEndTurn),
	}}
	reg, echo := newShimRegistry(t)

	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", ToolCallShim: true, MaxIterations: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), userConv("echo a"), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if echo.called != 1 {
		t.Errorf("echo called %d times, want 1 — the summary turn's tagged JSON must not execute", echo.called)
	}
	last := adapter.reqs[len(adapter.reqs)-1]
	if strings.Contains(last.System, "<tool_call>") {
		t.Error("shim catalog was sent on the tools-suppressed summary turn")
	}
}
