package tui

import "github.com/charmbracelet/lipgloss"

// Semantic True Color palette. Lipgloss / muesli/termenv degrades gracefully
// to 256-color and 16-color terminals automatically.
var (
	colSurface   = lipgloss.Color("#0F1117")
	colBorder    = lipgloss.Color("#2D3148")
	colTextDim   = lipgloss.Color("#CBD5E1")
	colTextMuted = lipgloss.Color("#64748B")

	colAccent  = lipgloss.Color("#7C3AED")
	colSuccess = lipgloss.Color("#10B981")
	colWarning = lipgloss.Color("#F59E0B")
	colDanger  = lipgloss.Color("#EF4444")

	colUserFg    = lipgloss.Color("#38BDF8")
	colAssistFg  = lipgloss.Color("#A78BFA")
	colToolFg    = lipgloss.Color("#94A3B8")
	colToolErrFg = lipgloss.Color("#F87171")

	colBrandBg  = lipgloss.Color("#2E1065") // deep purple brand box
	colBrandFg  = lipgloss.Color("#DDD6FE") // light lavender brand text
	colShield   = lipgloss.Color("#818CF8") // medium indigo for shield art
	colCwd      = lipgloss.Color("#38BDF8") // sky blue for working directory
	colInputSep = lipgloss.Color("#374151") // slightly brighter separator for input borders

	// Mode badge backgrounds — each mode gets a coloured pill for at-a-glance status.
	colPlanBg  = lipgloss.Color("#0C2440") // deep navy  → safe/read-only
	colPlanFg  = lipgloss.Color("#60A5FA") // sky blue
	colBuildBg = lipgloss.Color("#431407") // deep amber → file writes active
	colBuildFg = lipgloss.Color("#FCD34D") // warm amber
	colAutoBg  = lipgloss.Color("#052E16") // deep green → full capability
	colAutoFg  = lipgloss.Color("#4ADE80") // mint green
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

	titleMeta  lipgloss.Style
	brandLabel lipgloss.Style

	shieldArt   lipgloss.Style
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

		// Mode pills: coloured background so status is readable at a glance.
		modePlan:  lipgloss.NewStyle().Background(colPlanBg).Foreground(colPlanFg).Bold(true).Padding(0, 1),
		modeBuild: lipgloss.NewStyle().Background(colBuildBg).Foreground(colBuildFg).Bold(true).Padding(0, 1),
		modeAuto:  lipgloss.NewStyle().Background(colAutoBg).Foreground(colAutoFg).Bold(true).Padding(0, 1),

		statusText: lipgloss.NewStyle().Foreground(colTextDim),
		statusDim:  lipgloss.NewStyle().Foreground(colTextMuted),
		costText:   lipgloss.NewStyle().Foreground(colSuccess),

		titleMeta:  lipgloss.NewStyle().Foreground(colTextMuted),
		brandLabel: lipgloss.NewStyle().Background(colBrandBg).Foreground(colBrandFg).Bold(true),

		shieldArt:   lipgloss.NewStyle().Foreground(colShield),
		cwdStyle:    lipgloss.NewStyle().Foreground(colCwd),
		welcomeKey:  lipgloss.NewStyle().Foreground(colTextMuted),
		welcomeVal:  lipgloss.NewStyle().Foreground(colTextDim),
		welcomeName: lipgloss.NewStyle().Foreground(colAccent).Bold(true),
		inputSep:    lipgloss.NewStyle().Foreground(colInputSep),

		turnSep:    lipgloss.NewStyle().Foreground(colBorder),
		elapsedDim: lipgloss.NewStyle().Foreground(colTextMuted),

		diffAdd:  lipgloss.NewStyle().Foreground(colSuccess),
		diffDel:  lipgloss.NewStyle().Foreground(colDanger),
		diffMeta: lipgloss.NewStyle().Foreground(colTextMuted),
		toolBody: lipgloss.NewStyle().Foreground(colTextDim),
		toolGut:  lipgloss.NewStyle().Foreground(colBorder),
	}
}
