package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

// phaseModel returns a model that has just sent a turn — streaming, still in
// the waiting-for-first-token phase — with the run's clock backdated by
// sinceStart so the elapsed readout has something to show.
func phaseModel(t *testing.T, sinceStart time.Duration) model {
	t.Helper()
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.ta.SetValue("explain this repo")
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.inputTokens = 4200
	m.streamStart = time.Now().Add(-sinceStart)
	m.refresh()
	return m
}

// TestStreamPhaseSplitAtFirstToken is the P33.4 phase split: the 10-60s a
// context-heavy turn spends before its first token used to be indistinguishable
// from generation — both drew the same shimmer — so the wait must name itself,
// and must stop claiming to be a wait the moment output arrives.
func TestStreamPhaseSplitAtFirstToken(t *testing.T) {
	m := phaseModel(t, 12*time.Second)

	if m.status != statusWaiting {
		t.Fatalf("status = %q, want %q before any model output", m.status, statusWaiting)
	}
	got := plainView(m)
	if !strings.Contains(got, statusWaiting) {
		t.Fatalf("expected the waiting phase named on screen, got:\n%s", got)
	}
	if !strings.Contains(got, "· 12s") {
		t.Errorf("expected the wait's elapsed time alongside it, got:\n%s", got)
	}

	m.applyEvent(api.Event{Kind: api.KindText, Text: "The repo is a daemon plus client."})
	m.refresh()
	if m.status != statusGenerating {
		t.Fatalf("status = %q, want %q once the first token arrived", m.status, statusGenerating)
	}
	if m.firstTokenAt.IsZero() {
		t.Error("expected the first token to have started the generation clock")
	}
	if got := plainView(m); strings.Contains(got, statusWaiting) {
		t.Errorf("the waiting phase outlived the first token:\n%s", got)
	}
}

// TestStreamPhaseThinkingCountsAsFirstToken covers a reasoning model whose run
// opens with reasoning deltas rather than prose: those are output too, and the
// wait is over as soon as they land.
func TestStreamPhaseThinkingCountsAsFirstToken(t *testing.T) {
	m := phaseModel(t, 30*time.Second)

	m.applyEvent(api.Event{Kind: api.KindThinking, Text: "Let me check the layout."})
	m.refresh()
	if m.status != statusGenerating {
		t.Fatalf("status = %q, want %q once reasoning started", m.status, statusGenerating)
	}
	if got := plainView(m); strings.Contains(got, statusWaiting) {
		t.Errorf("reasoning deltas left the phase line claiming a wait:\n%s", got)
	}
}

// TestStreamPhaseEndsAtProvisionalToolCard is the P33.3 interaction: a run
// whose first act is a tool call puts a "preparing <tool>…" card on screen
// before a single character of prose. The phase line must not sit above that
// card insisting nothing has come back from the model yet.
func TestStreamPhaseEndsAtProvisionalToolCard(t *testing.T) {
	m := phaseModel(t, 20*time.Second)

	m.applyEvent(api.Event{Kind: api.KindToolCallStart, Tool: "read_file", ToolID: "t1"})
	m.refresh()
	if m.status != statusGenerating {
		t.Fatalf("status = %q, want %q once the model named a tool", m.status, statusGenerating)
	}
	got := plainView(m)
	if !strings.Contains(got, "read_file") {
		t.Fatalf("expected the provisional tool card on screen, got:\n%s", got)
	}
	if strings.Contains(got, statusWaiting) {
		t.Errorf("the waiting phase contradicts the provisional tool card:\n%s", got)
	}
}

// TestStreamHintStaysVisibleDuringGeneration is the other half of P33.4: the
// hint used to live only in the no-live-text branch of the tail, so every token
// number vanished at exactly the moment there were tokens to count. It belongs
// in the status bar for the whole run.
func TestStreamHintStaysVisibleDuringGeneration(t *testing.T) {
	m := phaseModel(t, 25*time.Second)

	m.applyEvent(api.Event{Kind: api.KindText, Text: strings.Repeat("streamed answer text. ", 80)})
	m.firstTokenAt = time.Now().Add(-20 * time.Second) // backdate the generation window
	m.refresh()

	got := plainView(m)
	if !strings.Contains(got, "streamed answer text.") {
		t.Fatalf("expected the live tail on screen, got:\n%s", got)
	}
	if !strings.Contains(got, "↑4.2k") {
		t.Errorf("prompt size disappeared once text started flowing:\n%s", got)
	}
	if !strings.Contains(got, "↓~440") {
		t.Errorf("output estimate disappeared once text started flowing:\n%s", got)
	}
	if !strings.Contains(got, "~22 tok/s") {
		t.Errorf("throughput missing during generation:\n%s", got)
	}
}

// TestStreamStatsRateExcludesTheWait: on a local model the wait for the first
// token dwarfs the generation window, so averaging it into tok/s would report a
// rate the model never ran at.
func TestStreamStatsRateExcludesTheWait(t *testing.T) {
	m := phaseModel(t, 60*time.Second)
	m.outBytes = 4000
	m.firstTokenAt = time.Now().Add(-10 * time.Second)

	st := m.streamStats()
	if st.outputToks != 1000 {
		t.Fatalf("outputToks = %d, want 1000", st.outputToks)
	}
	if !st.estimated {
		t.Error("byte-derived counts must be marked estimated")
	}
	if got := int(st.tokPerSec + 0.5); got != 100 {
		t.Errorf("tokPerSec = %v, want ~100 (1000 tokens over the 10s generation window, not the 60s run)", st.tokPerSec)
	}
}

func TestFormatStreamHint(t *testing.T) {
	tests := []struct {
		name string
		st   streamStats
		want string
	}{
		{
			name: "waiting: elapsed and prompt only",
			st:   streamStats{elapsedSecs: 12, inputToks: 4200, estimated: true},
			want: " · 12s · ↑4.2k",
		},
		{
			name: "generating: estimates marked with ~",
			st:   streamStats{elapsedSecs: 12, inputToks: 4200, outputToks: 380, tokPerSec: 14.2, estimated: true},
			want: " · 12s · ↑4.2k · ↓~380 · ~14 tok/s",
		},
		{
			name: "reported counts drop the ~",
			st:   streamStats{elapsedSecs: 12, outputToks: 380, tokPerSec: 14.2},
			want: " · 12s · ↓380 · 14 tok/s",
		},
		{
			name: "nothing substantiated yet",
			st:   streamStats{estimated: true},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatStreamHint(tt.st); got != tt.want {
				t.Errorf("formatStreamHint() = %q, want %q", got, tt.want)
			}
		})
	}
}
