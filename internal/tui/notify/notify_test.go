package notify

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"off":     Off,
		"OFF":     Off,
		" bell ":  Bell,
		"desktop": Desktop,
		"both":    Both,
		"":        Both,
		"bogus":   Both,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSequenceOff(t *testing.T) {
	if got := Sequence(Off, Event{Title: "Aegis", Body: "hi"}); got != "" {
		t.Errorf("Sequence(Off, ...) = %q, want empty", got)
	}
}

func TestSequenceBellOnly(t *testing.T) {
	got := Sequence(Bell, Event{Title: "Aegis", Body: "hi"})
	if got != bel {
		t.Errorf("Sequence(Bell, ...) = %q, want just BEL", got)
	}
}

func TestSequenceDesktopOnly(t *testing.T) {
	got := Sequence(Desktop, Event{Title: "Aegis", Body: "hi"})
	if strings.Count(got, bel) == 0 {
		t.Fatalf("expected desktop sequence to still be BEL-terminated, got %q", got)
	}
	if got[:1] == bel {
		// Desktop-only must not lead with a bare bell as the first byte —
		// the OSC prefix should come first.
		t.Errorf("Sequence(Desktop, ...) = %q, unexpectedly starts with a bare BEL", got)
	}
}

func TestSequenceBoth(t *testing.T) {
	got := Sequence(Both, Event{Title: "Aegis", Body: "hi"})
	if !strings.HasPrefix(got, bel) {
		t.Errorf("Sequence(Both, ...) = %q, want to start with BEL then the OSC sequences", got)
	}
	if !strings.Contains(got, "]9;hi") {
		t.Errorf("Sequence(Both, ...) = %q, missing OSC 9 body", got)
	}
	if !strings.Contains(got, "]777;notify;Aegis;hi") {
		t.Errorf("Sequence(Both, ...) = %q, missing OSC 777 fallback", got)
	}
}

func TestSanitizeStripsControlAndSemicolons(t *testing.T) {
	got := sanitize("bad\x07;text\x1b[31m")
	if strings.ContainsAny(got, ";\x07\x1b") {
		t.Errorf("sanitize left dangerous characters: %q", got)
	}
	if got != "bad,text[31m" {
		t.Errorf("sanitize(...) = %q, want %q", got, "bad,text[31m")
	}
}
