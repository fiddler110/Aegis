package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// updateComposer is the tail of the update path: it forwards whatever no
// domain handler claimed to the composer's textarea, then applies the scroll
// and mouse handling that rides on the same message.
func (m model) updateComposer(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	// The approval dialog owns all input while open (P25.4a): tea.KeyMsg
	// already returns before reaching here, but this guard covers
	// every other message type too so no future case can leak a keystroke,
	// paste, or other input into the composer out from under the dialog.
	if m.approval == nil {
		var cmd tea.Cmd
		prevTAH := m.ta.Height()
		prevEmpty := strings.TrimSpace(m.ta.Value()) == ""
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
		// Recompute inline completion after the textarea consumes the key.
		if _, isKey := msg.(tea.KeyMsg); isKey {
			// With DynamicHeight, typing or deleting can grow/shrink the textarea;
			// update the viewport height immediately so it never overlaps the input.
			if m.ta.Height() != prevTAH {
				m.applyViewportHeight()
			}
			// P33.10 first-keystroke pre-warm: the composer just went from empty
			// to non-empty — the user has started a new message. Kick off a warm
			// load (gated on /api/ps unloaded inside the cmd) so it overlaps the
			// typing latency instead of stalling the send. warmPinged debounces
			// it to once per empty→typing transition; it resets when the composer
			// goes empty again below.
			nowEmpty := strings.TrimSpace(m.ta.Value()) == ""
			if prevEmpty && !nowEmpty && !m.warmPinged {
				if c := m.maybeWarmOllamaCmd(); c != nil {
					m.warmPinged = true
					cmds = append(cmds, c)
				}
			} else if nowEmpty {
				m.warmPinged = false
			}
			m.syncCompletion()
			// Any non-ESC key while backtrackArmed clears the not-streaming
			// double-tap-to-backtrack arm state (P22.3) — the ESC case already
			// returns early and manages backtrackArmed itself.
			if m.backtrackArmed {
				m.backtrackArmed = false
				m.refresh()
			}
		}
	}
	switch tmsg := msg.(type) {
	case tea.KeyMsg:
		// P21.7: while the textarea owns typed input, only the dedicated page
		// keys scroll the transcript. Forwarding every key (the old "known
		// existing quirk") meant typing any 'u'/'k'/'b'/space — or pressing the
		// arrow keys to edit the draft — both edited the text AND scrolled the
		// transcript, which silently killed auto-follow mid-stream. The vi-style
		// scroll keys still work where the textarea isn't capturing input (the
		// approval dialog's fall-through path in approval.go).
		switch tmsg.String() {
		case "pgup", "pgdown":
			if m.transcript.HandleKey(tmsg) {
				m.followBottom = m.transcript.AtBottom()
			}
		}
	case tea.MouseWheelMsg:
		if m.transcript.HandleMouseWheel(tmsg) {
			m.followBottom = m.transcript.AtBottom()
		}
	case tea.MouseClickMsg:
		cmds = append(cmds, m.handleMouseClick(tmsg))
	case tea.MouseMotionMsg:
		m.handleMouseMotion(tmsg)
	case tea.MouseReleaseMsg:
		cmds = append(cmds, m.handleMouseRelease(tmsg))
	}
	return m, cmds
}
