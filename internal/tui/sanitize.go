package tui

import "strings"

// stripControlSeqs removes ANSI/OSC/C1 terminal control sequences from s,
// leaving normal printable text (including UTF-8) untouched.
//
// mdRender's input here is exactly the model's own generated prose (P24.20,
// FIND-17): if adversarial content ever reaches the model's output verbatim
// (e.g. via a prompt-injection vector reproducing attacker text), an
// unsanitized terminal renderer could be manipulated — cursor
// repositioning, hidden/overwritten text, or OSC-based clipboard/title-bar
// tricks in terminals that support them. glamour renders markdown structure
// but does not itself strip unrelated raw escape sequences embedded in the
// source text, and the plain-wrap fallback does no stripping either, so the
// raw text is sanitized here before either path sees it. Unlike ansi16.go's
// remapANSI16 (which rewrites SGR colour codes in already-trusted shell-tool
// output and deliberately preserves them), this strips every recognized
// control sequence outright, since none of them are legitimate in
// model-generated markdown prose — the TUI's own chrome/styling is applied
// separately via lipgloss/glamour after this point, not embedded in the raw
// model text.
func stripControlSeqs(s string) string {
	if !strings.ContainsRune(s, 0x1b) && !strings.ContainsAny(s, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1c\x1d\x1e\x1f\x7f") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	i := 0
	n := len(s)
	for i < n {
		c := s[i]

		if c == 0x1b { // ESC
			// Try to recognize and skip a full escape sequence; if it isn't
			// one of the recognized forms, drop just the ESC byte itself so
			// we never emit a bare ESC into the render pipeline.
			if consumed := escSeqLen(s[i:]); consumed > 0 {
				i += consumed
				continue
			}
			i++
			continue
		}

		// Other C0 control bytes: drop everything except the whitespace
		// controls that are meaningful in normal text (tab, LF, CR) — glamour
		// and the wrap fallback both rely on these.
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			i++
			continue
		}
		// DEL and C1 control range (0x80-0x9f) when expressed as raw bytes
		// (rather than the ESC-prefixed 7-bit form handled above) — UTF-8
		// continuation bytes never appear as a lone byte in this range at the
		// start of a rune, so this only ever strips genuine C1 controls, not
		// multi-byte UTF-8 sequences (whose bytes are all >= 0x80 but whose
		// leading byte is >= 0xC0).
		if c == 0x7f {
			i++
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

// escSeqLen returns the number of bytes of s (which starts with ESC, 0x1b)
// that make up a recognized escape sequence, or 0 if s doesn't start with
// one it recognizes (in which case the caller drops just the ESC byte).
//
// Recognized forms:
//   - CSI:  ESC '[' params... final-byte   (params/intermediate 0x20-0x3f, final 0x40-0x7e)
//   - OSC:  ESC ']' ... (BEL | ESC '\')      -- terminated by BEL or ST
//   - DCS/APC/PM/SOS (ESC 'P'/'X'/'^'/'_'): ... terminated by ST (ESC '\') or BEL
//   - Other 7-bit C1 forms ESC <byte in 0x40-0x5f>: two-byte sequence
func escSeqLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b {
		return 0
	}
	switch s[1] {
	case '[': // CSI
		i := 2
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
			i++
		}
		if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
			return i + 1
		}
		return i // unterminated at end of string — drop what we scanned
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC — string terminated by BEL or ST
		i := 2
		for i < len(s) {
			if s[i] == 0x07 { // BEL
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' { // ST (ESC \)
				return i + 2
			}
			i++
		}
		return i // unterminated — drop through end of string
	default:
		if s[1] >= 0x40 && s[1] <= 0x5f {
			// Other 7-bit C1 two-byte sequences (e.g. ESC 'M' reverse index).
			return 2
		}
		return 0
	}
}
