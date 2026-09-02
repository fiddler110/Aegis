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
	if m.streamState.streaming {
		cmds = append(cmds, cmd) // always re-queue so animation resumes on scroll-back
		// P74.12: ease the status bar's displayed token counts toward the
		// real values every tick, independent of followBottom — cheap to
		// update even when not redrawn, so the counter is already caught up
		// by the time the view scrolls back into frame.
		m.easeStatCounters()
		// P3.7: suppress redraws when the "● thinking…" indicator is scrolled
		// off-screen — it lives at the viewport bottom, visible only when
		// followBottom is true.
		if m.streamState.followBottom {
			m.chrome.pollTick++
			// P74.10: reduced motion freezes animStep instead of advancing it,
			// which freezes shimmerText/caretGlyph/thinkingPhrase (they all
			// read it) and skips the re-render that would otherwise redraw an
			// unchanging frame every tick.
			if !m.chrome.reducedMotion {
				m.streamState.animStep++
				m.updatePendingToolCards() // P21.2: keep pending cards' shimmer live
				m.refresh()
			}
			// P2.5: poll sub-agent roster every 20 ticks. Keyed on pollTick
			// rather than animStep so reduced motion doesn't also silence it.
			if m.chrome.pollTick%20 == 0 {
				cmds = append(cmds, m.fetchTeammatesQuiet())
			}
		}
	}
	return m, cmds
}

// updateToastExpired retires the toast whose lifetime just elapsed. It only
// clears activeToast if the message names the toast currently shown (P63.10):
// two toasts in quick succession must not let the first one's timer cut the
// second one short.
func (m model) updateToastExpired(msg toastExpiredMsg) model {
	if m.overlays.activeToast == msg.t {
		m.overlays.activeToast = nil
	}
	return m
}

// easeStatCounters advances displayedInputTokens/displayedOutputTokens one
// frame toward inputTokens/outputTokens (P74.12): a gap-proportional step,
// large when far behind and small when close, so the status bar's counter
// climbs smoothly instead of jumping in chunk-sized increments each time a
// turn's usage lands. Reduced motion (P74.10) snaps straight to the true
// number instead of animating toward it.
func (m *model) easeStatCounters() {
	if m.chrome.reducedMotion {
		m.usage.displayedInputTokens = m.usage.inputTokens
		m.usage.displayedOutputTokens = m.usage.outputTokens
		return
	}
	m.usage.displayedInputTokens = easeTowards(m.usage.displayedInputTokens, m.usage.inputTokens)
	m.usage.displayedOutputTokens = easeTowards(m.usage.displayedOutputTokens, m.usage.outputTokens)
}

// easeTowards returns displayed moved one step toward target: an eighth of
// the remaining gap, floored at a step of 1 (or -1 when target dropped, e.g.
// a new run resetting the counter to zero) so it always converges rather than
// stalling just short of the target.
func easeTowards(displayed, target int) int {
	gap := target - displayed
	switch {
	case gap == 0:
		return displayed
	case gap > 0:
		if step := gap / 8; step > 1 {
			return displayed + step
		}
		return displayed + 1
	default:
		if step := gap / 8; step < -1 {
			return displayed + step
		}
		return displayed - 1
	}
}
