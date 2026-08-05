package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// endTurnAdapter answers one turn and stops.
func endTurnAdapter() *scriptedAdapter {
	return &scriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "ok"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}
}

// TestContextWindowFloorSuppressesCompaction is the P59.7 regression: a
// conversation that would trip the 85% trigger against the window captured at
// New must NOT compact once the adapter reports an escalation that gave it
// room. Before this, ContextWindowTokens was read once and never re-read, so
// the engine kept summarizing — minutes of local-model calls — during the very
// overflow recovery that raised the window.
func TestContextWindowFloorSuppressesCompaction(t *testing.T) {
	comp := &noticeCompactor{}
	eng, err := New(Options{
		Adapter: endTurnAdapter(), Tools: tool.NewRegistry(), Compactor: comp,
		Model: "test", MaxTokens: 100, ContextWindowTokens: 100,
		ContextWindowFloor: func() int { return 100_000 },
	})
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	if err := eng.Run(context.Background(), bigConversation(), func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, n := range notices {
		if strings.Contains(n, "compacted") || strings.Contains(n, "context ~") {
			t.Errorf("compaction fired against the pre-escalation window despite a floor of 100000: %q", n)
		}
	}
}

// TestContextWindowFloorBelowConfiguredIsIgnored: the floor is a floor, not an
// override. A zero (no escalation happened, or the backend can't escalate) or a
// smaller reported value must leave the constructed window in force — otherwise
// every non-escalating backend would silently lose proactive compaction.
func TestContextWindowFloorBelowConfiguredIsIgnored(t *testing.T) {
	for name, floor := range map[string]func() int{
		"no escalation yet":       func() int { return 0 },
		"smaller than configured": func() int { return 10 },
	} {
		t.Run(name, func(t *testing.T) {
			comp := &noticeCompactor{}
			eng, err := New(Options{
				Adapter: endTurnAdapter(), Tools: tool.NewRegistry(), Compactor: comp,
				Model: "test", MaxTokens: 100, ContextWindowTokens: 100,
				ContextWindowFloor: floor,
			})
			if err != nil {
				t.Fatal(err)
			}
			var compacted bool
			if err := eng.Run(context.Background(), bigConversation(), func(ev Event) {
				if ev.Kind == KindNotice && strings.Contains(ev.Text, "compacted") {
					compacted = true
				}
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !compacted {
				t.Error("no compaction: a floor at or below the configured window must not disable the trigger")
			}
		})
	}
}
