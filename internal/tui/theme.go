package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// Semantic palette, built on the Charmtone "Pantera" dark palette — the exact
// role set Crush uses (see internal/ui/styles/themes.go → CharmtonePantera) so
// the UI reads as a single, cohesive dark theme rather than an ad-hoc set of
// hexes. lipgloss v2 dropped AdaptiveColor; the palette is intentionally
// dark-first. Colors satisfy the color.Color interface (both charmtone keys and
// lipgloss.Color do) and degrade gracefully to 256- and 16-color terminals.
//
// The roles below mirror Crush's quickStyleOpts. Aegis-semantic aliases
// (colAccent, colUserFg, mode pills, …) are defined further down in terms of
// these roles so render sites keep stable names while sharing one source of
// truth.
var (
	// --- Brand ---
	colPrimary   color.Color = charmtone.Charple // brand purple / primary accent
	colSecondary color.Color = charmtone.Dolly   // secondary accent (lavender)
	colAccentAlt color.Color = charmtone.Bok     // tertiary accent (green)
	colKeyword   color.Color = charmtone.Blush   // keyword / highlight pink

	// --- Foreground tiers (brightest → faintest) ---
	colFgBase color.Color = charmtone.Sash   // primary body text
	colFgSub  color.Color = charmtone.Smoke  // secondary text
	colFgMore color.Color = charmtone.Squid  // tertiary / dim labels
	colFgMost color.Color = charmtone.Oyster // faintest / muted captions

	// --- Background / surface tiers (base → most visible) ---
	colBgBase  color.Color = charmtone.Pepper // base background
	colBgLeast color.Color = charmtone.BBQ    // least-visible raised surface
	colBgLess  color.Color = charmtone.Char   // low-contrast surface
	colBgMost  color.Color = charmtone.Iron   // most-visible surface / borders

	colSeparator color.Color = charmtone.Char   // rules / dividers
	colOnPrimary color.Color = charmtone.Butter // fg on primary-coloured bg

	// --- Status roles (each with subtle variants, as in Crush) ---
	colDestructive color.Color = charmtone.Coral    // destructive action red
	colError       color.Color = charmtone.Sriracha // error red (deeper)
	colWarn        color.Color = charmtone.Mustard  // warning amber
	colWarnSubtle  color.Color = charmtone.Zest     // warning amber (subtle)
	colDenied      color.Color = charmtone.Tang     // denied / blocked orange
	colBusy        color.Color = charmtone.Citron   // busy / working yellow-green
	colInfo        color.Color = charmtone.Malibu   // info blue
	colInfoMore    color.Color = charmtone.Sardine  // info blue (subtle)
	colInfoMost    color.Color = charmtone.Damson   // info blue (most subtle)
	colSuccessRole color.Color = charmtone.Julep    // success green
	colSuccessMore color.Color = charmtone.Bok      // success green (subtle)
	colSuccessMost color.Color = charmtone.Guac     // success green (most subtle)

	// --- Aegis-semantic aliases (defined in terms of the roles above) ---
	colSurface   = colBgBase // base background
	colBorder    = colBgMost // visible borders / rules
	colTextDim   = colFgSub  // secondary body text
	colTextMuted = colFgMost // muted / caption text
	colAccent    = colPrimary
	colSuccess   = colSuccessRole
	colWarning   = colWarn
	colDanger    = colDestructive

	colUserFg    = colInfo        // user messages (blue)
	colAssistFg  = colPrimary     // assistant messages (purple)
	colToolFg    = colFgMore      // tool output
	colToolErrFg = colDestructive // tool errors

	colBrandBg              = colPrimary      // brand chip background
	colBrandFg              = colOnPrimary    // brand chip text
	colShield   color.Color = charmtone.Guppy // shield / logo art
	colCwd                  = colInfo         // working directory
	colInputSep             = colSeparator    // input border separator

	// Gradient ramp endpoints. colGrad* ramps the shield art vertically (cool
	// lavender crest → brand purple point); colWord* is a brighter horizontal
	// ramp for the AEGIS wordmark so the name reads clearly against the shield.
	colGradFrom color.Color = charmtone.Dolly
	colGradTo   color.Color = charmtone.Charple
	colWordFrom color.Color = charmtone.Salt
	colWordTo   color.Color = charmtone.Dolly

	// ansiPalette remaps the basic ANSI-16 terminal colours that programs emit
	// (e.g. `ls --color`, `git`, `grep --color`) onto legible Charmtone colours,
	// so raw shell-tool output reads on-theme instead of relying on the user's
	// terminal defaults. Indices 0–7 normal, 8–15 bright. Mirrors Crush's
	// CharmtonePantera ANSI mapping.
	ansiPalette = [16]color.Color{
		charmtone.BBQ,     // 0  black
		charmtone.Coral,   // 1  red
		charmtone.Guac,    // 2  green
		charmtone.Mustard, // 3  yellow
		charmtone.Charple, // 4  blue
		charmtone.Dolly,   // 5  magenta
		charmtone.Malibu,  // 6  cyan
		charmtone.Smoke,   // 7  white
		charmtone.Iron,    // 8  bright black
		charmtone.Tuna,    // 9  bright red
		charmtone.Julep,   // 10 bright green
		charmtone.Zest,    // 11 bright yellow
		charmtone.Guppy,   // 12 bright blue
		charmtone.Blush,   // 13 bright magenta
		charmtone.Sardine, // 14 bright cyan
		charmtone.Salt,    // 15 bright white
	}

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
