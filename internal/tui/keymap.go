package tui

import "charm.land/bubbles/v2/key"

// keyMap holds all named key bindings for the TUI. Using key.Binding means
// each binding carries its own help text, which the help overlay aggregates.
type keyMap struct {
	Send          key.Binding
	Queue         key.Binding
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
}

func defaultKeyMap() keyMap {
	return keyMap{
		Send:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send / steer")),
		Queue:         key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("alt+enter", "queue next message (while streaming)")),
		Newline:       key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "insert newline (ctrl+j fallback)")),
		Thinking:      key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "expand/collapse thinking")),
		Complete:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "command completion")),
		Help:          key.NewBinding(key.WithKeys("f1"), key.WithHelp("f1", "toggle help")),
		Palette:       key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "command palette")),
		Cancel:        key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel / quit")),
		Interrupt:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "interrupt run (×2 to stop)")),
		Clear:         key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear transcript")),
		Editor:        key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "open in $EDITOR")),
		CycleMode:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "cycle mode")),
		HistUp:        key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "history prev")),
		HistDown:      key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "history next")),
		Teammates:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "list sub-agents")),
		Sessions:      key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "switch session")),
		Terminal:      key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "toggle terminal pane")),
		SidebarToggle: key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "toggle sidebar")),
	}
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
		{km.Queue.Help().Key, km.Queue.Help().Desc},
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
		{km.HistUp.Help().Key, km.HistUp.Help().Desc},
		{km.HistDown.Help().Key, km.HistDown.Help().Desc},
	}
}
