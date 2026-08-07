package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// updateSessionsLoaded fills the session picker with the fetched rows.
func (m model) updateSessionsLoaded(msg sessionsLoadedMsg) (tea.Model, tea.Cmd) {
	if !m.awaitingPicker(dialogSessionPicker) {
		if msg.err != nil {
			t, cmd := newToastCmd("sessions: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		return m, nil
	}
	if msg.err != nil {
		return m, m.dialog.setNotice("sessions: " + msg.err.Error())
	}
	if len(msg.items) == 0 {
		return m, m.dialog.setNotice("no sessions to switch to")
	}
	return m, m.dialog.setItems(sessionPickerItems(msg.items), sessionPickerH(m.height, len(msg.items)))
}

// updateSessionSwitched adopts a session the user switched to.
func (m model) updateSessionSwitched(msg sessionSwitchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t, cmd := newToastCmd("switch: "+msg.err.Error(), toastError)
		m.activeToast = t
		return m, cmd
	}
	m.applySwitchedSession(msg.sess)
	m.refresh()
	return m, nil
}

// updateBacktrackTargets fills the backtrack picker with the fetched
// checkpoints.
func (m model) updateBacktrackTargets(msg backtrackTargetsMsg) (tea.Model, tea.Cmd) {
	if !m.awaitingPicker(dialogBacktrackPicker) {
		if msg.err != nil {
			t, cmd := newToastCmd("backtrack: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		return m, nil
	}
	if msg.err != nil {
		return m, m.dialog.setNotice("backtrack: " + msg.err.Error())
	}
	if len(msg.items) == 0 {
		return m, m.dialog.setNotice("no checkpoints yet — send a message first")
	}
	return m, m.dialog.setItems(backtrackPickerItems(msg.items), backtrackPickerH(m.height, len(msg.items)))
}

// updateForked adopts the session a backtrack fork created.
func (m model) updateForked(msg forkedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t, cmd := newToastCmd("fork: "+msg.err.Error(), toastError)
		m.activeToast = t
		return m, cmd
	}
	m.applySwitchedSession(msg.sess)
	if msg.prefill != "" {
		// P22.3: hand the original message back for editing rather than
		// resending it verbatim — the whole point of backtracking.
		m.ta.SetValue(msg.prefill)
	}
	t, cmd := newToastCmd(fmt.Sprintf("Forked into %q — edit and send to continue.", msg.title), toastInfo)
	m.activeToast = t
	m.refresh()
	return m, cmd
}
