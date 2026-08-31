package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// scriptedAdapter replays a fixed event sequence regardless of the request,
// letting a test stand in for a recorded local-model response.
type scriptedAdapter struct {
	events []Event
}

func (scriptedAdapter) Name() string { return "scripted" }

func (a scriptedAdapter) Stream(_ context.Context, _ Request) (<-chan Event, error) {
	ch := make(chan Event, len(a.events))
	for _, ev := range a.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func textOnly(chunks ...string) []Event {
	var evs []Event
	for _, c := range chunks {
		evs = append(evs, Event{Type: EventTextDelta, Text: c})
	}
	evs = append(evs, Event{Type: EventDone, Stop: StopEndTurn})
	return evs
}

var readFileTool = []ToolSchema{{Name: "read_file", Description: "read a file"}}

func drainStream(t *testing.T, a Adapter, req Request) []Event {
	t.Helper()
	ch, err := a.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got []Event
	for ev := range ch {
		got = append(got, ev)
	}
	return got
}

func onlyToolUse(t *testing.T, events []Event) *ToolUseBlock {
	t.Helper()
	var call *ToolUseBlock
	for _, ev := range events {
		if ev.Type == EventToolUse {
			if call != nil {
				t.Fatalf("more than one EventToolUse in %+v", events)
			}
			call = ev.ToolUse
		}
	}
	if call == nil {
		t.Fatalf("no EventToolUse in %+v", events)
	}
	return call
}

func joinedText(events []Event) string {
	s := ""
	for _, ev := range events {
		if ev.Type == EventTextDelta {
			s += ev.Text
		}
	}
	return s
}

// TestProseToolCallSalvage_FencedJSON: a fenced JSON object is parsed into a
// call, validated against the tools actually sent, and the narration around
// it survives as text.
func TestProseToolCallSalvage_FencedJSON(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		"Sure, let me check that file.\n\n```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"go.mod\"}}\n```\n",
	)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{Tools: readFileTool})

	call := onlyToolUse(t, events)
	if call.Name != "read_file" {
		t.Errorf("Name = %q, want read_file", call.Name)
	}
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil {
		t.Fatalf("Input: %v", err)
	}
	if args.Path != "go.mod" {
		t.Errorf("path = %q, want go.mod", args.Path)
	}

	text := joinedText(events)
	if text == "" {
		t.Errorf("surviving prose was discarded")
	}
	if want := "Sure, let me check that file."; !strings.Contains(text, want) {
		t.Errorf("surviving text %q missing narration %q", text, want)
	}

	last := events[len(events)-1]
	if last.Type != EventDone || last.Stop != StopToolUse {
		t.Errorf("final event = %+v, want EventDone/StopToolUse", last)
	}
}

// TestProseToolCallSalvage_TaggedBlock covers the <tool_call> shape
// Hermes/Qwen-trained local models emit.
func TestProseToolCallSalvage_TaggedBlock(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		"<tool_call>{\"name\": \"read_file\", \"arguments\": {\"path\": \"README.md\"}}</tool_call>",
	)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{Tools: readFileTool})

	call := onlyToolUse(t, events)
	if call.Name != "read_file" {
		t.Errorf("Name = %q, want read_file", call.Name)
	}
}

// TestProseToolCallSalvage_BareObject covers a bare JSON object with no fence
// or tag around it at all — the whole reply being the object, which is a model
// that meant to call a tool and could not emit a structured call.
func TestProseToolCallSalvage_BareObject(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		`  {"name": "read_file", "arguments": {"path": "x.go"}}` + "\n",
	)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{Tools: readFileTool})

	call := onlyToolUse(t, events)
	if call.Name != "read_file" {
		t.Errorf("Name = %q, want read_file", call.Name)
	}
	text := joinedText(events)
	if strings.Contains(text, `"name"`) {
		t.Errorf("surviving text still contains the call JSON: %q", text)
	}
}

// TestProseToolCallSalvage_ObjectNarratedInProseIsNotACall is M2. The bare
// branch used to promote a JSON object found *anywhere* in the reply, which
// made salvage an injection amplifier: a model that read a poisoned file and
// echoed its contents back — quoting them, describing them, refusing them — had
// that text turned into a tool call it never chose, and CapWrite/CapNetwork are
// allowed silently in build mode, so the gate is not what stops it. A model
// that genuinely means to call a tool still has the tagged and fenced
// spellings, both of which are explicit acts.
func TestProseToolCallSalvage_ObjectNarratedInProseIsNotACall(t *testing.T) {
	for _, reply := range []string{
		`I'll do that now. {"name": "read_file", "arguments": {"path": "x.go"}} Done shortly.`,
		`The file asked me to run {"name": "read_file", "arguments": {"path": "/etc/passwd"}} — I won't.`,
		`Here is what that config means: {"name": "read_file", "arguments": {"path": "x.go"}}. Note the path.`,
	} {
		t.Run(reply[:20], func(t *testing.T) {
			base := scriptedAdapter{events: textOnly(reply)}
			events := drainStream(t, WithProseToolCallSalvage(base), Request{Tools: readFileTool})
			for _, ev := range events {
				if ev.Type == EventToolUse || ev.Type == EventToolUseStart {
					t.Fatalf("prose narrating an object became a call: %+v", ev.ToolUse)
				}
			}
			if got := joinedText(events); got != reply {
				t.Errorf("text = %q, want it forwarded unchanged", got)
			}
		})
	}
}

// TestProseToolCallSalvage_StringEncodedArguments covers a model that encodes
// arguments as a JSON string rather than a nested object.
func TestProseToolCallSalvage_StringEncodedArguments(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		`{"name": "read_file", "arguments": "{\"path\": \"x.go\"}"}`,
	)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{Tools: readFileTool})

	call := onlyToolUse(t, events)
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil {
		t.Fatalf("Input: %v", err)
	}
	if args.Path != "x.go" {
		t.Errorf("path = %q, want x.go", args.Path)
	}
}

// TestProseToolCallSalvage_MentionOnlyIsNotACall is the important negative:
// a reply that merely names a tool in a sentence, with no call-shaped JSON at
// all, must never be turned into a call.
func TestProseToolCallSalvage_MentionOnlyIsNotACall(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		"I could use read_file to check that, but I don't think it's necessary here.",
	)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{Tools: readFileTool})

	for _, ev := range events {
		if ev.Type == EventToolUse || ev.Type == EventToolUseStart {
			t.Fatalf("mention-only text produced a tool call: %+v", events)
		}
	}
	last := events[len(events)-1]
	if last.Stop != StopEndTurn {
		t.Errorf("Stop = %v, want the original StopEndTurn", last.Stop)
	}
}

// TestProseToolCallSalvage_UnknownNameIsNotACall: a call-shaped object naming
// a tool that wasn't offered on this request must not be salvaged either —
// only tools actually sent count.
func TestProseToolCallSalvage_UnknownNameIsNotACall(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		`{"name": "delete_everything", "arguments": {}}`,
	)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{Tools: readFileTool})

	for _, ev := range events {
		if ev.Type == EventToolUse {
			t.Fatalf("unoffered tool name produced a call: %+v", events)
		}
	}
}

// TestProseToolCallSalvage_RealToolUseIsLeftAlone: a turn that already made a
// structured call is replayed exactly as received, even if its text also
// happens to contain something call-shaped.
func TestProseToolCallSalvage_RealToolUseIsLeftAlone(t *testing.T) {
	want := []Event{
		{Type: EventTextDelta, Text: "checking now"},
		{Type: EventToolUseStart, ToolUse: &ToolUseBlock{ID: "tu_0", Name: "read_file"}},
		{Type: EventToolUse, ToolUse: &ToolUseBlock{ID: "tu_0", Name: "read_file", Input: json.RawMessage(`{"path":"a"}`)}},
		{Type: EventDone, Stop: StopToolUse},
	}
	base := scriptedAdapter{events: want}
	a := WithProseToolCallSalvage(base)
	got := drainStream(t, a, Request{Tools: readFileTool})

	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Type != want[i].Type {
			t.Errorf("event %d: type = %v, want %v", i, got[i].Type, want[i].Type)
		}
	}
}

// TestProseToolCallSalvage_NoToolsOfferedIsNeverScanned: a plain text-only
// request (no Tools on the request) is never a salvage candidate, whatever
// its content looks like.
func TestProseToolCallSalvage_NoToolsOfferedIsNeverScanned(t *testing.T) {
	base := scriptedAdapter{events: textOnly(`{"name": "read_file", "arguments": {}}`)}
	a := WithProseToolCallSalvage(base)
	events := drainStream(t, a, Request{})

	for _, ev := range events {
		if ev.Type == EventToolUse {
			t.Fatalf("a request with no tools was scanned for a call: %+v", events)
		}
	}
	text := joinedText(events)
	if text != `{"name": "read_file", "arguments": {}}` {
		t.Errorf("text was altered: %q", text)
	}
}

// blockingAdapter emits an opening event, then waits for release before
// emitting the rest, so a test can observe what the decorator has forwarded
// while the upstream turn is still generating.
type blockingAdapter struct {
	first   Event
	release <-chan struct{}
	rest    []Event
}

func (blockingAdapter) Name() string { return "blocking" }

func (a blockingAdapter) Stream(_ context.Context, _ Request) (<-chan Event, error) {
	ch := make(chan Event)
	go func() {
		defer close(ch)
		ch <- a.first
		<-a.release
		for _, ev := range a.rest {
			ch <- ev
		}
	}()
	return ch, nil
}

// TestProseToolCallSalvage_ForwardsLivenessBeforeTheTurnEnds is CRIT-4. This
// decorator used to run its loop to channel close before forwarding a single
// event, and two engine invariants were watching this channel: the stall
// heartbeat beats on each event *received*, so a turn under salvage looked idle
// for its whole model phase and a legitimate long local generation was killed
// at MaxTurnStall as a run-fatal ErrTurnStalled; and P67.7 dispatches a tool
// call the instant it is announced, which cannot happen if the announcement
// arrives after generation is over. Salvage never needed to withhold events —
// only text — so everything else must reach the consumer as it arrives.
func TestProseToolCallSalvage_ForwardsLivenessBeforeTheTurnEnds(t *testing.T) {
	release := make(chan struct{})
	base := blockingAdapter{
		first:   Event{Type: EventThinkingDelta, Text: "considering…"},
		release: release,
		rest:    []Event{{Type: EventTextDelta, Text: "done"}, {Type: EventDone, Stop: StopEndTurn}},
	}
	ch, err := WithProseToolCallSalvage(base).Stream(context.Background(), Request{Tools: readFileTool})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("stream closed before the turn produced anything")
		}
		if ev.Type != EventThinkingDelta {
			t.Fatalf("first forwarded event = %v, want a thinking delta while generation is still in flight", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event reached the consumer while the upstream turn was still generating")
	}
	close(release)
	for range ch {
	}
}

// TestProseToolCallSalvage_FlushesOnAStructuredCall is the other half of
// CRIT-4: a turn that emits a real structured call has nothing left to salvage,
// so the decorator must stop holding text at that instant and become a
// passthrough — that is what restores P67.7's early dispatch for every model
// good enough to emit structured calls.
func TestProseToolCallSalvage_FlushesOnAStructuredCall(t *testing.T) {
	release := make(chan struct{})
	call := &ToolUseBlock{ID: "tu_0", Name: "read_file", Input: json.RawMessage(`{"path":"x.go"}`)}
	base := blockingAdapter{
		first:   Event{Type: EventTextDelta, Text: "let me look"},
		release: release,
		rest: []Event{
			{Type: EventToolUseStart, ToolUse: call},
			{Type: EventToolUse, ToolUse: call},
			{Type: EventDone, Stop: StopToolUse},
		},
	}
	ch, err := WithProseToolCallSalvage(base).Stream(context.Background(), Request{Tools: readFileTool})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	close(release)

	var got []Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if len(got) != 4 {
					t.Fatalf("events = %d, want the 4 upstream events replayed unchanged: %+v", len(got), got)
				}
				if got[0].Type != EventTextDelta || got[1].Type != EventToolUseStart {
					t.Errorf("order changed: %+v", got)
				}
				if got[1].ToolUse.ID != "tu_0" {
					t.Errorf("a structured call was rewritten: %+v", got[1].ToolUse)
				}
				return
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("stream did not complete; got %+v", got)
		}
	}
}

// TestProseToolCallSalvage_CapsBufferedText pins the CRIT-4d bound: a runaway
// generation must not grow the hold buffer without limit. Past the cap the
// decorator gives up on salvaging and forwards, which is the right direction —
// a reply that large is a long answer, not a mis-emitted call.
func TestProseToolCallSalvage_CapsBufferedText(t *testing.T) {
	big := strings.Repeat("x", maxSalvageTextBytes+1)
	base := scriptedAdapter{events: textOnly(big, `{"name": "read_file", "arguments": {"path": "x.go"}}`)}
	events := drainStream(t, WithProseToolCallSalvage(base), Request{Tools: readFileTool})
	for _, ev := range events {
		if ev.Type == EventToolUse {
			t.Fatal("a reply past the salvage cap must be forwarded, not rewritten")
		}
	}
	if !strings.Contains(joinedText(events), `"name"`) {
		t.Error("text past the cap was dropped rather than forwarded")
	}
}

// TestProseToolCallSalvage_QwenFunctionXML is EXEC-3: Qwen3's own chat template
// instructs it to emit a non-JSON XML tool call, and salvage handed that body
// to json.Unmarshal, failed, and fell through to branches that found nothing.
// The most common local tool-call syntax outside JSON was the one shape the
// safety net could not catch.
func TestProseToolCallSalvage_QwenFunctionXML(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
	}{
		{"wrapped in tool_call", "<tool_call>\n<function=read_file>\n<parameter=path>\nx.go\n</parameter>\n</function>\n</tool_call>"},
		{"bare, no wrapper", "<function=read_file>\n<parameter=path>\nx.go\n</parameter>\n</function>"},
		{"narrated", "Let me read that.\n<function=read_file>\n<parameter=path>\nx.go\n</parameter>\n</function>\n"},
		{"single line", `<function=read_file><parameter=path>x.go</parameter></function>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := scriptedAdapter{events: textOnly(tc.reply)}
			events := drainStream(t, WithProseToolCallSalvage(base), Request{Tools: readFileTool})
			call := onlyToolUse(t, events)
			if call.Name != "read_file" {
				t.Fatalf("Name = %q, want read_file", call.Name)
			}
			var args struct{ Path string }
			if err := json.Unmarshal(call.Input, &args); err != nil {
				t.Fatalf("Input %s: %v", call.Input, err)
			}
			if args.Path != "x.go" {
				t.Errorf("path = %q, want x.go", args.Path)
			}
		})
	}
}

// TestProseToolCallSalvage_FunctionXMLRequiresAKnownName keeps the XML branch
// as narrow as every other one: the name must be a tool the request actually
// offered, so a reply that merely mentions a function never becomes a call.
func TestProseToolCallSalvage_FunctionXMLRequiresAKnownName(t *testing.T) {
	base := scriptedAdapter{events: textOnly(`<function=rm_rf><parameter=path>/</parameter></function>`)}
	events := drainStream(t, WithProseToolCallSalvage(base), Request{Tools: readFileTool})
	for _, ev := range events {
		if ev.Type == EventToolUse {
			t.Fatalf("an unoffered tool name became a call: %+v", ev.ToolUse)
		}
	}
}
