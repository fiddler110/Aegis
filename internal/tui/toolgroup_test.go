package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/api"
)

// TestReadGroup_TwoSequentialReadsCollapse covers P74.4's core case: two
// consecutive, successful read-capability calls (the model waits for each
// result before issuing the next, the common exploration-phase shape) fold
// into one collapsed card instead of two independent ones.
func TestReadGroup_TwoSequentialReadsCollapse(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("look around", nil)
	m.streaming = true
	m.followBottom = true

	in1, _ := json.Marshal(map[string]string{"path": "a.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: in1})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package a\n"})

	in2, _ := json.Marshal(map[string]string{"path": "b.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_2", ToolInput: in2})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_2", ToolResult: "package b\n"})

	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "Read 2 files") {
		t.Fatalf("expected a collapsed 2-file summary, got:\n%s", got)
	}
}

// TestReadGroup_ErrorBreaksTheGroup covers the roadmap's narrow-grouping
// rule: a failed call never joins a group, and it stays visible on its own
// even when it's sandwiched between two otherwise-groupable successes.
func TestReadGroup_ErrorBreaksTheGroup(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("look around", nil)
	m.streaming = true
	m.followBottom = true

	in1, _ := json.Marshal(map[string]string{"path": "a.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: in1})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package a\n"})

	in2, _ := json.Marshal(map[string]string{"path": "missing.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_2", ToolInput: in2})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_2", ToolResult: "no such file", ToolIsError: true})

	in3, _ := json.Marshal(map[string]string{"path": "c.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_3", ToolInput: in3})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_3", ToolResult: "package c\n"})

	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "read 2 files") || strings.Contains(got, "read 3 files") {
		t.Fatalf("expected no group spanning the errored call, got:\n%s", got)
	}
	if !strings.Contains(got, "no such file") {
		t.Fatalf("expected the failed call to still render its own error card, got:\n%s", got)
	}
}

// TestReadGroup_WriteBreaksTheGroup: a write/execute call in between two
// read-capability successes must not be folded in, and must not itself
// become part of any group.
func TestReadGroup_WriteBreaksTheGroup(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("look around", nil)
	m.streaming = true
	m.followBottom = true

	in1, _ := json.Marshal(map[string]string{"path": "a.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: in1})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package a\n"})

	winput, _ := json.Marshal(map[string]string{"path": "a.go", "content": "package a\n\nfunc f() {}\n"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "write_file", ToolID: "tu_2", ToolInput: winput})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "write_file", ToolID: "tu_2", ToolResult: "wrote a.go"})

	in3, _ := json.Marshal(map[string]string{"path": "b.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_3", ToolInput: in3})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_3", ToolResult: "package b\n"})

	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "read 2 files") {
		t.Fatalf("expected the write call to break grouping across it, got:\n%s", got)
	}
}

// TestReadGroup_MixedGrepAndReadSummarize covers grep+read_file grouping
// together into one summary distinguishing "patterns" from "files".
func TestReadGroup_MixedGrepAndReadSummarize(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("look around", nil)
	m.streaming = true
	m.followBottom = true

	gin, _ := json.Marshal(map[string]string{"pattern": "TODO"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "grep", ToolID: "tu_1", ToolInput: gin})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "grep", ToolID: "tu_1", ToolResult: "a.go:1:TODO\n"})

	rin, _ := json.Marshal(map[string]string{"path": "a.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_2", ToolInput: rin})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_2", ToolResult: "package a\n"})

	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "Searched 1 pattern") || !strings.Contains(got, "read 1 file") {
		t.Fatalf("expected a combined pattern+file summary, got:\n%s", got)
	}
}

// TestReadGroup_ParallelRoundMergesOnlyWhatHasResolved documents the P74.4
// parallel-round decision: three read_file calls are appended together
// (a whole round, before any result arrives — engine.runTools' concurrent
// read path), and their results land out of call order. A card is only
// ever folded into a group once its own result has actually arrived, so a
// still-pending sibling positioned between two resolved ones must not be
// silently counted — grouping stays correct even though it under-groups
// this particular ordering rather than claiming three reads before the
// third one is confirmed to have succeeded.
func TestReadGroup_ParallelRoundMergesOnlyWhatHasResolved(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("read three files", nil)
	m.streaming = true
	m.followBottom = true

	inA, _ := json.Marshal(map[string]string{"path": "a.go"})
	inB, _ := json.Marshal(map[string]string{"path": "b.go"})
	inC, _ := json.Marshal(map[string]string{"path": "c.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_a", ToolInput: inA})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_b", ToolInput: inB})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_c", ToolInput: inC})

	// B resolves first, while A (positioned before it) is still pending:
	// B cannot merge backward into anything yet.
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_b", ToolResult: "package b\n"})
	m.refresh()
	if strings.Contains(plainView(m), "Read 2 files") || strings.Contains(plainView(m), "Read 3 files") {
		t.Fatalf("expected no group yet — A hasn't resolved — got:\n%s", plainView(m))
	}

	// C resolves next: C's positional predecessor is B, already resolved
	// and ungrouped, so B+C merge into a two-member group.
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_c", ToolResult: "package c\n"})
	m.refresh()
	if !strings.Contains(plainView(m), "Read 2 files") {
		t.Fatalf("expected B+C to merge into a 2-file group, got:\n%s", plainView(m))
	}

	// A resolves last: A sits before the group, not adjacent to it (B's
	// card, not A's, is the group's positional start), so A stays separate.
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_a", ToolResult: "package a\n"})
	m.refresh()
	got := plainView(m)
	if strings.Contains(got, "Read 3 files") {
		t.Fatalf("expected A to stay outside the group (it precedes the group's own start), got:\n%s", got)
	}
	if !strings.Contains(got, "package a") {
		t.Fatalf("expected A's own result still rendered on its own card, got:\n%s", got)
	}
}

// TestReadGroup_ExpandsInFullMode: with /tools full active (toolCompact
// false), a group renders one line per call instead of the single collapsed
// summary.
func TestReadGroup_ExpandsInFullMode(t *testing.T) {
	m := followBottomTestModel(t)
	m.appendUser("look around", nil)
	m.streaming = true
	m.followBottom = true
	m.toolCompact = false

	in1, _ := json.Marshal(map[string]string{"path": "a.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_1", ToolInput: in1})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_1", ToolResult: "package a\n"})

	in2, _ := json.Marshal(map[string]string{"path": "b.go"})
	m.applyEvent(api.Event{Kind: api.KindToolCall, Tool: "read_file", ToolID: "tu_2", ToolInput: in2})
	m.applyEvent(api.Event{Kind: api.KindToolResult, Tool: "read_file", ToolID: "tu_2", ToolResult: "package b\n"})

	m.refresh()
	got := plainView(m)
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "b.go") {
		t.Fatalf("expected both file paths listed in expanded mode, got:\n%s", got)
	}
}
