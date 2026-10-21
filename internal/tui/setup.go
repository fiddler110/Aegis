package tui

import (
	tea "charm.land/bubbletea/v2"
)

// RunSetup runs the /config dialog on its own, before any daemon exists. It is
// the first-run path: a bare `aegis` with no config file used to start on the
// built-in defaults (Anthropic, no key in most environments) and fail on the
// first turn, with `aegis --first-init`'s commented template as the only
// guided route — a file to read and edit, not a question to answer.
//
// It reports whether a config was saved. Discarding is not an error; the
// caller decides what a run with no config should do.
func RunSetup(themeName, workDir string) (saved bool, fitNote string, err error) {
	if isAutoTheme(themeName) {
		themeName = "dark" // no terminal background report before the program starts
	}
	applyTheme(themeName, workDir)
	w := newWizard(100, 40, newTheme())
	w.notice = "First run — choose a provider and model, check the connection, then Save and exit."
	m := &setupModel{w: w}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return false, "", err
	}
	if w.saveErr != "" {
		return false, "", errSetup(w.saveErr)
	}
	return w.saved, w.fitNote, nil
}

type errSetup string

func (e errSetup) Error() string { return "setup: " + string(e) }

// setupModel hosts the wizard as a whole program instead of an overlay.
type setupModel struct{ w *wizardModel }

func (m *setupModel) Init() tea.Cmd { return m.w.init() }

func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.w.width, m.w.height = ws.Width, ws.Height
	}
	cmd := m.w.update(msg)
	if m.w.done {
		return m, tea.Quit
	}
	return m, cmd
}

func (m *setupModel) View() tea.View {
	v := tea.NewView(m.w.view())
	v.AltScreen = true
	return v
}
