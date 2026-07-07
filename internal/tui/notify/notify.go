// Package notify builds the terminal escape sequences for the TUI's
// attention system (P16.1): a bell and/or OS desktop notification fired when
// a run finishes, needs approval, or errors while the user isn't looking.
// Title updates are handled separately by the caller via tea.View.WindowTitle
// (OSC 0/2), which needs no raw sequences of its own — this package only
// covers the channels bubbletea has no built-in field for.
package notify

import "strings"

// Mode selects which notification channels are active.
type Mode string

const (
	Off     Mode = "off"     // no bell, no desktop notification
	Bell    Mode = "bell"    // terminal BEL only
	Desktop Mode = "desktop" // OSC 9 / OSC 777 only
	Both    Mode = "both"    // bell + desktop (default)
)

// ParseMode normalizes a config string to a known Mode. Anything unrecognized
// (including empty) resolves to Both, so notifications are on by default.
func ParseMode(s string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case Off:
		return Off
	case Bell:
		return Bell
	case Desktop:
		return Desktop
	default:
		return Both
	}
}

// Event is a terminal-attention-worthy occurrence: stream-end,
// approval-pending, or error.
type Event struct {
	Title string // desktop notification title, e.g. "Aegis"
	Body  string // desktop notification body
}

const (
	esc = "\x1b"
	bel = "\a"
)

// Sequence returns the raw escape sequence to emit for the given mode and
// event, or "" if the mode has nothing to send (Off).
func Sequence(mode Mode, ev Event) string {
	var b strings.Builder
	if mode == Bell || mode == Both {
		b.WriteString(bel)
	}
	if mode == Desktop || mode == Both {
		b.WriteString(desktopSequence(ev.Title, ev.Body))
	}
	return b.String()
}

// desktopSequence emits both OSC 9 (iTerm2, Windows Terminal, kitty, and most
// others) and OSC 777 (Konsole and some multiplexers) so either convention is
// picked up; a terminal that understands neither just ignores both as inert
// escape sequences.
func desktopSequence(title, body string) string {
	title, body = sanitize(title), sanitize(body)
	osc9 := esc + "]9;" + body + bel
	osc777 := esc + "]777;notify;" + title + ";" + body + bel
	return osc9 + osc777
}

// sanitize strips control characters and semicolons so untrusted text (e.g.
// an error message or tool name) can't break out of the escape sequence or
// smuggle a second one.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.ReplaceAll(s, ";", ",")
}
