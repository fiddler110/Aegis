package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// errResult is toolResult with the error flag set.
func errResult(id, content string) provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: id, Content: content, IsError: true},
	}}
}

// coldConv builds a conversation with n clearable read_file results, each large
// enough to clear, in call order.
func coldConv(t *testing.T, n int) []provider.Message {
	t.Helper()
	big := strings.Repeat("package foo // a result large enough to be worth clearing\n", 20)
	var msgs []provider.Message
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs,
			toolUse(id, "read_file", json.RawMessage(`{"path":"f`+id+`.go"}`)),
			toolResult(id, big),
		)
	}
	return msgs
}

// clearedIDs reports which tool_use IDs hold the sentinel.
func clearedIDs(msgs []provider.Message) map[string]bool {
	out := map[string]bool{}
	for _, m := range msgs {
		for _, blk := range m.Content {
			if tr, ok := blk.(provider.ToolResultBlock); ok && tr.Content == ColdCacheSentinel {
				out[tr.ToolUseID] = true
			}
		}
	}
	return out
}

// TestColdClearKeepsTheMostRecentN pins the basic contract: the trailing keep
// clearable results survive verbatim and every earlier one becomes the sentinel.
func TestColdClearKeepsTheMostRecentN(t *testing.T) {
	msgs := coldConv(t, 5)
	out, cleared, freed := ClearColdToolResults(msgs, 2)
	if cleared != 3 {
		t.Fatalf("cleared = %d, want 3", cleared)
	}
	if freed <= 0 {
		t.Fatalf("freed = %d, want > 0", freed)
	}
	got := clearedIDs(out)
	for _, id := range []string{"a", "b", "c"} {
		if !got[id] {
			t.Errorf("result %q should have been cleared", id)
		}
	}
	for _, id := range []string{"d", "e"} {
		if got[id] {
			t.Errorf("result %q is inside the keep window and must survive", id)
		}
	}
}

// TestColdClearFloorsKeepAtOne is P67.6's first named constraint. A keep of 0 —
// or a negative one from an arithmetic slip upstream — must still leave the
// model one result to work from, never an empty working context.
func TestColdClearFloorsKeepAtOne(t *testing.T) {
	for _, keep := range []int{0, -1, -100} {
		msgs := coldConv(t, 4)
		out, cleared, _ := ClearColdToolResults(msgs, keep)
		if cleared != 3 {
			t.Errorf("keep=%d: cleared = %d, want 3 (one must survive)", keep, cleared)
		}
		if clearedIDs(out)["d"] {
			t.Errorf("keep=%d: the most recent result was cleared; the floor did not hold", keep)
		}
	}
}

// TestColdClearIsIdempotent pins the wire-format property. A second cold resume
// over an already-cleared conversation must report no work — that is what lets
// the caller treat cleared==0 as "nothing to do" rather than re-persisting the
// conversation on every idle turn.
func TestColdClearIsIdempotent(t *testing.T) {
	once, cleared, _ := ClearColdToolResults(coldConv(t, 5), 2)
	if cleared != 3 {
		t.Fatalf("first pass cleared = %d, want 3", cleared)
	}
	twice, cleared2, freed2 := ClearColdToolResults(once, 2)
	if cleared2 != 0 || freed2 != 0 {
		t.Fatalf("second pass cleared = %d / freed = %d, want 0 / 0", cleared2, freed2)
	}
	if len(clearedIDs(twice)) != 3 {
		t.Fatalf("second pass changed which results are cleared: %v", clearedIDs(twice))
	}
}

// TestColdClearDoesNotMutateTheInput guards the copy-on-write. The caller's
// slice is the live conversation; a pass that aliased it would rewrite history
// the engine had not yet decided to accept.
func TestColdClearDoesNotMutateTheInput(t *testing.T) {
	msgs := coldConv(t, 4)
	before := msgs[1].Content[0].(provider.ToolResultBlock).Content
	if _, cleared, _ := ClearColdToolResults(msgs, 1); cleared == 0 {
		t.Fatal("fixture cleared nothing")
	}
	after := msgs[1].Content[0].(provider.ToolResultBlock).Content
	if before != after {
		t.Fatal("ClearColdToolResults mutated the caller's messages")
	}
}

// TestColdClearNoOpReturnsTheSameSlice pins the other half of copy-on-write: a
// pass with nothing to do must not allocate a copy the caller then persists.
func TestColdClearNoOpReturnsTheSameSlice(t *testing.T) {
	msgs := coldConv(t, 2)
	out, cleared, _ := ClearColdToolResults(msgs, 5)
	if cleared != 0 {
		t.Fatalf("cleared = %d, want 0", cleared)
	}
	if &out[0] != &msgs[0] {
		t.Fatal("a no-op pass copied the conversation")
	}
}

// TestColdClearSkipsWhatItMustNotDrop covers the three exclusion rules at once:
// errors, non-clearable tools, and results too small to be worth the rewrite.
func TestColdClearSkipsWhatItMustNotDrop(t *testing.T) {
	big := strings.Repeat("x", 4000)
	msgs := []provider.Message{
		toolUse("err", "read_file", json.RawMessage(`{"path":"a.go"}`)),
		errResult("err", big),
		toolUse("write", "write_file", json.RawMessage(`{"path":"b.go"}`)),
		toolResult("write", big),
		toolUse("ask", "ask_user", json.RawMessage(`{}`)),
		toolResult("ask", big),
		toolUse("tiny", "grep", json.RawMessage(`{"pattern":"x"}`)),
		toolResult("tiny", "one hit"),
		// One genuinely clearable pair, so the fixture proves the pass ran at
		// all rather than passing because nothing matched.
		toolUse("ok1", "shell", json.RawMessage(`{"command":"go test ./..."}`)),
		toolResult("ok1", big),
		toolUse("ok2", "shell", json.RawMessage(`{"command":"go vet ./..."}`)),
		toolResult("ok2", big),
	}
	out, cleared, _ := ClearColdToolResults(msgs, 1)
	if cleared != 1 {
		t.Fatalf("cleared = %d, want exactly 1 (the superseded shell result)", cleared)
	}
	got := clearedIDs(out)
	if !got["ok1"] {
		t.Error("the older shell result should have been cleared")
	}
	for _, id := range []string{"err", "write", "ask", "tiny", "ok2"} {
		if got[id] {
			t.Errorf("result %q must never be cleared", id)
		}
	}
}

// TestColdClearSentinelIsStable is the rename guard. The sentinel is read back
// out of persisted conversations by the idempotence check above, so changing it
// silently breaks accumulation the same way renaming the compaction summary's
// <read-files> tag does — a test that fails loudly is the cheapest way to make
// that a decision rather than an accident.
func TestColdClearSentinelIsStable(t *testing.T) {
	const want = "[cleared: this tool result was dropped when the conversation resumed after an idle gap; re-run the tool if you still need it]"
	if ColdCacheSentinel != want {
		t.Fatalf("ColdCacheSentinel changed.\n got: %q\nwant: %q\n\nThis string is a wire format: it is written into persisted conversations "+
			"and read back by ClearColdToolResults on every later turn. Changing it makes every previously-cleared result look uncleared, so the pass "+
			"reports a yield on a conversation it did not change. If the change is intended, update this test in the same commit.", ColdCacheSentinel, want)
	}
}

// TestSummarizerColdCacheKeepDefault pins the Options default and that the
// method wired to engine.ColdCacheCompactor honors it.
func TestSummarizerColdCacheKeepDefault(t *testing.T) {
	s := New(Options{Adapter: &recordingAdapter{}, Model: "m", ContextWindow: 32768})
	if s.coldCacheKeep != 3 {
		t.Fatalf("default ColdCacheKeep = %d, want 3", s.coldCacheKeep)
	}
	_, cleared, _ := s.ClearColdToolResults(coldConv(t, 6))
	if cleared != 3 {
		t.Fatalf("cleared = %d, want 3 (6 results, keep 3)", cleared)
	}
}
