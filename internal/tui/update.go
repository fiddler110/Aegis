package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// Update is the dispatcher for the Bubbletea update loop. It is deliberately
// thin: the modal overlays get first refusal on every message (in the order
// they stack on screen), then the per-message-domain handlers in the
// update_*.go files own the work, and whatever no handler claimed falls
// through to the composer.
//
// Two shapes appear among the handlers. A handler that always consumes its
// message returns (tea.Model, tea.Cmd) and its result is returned straight to
// Bubbletea. A handler that may decline returns a trailing bool; false means
// the message keeps travelling down this function, carrying whatever model
// state the declining handler already changed.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Focus tracking (P16.1) always updates regardless of any open overlay —
	// it carries no interaction semantics of its own, just suppression state
	// for the attention system.
	if mm, cmd, done := m.updateFocus(msg); done {
		return mm, cmd
	}

	// Wizard overlay: delegate all messages while the wizard is open.
	if m.overlays.wizard != nil {
		return m.updateWizard(msg)
	}

	// Security-config overlay: delegate all messages while it's open (P11.11).
	if m.overlays.securityConfig != nil {
		return m.updateSecurityConfig(msg)
	}

	// Transient informational panel (P33.11): modal except for the
	// stream-lifecycle allowlist (P33.20), which falls through.
	if m.overlays.transientPanel != nil {
		var cmd tea.Cmd
		var done bool
		m, cmd, done = m.updateTransientPanel(msg)
		if done {
			return m, cmd
		}
	}

	// Dialog overlay (command palette, persona/session/timeline/model picker):
	// modal except for its own P33.20 allowlist.
	if m.overlays.dialog != nil {
		var cmd tea.Cmd
		var done bool
		m, cmd, done = m.updateDialog(msg)
		if done {
			return m, cmd
		}
	}

	// Quit-confirmation overlay: shown instead of quitting outright when a
	// turn is streaming (ctrl+c / /quit / /exit) — P16.6.
	if m.overlays.quitConfirm {
		return m.updateQuitConfirm(msg)
	}

	// Help overlay: only Escape or F1 closes it.
	if m.overlays.helpOpen {
		return m.updateHelp(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m = m.updateWindowSize(msg)

	case tea.BackgroundColorMsg:
		m = m.updateBackgroundColor(msg)

	case spinner.TickMsg:
		var tickCmds []tea.Cmd
		m, tickCmds = m.updateSpinnerTick(msg)
		cmds = append(cmds, tickCmds...)

	case toastExpiredMsg:
		m = m.updateToastExpired(msg)

	case clipboardResultMsg:
		return m.updateClipboardResult(msg)

	case pasteImageResultMsg:
		return m.updatePasteImageResult(msg)

	case editorDoneMsg:
		var cmd tea.Cmd
		var done bool
		m, cmd, done = m.updateEditorDone(msg)
		if done {
			return m, cmd
		}

	case tea.PasteMsg:
		var cmd tea.Cmd
		var done bool
		m, cmd, done = m.updatePaste(msg)
		if done {
			return m, cmd
		}

	case tea.KeyMsg:
		mm, cmd, done := m.updateKey(msg)
		if done {
			return mm, cmd
		}
		m = mm.(model)

	case streamStartedMsg:
		return m.updateStreamStarted(msg)

	case eventMsg:
		return m.updateEvent(msg)

	case batchEventMsg:
		return m.updateBatchEvent(msg)

	case streamClosedMsg:
		return m.updateStreamClosed(msg)

	case errMsg:
		return m.updateErr(msg)

	case steerFailedMsg:
		return m.updateSteerFailed(msg)

	case bangMsg: // P2.2: shell command result
		return m.updateBang(msg)

	case teammatesUpdateMsg: // P2.5: silent sub-agent poll
		return m.updateTeammatesUpdate(msg)

	case cronJobsUpdateMsg: // silent ListCronJobs poll
		return m.updateCronJobs(msg)

	case teammatesMsg:
		return m.updateTeammates(msg)

	case statusInfoMsg:
		return m.updateStatusInfo(msg)

	case statusTickMsg:
		return m.updateStatusTick(msg)

	case sessionsLoadedMsg:
		return m.updateSessionsLoaded(msg)

	case sessionSwitchedMsg:
		return m.updateSessionSwitched(msg)

	case backtrackTargetsMsg:
		return m.updateBacktrackTargets(msg)

	case forkedMsg:
		return m.updateForked(msg)

	case termOutputMsg:
		return m.updateTermOutput(msg)

	case termDoneMsg:
		return m.updateTermDone(msg)

	case slashResultMsg:
		return m.updateSlashResult(msg)
	}

	m, cmds = m.updateComposer(msg, cmds)
	// P21.7: followBottom is user intent — paused by an explicit scroll away
	// from the bottom, resumed by scrolling back to it (both handled in
	// updateComposer, at the scroll inputs themselves) or by sending/queueing a
	// message. It is deliberately NOT re-derived from geometry here on every
	// message: any layout perturbation (approval dialog, textarea growth)
	// briefly makes AtBottom() read false, and a blanket re-derivation would
	// turn that into a permanently dead auto-follow with no user scroll having
	// happened.
	return m, tea.Batch(cmds...)
}
