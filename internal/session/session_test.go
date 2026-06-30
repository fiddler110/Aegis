package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/trace"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSessionRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "first", "be helpful", "build", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
		{Role: provider.RoleAssistant, Content: []provider.Block{
			provider.TextBlock{Text: "calling tool"},
			provider.ToolUseBlock{ID: "tu1", Name: "grep", Input: json.RawMessage(`{"pattern":"x"}`)},
		}},
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "tu1", Content: "found", IsError: false},
		}},
	}
	if err := st.SaveMessages(ctx, sess.ID, msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	got, err := st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "first" || got.System != "be helpful" || got.Mode != "build" {
		t.Errorf("metadata mismatch: %+v", got)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(got.Messages))
	}
	// Verify the tool-use block survived with its input intact.
	tu, ok := got.Messages[1].Content[1].(provider.ToolUseBlock)
	if !ok {
		t.Fatalf("expected ToolUseBlock, got %T", got.Messages[1].Content[1])
	}
	if tu.Name != "grep" || string(tu.Input) != `{"pattern":"x"}` {
		t.Errorf("tool-use block corrupted: %+v", tu)
	}
	tr, ok := got.Messages[2].Content[0].(provider.ToolResultBlock)
	if !ok || tr.ToolUseID != "tu1" || tr.Content != "found" {
		t.Errorf("tool-result block corrupted: %+v", got.Messages[2].Content[0])
	}
}

func TestCreatePersistsPersona(t *testing.T) {
	store := newTestStore(t) // existing helper in this test file
	s, err := store.Create(context.Background(), "t", "sys", "build", "security-architect")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Persona != "security-architect" {
		t.Errorf("persona = %q, want security-architect", got.Persona)
	}
}

func TestListAndDelete(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a, _ := st.Create(ctx, "a", "", "plan", "")
	_, _ = st.Create(ctx, "b", "", "plan", "")

	metas, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Fatalf("List returned %d, want 2", len(metas))
	}

	if err := st.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, a.ID); err != ErrNotFound {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

func TestGetMissing(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Get(context.Background(), "nope"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAppendTraces(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sess, err := st.Create(ctx, "traced", "sys", "build", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Empty append is a no-op and must not error.
	if err := st.AppendTraces(ctx, sess.ID, nil); err != nil {
		t.Fatalf("AppendTraces(nil): %v", err)
	}

	// First run: two turns.
	run1 := []trace.TurnTrace{
		{Index: 0, Model: "claude-opus-4-8", InputTokens: 100, OutputTokens: 20, CostUSD: 0.003, WallMS: 1200,
			ToolCalls: []trace.ToolCall{{Name: "grep", DurationMS: 40}}},
		{Index: 1, Model: "claude-opus-4-8", InputTokens: 130, OutputTokens: 8, CostUSD: 0.002, WallMS: 800},
	}
	if err := st.AppendTraces(ctx, sess.ID, run1); err != nil {
		t.Fatalf("AppendTraces(run1): %v", err)
	}
	// Second run on the same session appends rather than overwrites.
	run2 := []trace.TurnTrace{
		{Index: 0, Model: "claude-opus-4-8", InputTokens: 50, OutputTokens: 5, CostUSD: 0.001, WallMS: 300},
	}
	if err := st.AppendTraces(ctx, sess.ID, run2); err != nil {
		t.Fatalf("AppendTraces(run2): %v", err)
	}

	got, err := st.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Traces) != 3 {
		t.Fatalf("got %d traces, want 3", len(got.Traces))
	}
	if got.Traces[0].ToolCalls[0].Name != "grep" || got.Traces[0].ToolCalls[0].DurationMS != 40 {
		t.Errorf("tool call not round-tripped: %+v", got.Traces[0].ToolCalls)
	}
	if got.Traces[2].InputTokens != 50 {
		t.Errorf("trace[2].InputTokens = %d, want 50", got.Traces[2].InputTokens)
	}
}

func TestAppendTracesMissingSession(t *testing.T) {
	st := newTestStore(t)
	err := st.AppendTraces(context.Background(), "nope", []trace.TurnTrace{{Index: 0}})
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
