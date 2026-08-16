package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
)

// capturingAdapter records the request it was handed, so a test can assert what
// the summarizing model was actually shown rather than only what came back.
type capturingAdapter struct {
	summary string
	reqs    []provider.Request
}

func (a *capturingAdapter) Name() string { return "capturing" }
func (a *capturingAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.reqs = append(a.reqs, req)
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: a.summary}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

func longConversation() []provider.Message {
	return []provider.Message{
		text(provider.RoleUser, "msg one is fairly long here"),
		text(provider.RoleAssistant, "reply one is also long"),
		text(provider.RoleUser, "msg two continues at length"),
		text(provider.RoleAssistant, "reply two continues as well"),
		text(provider.RoleUser, "msg three"),
		text(provider.RoleAssistant, "final reply kept"),
	}
}

// TestSummaryCarriesTheFileSet is the P65.2 deterministic half: paths the caller
// reports must reach the summarization request *and* be re-emitted into the
// summary by code, not by the model. The second is the load-bearing one — the
// emitted tags are what the next compaction parses, so a summary that merely
// mentioned the files in prose would break accumulation silently.
func TestSummaryCarriesTheFileSet(t *testing.T) {
	a := &capturingAdapter{summary: "## Goal\n- ship it"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})

	ctx := WithFiles(context.Background(), []string{"internal/a.go", "docs/b.md"}, []string{"internal/c.go"})
	out, changed, err := s.Compact(ctx, "", longConversation())
	if err != nil || !changed {
		t.Fatalf("Compact: changed=%v err=%v", changed, err)
	}
	if len(a.reqs) != 1 {
		t.Fatalf("expected one summarization request, got %d", len(a.reqs))
	}

	sent := a.reqs[0].Messages[0].Content[0].(provider.TextBlock).Text
	for _, want := range []string{"internal/a.go", "docs/b.md", "internal/c.go", fileListPreamble} {
		if !strings.Contains(sent, want) {
			t.Errorf("summarization request is missing %q", want)
		}
	}

	summary := out[0].Content[0].(provider.TextBlock).Text
	if !strings.Contains(summary, readFilesOpen) || !strings.Contains(summary, modifiedFilesOpen) {
		t.Fatalf("summary carries no tagged file lists:\n%s", summary)
	}
	read, modified := parseFileLists(summary)
	if len(read) != 2 || read[0] != "internal/a.go" {
		t.Errorf("read list = %v, want the two read paths in order", read)
	}
	if len(modified) != 1 || modified[0] != "internal/c.go" {
		t.Errorf("modified list = %v, want internal/c.go", modified)
	}
	// A path must not switch lists — telling the model it modified a file it
	// only read is worse than saying nothing.
	if strings.Contains(strings.SplitN(summary, modifiedFilesOpen, 2)[1], "docs/b.md") {
		t.Error("a read-only path leaked into the modified list")
	}
}

// TestFileListsAccumulateAcrossCompactions is the half a single-compaction
// fixture cannot test, and the item says so explicitly: the failure it guards
// against is a second compaction quietly dropping what the first recorded. It
// also pins the tags as a wire format between successive summaries — renaming
// them breaks accumulation with every single-compaction test still green.
func TestFileListsAccumulateAcrossCompactions(t *testing.T) {
	a := &capturingAdapter{summary: "## Goal\n- keep going"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})

	first, changed, err := s.Compact(
		WithFiles(context.Background(), []string{"old/read.go"}, []string{"old/written.go"}),
		"", longConversation())
	if err != nil || !changed {
		t.Fatalf("first Compact: changed=%v err=%v", changed, err)
	}

	// Grow the conversation again and compact a second time, reporting a
	// different file this run — the previous summary is now part of the prefix.
	second := append(first, longConversation()...)
	out, changed, err := s.Compact(
		WithFiles(context.Background(), []string{"new/read.go"}, nil),
		"", second)
	if err != nil || !changed {
		t.Fatalf("second Compact: changed=%v err=%v", changed, err)
	}

	summary := out[0].Content[0].(provider.TextBlock).Text
	read, modified := parseFileLists(summary)
	for _, want := range []string{"old/read.go", "new/read.go"} {
		if !contains(read, want) {
			t.Errorf("read list %v lost %q across the second compaction", read, want)
		}
	}
	// The modified list survives even though this run reported no writes: it
	// came from the first summary, which is the whole accumulation mechanism.
	if !contains(modified, "old/written.go") {
		t.Errorf("modified list %v lost the path recorded by the first compaction", modified)
	}
}

// TestFallbackCompactCarriesTheFileSet covers the path that fires when a local
// summarizer keeps failing — the same population the carried lists exist to
// help. Because the fallback replaces the prefix outright, dropping the lists
// here would destroy them permanently rather than for one turn.
func TestFallbackCompactCarriesTheFileSet(t *testing.T) {
	a := &capturingAdapter{summary: "## Goal\n- x"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})

	first, _, err := s.Compact(
		WithFiles(context.Background(), []string{"kept/read.go"}, []string{"kept/written.go"}),
		"", longConversation())
	if err != nil {
		t.Fatal(err)
	}

	out, changed := s.FallbackCompact(append(first, longConversation()...))
	if !changed {
		t.Fatal("expected the fallback to compact")
	}
	note := out[0].Content[0].(provider.TextBlock).Text
	read, modified := parseFileLists(note)
	if !contains(read, "kept/read.go") || !contains(modified, "kept/written.go") {
		t.Errorf("fallback lost the accumulated file set: read=%v modified=%v", read, modified)
	}
}

// TestCarriedFileListIsCappedAndSaysSo pins maxCarriedFiles and, more
// importantly, that hitting it is *stated*. A silently shortened list reads to
// the model as a complete one — the same rule truncNotice and omissionNote are
// built on. The boundary cases are asserted either side of the cap because a
// count assertion alone cannot tell adjacent thresholds apart (P63.9).
func TestCarriedFileListIsCappedAndSaysSo(t *testing.T) {
	atCap := make([]string, maxCarriedFiles)
	for i := range atCap {
		atCap[i] = string(rune('a'+i%26)) + "/f.go"
	}
	if got := renderFileLists(atCap, nil); strings.Contains(got, "omitted") {
		t.Error("a list exactly at the cap must not claim anything was omitted")
	}

	overCap := append([]string{"oldest/dropped.go"}, atCap...)
	got := renderFileLists(overCap, nil)
	if !strings.Contains(got, "omitted") {
		t.Error("a list over the cap must say something was omitted")
	}
	if strings.Contains(got, "oldest/dropped.go") {
		t.Error("the cap must drop the oldest path, not the newest")
	}
	if !strings.Contains(got, atCap[len(atCap)-1]) {
		t.Error("the cap must keep the most recently touched path")
	}
}

// TestSummarySkeletonCostIsBounded is the measurement the item asks for before
// choosing a section list: a skeleton is output tokens, summaryTokens bounds the
// reply, and a skeleton that crowds out content on a small window is a real
// regression. Measured with tokenest, per this document's own repeated finding
// about checking the instrument.
//
// The bound is on the *system prompt's* growth, which is what the change
// actually spends. It is deliberately generous — this is a report with a
// sanity ceiling, not a tuned threshold — but it fails loudly if someone adds
// six more headings.
func TestSummarySkeletonCostIsBounded(t *testing.T) {
	cost := tokenest.Estimate(summarizeSystemPrompt)
	t.Logf("summarize system prompt: %d estimated tokens", cost)
	if cost > 200 {
		t.Errorf("the summarization skeleton costs %d tokens; a skeleton that crowds out the summary "+
			"is a regression — cut sections rather than raising this", cost)
	}
	// Every heading the prompt names must actually be listed, or the model is
	// being asked to fill a skeleton it was never shown.
	for _, h := range []string{"## Goal", "## Constraints", "## Progress", "## Key Decisions", "## Next Steps"} {
		if !strings.Contains(summarizeSystemPrompt, h) {
			t.Errorf("skeleton is missing %q", h)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
