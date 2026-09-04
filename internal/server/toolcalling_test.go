package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/toolcallprobe"
)

// probeAdapter scripts the tool-calling probe's outcome and counts how many
// times it was actually asked.
type probeAdapter struct {
	calls    atomic.Int32
	numCtx   atomic.Int32 // NumCtx of the most recent request (P66.20/LLM-10)
	toolCall bool         // emit a structured tool call
	streamEr error        // fail at Stream()
	eventErr error        // fail mid-stream
	gate     chan struct{}
}

func (a *probeAdapter) Name() string { return "probe" }

func (a *probeAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.calls.Add(1)
	a.numCtx.Store(int32(req.NumCtx))
	if a.streamEr != nil {
		return nil, a.streamEr
	}
	ch := make(chan provider.Event, 3)
	go func() {
		defer close(ch)
		if a.gate != nil {
			<-a.gate // hold the probe open so concurrent callers pile up
		}
		if a.eventErr != nil {
			ch <- provider.Event{Type: provider.EventError, Err: a.eventErr}
			return
		}
		if a.toolCall {
			ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu_1", Name: "list_files", Input: json.RawMessage(`{"path":"."}`),
			}}
		} else {
			ch <- provider.Event{Type: provider.EventTextDelta, Text: `I would call {"name":"list_files","arguments":{"path":"."}}`}
		}
		ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	}()
	return ch, nil
}

func ollamaServer(t *testing.T, a provider.Adapter) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Provider.Default = "ollama"
	cfg.Provider.Model = "test-model"
	return &Server{cfg: cfg, adapter: a, toolCalling: toolcallprobe.NewGate()}
}

func TestToolCallingWarningFlagsModelWithNoToolCalls(t *testing.T) {
	a := &probeAdapter{toolCall: false}
	s := ollamaServer(t, a)

	warn := s.toolCallingWarning(context.Background(), "sess-1", "bad-model")
	if !strings.Contains(warn, "bad-model") || !strings.Contains(warn, "tool") {
		t.Fatalf("expected a warning naming the model, got %q", warn)
	}
}

func TestToolCallingWarningSilentForCapableModel(t *testing.T) {
	a := &probeAdapter{toolCall: true}
	s := ollamaServer(t, a)

	if warn := s.toolCallingWarning(context.Background(), "sess-1", "good-model"); warn != "" {
		t.Fatalf("expected no warning for a tool-calling model, got %q", warn)
	}
}

// TestToolCallingWarningNeverBlamesAnOutage is the rule that matters most: a
// probe that couldn't reach a verdict must stay silent rather than tell the
// user their model can't call tools. Neither failure mode may be cached, so a
// transient outage can't poison the verdict for the daemon's lifetime.
func TestToolCallingWarningNeverBlamesAnOutage(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    *probeAdapter
	}{
		{"stream refused", &probeAdapter{streamEr: errors.New("connection refused")}},
		{"mid-stream error", &probeAdapter{eventErr: errors.New("model requires more system memory")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ollamaServer(t, tc.a)
			if warn := s.toolCallingWarning(context.Background(), "sess-1", "m"); warn != "" {
				t.Fatalf("expected silence on an inconclusive probe, got %q", warn)
			}
			if _, cached := s.toolCalling.Cached("m"); cached {
				t.Error("an inconclusive probe must not be cached")
			}
			// A later, healthy probe must still be able to reach a verdict.
			s.toolCalling.Verdict(context.Background(), &probeAdapter{toolCall: true}, "m")
			if v, cached := s.toolCalling.Cached("m"); !cached || v != toolcallprobe.OK {
				t.Errorf("verdict after recovery = %v (cached=%v), want toolcallprobe.OK", v, cached)
			}
		})
	}
}

// TestToolCallingWarningProbesOncePerModel uses a distinct session per call so
// the warn-once bound below can't mask a re-probe: each session is owed its own
// warning, but they must all be served from one cached verdict.
func TestToolCallingWarningProbesOncePerModel(t *testing.T) {
	a := &probeAdapter{toolCall: false}
	s := ollamaServer(t, a)

	for _, sess := range []string{"sess-1", "sess-2", "sess-3"} {
		if warn := s.toolCallingWarning(context.Background(), sess, "bad-model"); warn == "" {
			t.Fatalf("session %s: expected the cached verdict to warn", sess)
		}
	}
	if got := a.calls.Load(); got != 1 {
		t.Errorf("probed %d times, want exactly 1 (verdict must be cached)", got)
	}
}

// TestToolCallingWarningWarnsOncePerSession pins the nagging bound: a
// tool-incapable model is still fine to converse with, so a session hears about
// it once, not on every turn.
func TestToolCallingWarningWarnsOncePerSession(t *testing.T) {
	a := &probeAdapter{toolCall: false}
	s := ollamaServer(t, a)

	if warn := s.toolCallingWarning(context.Background(), "sess-1", "bad-model"); warn == "" {
		t.Fatal("expected the first turn to warn")
	}
	for turn := 2; turn <= 4; turn++ {
		if warn := s.toolCallingWarning(context.Background(), "sess-1", "bad-model"); warn != "" {
			t.Errorf("turn %d re-warned: %q", turn, warn)
		}
	}
	// A different model in the same session is new information, so it warns.
	if warn := s.toolCallingWarning(context.Background(), "sess-1", "other-bad-model"); warn == "" {
		t.Error("expected a model switch to warn again")
	}
	// So is the same model in a session that hasn't been told yet.
	if warn := s.toolCallingWarning(context.Background(), "sess-2", "bad-model"); warn == "" {
		t.Error("expected an untold session to warn")
	}
}

// TestToolCallingWarningCollapsesConcurrentProbes covers the real daemon shape:
// several sessions starting runs on the same cold model at once must share one
// probe, not queue a model load each.
func TestToolCallingWarningCollapsesConcurrentProbes(t *testing.T) {
	a := &probeAdapter{toolCall: false, gate: make(chan struct{})}
	s := ollamaServer(t, a)

	var wg sync.WaitGroup
	warns := make([]string, 8)
	for i := range warns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Distinct sessions: every caller is owed a warning, so a dropped
			// one means a lost verdict rather than the warn-once bound.
			warns[i] = s.toolCallingWarning(context.Background(), fmt.Sprintf("sess-%d", i), "bad-model")
		}()
	}
	close(a.gate) // release them all at once
	wg.Wait()

	for i, w := range warns {
		if w == "" {
			t.Fatalf("caller %d got no warning", i)
		}
	}
	if got := a.calls.Load(); got != 1 {
		t.Errorf("probed %d times under concurrency, want exactly 1", got)
	}
}

// TestToolCallingWarningSkipsNonLocalProvider keeps the probe off the paid-API
// path entirely — the same gate doctor and the reachability check use.
func TestToolCallingWarningSkipsNonLocalProvider(t *testing.T) {
	a := &probeAdapter{toolCall: false}
	cfg := &config.Config{}
	cfg.Provider.Default = "anthropic"
	cfg.Provider.APIKey = "sk-test"
	s := &Server{cfg: cfg, adapter: a, toolCalling: toolcallprobe.NewGate()}

	if warn := s.toolCallingWarning(context.Background(), "sess-1", "claude-opus-4-8"); warn != "" {
		t.Fatalf("expected no probe for a cloud provider, got %q", warn)
	}
	if a.calls.Load() != 0 {
		t.Error("a cloud provider must never be probed")
	}
}

// TestToolCallingWarningProbesAtTheRunsContextWindow is P66.20/LLM-10. The
// probe's entire justification for running at turn start is that it shares the
// cold model load the turn was about to pay for — which is only true if it asks
// Ollama for the same num_ctx. toolcallprobe.Run sets none, so a bare adapter
// let the probe load the model at Ollama's default 4096 and the very next
// request (the turn itself, at the resolved window) forced a full unload and
// reload of the weights: the probe buying a cold load instead of sharing one.
func TestToolCallingWarningProbesAtTheRunsContextWindow(t *testing.T) {
	a := &probeAdapter{toolCall: true}
	s := ollamaServer(t, a)
	s.cfg.Provider.ContextWindow = 32768

	want, _ := s.effectiveContextWindowFor(context.Background(), "resident-model")
	if want <= 0 {
		t.Fatalf("test setup: effective window = %d, want a positive window to assert against", want)
	}
	s.toolCallingWarning(context.Background(), "sess-1", "resident-model")

	if a.calls.Load() != 1 {
		t.Fatalf("probed %d times, want exactly 1", a.calls.Load())
	}
	if got := int(a.numCtx.Load()); got != want {
		t.Errorf("probe requested num_ctx=%d, want %d — the window the turn behind it will ask for", got, want)
	}
}

// TestToolCallingWarningLeavesNumCtxUnsetWithNoDetectedWindow keeps the fix
// from inventing a number: with nothing configured and nothing detectable,
// provider.WithNumCtx returns the adapter untouched and the request carries no
// NumCtx at all, exactly as before.
func TestToolCallingWarningLeavesNumCtxUnsetWithNoDetectedWindow(t *testing.T) {
	a := &probeAdapter{toolCall: true}
	s := ollamaServer(t, a)

	if win, _ := s.effectiveContextWindowFor(context.Background(), "unknown-model"); win > 0 {
		t.Skipf("test setup: a window of %d was resolvable; this case needs none", win)
	}
	s.toolCallingWarning(context.Background(), "sess-1", "unknown-model")
	if got := a.numCtx.Load(); got != 0 {
		t.Errorf("probe requested num_ctx=%d with no window resolved, want 0 (unset)", got)
	}
}

func TestToolCallingWarningSkipsUnresolvedModel(t *testing.T) {
	a := &probeAdapter{toolCall: false}
	s := ollamaServer(t, a)

	for _, model := range []string{"", "  ", "auto"} {
		if warn := s.toolCallingWarning(context.Background(), "sess-1", model); warn != "" {
			t.Errorf("model %q: expected no warning, got %q", model, warn)
		}
	}
	if a.calls.Load() != 0 {
		t.Error("an unresolved model must never be probed")
	}
}
