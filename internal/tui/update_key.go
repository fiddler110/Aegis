package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/commands"
)

// updateKey handles a keypress that reached the main update path (every modal
// overlay declined it first). A false bool means the key was not claimed here
// and falls through to the composer, which forwards it to the textarea — the
// returned model carries any state the declining branches already changed.
func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// P40.3: transcript search is a modal input mode — while it is open it
	// owns every keypress (navigation, query editing, close). Non-key
	// messages (stream events, ticks, resize) still flow to the main switch,
	// so the transcript keeps updating live behind the search bar.
	if m.overlays.search != nil {
		mm, cmd := m.handleSearchKey(msg)
		return mm, cmd, true
	}

	// Terminal toggle: always available regardless of focus or streaming state.
	if key.Matches(msg, m.overlays.keys.Terminal) {
		m.toggleTerminal()
		return m, nil, true
	}

	// Route all input to the terminal pane while it holds keyboard focus.
	if m.splitTerm.termFocused {
		return m, m.handleTerminalKey(msg), true
	}

	// Approval dialog intercepts all keys while the engine is waiting for
	// user confirmation (TQ6): ↑/↓ + enter select an option; y/a/n/f are
	// shortcuts; unmatched keys fall through to viewport scrolling.
	if m.overlays.approval != nil {
		mm, cmd := m.handleApprovalKey(msg)
		return mm, cmd, true
	}

	// Inline completion popup intercepts navigation/accept keys first.
	// Other keys fall through to the textarea and trigger a recompute.
	if m.overlays.completion.active {
		switch msg.String() {
		case "up":
			m.overlays.completion.move(-1)
			return m, nil, true
		case "down", "ctrl+n":
			m.overlays.completion.move(1)
			return m, nil, true
		case "ctrl+p":
			m.overlays.completion.move(-1)
			return m, nil, true
		case "esc":
			m.overlays.completion = completionState{}
			m.refresh()
			return m, nil, true
		case "tab":
			return m, m.acceptCompletion(false), true
		case "enter":
			return m, m.acceptCompletion(true), true
		}
	}

	// P13.3.1: diagnose the last failed !-command, if any is pending.
	if key.Matches(msg, m.overlays.keys.Diagnose) {
		if cmd := m.diagnoseLastFailureCmd(); cmd != nil {
			return m, cmd, true
		}
	}

	// P75.1: expand/collapse the most recently resolved tool result or
	// read/search group in place, independent of the session-wide
	// /tools full|compact toggle.
	if key.Matches(msg, m.overlays.keys.ToolBlockToggle) {
		m.toggleLastToolBlock()
		m.refresh()
		return m, nil, true
	}

	// P40.1: resize the sidebar when it has focus (terminal-focused resize
	// is handled in handleTerminalKey). A no-op when the sidebar is closed
	// or too narrow to show — the keys then fall through harmlessly.
	if key.Matches(msg, m.overlays.keys.PaneNarrower) {
		if m.resizePane(-paneResizeStep) {
			return m, nil, true
		}
	}
	if key.Matches(msg, m.overlays.keys.PaneWider) {
		if m.resizePane(paneResizeStep) {
			return m, nil, true
		}
	}

	// P40.3: open the incremental transcript-search overlay. Handled here
	// (not in the msg.String() switch) so a rebind takes effect, and only
	// when it isn't already open — handleSearchKey above owns keys after
	// that. Available while streaming too: it only reads the transcript.
	if key.Matches(msg, m.overlays.keys.TranscriptSearch) {
		m.openSearch()
		return m, nil, true
	}

	// Sidebar toggle: was a hardcoded "ctrl+b" case in the switch below,
	// which meant a tui.keybindings override for sidebartoggle silently did
	// nothing — the same class of bug applyKeybindings' fail-fast validation
	// exists to prevent for the keys checked via key.Matches above.
	if key.Matches(msg, m.overlays.keys.SidebarToggle) {
		m.chrome.sidebarOpen = !m.chrome.sidebarOpen
		m.layout()
		m.refresh()
		return m, nil, true
	}

	switch msg.String() {
	case "esc", "alt+esc":
		if m.streamState.streaming {
			// P33.5: a single ESC cancels the run. Text in the input box
			// still gets cleared first, so there the interrupt is the next
			// press — except when a fast double-tap lands both ESC bytes in
			// the same terminal read, likeliest exactly here while streaming
			// keeps the reader busy re-rendering. Ultraviolet's decoder
			// reports that as one "alt+esc" event instead of two separate
			// "esc" ones, so treat it as the clear plus the confirmed
			// interrupt it was meant to be.
			if strings.TrimSpace(m.ta.Value()) != "" {
				m.ta.Reset()
				if msg.String() != "alt+esc" {
					m.composer.backtrackArmed = false
					return m, nil, true
				}
			}
			// An explicit interrupt also discards any queued messages
			// (TQ8) — auto-sending after the user hit the brakes would be
			// a surprise.
			if m.streamState.cancel != nil {
				m.streamState.cancel()
			}
			m.composer.backtrackArmed = false
			m.composer.queued = nil
			m.composer.interrupted = true
			m.refresh()
			return m, nil, true
		}
		// Not streaming: an empty input box has nothing to clear, so a
		// genuine second Esc press there opens the P22.3 backtrack
		// picker instead of the no-op this used to be — same double-tap
		// detection as the streaming branch above, including its
		// documented same-frame alt+esc quirk.
		if strings.TrimSpace(m.ta.Value()) == "" {
			if m.composer.backtrackArmed || msg.String() == "alt+esc" {
				m.composer.backtrackArmed = false
				m.refresh()
				picker := newBacktrackPicker(m.chrome.width, m.chrome.height, m.sp.View())
				m.overlays.dialog = &picker
				return m, tea.Batch(m.fetchBacktrackTargets(), m.sp.Tick), true
			}
			m.composer.backtrackArmed = true
			m.refresh()
			return m, nil, true
		}
		m.ta.Reset()
		m.composer.backtrackArmed = false
		return m, nil, true

	case "ctrl+c":
		if m.streamState.streaming && m.streamState.cancel != nil {
			m.streamState.cancel() // interrupt the in-flight run; press again to quit
			m.composer.backtrackArmed = false
			m.composer.queued = nil // TQ8: explicit interrupt discards the queue
			m.composer.interrupted = true
			return m, nil, true
		}
		if m.streamState.cancel != nil {
			m.streamState.cancel()
		}
		if m.splitTerm.termRun != nil {
			m.splitTerm.termRun.cancel()
		}
		saveStash(m.sessionMeta.stashPath, m.ta.Value())
		return m, tea.Quit, true
	case "ctrl+o":
		// TQ9: expand/collapse all thinking blocks in the transcript.
		m.toggleThinking()
		return m, nil, true
	case "ctrl+t":
		return m, m.fetchTeammates(), true
	case "ctrl+y":
		if !m.streamState.streaming {
			picker := newSessionPicker(m.chrome.width, m.chrome.height, m.sp.View())
			m.overlays.dialog = &picker
			return m, tea.Batch(m.fetchSessions(), m.sp.Tick), true
		}
	case "ctrl+r":
		// P22.4: reverse-search over sent-message history, like a shell's
		// Ctrl+R — moved the session switcher to ctrl+y to free this key
		// up for the muscle-memory binding shell users expect.
		if !m.streamState.streaming {
			if len(m.composer.history) == 0 {
				t, cmd := newToastCmd("no input history yet", toastInfo)
				m.overlays.activeToast = t
				return m, cmd, true
			}
			m.overlays.completion = completionState{}
			picker := newHistoryPicker(m.chrome.width, m.chrome.height, m.composer.history)
			m.overlays.dialog = &picker
			return m, nil, true
		}
	case "ctrl+l":
		if !m.streamState.streaming {
			return m, m.handleSlashCommand(&commands.ParsedCommand{Name: "clear", Raw: "/clear"}), true
		}
	case "ctrl+k":
		if !m.streamState.streaming {
			m.overlays.completion = completionState{}
			pal := newPalette(m.chrome.width, m.chrome.height, m.commandEntries())
			m.overlays.dialog = &pal
			return m, nil, true
		}
	case "f1":
		m.overlays.helpOpen = !m.overlays.helpOpen
		return m, nil, true
	case "ctrl+e":
		if !m.streamState.streaming {
			return m, m.openEditorCmd(), true
		}
	case "ctrl+v":
		return m, pasteClipboardImageCmd(), true
	case "shift+tab", "ctrl+tab":
		if !m.streamState.streaming {
			return m, m.cycleModeCmd(), true
		}
	case "up":
		// TQ9: within a multiline draft ↑ moves the cursor; history
		// navigation only triggers when the cursor is already on the first
		// line (the standard Claude Code/opencode behaviour).
		if !m.streamState.streaming && m.ta.Line() == 0 && len(m.composer.history) > 0 {
			if m.composer.histIdx == -1 {
				m.composer.draftInput = m.ta.Value()
				m.composer.histIdx = len(m.composer.history) - 1
			} else if m.composer.histIdx > 0 {
				m.composer.histIdx--
			}
			m.ta.SetValue(m.composer.history[m.composer.histIdx])
			return m, nil, true
		}
	case "down":
		// TQ9: mirror of ↑ — only leave history when the cursor sits on
		// the last line of the recalled entry.
		if !m.streamState.streaming && m.composer.histIdx != -1 && m.ta.Line() == m.ta.LineCount()-1 {
			if m.composer.histIdx == len(m.composer.history)-1 {
				m.composer.histIdx = -1
				m.ta.SetValue(m.composer.draftInput)
				m.composer.draftInput = ""
			} else {
				m.composer.histIdx++
				m.ta.SetValue(m.composer.history[m.composer.histIdx])
			}
			return m, nil, true
		}
	case "enter":
		if m.streamState.streaming {
			// TQ8: while the model is running, Enter queues the draft as the
			// next user turn; it auto-sends when the current run finishes.
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil, true
			}
			m.ta.Reset()
			m.composer.backtrackArmed = false
			m.composer.queued = append(m.composer.queued, text)
			m.streamState.followBottom = true
			m.applyViewportHeight() // ta was just Reset; resync pane height
			m.refresh()
			return m, nil, true
		}
		text := strings.TrimSpace(m.ta.Value())
		if text == "" {
			return m, nil, true
		}
		// P2.2: Bang ! shell mode — execute the rest as a shell command.
		if strings.HasPrefix(text, "!") {
			shellCmd := strings.TrimSpace(text[1:])
			if shellCmd == "" {
				return m, nil, true
			}
			m.ta.Reset()
			m.composer.history = append(m.composer.history, text)
			m.composer.histIdx = -1
			m.composer.draftInput = ""
			return m, m.execBangCmd(shellCmd), true
		}
		if parsed := commands.Parse(text); parsed != nil {
			m.ta.Reset()
			m.composer.histIdx = -1
			m.composer.draftInput = ""
			return m, m.dispatchSlash(parsed), true
		}
		m.ta.Reset()
		return m, m.sendUserMessage(text), true

	case "alt+enter":
		// P33.8: while streaming, alt+enter injects the draft as a steering
		// message between tool rounds instead of queueing it. When idle it
		// behaves like a plain send.
		text := strings.TrimSpace(m.ta.Value())
		if text == "" {
			return m, nil, true
		}
		m.ta.Reset()
		if m.streamState.streaming {
			m.composer.backtrackArmed = false
			m.composer.pendingSteers = append(m.composer.pendingSteers, pendingSteerEntry{text: text, origin: steerOriginUser})
			m.streamState.followBottom = true
			m.applyViewportHeight() // ta was just Reset; resync pane height
			m.refresh()
			return m, m.sendSteerCmd(text, steerOriginUser), true
		}
		return m, m.sendUserMessage(text), true
	}

	return m, nil, false
}
