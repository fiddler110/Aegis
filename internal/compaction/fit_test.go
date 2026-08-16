package compaction

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// P53.3: the summarization request must fit the window it exists to protect.

// recordingAdapter captures the last request it was asked to stream, so the
// fit-check tests can assert on the transcript actually sent.
type recordingAdapter struct {
	summary string
	called  int
	last    provider.Request
}

func (a *recordingAdapter) Name() string { return "recording" }

func (a *recordingAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.called++
	a.last = req
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: a.summary}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

func (a *recordingAdapter) sentText(t *testing.T) string {
	t.Helper()
	if len(a.last.Messages) != 1 || len(a.last.Messages[0].Content) != 1 {
		t.Fatalf("unexpected summarization request shape: %+v", a.last)
	}
	blk, ok := a.last.Messages[0].Content[0].(provider.TextBlock)
	if !ok {
		t.Fatalf("summarization request block is %T, want TextBlock", a.last.Messages[0].Content[0])
	}
	return blk.Text
}

func firstText(t *testing.T, m provider.Message) string {
	t.Helper()
	blk, ok := m.Content[0].(provider.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", m.Content[0])
	}
	return blk.Text
}

// TestSummarizeFitsCommonPathUntouched proves the ordinary case is unaffected:
// a prefix that comfortably fits is sent verbatim — no block truncation, no
// dropped messages, no omission note on the summary.
func TestSummarizeFitsCommonPathUntouched(t *testing.T) {
	a := &recordingAdapter{summary: "summary of earlier work"}
	s := New(Options{Adapter: a, Model: "m", ContextWindow: 32768, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "first question about the parser"),
		text(provider.RoleAssistant, "first answer about the parser"),
		text(provider.RoleUser, "second question about the lexer"),
		text(provider.RoleAssistant, "second answer about the lexer"),
		text(provider.RoleUser, "third question"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	out, changed, err := s.ForceCompact(context.Background(), "", msgs)
	if err != nil || !changed {
		t.Fatalf("ForceCompact: changed=%v err=%v", changed, err)
	}
	sent := a.sentText(t)
	for _, want := range []string{"first question about the parser", "second answer about the lexer"} {
		if !strings.Contains(sent, want) {
			t.Errorf("transcript missing %q; got %q", want, sent)
		}
	}
	if strings.Contains(sent, "truncated by compaction") {
		t.Errorf("a fitting transcript must not be truncated: %q", sent)
	}
	if got := firstText(t, out[0]); strings.Contains(got, "Compaction note") {
		t.Errorf("a fitting transcript must not report omissions: %q", got)
	}
}

// TestSummarizeTruncatesOversizedBlock is the P53.3 core: a single very large
// block must be truncated in place (with a visible marker) rather than pushing
// the summarization request past the window, and without dropping the small
// messages around it.
func TestSummarizeTruncatesOversizedBlock(t *testing.T) {
	a := &recordingAdapter{summary: "summary"}
	s := New(Options{Adapter: a, Model: "m", ContextWindow: 8192, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "please read the giant file"),
		text(provider.RoleAssistant, "HEAD"+strings.Repeat("x", 60000)+"TAIL"),
		text(provider.RoleUser, "thanks"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	out, changed, err := s.ForceCompact(context.Background(), "", msgs)
	if err != nil || !changed {
		t.Fatalf("ForceCompact: changed=%v err=%v", changed, err)
	}
	sent := a.sentText(t)
	if !strings.Contains(sent, "truncated by compaction") {
		t.Errorf("oversized block was not marked as truncated: %.200q", sent)
	}
	if n, budget := summarizeRequestTokens(sent, ""), s.summarizeFitBudget(); n > budget {
		t.Errorf("request still over budget: %d > %d", n, budget)
	}
	// Truncation is middle-out, so both ends of the block survive, and the
	// small surrounding message is untouched.
	if !strings.Contains(sent, "HEAD") || !strings.Contains(sent, "TAIL") {
		t.Errorf("middle-out truncation should keep head and tail: %.200q", sent)
	}
	if !strings.Contains(sent, "please read the giant file") {
		t.Error("small sibling message should not have been dropped")
	}
	if got := firstText(t, out[0]); strings.Contains(got, "Compaction note") {
		t.Errorf("truncation alone must not claim messages were omitted: %q", got)
	}
}

// TestSummarizeDropsOldestWhenTruncationInsufficient: when every message is
// oversized, truncation alone cannot get under budget, so the oldest messages
// are dropped — and the summary must say so, because Compact replaces the whole
// prefix and dropped content is gone with no other record.
func TestSummarizeDropsOldestWhenTruncationInsufficient(t *testing.T) {
	a := &recordingAdapter{summary: "summary"}
	s := New(Options{Adapter: a, Model: "m", ContextWindow: 2048, KeepRecent: 2})
	var msgs []provider.Message
	for i := 0; i < 20; i++ {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		msgs = append(msgs, text(role, "MARK"+strconv.Itoa(i)+" "+strings.Repeat("y", 2000)))
	}
	msgs = append(msgs, text(provider.RoleUser, "last question"), text(provider.RoleAssistant, "final reply kept"))

	out, changed, err := s.ForceCompact(context.Background(), "", msgs)
	if err != nil || !changed {
		t.Fatalf("ForceCompact: changed=%v err=%v", changed, err)
	}
	sent := a.sentText(t)
	if n, budget := summarizeRequestTokens(sent, ""), s.summarizeFitBudget(); n > budget {
		t.Errorf("request still over budget: %d > %d", n, budget)
	}
	if !strings.Contains(sent, "truncated by compaction") {
		t.Error("blocks should still have been truncated before dropping")
	}
	if strings.Contains(sent, "MARK0 ") {
		t.Error("oldest message should have been dropped")
	}
	if got := firstText(t, out[0]); !strings.Contains(got, "earliest message(s) of this span were omitted") {
		t.Errorf("summary must disclose dropped messages, got %q", got)
	}
	// The verbatim tail is untouched by any of this.
	if got := firstText(t, out[len(out)-1]); got != "final reply kept" {
		t.Errorf("recent message not preserved: %q", got)
	}
}

// TestSummarizeNoBudgetSkipsFitCheck: with no meaningful budget (a fixed
// trigger budget smaller than the reserved summary output is not a context
// window), there is nothing to fit inside, so the transcript goes out whole
// rather than being shrunk against an invented bound.
func TestSummarizeNoBudgetSkipsFitCheck(t *testing.T) {
	a := &recordingAdapter{summary: "summary"}
	s := New(Options{Adapter: a, Model: "m", MaxBudget: 5, KeepRecent: 2})
	if got := s.summarizeFitBudget(); got != 0 {
		t.Fatalf("summarizeFitBudget = %d, want 0 (skip)", got)
	}
	msgs := []provider.Message{
		text(provider.RoleUser, strings.Repeat("z", 50000)),
		text(provider.RoleAssistant, "ok"),
		text(provider.RoleUser, "more"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	if _, changed, err := s.Compact(context.Background(), "", msgs); err != nil || !changed {
		t.Fatalf("Compact: changed=%v err=%v", changed, err)
	}
	if sent := a.sentText(t); strings.Contains(sent, "truncated by compaction") {
		t.Error("no budget known: transcript must not be shrunk")
	}
}

// TestSummarizeUnfittableBudgetIsNonFatal: when the reserve plus the reserved
// output exceed the whole window, no request can fit — the summarizer must
// report an error (which the engine logs and continues past) and must never
// issue the oversized request anyway.
func TestSummarizeUnfittableBudgetIsNonFatal(t *testing.T) {
	a := &recordingAdapter{summary: "summary"}
	s := New(Options{Adapter: a, Model: "m", ContextWindow: 1100, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "one"),
		text(provider.RoleAssistant, "two"),
		text(provider.RoleUser, "three"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	out, _, err := s.ForceCompact(context.Background(), "", msgs)
	if err == nil {
		t.Fatal("expected an error when no request can fit the budget")
	}
	if a.called != 0 {
		t.Errorf("a known-oversized request was issued anyway (called=%d)", a.called)
	}
	if len(out) != len(msgs) {
		t.Errorf("messages should be returned unchanged on failure, got %d", len(out))
	}
}
