package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
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

	sess, err := st.Create(ctx, "first", "be helpful", "build")
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

func TestListAndDelete(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a, _ := st.Create(ctx, "a", "", "plan")
	_, _ = st.Create(ctx, "b", "", "plan")

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
