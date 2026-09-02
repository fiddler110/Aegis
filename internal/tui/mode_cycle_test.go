package tui

import (
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

// shift+tab must advance the permission mode plan→build→auto→plan and reflect
// each step immediately — in m.slash.mode and the rendered badge — without
// waiting on the async /mode UpdateSession RPC. driveUpdate discards the
// returned command, so this asserts the optimistic, synchronous state change:
// before the optimistic update the mode only moved when that RPC completed, so
// the badge lagged and two quick presses both re-read the same stale mode.
func TestShiftTabCyclesModeOptimistically(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "plan", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	for _, want := range []string{"build", "auto", "plan", "build"} {
		m = driveUpdate(t, m, shiftTab)
		if m.slash.mode != want {
			t.Fatalf("after shift+tab: m.slash.mode = %q, want %q", m.slash.mode, want)
		}
		if badge := ansi.Strip(m.renderModeBadge()); badge != want {
			t.Errorf("mode badge = %q, want %q", badge, want)
		}
	}
}

// ctrl+tab is the silent alternate for CycleMode (P<dashboard>): terminals
// that support Kitty/modifyOtherKeys key disambiguation can deliver it as
// distinct from tab/shift+tab, so it must drive the same optimistic cycle.
func TestCtrlTabCyclesMode(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "plan", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	ctrlTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl}
	m = driveUpdate(t, m, ctrlTab)
	if m.slash.mode != "build" {
		t.Fatalf("after ctrl+tab: m.slash.mode = %q, want build", m.slash.mode)
	}
}

// While streaming, shift+tab must not change the mode (mode is locked for the
// duration of a run) — the handler only cycles when !m.streaming.
func TestShiftTabIgnoredWhileStreaming(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true
	// Pre-seed the custom-command cache so the fall-through completion sync
	// (shift+tab isn't consumed while streaming) doesn't hit the nil test client.
	m.slash.customs = []api.CommandInfo{}

	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.slash.mode != "build" {
		t.Fatalf("mode changed while streaming: got %q, want build", m.slash.mode)
	}
}
