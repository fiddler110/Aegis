package tui

import (
	"embed"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// builtinThemesFS holds the popular color-scheme JSON files Aegis ships
// embedded in the binary (P16.7), materialized the same way builtin skills
// are (see internal/skills/embedded.go) but read directly here since themes
// have no on-disk artifact the model needs to inspect.
//
//go:embed themes/builtin
var builtinThemesFS embed.FS

// themeFile is the on-disk/embedded JSON shape for a loadable theme: the
// standard 16-color ANSI palette plus background/foreground, the same shape
// most published terminal color schemes (Alacritty, iTerm2, Windows
// Terminal) already ship in, so popular themes like catppuccin/dracula/
// gruvbox/tokyonight need no bespoke authoring — every semantic colorScheme
// role is derived from these 18 colors.
type themeFile struct {
	Name       string `json:"name"`
	Dark       bool   `json:"dark"` // true selects the "dark" glamour style, false "light"
	Background string `json:"background"`
	Foreground string `json:"foreground"`

	Black   string `json:"black"`
	Red     string `json:"red"`
	Green   string `json:"green"`
	Yellow  string `json:"yellow"`
	Blue    string `json:"blue"`
	Magenta string `json:"magenta"`
	Cyan    string `json:"cyan"`
	White   string `json:"white"`

	BrightBlack   string `json:"brightBlack"`
	BrightRed     string `json:"brightRed"`
	BrightGreen   string `json:"brightGreen"`
	BrightYellow  string `json:"brightYellow"`
	BrightBlue    string `json:"brightBlue"`
	BrightMagenta string `json:"brightMagenta"`
	BrightCyan    string `json:"brightCyan"`
	BrightWhite   string `json:"brightWhite"`
}

var hexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// parseHexColor validates and wraps a "#rgb"/"#rrggbb" string as a color.Color.
func parseHexColor(field, s string) (color.Color, error) {
	if !hexColorRe.MatchString(s) {
		return nil, fmt.Errorf("field %q: invalid hex color %q", field, s)
	}
	return lipgloss.Color(s), nil
}

// toScheme derives a full colorScheme from the theme file's 18 base colors.
// Foreground/background tiers and separators are blended from the base pair
// (reusing the same blend() helper P16.3 introduced for diff tints) rather
// than requiring every theme author to hand-pick a dozen extra shades.
func (tf themeFile) toScheme() (colorScheme, error) {
	fields := map[string]string{
		"background":    tf.Background,
		"foreground":    tf.Foreground,
		"black":         tf.Black,
		"red":           tf.Red,
		"green":         tf.Green,
		"yellow":        tf.Yellow,
		"blue":          tf.Blue,
		"magenta":       tf.Magenta,
		"cyan":          tf.Cyan,
		"white":         tf.White,
		"brightBlack":   tf.BrightBlack,
		"brightRed":     tf.BrightRed,
		"brightGreen":   tf.BrightGreen,
		"brightYellow":  tf.BrightYellow,
		"brightBlue":    tf.BrightBlue,
		"brightMagenta": tf.BrightMagenta,
		"brightCyan":    tf.BrightCyan,
		"brightWhite":   tf.BrightWhite,
	}
	parsed := make(map[string]color.Color, len(fields))
	for name, hex := range fields {
		if strings.TrimSpace(hex) == "" {
			return colorScheme{}, fmt.Errorf("missing required field %q", name)
		}
		c, err := parseHexColor(name, hex)
		if err != nil {
			return colorScheme{}, err
		}
		parsed[name] = c
	}

	background, foreground := parsed["background"], parsed["foreground"]

	s := colorScheme{
		primary:   parsed["brightMagenta"],
		secondary: parsed["brightBlue"],
		accentAlt: parsed["green"],
		keyword:   parsed["magenta"],

		fgBase: foreground,
		fgSub:  blend(foreground, background, 0.25),
		fgMore: blend(foreground, background, 0.45),
		fgMost: blend(foreground, background, 0.65),

		bgBase:  background,
		bgLeast: blend(background, foreground, 0.06),
		bgLess:  blend(background, foreground, 0.12),
		bgMost:  blend(background, foreground, 0.22),

		separator: blend(background, foreground, 0.16),
		onPrimary: lipgloss.Color("#FFFFFF"),

		destructive: parsed["red"],
		errorC:      parsed["brightRed"],
		warn:        parsed["yellow"],
		warnSubtle:  parsed["brightYellow"],
		denied:      parsed["red"],
		busy:        parsed["brightYellow"],
		info:        parsed["blue"],
		infoMore:    parsed["brightBlue"],
		infoMost:    parsed["brightCyan"],
		successRole: parsed["green"],
		successMore: parsed["brightGreen"],
		successMost: blend(parsed["brightGreen"], lipgloss.Color("#FFFFFF"), 0.25),

		ansi: [16]color.Color{
			parsed["black"], parsed["red"], parsed["green"], parsed["yellow"],
			parsed["blue"], parsed["magenta"], parsed["cyan"], parsed["white"],
			parsed["brightBlack"], parsed["brightRed"], parsed["brightGreen"], parsed["brightYellow"],
			parsed["brightBlue"], parsed["brightMagenta"], parsed["brightCyan"], parsed["brightWhite"],
		},
	}
	s.shield = s.primary
	s.gradFrom, s.gradTo = s.secondary, s.primary
	s.wordFrom, s.wordTo = s.fgBase, s.primary
	if tf.Dark {
		s.glamourStyle = "dark"
	} else {
		s.glamourStyle = "light"
	}
	return s, nil
}

// themeDirs returns the standard directories searched for theme files:
// project-local first, user-global second (mirrors persona.DiscoverDirs /
// skills.Discover — project overrides user, both override embedded builtins).
func themeDirs(workDir string) []string {
	var dirs []string
	if workDir != "" {
		dirs = append(dirs, filepath.Join(workDir, ".aegis", "themes"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".aegis", "themes"))
	}
	return dirs
}

// loadThemeFile reads and parses a single theme JSON file.
func loadThemeFile(path string) (colorScheme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return colorScheme{}, err
	}
	var tf themeFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return colorScheme{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return tf.toScheme()
}

// loadBuiltinTheme reads an embedded built-in theme by name.
func loadBuiltinTheme(name string) (colorScheme, bool) {
	data, err := builtinThemesFS.ReadFile("themes/builtin/" + name + ".json")
	if err != nil {
		return colorScheme{}, false
	}
	var tf themeFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return colorScheme{}, false
	}
	s, err := tf.toScheme()
	if err != nil {
		return colorScheme{}, false
	}
	return s, true
}

// builtinThemeNames lists every embedded built-in theme, sorted.
func builtinThemeNames() []string {
	entries, err := builtinThemesFS.ReadDir("themes/builtin")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
	}
	sort.Strings(names)
	return names
}

// loadNamedScheme resolves name to a colorScheme, searching (in order)
// project .aegis/themes/, user ~/.aegis/themes/, then the embedded builtins.
// The two hardcoded originals ("dark"/"light") are checked first since they
// are Go structs, not JSON files, and remain the unconditional fallback.
func loadNamedScheme(name, workDir string) (colorScheme, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dark":
		return darkScheme(), true
	case "light":
		return lightScheme(), true
	}
	for _, dir := range themeDirs(workDir) {
		path := filepath.Join(dir, name+".json")
		if s, err := loadThemeFile(path); err == nil {
			return s, true
		}
	}
	return loadBuiltinTheme(strings.ToLower(strings.TrimSpace(name)))
}

// availableThemeNames lists every theme name known without touching the
// filesystem beyond a directory listing: "dark", "light", any project/user
// theme files, and the embedded builtins — used for /theme's error message
// and tab completion.
func availableThemeNames(workDir string) []string {
	seen := map[string]bool{"dark": true, "light": true}
	names := []string{"dark", "light"}
	for _, dir := range themeDirs(workDir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
				continue
			}
			n := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	for _, n := range builtinThemeNames() {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return names
}
