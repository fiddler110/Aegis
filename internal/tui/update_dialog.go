package tui

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/commands"
)

// updateDialog drives the dialog overlay (command palette,
// persona/session/timeline/model picker): all input is routed to it. Result
// messages are handled here so they are not re-intercepted by this same block
// on the next tick (the overlay would swallow them otherwise). P16.6 collapsed
// four near-identical blocks into one, dispatching by dialog.kind. A false bool
// means the message falls through to the main update path.
func (m model) updateDialog(msg tea.Msg) (model, tea.Cmd, bool) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.layout()
		return m, nil, true
	}
	// P33.7: keep the loading row's spinner turning. The tick is claimed
	// here rather than left to the spinner.TickMsg case in the main switch,
	// which this block returns ahead of, and re-queued only while a fetch is
	// actually outstanding — once the rows land it stops on its own.
	if _, ok := msg.(spinner.TickMsg); ok && m.dialog.loading {
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		m.dialog.setLoadingFrame(m.sp.View())
		return m, cmd, true
	}
	if c, ok := msg.(dialogCancelMsg); ok && c.kind == m.dialog.kind {
		m.dialog = nil
		m.ta.Focus()
		if c.kind == dialogPersonaPicker {
			m.refresh()
		}
		if c.kind == dialogThreatModelPicker {
			m.pendingThreatModelTarget = ""
			m.pendingThreatModelUnattended = false
		}
		return m, nil, true
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
				return m, nil, true
			}
			parsed := &commands.ParsedCommand{Name: item.name, Raw: "/" + item.name}
			return m, m.dispatchSlash(parsed), true
		case dialogPersonaPicker:
			item := sel.item.(personaItem)
			parsed := &commands.ParsedCommand{Name: "persona", Args: []string{item.name}, Raw: "/persona " + item.name}
			return m, m.handleSlashCommand(parsed), true
		case dialogSessionPicker:
			item := sel.item.(sessionItem)
			if item.id == m.cfg.SessionID {
				return m, nil, true // already on this session
			}
			return m, m.switchSessionCmd(item.id), true
		case dialogTimelinePicker:
			item := sel.item.(timelineItem)
			// Scroll to the selected turn's recorded item position.
			m.transcript.ScrollToItem(item.e.blockIndex)
			m.followBottom = false
			m.refresh()
			return m, nil, true
		case dialogModelPicker:
			item := sel.item.(modelItem)
			parsed := &commands.ParsedCommand{Name: "model", Args: []string{item.id}, Raw: "/model " + item.id}
			return m, m.handleSlashCommand(parsed), true
		case dialogThreatModelPicker:
			item := sel.item.(frameworkItem)
			target := m.pendingThreatModelTarget
			unattended := m.pendingThreatModelUnattended
			m.pendingThreatModelTarget = ""
			m.pendingThreatModelUnattended = false
			args := strings.Fields(item.name) // splits "NIST 800-154" into the two tokens extractThreatModelFramework expects
			if target != "" {
				args = append(args, target)
			}
			if unattended {
				// Re-stated as the token the handler parses, so the picker
				// path and the typed-framework path go through exactly one
				// piece of flag-parsing code (P52.12).
				args = append(args, "unattended")
			}
			parsed := &commands.ParsedCommand{Name: "threat-model", Args: args, Raw: "/threat-model " + strings.Join(args, " ")}
			return m, m.handleSlashCommand(parsed), true
		case dialogHistoryPicker:
			item := sel.item.(historyItem)
			// Recall the entry onto the input line for further editing or
			// sending, same as a shell reverse-search accepting a match —
			// it does not send immediately.
			m.ta.SetValue(item.text)
			m.histIdx = -1
			m.draftInput = ""
			return m, nil, true
		case dialogBacktrackPicker:
			item := sel.item.(backtrackItem)
			// P22.3: fork at that turn's checkpoint and pre-fill its
			// original text so the user edits before resending, rather
			// than the plain "load onto the input line" the history
			// picker above does — the picked entry has already been sent
			// once in the (now-untouched) source session.
			return m, m.forkAndSwitchCmd(item.cpID, item.text), true
		}
		return m, nil, true
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
			return m, cmd, true
		}
	default:
		updated, cmd := m.dialog.Update(msg)
		m.dialog = &updated
		return m, cmd, true
	}
	return m, nil, false
}
