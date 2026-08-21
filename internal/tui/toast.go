package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
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

// toastExpiredMsg names the toast its timer was armed for (by pointer
// identity), so a stale timer from an earlier toast can't retire a newer one
// shown in the same TTL window (P63.10).
type toastExpiredMsg struct{ t *toast }

// newToastCmd creates a toast and a Cmd that fires toastExpiredMsg after TTL.
func newToastCmd(message string, level toastLevel) (*toast, tea.Cmd) {
	t := &toast{message: message, level: level}
	cmd := tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpiredMsg{t: t} })
	return t, cmd
}
