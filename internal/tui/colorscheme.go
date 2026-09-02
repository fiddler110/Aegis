package tui

import (
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// colorScheme is one complete set of semantic colors plus the matching
// glamour markdown style (TQ10). The package-level col* variables render
// sites use are assigned from a scheme by applyScheme; built-ins are
// darkScheme (Charmtone "Pantera", the original hardcoded look) and
// lightScheme, selected via the tui.theme config key.
type colorScheme struct {
	// Brand
	primary, secondary, accentAlt, keyword color.Color

	// Foreground tiers (brightest → faintest against the base background)
	fgBase, fgSub, fgMore, fgMost color.Color

	// Background / surface tiers (base → most visible)
	bgBase, bgLeast, bgLess, bgMost color.Color

	// selectionBg (P74.18) is a solid fill for the drag-selection overlay —
	// it replaces a cell's background while leaving its foreground untouched,
	// which is what a real terminal's own selection does. Picked for contrast
	// against this scheme's fgBase specifically, not just eyeballed on dark:
	// a light scheme's near-black text wants a pale fill, a dark scheme's
	// wants a bright one.
	selectionBg color.Color

	separator, onPrimary color.Color

	// Status roles
	destructive, errorC, warn, warnSubtle, denied, busy color.Color
	info, infoMore, infoMost                            color.Color
	successRole, successMore, successMost               color.Color

	// Logo / gradient art
	shield, gradFrom, gradTo, wordFrom, wordTo color.Color

	// agentPalette (P74.13) is a fixed set of distinct hues, one per running
	// sub-agent, assigned by hashing the agent id (see agentColor) so the same
	// teammate reads as the same colour everywhere it appears — sidebar,
	// transcript, status — without threading assignment state anywhere.
	agentPalette [8]color.Color

	// ANSI-16 remap for raw shell-tool output
	ansi [16]color.Color

	// glamour style name ("dark" / "light") matched to this scheme
	glamourStyle string
}

// darkScheme is the Charmtone "Pantera" set — the exact role mapping Crush
// uses (see internal/ui/styles/themes.go → CharmtonePantera) so the UI reads
// as a single cohesive dark theme rather than an ad-hoc set of hexes.
func darkScheme() colorScheme {
	return colorScheme{
		primary:   charmtone.Charple, // brand purple / primary accent
		secondary: charmtone.Dolly,   // secondary accent (lavender)
		accentAlt: charmtone.Bok,     // tertiary accent (green)
		keyword:   charmtone.Blush,   // keyword / highlight pink

		fgBase: charmtone.Sash,   // primary body text
		fgSub:  charmtone.Smoke,  // secondary text
		fgMore: charmtone.Squid,  // tertiary / dim labels
		fgMost: charmtone.Oyster, // faintest / muted captions

		bgBase:  charmtone.Pepper, // base background
		bgLeast: charmtone.BBQ,    // least-visible raised surface
		bgLess:  charmtone.Char,   // low-contrast surface
		bgMost:  charmtone.Iron,   // most-visible surface / borders

		// A bright fill against the dark base — visibly past bgMost, not a
		// subtle tint — so it reads as "selected" over any chroma foreground.
		selectionBg: blend(charmtone.Pepper, charmtone.Charple, 0.55),

		separator: charmtone.Char,
		onPrimary: charmtone.Butter,

		destructive: charmtone.Coral,
		errorC:      charmtone.Sriracha,
		warn:        charmtone.Mustard,
		warnSubtle:  charmtone.Zest,
		denied:      charmtone.Tang,
		busy:        charmtone.Citron,
		info:        charmtone.Malibu,
		infoMore:    charmtone.Sardine,
		infoMost:    charmtone.Damson,
		successRole: charmtone.Julep,
		successMore: charmtone.Bok,
		successMost: charmtone.Guac,

		// Cool lavender crest → brand purple point; brighter horizontal ramp
		// for the AEGIS wordmark so the name reads against the shield.
		shield:   charmtone.Guppy,
		gradFrom: charmtone.Dolly,
		gradTo:   charmtone.Charple,
		wordFrom: charmtone.Salt,
		wordTo:   charmtone.Dolly,

		agentPalette: [8]color.Color{
			charmtone.Charple, // purple
			charmtone.Malibu,  // blue
			charmtone.Julep,   // green
			charmtone.Mustard, // yellow
			charmtone.Coral,   // red/orange
			charmtone.Blush,   // pink
			charmtone.Sardine, // cyan
			charmtone.Guppy,   // bright blue
		},

		ansi: [16]color.Color{
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
		},

		glamourStyle: "dark",
	}
}

// lightScheme mirrors the dark role set for light terminal backgrounds: same
// brand purple, darkened text tiers, paper surfaces, and deeper status hues
// so everything keeps contrast against a near-white base.
func lightScheme() colorScheme {
	return colorScheme{
		primary:   charmtone.Charple,         // brand purple still reads on light
		secondary: lipgloss.Color("#7A5CDB"), // deeper lavender
		accentAlt: lipgloss.Color("#1A7F37"), // green
		keyword:   lipgloss.Color("#BF3989"), // highlight pink

		fgBase: lipgloss.Color("#1F2328"), // near-black body text
		fgSub:  lipgloss.Color("#424A53"),
		fgMore: lipgloss.Color("#6E7781"),
		fgMost: lipgloss.Color("#8C959F"),

		bgBase:  lipgloss.Color("#FAFAFA"),
		bgLeast: lipgloss.Color("#F1F1F4"),
		bgLess:  lipgloss.Color("#E8E8EE"),
		bgMost:  lipgloss.Color("#D0D3DC"),

		// A pale fill against the near-white base and near-black fgBase —
		// enough to read as "selected" without the fg/bg contrast a bright
		// fill would cost on light.
		selectionBg: lipgloss.Color("#C7D2FE"),

		separator: lipgloss.Color("#DCDDE4"),
		onPrimary: lipgloss.Color("#FFFFFF"),

		destructive: lipgloss.Color("#C4432B"),
		errorC:      lipgloss.Color("#B3261E"),
		warn:        lipgloss.Color("#9A6700"),
		warnSubtle:  lipgloss.Color("#BF8700"),
		denied:      lipgloss.Color("#BC4C00"),
		busy:        lipgloss.Color("#7D8500"),
		info:        lipgloss.Color("#0969DA"),
		infoMore:    lipgloss.Color("#218BFF"),
		infoMost:    lipgloss.Color("#54AEFF"),
		successRole: lipgloss.Color("#1A7F37"),
		successMore: lipgloss.Color("#2DA44E"),
		successMost: lipgloss.Color("#4AC26B"),

		shield:   lipgloss.Color("#6639BA"),
		gradFrom: lipgloss.Color("#8250DF"),
		gradTo:   charmtone.Charple,
		wordFrom: lipgloss.Color("#1F2328"),
		wordTo:   lipgloss.Color("#6639BA"),

		agentPalette: [8]color.Color{
			lipgloss.Color("#7A5CDB"), // purple
			lipgloss.Color("#0969DA"), // blue
			lipgloss.Color("#1A7F37"), // green
			lipgloss.Color("#9A6700"), // amber
			lipgloss.Color("#C4432B"), // red
			lipgloss.Color("#BF3989"), // pink
			lipgloss.Color("#1B7C83"), // cyan
			lipgloss.Color("#BC4C00"), // orange
		},

		ansi: [16]color.Color{
			lipgloss.Color("#24292F"), // 0  black
			lipgloss.Color("#CF222E"), // 1  red
			lipgloss.Color("#116329"), // 2  green
			lipgloss.Color("#4D2D00"), // 3  yellow
			lipgloss.Color("#0969DA"), // 4  blue
			lipgloss.Color("#8250DF"), // 5  magenta
			lipgloss.Color("#1B7C83"), // 6  cyan
			lipgloss.Color("#6E7781"), // 7  white
			lipgloss.Color("#57606A"), // 8  bright black
			lipgloss.Color("#A40E26"), // 9  bright red
			lipgloss.Color("#1A7F37"), // 10 bright green
			lipgloss.Color("#633C01"), // 11 bright yellow
			lipgloss.Color("#218BFF"), // 12 bright blue
			lipgloss.Color("#A475F9"), // 13 bright magenta
			lipgloss.Color("#3192AA"), // 14 bright cyan
			lipgloss.Color("#8C959F"), // 15 bright white
		},

		glamourStyle: "light",
	}
}

// Semantic package-level color roles. Render sites keep these stable names;
// applyScheme rebinds them all when the theme changes. Colors satisfy the
// color.Color interface and degrade gracefully to 256- and 16-color
// terminals. Initialized to the dark scheme; tui.Run applies the configured
// theme before any styles are built (styles capture colors at creation).
var (
	// --- Brand ---
	colPrimary   color.Color
	colSecondary color.Color
	colAccentAlt color.Color
	colKeyword   color.Color

	// --- Foreground tiers (brightest → faintest) ---
	colFgBase color.Color
	colFgSub  color.Color
	colFgMore color.Color
	colFgMost color.Color

	// --- Background / surface tiers (base → most visible) ---
	colBgBase color.Color
	colBgLess color.Color
	colBgMost color.Color

	// colSelectionBg (P74.18) is the drag-selection overlay's fill — set
	// background only, leave foreground untouched, unlike the old SGR-7
	// Reverse() treatment which fragmented over per-cell chroma colors.
	colSelectionBg color.Color

	colSeparator color.Color
	colOnPrimary color.Color

	// --- Status roles ---
	colDestructive color.Color
	colError       color.Color
	colWarn        color.Color
	colWarnSubtle  color.Color
	colDenied      color.Color
	colBusy        color.Color
	colInfo        color.Color
	colSuccessRole color.Color
	colSuccessMore color.Color
	colSuccessMost color.Color

	// --- Aegis-semantic aliases (derived from the roles above) ---
	colSurface   color.Color // base background
	colBorder    color.Color // visible borders / rules
	colTextDim   color.Color // secondary body text
	colTextMuted color.Color // muted / caption text
	colAccent    color.Color
	colSuccess   color.Color
	colWarning   color.Color
	colDanger    color.Color

	colUserFg    color.Color // user messages
	colAssistFg  color.Color // assistant messages
	colToolFg    color.Color // tool output
	colToolErrFg color.Color // tool errors

	colBrandBg  color.Color // brand chip background
	colBrandFg  color.Color // brand chip text
	colCwd      color.Color // working directory
	colInputSep color.Color // input border separator

	// colDiffAddBg / colDiffDelBg (P16.3) are subtle background tints for
	// added/removed diff lines — colSuccessRole/colDestructive blended low
	// into colBgBase so a tinted line still reads as "this theme's
	// background", not an unrelated highlighter colour.
	colDiffAddBg color.Color
	colDiffDelBg color.Color

	// Gradient ramp endpoints for the shield art and AEGIS wordmark.
	colGradFrom color.Color
	colGradTo   color.Color
	colWordFrom color.Color
	colWordTo   color.Color

	// colAgentPalette (P74.13) is the active scheme's fixed 8-colour agent
	// set; agentColor hashes an agent id into it.
	colAgentPalette [8]color.Color

	// ansiPalette remaps the basic ANSI-16 terminal colours that programs
	// emit (e.g. `ls --color`, `git`) onto legible theme colours, so raw
	// shell-tool output reads on-theme instead of relying on the user's
	// terminal defaults. Indices 0–7 normal, 8–15 bright.
	ansiPalette [16]color.Color

	// glamourStyleName is the glamour markdown style matched to the active
	// scheme, consumed by newGlamourRenderer.
	glamourStyleName string
)

func init() { applyScheme(darkScheme()) }

// applyTheme selects the active color scheme by name (tui.theme config, or
// /theme). Beyond the two hardcoded originals ("dark"/"light"), name is
// resolved via loadNamedScheme (P16.7): project .aegis/themes/<name>.json,
// then user ~/.aegis/themes/<name>.json, then an embedded builtin
// (catppuccin, dracula, gruvbox, tokyonight). Unknown/unparseable names fall
// back to dark. Returns the name actually applied — the resolved (lowercased)
// input name on success, else "dark" — so callers can keep cfg.Theme in sync
// without a separate normalization pass.
//
// Must run before newModel, since lipgloss styles capture colors at
// creation. Also called at runtime by /theme: rebinding the package-level
// col* vars here is only half of a live switch — the caller must also
// rebuild m.th (newTheme) and m.streamState.renderer (newGlamourRenderer), which capture
// colors/style at creation time too.
// isAutoTheme reports whether a configured theme name requests automatic
// light/dark detection from the terminal background (P40.5). Empty and "auto"
// both mean auto; any explicit scheme name opts out.
func isAutoTheme(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return true
	default:
		return false
	}
}

func applyTheme(name, workDir string) string {
	resolved := strings.ToLower(strings.TrimSpace(name))
	if s, ok := loadNamedScheme(resolved, workDir); ok {
		applyScheme(s)
		return resolved
	}
	applyScheme(darkScheme())
	return "dark"
}

// applyScheme rebinds every package-level color role, the derived aliases,
// the ANSI-16 remap, and the glamour style from s.
func applyScheme(s colorScheme) {
	colPrimary, colSecondary, colAccentAlt, colKeyword = s.primary, s.secondary, s.accentAlt, s.keyword
	colFgBase, colFgSub, colFgMore, colFgMost = s.fgBase, s.fgSub, s.fgMore, s.fgMost
	colBgBase, colBgLess, colBgMost = s.bgBase, s.bgLess, s.bgMost
	colSelectionBg = s.selectionBg
	colSeparator, colOnPrimary = s.separator, s.onPrimary

	colDestructive, colError = s.destructive, s.errorC
	colWarn, colWarnSubtle = s.warn, s.warnSubtle
	colDenied, colBusy = s.denied, s.busy
	colInfo = s.info
	colSuccessRole, colSuccessMore, colSuccessMost = s.successRole, s.successMore, s.successMost

	colSurface = colBgBase
	colBorder = colBgMost
	colTextDim = colFgSub
	colTextMuted = colFgMost
	colAccent = colPrimary
	colSuccess = colSuccessRole
	colWarning = colWarn
	colDanger = colDestructive

	colUserFg = colInfo
	colAssistFg = colPrimary
	colToolFg = colFgMore
	colToolErrFg = colDestructive

	colBrandBg = colPrimary
	colBrandFg = colOnPrimary
	colCwd = colInfo
	colInputSep = colSeparator

	colGradFrom, colGradTo = s.gradFrom, s.gradTo
	colWordFrom, colWordTo = s.wordFrom, s.wordTo
	colAgentPalette = s.agentPalette

	colDiffAddBg = blend(colBgBase, colSuccessRole, 0.16)
	colDiffDelBg = blend(colBgBase, colDestructive, 0.16)

	ansiPalette = s.ansi
	glamourStyleName = s.glamourStyle
}

// agentColor (P74.13) picks a stable colour for an agent id out of the
// active scheme's 8-colour agentPalette. Hashing (rather than assignment
// order) means the same id renders the same colour across the sidebar,
// transcript and status bar without any of them sharing assignment state,
// and survives a restart since nothing is stored.
func agentColor(id string) color.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return colAgentPalette[h.Sum32()%uint32(len(colAgentPalette))]
}

// blend linearly interpolates from base toward tint by t (0 = base, 1 =
// tint), used to derive on-theme background tints (e.g. diff add/del rows)
// from existing role colors instead of hardcoding new hexes per scheme.
func blend(base, tint color.Color, t float64) color.Color {
	br, bg, bb, _ := base.RGBA()
	tr, tg, tb, _ := tint.RGBA()
	lerp := func(a, b uint32) uint8 {
		return uint8(float64(a>>8)*(1-t) + float64(b>>8)*t)
	}
	return color.RGBA{R: lerp(br, tr), G: lerp(bg, tg), B: lerp(bb, tb), A: 0xff}
}
