package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
// or tag around it at all.
func TestProseToolCallSalvage_BareObject(t *testing.T) {
	base := scriptedAdapter{events: textOnly(
		`I'll do that now. {"name": "read_file", "arguments": {"path": "x.go"}} Done shortly.`,
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
