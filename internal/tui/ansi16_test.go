package tui

import (
	"image/color"
	"strings"
	"testing"
)

func TestRemapANSI16(t *testing.T) {
	pal := [16]color.Color{
		3:  color.RGBA{R: 1, G: 2, B: 3, A: 255},    // yellow
		9:  color.RGBA{R: 10, G: 20, B: 30, A: 255}, // bright red
		12: color.RGBA{R: 40, G: 50, B: 60, A: 255}, // bright blue (bg)
	}

	tests := []struct {
		name, in, want string
	}{
		{"no escapes passthrough", "plain text", "plain text"},
		{"basic fg yellow", "\x1b[33mhi\x1b[0m", "\x1b[38;2;1;2;3mhi\x1b[0m"},
		{"bright fg red", "\x1b[91mx", "\x1b[38;2;10;20;30mx"},
		{"bright bg blue", "\x1b[104mx", "\x1b[48;2;40;50;60mx"},
		{"reset untouched", "\x1b[0mx", "\x1b[0mx"},
		{"bare reset untouched", "\x1b[mx", "\x1b[mx"},
		{"truecolor passthrough", "\x1b[38;2;9;9;9mx", "\x1b[38;2;9;9;9mx"},
		{"256 passthrough", "\x1b[38;5;200mx", "\x1b[38;5;200mx"},
		{"mixed attrs keep bold", "\x1b[1;33mx", "\x1b[1;38;2;1;2;3mx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remapANSI16(tt.in, pal)
			if got != tt.want {
				t.Errorf("remapANSI16(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}

	// A nil palette entry should fall back to the bare introducer (terminal
	// default) rather than emitting a malformed sequence.
	if got := remapANSI16("\x1b[31mx", [16]color.Color{}); !strings.Contains(got, "\x1b[38m") {
		t.Errorf("nil palette entry: got %q, want bare \\x1b[38m introducer", got)
	}
}
