package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/persona"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool"
)

// toggledAdapter is a fake provider.Adapter whose Stream can be switched to
// fail on demand, standing in for a primary target an outage has taken down.
type toggledAdapter struct {
	name string
	mu   sync.Mutex
	fail bool
}

func (a *toggledAdapter) Name() string { return a.name }

func (a *toggledAdapter) setFail(v bool) {
	a.mu.Lock()
	a.fail = v
	a.mu.Unlock()
}

func (a *toggledAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.mu.Lock()
	fail := a.fail
	a.mu.Unlock()
	if fail {
		return nil, provider.NewHTTPError(a.name, 500, "", "down")
	}
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: "ok"}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{}}
	close(ch)
	return ch, nil
}

// TestNewEngine_CompactionTriggerFollowsActiveFailover is the LLM-11 second-
// half regression: once a fallback is actively serving requests, the next
// turn's compaction trigger must be sized against the fallback's own window,
// not the primary's — the primary's window is what continuing to use it
// would silently under- or over-compact against. It must also revert the
// instant the primary serves a call again, since a stale reading would
// mis-size a turn the primary is actually about to handle.
func TestNewEngine_CompactionTriggerFollowsActiveFailover(t *testing.T) {
	ts := fakeOllamaMultiModel(t, map[string]int{"primary-model": 16384, "fallback-model": 32768}, nil)
	s := ctxWinServer(0, "ollama", ts.URL)
	s.cfg.Provider.Model = "primary-model"
	s.tools = tool.NewRegistry()

	primary := &toggledAdapter{name: "primary"}
	fallback := &toggledAdapter{name: "fallback"}
	s.adapter = provider.WithFailover(primary, []provider.FallbackTarget{{Adapter: fallback, Model: "fallback-model"}}, s.logger)
	s.initContextWindow(context.Background())

	// Baseline: primary healthy, no failover has happened yet.
	eng, _, err := s.newEngine("sess", "build", permission.AutoApprove{}, nil, persona.Persona{}, false, nil, nil, "", t.TempDir(), "hi", nil, time.Time{})
	if err != nil {
		t.Fatalf("newEngine (baseline): %v", err)
	}
	if got := eng.EffectiveContextWindow(); got != 16384 {
		t.Fatalf("baseline compaction window = %d, want 16384 (primary's own)", got)
	}

	// Force the primary down and actually run one turn, so failoverAdapter's
	// own state (not a test double) records the switch to the fallback.
	primary.setFail(true)
	conv := &engine.Conversation{}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}})
	if err := eng.Run(context.Background(), conv, func(engine.Event) {}); err != nil {
		t.Fatalf("Run (forcing failover): %v", err)
	}
	if model, active := provider.ActiveFailoverModel(s.adapter); !active || model != "fallback-model" {
		t.Fatalf("setup: expected the fallback to be active after the forced failure, got (%q, %v)", model, active)
	}

	// Next turn: the primary is (as far as this test can tell) still down, so
	// the compaction trigger must follow the fallback's own, larger window.
	eng2, _, err := s.newEngine("sess", "build", permission.AutoApprove{}, nil, persona.Persona{}, false, nil, nil, "", t.TempDir(), "hi again", nil, time.Time{})
	if err != nil {
		t.Fatalf("newEngine (during failover): %v", err)
	}
	if got := eng2.EffectiveContextWindow(); got != 32768 {
		t.Errorf("compaction window while failed over = %d, want 32768 (the active fallback's window)", got)
	}

	// The primary recovers and serves the very next call: the stale failover
	// reading must not linger and mis-size a turn the primary is handling.
	primary.setFail(false)
	conv2 := &engine.Conversation{}
	conv2.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}})
	if err := eng2.Run(context.Background(), conv2, func(engine.Event) {}); err != nil {
		t.Fatalf("Run (primary recovers): %v", err)
	}
	eng3, _, err := s.newEngine("sess", "build", permission.AutoApprove{}, nil, persona.Persona{}, false, nil, nil, "", t.TempDir(), "hi once more", nil, time.Time{})
	if err != nil {
		t.Fatalf("newEngine (after recovery): %v", err)
	}
	if got := eng3.EffectiveContextWindow(); got != 16384 {
		t.Errorf("compaction window after the primary recovers = %d, want 16384 (back to the primary's own)", got)
	}
}
