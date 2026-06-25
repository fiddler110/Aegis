package tui

import "github.com/charmbracelet/lipgloss"

// Semantic True Color palette. Lipgloss / muesli/termenv degrades gracefully
// to 256-color and 16-color terminals automatically.
var (
	colSurface   = lipgloss.Color("#13161E")
	colBorder    = lipgloss.Color("#2D3148")
	colTextDim   = lipgloss.Color("#94A3B8")
	colTextMuted = lipgloss.Color("#475569")

	colAccent   = lipgloss.Color("#7C3AED")
	colSuccess  = lipgloss.Color("#10B981")
	colWarning  = lipgloss.Color("#F59E0B")
	colDanger   = lipgloss.Color("#EF4444")

	colUserFg    = lipgloss.Color("#38BDF8")
	colAssistFg  = lipgloss.Color("#A78BFA")
	colToolFg    = lipgloss.Color("#64748B")
	colToolErrFg = lipgloss.Color("#F87171")
)

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

	modePlan  lipgloss.Style
	modeBuild lipgloss.Style
	modeAuto  lipgloss.Style

	statusText lipgloss.Style
	statusDim  lipgloss.Style
	costText   lipgloss.Style

	titleBrand lipgloss.Style
	titleMeta  lipgloss.Style
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

		modePlan:  lipgloss.NewStyle().Foreground(colTextMuted),
		modeBuild: lipgloss.NewStyle().Foreground(colWarning).Bold(true),
		modeAuto:  lipgloss.NewStyle().Foreground(colSuccess).Bold(true),

		statusText: lipgloss.NewStyle().Foreground(colTextDim),
		statusDim:  lipgloss.NewStyle().Foreground(colTextMuted),
		costText:   lipgloss.NewStyle().Foreground(colSuccess),

		titleBrand: lipgloss.NewStyle().Foreground(colAccent).Bold(true),
		titleMeta:  lipgloss.NewStyle().Foreground(colTextMuted),
	}
}
