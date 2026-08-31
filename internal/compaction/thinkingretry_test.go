package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// thinkingAdapter models a reasoning model whose extended-thinking preamble
// eats the whole completion budget: the first call streams thinking and no
// content, and only a call that explicitly asks for thinking off produces a
// summary. That is not a hypothetical — it is what
// aegis-qwen35-9b:32k did on every compaction cycle of the first live_workflow
// run (C2): num_predict 1024, done_reason "length", zero content bytes.
type thinkingAdapter struct {
	summary string
	reqs    []provider.Request
}

func (a *thinkingAdapter) Name() string { return "thinking" }

func (a *thinkingAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.reqs = append(a.reqs, req)
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventThinkingDelta, Text: "let me think about this at length"}
	if req.SuppressThinking {
		ch <- provider.Event{Type: provider.EventTextDelta, Text: a.summary}
	}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

// mutePreambleAdapter emits neither thinking nor content: a model that simply
// has nothing to say. The retry must not fire for it — a second call would
// spend another round trip to reach the same fallback.
type mutePreambleAdapter struct{ calls int }

func (a *mutePreambleAdapter) Name() string { return "mute" }

func (a *mutePreambleAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	a.calls++
	ch := make(chan provider.Event, 1)
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

func compactionFixture() []provider.Message {
	return []provider.Message{
		text(provider.RoleUser, "msg one is fairly long here"),
		text(provider.RoleAssistant, "reply one is also long"),
		text(provider.RoleUser, "msg two continues"),
		text(provider.RoleAssistant, "reply two continues"),
		text(provider.RoleUser, "msg three"),
		text(provider.RoleAssistant, "final reply kept"),
	}
}

// TestSummarizerRetriesWithoutThinkingWhenThePreambleAteTheBudget is the P79.3
// regression test. Compaction is deliberately not in
// SuppressesExtendedThinking — a summary is a long unstructured reply that
// thinking helps — so the fix is not to forbid the preamble but to notice when
// it consumed the whole budget and ask once more without it.
func TestSummarizerRetriesWithoutThinkingWhenThePreambleAteTheBudget(t *testing.T) {
	a := &thinkingAdapter{summary: "earlier we set up the project"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})

	out, changed, err := s.Compact(context.Background(), "", compactionFixture())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !changed {
		t.Fatal("no compaction happened; the retry never produced a summary")
	}
	if len(a.reqs) != 2 {
		t.Fatalf("adapter calls = %d, want 2 (first with thinking, then the retry without)", len(a.reqs))
	}
	if a.reqs[0].SuppressThinking {
		t.Error("the first attempt suppressed thinking; it must be the model's default, so summaries keep the preamble when it fits")
	}
	if !a.reqs[1].SuppressThinking {
		t.Error("the retry did not set SuppressThinking, so it would fail exactly as the first attempt did")
	}
	// The retry must be the same request otherwise: a summary of a different
	// transcript is not the summary the fit check was priced against.
	if a.reqs[0].MaxTokens != a.reqs[1].MaxTokens || a.reqs[0].System != a.reqs[1].System {
		t.Error("the retry changed the request beyond SuppressThinking")
	}
	if !strings.Contains(joinText(out), "earlier we set up the project") {
		t.Errorf("compacted conversation does not carry the retry's summary: %q", joinText(out))
	}
}

// TestSummarizerDoesNotRetryAModelThatSaidNothingAtAll pins the other half:
// the retry is for a budget that went to thinking, not for an empty reply in
// general. A model that emits no preamble either gets one call and the
// existing empty-output error, which is what latches the LLM summarizer off
// (P39.8) instead of doubling every compaction's cost.
func TestSummarizerDoesNotRetryAModelThatSaidNothingAtAll(t *testing.T) {
	a := &mutePreambleAdapter{}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})

	if _, _, err := s.Compact(context.Background(), "", compactionFixture()); err == nil {
		t.Fatal("Compact: want the empty-output error, got nil")
	}
	if a.calls != 1 {
		t.Errorf("adapter calls = %d, want 1 — a model that never thought has no budget problem to retry around", a.calls)
	}
}

func joinText(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		for _, blk := range m.Content {
			if tb, ok := blk.(provider.TextBlock); ok {
				b.WriteString(tb.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
