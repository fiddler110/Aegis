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

// TestPruneSuccessfulWriteInput blanks the verbatim file content in a
// write_file tool_use Input once the write has a successful result: the bytes
// are durably on disk, so resending them every turn is dead weight (P36.2).
func TestPruneSuccessfulWriteInput(t *testing.T) {
	body := strings.Repeat("some long file content line\n", 200) // ~5KB
	input := json.RawMessage(`{"path":"report.md","content":` + mustJSONString(body) + `}`)
	msgs := []provider.Message{
		toolUse("w", "write_file", input),
		toolResult("w", "wrote 5400 bytes to report.md"),
		text(provider.RoleAssistant, "written"),
		text(provider.RoleUser, "thanks"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned == 0 {
		t.Fatal("expected the write_file content payload to be pruned")
	}
	tu := out[0].Content[0].(provider.ToolUseBlock)
	if strings.Contains(string(tu.Input), "some long file content line") {
		t.Errorf("verbatim file content should have been blanked, got %q", string(tu.Input))
	}
	// The rewritten Input must stay well-formed and still deserialize into the
	// tool's argument struct, with the path preserved and the content replaced
	// by a marker.
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(tu.Input, &args); err != nil {
		t.Fatalf("pruned write_file Input no longer deserializes: %v", err)
	}
	if args.Path != "report.md" {
		t.Errorf("path must be preserved, got %q", args.Path)
	}
	if !strings.Contains(args.Content, "pruned") {
		t.Errorf("expected a pruned marker in content, got %q", args.Content)
	}
}

// TestPruneSuccessfulEditInput blanks the before/after snippets of a committed
// edit_file call, preserving path and all required fields.
func TestPruneSuccessfulEditInput(t *testing.T) {
	oldS := strings.Repeat("old body line\n", 100)
	newS := strings.Repeat("new body line\n", 100)
	input := json.RawMessage(`{"path":"main.go","old_string":` + mustJSONString(oldS) + `,"new_string":` + mustJSONString(newS) + `}`)
	msgs := []provider.Message{
		toolUse("e", "edit_file", input),
		toolResult("e", "edited main.go"),
		text(provider.RoleAssistant, "edited"),
		text(provider.RoleUser, "thanks"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned == 0 {
		t.Fatal("expected the edit_file snippets to be pruned")
	}
	tu := out[0].Content[0].(provider.ToolUseBlock)
	var args struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(tu.Input, &args); err != nil {
		t.Fatalf("pruned edit_file Input no longer deserializes: %v", err)
	}
	if args.Path != "main.go" {
		t.Errorf("path must be preserved, got %q", args.Path)
	}
	if strings.Contains(args.OldString, "old body line") || strings.Contains(args.NewString, "new body line") {
		t.Errorf("edit snippets should have been blanked, got old=%q new=%q", args.OldString, args.NewString)
	}
	if !strings.Contains(args.OldString, "pruned") || !strings.Contains(args.NewString, "pruned") {
		t.Errorf("expected pruned markers, got old=%q new=%q", args.OldString, args.NewString)
	}
}

// TestPruneSuccessfulMultiEditInput blanks the before/after snippets of every
// edit in a committed multi_edit call while preserving each edit's path and
// the overall array structure — the P36.2 rule extended to multi_edit's nested
// "edits" shape.
func TestPruneSuccessfulMultiEditInput(t *testing.T) {
	oldA := strings.Repeat("old alpha line\n", 100)
	newA := strings.Repeat("new alpha line\n", 100)
	oldB := strings.Repeat("old beta line\n", 100)
	newB := strings.Repeat("new beta line\n", 100)
	input := json.RawMessage(`{"edits":[` +
		`{"path":"a.go","old_string":` + mustJSONString(oldA) + `,"new_string":` + mustJSONString(newA) + `},` +
		`{"path":"b.go","old_string":` + mustJSONString(oldB) + `,"new_string":` + mustJSONString(newB) + `}` +
		`]}`)
	msgs := []provider.Message{
		toolUse("m", "multi_edit", input),
		toolResult("m", "applied 2 edit(s) across 2 file(s)"),
		text(provider.RoleAssistant, "edited"),
		text(provider.RoleUser, "thanks"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned == 0 {
		t.Fatal("expected the multi_edit snippets to be pruned")
	}
	tu := out[0].Content[0].(provider.ToolUseBlock)
	var args struct {
		Edits []struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(tu.Input, &args); err != nil {
		t.Fatalf("pruned multi_edit Input no longer deserializes: %v", err)
	}
	if len(args.Edits) != 2 {
		t.Fatalf("edits array must be preserved, got %d edits", len(args.Edits))
	}
	wantPaths := []string{"a.go", "b.go"}
	for i, e := range args.Edits {
		if e.Path != wantPaths[i] {
			t.Errorf("edit %d path must be preserved, got %q", i, e.Path)
		}
		if strings.Contains(e.OldString, "old ") || strings.Contains(e.NewString, "new ") {
			t.Errorf("edit %d snippets should have been blanked, got old=%q new=%q", i, e.OldString, e.NewString)
		}
		if !strings.Contains(e.OldString, "pruned") || !strings.Contains(e.NewString, "pruned") {
			t.Errorf("edit %d expected pruned markers, got old=%q new=%q", i, e.OldString, e.NewString)
		}
	}
}

// TestPruneKeepsFailedMultiEditInput is the safety counterpart: a multi_edit
// whose result is an error changed nothing on disk, so its Input (and every
// snippet) must survive verbatim for a retry.
func TestPruneKeepsFailedMultiEditInput(t *testing.T) {
	body := strings.Repeat("body\n", 200)
	input := json.RawMessage(`{"edits":[{"path":"a.go","old_string":` + mustJSONString(body) + `,"new_string":"x"}]}`)
	msgs := []provider.Message{
		toolUse("m", "multi_edit", input),
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "m", Content: "edit 1: old_string not found in a.go", IsError: true},
		}},
		text(provider.RoleAssistant, "failed"),
		text(provider.RoleUser, "retry"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned != 0 {
		t.Errorf("a failed multi_edit must not be pruned, pruned=%d", pruned)
	}
	tu := out[0].Content[0].(provider.ToolUseBlock)
	if string(tu.Input) != string(input) {
		t.Errorf("failed multi_edit Input was modified")
	}
}

// TestPruneKeepsFailedWriteInput is the safety counterpart: a write whose
// result is an error left nothing on disk to re-read, so its Input must be
// preserved verbatim.
func TestPruneKeepsFailedWriteInput(t *testing.T) {
	body := strings.Repeat("content\n", 200)
	input := json.RawMessage(`{"path":"report.md","content":` + mustJSONString(body) + `}`)
	msgs := []provider.Message{
		toolUse("w", "write_file", input),
		{Role: provider.RoleUser, Content: []provider.Block{
			provider.ToolResultBlock{ToolUseID: "w", Content: "write failed: disk full", IsError: true},
		}},
		text(provider.RoleAssistant, "failed"),
		text(provider.RoleUser, "retry"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned != 0 {
		t.Errorf("a failed write must not be pruned, pruned=%d", pruned)
	}
	tu := out[0].Content[0].(provider.ToolUseBlock)
	if string(tu.Input) != string(input) {
		t.Errorf("failed write_file Input was modified")
	}
}

// TestPruneKeepsRecentWriteInput confirms a write inside the keepRecent window
// is never touched.
func TestPruneKeepsRecentWriteInput(t *testing.T) {
	body := strings.Repeat("content\n", 200)
	input := json.RawMessage(`{"path":"report.md","content":` + mustJSONString(body) + `}`)
	msgs := []provider.Message{
		toolUse("w", "write_file", input),
		toolResult("w", "wrote bytes"),
	}
	out, pruned := pruneStaleToolResults(msgs, len(msgs))
	if pruned != 0 {
		t.Errorf("write inside keepRecent window must not be pruned, pruned=%d", pruned)
	}
	tu := out[0].Content[0].(provider.ToolUseBlock)
	if string(tu.Input) != string(input) {
		t.Errorf("recent write_file Input was modified")
	}
}

// TestPruneSkillReferenceRead drops a read_file of a skill's own directory
// after first use — static reference content is trivially re-fetchable, so it
// need not survive into the pre-keepRecent prefix even without a second read
// (P36.2).
func TestPruneSkillReferenceRead(t *testing.T) {
	skillDoc := strings.Repeat("STRIDE step guidance line\n", 300) // ~7KB, read once
	msgs := []provider.Message{
		toolUse("s", "read_file", json.RawMessage(`{"path":".aegis/builtin-skills/threat-model-analyst/reference.md"}`)),
		toolResult("s", skillDoc),
		text(provider.RoleAssistant, "loaded skill"),
		text(provider.RoleUser, "go"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned == 0 {
		t.Fatal("expected the one-time skill-reference read to be pruned")
	}
	tr := out[1].Content[0].(provider.ToolResultBlock)
	if strings.Contains(tr.Content, "STRIDE step guidance") {
		t.Errorf("skill-reference content should have been dropped, got %q", tr.Content)
	}
	if !strings.Contains(tr.Content, "pruned") {
		t.Errorf("expected pruned marker, got %q", tr.Content)
	}
}

// TestPruneKeepsNonSkillRead is the false-positive counterpart: an ordinary
// workspace read that is never re-read must survive, because it may still
// reflect state the model relies on and could go stale if silently dropped.
func TestPruneKeepsNonSkillRead(t *testing.T) {
	doc := strings.Repeat("ordinary source line\n", 300)
	msgs := []provider.Message{
		toolUse("r", "read_file", json.RawMessage(`{"path":"internal/app/main.go"}`)),
		toolResult("r", doc),
		text(provider.RoleAssistant, "read it"),
		text(provider.RoleUser, "go"),
	}
	out, pruned := pruneStaleToolResults(msgs, 1)
	if pruned != 0 {
		t.Errorf("a one-time non-skill read must be preserved, pruned=%d", pruned)
	}
	tr := out[1].Content[0].(provider.ToolResultBlock)
	if tr.Content != doc {
		t.Errorf("non-skill read content should be preserved verbatim")
	}
}

// TestIsSkillReferencePath exercises the path matcher directly, including the
// project skills dir and Windows separators.
func TestIsSkillReferencePath(t *testing.T) {
	cases := map[string]bool{
		".aegis/skills/foo/SKILL.md":                        true,
		".aegis/builtin-skills/threat-model-analyst/ref.md": true,
		"project/.aegis/skills/x.md":                        true,
		`.aegis\skills\foo\SKILL.md`:                        true,
		"internal/app/main.go":                              false,
		"skills-notes.md":                                   false,
		"":                                                  false,
	}
	for path, want := range cases {
		if got := isSkillReferencePath(path); got != want {
			t.Errorf("isSkillReferencePath(%q) = %v, want %v", path, got, want)
		}
	}
}

// mustJSONString marshals s as a JSON string literal for embedding in a raw
// input fixture.
func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
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
