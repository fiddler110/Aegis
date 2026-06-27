package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// Semantic palette, built on the Charmtone "Pantera" dark palette (the same
// family Crush uses) so the UI reads as a single, cohesive dark theme rather
// than an ad-hoc set of hexes. lipgloss v2 dropped AdaptiveColor; the palette
// is intentionally dark-first. Colors satisfy the color.Color interface (both
// charmtone keys and lipgloss.Color do) and degrade gracefully to 256- and
// 16-color terminals.
var (
	colSurface color.Color = charmtone.Pepper // base background
	colBorder  color.Color = charmtone.Iron   // visible borders / rules

	// Body and secondary text.
	colTextDim   color.Color = charmtone.Smoke  // primary body text
	colTextMuted color.Color = charmtone.Oyster // muted / secondary text

	colAccent  color.Color = charmtone.Charple // brand purple / primary accent
	colSuccess color.Color = charmtone.Julep   // green
	colWarning color.Color = charmtone.Mustard // amber
	colDanger  color.Color = charmtone.Coral   // red

	colUserFg    color.Color = charmtone.Malibu  // user messages (blue)
	colAssistFg  color.Color = charmtone.Charple // assistant messages (purple)
	colToolFg    color.Color = charmtone.Squid   // tool output
	colToolErrFg color.Color = charmtone.Coral   // tool errors

	colBrandBg  color.Color = charmtone.Charple // brand chip background
	colBrandFg  color.Color = charmtone.Butter  // brand chip text
	colShield   color.Color = charmtone.Guppy   // shield / logo art
	colCwd      color.Color = charmtone.Malibu  // working directory
	colInputSep color.Color = charmtone.Char    // input border separator

	// Gradient ramp endpoints. colGrad* ramps the shield art vertically (cool
	// lavender crest → brand purple point); colWord* is a brighter horizontal
	// ramp for the AEGIS wordmark so the name reads clearly against the shield.
	colGradFrom color.Color = charmtone.Dolly
	colGradTo   color.Color = charmtone.Charple
	colWordFrom color.Color = charmtone.Salt
	colWordTo   color.Color = charmtone.Dolly

	// Mode badge backgrounds — each mode gets a coloured pill for at-a-glance
	// status. Deep desaturated backgrounds with a bright Charmtone foreground.
	colPlanBg  color.Color = lipgloss.Color("#0C2440") // deep navy  → safe/read-only
	colPlanFg  color.Color = charmtone.Malibu          // sky blue
	colBuildBg color.Color = lipgloss.Color("#431407") // deep amber → file writes active
	colBuildFg color.Color = charmtone.Zest            // warm amber
	colAutoBg  color.Color = lipgloss.Color("#052E16") // deep green → full capability
	colAutoFg  color.Color = charmtone.Guac            // mint green
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

		// Mode pills: coloured background so status is readable at a glance.
		modePlan:  lipgloss.NewStyle().Background(colPlanBg).Foreground(colPlanFg).Bold(true).Padding(0, 1),
		modeBuild: lipgloss.NewStyle().Background(colBuildBg).Foreground(colBuildFg).Bold(true).Padding(0, 1),
		modeAuto:  lipgloss.NewStyle().Background(colAutoBg).Foreground(colAutoFg).Bold(true).Padding(0, 1),

		statusText: lipgloss.NewStyle().Foreground(colTextDim),
		statusDim:  lipgloss.NewStyle().Foreground(colTextMuted),
		costText:   lipgloss.NewStyle().Foreground(colSuccess),

		titleMeta: lipgloss.NewStyle().Foreground(colTextMuted),

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

		thinking:    lipgloss.NewStyle().Foreground(colTextMuted).Bold(true),
		thinkingDim: lipgloss.NewStyle().Foreground(colTextMuted).Italic(true),
	}
}
