package tui

import "testing"

func TestStripControlSeqs(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text passthrough", "plain text, no escapes", "plain text, no escapes"},
		{
			"unicode passthrough",
			"héllo wörld — 日本語 🎉 café",
			"héllo wörld — 日本語 🎉 café",
		},
		{
			"markdown passthrough",
			"# Heading\n\n- item one\n- item two\n\n```go\nfunc f() {}\n```\n",
			"# Heading\n\n- item one\n- item two\n\n```go\nfunc f() {}\n```\n",
		},
		{
			"cursor repositioning CSI stripped",
			"before\x1b[10;5Hafter",
			"beforeafter",
		},
		{
			"cursor up/hide CSI stripped",
			"x\x1b[2Ay\x1b[?25lz",
			"xyz",
		},
		{
			"SGR color CSI stripped",
			"\x1b[31mred text\x1b[0m",
			"red text",
		},
		{
			"OSC terminal title (BEL terminated) stripped",
			"before\x1b]0;evil title\x07after",
			"beforeafter",
		},
		{
			"OSC hyperlink (ST terminated) stripped",
			"click \x1b]8;;http://evil.example\x1b\\here\x1b]8;;\x1b\\ done",
			"click here done",
		},
		{
			"OSC 52 clipboard trick stripped",
			"data\x1b]52;c;ZXZpbA==\x07end",
			"dataend",
		},
		{
			"bare ESC dropped",
			"a\x1bb",
			"ab",
		},
		{
			"C0 control bytes dropped, tab/newline/CR kept",
			"a\x01b\tc\nd\re",
			"ab\tc\nd\re",
		},
		{
			"DEL dropped",
			"a\x7fb",
			"ab",
		},
		{
			"unterminated OSC at end of string dropped",
			"before\x1b]0;no terminator",
			"before",
		},
		{
			"empty string",
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripControlSeqs(tt.in)
			if got != tt.want {
				t.Errorf("stripControlSeqs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestStripControlSeqsIdempotent confirms a second pass is a no-op, i.e. the
// function fully removes what it recognizes rather than leaving partial
// escape fragments that could reassemble into a sequence.
func TestStripControlSeqsIdempotent(t *testing.T) {
	in := "hello \x1b[31mworld\x1b[0m \x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\ done\x1b[10;5H!"
	once := stripControlSeqs(in)
	twice := stripControlSeqs(once)
	if once != twice {
		t.Errorf("not idempotent: once=%q twice=%q", once, twice)
	}
}

// TestStripDangerousSeqs covers P28.1: unlike stripControlSeqs, SGR colour
// (used by remapANSI16 on legitimate tool output) must survive; every other
// sequence class must not.
func TestStripDangerousSeqs(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text passthrough", "plain text, no escapes", "plain text, no escapes"},
		{
			"unicode passthrough",
			"héllo wörld — 日本語 🎉 café",
			"héllo wörld — 日本語 🎉 café",
		},
		{
			"SGR color CSI preserved",
			"\x1b[31mred text\x1b[0m",
			"\x1b[31mred text\x1b[0m",
		},
		{
			"bare SGR reset preserved",
			"a\x1b[mb",
			"a\x1b[mb",
		},
		{
			"cursor repositioning CSI stripped",
			"before\x1b[10;5Hafter",
			"beforeafter",
		},
		{
			"cursor up/hide CSI stripped, SGR kept",
			"x\x1b[2A\x1b[31my\x1b[?25lz\x1b[0m",
			"x\x1b[31myz\x1b[0m",
		},
		{
			"alternate screen buffer switch stripped",
			"before\x1b[?1049hafter\x1b[?1049l",
			"beforeafter",
		},
		{
			"OSC terminal title (BEL terminated) stripped",
			"before\x1b]0;evil title\x07after",
			"beforeafter",
		},
		{
			"OSC hyperlink (ST terminated) stripped",
			"click \x1b]8;;http://evil.example\x1b\\here\x1b]8;;\x1b\\ done",
			"click here done",
		},
		{
			"OSC 52 clipboard trick stripped",
			"data\x1b]52;c;ZXZpbA==\x07end",
			"dataend",
		},
		{
			"bare ESC dropped",
			"a\x1bb",
			"ab",
		},
		{
			"C0 control bytes dropped, tab/newline/CR kept",
			"a\x01b\tc\nd\re",
			"ab\tc\nd\re",
		},
		{
			"DEL dropped",
			"a\x7fb",
			"ab",
		},
		{
			"unterminated OSC at end of string dropped",
			"before\x1b]0;no terminator",
			"before",
		},
		{
			"unterminated CSI at end of string dropped",
			"before\x1b[31",
			"before",
		},
		{
			"empty string",
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripDangerousSeqs(tt.in)
			if got != tt.want {
				t.Errorf("stripDangerousSeqs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestStripDangerousSeqsIdempotent mirrors TestStripControlSeqsIdempotent:
// a second pass must be a no-op, including over the SGR sequences this
// variant deliberately preserves.
func TestStripDangerousSeqsIdempotent(t *testing.T) {
	in := "hello \x1b[31mworld\x1b[0m \x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\ done\x1b[10;5H!"
	once := stripDangerousSeqs(in)
	twice := stripDangerousSeqs(once)
	if once != twice {
		t.Errorf("not idempotent: once=%q twice=%q", once, twice)
	}
}
