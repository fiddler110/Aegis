package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const toastTTL = 5 * time.Second

type toastLevel int

const (
	toastInfo toastLevel = iota
	toastWarn
	toastError
)

type toast struct {
	message string
	level   toastLevel
}

type toastExpiredMsg struct{}

// newToastCmd creates a toast and a Cmd that fires toastExpiredMsg after TTL.
func newToastCmd(message string, level toastLevel) (*toast, tea.Cmd) {
	t := &toast{message: message, level: level}
	cmd := tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{} })
	return t, cmd
}
