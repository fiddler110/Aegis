package tui

import (
	tea "charm.land/bubbletea/v2"
)

// updateFocus handles terminal focus tracking (P16.1), which always updates
// regardless of any open overlay — it carries no interaction semantics of its
// own, just suppression state for the attention system. The bool reports
// whether the message was claimed.
func (m model) updateFocus(msg tea.Msg) (model, tea.Cmd, bool) {
	switch msg.(type) {
	case tea.FocusMsg:
		m.focused = true
		// P33.10: regaining focus is a strong signal the user is about to type,
		// and enough idle time may have passed for Ollama to have unloaded the
		// model. Pre-warm now (gated on /api/ps inside the cmd) so the message
		// they send next doesn't open with a cold reload. No-op off Ollama.
		return m, m.maybeWarmOllamaCmd(), true
	case tea.BlurMsg:
		m.focused = false
		return m, nil, true
	case ollamaWarmedMsg:
		// P33.10: the pre-warm finished (or was gated out). It carries no UI
		// state — swallow it so it never reaches the composer's textarea update.
		return m, nil, true
	}
	return m, nil, false
}

// updateWizard delegates all messages while the wizard overlay is open.
func (m model) updateWizard(msg tea.Msg) (model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.wizard.width = ws.Width
		m.wizard.height = ws.Height
		m.layout()
		return m, nil
	}
	cmd := m.wizard.update(msg)
	if m.wizard.done {
		if m.wizard.saved {
			m.transcript.Append(
				m.th.statusText.Render("✓ Configuration saved — restart Aegis to apply changes.") + "\n\n",
			)
			// P69.6: a stated VRAM budget that could not size the window leaves
			// one thing for the user to finish, and the wizard closes too fast to
			// say so in its own view. The transcript is where it survives.
			if note := m.wizard.fitNote; note != "" {
				m.transcript.Append(m.th.statusDim.Render(note) + "\n\n")
			}
		}
		m.wizard = nil
		m.refresh()
	}
	return m, cmd
}

// updateSecurityConfig delegates all messages while the security-config
// overlay is open (P11.11).
func (m model) updateSecurityConfig(msg tea.Msg) (model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.securityConfig.width = ws.Width
		m.securityConfig.height = ws.Height
		m.layout()
		return m, nil
	}
	cmd := m.securityConfig.update(msg)
	if m.securityConfig.done {
		if m.securityConfig.saved {
			m.transcript.Append(
				m.th.statusText.Render("✓ Security config saved — restart Aegis to apply changes.") + "\n\n",
			)
		}
		m.securityConfig = nil
		m.refresh()
	}
	return m, cmd
}

// updateTransientPanel drives the transient informational panel (P33.11): a
// modal, scrollable overlay for read-only slash output (/status, /help,
// /memory …). Keys drive scroll/dismiss; a resize re-wraps it. Everything else
// is dropped rather than acted on while it's up — except the stream-lifecycle
// allowlist (P33.20), which must reach the main switch so a run's output is
// never swallowed by an open overlay. (A transient panel can't actually be open
// during a run today — slash commands only dispatch while !streaming and the
// panel captures input while up — but the allowlist keeps that a property of
// the dispatch gate rather than of this block.) A false bool means the message
// falls through to the main update path.
func (m model) updateTransientPanel(msg tea.Msg) (model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.transientPanel.resize(m.width, m.height)
		return m, nil, true
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "enter", "q":
			m.transientPanel = nil
			m.ta.Focus()
			m.refresh()
			return m, nil, true
		case "up", "k":
			m.transientPanel.scroll(-1)
			return m, nil, true
		case "down", "j":
			m.transientPanel.scroll(1)
			return m, nil, true
		// P40.2: u/d half-page mirror the transcript pane's vi vocabulary so
		// both scrollable content surfaces navigate identically.
		case "u", "ctrl+u":
			m.transientPanel.scroll(-m.transientPanel.height / 2)
			return m, nil, true
		case "d", "ctrl+d":
			m.transientPanel.scroll(m.transientPanel.height / 2)
			return m, nil, true
		case "pgup", "b", "ctrl+b":
			m.transientPanel.scroll(-m.transientPanel.height)
			return m, nil, true
		case "pgdown", "space", "f", "ctrl+f":
			m.transientPanel.scroll(m.transientPanel.height)
			return m, nil, true
		case "g", "home":
			m.transientPanel.scroll(-len(m.transientPanel.lines))
			return m, nil, true
		case "G", "end":
			m.transientPanel.scroll(len(m.transientPanel.lines))
			return m, nil, true
		}
		return m, nil, true // modal: swallow every other key
	}
	if !isStreamLifecycleMsg(msg) {
		return m, nil, true
	}
	// Stream-lifecycle event: fall through to the main switch.
	return m, nil, false
}

// updateQuitConfirm drives the quit-confirmation overlay: shown instead of
// quitting outright when a turn is streaming (ctrl+c / /quit / /exit) — P16.6.
func (m model) updateQuitConfirm(msg tea.Msg) (model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.layout()
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "y", "enter":
			if m.cancel != nil {
				m.cancel()
			}
			if m.termRun != nil {
				m.termRun.cancel()
			}
			saveStash(m.stashPath, m.ta.Value())
			return m, tea.Quit
		case "n", "esc":
			m.quitConfirm = false
		}
	}
	return m, nil
}

// updateHelp drives the help overlay: only Escape or F1 closes it.
func (m model) updateHelp(msg tea.Msg) (model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "esc" || k.String() == "f1" {
			m.helpOpen = false
		}
	}
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.layout()
	}
	return m, nil
}
