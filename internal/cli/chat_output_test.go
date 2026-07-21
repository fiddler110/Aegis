package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
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
	emitStreamEvent(&buf, engine.Event{Kind: engine.KindNotice, Text: "model emitted a tool call as text"})
	emitStreamEvent(&buf, engine.Event{Kind: engine.KindTrace}) // must be dropped

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), buf.String())
	}
	var first streamEvent
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Type != "text" || first.Text != "hi" {
		t.Errorf("bad first event: %+v", first)
	}
	// A notice's entire payload is its text; emitting the kind alone tells a
	// stream-json consumer nothing.
	var notice streamEvent
	if err := json.Unmarshal([]byte(lines[2]), &notice); err != nil {
		t.Fatal(err)
	}
	if notice.Type != "notice" || notice.Text != "model emitted a tool call as text" {
		t.Errorf("notice lost its text: %+v", notice)
	}
}

// TestEmitStreamEventTurnDone is the P38.3 regression: turn_done previously
// fell through emitStreamEvent's switch with none of its Usage/CostUSD
// payload serialized, leaving a stream-json consumer with no per-turn token
// figures at all (only the final "result" trailer).
func TestEmitStreamEventTurnDone(t *testing.T) {
	var buf bytes.Buffer
	emitStreamEvent(&buf, engine.Event{
		Kind: engine.KindTurnDone,
		Usage: &provider.Usage{
			InputTokens:          123,
			OutputTokens:         45,
			CacheReadTokens:      6,
			CacheCreationTokens:  7,
			PromptEvalDurationMS: 89,
		},
		CostUSD: 0.02,
	})

	var got streamEvent
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "turn_done" {
		t.Fatalf("Type = %q, want turn_done", got.Type)
	}
	if got.InputTokens != 123 || got.OutputTokens != 45 || got.CacheReadTokens != 6 ||
		got.CacheCreationTokens != 7 || got.PromptEvalDurationMS != 89 || got.CostUSD != 0.02 {
		t.Errorf("usage not carried through: %+v", got)
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
