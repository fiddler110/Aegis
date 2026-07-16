package toolcallprobe

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// scriptedAdapter replays one probe outcome and counts how often it was asked,
// so tests can assert both the verdict and whether it was cached.
type scriptedAdapter struct {
	calls     atomic.Int32
	toolCalls int
	stop      provider.StopReason
	streamErr error
	eventErr  error
}

func (a *scriptedAdapter) Name() string { return "scripted" }

func (a *scriptedAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	a.calls.Add(1)
	if a.streamErr != nil {
		return nil, a.streamErr
	}
	ch := make(chan provider.Event, a.toolCalls+2)
	go func() {
		defer close(ch)
		if a.eventErr != nil {
			ch <- provider.Event{Type: provider.EventError, Err: a.eventErr}
			return
		}
		for i := 0; i < a.toolCalls; i++ {
			ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: "tu", Name: "list_files", Input: json.RawMessage(`{"path":"."}`),
			}}
		}
		ch <- provider.Event{Type: provider.EventDone, Stop: a.stop}
	}()
	return ch, nil
}

// TestRunReportsTruncation is the core of the P34.2 false-positive fix: a run
// cut off at the token cap must say so, because zero tool calls means something
// completely different when the model never got to finish.
func TestRunReportsTruncation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		toolCalls     int
		stop          provider.StopReason
		wantCalls     int
		wantTruncated bool
	}{
		{"clean refusal", 0, provider.StopEndTurn, 0, false},
		{"truncated before the call", 0, provider.StopMaxTokens, 0, true},
		{"tool call", 1, provider.StopToolUse, 1, false},
		{"truncated after the call", 1, provider.StopMaxTokens, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Run(context.Background(), &scriptedAdapter{toolCalls: tc.toolCalls, stop: tc.stop}, "m")
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.ToolCalls != tc.wantCalls || res.Truncated != tc.wantTruncated {
				t.Errorf("Run = %+v, want {ToolCalls:%d Truncated:%v}", res, tc.wantCalls, tc.wantTruncated)
			}
		})
	}
}

// TestRunUsesAGenerousTokenCap pins the measured cap. At the original 256 the
// probe was a coin flip on qwen3:14b, which needed 124-825 completion tokens
// across five runs of this prompt and got truncated mid-reasoning on most of
// them — reported as a model that can't call tools.
func TestRunUsesAGenerousTokenCap(t *testing.T) {
	var got provider.Request
	a := &captureAdapter{onReq: func(r provider.Request) { got = r }}
	if _, err := Run(context.Background(), a, "m"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.MaxTokens != SmokeMaxTokens {
		t.Errorf("probe MaxTokens = %d, want SmokeMaxTokens (%d)", got.MaxTokens, SmokeMaxTokens)
	}
	if SmokeMaxTokens < 1024 {
		t.Errorf("SmokeMaxTokens = %d — too tight for a reasoning model's preamble; qwen3:14b was measured needing up to 825 completion tokens on this prompt", SmokeMaxTokens)
	}
}

type captureAdapter struct{ onReq func(provider.Request) }

func (a *captureAdapter) Name() string { return "capture" }
func (a *captureAdapter) Stream(_ context.Context, req provider.Request) (<-chan provider.Event, error) {
	a.onReq(req)
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "tu", Name: "list_files"}}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopToolUse}
	close(ch)
	return ch, nil
}

// TestVerdictTruncatedIsUnknownAndUncached is the false positive observed live:
// qwen3:14b calls tools reliably, but thought past the old 256-token cap on
// most runs, and the probe cached "Unsupported" for the daemon's whole life —
// warning on every session with that model. Truncation must reach no verdict,
// and an unreachable verdict must never be cached.
func TestVerdictTruncatedIsUnknownAndUncached(t *testing.T) {
	a := &scriptedAdapter{toolCalls: 0, stop: provider.StopMaxTokens}
	g := NewGate()

	if v := g.Verdict(context.Background(), a, "qwen3:14b"); v != Unknown {
		t.Errorf("verdict on a truncated probe = %v, want Unknown (the call may have been the next token)", v)
	}
	if _, cached := g.Cached("qwen3:14b"); cached {
		t.Error("a truncated probe's non-verdict was cached — it would outlive the run that couldn't justify it")
	}
	if w := g.Warning(context.Background(), a, "qwen3:14b"); w != "" {
		t.Errorf("warned about a model the probe never reached a verdict on: %q", w)
	}

	// The next probe is free to reach a real verdict: nothing poisoned it.
	a.toolCalls, a.stop = 1, provider.StopToolUse
	if v := g.Verdict(context.Background(), a, "qwen3:14b"); v != OK {
		t.Errorf("verdict after a clean re-probe = %v, want OK", v)
	}
}

// TestVerdictUntruncatedZeroIsUnsupported keeps the fix from swallowing the
// real signal P34.2 exists for: a model that finishes its turn and still makes
// no tool call is the negative verdict the probe can justify.
func TestVerdictUntruncatedZeroIsUnsupported(t *testing.T) {
	a := &scriptedAdapter{toolCalls: 0, stop: provider.StopEndTurn}
	g := NewGate()

	if v := g.Verdict(context.Background(), a, "m"); v != Unsupported {
		t.Errorf("verdict = %v, want Unsupported", v)
	}
	if v, cached := g.Cached("m"); !cached || v != Unsupported {
		t.Errorf("Cached = (%v, %v), want (Unsupported, true)", v, cached)
	}
	if w := g.Warning(context.Background(), a, "m"); w == "" {
		t.Error("no warning for a model that finished its turn without calling the tool")
	}
	if got := a.calls.Load(); got != 1 {
		t.Errorf("adapter probed %d times, want 1 (the verdict should be cached)", got)
	}
}

// TestVerdictErrorIsUnknownAndUncached pins Run's stated contract: a probe that
// couldn't run is never the model's fault.
func TestVerdictErrorIsUnknownAndUncached(t *testing.T) {
	for name, a := range map[string]*scriptedAdapter{
		"stream failed":    {streamErr: errors.New("connection refused")},
		"error mid-stream": {eventErr: errors.New("model server exploded")},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewGate()
			if v := g.Verdict(context.Background(), a, "m"); v != Unknown {
				t.Errorf("verdict = %v, want Unknown", v)
			}
			if _, cached := g.Cached("m"); cached {
				t.Error("a failed probe was cached")
			}
			if w := g.Warning(context.Background(), a, "m"); w != "" {
				t.Errorf("warned when the probe could not run: %q", w)
			}
		})
	}
}
