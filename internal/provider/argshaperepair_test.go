package provider

import (
	"encoding/json"
	"testing"
)

var readFileSchemaTool = []ToolSchema{{
	Name:        "read_file",
	Description: "read a file",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
}}

func toolUseOnly(id, name string, input json.RawMessage) []Event {
	return []Event{
		{Type: EventToolUseStart, ToolUse: &ToolUseBlock{ID: id, Name: name}},
		{Type: EventToolUse, ToolUse: &ToolUseBlock{ID: id, Name: name, Input: input}},
		{Type: EventDone, Stop: StopToolUse},
	}
}

func TestArgumentShapeRepair_WellFormedInputUnchanged(t *testing.T) {
	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`{"path":"go.mod"}`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	if string(call.Input) != `{"path":"go.mod"}` {
		t.Errorf("Input = %s, want unchanged", call.Input)
	}
}

func TestArgumentShapeRepair_DoubleEncodedString(t *testing.T) {
	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`"{\"path\":\"go.mod\"}"`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil {
		t.Fatalf("Input not valid JSON object: %v (%s)", err, call.Input)
	}
	if args.Path != "go.mod" {
		t.Errorf("path = %q, want go.mod", args.Path)
	}
}

func TestArgumentShapeRepair_UnwrapsRedundantWrapperKey(t *testing.T) {
	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`{"arguments":{"path":"go.mod"}}`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil {
		t.Fatalf("Input not valid JSON object: %v (%s)", err, call.Input)
	}
	if args.Path != "go.mod" {
		t.Errorf("path = %q, want go.mod", args.Path)
	}
}

func TestArgumentShapeRepair_WrapsBareScalarForSingleProperty(t *testing.T) {
	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`"go.mod"`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil {
		t.Fatalf("Input not valid JSON object: %v (%s)", err, call.Input)
	}
	if args.Path != "go.mod" {
		t.Errorf("path = %q, want go.mod", args.Path)
	}
}

func TestArgumentShapeRepair_MultiPropertyBareScalarLeftAlone(t *testing.T) {
	multi := []ToolSchema{{
		Name:        "write_file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`),
	}}
	base := scriptedAdapter{events: toolUseOnly("tu_1", "write_file", json.RawMessage(`"go.mod"`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: multi})
	call := onlyToolUse(t, events)
	if string(call.Input) != `"go.mod"` {
		t.Errorf("Input = %s, want unchanged (ambiguous which field a bare scalar belongs to)", call.Input)
	}
}

func TestArgumentShapeRepair_UnmatchedObjectLeftAlone(t *testing.T) {
	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`{"filename":"go.mod"}`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	if string(call.Input) != `{"filename":"go.mod"}` {
		t.Errorf("Input = %s, want unchanged — wrong key name is not this decorator's problem", call.Input)
	}
}

func TestArgumentShapeRepair_NoToolsBypassesEntirely(t *testing.T) {
	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`"go.mod"`))}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{})
	call := onlyToolUse(t, events)
	if string(call.Input) != `"go.mod"` {
		t.Errorf("Input = %s, want unchanged when the request carried no tool schemas", call.Input)
	}
}

func TestArgumentShapeRepair_EmptyInputBecomesEmptyObject(t *testing.T) {
	noArgTool := []ToolSchema{{Name: "list_files"}}
	base := scriptedAdapter{events: toolUseOnly("tu_1", "list_files", nil)}
	a := WithArgumentShapeRepair(base)
	events := drainStream(t, a, Request{Tools: noArgTool})
	call := onlyToolUse(t, events)
	if string(call.Input) != `{}` {
		t.Errorf("Input = %s, want {}", call.Input)
	}
}
