package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/api"
)

func escKeyMsg() tea.KeyMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// TestEscEsc_EmptyInputNotStreaming_OpensBacktrackPicker is the P22.3
// regression: two Esc presses in a row, with the input box already empty and
// no run streaming, must arm-then-fire a fetchBacktrackTargets command
// instead of the old silent no-op. The first press only arms backtrackArmed; the
// dialog/command only appears on the second, genuinely-a-double-tap press —
// mirroring the streaming double-tap-to-cancel detection this shares a flag
// with.
func TestEscEsc_EmptyInputNotStreaming_OpensBacktrackPicker(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.streaming {
		t.Fatal("precondition: model should not be streaming")
	}
	if m.ta.Value() != "" {
		t.Fatal("precondition: input box should start empty")
	}

	// First Esc: arms the double-tap, does not fire anything yet.
	next, cmd := m.Update(escKeyMsg())
	m = next.(model)
	if !m.backtrackArmed {
		t.Fatal("expected first Esc on empty input to arm backtrackArmed")
	}
	if cmd != nil {
		t.Fatal("expected no command from the first (arming) Esc press")
	}

	// Second Esc: fires the backtrack picker fetch and disarms.
	next, cmd = m.Update(escKeyMsg())
	m = next.(model)
	if m.backtrackArmed {
		t.Error("expected backtrackArmed to reset after the second Esc")
	}
	if cmd == nil {
		t.Fatal("expected the second Esc to return a fetchBacktrackTargets command")
	}
}

// TestEscEsc_SameFrameAltEsc mirrors the streaming branch's documented
// same-frame quirk: a terminal that coalesces a fast double-tap into one
// "alt+esc" event must be treated as an already-confirmed second press, not
// require a third/fourth tap.
func TestEscEsc_SameFrameAltEsc(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModAlt})
	m = next.(model)
	if m.backtrackArmed {
		t.Error("alt+esc should fire immediately, not leave backtrackArmed armed")
	}
	if cmd == nil {
		t.Fatal("expected alt+esc (same-frame double-tap) to fire immediately")
	}
}

// TestEsc_NonEmptyInput_ClearsWithoutArming checks Esc's existing job is
// unchanged: with text in the box, a single Esc clears it and does not arm
// the backtrack double-tap (arming only starts once the box is already
// empty, so the very next Esc after a clear is a fresh "first press").
func TestEsc_NonEmptyInput_ClearsWithoutArming(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.ta.SetValue("draft text")

	next, cmd := m.Update(escKeyMsg())
	m = next.(model)
	if m.ta.Value() != "" {
		t.Errorf("expected Esc to clear the input, got %q", m.ta.Value())
	}
	if m.backtrackArmed {
		t.Error("clearing non-empty input should not arm the backtrack double-tap")
	}
	if cmd != nil {
		t.Error("expected no command from a plain clear-the-input Esc")
	}
}

// TestEscEsc_InterveningKeyDisarms checks that any non-Esc key between the
// two presses cancels the arm state, same as the streaming cancel-arm does.
func TestEscEsc_InterveningKeyDisarms(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	// Typed runes run syncCompletion -> slash.Customs(); pre-seed the customs
	// cache so this client-less test model never calls the nil daemon client
	// (same pre-existing crash risk follow_intent_test.go sidesteps).
	m.slash.customs = []api.CommandInfo{}

	m = driveUpdate(t, m, escKeyMsg())
	if !m.backtrackArmed {
		t.Fatal("expected first Esc to arm backtrackArmed")
	}

	// "down" with no history navigation in progress is a genuine no-op on
	// the input box (unlike a printable rune, which would leave the box
	// non-empty and change what the *next* Esc does), isolating the "any
	// intervening key disarms" behavior from the "Esc always clears
	// non-empty input first" behavior exercised separately above.
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.backtrackArmed {
		t.Fatal("expected a non-Esc key to disarm backtrackArmed")
	}
	if m.ta.Value() != "" {
		t.Fatalf("precondition: input should still be empty, got %q", m.ta.Value())
	}

	// A subsequent single Esc should only re-arm, not fire, since the
	// sequence was broken.
	next, cmd := m.Update(escKeyMsg())
	m = next.(model)
	if !m.backtrackArmed {
		t.Error("expected Esc after the break to re-arm rather than fire")
	}
	if cmd != nil {
		t.Error("expected no command from a re-arming Esc after an interruption")
	}
}
