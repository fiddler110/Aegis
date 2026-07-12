package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdScrollbackSentinels checks cmdScrollback's dispatcher-level
// contract: no args toggles, "on"/"off" (any case) pick a side explicitly,
// and anything else is rejected — the same shape as cmdHumor's tests
// (theme_test.go / humor's own precedent), since /scrollback follows that
// on/off/toggle convention rather than /tools' two-subcommand one.
func TestCmdScrollbackSentinels(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")

	res := d.Dispatch(&commands.ParsedCommand{Name: "scrollback"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if res.Output != "\x00scrollback-toggle" {
		t.Errorf("expected toggle sentinel, got %q", res.Output)
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "scrollback", Args: []string{"ON"}})
	if res.Output != "\x00scrollback-on" {
		t.Errorf("expected on sentinel, got %q", res.Output)
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "scrollback", Args: []string{"off"}})
	if res.Output != "\x00scrollback-off" {
		t.Errorf("expected off sentinel, got %q", res.Output)
	}

	res = d.Dispatch(&commands.ParsedCommand{Name: "scrollback", Args: []string{"bogus"}})
	if !res.IsError {
		t.Fatalf("expected an error for an unrecognized argument, got: %s", res.Output)
	}
}

// TestScrollbackToggleLive drives the real Update path (the slashResultMsg
// sentinels cmdScrollback emits) and checks the state transitions: default
// off, "on" flips it and reports a status line, "toggle" flips again, "off"
// forces it back off.
func TestScrollbackToggleLive(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	if m.rawScrollback {
		t.Fatal("expected raw scrollback mode off by default")
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-on"})
	if !m.rawScrollback {
		t.Fatal("expected raw scrollback mode on after \\x00scrollback-on")
	}
	if got := plainView(m); !strings.Contains(got, "Raw scrollback mode: on") {
		t.Errorf("expected an on-confirmation status line, got:\n%s", got)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-toggle"})
	if m.rawScrollback {
		t.Fatal("expected toggle to flip raw scrollback mode back off")
	}
	if got := plainView(m); !strings.Contains(got, "Raw scrollback mode: off") {
		t.Errorf("expected an off-confirmation status line, got:\n%s", got)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-on"})
	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-off"})
	if m.rawScrollback {
		t.Fatal("expected explicit off to force raw scrollback mode off")
	}
}

// TestScrollbackViewReleasesAltScreenAndMouse is the actual premise check for
// P22.6: confirms tui.go's tea.View() really does carry AltScreen=true and
// MouseMode=CellMotion by default (contra the roadmap text's assumption that
// alt-screen alone was already off), and that raw scrollback mode flips both
// off — the two terminal-mode bits release() actually needs to hand real
// scrollback/selection/search back to the terminal emulator.
func TestScrollbackViewReleasesAltScreenAndMouse(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	v := m.View()
	if !v.AltScreen {
		t.Error("expected alt-screen ON by default (confirms the roadmap's alt-screen assumption about default TUI behavior)")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("expected mouse cell-motion capture ON by default, got %v", v.MouseMode)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-on"})
	v = m.View()
	if v.AltScreen {
		t.Error("expected alt-screen released in raw scrollback mode")
	}
	if v.MouseMode != tea.MouseModeNone {
		t.Errorf("expected mouse capture released in raw scrollback mode, got %v", v.MouseMode)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-off"})
	v = m.View()
	if !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion {
		t.Error("expected alt-screen and mouse capture restored after turning raw scrollback mode back off")
	}
}

// TestScrollbackModeUnclipsTranscript is the rendering-mode branch this mode
// actually needs, per the investigation in tui.go's View()/applyViewportHeight
// doc comments: releasing alt-screen alone does not restore native
// scrollback, because transcriptPane.View() clips to a bounded, in-place-
// redrawn viewport window regardless of alt-screen. This confirms normal mode
// clips old content out of the rendered frame, and raw scrollback mode
// doesn't: the pane height tracks total content height and every appended
// line stays in the rendered frame.
func TestScrollbackModeUnclipsTranscript(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	for i := 0; i < 200; i++ {
		m.transcript.Append(fmt.Sprintf("scrollback-marker-%03d\n", i))
	}
	m.refresh()

	// Normal (bounded) mode: the pane clips to fit the terminal window, so
	// the earliest lines have scrolled out of the visible rendered frame.
	if m.transcript.Height() >= m.transcript.TotalHeight() {
		t.Fatalf("expected bounded mode to clip: pane height %d, total content height %d",
			m.transcript.Height(), m.transcript.TotalHeight())
	}
	if got := plainView(m); strings.Contains(got, "scrollback-marker-000") {
		t.Errorf("expected the earliest marker to be clipped out of the bounded viewport, got:\n%s", got)
	}

	// Raw scrollback mode: nothing clipped — pane height matches total
	// content height, and every appended line is present in the frame the
	// terminal actually receives (what lets old lines scroll through the
	// terminal's own history instead of being discarded by the in-app
	// viewport).
	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-on"})
	if m.transcript.Height() != m.transcript.TotalHeight() {
		t.Fatalf("expected raw mode pane height to equal total content height: pane %d, total %d",
			m.transcript.Height(), m.transcript.TotalHeight())
	}
	got := plainView(m)
	if !strings.Contains(got, "scrollback-marker-000") {
		t.Errorf("expected the earliest marker present (unclipped) in raw scrollback mode, got:\n%s", got)
	}
	if !strings.Contains(got, "scrollback-marker-199") {
		t.Errorf("expected the latest marker present in raw scrollback mode, got:\n%s", got)
	}

	// Appending more content after the mode is already on must keep growing
	// the pane in lockstep (refresh()'s raw-mode branch, not just the one at
	// toggle time in applyViewportHeight) rather than falling behind and
	// clipping the newest content.
	m.transcript.Append("scrollback-marker-200\n")
	m.refresh()
	if got := plainView(m); !strings.Contains(got, "scrollback-marker-200") {
		t.Errorf("expected newly appended content to stay unclipped after the mode is already on, got:\n%s", got)
	}
}

// TestScrollbackModeHidesSidebar checks renderChat's raw-mode branch: the
// sidebar (which assumes a fixed-height dashboard column, and would render as
// a lipgloss Height()-padded block thousands of lines tall next to an
// unbounded transcript) is suppressed while raw scrollback mode is on, even
// though sidebarOpen itself is untouched (toggling raw mode back off restores
// exactly the sidebar state from before).
func TestScrollbackModeHidesSidebar(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00sidebar-toggle"})
	if !m.sidebarOpen {
		t.Fatal("expected sidebar open after toggling it on")
	}
	if got := plainView(m); !strings.Contains(got, "SESSION") {
		t.Errorf("expected sidebar section header visible with sidebar open, got:\n%s", got)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-on"})
	if !m.sidebarOpen {
		t.Fatal("expected sidebarOpen to stay true (raw mode only suppresses rendering it)")
	}
	if got := plainView(m); strings.Contains(got, "◇ SESSION") {
		t.Errorf("expected sidebar hidden while raw scrollback mode is on, got:\n%s", got)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00scrollback-off"})
	if got := plainView(m); !strings.Contains(got, "SESSION") {
		t.Errorf("expected sidebar restored after turning raw scrollback mode back off, got:\n%s", got)
	}
}
