package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// P76.2: every quit path must cancel the interactive terminal pane's command
// context (m.splitTerm.termRun.cancel), not only the model-turn context (m.streamState.cancel).
// Without it, the execTermCmd goroutine and the child process behind it are
// orphaned past p.Run() returning. Run()'s doc comment in tui.go states this
// as a property of the package; these tests are what keeps it true. Each of
// the three quit paths gets its own case because they are three separate
// switch arms in three files, and a fourth one added later without the cancel
// is exactly the regression this catches.
func TestQuitPathsCancelTheTerminalRun(t *testing.T) {
	// armTermRun gives m a termRun whose cancel func is observable.
	armTermRun := func(m *model) context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		m.splitTerm.termRun = &termRun{cancel: cancel}
		return ctx
	}

	t.Run("ctrl+c", func(t *testing.T) {
		m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
		ctx := armTermRun(&m)
		if _, cmd, _ := m.updateKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
			t.Fatal("ctrl+c on a non-streaming model should return a quit command")
		}
		assertCancelled(t, ctx)
	})

	t.Run("quit confirmation", func(t *testing.T) {
		m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
		m.overlays.quitConfirm = true
		ctx := armTermRun(&m)
		if _, cmd := m.updateQuitConfirm(tea.KeyPressMsg{Code: 'y'}); cmd == nil {
			t.Fatal("confirming the quit dialog should return a quit command")
		}
		assertCancelled(t, ctx)
	})

	t.Run("/quit", func(t *testing.T) {
		m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
		ctx := armTermRun(&m)
		if _, cmd := m.updateSlashResult(slashResultMsg{Quit: true}); cmd == nil {
			t.Fatal("/quit on a non-streaming model should return a quit command")
		}
		assertCancelled(t, ctx)
	})
}

func assertCancelled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("quit path left the interactive terminal command's context live (P76.2)")
	}
}
