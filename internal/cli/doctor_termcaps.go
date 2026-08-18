package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/termcaps"
	"github.com/fiddler110/aegis/internal/tui"
)

// doctorTerminalCapsCheck reports what the terminal itself said about the
// capabilities the TUI can use (P67.9).
//
// Before P67.9 there was nothing to report but a guess: the kitty tier was
// chosen from a TERM substring, so the strongest honest statement was that it
// was *plausible*. The probe replaces the guess with an answer, and this row is
// where the difference shows — "supported" when the terminal answered,
// "plausible" only when nobody could be asked (doctor's output piped to a file,
// CI, a non-TTY service context), which is exactly when it is still the truth.
//
// It never runs a probe of its own: termcaps.Cached is one round-trip per
// process, shared with the TUI's startup resolution.
func doctorTerminalCapsCheck(cfg *config.Config) doctorCheck {
	return doctorTerminalCapsRow(termcaps.Cached(), cfg.TUI.ImageRendering, os.Environ())
}

func doctorTerminalCapsRow(caps termcaps.Caps, setting string, environ []string) doctorCheck {
	const name = "terminal capabilities"
	setting = strings.TrimSpace(setting)

	if !strings.HasPrefix(caps.Source, "probed") {
		plausible := "no"
		if tui.KittyPlausible(environ) {
			plausible = "yes"
		}
		return doctorCheck{
			Name: name, Severity: doctorPass,
			Detail: fmt.Sprintf("%s; kitty graphics plausible from the environment: %s",
				caps.Source, plausible),
		}
	}

	detail := fmt.Sprintf("supported: %s — %s", caps.Summary(), caps.Source)
	switch {
	case setting == "kitty" && !caps.KittyGraphics:
		return doctorCheck{
			Name: name, Severity: doctorWarn,
			Detail: detail + `; tui.image_rendering is pinned to "kitty" but this terminal did not answer the graphics query`,
			Fix:    `set tui.image_rendering: auto — the kitty tier is auto-selected when the terminal says it supports it; keep the pin only for a terminal you know supports it but that answers no queries`,
		}
	case caps.KittyGraphics && setting != "" && setting != "auto" && setting != "kitty":
		detail += fmt.Sprintf("; kitty graphics is supported but tui.image_rendering is pinned to %q", setting)
	}
	return doctorCheck{Name: name, Severity: doctorPass, Detail: detail}
}
