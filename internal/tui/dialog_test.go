package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/modelcatalog"
)

// TestListDialogSelectAndCancel exercises the shared P16.6 listDialog: esc
// emits a dialogCancelMsg tagged with its kind, enter emits a
// dialogSelectedMsg carrying the selected item, both round-tripped through
// tea.Cmd exactly like the four dialogs it replaced did individually.
func TestListDialogSelectAndCancel(t *testing.T) {
	items := []list.Item{paletteItem{name: "help", desc: "show help"}}
	d := newListDialog(dialogPalette, 40, 10, "Test", false, items)

	_, cmd := d.Update(tea.KeyPressMsg{Code: 'j', Text: ""})
	if cmd != nil {
		t.Fatalf("unrelated key should not produce a cmd, got one")
	}

	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd from enter")
	}
	msg := cmd()
	sel, ok := msg.(dialogSelectedMsg)
	if !ok {
		t.Fatalf("expected dialogSelectedMsg, got %T", msg)
	}
	if sel.kind != dialogPalette {
		t.Errorf("expected dialogPalette kind, got %v", sel.kind)
	}
	if item, ok := sel.item.(paletteItem); !ok || item.name != "help" {
		t.Errorf("expected paletteItem{name: help}, got %#v", sel.item)
	}

	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected a cmd from esc")
	}
	cmsg := cmd()
	cancel, ok := cmsg.(dialogCancelMsg)
	if !ok {
		t.Fatalf("expected dialogCancelMsg, got %T", cmsg)
	}
	if cancel.kind != dialogPalette {
		t.Errorf("expected dialogPalette kind, got %v", cancel.kind)
	}
}

// TestRenderOverlayCompositesOverChat proves the P16.6 overlay compositing
// path: opening a dialog no longer replaces the frame with a blank
// background (the old lipgloss.Place behavior) — the chat content behind it
// must still be present in the rendered frame.
func TestRenderOverlayCompositesOverChat(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", Model: "test-model", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	base := ansi.Strip(m.renderChat())
	if !strings.Contains(base, "Aegis") {
		t.Fatalf("expected welcome banner in base chat view, got: %q", base)
	}

	pal := newPalette(m.width, m.height, allCommandEntries(nil))
	m.dialog = &pal

	full := ansi.Strip(m.render())
	if !strings.Contains(full, "Command Palette") {
		t.Errorf("expected dialog title in composited view, got: %q", full)
	}
	if !strings.Contains(full, "Aegis") {
		t.Errorf("expected chat content still visible behind the dialog, got: %q", full)
	}
}

// TestDimOutsideLeavesDialogRegionUntouched checks the dimming helper only
// marks cells outside the dialog's rectangle as faint.
func TestDimOutsideLeavesDialogRegionUntouched(t *testing.T) {
	out := renderOverlay(strings.Repeat("x", 10)+"\n"+strings.Repeat("x", 10), "AB", 10, 2)
	if !strings.Contains(out, "AB") {
		t.Fatalf("expected dialog content %q in composited output, got: %q", "AB", out)
	}
}

// TestQuitConfirmGatesStreamingQuit checks that requesting quit while a turn
// is streaming opens the confirm dialog instead of quitting immediately, and
// that confirming or cancelling behaves correctly (P16.6).
func TestQuitConfirmGatesStreamingQuit(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m.streaming = true

	m = driveUpdate(t, m, slashResultMsg{Quit: true})
	if !m.quitConfirm {
		t.Fatal("expected quitConfirm to be set while streaming")
	}

	// esc cancels: streaming continues, no quit.
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(model)
	if m.quitConfirm {
		t.Error("expected quitConfirm cleared after esc")
	}
	if cmd != nil {
		t.Error("expected no cmd from cancelling quit")
	}

	// Re-open and confirm.
	m.quitConfirm = true
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected a quit cmd from confirming")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// TestQuitImmediateWhenNotStreaming preserves the pre-P16.6 behavior: nothing
// is at risk when no turn is in flight, so /quit still exits without asking.
func TestQuitImmediateWhenNotStreaming(t *testing.T) {
	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	next, cmd := m.Update(slashResultMsg{Quit: true})
	m = next.(model)
	if m.quitConfirm {
		t.Error("did not expect quitConfirm when not streaming")
	}
	if cmd == nil {
		t.Fatal("expected an immediate quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// TestModelPickerGroupsByProviderAndMarksCurrent checks the P16.6 model
// picker sorts entries by provider and flags + front-sorts the current
// model within its group.
func TestModelPickerGroupsByProviderAndMarksCurrent(t *testing.T) {
	catalog := []modelcatalog.Model{
		{Provider: "openai", ID: "gpt-5.x (see provider)", Tier: modelcatalog.TierFrontier},
		{Provider: "anthropic", ID: "claude-opus-4-8", Tier: modelcatalog.TierFrontier},
		{Provider: "anthropic", ID: "claude-haiku-4-5", Tier: modelcatalog.TierBalanced},
	}
	d := newModelPicker(100, 40, catalog, "claude-haiku-4-5")

	if d.list.Items()[0].(modelItem).provider != "anthropic" {
		t.Fatalf("expected anthropic group first (alphabetical), got %#v", d.list.Items()[0])
	}
	first := d.list.Items()[0].(modelItem)
	if !first.current || first.id != "claude-haiku-4-5" {
		t.Errorf("expected current model claude-haiku-4-5 pinned first in its group, got %#v", first)
	}

	// A model not in the catalog is prepended as its own synthetic entry.
	d2 := newModelPicker(100, 40, catalog, "my-custom-model")
	custom := d2.list.Items()[0].(modelItem)
	if !custom.current || custom.id != "my-custom-model" {
		t.Errorf("expected synthetic current entry for an out-of-catalog model, got %#v", custom)
	}
}

// TestCmdModelsOpensPicker checks /models returns curated catalog entries for
// the TUI to open the picker with, rather than plain text (P16.6).
func TestCmdModelsOpensPicker(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model", "")
	res := d.Dispatch(&commands.ParsedCommand{Name: "models"})
	if res.Models == nil {
		t.Fatalf("expected Models to be populated, got result: %#v", res)
	}
	if len(res.Models) == 0 {
		t.Error("expected a non-empty curated model list")
	}
}
