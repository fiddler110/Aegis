package tui

import (
	"charm.land/lipgloss/v2"
)

// The semantic color roles this file's styles are built from live in
// colorscheme.go (TQ10): a colorScheme struct with dark + light built-ins,
// selected via the tui.theme config key and bound to the package-level col*
// variables by applyTheme before any styles are created.

// theme holds all pre-built styles. lipgloss.Style is a value type so every
// field can be shared across renders without mutation.
type theme struct {
	user      lipgloss.Style
	assistant lipgloss.Style

	tool    lipgloss.Style
	toolErr lipgloss.Style
	errLine lipgloss.Style

	sideSection lipgloss.Style
	sideValue   lipgloss.Style
	sideMuted   lipgloss.Style

	statusText lipgloss.Style
	statusDim  lipgloss.Style
	costText   lipgloss.Style

	titleMeta lipgloss.Style

	cwdStyle    lipgloss.Style
	welcomeKey  lipgloss.Style
	welcomeVal  lipgloss.Style
	welcomeName lipgloss.Style
	inputSep    lipgloss.Style

	turnSep    lipgloss.Style // subtle horizontal rule between conversation turns
	elapsedDim lipgloss.Style // muted elapsed-time counter shown during streaming

	diffAdd  lipgloss.Style // added line in a tool diff (+)
	diffDel  lipgloss.Style // removed line in a tool diff (-)
	diffMeta lipgloss.Style // file path / "N more lines" footer
	toolBody lipgloss.Style // multi-line tool output body
	toolGut  lipgloss.Style // gutter rule beside tool output

	thinking    lipgloss.Style // "✻ thinking" header
	thinkingDim lipgloss.Style // extended-thinking body
}

func newTheme() theme {
	return theme{
		user:      lipgloss.NewStyle().Foreground(colUserFg).Bold(true),
		assistant: lipgloss.NewStyle().Foreground(colAssistFg).Bold(true),

		tool:    lipgloss.NewStyle().Foreground(colToolFg),
		toolErr: lipgloss.NewStyle().Foreground(colToolErrFg),
		errLine: lipgloss.NewStyle().Foreground(colDanger).Bold(true),

		sideSection: lipgloss.NewStyle().Foreground(colTextMuted).Bold(true),
		sideValue:   lipgloss.NewStyle().Foreground(colTextDim),
		sideMuted:   lipgloss.NewStyle().Foreground(colTextMuted),

		statusText: lipgloss.NewStyle().Foreground(colFgBase),
		statusDim:  lipgloss.NewStyle().Foreground(colTextMuted),
		costText:   lipgloss.NewStyle().Foreground(colSuccess),

		titleMeta: lipgloss.NewStyle().Foreground(colTextMuted),

		cwdStyle:    lipgloss.NewStyle().Foreground(colCwd),
		welcomeKey:  lipgloss.NewStyle().Foreground(colTextMuted),
		welcomeVal:  lipgloss.NewStyle().Foreground(colFgBase),
		welcomeName: lipgloss.NewStyle().Foreground(colAccent).Bold(true),
		inputSep:    lipgloss.NewStyle().Foreground(colInputSep),

		turnSep:    lipgloss.NewStyle().Foreground(colSeparator),
		elapsedDim: lipgloss.NewStyle().Foreground(colTextMuted),

		diffAdd:  lipgloss.NewStyle().Foreground(colSuccess),
		diffDel:  lipgloss.NewStyle().Foreground(colDanger),
		diffMeta: lipgloss.NewStyle().Foreground(colTextMuted),
		toolBody: lipgloss.NewStyle().Foreground(colTextDim),
		toolGut:  lipgloss.NewStyle().Foreground(colSeparator),

		thinking:    lipgloss.NewStyle().Foreground(colTextMuted).Bold(true),
		thinkingDim: lipgloss.NewStyle().Foreground(colTextMuted).Italic(true),
	}
}
