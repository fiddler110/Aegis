package tui

import "charm.land/bubbles/v2/key"

// keyMap holds all named key bindings for the TUI. Using key.Binding means
// each binding carries its own help text, which the help overlay aggregates.
type keyMap struct {
	Send      key.Binding
	Newline   key.Binding
	Complete  key.Binding
	Help      key.Binding
	Palette   key.Binding
	Cancel    key.Binding
	Interrupt key.Binding
	Clear     key.Binding
	Editor    key.Binding
	CycleMode key.Binding
	HistUp    key.Binding
	HistDown  key.Binding
	Teammates key.Binding
	Sessions  key.Binding
	Terminal  key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Send:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send / steer")),
		Newline:   key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "insert newline")),
		Complete:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "command completion")),
		Help:      key.NewBinding(key.WithKeys("f1"), key.WithHelp("f1", "toggle help")),
		Palette:   key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "command palette")),
		Cancel:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel / quit")),
		Interrupt: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "interrupt run (×2 to stop)")),
		Clear:     key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear transcript")),
		Editor:    key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "open in $EDITOR")),
		CycleMode: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "cycle mode")),
		HistUp:    key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "history prev")),
		HistDown:  key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "history next")),
		Teammates: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "list sub-agents")),
		Sessions:  key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "switch session")),
		Terminal:  key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "toggle terminal pane")),
	}
}
