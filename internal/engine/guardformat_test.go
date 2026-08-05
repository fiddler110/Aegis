package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// recordingAdapter (toolshim_test.go) and textTurn (toolfailure_test.go) are
// reused here: this test asserts on the same thing the shim tests do — what
// actually went over the wire per turn.

// TestSchemaGuardRetryIsConstrained is the P59.8 behavior: a schema guard that
// rejects a free-generation reply must re-ask with decoding constrained to the
// required shape — the first turn unconstrained (that is where the work
// happens), the corrective retry carrying the schema and no tools.
func TestSchemaGuardRetryIsConstrained(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		textTurn("here you go, boss"), // not JSON — the guard rejects it
		textTurn(`{"summary":"s","risk":"low"}`),
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	required := []string{"summary", "risk"}
	format := guard.SchemaFormat(required)
	eng, err := New(Options{
		Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100,
		OutputGuard: guard.SchemaGuard(required), OutputGuardMaxRetries: 1,
		OutputGuardFormat: format,
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "report"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(adapter.reqs) != 2 {
		t.Fatalf("adapter calls = %d, want 2 (initial + guard retry)", len(adapter.reqs))
	}
	if adapter.reqs[0].Format != nil {
		t.Errorf("the first turn must be unconstrained; got Format=%s", adapter.reqs[0].Format)
	}
	if len(adapter.reqs[0].Tools) == 0 {
		t.Error("the first turn must still offer tools")
	}
	if string(adapter.reqs[1].Format) != string(format) {
		t.Errorf("retry Format = %s, want %s", adapter.reqs[1].Format, format)
	}
	if len(adapter.reqs[1].Tools) != 0 {
		t.Errorf("a constrained retry must not also offer tools, got %d", len(adapter.reqs[1].Tools))
	}
}

// TestGuardFormatUnsetLeavesRetryFree: an llm-mode guard (or any guard with no
// machine-checkable shape) has nothing to compile, and its corrective retry must
// stay ordinary free generation — including keeping its tools, since that
// retry's whole point can be "go fix the file you wrote".
func TestGuardFormatUnsetLeavesRetryFree(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		textTurn("nope"),
		textTurn(`{"summary":"s"}`),
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{
		Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100,
		OutputGuard: guard.SchemaGuard([]string{"summary"}), OutputGuardMaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "report"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, req := range adapter.reqs {
		if req.Format != nil {
			t.Errorf("turn %d carried Format=%s with no OutputGuardFormat set", i, req.Format)
		}
		if len(req.Tools) == 0 {
			t.Errorf("turn %d dropped its tools with no constraint in play", i)
		}
	}
}

// TestSchemaGuardFormatIsPerRetry: the constraint is consumed by the turn that
// answers the correction, not latched for the run. A second guard failure
// re-applies it; a turn that is not a guard retry never carries it.
func TestSchemaGuardFormatIsPerRetry(t *testing.T) {
	adapter := &recordingAdapter{turns: [][]provider.Event{
		textTurn("still prose"),
		textTurn("prose again"),
		textTurn(`{"summary":"s"}`),
	}}
	required := []string{"summary"}
	eng, err := New(Options{
		Adapter: adapter, Tools: tool.NewRegistry(), Model: "test", MaxTokens: 100,
		OutputGuard: guard.SchemaGuard(required), OutputGuardMaxRetries: 2,
		OutputGuardFormat: guard.SchemaFormat(required),
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "report"}}})
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(adapter.reqs) != 3 {
		t.Fatalf("adapter calls = %d, want 3", len(adapter.reqs))
	}
	if adapter.reqs[0].Format != nil {
		t.Error("first turn must be unconstrained")
	}
	for i := 1; i < 3; i++ {
		if adapter.reqs[i].Format == nil {
			t.Errorf("retry %d must be constrained", i)
		}
	}
}

// TestSchemaFormatMatchesGuardContract: the schema handed to the backend must
// require exactly the keys the guard checks — no more (which would reject
// answers the guard would accept, under a grammar the model cannot argue with)
// and no fewer.
func TestSchemaFormatMatchesGuardContract(t *testing.T) {
	raw := guard.SchemaFormat([]string{"summary", " risk ", "summary", ""})
	if raw == nil {
		t.Fatal("SchemaFormat returned nil for a non-empty key list")
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("SchemaFormat output is not valid JSON: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("type = %q, want object", schema.Type)
	}
	want := map[string]bool{"summary": true, "risk": true}
	if len(schema.Required) != len(want) {
		t.Fatalf("required = %v, want the two distinct trimmed keys", schema.Required)
	}
	for _, k := range schema.Required {
		if !want[k] {
			t.Errorf("unexpected required key %q", k)
		}
		if _, ok := schema.Properties[k]; !ok {
			t.Errorf("required key %q missing from properties", k)
		}
	}
	// An open value schema: the guard asserts presence, never type, so the
	// constraint must not narrow what a value may be.
	if got := string(schema.Properties["summary"]); got != "{}" {
		t.Errorf("property schema = %s, want {} (guard checks presence only)", got)
	}
	if guard.SchemaFormat(nil) != nil || guard.SchemaFormat([]string{" ", ""}) != nil {
		t.Error("no requirable keys must yield nil (don't constrain)")
	}
}
