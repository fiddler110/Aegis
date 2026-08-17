package ollama

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// proseAndCall is the assistant turn at the centre of this file: the model
// narrated and called a tool in the same turn, which is what a thinking model
// does most turns and what provider.Message carries as two blocks.
func proseAndCall() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "read the config"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.TextBlock{Text: "I'll read the configuration file to find the token."},
			toolUse("tu_0", "read_file", `{"path":"srv/etc/config.txt"}`),
		}},
		toolResults(provider.ToolResultBlock{ToolUseID: "tu_0", Content: "token = ZX-4417-QQ"}),
	}
}

func assistantTurn(t *testing.T, wire []wireMessage) wireMessage {
	t.Helper()
	for _, m := range wire {
		if m.Role == "assistant" {
			return m
		}
	}
	t.Fatal("no assistant message in wire output")
	return wireMessage{}
}

// TestTranslateWithholdsProseOnlyWhenAsked pins both directions of the
// dropProse switch. The mitigation exists because Qwen3's stock Ollama chat
// template renders the assistant turn as `{{ if .Content }}…{{ else if
// .ToolCalls }}…{{ end }}`, so sending both fields loses the *call* — measured
// on qwen3:14b-32k as 0/3 correct when asked which path it had just read, and
// 3/3 once the prose was withheld. The tool call must survive either way; only
// the narration is negotiable.
func TestTranslateWithholdsProseOnlyWhenAsked(t *testing.T) {
	t.Run("default keeps both", func(t *testing.T) {
		am := assistantTurn(t, translate("", proseAndCall(), false))
		if am.Content == "" {
			t.Error("prose was dropped with dropProse=false; a correct template must pay nothing")
		}
		if len(am.ToolCalls) != 1 {
			t.Fatalf("got %d tool calls, want 1", len(am.ToolCalls))
		}
	})

	t.Run("dropProse keeps the call", func(t *testing.T) {
		am := assistantTurn(t, translate("", proseAndCall(), true))
		if am.Content != "" {
			t.Errorf("prose survived with dropProse=true: %q", am.Content)
		}
		if len(am.ToolCalls) != 1 {
			t.Fatalf("got %d tool calls, want 1 — the call is the part that must never be lost", len(am.ToolCalls))
		}
		if got := string(am.ToolCalls[0].Function.Arguments); !strings.Contains(got, "srv/etc/config.txt") {
			t.Errorf("arguments = %s, want the original path intact", got)
		}
	})
}

// TestTranslateKeepsProseOnTurnsWithoutCalls is the narrow-scope guard: a
// plain assistant answer carries no tool call, so there is nothing for the
// template to drop and nothing to withhold. Widening the mitigation to every
// assistant turn would silently delete the model's answers from history.
func TestTranslateKeepsProseOnTurnsWithoutCalls(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{provider.TextBlock{Text: "hello"}}},
	}
	am := assistantTurn(t, translate("", msgs, true))
	if am.Content != "hello" {
		t.Errorf("content = %q, want %q", am.Content, "hello")
	}
}

// TestTemplateVerdictIsProbedOncePerModel pins the caching contract: the
// template is read at most once per model per process, the verdict is
// persisted so the next process reads it from the store instead of the
// server, and two models are resolved independently — one daemon adapter
// serves a whole session mix, and Qwen3 and Qwen3.5 disagree about this.
func TestTemplateVerdictIsProbedOncePerModel(t *testing.T) {
	var probes int64
	caps := newFakeCapStore()
	a := New(WithCapabilityStore(caps), WithTemplateProbe(
		func(_ context.Context, _, model string) (bool, bool) {
			atomic.AddInt64(&probes, 1)
			return model == "broken", true
		}))

	for i := 0; i < 3; i++ {
		if got := a.templateDropsToolCalls(context.Background(), "broken"); !got {
			t.Fatal("broken model: want drops=true")
		}
		if got := a.templateDropsToolCalls(context.Background(), "fine"); got {
			t.Fatal("fine model: want drops=false")
		}
	}
	if probes != 2 {
		t.Errorf("probed the server %d times, want 2 (once per model)", probes)
	}

	// A second adapter stands in for the next process: it must resolve both
	// models from the store without probing at all.
	var probes2 int64
	b := New(WithCapabilityStore(caps), WithTemplateProbe(
		func(_ context.Context, _, _ string) (bool, bool) {
			atomic.AddInt64(&probes2, 1)
			return false, true
		}))
	if got := b.templateDropsToolCalls(context.Background(), "broken"); !got {
		t.Error("persisted verdict for broken model was not honored")
	}
	if got := b.templateDropsToolCalls(context.Background(), "fine"); got {
		t.Error("persisted verdict for fine model was not honored")
	}
	if probes2 != 0 {
		t.Errorf("second adapter probed %d times, want 0", probes2)
	}
}

// TestUnreadableTemplateIsNotPersisted covers the failure path. A server that
// cannot answer must leave the model unmitigated for this process — guessing
// "drops" would delete narration from a model that never had the defect — but
// must not write that non-answer to the store, or one unreachable moment
// answers the question wrongly forever.
func TestUnreadableTemplateIsNotPersisted(t *testing.T) {
	caps := newFakeCapStore()
	a := New(WithCapabilityStore(caps), WithTemplateProbe(
		func(_ context.Context, _, _ string) (bool, bool) { return false, false }))

	if got := a.templateDropsToolCalls(context.Background(), "unknown"); got {
		t.Error("want drops=false when the template is unreadable")
	}
	if caps.dropsWrites != 0 {
		t.Errorf("persisted %d verdicts from an unreadable template, want 0", caps.dropsWrites)
	}
}
