package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// updateSpinnerTick advances the spinner animation and the per-frame work that
// rides on it. It does not consume the message — the returned commands are
// appended to the ones the composer collects.
func (m model) updateSpinnerTick(msg spinner.TickMsg) (model, []tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.sp, cmd = m.sp.Update(msg)
	if m.streaming {
		cmds = append(cmds, cmd) // always re-queue so animation resumes on scroll-back
		// P3.7: suppress redraws when the "● thinking…" indicator is scrolled
		// off-screen — it lives at the viewport bottom, visible only when
		// followBottom is true.
		if m.followBottom {
			m.animStep++
			m.updatePendingToolCards() // P21.2: keep pending cards' shimmer live
			m.refresh()
			// P2.5: poll sub-agent roster every 20 animation frames.
			if m.animStep%20 == 0 {
				cmds = append(cmds, m.fetchTeammatesQuiet())
			}
		}
	}
	return m, cmds
}

// updateToastExpired retires the toast whose lifetime just elapsed.
func (m model) updateToastExpired(msg toastExpiredMsg) model {
	m.activeToast = nil
	return m
}
