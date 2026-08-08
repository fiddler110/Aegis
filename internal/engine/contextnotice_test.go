package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// noticeCompactor declines the run-start compaction pass, then compacts on
// the proactive per-turn check — so tests exercise the mid-run notice path.
type noticeCompactor struct{ calls int }

func (c *noticeCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	c.calls++
	if c.calls == 1 {
		return msgs, false, nil
	}
	return msgs[len(msgs)-1:], true, nil
}

func bigConversation() *Conversation {
	conv := &Conversation{System: "sys"}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: strings.Repeat("filler words here ", 40)}, // ~180 estimated tokens
	}})
	return conv
}

// TestProactiveCompactionEmitsNotice: crossing the 85% threshold with a
// working compactor surfaces a KindNotice so the user sees the compaction
// instead of it being a silent log line.
func TestProactiveCompactionEmitsNotice(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "ok"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	comp := &noticeCompactor{}
	eng, err := New(Options{Adapter: adapter, Tools: tool.NewRegistry(), Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Errorf("notices = %v, want one compaction notice", notices)
	}
}

// failingFallbackCompactor always fails Compact (simulating a local-model
// summarizer that reliably returns empty output — the P28.4 live-eval
// failure) but implements FallbackCompact, so the engine can fall back to a
// deterministic pass after two consecutive failures.
type failingFallbackCompactor struct {
	compactCalls  int
	fallbackCalls int
}

func (c *failingFallbackCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	c.compactCalls++
	return msgs, false, errors.New("summarizer returned empty output")
}

func (c *failingFallbackCompactor) FallbackCompact(msgs []provider.Message) ([]provider.Message, bool) {
	c.fallbackCalls++
	return msgs[len(msgs)-1:], true
}

// TestProactiveCompactionFallsBackAfterTwoFailures is the P28.4 regression:
// a Compactor whose LLM summarizer fails twice in a row must not leave the
// engine skipping compaction indefinitely — the engine falls back to the
// Compactor's deterministic FallbackCompact and the run keeps making
// progress on context size.
func TestProactiveCompactionFallsBackAfterTwoFailures(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_2", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &failingFallbackCompactor{}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if comp.compactCalls < 2 {
		t.Fatalf("expected Compact to be attempted at least twice, got %d", comp.compactCalls)
	}
	if comp.fallbackCalls != 1 {
		t.Fatalf("expected FallbackCompact to be called exactly once (after the 2nd consecutive failure), got %d", comp.fallbackCalls)
	}
	found := false
	for _, n := range notices {
		if strings.Contains(n, "fallback") {
			found = true
		}
	}
	if !found {
		t.Errorf("notices = %v, want one mentioning the fallback compaction", notices)
	}
}

// TestContextFullNoticeWithoutCompactor: with no compactor and the estimate
// at/over 95% of the window, the engine warns the user exactly once per run —
// this is the silent-truncation case on local model servers.
func TestContextFullNoticeWithoutCompactor(t *testing.T) {
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu_1", Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var notices []string
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Two iterations both exceed the threshold, but the warning fires once.
	if len(notices) != 1 || !strings.Contains(notices[0], "context") {
		t.Errorf("notices = %v, want exactly one context-full warning", notices)
	}
}

// latchCompactor always fails Compact (a weak local-model summarizer that only
// ever returns empty) and its FallbackCompact returns the messages unchanged so
// the token estimate stays over threshold and compaction keeps triggering every
// iteration. It counts both call kinds so a test can assert the P39.8 latch.
type latchCompactor struct {
	compactCalls  int
	fallbackCalls int
}

func (c *latchCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	c.compactCalls++
	return msgs, false, errors.New("summarizer returned empty output")
}

func (c *latchCompactor) FallbackCompact(msgs []provider.Message) ([]provider.Message, bool) {
	c.fallbackCalls++
	return msgs, true // "changed" but same content, so compaction keeps firing each turn
}

// TestProactiveCompactionLatchesOffSummarizer is the P39.8 regression: a
// summarizer that reliably fails must be given up on after a bounded number of
// cumulative attempts, not re-tried on every one of many compaction cycles
// (the observed 42× "summarizer returned empty output"). Once latched, the
// engine compacts deterministically and stops calling Compact.
func TestProactiveCompactionLatchesOffSummarizer(t *testing.T) {
	// Eight tool-call turns then a final text turn: every tool round crosses the
	// 85% threshold, so compaction is attempted far more times than the latch
	// allows the LLM summarizer to run.
	var turns [][]provider.Event
	for i := 0; i < 8; i++ {
		// Vary the input each turn so the loop detector doesn't abort the run
		// before we've exercised enough compaction cycles to prove the latch.
		input := json.RawMessage(fmt.Sprintf(`{"msg":"hi-%d"}`, i))
		turns = append(turns, []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: fmt.Sprintf("tu-%d", i), Name: "echo", Input: input}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		})
	}
	turns = append(turns, []provider.Event{
		{Type: provider.EventTextDelta, Text: "done"},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
	})
	adapter := &scriptedAdapter{turns: turns}
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &latchCompactor{}
	eng, err := New(Options{Adapter: adapter, Tools: reg, Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}

	var latchNotice bool
	err = eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "summarizer disabled for this run") {
			latchNotice = true
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The run-start pass calls Compact once; the proactive path may call it up to
	// the give-up threshold before latching. It must NOT keep calling per turn.
	if comp.compactCalls > summarizerGiveUpThreshold+1 {
		t.Errorf("Compact called %d times; expected it to latch off at <= %d despite 8+ compaction cycles", comp.compactCalls, summarizerGiveUpThreshold+1)
	}
	if comp.fallbackCalls == 0 {
		t.Error("expected the deterministic FallbackCompact to take over after latching")
	}
	if !latchNotice {
		t.Error("expected a notice announcing the summarizer was disabled for the run")
	}
}

// sequenceCompactor logs every call it receives, so a test can assert *when*
// the deterministic fallback fired rather than only how often. Compact always
// fails; FallbackCompact reports a change without shortening anything, so the
// estimate stays over the trigger and compaction is attempted every turn.
type sequenceCompactor struct{ log []string }

func (c *sequenceCompactor) Compact(_ context.Context, _ string, msgs []provider.Message) ([]provider.Message, bool, error) {
	c.log = append(c.log, "compact")
	return msgs, false, errors.New("summarizer returned empty output")
}

func (c *sequenceCompactor) FallbackCompact(msgs []provider.Message) ([]provider.Message, bool) {
	c.log = append(c.log, "fallback")
	return msgs, true
}

// TestProactiveCompactionFallbackCounterIsConsecutiveAndResets pins the two
// halves of P28.4's counter that TestProactiveCompactionFallsBackAfterTwoFailures
// leaves unspecified — it asserts only that the fallback ran *once* in a run
// short enough that a threshold of 2 and a threshold of 3 are indistinguishable,
// and it never gets far enough for the reset to matter. Mutating either
// (`failures >= 2` → `>= 3`, or deleting `failures = 0`) leaves that test green.
//
// Asserting the exact call sequence pins three things at once:
//   - the run-start pass is not part of this bookkeeping (its failure is the
//     first "compact" and does not push the counter to the trigger),
//   - the fallback fires on the *second consecutive* proactive failure,
//   - a fired fallback resets the counter, so the failure after it is the first
//     of a new streak and must not fall back again.
func TestProactiveCompactionFallbackCounterIsConsecutiveAndResets(t *testing.T) {
	var turns [][]provider.Event
	for i := 0; i < 2; i++ {
		turns = append(turns, []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: fmt.Sprintf("tu-%d", i), Name: "echo", Input: json.RawMessage(fmt.Sprintf(`{"msg":"hi-%d"}`, i))}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		})
	}
	turns = append(turns, []provider.Event{
		{Type: provider.EventTextDelta, Text: "done"},
		{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
	})
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	comp := &sequenceCompactor{}
	eng, err := New(Options{Adapter: &scriptedAdapter{turns: turns}, Tools: reg, Compactor: comp, Model: "test", MaxTokens: 100, ContextWindowTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(context.Background(), bigConversation(), func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// entry pass, then one proactive attempt per turn: fail, fail+fallback, fail.
	want := []string{"compact", "compact", "compact", "fallback", "compact"}
	if got := strings.Join(comp.log, ","); got != strings.Join(want, ",") {
		t.Errorf("compactor call sequence = [%s], want [%s]", got, strings.Join(want, ","))
	}
}

// TestShimCatalogCountsTowardCompactionTrigger is the P53.6 half of the headroom
// estimate: the shim's tool catalog rides every request but is appended inside
// turn(), outside conv.System, so a conversation measured without it undercounts
// the real prompt by exactly what the shim adds — the wrong direction for a
// check whose job is to compact *before* a local server silently truncates.
//
// The same conversation and window are run twice; only the shim differs. Without
// it nothing crosses the trigger and no compaction happens; with it, it does.
func TestShimCatalogCountsTowardCompactionTrigger(t *testing.T) {
	compactedWith := func(shim bool) bool {
		reg := tool.NewRegistry()
		if err := reg.Register(&echoTool{}); err != nil {
			t.Fatal(err)
		}
		adapter := &scriptedAdapter{turns: [][]provider.Event{{
			{Type: provider.EventTextDelta, Text: "ok"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		}}}
		// Declines the run-start pass (call 1) and compacts on any later call, so
		// calls > 1 means a *proactive* attempt was made — i.e. the estimate
		// crossed the trigger.
		comp := &noticeCompactor{}
		eng, err := New(Options{Adapter: adapter, Tools: reg, Compactor: comp, Model: "test",
			MaxTokens: 100, ContextWindowTokens: 400, ToolCallShim: shim})
		if err != nil {
			t.Fatal(err)
		}
		conv := &Conversation{System: "sys"}
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
			provider.TextBlock{Text: strings.Repeat("filler words here ", 30)},
		}})
		if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return comp.calls > 1
	}

	if compactedWith(false) {
		t.Fatal("conversation crossed the trigger without the shim — the fixture no longer isolates the catalog's contribution")
	}
	if !compactedWith(true) {
		t.Error("the shim's tool catalog did not count toward the compaction trigger")
	}
}
