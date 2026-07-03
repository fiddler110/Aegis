package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
)

func TestParseOutputFormat(t *testing.T) {
	cases := map[string]outputFormatKind{
		"":            outputText,
		"text":        outputText,
		"json":        outputJSON,
		"stream-json": outputStreamJSON,
		"STREAM-JSON": outputStreamJSON,
	}
	for in, want := range cases {
		got, err := parseOutputFormat(in)
		if err != nil || got != want {
			t.Errorf("parseOutputFormat(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := parseOutputFormat("yaml"); err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestEmitStreamEvent(t *testing.T) {
	var buf bytes.Buffer
	emitStreamEvent(&buf, engine.Event{Kind: engine.KindText, Text: "hi"})
	emitStreamEvent(&buf, engine.Event{Kind: engine.KindToolCall, ToolName: "read", ToolInput: json.RawMessage(`{"path":"x"}`)})
	emitStreamEvent(&buf, engine.Event{Kind: engine.KindTrace}) // must be dropped

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	var first streamEvent
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Type != "text" || first.Text != "hi" {
		t.Errorf("bad first event: %+v", first)
	}
}

func TestEmitFinalJSON(t *testing.T) {
	var buf bytes.Buffer
	emitFinalJSON(&buf, chatResult{Answer: "done", CostUSD: 0.01, ToolCalls: 2})
	var res chatResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Answer != "done" || res.ToolCalls != 2 {
		t.Errorf("roundtrip mismatch: %+v", res)
	}
}
