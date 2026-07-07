package tui

import "testing"

func TestApplyThemeSwitchesSchemeAndGlamourStyle(t *testing.T) {
	defer applyTheme("dark", "") // restore for other tests: styles read package vars

	applyTheme("light", "")
	if glamourStyleName != "light" {
		t.Fatalf("expected glamour style %q, got %q", "light", glamourStyleName)
	}
	lightBg := colSurface

	applyTheme("dark", "")
	if glamourStyleName != "dark" {
		t.Fatalf("expected glamour style %q, got %q", "dark", glamourStyleName)
	}
	if colSurface == lightBg {
		t.Fatal("expected dark and light base backgrounds to differ")
	}

	// Unknown names fall back to dark rather than leaving stale colors.
	applyTheme("light", "")
	if got := applyTheme("solarized-nonsense", ""); got != "dark" {
		t.Errorf("expected resolved name %q, got %q", "dark", got)
	}
	if glamourStyleName != "dark" {
		t.Fatalf("expected unknown theme to fall back to dark, got %q", glamourStyleName)
	}
}

func TestApplyThemeLoadsEmbeddedBuiltins(t *testing.T) {
	defer applyTheme("dark", "") // restore for other tests: styles read package vars

	for _, name := range builtinThemeNames() {
		got := applyTheme(name, "")
		if got != name {
			t.Errorf("expected resolved name %q, got %q", name, got)
		}
	}
}
