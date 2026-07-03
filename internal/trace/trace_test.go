package trace

import (
	"encoding/json"
	"testing"
	"time"
)

// TestTurnTraceRoundTrip verifies a fully populated TurnTrace survives a JSON
// round trip unchanged. TurnTrace is persisted as a BLOB per row in
// internal/session's session_traces table and read back by `aegis sessions
// trace`, so a broken field would silently corrupt stored observability data.
func TestTurnTraceRoundTrip(t *testing.T) {
	started := time.Now().UTC().Truncate(time.Millisecond)
	in := TurnTrace{
		Index:               2,
		Model:               "claude-sonnet",
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
		CostUSD:             0.0123,
		Estimated:           true,
		ToolCalls:           []ToolCall{{Name: "shell", DurationMS: 120, IsError: false}, {Name: "read_file", DurationMS: 5, IsError: true}},
		WallMS:              1500,
		StartedAt:           started,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out TurnTrace
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Index != in.Index || out.Model != in.Model || out.InputTokens != in.InputTokens ||
		out.OutputTokens != in.OutputTokens || out.CacheReadTokens != in.CacheReadTokens ||
		out.CacheCreationTokens != in.CacheCreationTokens || out.CostUSD != in.CostUSD ||
		out.Estimated != in.Estimated || out.WallMS != in.WallMS || !out.StartedAt.Equal(in.StartedAt) {
		t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
	}
	if len(out.ToolCalls) != len(in.ToolCalls) {
		t.Fatalf("ToolCalls len = %d, want %d", len(out.ToolCalls), len(in.ToolCalls))
	}
	for i := range in.ToolCalls {
		if out.ToolCalls[i] != in.ToolCalls[i] {
			t.Errorf("ToolCalls[%d] = %+v, want %+v", i, out.ToolCalls[i], in.ToolCalls[i])
		}
	}
}

// TestTurnTraceOmitsOptionalFields checks that a minimal, single-model-call
// turn (no cache tokens, no tool calls, not estimated) serializes without
// those zero-value fields, keeping stored traces compact.
func TestTurnTraceOmitsOptionalFields(t *testing.T) {
	data, err := json.Marshal(TurnTrace{Index: 0, Model: "test", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, omitted := range []string{"cache_read_tokens", "cache_creation_tokens", "estimated", "tool_calls"} {
		if _, ok := raw[omitted]; ok {
			t.Errorf("expected %q to be omitted for a minimal trace, got %v", omitted, raw[omitted])
		}
	}
}

// TestToolCallOmitsIsErrorWhenFalse verifies a successful tool call doesn't
// serialize is_error:false, since ToolCall.IsError uses omitempty and callers
// (e.g. `aegis sessions trace`) treat its absence as success.
func TestToolCallOmitsIsErrorWhenFalse(t *testing.T) {
	data, err := json.Marshal(ToolCall{Name: "shell", DurationMS: 10})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := raw["is_error"]; ok {
		t.Error("is_error should be omitted when false")
	}
}
