package tui

import (
	tea "charm.land/bubbletea/v2"
)

// updateWindowSize re-lays-out for a terminal resize. It does not consume the
// message: the composer still forwards it to the textarea.
func (m model) updateWindowSize(msg tea.WindowSizeMsg) model {
	m.chrome.width, m.chrome.height = msg.Width, msg.Height
	m.layout()
	m.refresh()
	m.chrome.ready = true
	return m
}

// updateBackgroundColor handles the terminal's answer to our
// RequestBackgroundColor (P40.5). Only act in "auto" mode (an explicit /theme
// opts out by clearing autoTheme). Pick light vs. dark and rebuild the
// styles/renderer the same way the live /theme switch does — the provisional
// scheme was bound at startup.
func (m model) updateBackgroundColor(msg tea.BackgroundColorMsg) model {
	if m.chrome.autoTheme {
		want := "dark"
		if !msg.IsDark() {
			want = "light"
		}
		if want != m.cfg.Theme {
			m.cfg.Theme = applyTheme(want, m.cfg.WorkDir)
			m.th = newTheme()
			// rendererW is 0 until the first WindowSizeMsg; if this arrives
			// first, layout() rebuilds the renderer with the new glamour
			// style on the imminent resize, so only rebuild here once sized.
			if m.streamState.rendererW > 0 {
				m.streamState.renderer = newGlamourRenderer(m.streamState.rendererW)
			}
			m.refresh()
		}
	}
	return m
}
