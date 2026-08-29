// Package termsafe strips terminal control sequences from text that is about
// to be written to a terminal. It has one home rather than one copy per
// surface: the TUI (internal/tui) and the plain CLI renderer
// (internal/cli/chat_render.go) both write model-generated prose and raw tool
// output to a real terminal, and both need exactly these two policies.
package termsafe

import (
	"strings"
	"unicode/utf8"
)

// StripControlSeqs removes ANSI/OSC/C1 terminal control sequences from s,
// leaving normal printable text (including UTF-8) untouched.
//
// The input on this path is exactly the model's own generated prose (P24.20,
// FIND-17): if adversarial content ever reaches the model's output verbatim
// (e.g. via a prompt-injection vector reproducing attacker text), an
// unsanitized terminal renderer could be manipulated — cursor
// repositioning, hidden/overwritten text, or OSC-based clipboard/title-bar
// tricks in terminals that support them. glamour renders markdown structure
// but does not itself strip unrelated raw escape sequences embedded in the
// source text, and the plain-text fallback does no stripping either, so the
// raw text is sanitized here before either path sees it. Unlike the TUI's
// remapANSI16 (which rewrites SGR colour codes in already-trusted shell-tool
// output and deliberately preserves them), this strips every recognized
// control sequence outright, since none of them are legitimate in
// model-generated markdown prose — the renderer's own chrome/styling is
// applied separately via lipgloss/glamour after this point, not embedded in
// the raw model text.
func StripControlSeqs(s string) string {
	if !strings.ContainsRune(s, 0x1b) && !strings.ContainsAny(s, c0AndDel) && !containsC1(s) {
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
		// DEL, then the C1 control range — see c1SkipLen. This comment used to
		// claim the C1 half was already handled here; only the DEL test below
		// existed, so the property a reader would have relied on was not
		// actually provided. SEC-G.
		if c == 0x7f { // DEL
			i++
			continue
		}
		// Everything at or above 0x80 is decoded as a rune rather than copied
		// byte-by-byte. That is what makes the C1 strip below safe: the C1
		// range (0x80-0x9f) overlaps UTF-8 *continuation* bytes exactly, so a
		// byte-wise test would mistake the second byte of "À" (0xc3 0x80) for a
		// control and corrupt the text. Advancing a whole rune at a time means
		// c1SkipLen is only ever asked about a rune boundary.
		if c >= 0x80 {
			if skip := c1SkipLen(s[i:]); skip > 0 {
				i += skip
				continue
			}
			_, size := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+size])
			i += size
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

// StripDangerousSeqs removes terminal control sequences that can manipulate
// the terminal beyond text colour — OSC/DCS/APC/PM/SOS strings (OSC 8
// hyperlink-text spoofing, OSC 52 clipboard hijack, OSC 0/2 title-bar
// spoofing), other C1 control forms, and any CSI sequence other than SGR
// ("select graphic rendition", ESC '[' ... 'm') — which covers cursor
// movement/hiding and alternate-screen-buffer switches. SGR sequences are
// left untouched so a subsequent remapANSI16 pass still has colour codes to
// rewrite.
//
// Unlike StripControlSeqs (which strips SGR too, since it only ever runs on
// the model's own generated prose where no legitimate colour is expected),
// this is for untrusted raw tool output — shell stdout/stderr, read_file
// contents, grep/web_fetch/web_search results (P28.1) — that legitimately
// carries ANSI colour from the tool it came from.
func StripDangerousSeqs(s string) string {
	if !strings.ContainsRune(s, 0x1b) && !strings.ContainsAny(s, c0AndDel) && !containsC1(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	i := 0
	n := len(s)
	for i < n {
		c := s[i]

		if c == 0x1b { // ESC
			if consumed := escSeqLen(s[i:]); consumed > 0 {
				// Keep only CSI SGR sequences (ESC '[' ... 'm'); every other
				// recognized form — CSI cursor/mode changes, OSC, DCS/APC/PM/SOS,
				// other 7-bit C1 forms — is dropped.
				if s[i+1] == '[' && s[i+consumed-1] == 'm' {
					b.WriteString(s[i : i+consumed])
				}
				i += consumed
				continue
			}
			i++
			continue
		}

		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			i++
			continue
		}
		if c == 0x7f { // DEL
			i++
			continue
		}
		// Everything at or above 0x80 is decoded as a rune rather than copied
		// byte-by-byte. That is what makes the C1 strip below safe: the C1
		// range (0x80-0x9f) overlaps UTF-8 *continuation* bytes exactly, so a
		// byte-wise test would mistake the second byte of "À" (0xc3 0x80) for a
		// control and corrupt the text. Advancing a whole rune at a time means
		// c1SkipLen is only ever asked about a rune boundary.
		if c >= 0x80 {
			if skip := c1SkipLen(s[i:]); skip > 0 {
				i += skip
				continue
			}
			_, size := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+size])
			i += size
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

// c0AndDel is the set of C0 control bytes (minus tab/LF/CR, which are
// meaningful in normal text) plus DEL, used by both fast paths.
const c0AndDel = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1c\x1d\x1e\x1f\x7f"

// containsC1 reports whether s holds a C1 control in either spelling. It
// exists so the fast paths cannot return early on a string whose only
// dangerous content is an 8-bit C1 byte.
func containsC1(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 && c <= 0x9f {
			return true
		}
		if s[i] == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] <= 0x9f {
			return true
		}
	}
	return false
}

// c1SkipLen reports how many bytes to drop when s begins with a C1 control,
// and 0 when it does not. s must begin at a rune boundary.
//
// C1 controls have two spellings that reach a terminal as bytes. The raw
// 8-bit form is a lone 0x80-0x9f byte: it cannot begin a valid UTF-8 rune
// (leading bytes are < 0x80 or >= 0xc2), so at a rune boundary it is a
// control, not text — 0x9b in particular is 8-bit CSI, the same introducer as
// ESC '['. The encoded form is U+0080-U+009F written properly as UTF-8, which
// is always 0xc2 followed by 0x80-0x9f. Neither is legitimate in prose or in
// tool output, and the ESC-prefixed 7-bit form is handled by escSeqLen above.
//
// In practice a modern terminal in UTF-8 mode renders a lone 0x9b as U+FFFD
// rather than acting on it, so this is defense in depth rather than a live
// exploit — but it is the property this package's documentation asserts, and
// asserting it without providing it is worse than either.
func c1SkipLen(s string) int {
	c := s[0]
	if c >= 0x80 && c <= 0x9f {
		return 1
	}
	if c == 0xc2 && len(s) >= 2 && s[1] >= 0x80 && s[1] <= 0x9f {
		return 2
	}
	return 0
}
