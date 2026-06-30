package engine

import (
	"context"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
)

// scriptAdapter returns one text response per Stream call, in order.
type scriptAdapter struct {
	replies []string
	i       int
}

func (a *scriptAdapter) Name() string { return "script" }
func (a *scriptAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	text := "done"
	if a.i < len(a.replies) {
		text = a.replies[a.i]
	}
	a.i++
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}}
	close(ch)
	return ch, nil
}

func runWith(t *testing.T, opts Options) []Event {
	t.Helper()
	eng, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	var got []Event
	if err := eng.Run(context.Background(), conv, func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func TestGuardPassEmitsDoneOnly(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"final"}}, Model: "m",
		OutputGuard:           func(context.Context, string) (bool, string) { return true, "" },
		OutputGuardMaxRetries: 2,
	})
	for _, k := range kinds(evs) {
		if k == KindGuard {
			t.Error("passing guard should emit no KindGuard event")
		}
	}
}

func TestGuardFailThenPass(t *testing.T) {
	calls := 0
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"bad", "good"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(_ context.Context, text string) (bool, string) {
			calls++
			if text == "good" {
				return true, ""
			}
			return false, "needs work"
		},
	})
	var guardEvents int
	for _, e := range evs {
		if e.Kind == KindGuard {
			guardEvents++
			if e.GuardReason != "needs work" {
				t.Errorf("guard reason = %q", e.GuardReason)
			}
		}
	}
	if guardEvents != 1 {
		t.Errorf("expected 1 KindGuard event, got %d", guardEvents)
	}
	if calls != 2 {
		t.Errorf("expected guard called twice, got %d", calls)
	}
}

func TestGuardExhaustedSurfaces(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"a", "b", "c", "d"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard:           func(context.Context, string) (bool, string) { return false, "always bad" },
	})
	var guardEvents, doneEvents int
	for _, e := range evs {
		switch e.Kind {
		case KindGuard:
			guardEvents++
		case KindDone:
			doneEvents++
		}
	}
	// 2 retries => 2 failure events on retries + 1 final exhausted event = 3.
	if guardEvents != 3 {
		t.Errorf("expected 3 KindGuard events, got %d", guardEvents)
	}
	if doneEvents != 1 {
		t.Errorf("expected exactly 1 KindDone, got %d", doneEvents)
	}
}
