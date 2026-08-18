// Package termcaps asks the terminal what it can do instead of guessing from
// TERM (P67.9).
//
// The problem with asking is that a terminal which does not understand a query
// says nothing at all, so a naive implementation needs a timeout — short enough
// to not sit in startup, long enough to be reliable, and no value is both. The
// trick that removes it: end the batch with a DA1 request (CSI c), which every
// terminal since the VT100 answers, and rely on terminals answering queries in
// the order they were asked. A feature's reply arriving *before* the DA1 reply
// proves the feature exists; DA1 arriving first proves it does not. One
// round-trip for the whole batch and no per-query timeout anywhere.
//
// There is still one outer safety deadline (ProbeDeadline), but it is not part
// of the decision: it only stops a non-conforming terminal, or a pipe that
// happens to pass the isatty check, from hanging startup forever. Every
// decision this package makes is the DA1 ordering rule.
//
// Replies arrive on the same file descriptor as keystrokes, so the probe runs
// once, before bubbletea is started and takes ownership of stdin — see
// probe.go. Results are cached for the process; nothing here is per-frame.
package termcaps

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/fiddler110/aegis/internal/termsafe"
)

// Caps is what the terminal answered. The zero value is "nothing supported,
// nothing asked" — the correct posture for a pipe, a CI runner or `aegis serve`.
type Caps struct {
	// KittyGraphics is true when the terminal answered the kitty graphics
	// protocol query (APC _G … ST) before DA1.
	KittyGraphics bool
	// SyncOutput is true when the terminal reported DECSET 2026 (synchronized
	// output) as a mode it recognizes.
	SyncOutput bool
	// TrueColor is true when the terminal reported the Tc/RGB terminfo
	// capability via XTGETTCAP.
	TrueColor bool
	// Probed is true only when the round-trip completed — i.e. the DA1 reply
	// that terminates the batch was seen. A false Probed with a true feature
	// flag still means the feature was affirmatively answered; it means the
	// terminator was not, so the negatives are unproven.
	Probed bool
	// Source records how these values were arrived at, for `aegis doctor`.
	Source string
}

// Query strings. Each is a question a supporting terminal answers and a
// non-supporting one ignores; the batch is written in one Write so the replies
// come back in this order.
const (
	// queryKitty asks kitty to report on image id 31 (a=q is "query only", so
	// nothing is transmitted or displayed). Supporting terminals answer
	// ESC _ G i=31;OK ESC \ (or an error code, which equally proves they parsed
	// the APC).
	queryKitty = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	// querySync is DECRQM for private mode 2026 (synchronized output). The
	// answer is CSI ? 2026 ; Ps $ y.
	querySync = "\x1b[?2026$p"
	// queryTrueColor is XTGETTCAP for the "Tc" (5463) and "RGB" (524742)
	// terminfo capability names, hex-encoded as the protocol requires.
	queryTrueColor = "\x1bP+q5463;524742\x1b\\"
	// queryDA1 is the terminator: Primary Device Attributes, answered by
	// every terminal since the VT100.
	queryDA1 = "\x1b[c"

	// QueryBatch is the whole batch, in reply order, DA1 last.
	QueryBatch = queryKitty + querySync + queryTrueColor + queryDA1
)

// ErrNoTerminator means the stream ended (or the safety deadline fired) before
// the DA1 reply. Features that were affirmatively answered are still valid;
// the absent ones are simply unproven.
var ErrNoTerminator = errors.New("termcaps: input ended before the DA1 reply")

// maxReplyBytes bounds how much a terminal can send before DA1 arrives. A
// conforming reply batch is a few dozen bytes; this only exists so a terminal
// (or a pipe) that streams forever cannot grow the buffer without bound.
const maxReplyBytes = 64 << 10

// Decide reads replies from r and applies the DA1 ordering rule: every feature
// reply seen before the DA1 reply is supported, and DA1 ends the read. It never
// consults a clock — the caller owns the outer safety deadline, which surfaces
// here as a read error.
//
// Bytes that are not part of a recognized reply (type-ahead keystrokes, garbage
// from a terminal that answered something else) are skipped rather than
// misread; a truncated trailing sequence is simply never classified.
func Decide(r io.Reader) (Caps, error) {
	var caps Caps
	var buf []byte
	tmp := make([]byte, 256)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			var done bool
			buf, done = scan(&caps, buf)
			if done {
				caps.Probed = true
				return caps, nil
			}
			if len(buf) > maxReplyBytes {
				return caps, ErrNoTerminator
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return caps, ErrNoTerminator
			}
			return caps, err
		}
	}
}

// scan consumes every complete escape sequence at the head of buf, folding each
// into caps, and returns the unconsumed remainder plus whether the DA1
// terminator was seen. A partial sequence at the tail is left in the remainder
// for the next read.
func scan(caps *Caps, buf []byte) ([]byte, bool) {
	for len(buf) > 0 {
		i := indexESC(buf)
		if i < 0 {
			return nil, false // no sequence start at all: all stray input
		}
		buf = buf[i:] // drop stray bytes before the sequence (keystrokes, noise)
		n, status := termsafe.NextSeq(string(buf))
		switch status {
		case termsafe.SeqPartial:
			return buf, false // wait for the rest
		case termsafe.SeqInvalid:
			buf = buf[1:] // not a sequence we know: drop the ESC and rescan
			continue
		}
		seq := string(buf[:n])
		buf = buf[n:]
		if classify(caps, seq) {
			return buf, true
		}
	}
	return nil, false
}

func indexESC(b []byte) int {
	for i, c := range b {
		if c == 0x1b {
			return i
		}
	}
	return -1
}

// classify folds one complete reply into caps and reports whether it was the
// DA1 terminator.
func classify(caps *Caps, seq string) bool {
	switch {
	case strings.HasPrefix(seq, "\x1b_G"):
		// Any APC _G answer proves the graphics protocol was parsed; kitty
		// answers "OK" for a supported query and an error code otherwise, and
		// a terminal that does not speak it answers nothing at all.
		caps.KittyGraphics = true
	case strings.HasPrefix(seq, "\x1b[?") && strings.HasSuffix(seq, "$y"):
		caps.SyncOutput = decrpmSupported(seq)
	case strings.HasPrefix(seq, "\x1bP1+r"):
		// DCS 1 + r <hex name>=<hex value> ST — the leading 1 is "valid".
		caps.TrueColor = true
	case strings.HasPrefix(seq, "\x1b[?") && strings.HasSuffix(seq, "c"):
		return true // DA1: the batch is over
	}
	return false
}

// decrpmSupported reads the Ps value out of a DECRPM reply
// (CSI ? 2026 ; Ps $ y) for mode 2026. Per DEC: 0 = not recognized,
// 1 = set, 2 = reset, 3 = permanently set, 4 = permanently reset. Only 1-3
// mean the terminal will actually honor the mode.
func decrpmSupported(seq string) bool {
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b[?"), "$y")
	semi := strings.LastIndexByte(body, ';')
	if semi < 0 {
		return false
	}
	if mode := body[:semi]; mode != "2026" {
		return false
	}
	v, err := strconv.Atoi(body[semi+1:])
	if err != nil {
		return false
	}
	return v >= 1 && v <= 3
}

// Summary renders one human line for `aegis doctor`.
func (c Caps) Summary() string {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	return "kitty graphics=" + yn(c.KittyGraphics) +
		", synchronized output=" + yn(c.SyncOutput) +
		", truecolor=" + yn(c.TrueColor)
}
