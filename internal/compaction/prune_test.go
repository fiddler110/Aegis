package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

func toolUse(id, name string, input json.RawMessage) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, Content: []provider.Block{
		provider.ToolUseBlock{ID: id, Name: name, Input: input},
	}}
}

func toolResult(id, content string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: id, Content: content},
	}}
}

func TestPruneSupersededFileRead(t *testing.T) {
	msgs := []provider.Message{
		toolUse("a", "read_file", json.RawMessage(`{"path":"foo.go"}`)),
		toolResult("a", "package foo\n\nfunc Foo() {}\n"),
		text(provider.RoleUser, "now check something else"),
		toolUse("b", "read_file", json.RawMessage(`{"path":"foo.go"}`)),
		toolResult("b", "package foo\n\nfunc Foo() { return }\n"),
		text(provider.RoleAssistant, "done"),
		text(provider.RoleUser, "thanks"),
	}
	// keepRecent=1 keeps only the last message verbatim, so everything else
	// is eligible for pruning.
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned == 0 {
		t.Fatal("expected some pruning")
	}
	first := out[1].Content[0].(provider.ToolResultBlock)
	if strings.Contains(first.Content, "func Foo() {}") {
		t.Errorf("stale read_file result should have been pruned, got %q", first.Content)
	}
	if !strings.Contains(first.Content, "pruned") {
		t.Errorf("expected pruned marker, got %q", first.Content)
	}
	// The later, final read of foo.go must survive untouched.
	second := out[4].Content[0].(provider.ToolResultBlock)
	if !strings.Contains(second.Content, "return") {
		t.Errorf("last read of foo.go should be preserved, got %q", second.Content)
	}
}

// TestPruneLargeSearchDumpSupersededByIdenticalLaterSearch is the P9
// regression: a search dump is only pruned once verified superseded by an
// identical later call — mirroring the read_file "re-read later" check —
// rather than merely because it's old and large.
func TestPruneLargeSearchDumpSupersededByIdenticalLaterSearch(t *testing.T) {
	big := strings.Repeat("match found in file.go:1\n", 40) // > 500 chars
	msgs := []provider.Message{
		toolUse("g1", "grep", json.RawMessage(`{"pattern":"match"}`)),
		toolResult("g1", big),
		text(provider.RoleUser, "run it again"),
		toolUse("g2", "grep", json.RawMessage(`{"pattern":"match"}`)),
		toolResult("g2", big),
		text(provider.RoleAssistant, "found it"),
		text(provider.RoleUser, "ok"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned == 0 {
		t.Fatal("expected pruning of the superseded grep dump")
	}
	first := out[1].Content[0].(provider.ToolResultBlock)
	if len(first.Content) >= len(big) {
		t.Errorf("superseded grep dump should have shrunk, before=%d after=%d", len(big), len(first.Content))
	}
	if !strings.Contains(first.Content, "pruned") {
		t.Errorf("expected pruned marker, got %q", first.Content)
	}
	// The later, superseding search must survive untouched.
	second := out[4].Content[0].(provider.ToolResultBlock)
	if second.Content != big {
		t.Errorf("the superseding search result should be preserved verbatim")
	}
}

// TestPruneKeepsSearchDumpWithoutLaterIdenticalSearch is the false-positive
// counterpart: a search dump with no later identical call must survive even
// though it's old and large — the model may return to it many turns later
// without re-running the search, and pruning by age alone would silently
// destroy the only record of that information.
func TestPruneKeepsSearchDumpWithoutLaterIdenticalSearch(t *testing.T) {
	big := strings.Repeat("match found in file.go:1\n", 40) // > 500 chars
	msgs := []provider.Message{
		toolUse("g", "grep", json.RawMessage(`{"pattern":"match"}`)),
		toolResult("g", big),
		text(provider.RoleAssistant, "found it"),
		text(provider.RoleUser, "ok"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned != 0 {
		t.Errorf("expected no pruning without a superseding later search, pruned=%d", pruned)
	}
	tr := out[1].Content[0].(provider.ToolResultBlock)
	if tr.Content != big {
		t.Errorf("grep dump without a later identical search should be preserved verbatim, got %q", tr.Content)
	}
}

func TestPruneNeverTouchesRecentWindow(t *testing.T) {
	big := strings.Repeat("x", 1000)
	msgs := []provider.Message{
		toolUse("g", "grep", json.RawMessage(`{"pattern":"x"}`)),
		toolResult("g", big),
	}
	// keepRecent covers the whole conversation: nothing should be touched.
	out, pruned := pruneStaleToolResults(msgs, len(msgs))
	if pruned != 0 {
		t.Errorf("expected no pruning inside the keepRecent window, pruned=%d", pruned)
	}
	tr := out[1].Content[0].(provider.ToolResultBlock)
	if tr.Content != big {
		t.Errorf("recent tool result was modified")
	}
}

func TestPruneSkipsErrorResults(t *testing.T) {
	big := strings.Repeat("x", 1000)
	msgs := []provider.Message{
		toolUse("g", "grep", json.RawMessage(`{"pattern":"x"}`)),
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "g", Content: big, IsError: true},
		}},
		text(provider.RoleUser, "retry"),
		text(provider.RoleAssistant, "ok"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned != 0 {
		t.Errorf("error results should never be pruned, pruned=%d", pruned)
	}
	tr := out[1].Content[0].(provider.ToolResultBlock)
	if tr.Content != big {
		t.Error("error tool result was modified")
	}
}

func TestCompactPrunesBeforeSummarizing(t *testing.T) {
	a := &summaryAdapter{summary: "should not be used"}
	// MaxBudget chosen so pruning the superseded dump alone brings us back
	// under budget, while the unpruned pair of dumps exceeds it (so
	// compaction is triggered in the first place).
	big := strings.Repeat("match line here\n", 60)
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 400, KeepRecent: 2})
	msgs := []provider.Message{
		toolUse("g1", "grep", json.RawMessage(`{"pattern":"x"}`)),
		toolResult("g1", big),
		text(provider.RoleUser, "run it again"),
		toolUse("g2", "grep", json.RawMessage(`{"pattern":"x"}`)),
		toolResult("g2", big),
		text(provider.RoleUser, "thanks"),
		text(provider.RoleAssistant, "np"),
	}
	out, changed, err := s.Compact(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !changed {
		t.Fatal("expected pruning to count as a change")
	}
	if a.called != 0 {
		t.Errorf("LLM summarizer should not have been invoked, called=%d", a.called)
	}
	tr := out[1].Content[0].(provider.ToolResultBlock)
	if !strings.Contains(tr.Content, "pruned") {
		t.Errorf("expected the superseded grep dump to be pruned, got %q", tr.Content)
	}
}
