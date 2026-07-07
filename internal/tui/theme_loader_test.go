package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinThemeNamesListsShippedThemes(t *testing.T) {
	names := builtinThemeNames()
	want := []string{"catppuccin", "dracula", "gruvbox", "tokyonight"}
	if len(names) != len(want) {
		t.Fatalf("expected %d builtin themes, got %v", len(want), names)
	}
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Errorf("expected builtin theme %q, got %v", w, names)
		}
	}
}

func TestLoadNamedSchemeResolvesEmbeddedBuiltins(t *testing.T) {
	for _, name := range builtinThemeNames() {
		s, ok := loadNamedScheme(name, "")
		if !ok {
			t.Fatalf("expected builtin %q to load", name)
		}
		if s.bgBase == nil || s.fgBase == nil {
			t.Errorf("theme %q: expected non-nil base colors", name)
		}
	}
}

func TestLoadNamedSchemeUnknownNameFails(t *testing.T) {
	if _, ok := loadNamedScheme("not-a-real-theme", ""); ok {
		t.Fatal("expected unknown theme name to fail resolution")
	}
}

func TestLoadNamedSchemeProjectOverridesUser(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir) // os.UserHomeDir on Windows reads USERPROFILE

	writeTheme := func(dir, bg string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data := customThemeJSON(bg)
		if err := os.WriteFile(filepath.Join(dir, "mytheme.json"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTheme(filepath.Join(homeDir, ".aegis", "themes"), "#000000")
	writeTheme(filepath.Join(projectRoot, ".aegis", "themes"), "#111111")

	s, ok := loadNamedScheme("mytheme", projectRoot)
	if !ok {
		t.Fatal("expected mytheme to resolve")
	}
	r, g, b, _ := s.bgBase.RGBA()
	if r>>8 != 0x11 || g>>8 != 0x11 || b>>8 != 0x11 {
		t.Errorf("expected project theme (bg #111111) to win over user theme, got rgb(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestThemeFileRejectsInvalidHex(t *testing.T) {
	dir := t.TempDir()
	data := customThemeJSON("not-a-hex-color")
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadThemeFile(path); err == nil {
		t.Fatal("expected an error for an invalid hex color")
	}
}

func TestThemeFileRejectsMissingField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incomplete.json")
	if err := os.WriteFile(path, []byte(`{"name":"incomplete","dark":true,"background":"#111111"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadThemeFile(path); err == nil {
		t.Fatal("expected an error for a theme file missing required fields")
	}
}

func TestAvailableThemeNamesIncludesBuiltinsAndCustom(t *testing.T) {
	projectRoot := t.TempDir()
	dir := filepath.Join(projectRoot, ".aegis", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom.json"), []byte(customThemeJSON("#222222")), 0o644); err != nil {
		t.Fatal(err)
	}

	names := availableThemeNames(projectRoot)
	for _, want := range []string{"dark", "light", "custom", "dracula"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q in available theme names, got %v", want, names)
		}
	}
}

// customThemeJSON returns a minimal but complete theme file with the given
// background color, used to test project/user file resolution and
// validation without depending on the shipped builtin palettes.
func customThemeJSON(background string) string {
	return `{
		"name": "mytheme",
		"dark": true,
		"background": "` + background + `",
		"foreground": "#eeeeee",
		"black": "#000000",
		"red": "#ff0000",
		"green": "#00ff00",
		"yellow": "#ffff00",
		"blue": "#0000ff",
		"magenta": "#ff00ff",
		"cyan": "#00ffff",
		"white": "#ffffff",
		"brightBlack": "#111111",
		"brightRed": "#ff1111",
		"brightGreen": "#11ff11",
		"brightYellow": "#ffff11",
		"brightBlue": "#1111ff",
		"brightMagenta": "#ff11ff",
		"brightCyan": "#11ffff",
		"brightWhite": "#ffffff"
	}`
}
