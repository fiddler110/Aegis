package tui

import (
	"fmt"
	"strings"
)

// validDashboardSections is the recognized set of sidebar section
// identifiers a tui.dashboard.sections config entry may name (P<dashboard>).
// Kept in sync with the section closures renderSidebar dispatches through.
var validDashboardSections = map[string]bool{
	"session":   true,
	"mode":      true,
	"approvals": true,
	"model":     true,
	"sandbox":   true,
	"cron":      true,
	"tools":     true,
	"files":     true,
	"agents":    true,
	"context":   true,
	"cost":      true,
}

// defaultDashboardSections is today's fixed sidebar order, used whenever
// tui.dashboard.sections is empty/unset — approvals sits right after
// session/mode since a pending approval is time-sensitive and actionable,
// not something that should be scrolled past.
var defaultDashboardSections = []string{
	"session", "mode", "approvals", "model", "sandbox",
	"cron", "tools", "files", "agents", "context", "cost",
}

// validateDashboardSections rejects any name in names that isn't a known
// section identifier, combining every unknown name into one error — the same
// fail-fast-before-model-construction posture keymap.go's applyKeybindings
// uses for tui.keybindings, so a config typo errors at startup instead of
// silently doing nothing.
func validateDashboardSections(names []string) error {
	var unknown []string
	for _, n := range names {
		if !validDashboardSections[strings.ToLower(strings.TrimSpace(n))] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("tui.dashboard.sections: unknown section(s): %s", strings.Join(unknown, ", "))
	}
	return nil
}
