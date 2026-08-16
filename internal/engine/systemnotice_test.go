package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tokenest"
	"github.com/fiddler110/aegis/internal/tool"
)

// This file is the P66.7 fixture for review finding LLM-16: a system prompt that
// already fills half the served window is a run that can never be rescued by
// compaction, and the daemon has to say so once, up front, rather than let the
// per-turn guard rediscover it and report it only at 95% full.
//
// The window in each case is *derived* from the estimate of the fixture's own
// system prompt rather than written down, so the tests keep meaning what they
// say if tokenest's heuristic is ever retuned. That is also what makes the
// mutation pair below honest: 49% and 51% of the same prompt.

// systemNoticeFragment is the identifying phrase of the P66.7 notice. Tests
// match on it rather than on the whole sentence so rewording the remedy does not
// break them, while still telling this notice apart from the compaction ones.
const systemNoticeFragment = "system prompt and tool schemas are ~"

// oversizedSystemFixture is a system prompt large enough to be measured against
// a window without the arithmetic below rounding into the noise.
func oversizedSystemFixture() string {
	return strings.Repeat("you are a careful agent working inside a large repository. ", 60)
}

// windowForSharePercent returns the window at which the fixed part of the
// request (system prompt + tool schemas) occupies approximately share percent.
// A larger share means a smaller window.
func windowForSharePercent(t *testing.T, reg *tool.Registry, system string, share int) int {
	t.Helper()
	probe, err := New(Options{
		Adapter: endTurnAdapter(), Tools: reg, Model: "test",
		MaxTokens: 100, ContextWindowTokens: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixed := tokenest.Estimate(system) + probe.newCompactionGuard().overhead()
	if fixed <= 0 {
		t.Fatalf("fixture system prompt estimated at %d tokens", fixed)
	}
	return fixed * 100 / share
}

// runForSystemNotice runs one engine to completion and returns every notice it
// emitted.
func runForSystemNotice(t *testing.T, reg *tool.Registry, adapter provider.Adapter, system string, window int) []string {
	t.Helper()
	eng, err := New(Options{
		Adapter: adapter, Tools: reg, Model: "test",
		MaxTokens: 100, ContextWindowTokens: window,
	})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{System: system}
	conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
		provider.TextBlock{Text: "go"},
	}})
	var notices []string
	if err := eng.Run(context.Background(), conv, func(ev Event) {
		if ev.Kind == KindNotice {
			notices = append(notices, ev.Text)
		}
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return notices
}

func systemNotices(notices []string) []string {
	var out []string
	for _, n := range notices {
		if strings.Contains(n, systemNoticeFragment) {
			out = append(out, n)
		}
	}
	return out
}

// TestOversizedSystemPromptEmitsNotice: a system prompt filling most of the
// served window produces exactly one actionable notice, naming both numbers and
// the model-server knob that fixes it.
func TestOversizedSystemPromptEmitsNotice(t *testing.T) {
	reg := tool.NewRegistry()
	sys := oversizedSystemFixture()
	window := windowForSharePercent(t, reg, sys, 90)

	got := systemNotices(runForSystemNotice(t, reg, endTurnAdapter(), sys, window))
	if len(got) != 1 {
		t.Fatalf("system-prompt notices = %v, want exactly 1", got)
	}
	for _, want := range []string{"OLLAMA_CONTEXT_LENGTH", "num_ctx", "context window"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("notice %q does not mention %q", got[0], want)
		}
	}
}

// TestOversizedSystemPromptSilentWithRoomToSpare: the common case — a window
// with plenty of room — must stay silent. A notice on every run would be noise
// and would train users to ignore the one that matters.
func TestOversizedSystemPromptSilentWithRoomToSpare(t *testing.T) {
	reg := tool.NewRegistry()
	sys := oversizedSystemFixture()
	window := windowForSharePercent(t, reg, sys, 10)

	if got := systemNotices(runForSystemNotice(t, reg, endTurnAdapter(), sys, window)); len(got) != 0 {
		t.Fatalf("system-prompt notices = %v, want none at ~10%% of the window", got)
	}
}

// TestOversizedSystemPromptThresholdMutation is the threshold mutation test the
// method notes require for a new numeric constant: fixtures at 49% and 51% of
// the same window must land on opposite sides of oversizedSystemPercent. A test
// that only checked "huge fires, tiny does not" would pass for any threshold
// between them, and so could not tell 50 from 60.
//
// The 49/51 are written out rather than derived from oversizedSystemPercent on
// purpose — expressing them as `oversizedSystemPercent ± 1` would make the
// fixtures move with the constant and pass for *any* value of it, which is the
// exact failure this test exists to rule out. Moving the constant must fail
// here, and editing these two numbers is how you record that you meant to.
func TestOversizedSystemPromptThresholdMutation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		share int
		want  int
	}{
		{"just below the 50% threshold", 49, 0},
		{"just above the 50% threshold", 51, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := tool.NewRegistry()
			sys := oversizedSystemFixture()
			window := windowForSharePercent(t, reg, sys, tc.share)

			got := systemNotices(runForSystemNotice(t, reg, endTurnAdapter(), sys, window))
			if len(got) != tc.want {
				t.Fatalf("system-prompt notices at ~%d%% of the window = %v, want %d", tc.share, got, tc.want)
			}
		})
	}
}

// TestOversizedSystemPromptNoticeOncePerRun: the condition is constant for the
// whole run, so a multi-turn run must not repeat it — this is the property the
// 95%-full notice had to add fullWarned for, and the reason noticeOversizedSystem
// latches rather than trusting its call site.
func TestOversizedSystemPromptNoticeOncePerRun(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	sys := oversizedSystemFixture()
	window := windowForSharePercent(t, reg, sys, 90)

	toolTurn := func(id string) []provider.Event {
		return []provider.Event{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{
				ID: id, Name: "echo", Input: json.RawMessage(`{"msg":"hi"}`)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		}
	}
	adapter := &scriptedAdapter{turns: [][]provider.Event{
		toolTurn("tu_1"), toolTurn("tu_2"),
		{
			{Type: provider.EventTextDelta, Text: "done"},
			{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 1}},
		},
	}}

	if got := systemNotices(runForSystemNotice(t, reg, adapter, sys, window)); len(got) != 1 {
		t.Fatalf("system-prompt notices across a 3-turn run = %v, want exactly 1", got)
	}
}
