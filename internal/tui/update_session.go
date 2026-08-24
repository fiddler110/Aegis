package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/session"
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

// applySwitchedSession swaps the active session, resetting per-session UI state
// and replaying the loaded transcript.
func (m *model) applySwitchedSession(sess *session.Session) {
	m.cfg.SessionID = sess.ID
	m.cfg.Mode = sess.Mode
	m.slash.SetSession(sess.ID, sess.Mode, sess.Model)
	// The status bar/sidebar model badge (m.cfg.Model) otherwise keeps
	// showing whatever session this replaced was on — SetSession above
	// already resolved sess.Model against the TUI's boot-time default, so
	// mirror that here the same way a successful "/model" does via
	// SlashResult.Model (P14.7's own display-sync mechanism, applied to a
	// session switch instead of a switch within one).
	m.cfg.Model = m.slash.EffectiveModel()

	m.transcript.Reset()
	m.lastAnswerBlock = nil
	m.thinkEntries = nil
	m.tools = m.tools[:0]
	m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
	m.displayedInputTokens, m.displayedOutputTokens = 0, 0
	m.cacheReadTokens, m.cacheCreationTokens = 0, 0
	m.tokensEstimated = false
	m.turnCount = 0
	m.changedFiles = nil
	m.teammates = nil
	m.timelineEntries = nil
	m.toolState.pendingReadPaths = nil
	m.toolState.pendingTools = nil
	m.toolState.pendingToolOrder = nil
	m.toolState.activeReadGroup = nil
	m.toolState.soloReadCard = nil
	m.streaming = false
	m.status = "ready"
	m.lastFailure = nil

	m.transcript.Append(buildWelcomeContent(m.cfg, m.workDir, m.th))
	m.loadHistory(sess.Messages)
	m.followBottom = true
}

// loadHistory replays stored conversation messages into the transcript so a
// resumed session shows its prior turns (user text, assistant prose, and tool
// activity) using the same rendering as a live run.
func (m *model) loadHistory(msgs []provider.Message) {
	toolNames := map[string]string{} // tool_use ID → name, for labelling results
	toolPaths := map[string]string{} // tool_use ID → path, for read_file highlighting (P16.2)
	for _, msg := range msgs {
		switch msg.Role {
		case provider.RoleUser:
			var text string
			var results []provider.ToolResultBlock
			var imageBlocks []provider.ImageBlock
			for _, b := range msg.Content {
				switch v := b.(type) {
				case provider.TextBlock:
					text += v.Text
				case provider.ToolResultBlock:
					results = append(results, v)
				case provider.ImageBlock:
					imageBlocks = append(imageBlocks, v)
				}
			}
			if len(results) == 0 {
				if len(imageBlocks) > 0 {
					suffix := ""
					if len(imageBlocks) != 1 {
						suffix = "s"
					}
					note := fmt.Sprintf("🖼 %d image%s", len(imageBlocks), suffix)
					if text != "" {
						text += "  " + note
					} else {
						text = "(" + note + ")"
					}
				}
				if text != "" {
					m.appendUser(text, m.renderImageThumbnailsFromBlocks(imageBlocks))
				}
			}
			for _, r := range results {
				name := toolNames[r.ToolUseID]
				if name == "" {
					name = "tool"
				}
				m.transcript.Append(renderToolResult(m.th, name, r.Content, r.IsError, m.transcript.Width(), m.toolMaxLines(), toolPaths[r.ToolUseID]) + "\n")
			}
		case provider.RoleAssistant:
			for _, b := range msg.Content {
				switch v := b.(type) {
				case provider.ThinkingBlock:
					if t := strings.TrimSpace(v.Text); t != "" {
						m.appendThinkingBlock(t, 0) // duration unknown for replayed turns
					}
				case provider.TextBlock:
					if v.Text != "" {
						m.liveText.WriteString(v.Text)
						m.flushLiveText()
					}
				case provider.ToolUseBlock:
					toolNames[v.ID] = v.Name
					if v.Name == "read_file" {
						var inp struct {
							Path string `json:"path"`
						}
						if json.Unmarshal(v.Input, &inp) == nil {
							toolPaths[v.ID] = inp.Path
						}
					}
					m.transcript.Append("\n" + renderToolCall(m.th, v.Name, v.Input, m.transcript.Width()) + "\n")
				}
			}
		}
	}
}
