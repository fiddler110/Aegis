package provider

import (
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/profile"
)

// TestWithHarness_ResolvesPerRequestModel confirms the harness decorator
// decides which repairs engage from Request.Model on every call, not once at
// wrap time — the property P74.17 exists for: one shared adapter serving a
// primary model and a task-routed small model must treat them differently.
func TestWithHarness_ResolvesPerRequestModel(t *testing.T) {
	resolve := profile.NewResolver(false, map[string]profile.Override{
		"local-small": {ArgumentShapeRepair: boolPtr(true)},
	})

	base := scriptedAdapter{events: toolUseOnly("tu_1", "read_file", json.RawMessage(`"go.mod"`))}
	a := WithHarness(base, resolve)

	// The "local-small" model has argument-shape repair enabled: a bare
	// scalar gets wrapped.
	events := drainStream(t, a, Request{Model: "local-small", Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil || args.Path != "go.mod" {
		t.Errorf("local-small: Input = %s, want repaired to {\"path\":\"go.mod\"}", call.Input)
	}

	// A different model on the same adapter, with no override, gets the
	// cloud default (nothing engaged): the bare scalar passes through as-is.
	events = drainStream(t, a, Request{Model: "claude-sonnet-5", Tools: readFileSchemaTool})
	call = onlyToolUse(t, events)
	if string(call.Input) != `"go.mod"` {
		t.Errorf("claude-sonnet-5: Input = %s, want unchanged", call.Input)
	}
}

// TestWithHarness_NeitherBehaviorEngagedPassesThroughUnmodified confirms a
// model with no repair behavior engaged sees exactly the base adapter's
// output — including a prose-only reply that salvage would otherwise have
// rewritten.
func TestWithHarness_NeitherBehaviorEngagedPassesThroughUnmodified(t *testing.T) {
	resolve := profile.NewResolver(false, nil)
	base := scriptedAdapter{events: textOnly("```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"go.mod\"}}\n```")}
	a := WithHarness(base, resolve)
	events := drainStream(t, a, Request{Model: "claude-sonnet-5", Tools: readFileTool})
	for _, ev := range events {
		if ev.Type == EventToolUse {
			t.Fatalf("expected no tool_use event with both behaviors off, got %+v", events)
		}
	}
}

// TestWithHarness_BothBehaviorsCompose confirms a model with both behaviors
// on gets prose salvaged into a structured call *and* that call's arguments
// shape-repaired.
func TestWithHarness_BothBehaviorsCompose(t *testing.T) {
	resolve := profile.NewResolver(true, nil)
	// The prose call itself is well-formed enough for salvage to parse, but
	// its "arguments" field is wrapped one layer too deep — a mistake salvage
	// (which only unwraps a JSON-string-encoded object, not a redundant
	// object wrapper) leaves alone, and shape repair then cleans up.
	base := scriptedAdapter{events: textOnly("```json\n{\"name\": \"read_file\", \"arguments\": {\"arguments\": {\"path\": \"go.mod\"}}}\n```")}
	a := WithHarness(base, resolve)
	events := drainStream(t, a, Request{Model: "qwen3:14b", Tools: readFileSchemaTool})
	call := onlyToolUse(t, events)
	var args struct{ Path string }
	if err := json.Unmarshal(call.Input, &args); err != nil || args.Path != "go.mod" {
		t.Errorf("Input = %s, want salvaged and shape-repaired to {\"path\":\"go.mod\"}", call.Input)
	}
}

func TestWithHarness_NilResolveReturnsBaseUnchanged(t *testing.T) {
	base := scriptedAdapter{events: textOnly("hi")}
	a := WithHarness(base, nil)
	if _, ok := a.(scriptedAdapter); !ok {
		t.Errorf("expected base returned unchanged for nil resolve, got %T", a)
	}
}

func boolPtr(b bool) *bool { return &b }
