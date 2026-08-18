package termsafe

// SeqStatus classifies what NextSeq found at the head of its input.
type SeqStatus int

const (
	// SeqInvalid means the input does not begin a recognized escape
	// sequence; callers drop one byte and rescan.
	SeqInvalid SeqStatus = iota
	// SeqPartial means the input begins a recognized sequence that is not
	// terminated yet — more bytes may complete it. Incremental readers must
	// keep the bytes and read again rather than acting on them.
	SeqPartial
	// SeqComplete means the first n bytes are one whole escape sequence.
	SeqComplete
)

// NextSeq reports the length and completeness of the escape sequence at the
// start of s.
//
// It exists so incremental readers of a terminal's *input* stream — the
// capability probe in internal/termcaps, which has to tell a DA1 reply from a
// kitty APC reply from a stray keystroke — can share this package's one
// recognizer instead of growing a second escape-sequence parser. Stripping
// (StripControlSeqs/StripDangerousSeqs) works on complete strings and can
// treat an unterminated sequence as "drop the rest"; a reader that is still
// waiting for bytes cannot, which is the only reason this returns a status
// rather than just a length.
//
// The recognized forms are exactly escSeqLen's: CSI, OSC, DCS/APC/PM/SOS, and
// the two-byte 7-bit C1 forms.
func NextSeq(s string) (int, SeqStatus) {
	if len(s) == 0 || s[0] != 0x1b {
		return 0, SeqInvalid
	}
	if len(s) == 1 {
		return 1, SeqPartial // a lone ESC could still be the start of anything
	}
	n := escSeqLen(s)
	if n == 0 {
		return 0, SeqInvalid
	}
	switch s[1] {
	case '[': // CSI — complete only when a final byte (0x40-0x7e) was reached.
		// n >= 3 matters: escSeqLen returns the scan position for an
		// unterminated CSI, and for the two-byte "\x1b[" that position lands
		// on '[' itself, which is inside the final-byte range.
		if last := s[n-1]; n >= 3 && last >= 0x40 && last <= 0x7e {
			return n, SeqComplete
		}
		return n, SeqPartial
	case ']', 'P', 'X', '^', '_': // string sequences — complete on BEL or ST
		if s[n-1] == 0x07 {
			return n, SeqComplete
		}
		if n >= 2 && s[n-2] == 0x1b && s[n-1] == '\\' {
			return n, SeqComplete
		}
		return n, SeqPartial
	default:
		return n, SeqComplete // two-byte C1 form
	}
}
