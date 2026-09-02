package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestValidateDashboardSectionsAcceptsKnownSubset(t *testing.T) {
	if err := validateDashboardSections([]string{"cost", "Session", " mode "}); err != nil {
		t.Fatalf("expected known section names (any case/whitespace) to validate, got: %v", err)
	}
	if err := validateDashboardSections(nil); err != nil {
		t.Fatalf("expected an empty/unset list to validate as the default, got: %v", err)
	}
}

func TestValidateDashboardSectionsRejectsUnknown(t *testing.T) {
	err := validateDashboardSections([]string{"cost", "nonexistent_widget"})
	if err == nil {
		t.Fatal("expected an error for an unknown section name")
	}
	if !strings.Contains(err.Error(), "nonexistent_widget") {
		t.Errorf("expected the error to name the offending section, got: %v", err)
	}
}

// TestRunRejectsUnknownDashboardSection is the Run()-level fail-fast guard,
// mirroring how an unknown keybinding action already errors before any
// model/program is constructed (P13.3.5) — a config typo must not be
// silently dropped by renderSidebar.
func TestRunRejectsUnknownDashboardSection(t *testing.T) {
	err := Run(Config{
		SessionID:         "s",
		Mode:              "build",
		Model:             "m",
		WorkDir:           t.TempDir(),
		DashboardSections: []string{"nonexistent_widget"},
	})
	if err == nil {
		t.Fatal("expected Run to error on an unknown dashboard section before starting the program")
	}
}

// TestRenderSidebarRespectsConfiguredSectionOrder asserts a configured subset
// of section names renders only those sections, in the order given.
func TestRenderSidebarRespectsConfiguredSectionOrder(t *testing.T) {
	m := newModel(Config{
		SessionID:         "s",
		Mode:              "build",
		Model:             "m",
		WorkDir:           t.TempDir(),
		DashboardSections: []string{"cost", "session"},
	})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.sidebarOpen = true
	m.costUSD = 1.23
	m.layout()

	got := plainView(m)
	costIdx := strings.Index(got, "◇ COST")
	sessionIdx := strings.Index(got, "◇ SESSION")
	if costIdx == -1 || sessionIdx == -1 {
		t.Fatalf("expected both configured sections present, got:\n%s", got)
	}
	if costIdx > sessionIdx {
		t.Fatalf("expected COST before SESSION per the configured order, got:\n%s", got)
	}
	if strings.Contains(got, "◇ MODE") || strings.Contains(got, "◇ MODEL") {
		t.Fatalf("expected sections outside the configured subset to be omitted, got:\n%s", got)
	}
}
