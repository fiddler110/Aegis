package cli

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/termcaps"
)

func TestDoctorTerminalCapsRowReportsSupportedNotPlausible(t *testing.T) {
	probed := termcaps.Caps{
		KittyGraphics: true, SyncOutput: true, TrueColor: true, Probed: true,
		Source: "probed: the terminal answered (DA1-terminated batch)",
	}
	row := doctorTerminalCapsRow(probed, "auto", []string{"TERM=xterm-256color"})
	if row.Severity != doctorPass {
		t.Errorf("severity = %v, want pass", row.Severity)
	}
	if !strings.Contains(row.Detail, "supported") {
		t.Errorf("a probed terminal should be reported as supported, got %q", row.Detail)
	}
	if strings.Contains(row.Detail, "plausible") {
		t.Errorf("a probed terminal must not be hedged as plausible: %q", row.Detail)
	}
	if !strings.Contains(row.Detail, "kitty graphics=yes") {
		t.Errorf("detail should name each answered capability, got %q", row.Detail)
	}
}

func TestDoctorTerminalCapsRowFallsBackToPlausible(t *testing.T) {
	// Piped/CI: nobody could be asked, so "plausible" is the honest word and
	// the environment heuristic is all that is left.
	notProbed := termcaps.Caps{Source: "not probed: stdin/stdout is not a terminal"}
	row := doctorTerminalCapsRow(notProbed, "auto", []string{"TERM=xterm-kitty"})
	if row.Severity != doctorPass {
		t.Errorf("severity = %v, want pass", row.Severity)
	}
	if !strings.Contains(row.Detail, "plausible from the environment: yes") {
		t.Errorf("detail = %q, want the plausible fallback", row.Detail)
	}

	row = doctorTerminalCapsRow(notProbed, "auto", []string{"TERM=xterm-256color"})
	if !strings.Contains(row.Detail, "plausible from the environment: no") {
		t.Errorf("detail = %q, want a negative plausibility for a plain xterm", row.Detail)
	}
}

func TestDoctorTerminalCapsRowWarnsOnAnUnsupportedPin(t *testing.T) {
	probed := termcaps.Caps{Probed: true, TrueColor: true, Source: "probed: the terminal answered (DA1-terminated batch)"}
	row := doctorTerminalCapsRow(probed, "kitty", nil)
	if row.Severity != doctorWarn {
		t.Fatalf("severity = %v, want warn when the pinned tier was not answered", row.Severity)
	}
	if !strings.Contains(row.Fix, "image_rendering") {
		t.Errorf("fix should name the config key, got %q", row.Fix)
	}
}

func TestDoctorTerminalCapsRowNotesAnOverriddenSupportedTier(t *testing.T) {
	probed := termcaps.Caps{KittyGraphics: true, Probed: true, Source: "probed: the terminal answered (DA1-terminated batch)"}
	row := doctorTerminalCapsRow(probed, "halfblock", nil)
	if row.Severity != doctorPass {
		t.Errorf("severity = %v, want pass — an explicit override is a choice, not a fault", row.Severity)
	}
	if !strings.Contains(row.Detail, "pinned to \"halfblock\"") {
		t.Errorf("detail should mention the override, got %q", row.Detail)
	}
}

// The row must appear in the report, not merely be constructible.
func TestDoctorChecksIncludeTerminalCapabilities(t *testing.T) {
	row := doctorTerminalCapsCheck(&config.Config{})
	if row.Name != "terminal capabilities" {
		t.Fatalf("unexpected row name %q", row.Name)
	}
	if row.Detail == "" {
		t.Error("the row must always say something, even with no terminal to ask")
	}
}
