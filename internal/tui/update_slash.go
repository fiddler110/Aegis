package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/fiddler110/aegis/internal/tui/notify"
)

// updateSlashResult applies the outcome of a dispatched slash command: the
// pickers and overlays it can open, the \x00-marked sentinels the dispatcher
// uses to hand theme/clipboard/transcript work back to a themed context, and
// the plain-text tail.
func (m model) updateSlashResult(msg slashResultMsg) (tea.Model, tea.Cmd) {
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
		m.pendingThreatModelUnattended = msg.ThreatModelUnattended
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
		m.displayedInputTokens, m.displayedOutputTokens = 0, 0
		m.inputTokensKnown = false
		m.cacheReadTokens, m.cacheCreationTokens = 0, 0
		m.tokensEstimated = false
		m.turnCount = 0
		m.changedFiles = m.changedFiles[:0]
		m.teammates = nil
		m.timelineEntries = m.timelineEntries[:0]
		m.toolBlocks = nil // P75.1: old entries point at transcript items /clear just dropped
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
		// P66.15: a slash command's text tail is not all harness-authored —
		// /scan network prints nmap's service banners, i.e. bytes a remote
		// host chose, and /scan path prints whatever a scanner put in a
		// finding title — and nothing between here and the terminal strips an
		// escape sequence. Same posture as raw tool output (P28.1): drop
		// everything but SGR colour, which the diff's own colours use.
		diffText := stripDangerousSeqs(strings.TrimPrefix(msg.Output, "\x00diff\n"))
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
		p := newTransientPanel(title, stripDangerousSeqs(msg.Output), msg.IsError, m.width, m.height)
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
		m.transcript.Append(style.Render(stripDangerousSeqs(msg.Output)) + "\n\n")
	}
	if msg.Drive != nil {
		// P52.12: an unattended phased drive. The transcript treats it as an
		// ordinary turn — the task is what the user asked for, and the
		// daemon's first notice event announces the phase plan — so only the
		// command that starts it differs from the Message branch below.
		m.appendUser(msg.Drive.Task, nil)
		m.beginStream()
		m.followBottom = true
		m.refresh()
		return m, tea.Batch(m.startDrive(*msg.Drive), m.sp.Tick)
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
