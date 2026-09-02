package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestWelcomeContentMentionsBacktrack guards the discoverability fix: the
// welcome screen's hint line previously never mentioned rewind/checkpoints
// at all, so a new user had no reason to learn Esc-Esc opens the backtrack
// picker. The addition must be purely additive — the original three hints
// stay present.
func TestWelcomeContentMentionsBacktrack(t *testing.T) {
	applyScheme(darkScheme())
	th := newTheme()
	got := ansi.Strip(buildWelcomeContent(Config{Model: "m", Mode: "build"}, "/tmp", th))

	for _, want := range []string{"/help", "ctrl+k", "shift+tab", "esc esc"} {
		if !strings.Contains(got, want) {
			t.Errorf("welcome content missing hint %q, got:\n%s", want, got)
		}
	}
}
