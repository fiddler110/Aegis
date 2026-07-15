package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
)

// keyMap holds all named key bindings for the TUI. Using key.Binding means
// each binding carries its own help text, which the help overlay aggregates.
type keyMap struct {
	Send          key.Binding
	Steer         key.Binding
	Newline       key.Binding
	Thinking      key.Binding
	Complete      key.Binding
	Help          key.Binding
	Palette       key.Binding
	Cancel        key.Binding
	Interrupt     key.Binding
	Clear         key.Binding
	Editor        key.Binding
	CycleMode     key.Binding
	HistUp        key.Binding
	HistDown      key.Binding
	Teammates     key.Binding
	Sessions      key.Binding
	Terminal      key.Binding
	SidebarToggle key.Binding
	PasteImage    key.Binding
	Diagnose      key.Binding
	HistorySearch key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Send:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send / queue next message (while streaming)")),
		Steer:         key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("alt+enter", "steer the running model (while streaming)")),
		Newline:       key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "insert newline (ctrl+j fallback)")),
		Thinking:      key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "expand/collapse thinking")),
		Complete:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "command completion")),
		Help:          key.NewBinding(key.WithKeys("f1"), key.WithHelp("f1", "toggle help")),
		Palette:       key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "command palette")),
		Cancel:        key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel / quit")),
		Interrupt:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "interrupt run / clear input (×2 when idle: backtrack)")),
		Clear:         key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear transcript")),
		Editor:        key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "open in $EDITOR")),
		CycleMode:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "cycle mode")),
		HistUp:        key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "history prev")),
		HistDown:      key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "history next")),
		Teammates:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "list sub-agents")),
		Sessions:      key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "switch session")),
		Terminal:      key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "toggle terminal pane")),
		SidebarToggle: key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "toggle sidebar")),
		PasteImage:    key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste image from clipboard")),
		Diagnose:      key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "diagnose last failed shell/terminal command")),
		// P22.4: Ctrl+R matches the shell reverse-search muscle memory this
		// item targets; the session switcher moved to Ctrl+Y to make room
		// (it previously held Ctrl+R — see docs/tui-guide.md and
		// docs/sessions.md, updated alongside this change).
		HistorySearch: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "search input history")),
	}
}

// bindingsByName maps the lowercase action names used by the tui.keybindings
// config section (P13.3.5) to their field in km, so overrides can be applied
// generically instead of via a giant switch that must be kept in sync by hand.
func (km *keyMap) bindingsByName() map[string]*key.Binding {
	return map[string]*key.Binding{
		"send":          &km.Send,
		"steer":         &km.Steer,
		"newline":       &km.Newline,
		"thinking":      &km.Thinking,
		"complete":      &km.Complete,
		"help":          &km.Help,
		"palette":       &km.Palette,
		"cancel":        &km.Cancel,
		"interrupt":     &km.Interrupt,
		"clear":         &km.Clear,
		"editor":        &km.Editor,
		"cyclemode":     &km.CycleMode,
		"histup":        &km.HistUp,
		"histdown":      &km.HistDown,
		"teammates":     &km.Teammates,
		"sessions":      &km.Sessions,
		"terminal":      &km.Terminal,
		"sidebartoggle": &km.SidebarToggle,
		"pasteimage":    &km.PasteImage,
		"diagnose":      &km.Diagnose,
		"historysearch": &km.HistorySearch,
	}
}

// applyKeybindings overrides named bindings' key sequences from config
// (P13.3.5). The help label is regenerated from the new primary key so
// help text (F1 overlay, /help) reflects what's actually bound rather than
// the hardcoded default. Returns an error naming every unknown action so a
// typo in tui.keybindings fails fast instead of silently doing nothing.
func (km *keyMap) applyKeybindings(overrides map[string][]string) error {
	if len(overrides) == 0 {
		return nil
	}
	byName := km.bindingsByName()
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)

	var unknown []string
	for _, name := range names {
		keys := overrides[name]
		b, ok := byName[strings.ToLower(name)]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if len(keys) == 0 {
			continue
		}
		desc := b.Help().Desc
		*b = key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], desc))
	}
	if len(unknown) > 0 {
		return fmt.Errorf("tui.keybindings: unknown action(s): %s", strings.Join(unknown, ", "))
	}
	return nil
}

// buildKeyMap starts from the hardcoded defaults and applies any
// tui.keybindings overrides (P13.3.5).
func buildKeyMap(overrides map[string][]string) (keyMap, error) {
	km := defaultKeyMap()
	if err := km.applyKeybindings(overrides); err != nil {
		return keyMap{}, err
	}
	return km, nil
}

// mustKeyMap builds the keyMap for overrides, falling back to the hardcoded
// defaults on error. Run() validates overrides up front and surfaces the
// error there; this fallback only matters for callers (tests) that
// construct a model directly without going through Run().
func mustKeyMap(overrides map[string][]string) keyMap {
	km, err := buildKeyMap(overrides)
	if err != nil {
		return defaultKeyMap()
	}
	return km
}

// keyHelpEntry is one row of a rendered keybinding list: the key label and
// its description.
type keyHelpEntry struct{ Key, Desc string }

// helpEntries is the single list of every keybinding's (key, description)
// pair — backing both the F1 overlay (renderHelpOverlay in tui.go) and
// /help's "Keyboard shortcuts" section (cmdHelp in slash.go, P14.9) so a new
// binding only needs to be added here once, the same single-source-of-truth
// approach commandDefs (P14.10) uses for slash commands.
func (km keyMap) helpEntries() []keyHelpEntry {
	return []keyHelpEntry{
		{km.Send.Help().Key, km.Send.Help().Desc},
		{km.Steer.Help().Key, km.Steer.Help().Desc},
		{km.Newline.Help().Key, km.Newline.Help().Desc},
		{km.Thinking.Help().Key, km.Thinking.Help().Desc},
		{km.Interrupt.Help().Key, km.Interrupt.Help().Desc},
		{km.Complete.Help().Key, km.Complete.Help().Desc},
		{km.Palette.Help().Key, km.Palette.Help().Desc},
		{km.Cancel.Help().Key, km.Cancel.Help().Desc},
		{km.Help.Help().Key, km.Help.Help().Desc},
		{km.Clear.Help().Key, km.Clear.Help().Desc},
		{km.Editor.Help().Key, km.Editor.Help().Desc},
		{km.CycleMode.Help().Key, km.CycleMode.Help().Desc},
		{km.Teammates.Help().Key, km.Teammates.Help().Desc},
		{km.Sessions.Help().Key, km.Sessions.Help().Desc},
		{km.Terminal.Help().Key, km.Terminal.Help().Desc},
		{km.SidebarToggle.Help().Key, km.SidebarToggle.Help().Desc},
		{km.PasteImage.Help().Key, km.PasteImage.Help().Desc},
		{km.Diagnose.Help().Key, km.Diagnose.Help().Desc},
		{km.HistUp.Help().Key, km.HistUp.Help().Desc},
		{km.HistDown.Help().Key, km.HistDown.Help().Desc},
		{km.HistorySearch.Help().Key, km.HistorySearch.Help().Desc},
	}
}
