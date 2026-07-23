package tui

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/commands"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Focus tracking (P16.1) always updates regardless of any open overlay —
	// it carries no interaction semantics of its own, just suppression state
	// for the attention system.
	switch msg.(type) {
	case tea.FocusMsg:
		m.focused = true
		// P33.10: regaining focus is a strong signal the user is about to type,
		// and enough idle time may have passed for Ollama to have unloaded the
		// model. Pre-warm now (gated on /api/ps inside the cmd) so the message
		// they send next doesn't open with a cold reload. No-op off Ollama.
		return m, m.maybeWarmOllamaCmd()
	case tea.BlurMsg:
		m.focused = false
		return m, nil
	case ollamaWarmedMsg:
		// P33.10: the pre-warm finished (or was gated out). It carries no UI
		// state — swallow it so it never reaches the composer's textarea update.
		return m, nil
	}

	// Wizard overlay: delegate all messages while the wizard is open.
	if m.wizard != nil {
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
			}
			m.wizard = nil
			m.refresh()
		}
		return m, cmd
	}

	// Security-config overlay: delegate all messages while it's open (P11.11).
	if m.securityConfig != nil {
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

	// Transient informational panel (P33.11): a modal, scrollable overlay for
	// read-only slash output (/status, /help, /memory …). Keys drive
	// scroll/dismiss; a resize re-wraps it. Everything else is dropped rather
	// than acted on while it's up — except the stream-lifecycle allowlist
	// (P33.20), which must reach the main switch so a run's output is never
	// swallowed by an open overlay. (A transient panel can't actually be open
	// during a run today — slash commands only dispatch while !streaming and
	// the panel captures input while up — but the allowlist keeps that a
	// property of the dispatch gate rather than of this block.)
	if m.transientPanel != nil {
		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			m.width, m.height = msg.Width, msg.Height
			m.layout()
			m.transientPanel.resize(m.width, m.height)
			return m, nil
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "enter", "q":
				m.transientPanel = nil
				m.ta.Focus()
				m.refresh()
				return m, nil
			case "up", "k":
				m.transientPanel.scroll(-1)
				return m, nil
			case "down", "j":
				m.transientPanel.scroll(1)
				return m, nil
			// P40.2: u/d half-page mirror the transcript pane's vi vocabulary so
			// both scrollable content surfaces navigate identically.
			case "u", "ctrl+u":
				m.transientPanel.scroll(-m.transientPanel.height / 2)
				return m, nil
			case "d", "ctrl+d":
				m.transientPanel.scroll(m.transientPanel.height / 2)
				return m, nil
			case "pgup", "b", "ctrl+b":
				m.transientPanel.scroll(-m.transientPanel.height)
				return m, nil
			case "pgdown", "space", "f", "ctrl+f":
				m.transientPanel.scroll(m.transientPanel.height)
				return m, nil
			case "g", "home":
				m.transientPanel.scroll(-len(m.transientPanel.lines))
				return m, nil
			case "G", "end":
				m.transientPanel.scroll(len(m.transientPanel.lines))
				return m, nil
			}
			return m, nil // modal: swallow every other key
		}
		if !isStreamLifecycleMsg(msg) {
			return m, nil
		}
		// Stream-lifecycle event: fall through to the main switch below.
	}

	// Dialog overlay (command palette, persona/session/timeline/model picker):
	// route all input to it. Result messages are handled here so they are not
	// re-intercepted by this same block on the next tick (the overlay would
	// swallow them otherwise). P16.6 collapsed four near-identical blocks
	// into one, dispatching by dialog.kind.
	if m.dialog != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = ws.Width, ws.Height
			m.layout()
			return m, nil
		}
		// P33.7: keep the loading row's spinner turning. The tick is claimed
		// here rather than left to the spinner.TickMsg case below, which this
		// block returns ahead of, and re-queued only while a fetch is actually
		// outstanding — once the rows land it stops on its own.
		if _, ok := msg.(spinner.TickMsg); ok && m.dialog.loading {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			m.dialog.setLoadingFrame(m.sp.View())
			return m, cmd
		}
		if c, ok := msg.(dialogCancelMsg); ok && c.kind == m.dialog.kind {
			m.dialog = nil
			m.ta.Focus()
			if c.kind == dialogPersonaPicker {
				m.refresh()
			}
			if c.kind == dialogThreatModelPicker {
				m.pendingThreatModelTarget = ""
			}
			return m, nil
		}
		if sel, ok := msg.(dialogSelectedMsg); ok && sel.kind == m.dialog.kind {
			kind := m.dialog.kind
			m.dialog = nil
			m.ta.Focus()
			switch kind {
			case dialogPalette:
				item := sel.item.(paletteItem)
				needsArgs := map[string]bool{"mode": true, "remember": true}
				if needsArgs[item.name] {
					m.ta.SetValue("/" + item.name + " ")
					return m, nil
				}
				parsed := &commands.ParsedCommand{Name: item.name, Raw: "/" + item.name}
				return m, m.dispatchSlash(parsed)
			case dialogPersonaPicker:
				item := sel.item.(personaItem)
				parsed := &commands.ParsedCommand{Name: "persona", Args: []string{item.name}, Raw: "/persona " + item.name}
				return m, m.handleSlashCommand(parsed)
			case dialogSessionPicker:
				item := sel.item.(sessionItem)
				if item.id == m.cfg.SessionID {
					return m, nil // already on this session
				}
				return m, m.switchSessionCmd(item.id)
			case dialogTimelinePicker:
				item := sel.item.(timelineItem)
				// Scroll to the selected turn's recorded item position.
				m.transcript.ScrollToItem(item.e.blockIndex)
				m.followBottom = false
				m.refresh()
				return m, nil
			case dialogModelPicker:
				item := sel.item.(modelItem)
				parsed := &commands.ParsedCommand{Name: "model", Args: []string{item.id}, Raw: "/model " + item.id}
				return m, m.handleSlashCommand(parsed)
			case dialogThreatModelPicker:
				item := sel.item.(frameworkItem)
				target := m.pendingThreatModelTarget
				m.pendingThreatModelTarget = ""
				args := strings.Fields(item.name) // splits "NIST 800-154" into the two tokens extractThreatModelFramework expects
				if target != "" {
					args = append(args, target)
				}
				parsed := &commands.ParsedCommand{Name: "threat-model", Args: args, Raw: "/threat-model " + strings.Join(args, " ")}
				return m, m.handleSlashCommand(parsed)
			case dialogHistoryPicker:
				item := sel.item.(historyItem)
				// Recall the entry onto the input line for further editing or
				// sending, same as a shell reverse-search accepting a match —
				// it does not send immediately.
				m.ta.SetValue(item.text)
				m.histIdx = -1
				m.draftInput = ""
				return m, nil
			case dialogBacktrackPicker:
				item := sel.item.(backtrackItem)
				// P22.3: fork at that turn's checkpoint and pre-fill its
				// original text so the user edits before resending, rather
				// than the plain "load onto the input line" the history
				// picker above does — the picked entry has already been sent
				// once in the (now-untouched) source session.
				return m, m.forkAndSwitchCmd(item.cpID, item.text)
			}
			return m, nil
		}
		// P33.20 allowlist: message types that must always reach the main update
		// path rather than be swallowed by this open-dialog block. The two
		// *LoadedMsg types are the async fetches that fill a picker opened on its
		// loading row (P33.7) — routing them into the list instead would leave
		// the spinner up forever. The stream-lifecycle events are here for the
		// reason P33.20 filed: an overlay left open during a run (a P33.11
		// transient panel, or any future one) would otherwise drop the run's
		// streamed output. Expressed as one allowlist so it doesn't accrete
		// per-message fall-through patches one item at a time.
		switch msg.(type) {
		case sessionsLoadedMsg, backtrackTargetsMsg,
			streamStartedMsg, eventMsg, batchEventMsg, streamClosedMsg, errMsg, steerFailedMsg:
		case slashResultMsg:
			// P33.13: the persona picker opens ahead of its data through the
			// same generic slashResultMsg every other slash command uses, so
			// (unlike sessionsLoadedMsg/backtrackTargetsMsg, each dedicated to
			// one picker) it can only fall through here while the dialog on
			// screen is actually the persona picker awaiting it — anything
			// else stays swallowed by the dialog below, same as always.
			if m.dialog.kind != dialogPersonaPicker {
				updated, cmd := m.dialog.Update(msg)
				m.dialog = &updated
				return m, cmd
			}
		default:
			updated, cmd := m.dialog.Update(msg)
			m.dialog = &updated
			return m, cmd
		}
	}

	// Quit-confirmation overlay: shown instead of quitting outright when a
	// turn is streaming (ctrl+c / /quit / /exit) — P16.6.
	if m.quitConfirm {
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
				saveStash(m.stashPath, m.ta.Value())
				return m, tea.Quit
			case "n", "esc":
				m.quitConfirm = false
			}
		}
		return m, nil
	}

	// Help overlay: only Escape or F1 closes it.
	if m.helpOpen {
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

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refresh()
		m.ready = true

	case tea.BackgroundColorMsg:
		// P40.5: the terminal answered our RequestBackgroundColor. Only act in
		// "auto" mode (an explicit /theme opts out by clearing autoTheme). Pick
		// light vs. dark and rebuild the styles/renderer the same way the live
		// /theme switch does — the provisional scheme was bound at startup.
		if m.autoTheme {
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
				if m.rendererW > 0 {
					m.renderer = newGlamourRenderer(m.rendererW)
				}
				m.refresh()
			}
		}

	case spinner.TickMsg:
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

	case toastExpiredMsg:
		m.activeToast = nil

	case clipboardResultMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("copy: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		t, cmd := newToastCmd("copied to clipboard", toastInfo)
		m.activeToast = t
		return m, cmd

	case pasteImageResultMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("paste image: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		if !msg.ok {
			t, cmd := newToastCmd("clipboard has no image", toastInfo)
			m.activeToast = t
			return m, cmd
		}
		m.ta.InsertString(attachTokenFor(msg.path))
		t, cmd := newToastCmd("image attached: "+filepath.Base(msg.path), toastInfo)
		m.activeToast = t
		return m, cmd

	case editorDoneMsg:
		m.ta.Focus()
		if msg.err != nil {
			t, cmd := newToastCmd("editor: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		if strings.TrimSpace(msg.content) != "" {
			m.ta.SetValue(msg.content)
		}

	case tea.PasteMsg:
		// TQ9: a pasted image path becomes an attachment token instead of raw
		// text, replacing the typed @image:<path> incantation. Anything else
		// falls through to the textarea's own paste handling. The approval
		// dialog owns all input while open (P25.4a), same as it does for
		// tea.KeyMsg below, so a paste can't land silently in the composer.
		if !m.termFocused && m.approval == nil {
			p := strings.TrimSpace(msg.Content)
			// Windows "Copy as path" and shell copies often quote the path.
			if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
				p = p[1 : len(p)-1]
			}
			if looksLikeImagePath(p) {
				m.ta.InsertString(attachTokenFor(p))
				t, cmd := newToastCmd("image attached: "+filepath.Base(p), toastInfo)
				m.activeToast = t
				return m, cmd
			}
		}

	case tea.KeyMsg:
		// Terminal toggle: always available regardless of focus or streaming state.
		if key.Matches(msg, m.keys.Terminal) {
			m.toggleTerminal()
			return m, nil
		}

		// Route all input to the terminal pane while it holds keyboard focus.
		if m.termFocused {
			return m, m.handleTerminalKey(msg)
		}

		// Approval dialog intercepts all keys while the engine is waiting for
		// user confirmation (TQ6): ↑/↓ + enter select an option; y/a/n/f are
		// shortcuts; unmatched keys fall through to viewport scrolling.
		if m.approval != nil {
			return m.handleApprovalKey(msg)
		}

		// Inline completion popup intercepts navigation/accept keys first.
		// Other keys fall through to the textarea and trigger a recompute.
		if m.completion.active {
			switch msg.String() {
			case "up":
				m.completion.move(-1)
				return m, nil
			case "down", "ctrl+n":
				m.completion.move(1)
				return m, nil
			case "ctrl+p":
				m.completion.move(-1)
				return m, nil
			case "esc":
				m.completion = completionState{}
				m.refresh()
				return m, nil
			case "tab":
				return m, m.acceptCompletion(false)
			case "enter":
				return m, m.acceptCompletion(true)
			}
		}

		// P13.3.1: diagnose the last failed !-command, if any is pending.
		if key.Matches(msg, m.keys.Diagnose) {
			if cmd := m.diagnoseLastFailureCmd(); cmd != nil {
				return m, cmd
			}
		}

		// P40.1: resize the sidebar when it has focus (terminal-focused resize
		// is handled in handleTerminalKey). A no-op when the sidebar is closed
		// or too narrow to show — the keys then fall through harmlessly.
		if key.Matches(msg, m.keys.PaneNarrower) {
			if m.resizePane(-paneResizeStep) {
				return m, nil
			}
		}
		if key.Matches(msg, m.keys.PaneWider) {
			if m.resizePane(paneResizeStep) {
				return m, nil
			}
		}

		switch msg.String() {
		case "esc", "alt+esc":
			if m.streaming {
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
						m.backtrackArmed = false
						return m, nil
					}
				}
				// An explicit interrupt also discards any queued messages
				// (TQ8) — auto-sending after the user hit the brakes would be
				// a surprise.
				if m.cancel != nil {
					m.cancel()
				}
				m.backtrackArmed = false
				m.queued = nil
				m.interrupted = true
				m.refresh()
				return m, nil
			}
			// Not streaming: an empty input box has nothing to clear, so a
			// genuine second Esc press there opens the P22.3 backtrack
			// picker instead of the no-op this used to be — same double-tap
			// detection as the streaming branch above, including its
			// documented same-frame alt+esc quirk.
			if strings.TrimSpace(m.ta.Value()) == "" {
				if m.backtrackArmed || msg.String() == "alt+esc" {
					m.backtrackArmed = false
					m.refresh()
					picker := newBacktrackPicker(m.width, m.height, m.sp.View())
					m.dialog = &picker
					return m, tea.Batch(m.fetchBacktrackTargets(), m.sp.Tick)
				}
				m.backtrackArmed = true
				m.refresh()
				return m, nil
			}
			m.ta.Reset()
			m.backtrackArmed = false
			return m, nil

		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel() // interrupt the in-flight run; press again to quit
				m.backtrackArmed = false
				m.queued = nil // TQ8: explicit interrupt discards the queue
				m.interrupted = true
				return m, nil
			}
			if m.cancel != nil {
				m.cancel()
			}
			saveStash(m.stashPath, m.ta.Value())
			return m, tea.Quit
		case "ctrl+b":
			m.sidebarOpen = !m.sidebarOpen
			m.layout()
			m.refresh()
			return m, nil
		case "ctrl+o":
			// TQ9: expand/collapse all thinking blocks in the transcript.
			m.toggleThinking()
			return m, nil
		case "ctrl+t":
			return m, m.fetchTeammates()
		case "ctrl+y":
			if !m.streaming {
				picker := newSessionPicker(m.width, m.height, m.sp.View())
				m.dialog = &picker
				return m, tea.Batch(m.fetchSessions(), m.sp.Tick)
			}
		case "ctrl+r":
			// P22.4: reverse-search over sent-message history, like a shell's
			// Ctrl+R — moved the session switcher to ctrl+y to free this key
			// up for the muscle-memory binding shell users expect.
			if !m.streaming {
				if len(m.history) == 0 {
					t, cmd := newToastCmd("no input history yet", toastInfo)
					m.activeToast = t
					return m, cmd
				}
				m.completion = completionState{}
				picker := newHistoryPicker(m.width, m.height, m.history)
				m.dialog = &picker
				return m, nil
			}
		case "ctrl+l":
			if !m.streaming {
				return m, m.handleSlashCommand(&commands.ParsedCommand{Name: "clear", Raw: "/clear"})
			}
		case "ctrl+k":
			if !m.streaming {
				m.completion = completionState{}
				pal := newPalette(m.width, m.height, m.commandEntries())
				m.dialog = &pal
				return m, nil
			}
		case "f1":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "ctrl+e":
			if !m.streaming {
				return m, m.openEditorCmd()
			}
		case "ctrl+v":
			return m, pasteClipboardImageCmd()
		case "shift+tab":
			if !m.streaming {
				return m, m.cycleModeCmd()
			}
		case "up":
			// TQ9: within a multiline draft ↑ moves the cursor; history
			// navigation only triggers when the cursor is already on the first
			// line (the standard Claude Code/opencode behaviour).
			if !m.streaming && m.ta.Line() == 0 && len(m.history) > 0 {
				if m.histIdx == -1 {
					m.draftInput = m.ta.Value()
					m.histIdx = len(m.history) - 1
				} else if m.histIdx > 0 {
					m.histIdx--
				}
				m.ta.SetValue(m.history[m.histIdx])
				return m, nil
			}
		case "down":
			// TQ9: mirror of ↑ — only leave history when the cursor sits on
			// the last line of the recalled entry.
			if !m.streaming && m.histIdx != -1 && m.ta.Line() == m.ta.LineCount()-1 {
				if m.histIdx == len(m.history)-1 {
					m.histIdx = -1
					m.ta.SetValue(m.draftInput)
					m.draftInput = ""
				} else {
					m.histIdx++
					m.ta.SetValue(m.history[m.histIdx])
				}
				return m, nil
			}
		case "enter":
			if m.streaming {
				// TQ8: while the model is running, Enter queues the draft as the
				// next user turn; it auto-sends when the current run finishes.
				text := strings.TrimSpace(m.ta.Value())
				if text == "" {
					return m, nil
				}
				m.ta.Reset()
				m.backtrackArmed = false
				m.queued = append(m.queued, text)
				m.followBottom = true
				m.applyViewportHeight() // ta was just Reset; resync pane height
				m.refresh()
				return m, nil
			}
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			// P2.2: Bang ! shell mode — execute the rest as a shell command.
			if strings.HasPrefix(text, "!") {
				shellCmd := strings.TrimSpace(text[1:])
				if shellCmd == "" {
					return m, nil
				}
				m.ta.Reset()
				m.history = append(m.history, text)
				m.histIdx = -1
				m.draftInput = ""
				return m, m.execBangCmd(shellCmd)
			}
			if parsed := commands.Parse(text); parsed != nil {
				m.ta.Reset()
				m.histIdx = -1
				m.draftInput = ""
				return m, m.dispatchSlash(parsed)
			}
			m.ta.Reset()
			return m, m.sendUserMessage(text)

		case "alt+enter":
			// P33.8: while streaming, alt+enter injects the draft as a steering
			// message between tool rounds instead of queueing it. When idle it
			// behaves like a plain send.
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			m.ta.Reset()
			if m.streaming {
				m.backtrackArmed = false
				m.pendingSteers = append(m.pendingSteers, pendingSteerEntry{text: text, origin: steerOriginUser})
				m.followBottom = true
				m.applyViewportHeight() // ta was just Reset; resync pane height
				m.refresh()
				return m, m.sendSteerCmd(text, steerOriginUser)
			}
			return m, m.sendUserMessage(text)
		}

	case streamStartedMsg:
		m.events = msg.ch
		m.cancel = msg.cancel
		m.streamStart = time.Now()
		m.backtrackArmed = false
		m.interrupted = false
		m.setQueueMode(true)
		return m, tea.Batch(waitForEvent(m.events), m.sp.Tick)

	case eventMsg:
		// Single-event path (kept for direct drivers such as the integration
		// tests); the live stream arrives as batchEventMsg. Both share
		// applyStreamBatch so the follow-bottom and notify bookkeeping stay
		// identical.
		notifyCmd := m.applyStreamBatch([]api.Event{api.Event(msg)})
		return m, tea.Batch(waitForEvent(m.events), notifyCmd)

	case batchEventMsg:
		notifyCmd := m.applyStreamBatch(msg.events)
		if msg.closed {
			// The stream closed within this same drain: run the exact
			// teardown the dedicated streamClosedMsg path does, on the
			// already-applied state, and carry any per-event notification
			// (e.g. a cost alert) alongside the closed path's own command.
			nm, closeCmd := m.Update(streamClosedMsg{})
			return nm, tea.Batch(notifyCmd, closeCmd)
		}
		return m, tea.Batch(waitForEvent(m.events), notifyCmd)

	case streamClosedMsg:
		m.flushThinking()
		m.flushLiveText() // safety: in case KindTurnDone wasn't the last event
		// P21.2: the universal safety net for a stuck-pending tool card — the
		// stream can close without KindError or KindDone at all on a
		// client-initiated cancel (engine.ErrInterrupted's callers return
		// before emitting anything), so this is the one place guaranteed to
		// run after every kind of run end. A no-op if KindError already
		// resolved everything.
		m.resolveStuckToolCards()
		m.streaming = false
		m.events = nil
		m.cancel = nil
		m.status = "ready"
		m.backtrackArmed = false
		m.setQueueMode(false)
		m.transcript.Append("\n")
		// P33.2: a steer the daemon never reported a verdict on — an older
		// daemon that doesn't emit KindSteerUnconsumed at all, or an event the
		// SSE buffer dropped — is unconsumed by definition once the stream is
		// gone, so treat it as such instead of leaving its echo dangling.
		for _, st := range m.pendingSteers {
			m.requeueSteer(st.text, st.origin)
		}
		m.pendingSteers = nil
		// TQ8: auto-send the next queued message, one per completed run. Don't
		// notify here — another run is about to start immediately.
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m, m.sendUserMessage(next)
		}
		m.refresh()
		return m, m.notifyCmd(notify.Event{Title: "Aegis", Body: "Run finished"})

	case errMsg:
		m.streaming = false
		m.backtrackArmed = false
		m.setQueueMode(false)
		m.transcript.Append(m.th.errLine.Render("error: "+msg.err.Error()) + "\n\n")
		// TQ8: don't auto-send into a failing session.
		if len(m.queued) > 0 {
			m.queued = nil
			m.transcript.Append(m.th.statusDim.Render("⏳ queued messages discarded after error") + "\n\n")
		}
		for _, st := range m.pendingSteers {
			m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered: "+oneLine(st.text)) + "\n\n")
		}
		m.pendingSteers = nil
		m.status = "ready"
		m.refresh()
		return m, m.notifyCmd(notify.Event{Title: "Aegis", Body: "Error: " + truncate(msg.err.Error(), 100)})

	case steerFailedMsg:
		// P33.15 #2: a steer POST failing does NOT mean the stream it was
		// meant to interrupt died — unlike errMsg above, this must not touch
		// m.streaming, m.queued, or any other in-flight m.pendingSteers.
		if _, found := m.resolvePendingSteer(msg.text); !found {
			// Already resolved by the main stream itself — a KindSteer/
			// KindSteerUnconsumed event, or the streamClosedMsg safety net —
			// arrived before this async POST response did. Nothing left to do.
			return m, nil
		}
		var statusErr *client.StatusError
		switch {
		case errors.As(msg.err, &statusErr) && statusErr.Code == http.StatusNotFound:
			// errSteerClosed: the run had already ended before the steer
			// reached the daemon. Not a failure — the run legitimately
			// finished — so this isn't shown as an error at all; hand the
			// text to the same requeue path KindSteerUnconsumed uses so a
			// user-typed steer still isn't silently lost (P33.15 #1).
			m.requeueSteer(msg.text, msg.origin)
		case errors.As(msg.err, &statusErr) && statusErr.Code == http.StatusTooManyRequests:
			// errSteerFull: the run is still live, this is just retryable.
			m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered (server busy — try again): "+oneLine(msg.text)) + "\n\n")
		default:
			m.transcript.Append(m.th.statusDim.Render("⇢ steer not delivered: "+oneLine(msg.text)) + "\n\n")
		}
		m.refresh()
		return m, nil

	case bangMsg: // P2.2: shell command result
		header := m.th.tool.Render("! " + msg.cmd)
		m.transcript.Append("\n" + header + "\n")
		if msg.output != "" {
			style := m.th.sideValue
			if msg.code != 0 {
				style = m.th.toolErr
			}
			m.transcript.Append(style.Render(msg.output) + "\n")
		}
		if msg.code != 0 {
			m.transcript.Append(m.th.toolErr.Render(fmt.Sprintf("exit %d", msg.code)) + "\n")
			// P13.3.1: a ! command never reaches the model automatically —
			// offer the same diagnose bridge the terminal pane gets.
			m.lastFailure = &shellFailure{source: "!", command: msg.cmd, output: msg.output, code: msg.code}
			m.transcript.Append(m.th.statusDim.Render(m.keys.Diagnose.Help().Key+" to ask Aegis to diagnose this") + "\n")
		}
		m.transcript.Append("\n")
		m.followBottom = true
		m.refresh()
		return m, nil

	case teammatesUpdateMsg: // P2.5: silent sub-agent poll
		m.teammates = msg.items
		m.refresh()
		return m, nil

	case teammatesMsg:
		m.renderTeammates(msg)
		m.refresh()
		return m, nil

	case statusInfoMsg:
		// Silent: a daemon that predates /status context fields (or an
		// unreachable one) just leaves the name-based fallback in place.
		if msg.err == nil && msg.info.ContextWindow > 0 {
			m.srvCtxWin = msg.info.ContextWindow
			m.srvCtxWinSrc = msg.info.ContextWindowSource
		}
		// P28.7 connection/model-health indicator: a request error means the
		// daemon itself is unreachable, distinct from the daemon being up but
		// reporting its configured provider as unreachable.
		m.connKnown = true
		if msg.err != nil {
			m.connReachable, m.connLatencyMS = false, 0
		} else {
			m.connReachable = msg.info.ProviderReachable
			m.connLatencyMS = msg.info.ProviderLatencyMS
		}
		return m, nil

	case statusTickMsg:
		return m, tea.Batch(m.fetchStatusInfo(), statusTickCmd())

	case sessionsLoadedMsg:
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

	case sessionSwitchedMsg:
		if msg.err != nil {
			t, cmd := newToastCmd("switch: "+msg.err.Error(), toastError)
			m.activeToast = t
			return m, cmd
		}
		m.applySwitchedSession(msg.sess)
		m.refresh()
		return m, nil

	case backtrackTargetsMsg:
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

	case forkedMsg:
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

	case termOutputMsg:
		m.term.handleOutput(msg.text)
		m.refresh()
		if m.termRun != nil {
			return m, waitForTermOutput(m.termRun)
		}
		return m, nil

	case termDoneMsg:
		m.termRun = nil
		m.term.handleDone(msg.err)
		if m.term.lastFailed {
			// P13.3.1: bridge the failure to the model on request — the
			// terminal pane's output never reaches it automatically.
			m.lastFailure = &shellFailure{
				source:  "terminal",
				command: m.term.lastCmd,
				output:  m.term.lastOutput,
				code:    m.term.lastExitCode,
			}
		}
		m.term.refreshVP()
		m.refresh()
		return m, nil

	case slashResultMsg:
		if msg.Quit {
			// P16.6: confirm before discarding an in-flight stream instead of
			// quitting silently — /quit and /exit used to cancel and exit
			// unconditionally even mid-response.
			if m.streaming {
				m.quitConfirm = true
				return m, nil
			}
			if m.cancel != nil {
				m.cancel()
			}
			saveStash(m.stashPath, m.ta.Value())
			return m, tea.Quit
		}
		if msg.Model != nil {
			m.cfg.Model = *msg.Model
		}
		if msg.Personas != nil {
			// P33.13: the picker already opened (in its loading state) the
			// moment "/persona" was dispatched — populate it in place rather
			// than opening a second one. Not awaiting means the user dismissed
			// it (or moved on to another dialog) before this landed: drop it,
			// same as the session/backtrack pickers' late-data handling.
			if m.awaitingPicker(dialogPersonaPicker) {
				return m, m.dialog.setItems(personaPickerItems(msg.Personas), personaPickerH(m.height, len(msg.Personas)))
			}
			return m, nil
		}
		if m.awaitingPicker(dialogPersonaPicker) {
			// The bare "/persona" dispatch that opened the loading dialog
			// came back with nothing to list (msg.Output alone) or failed
			// (msg.IsError) — report it inside the dialog the user is already
			// looking at instead of as a transcript line below.
			return m, m.dialog.setNotice(msg.Output)
		}
		if msg.Models != nil {
			picker := newModelPicker(m.width, m.height, msg.Models, m.cfg.Model)
			m.dialog = &picker
			return m, nil
		}
		if msg.ThreatModelTarget != nil {
			picker := newThreatModelFrameworkPicker(m.width, m.height)
			m.dialog = &picker
			m.pendingThreatModelTarget = *msg.ThreatModelTarget
			return m, nil
		}
		if msg.Output == "\x00wizard" {
			wiz := newWizard(m.width, m.height, m.th)
			m.wizard = wiz
			return m, wiz.init()
		}
		if msg.SecurityConfigGlobal != nil {
			sc := newSecurityConfigModel(m.width, m.height, m.th, *msg.SecurityConfigGlobal)
			m.securityConfig = sc
			return m, sc.init()
		}
		if msg.Output == "\x00timeline" { // P2.8
			if len(m.timelineEntries) == 0 {
				t, cmd := newToastCmd("no turns in timeline yet", toastInfo)
				m.activeToast = t
				return m, cmd
			}
			picker := newTimelinePicker(m.width, m.height, m.timelineEntries)
			m.dialog = &picker
			return m, nil
		}
		if msg.ReloadSession {
			// A rewind changed the conversation: reload it and report via toast,
			// since the reload resets the transcript.
			var cmds []tea.Cmd
			if msg.Output != "" {
				level := toastInfo
				if msg.IsError {
					level = toastError
				}
				t, c := newToastCmd(msg.Output, level)
				m.activeToast = t
				cmds = append(cmds, c)
			}
			cmds = append(cmds, m.switchSessionCmd(m.cfg.SessionID))
			return m, tea.Batch(cmds...)
		}
		if msg.SwitchToSession != "" {
			// P22.3: /fork created a genuinely different session (not just a
			// truncated version of this one) — load it the same way Ctrl+Y's
			// session picker does, rather than ReloadSession's "refetch this
			// same id" path above.
			var cmds []tea.Cmd
			if msg.Output != "" {
				level := toastInfo
				if msg.IsError {
					level = toastError
				}
				t, c := newToastCmd(msg.Output, level)
				m.activeToast = t
				cmds = append(cmds, c)
			}
			cmds = append(cmds, m.switchSessionCmd(msg.SwitchToSession))
			return m, tea.Batch(cmds...)
		}
		if msg.Output == "\x00clear" {
			m.transcript.Reset()
			m.lastAnswerBlock = nil
			m.thinkEntries = nil
			m.tools = m.tools[:0]
			m.inputTokens, m.outputTokens, m.costUSD = 0, 0, 0
			m.inputTokensKnown = false
			m.cacheReadTokens, m.cacheCreationTokens = 0, 0
			m.tokensEstimated = false
			m.turnCount = 0
			m.changedFiles = m.changedFiles[:0]
			m.teammates = nil
			m.timelineEntries = m.timelineEntries[:0]
			m.transcript.Append(buildWelcomeContent(m.cfg, m.workDir, m.th))
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00tools-compact" {
			m.toolCompact = true
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00tools-full" {
			m.toolCompact = false
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00sidebar-toggle" {
			m.sidebarOpen = !m.sidebarOpen
			m.layout()
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00scrollback-on" {
			return m, m.setRawScrollbackCmd(true)
		}
		if msg.Output == "\x00scrollback-off" {
			return m, m.setRawScrollbackCmd(false)
		}
		if msg.Output == "\x00scrollback-toggle" {
			return m, m.setRawScrollbackCmd(!m.rawScrollback)
		}
		if msg.Output == "\x00theme-show" {
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Current theme: %s", m.cfg.Theme)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00theme ") {
			// P14.8: applyTheme only rebinds the package-level col* vars —
			// m.th and m.renderer were built from those vars at creation time
			// (lipgloss styles and the glamour renderer both capture colors
			// once) and must be explicitly rebuilt to actually change what's
			// on screen. Already-rendered transcript content keeps its old
			// colors, same limitation /humor and /sidebar already have for
			// past output.
			name := strings.TrimPrefix(msg.Output, "\x00theme ")
			// P40.5: "/theme auto" re-enables background detection; any explicit
			// name opts out so a later BackgroundColorMsg can't override it.
			if isAutoTheme(name) {
				m.autoTheme = true
				m.cfg.Theme = applyTheme("dark", m.cfg.WorkDir) // provisional until the terminal replies
				m.th = newTheme()
				m.renderer = newGlamourRenderer(m.rendererW)
				m.transcript.Append(m.th.statusText.Render("Theme set to auto — detecting terminal background. Set tui.theme: auto in config to persist.") + "\n\n")
				m.refresh()
				return m, tea.RequestBackgroundColor
			}
			m.autoTheme = false
			name = applyTheme(name, m.cfg.WorkDir)
			m.cfg.Theme = name
			m.th = newTheme()
			m.renderer = newGlamourRenderer(m.rendererW)
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Theme switched to %s. This session only — set tui.theme: %s in config to persist.", name, name)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00notify-show" {
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Current notify mode: %s", m.notifyMode)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00notify ") {
			name := strings.TrimPrefix(msg.Output, "\x00notify ")
			m.notifyMode = notify.ParseMode(name)
			m.transcript.Append(m.th.statusText.Render(fmt.Sprintf("Notify mode switched to %s. This session only — set tui.notifications: %s in config to persist.", name, name)) + "\n\n")
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00copy") {
			arg := strings.TrimPrefix(msg.Output, "\x00copy")
			arg = strings.TrimSpace(arg)
			var text string
			if arg == "" {
				text = m.lastAssistantText
			} else {
				n := 0
				fmt.Sscanf(arg, "%d", &n)
				blocks := extractCodeBlocks(m.lastAssistantText)
				if n >= 1 && n <= len(blocks) {
					text = blocks[n-1]
				} else {
					t, cmd := newToastCmd(fmt.Sprintf("no code block #%d in last message", n), toastError)
					m.activeToast = t
					return m, cmd
				}
			}
			if text == "" {
				t, cmd := newToastCmd("nothing to copy (no assistant message yet)", toastInfo)
				m.activeToast = t
				return m, cmd
			}
			return m, copyToClipboardCmd(text)
		}
		if msg.Output == "\x00paste-image" {
			return m, pasteClipboardImageCmd()
		}
		if msg.Output == "\x00humor-on" {
			m.humorMode = true
			m.transcript.Append(m.th.statusText.Render("Humor mode: on — rolling for initiative 🎲") + "\n\n")
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00humor-off" {
			m.humorMode = false
			m.transcript.Append(m.th.statusText.Render("Humor mode: off — plain status text") + "\n\n")
			m.refresh()
			return m, nil
		}
		if msg.Output == "\x00humor-toggle" {
			m.humorMode = !m.humorMode
			if m.humorMode {
				m.transcript.Append(m.th.statusText.Render("Humor mode: on — rolling for initiative 🎲") + "\n\n")
			} else {
				m.transcript.Append(m.th.statusText.Render("Humor mode: off — plain status text") + "\n\n")
			}
			m.refresh()
			return m, nil
		}
		if strings.HasPrefix(msg.Output, "\x00diff\n") {
			// P22.1: chroma-highlight the raw git diff text here, where m.th
			// (the active theme) is available — the dispatcher that produced
			// it has no theme reference, same reason /theme and /clear pass
			// through a \x00 marker instead of pre-rendering.
			diffText := strings.TrimPrefix(msg.Output, "\x00diff\n")
			rendered := diffText
			if lines, ok := highlightUnifiedDiff(m.th, diffText); ok {
				rendered = strings.Join(lines, "\n")
			}
			m.transcript.Append(rendered + "\n")
			m.refresh()
			return m, nil
		}
		if msg.Transient && msg.Output != "" && msg.Message == "" {
			// P33.11: informational output opens a dismissable overlay panel
			// instead of appending to the transcript. Reached only after every
			// picker / sentinel / Message branch above returned, so a transient
			// command that opened a picker or sends a message is never
			// intercepted here — this is the plain-text tail alone.
			title := msg.TransientTitle
			if title == "" {
				title = "info"
			}
			p := newTransientPanel(title, msg.Output, msg.IsError, m.width, m.height)
			m.transientPanel = &p
			m.ta.Blur()
			m.refresh()
			return m, nil
		}
		if msg.Output != "" {
			style := m.th.statusText
			if msg.IsError {
				style = m.th.errLine
			}
			m.transcript.Append(style.Render(msg.Output) + "\n\n")
		}
		if msg.Message != "" {
			m.appendUser(msg.Message, nil)
			m.beginStream()
			m.followBottom = true
			m.refresh()
			return m, tea.Batch(m.startStream(msg.Message, nil), m.sp.Tick)
		}
		m.refresh()
		return m, nil
	}

	// The approval dialog owns all input while open (P25.4a): tea.KeyMsg
	// already returns above before reaching here, but this guard covers
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
	// P21.7: followBottom is user intent — paused by an explicit scroll away
	// from the bottom, resumed by scrolling back to it (both handled above, at
	// the scroll inputs themselves) or by sending/queueing a message. It is
	// deliberately NOT re-derived from geometry here on every message: any
	// layout perturbation (approval dialog, textarea growth) briefly makes
	// AtBottom() read false, and a blanket re-derivation would
	// turn that into a permanently dead auto-follow with no user scroll having
	// happened.
	return m, tea.Batch(cmds...)
}
