package termsafe

import "testing"

func TestNextSeq(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		wantN  int
		wantSt SeqStatus
	}{
		{"empty", "", 0, SeqInvalid},
		{"not a sequence", "abc", 0, SeqInvalid},
		{"lone ESC", "\x1b", 1, SeqPartial},
		{"CSI introducer only", "\x1b[", 2, SeqPartial},
		{"CSI params, no final", "\x1b[?2026", 7, SeqPartial},
		{"DA1 reply", "\x1b[?62;1;6c", 10, SeqComplete},
		{"DECRPM reply", "\x1b[?2026;2$y", 11, SeqComplete},
		{"CSI then trailing input", "\x1b[Aj", 3, SeqComplete},
		{"APC introducer only", "\x1b_", 2, SeqPartial},
		{"APC unterminated", "\x1b_Gi=31;O", 9, SeqPartial},
		{"APC with ST", "\x1b_Gi=31;OK\x1b\\", 12, SeqComplete},
		{"DCS with ST", "\x1bP1+r5463=\x1b\\", 12, SeqComplete},
		{"OSC with BEL", "\x1b]0;title\x07", 10, SeqComplete},
		{"two-byte C1", "\x1bM", 2, SeqComplete},
		{"ST alone", "\x1b\\", 2, SeqComplete},
		{"unrecognized ESC form", "\x1b0", 0, SeqInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, st := NextSeq(tc.in)
			if n != tc.wantN || st != tc.wantSt {
				t.Errorf("NextSeq(%q) = (%d, %v), want (%d, %v)", tc.in, n, st, tc.wantN, tc.wantSt)
			}
		})
	}
}

// TestNextSeqAgreesWithStripping keeps the exported incremental view and the
// stripping path on the same recognizer: a sequence NextSeq calls complete is
// one StripControlSeqs removes whole.
func TestNextSeqAgreesWithStripping(t *testing.T) {
	for _, seq := range []string{
		"\x1b[?62;1;6c", "\x1b[?2026;2$y", "\x1b_Gi=31;OK\x1b\\",
		"\x1bP1+r5463=\x1b\\", "\x1b]0;title\x07", "\x1b[31m",
	} {
		n, st := NextSeq(seq)
		if st != SeqComplete || n != len(seq) {
			t.Fatalf("NextSeq(%q) = (%d, %v), want the whole string complete", seq, n, st)
		}
		if got := StripControlSeqs("A" + seq + "B"); got != "AB" {
			t.Errorf("StripControlSeqs kept %q from %q", got, seq)
		}
	}
}
