package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// P67.6 engine-side tests. They cover the *when* — the idle gap, the purpose
// gate, the once-per-run latch — and stub the *what*, because which results are
// disposable is internal/compaction's decision and is tested there.

// coldStub is a Compactor implementing ColdCacheCompactor and nothing else. It
// records every call so a test can assert the pass was not merely harmless but
// never invoked, which is the difference between "the gate held" and "the gate
// ran and found nothing".
type coldStub struct {
	calls   int
	cleared int
	freed   int
}

func (c *coldStub) Compact(context.Context, string, []provider.Message) ([]provider.Message, bool, error) {
	return nil, false, nil
}

func (c *coldStub) ClearColdToolResults(msgs []provider.Message) ([]provider.Message, int, int) {
	c.calls++
	if c.cleared == 0 {
		return msgs, 0, 0
	}
	out := append([]provider.Message(nil), msgs...)
	out = append(out, provider.Message{Role: provider.RoleUser,
		Content: []provider.Block{provider.TextBlock{Text: "cleared"}}})
	return out, c.cleared, c.freed
}

// coldGuard builds a guard whose clock is fixed and whose conversation was last
// active `idle` ago. after is the configured threshold; purpose is the run's.
func coldGuard(t *testing.T, comp Compactor, after, idle time.Duration, purpose provider.Purpose) *compactionGuard {
	t.Helper()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	eng, err := New(Options{
		Adapter:             &scriptedAdapter{},
		Tools:               tool.NewRegistry(),
		Compactor:           comp,
		Model:               "test",
		MaxTokens:           thrashMaxTokens,
		ContextWindowTokens: thrashWindow,
		Purpose:             purpose,
		ColdCacheAfter:      after,
		LastActivityAt:      now.Add(-idle),
	})
	if err != nil {
		t.Fatal(err)
	}
	g := eng.newCompactionGuard()
	g.now = func() time.Time { return now }
	return g
}

// coldTurn runs one guard turn over a trivial conversation and reports whether
// the cold-cache notice was emitted.
func coldTurn(t *testing.T, g *compactionGuard) (noticed bool, conv *Conversation) {
	t.Helper()
	conv = &Conversation{System: "sys", Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hello"}}},
	}}
	g.beforeTurn(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice && strings.Contains(ev.Text, "resumed after") {
			noticed = true
		}
	}, false)
	return noticed, conv
}

// TestColdCacheFiresPastTheIdleGap is the happy path: an idle gap longer than
// the threshold rewrites the conversation and says so.
func TestColdCacheFiresPastTheIdleGap(t *testing.T) {
	c := &coldStub{cleared: 4, freed: 900}
	g := coldGuard(t, c, 20*time.Minute, 2*time.Hour, provider.PurposeForeground)
	noticed, conv := coldTurn(t, g)
	if c.calls != 1 {
		t.Fatalf("ClearColdToolResults called %d times, want 1", c.calls)
	}
	if !noticed {
		t.Error("no cold-cache notice was emitted")
	}
	if len(conv.Messages) != 2 {
		t.Errorf("conversation not rewritten: %d messages", len(conv.Messages))
	}
	ev := g.event()
	if ev == nil || ev.ColdCleared != 4 || ev.ColdFreedTokens != 900 {
		t.Errorf("trace = %+v, want ColdCleared=4 ColdFreedTokens=900", ev)
	}
}

// TestColdCacheStaysOffInsideTheGap pins the threshold itself: a conversation
// idle for less than the configured gap has a warm prefix and must be left
// alone.
func TestColdCacheStaysOffInsideTheGap(t *testing.T) {
	c := &coldStub{cleared: 4}
	g := coldGuard(t, c, 20*time.Minute, 5*time.Minute, provider.PurposeForeground)
	if noticed, _ := coldTurn(t, g); noticed {
		t.Error("the pass fired inside the idle threshold")
	}
	if c.calls != 0 {
		t.Fatalf("ClearColdToolResults called %d times, want 0", c.calls)
	}
}

// TestColdCacheOffByConfiguration pins that 0 disables it outright, without so
// much as reading the clock.
func TestColdCacheOffByConfiguration(t *testing.T) {
	c := &coldStub{cleared: 4}
	g := coldGuard(t, c, 0, 48*time.Hour, provider.PurposeForeground)
	if noticed, _ := coldTurn(t, g); noticed {
		t.Error("the pass fired with ColdCacheAfter=0")
	}
	if c.calls != 0 {
		t.Fatalf("ClearColdToolResults called %d times, want 0", c.calls)
	}
}

// TestColdCacheNeedsAKnownLastActivity is the zero-value rule: an unset
// LastActivityAt means "not known", not "idle since the epoch". A caller with no
// session record must not have its first turn shredded.
func TestColdCacheNeedsAKnownLastActivity(t *testing.T) {
	c := &coldStub{cleared: 4}
	g := coldGuard(t, c, 20*time.Minute, 0, provider.PurposeForeground)
	g.lastActivity = time.Time{}
	if noticed, _ := coldTurn(t, g); noticed {
		t.Error("the pass fired on an unknown last-activity time")
	}
	if c.calls != 0 {
		t.Fatalf("ClearColdToolResults called %d times, want 0", c.calls)
	}
}

// TestColdCacheGatesOnCallPurpose is P67.6's second named constraint and the
// reason the item was sequenced behind P67.3. An analysis-only caller must be
// able to run a conversation through the engine without mutating it as a side
// effect; a conversation-owning one must not be blocked.
func TestColdCacheGatesOnCallPurpose(t *testing.T) {
	cases := []struct {
		purpose provider.Purpose
		want    bool
	}{
		{provider.PurposeForeground, true},
		{provider.PurposeSubAgent, true},
		{provider.PurposeDebate, true},
		{provider.PurposeUnspecified, true},
		{provider.PurposeGuard, false},
		{provider.PurposeCompaction, false},
		{provider.PurposeProbe, false},
		{provider.PurposeTitle, false},
		{provider.PurposeSampling, false},
	}
	for _, tc := range cases {
		c := &coldStub{cleared: 2}
		g := coldGuard(t, c, 20*time.Minute, 2*time.Hour, tc.purpose)
		coldTurn(t, g)
		if got := c.calls > 0; got != tc.want {
			t.Errorf("purpose %q: pass ran = %v, want %v", tc.purpose, got, tc.want)
		}
	}
}

// TestColdCacheHonorsAContextDeclaredPurpose covers the launcher path:
// provider.WithPurpose on the context, with no Purpose on the engine. It mirrors
// provider.EffectivePurpose's precedence, which is the only reason the run-scoped
// default exists.
func TestColdCacheHonorsAContextDeclaredPurpose(t *testing.T) {
	c := &coldStub{cleared: 2}
	g := coldGuard(t, c, 20*time.Minute, 2*time.Hour, provider.PurposeUnspecified)
	conv := &Conversation{System: "sys", Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	ctx := provider.WithPurpose(context.Background(), provider.PurposeGuard)
	g.beforeTurn(ctx, conv, func(Event) {}, false)
	if c.calls != 0 {
		t.Fatalf("the pass ran under a context-declared guard purpose (%d calls)", c.calls)
	}
}

// TestColdCacheRunsOncePerRun is the anti-thrash latch. The condition the pass
// detects — a gap between the previous assistant message and this one — is true
// exactly once. Without the latch, a run whose stub keeps reporting work would
// rewrite the conversation on every turn, which is the thrash P62.7 exists to
// stop.
func TestColdCacheRunsOncePerRun(t *testing.T) {
	c := &coldStub{cleared: 3, freed: 500}
	g := coldGuard(t, c, 20*time.Minute, 2*time.Hour, provider.PurposeForeground)
	for turn := 1; turn <= 4; turn++ {
		coldTurn(t, g)
	}
	if c.calls != 1 {
		t.Fatalf("ClearColdToolResults called %d times over 4 turns, want 1", c.calls)
	}
}

// TestColdCacheLatchesEvenWhenItFindsNothing pins that the latch is set before
// the call rather than after it. A conversation with nothing to clear has still
// had its idle gap accounted for, and asking again next turn would put the cost
// of the scan on every turn of the run.
func TestColdCacheLatchesEvenWhenItFindsNothing(t *testing.T) {
	c := &coldStub{} // cleared: 0
	g := coldGuard(t, c, 20*time.Minute, 2*time.Hour, provider.PurposeForeground)
	coldTurn(t, g)
	coldTurn(t, g)
	if c.calls != 1 {
		t.Fatalf("ClearColdToolResults called %d times, want 1", c.calls)
	}
}

// TestColdCacheIsInertWithoutTheCapability keeps the optional-interface promise
// this file's other four seams make: a Compactor that implements only Compact
// behaves exactly as it did before P67.6.
func TestColdCacheIsInertWithoutTheCapability(t *testing.T) {
	g := coldGuard(t, plainCompactor{}, 20*time.Minute, 2*time.Hour, provider.PurposeForeground)
	noticed, conv := coldTurn(t, g)
	if noticed {
		t.Error("a cold-cache notice was emitted by a Compactor that cannot clear")
	}
	if len(conv.Messages) != 1 {
		t.Errorf("conversation was rewritten: %d messages", len(conv.Messages))
	}
}

// plainCompactor implements Compactor and none of the optional capabilities.
type plainCompactor struct{}

func (plainCompactor) Compact(context.Context, string, []provider.Message) ([]provider.Message, bool, error) {
	return nil, false, nil
}

// TestAfterTurnRefreshesLastActivity pins the intra-run half of the trigger: a
// completed turn makes the prefix warm again, and it does so whether or not the
// turn was admissible as a calibration sample — the two are unrelated questions.
func TestAfterTurnRefreshesLastActivity(t *testing.T) {
	g := coldGuard(t, &coldStub{}, 20*time.Minute, 2*time.Hour, provider.PurposeForeground)
	g.afterTurn(nil, 0) // nil usage: the calibration path returns immediately
	if !g.lastActivity.Equal(g.now()) {
		t.Fatalf("lastActivity = %v, want the current clock %v", g.lastActivity, g.now())
	}
}
