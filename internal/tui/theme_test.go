package tui

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/fiddler110/aegis/internal/commands"
)

// TestCmdThemeBareArgsShowsSentinel checks the no-args fast path emits the
// "show current theme" sentinel without validating/rejecting anything — the
// dispatcher has no model reference, so the actual current-theme text is
// filled in by tui.go's Update handler, not cmdTheme itself.
func TestCmdThemeBareArgsShowsSentinel(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "theme"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if res.Output != "\x00theme-show" {
		t.Errorf("expected show sentinel, got %q", res.Output)
	}
}

// TestCmdThemeRejectsUnknownName mirrors /mode and /sandbox's validation
// precedent: an invalid theme name is rejected at the dispatcher level, never
// reaching the "\x00theme " sentinel tui.go would otherwise act on.
func TestCmdThemeRejectsUnknownName(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	res := d.Dispatch(&commands.ParsedCommand{Name: "theme", Args: []string{"solarized"}})
	if !res.IsError {
		t.Fatalf("expected an error for an unknown theme name, got: %s", res.Output)
	}
}

// TestCmdThemeAcceptsKnownNames checks both valid names (any case) produce
// the switch sentinel cmdTheme's contract with tui.go's Update handler.
func TestCmdThemeAcceptsKnownNames(t *testing.T) {
	d := NewSlashDispatcher(nil, "sess", "build", "test-model")
	for _, name := range []string{"dark", "Light", "LIGHT"} {
		res := d.Dispatch(&commands.ParsedCommand{Name: "theme", Args: []string{name}})
		if res.IsError {
			t.Fatalf("unexpected error for %q: %s", name, res.Output)
		}
		if !strings.HasPrefix(res.Output, "\x00theme ") {
			t.Errorf("expected switch sentinel for %q, got %q", name, res.Output)
		}
	}
}

// TestThemeSwitchLive drives the real Update path (slashResultMsg carrying
// the "\x00theme " sentinel cmdTheme emits) to verify P14.8's actual runtime
// effect: the active color scheme, m.th, and the glamour renderer's style all
// flip without a restart.
func TestThemeSwitchLive(t *testing.T) {
	defer applyTheme("dark") // restore for other tests: styles read package vars

	m := newModel(Config{SessionID: "test-session", Mode: "build", WorkDir: t.TempDir(), Theme: "dark"})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	if m.cfg.Theme != "dark" {
		t.Fatalf("expected initial theme %q, got %q", "dark", m.cfg.Theme)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00theme light"})
	if m.cfg.Theme != "light" {
		t.Errorf("expected m.cfg.Theme to switch to %q, got %q", "light", m.cfg.Theme)
	}
	if glamourStyleName != "light" {
		t.Errorf("expected glamour style to switch to %q, got %q", "light", glamourStyleName)
	}
	if got := plainView(m); !strings.Contains(got, "Theme switched to light") {
		t.Errorf("expected confirmation message in transcript, got:\n%s", got)
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "\x00theme-show"})
	if got := plainView(m); !strings.Contains(got, "Current theme: light") {
		t.Errorf("expected current-theme message in transcript, got:\n%s", got)
	}
}
