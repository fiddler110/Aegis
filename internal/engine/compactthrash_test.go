package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/compaction"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// This file is the P62.7 fixture: a deterministic reproduction of the
// compaction thrash measured live (qwen3:14b, 24,576-token window) where every
// turn past the engine's trigger ran a compaction that pruned a little, never
// enough to drop back under the trigger, and paid a full conv.invalidate() and
// a "compacted N→N messages" notice for it.
//
// It is a *measurement* fixture first: TestPruneYieldPerTurnMeasurement records
// the yield in characters and estimated tokens per prune and compares it to the
// gap between the estimate and the trigger, which is the number the P62.7 fix
// keys on. The behavioural tests below assert the sequence of turns at which a
// compaction is applied, not merely how many there were (P63.9: a count cannot
// tell *when* something fired).

const (
	thrashWindow    = 24_576
	thrashMaxTokens = 8_192
)

// thrashConv builds a conversation that sits just under the engine's compaction
// trigger and grows by one agent turn per step, in the shape a tool-heavy run
// actually has: an assistant message with prose plus a repeated `grep` call, and
// a user message carrying that grep's dump. Repeating the *identical* search is
// what makes the older dumps prunable (pruneStaleToolResults only prunes a
// search result actually superseded by an identical later call), so each step
// leaves the pre-pass a little — and only a little — to free.
type thrashFixture struct {
	conv *Conversation
	step int
}

// newThrashFixture warms a conversation up to just under trigger using searches
// that are all *distinct* — nothing in the warm-up is prunable, so the first
// prune of the measured phase does not get handed a backlog that no steady-state
// turn could ever produce. Every turn after that repeats one identical search,
// which is what makes exactly one older dump stale per turn.
func newThrashFixture(trigger int) *thrashFixture {
	f := &thrashFixture{conv: &Conversation{System: "you are a helpful agent"}}
	f.conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: strings.Repeat("investigate the failing build carefully ", 30)},
	}})
	for f.conv.estimatedTokens() < trigger-thrashTurnTokens {
		f.grow(true)
	}
	return f
}

// thrashTurnTokens is roughly what one grow() adds, used to stop the warm-up
// just short of the trigger so the measured phase starts on the crossing turn.
const thrashTurnTokens = 330

// grow appends one agent turn's worth of messages: assistant prose the pre-pass
// can never touch, a search, and a dump just over staleSearchDumpThreshold so it
// is prunable-but-small once superseded. unique makes the search distinct, so
// no earlier dump becomes stale.
func (f *thrashFixture) grow(unique bool) {
	f.step++
	id := fmt.Sprintf("tu_%d", f.step)
	pattern := "handler"
	if unique {
		pattern = fmt.Sprintf("symbol_%d", f.step)
	}
	f.conv.Append(provider.Message{Role: provider.RoleAssistant, Content: []provider.Block{
		provider.TextBlock{Text: strings.Repeat("reasoning about the next step in this investigation ", 14)},
		provider.ToolUseBlock{ID: id, Name: "grep", Input: json.RawMessage(
			fmt.Sprintf(`{"pattern":%q,"path":"."}`, pattern))},
	}})
	f.conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.ToolResultBlock{ToolUseID: id, Content: strings.Repeat(
			fmt.Sprintf("internal/server/handler_%02d.go:41: func handle() {}\n", f.step%7), 11)},
	}})
}

// realSummarizer is the production Compactor, tuned the way the live fixture
// tuned it: a 24,576-token window and the default keepRecent, so the
// summarizer's own gate (80% of the window) sits far above the engine's trigger
// and only the deterministic pre-pass ever runs — which is precisely the regime
// P62.7 was measured in.
func realSummarizer() *compaction.Summarizer {
	return compaction.New(compaction.Options{
		Adapter:       summaryAdapter{},
		Model:         "test",
		ContextWindow: thrashWindow,
	})
}

func newGuardFor(t *testing.T, comp Compactor) *compactionGuard {
	t.Helper()
	eng, err := New(Options{
		Adapter:             &scriptedAdapter{},
		Tools:               tool.NewRegistry(),
		Compactor:           comp,
		Model:               "test",
		MaxTokens:           thrashMaxTokens,
		ContextWindowTokens: thrashWindow,
	})
	if err != nil {
		t.Fatal(err)
	}
	return eng.newCompactionGuard()
}

func totalChars(system string, msgs []provider.Message) int {
	n := len(system)
	for _, m := range msgs {
		for _, blk := range m.Content {
			switch v := blk.(type) {
			case provider.TextBlock:
				n += len(v.Text)
			case provider.ToolUseBlock:
				n += len(v.Input)
			case provider.ToolResultBlock:
				n += len(v.Content)
			}
		}
	}
	return n
}

// yieldRecorder wraps a Compactor and measures, from the outside, what each
// Compact call actually freed. It is how the yield was measured *before* the
// P62.7 seam widening made the compactor report it itself, and it stays as the
// independent check on that report.
type yieldRecorder struct {
	inner  Compactor
	calls  int
	chars  []int // characters freed, per call
	tokens []int // estimated tokens freed, per call
}

func (y *yieldRecorder) Compact(ctx context.Context, system string, msgs []provider.Message) ([]provider.Message, bool, error) {
	y.calls++
	beforeChars := totalChars(system, msgs)
	beforeTokens := tokenest.Messages(system, msgs)
	out, changed, err := y.inner.Compact(ctx, system, msgs)
	y.chars = append(y.chars, beforeChars-totalChars(system, out))
	y.tokens = append(y.tokens, beforeTokens-tokenest.Messages(system, out))
	return out, changed, err
}

// TestPruneYieldPerTurnMeasurement is step 1 of P62.7: record what a prune
// actually yields per turn, in bytes and estimated tokens, against the gap
// between the estimate and the trigger. The roadmap inferred the yield from
// message counts; this measures it.
//
// It asserts only the premise the fix is built on — that past the trigger every
// turn compacts, and that the yield is small next to the gap it would have to
// close — so that if the premise ever stops holding, the test says so rather
// than the fix silently becoming pointless.
func TestPruneYieldPerTurnMeasurement(t *testing.T) {
	rec := &yieldRecorder{inner: realSummarizer()}
	g := newGuardFor(t, rec)
	// yieldRecorder deliberately implements *only* Compact, not the P62.7
	// yield-reporting seam, so this guard cannot apply the minimum-yield check
	// and behaves exactly as the engine did before the fix. That is both how the
	// measurement stays a measurement of the old behaviour and the regression
	// test for the compatibility promise the optional interface makes.

	trigger := compactionTrigger(thrashWindow, thrashMaxTokens)
	f := newThrashFixture(trigger)

	var applied, lowYield int
	var lines []string
	for turn := 1; turn <= 20; turn++ {
		f.grow(false)
		before := len(f.conv.Messages)
		est := g.estimate(f.conv)
		gap := est - trigger

		callsBefore := rec.calls
		var notice string
		g.beforeTurn(context.Background(), f.conv, func(ev Event) {
			if ev.Kind == KindNotice {
				notice = ev.Text
			}
		}, false)
		if rec.calls == callsBefore {
			continue // under the trigger: no attempt
		}
		yieldChars := rec.chars[len(rec.chars)-1]
		yieldTokens := rec.tokens[len(rec.tokens)-1]
		if notice != "" {
			applied++
		}
		if float64(yieldTokens) < 0.5*float64(gap) {
			lowYield++
		}
		lines = append(lines, fmt.Sprintf(
			"turn %2d: est=%5d trigger=%5d gap=%4d msgs %d→%d yield=%5d chars / %4d tok (%.2f×gap) notice=%v",
			turn, est, trigger, gap, before, len(f.conv.Messages), yieldChars, yieldTokens,
			float64(yieldTokens)/float64(max(gap, 1)), notice != ""))
	}

	t.Log("P62.7 prune-yield measurement (pre-fix behaviour):\n" + strings.Join(lines, "\n"))
	if len(lines) < 5 {
		t.Fatalf("fixture never sustained compaction past the trigger (%d attempts) — it no longer reproduces P62.7", len(lines))
	}
	if applied < 5 {
		t.Fatalf("only %d of %d attempts applied a compaction; the thrash loop is not reproduced", applied, len(lines))
	}
	if lowYield*2 < len(lines) {
		t.Fatalf("only %d of %d prunes yielded less than half the gap — P62.7's premise (the yield is tiny) does not hold in this fixture", lowYield, len(lines))
	}
}
