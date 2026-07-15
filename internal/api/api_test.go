package api

import (
	"encoding/json"
	"testing"
	"time"
)

// TestEventKindWireValues locks the on-the-wire string for each EventKind.
// These strings are the SSE "event:" name and the JSON "kind" field the TUI
// and CLI clients switch on (see internal/tui/tui.go); an accidental rename
// here would silently break every existing client without a compile error.
func TestEventKindWireValues(t *testing.T) {
	cases := map[EventKind]string{
		KindText:            "text",
		KindThinking:        "thinking",
		KindToolCallStart:   "tool_call_start",
		KindToolCall:        "tool_call",
		KindToolResult:      "tool_result",
		KindTurnDone:        "turn_done",
		KindDone:            "done",
		KindError:           "error",
		KindApprovalRequest: "approval_request",
		KindSteer:           "steer",
		KindSteerUnconsumed: "steer_unconsumed",
		KindGuard:           "guard",
		KindCostAlert:       "cost_alert",
		KindNotice:          "notice",
	}
	for kind, want := range cases {
		if string(kind) != want {
			t.Errorf("EventKind %v = %q, want %q", kind, string(kind), want)
		}
	}
}

// TestEventRoundTrip verifies every field of the SSE Event wire type survives
// a JSON marshal/unmarshal cycle unchanged, since this is the exact
// serialization the daemon streams and the TUI/CLI clients decode.
func TestEventRoundTrip(t *testing.T) {
	in := Event{
		Kind:                KindToolResult,
		Text:                "hello",
		Tool:                "shell",
		ToolInput:           json.RawMessage(`{"cmd":"ls"}`),
		ToolResult:          "output",
		ToolIsError:         true,
		InputTokens:         10,
		OutputTokens:        20,
		CostUSD:             0.0042,
		Error:               "boom",
		CacheReadTokens:     5,
		CacheCreationTokens: 6,
		TokensEstimated:     true,
		ApprovalReason:      "writes a file",
		ApprovalID:          "run-1",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out Event
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// json.RawMessage is a []byte, so Event isn't comparable with ==;
	// compare field by field instead.
	if out.Kind != in.Kind || out.Text != in.Text || out.Tool != in.Tool ||
		string(out.ToolInput) != string(in.ToolInput) || out.ToolResult != in.ToolResult ||
		out.ToolIsError != in.ToolIsError || out.InputTokens != in.InputTokens ||
		out.OutputTokens != in.OutputTokens || out.CostUSD != in.CostUSD ||
		out.Error != in.Error || out.CacheReadTokens != in.CacheReadTokens ||
		out.CacheCreationTokens != in.CacheCreationTokens || out.TokensEstimated != in.TokensEstimated ||
		out.ApprovalReason != in.ApprovalReason || out.ApprovalID != in.ApprovalID {
		t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
	}
}

// TestEventOmitsEmptyFields checks the zero-value Event serializes to just
// {"kind":"..."} — every other field must be omitempty so idle heartbeats and
// simple text deltas don't bloat every SSE frame.
func TestEventOmitsEmptyFields(t *testing.T) {
	data, err := json.Marshal(Event{Kind: KindDone})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(raw) != 1 {
		t.Errorf("expected only \"kind\" to serialize for a zero-value Event, got %v", raw)
	}
	if raw["kind"] != "done" {
		t.Errorf("kind = %v, want \"done\"", raw["kind"])
	}
}

// TestSessionMetaRoundTrip verifies SessionMeta - the type returned by
// /sessions and /sessions/{id} - round-trips including its time fields and
// optional ArchivedAt pointer.
func TestSessionMetaRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	archived := now.Add(time.Hour)
	in := SessionMeta{
		ID:           "sess-1",
		Title:        "demo",
		Mode:         "build",
		Background:   true,
		Archived:     true,
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      1.23,
		CreatedAt:    now,
		UpdatedAt:    now,
		ArchivedAt:   &archived,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out SessionMeta
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Title != in.Title || out.Mode != in.Mode ||
		out.Background != in.Background || out.Archived != in.Archived ||
		out.InputTokens != in.InputTokens || out.OutputTokens != in.OutputTokens ||
		out.CostUSD != in.CostUSD || !out.CreatedAt.Equal(in.CreatedAt) ||
		!out.UpdatedAt.Equal(in.UpdatedAt) || out.ArchivedAt == nil || !out.ArchivedAt.Equal(*in.ArchivedAt) {
		t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
	}
}

// TestSessionMetaArchivedAtOmittedWhenNil ensures an active (non-archived)
// session doesn't serialize a null archived_at, matching what
// internal/session.Store persists for active sessions.
func TestSessionMetaArchivedAtOmittedWhenNil(t *testing.T) {
	data, err := json.Marshal(SessionMeta{ID: "s", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := raw["archived_at"]; ok {
		t.Error("archived_at should be omitted when nil")
	}
}

// TestImageInputRoundTrip covers both the path-based and inline-data forms of
// an image attachment, since the daemon branches on which fields are set.
func TestImageInputRoundTrip(t *testing.T) {
	for _, in := range []ImageInput{
		{Path: "/tmp/screenshot.png"},
		{MediaType: "image/png", Data: "YWJj"},
	} {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var out ImageInput
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if out != in {
			t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
		}
	}
}

// TestPostMessageRequestGuardEnabledTristate verifies the nil/true/false
// tristate on GuardEnabled survives JSON round trips, since the server
// distinguishes "unset" (use the configured default) from an explicit
// override (see handlePostMessage's guardEnabled resolution).
func TestPostMessageRequestGuardEnabledTristate(t *testing.T) {
	trueVal, falseVal := true, false
	for _, want := range []*bool{nil, &trueVal, &falseVal} {
		in := PostMessageRequest{Text: "hi", GuardEnabled: want}
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var out PostMessageRequest
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		switch {
		case want == nil && out.GuardEnabled != nil:
			t.Errorf("GuardEnabled = %v, want nil", *out.GuardEnabled)
		case want != nil && (out.GuardEnabled == nil || *out.GuardEnabled != *want):
			t.Errorf("GuardEnabled round trip = %v, want %v", out.GuardEnabled, *want)
		}
	}
}

// TestApproveRequestRoundTrip covers the allow-always + pattern fields used
// to persist a permission rule (TQ6).
func TestApproveRequestRoundTrip(t *testing.T) {
	in := ApproveRequest{Approved: true, AllowAlways: true, Pattern: "npm test*", ID: "run-1"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out ApproveRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
	}
}

// TestRewindRequestResponseRoundTrip covers the P3.4 git-rollback fields.
func TestRewindRequestResponseRoundTrip(t *testing.T) {
	reqIn := RewindRequest{CheckpointID: "cp-1", Scope: "both", GitRollback: true}
	data, err := json.Marshal(reqIn)
	if err != nil {
		t.Fatalf("Marshal request: %v", err)
	}
	var reqOut RewindRequest
	if err := json.Unmarshal(data, &reqOut); err != nil {
		t.Fatalf("Unmarshal request: %v", err)
	}
	if reqOut != reqIn {
		t.Errorf("request round trip mismatch:\n in = %+v\nout = %+v", reqIn, reqOut)
	}

	respIn := RewindResponse{Scope: "code", FilesRestored: 3, MessagesKept: 7}
	data, err = json.Marshal(respIn)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	var respOut RewindResponse
	if err := json.Unmarshal(data, &respOut); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if respOut != respIn {
		t.Errorf("response round trip mismatch:\n in = %+v\nout = %+v", respIn, respOut)
	}
}

// TestErrorResponseRoundTrip covers the shape every non-2xx handler writes.
func TestErrorResponseRoundTrip(t *testing.T) {
	data, err := json.Marshal(ErrorResponse{Error: "not found"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out ErrorResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Error != "not found" {
		t.Errorf("Error = %q, want \"not found\"", out.Error)
	}
}

// TestCheckpointInfoRoundTrip covers the optional GitSHA field (P3.4).
func TestCheckpointInfoRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	in := CheckpointInfo{ID: "cp-1", Seq: 4, Label: "fix the bug", GitSHA: "abc123", FileCount: 2, CreatedAt: now}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out CheckpointInfo
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != in.ID || out.Seq != in.Seq || out.Label != in.Label ||
		out.GitSHA != in.GitSHA || out.FileCount != in.FileCount || !out.CreatedAt.Equal(in.CreatedAt) {
		t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
	}
}

// TestRunInfoRoundTrip covers the /runs observability payload.
func TestRunInfoRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	in := RunInfo{RunID: "r1", SessionID: "s1", Title: "demo", StartedAt: now, Tools: 3, LastKind: "tool_call"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out RunInfo
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.RunID != in.RunID || out.SessionID != in.SessionID || out.Title != in.Title ||
		!out.StartedAt.Equal(in.StartedAt) || out.Tools != in.Tools || out.LastKind != in.LastKind {
		t.Errorf("round trip mismatch:\n in = %+v\nout = %+v", in, out)
	}
}

// TestTeammateEndedAtOmitzero verifies a still-running teammate (zero
// EndedAt) omits the field rather than serializing the zero time, since
// omitzero (not omitempty) is used for a time.Time.
func TestTeammateEndedAtOmitzero(t *testing.T) {
	data, err := json.Marshal(Teammate{AgentID: "a1", Name: "explorer", Team: "default", Status: "running", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := raw["ended_at"]; ok {
		t.Error("ended_at should be omitted for a running teammate")
	}
}
