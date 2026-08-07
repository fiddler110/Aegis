package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// updateBang renders the result of a P2.2 !-prefixed shell command.
func (m model) updateBang(msg bangMsg) (tea.Model, tea.Cmd) {
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
}

// updateTermOutput feeds a chunk of terminal-pane output into the pane.
func (m model) updateTermOutput(msg termOutputMsg) (tea.Model, tea.Cmd) {
	m.term.handleOutput(msg.text)
	m.refresh()
	if m.termRun != nil {
		return m, waitForTermOutput(m.termRun)
	}
	return m, nil
}

// updateTermDone finalizes a terminal-pane command.
func (m model) updateTermDone(msg termDoneMsg) (tea.Model, tea.Cmd) {
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
}
