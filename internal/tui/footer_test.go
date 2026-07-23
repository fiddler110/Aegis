package tui

import (
	"strings"
	"testing"
)

// P40.6: the status-bar hint segment is scoped to the focused input surface.
func TestContextualFooterHints(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", WorkDir: t.TempDir()})
	m.width = 120

	// Chat focus (default): palette / help / editor keys.
	chat := m.contextualFooterHints()
	for _, want := range []string{m.keys.Palette.Help().Key, m.keys.Help.Help().Key, m.keys.Editor.Help().Key} {
		if !strings.Contains(chat, want) {
			t.Errorf("chat hints %q missing %q", chat, want)
		}
	}
	if strings.Contains(chat, "diagnose") {
		t.Errorf("chat hints should not show terminal-only diagnose hint: %q", chat)
	}

	// Sidebar open adds a discoverable resize hint.
	m.sidebarOpen = true
	if !strings.Contains(m.contextualFooterHints(), "resize") {
		t.Errorf("with sidebar open, expected a resize hint, got %q", m.contextualFooterHints())
	}

	// Terminal focus: shows terminal-relevant actions, not the chat palette.
	m.termFocused = true
	term := m.contextualFooterHints()
	if !strings.Contains(term, "diagnose") || !strings.Contains(term, "resize") {
		t.Errorf("terminal hints %q missing expected actions", term)
	}
	if strings.Contains(term, m.keys.Palette.Help().Key) {
		t.Errorf("terminal hints should not show the chat palette key: %q", term)
	}
}
