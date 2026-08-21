package tui

import "testing"

func TestAgentColorStableAndDistinct(t *testing.T) {
	ids := []string{"agent-alpha", "agent-beta", "agent-gamma"}
	for _, id := range ids {
		first := agentColor(id)
		for i := 0; i < 5; i++ {
			if got := agentColor(id); got != first {
				t.Fatalf("agentColor(%q) not stable across calls: %v vs %v", id, got, first)
			}
		}
	}
	if agentColor(ids[0]) == agentColor(ids[1]) {
		t.Errorf("expected distinct agent ids to usually get distinct colors, %q and %q collided", ids[0], ids[1])
	}
}

func TestIsAutoTheme(t *testing.T) {
	auto := []string{"", "auto", "AUTO", "  Auto  "}
	explicit := []string{"dark", "light", "dracula", "gruvbox"}
	for _, n := range auto {
		if !isAutoTheme(n) {
			t.Errorf("isAutoTheme(%q) = false, want true", n)
		}
	}
	for _, n := range explicit {
		if isAutoTheme(n) {
			t.Errorf("isAutoTheme(%q) = true, want false", n)
		}
	}
}

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
