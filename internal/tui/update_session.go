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
			m.overlays.activeToast = t
			return m, cmd
		}
		return m, nil
	}
	if msg.err != nil {
		return m, m.overlays.dialog.setNotice("sessions: " + msg.err.Error())
	}
	if len(msg.items) == 0 {
		return m, m.overlays.dialog.setNotice("no sessions to switch to")
	}
	return m, m.overlays.dialog.setItems(sessionPickerItems(msg.items), sessionPickerH(m.chrome.height, len(msg.items)))
}

// updateSessionSwitched adopts a session the user switched to.
func (m model) updateSessionSwitched(msg sessionSwitchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t, cmd := newToastCmd("switch: "+msg.err.Error(), toastError)
		m.overlays.activeToast = t
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
			m.overlays.activeToast = t
			return m, cmd
		}
		return m, nil
	}
	if msg.err != nil {
		return m, m.overlays.dialog.setNotice("backtrack: " + msg.err.Error())
	}
	if len(msg.items) == 0 {
		return m, m.overlays.dialog.setNotice("no checkpoints yet — send a message first")
	}
	return m, m.overlays.dialog.setItems(backtrackPickerItems(msg.items), backtrackPickerH(m.chrome.height, len(msg.items)))
}

// updateForked adopts the session a backtrack fork created.
func (m model) updateForked(msg forkedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t, cmd := newToastCmd("fork: "+msg.err.Error(), toastError)
		m.overlays.activeToast = t
		return m, cmd
	}
	m.applySwitchedSession(msg.sess)
	if msg.prefill != "" {
		// P22.3: hand the original message back for editing rather than
		// resending it verbatim — the whole point of backtracking.
		m.ta.SetValue(msg.prefill)
	}
	t, cmd := newToastCmd(fmt.Sprintf("Forked into %q — edit and send to continue.", msg.title), toastInfo)
	m.overlays.activeToast = t
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
	m.streamState.lastAnswerBlock = nil
	m.streamState.thinkEntries = nil
	m.toolsUI.tools = m.toolsUI.tools[:0]
	m.usage.inputTokens, m.usage.outputTokens, m.usage.costUSD = 0, 0, 0
	m.usage.egressBytes = 0
	m.usage.displayedInputTokens, m.usage.displayedOutputTokens = 0, 0
	m.usage.cacheReadTokens, m.usage.cacheCreationTokens = 0, 0
	m.usage.tokensEstimated = false
	m.streamState.turnCount = 0
	m.sessionMeta.changedFiles = nil
	m.sessionMeta.teammates = nil
	m.sessionMeta.timelineEntries = nil
	m.toolsUI.state.pendingReadPaths = nil
	m.toolsUI.state.pendingTools = nil
	m.toolsUI.state.pendingToolOrder = nil
	m.toolsUI.state.activeReadGroup = nil
	m.toolsUI.state.soloReadCard = nil
	m.streamState.streaming = false
	m.status = "ready"
	m.sessionMeta.lastFailure = nil

	m.transcript.Append(buildWelcomeContent(m.cfg, m.sessionMeta.workDir, m.th))
	m.loadHistory(sess.Messages)
	m.streamState.followBottom = true
}

// loadHistory replays stored conversation messages into the transcript so a
// resumed session shows its prior turns (user text, assistant prose, and tool
// activity) using the same rendering as a live run.
func (m *model) loadHistory(msgs []provider.Message) {
	toolNames := map[string]string{} // tool_use ID → name, for labelling results
	toolPaths := map[string]string{} // tool_use ID → path, for read_file highlighting (P16.2)
	// writeCards holds a write_file call's transcript item and input, keyed by
	// tool_use ID (P64.4): appended immediately like every other tool's call
	// (so an orphaned call with no matching result still ends up in the
	// transcript, same as before this existed), but kept addressable so the
	// matching ToolResultBlock below can replace it in place with an accurate
	// diff once its Presentation payload — the file's actual prior content —
	// is available, mirroring what the live-streaming path does for the same
	// event via toolCard.
	writeCards := map[string]struct {
		item  *transcriptItem
		input json.RawMessage
	}{}
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
				if wc, ok := writeCards[r.ToolUseID]; ok {
					if old, ok := writePresentationOld(r.Presentation); ok {
						if s, ok := renderWriteDiffAgainst(m.th, name, wc.input, old, m.transcript.Width()); ok {
							m.transcript.SetItemRaw(wc.item, "\n"+s+"\n")
						}
					}
					delete(writeCards, r.ToolUseID)
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
						m.streamState.liveText.WriteString(v.Text)
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
					if v.Name == "write_file" {
						// Kept addressable so the matching ToolResultBlock
						// above can upgrade this to an accurate diff (P64.4);
						// appended now regardless, so an orphaned call still
						// shows up even if no result ever arrives.
						if item := m.transcript.AppendBlock("\n" + renderToolCall(m.th, v.Name, v.Input, m.transcript.Width()) + "\n"); item != nil {
							writeCards[v.ID] = struct {
								item  *transcriptItem
								input json.RawMessage
							}{item, v.Input}
						}
						continue
					}
					m.transcript.Append("\n" + renderToolCall(m.th, v.Name, v.Input, m.transcript.Width()) + "\n")
				}
			}
		}
	}
}
