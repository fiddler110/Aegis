package guard

import (
	"context"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
)

// fakeAdapter returns a fixed text response, ignoring the request.
type fakeAdapter struct{ reply string }

func (f fakeAdapter) Name() string { return "fake" }
func (f fakeAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: f.reply}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

func TestSchemaGuard(t *testing.T) {
	g := SchemaGuard([]string{"findings", "summary"})
	if ok, _ := g(context.Background(), `{"findings":[],"summary":"x"}`); !ok {
		t.Error("valid object with required keys should pass")
	}
	if ok, reason := g(context.Background(), `{"summary":"x"}`); ok || reason == "" {
		t.Errorf("missing key should fail with reason, got ok=%v reason=%q", ok, reason)
	}
	if ok, _ := g(context.Background(), `not json`); ok {
		t.Error("non-JSON should fail")
	}
	// Fenced JSON is tolerated.
	if ok, _ := g(context.Background(), "```json\n{\"findings\":1,\"summary\":2}\n```"); !ok {
		t.Error("fenced JSON should pass")
	}
}

func TestLLMGuardPassFail(t *testing.T) {
	pass := LLMGuard(fakeAdapter{reply: "PASS"}, "m", "rubric")
	if ok, _ := pass(context.Background(), "answer"); !ok {
		t.Error("PASS verdict should pass")
	}
	fail := LLMGuard(fakeAdapter{reply: "FAIL: missing citations"}, "m", "rubric")
	if ok, reason := fail(context.Background(), "answer"); ok || reason != "missing citations" {
		t.Errorf("FAIL verdict should fail with reason, got ok=%v reason=%q", ok, reason)
	}
	// Unparseable verdict fails open.
	weird := LLMGuard(fakeAdapter{reply: "I think maybe"}, "m", "rubric")
	if ok, _ := weird(context.Background(), "answer"); !ok {
		t.Error("unparseable verdict should fail open (pass)")
	}
}

func TestResolve(t *testing.T) {
	if g, _ := Resolve(Config{Disabled: true}, nil, ""); g != nil {
		t.Error("disabled config returns nil guard")
	}
	if g, n := Resolve(Config{Mode: "schema", Schema: []string{"a"}}, nil, ""); g == nil || n != 1 {
		t.Errorf("schema mode returns guard + default retries 1, got g=%v n=%d", g != nil, n)
	}
	if g, _ := Resolve(Config{Mode: "llm", Rubric: "r"}, nil, ""); g != nil {
		t.Error("llm mode with no adapter/model returns nil (skipped)")
	}
	if g, n := Resolve(Config{Mode: "llm", Rubric: "r", MaxRetries: 3}, fakeAdapter{reply: "PASS"}, "m"); g == nil || n != 3 {
		t.Errorf("llm mode with adapter returns guard + retries 3, got g=%v n=%d", g != nil, n)
	}
}
