package termcaps

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Recorded reply shapes, as a real terminal sends them.
const (
	replyKitty = "\x1b_Gi=31;OK\x1b\\"
	replySync  = "\x1b[?2026;2$y" // mode recognized, currently reset
	replyNoMod = "\x1b[?2026;0$y" // mode not recognized
	replyPerm  = "\x1b[?2026;4$y" // recognized but permanently reset
	replyTC    = "\x1bP1+r5463=\x1b\\"
	replyNoTC  = "\x1bP0+r5463\x1b\\"
	replyDA1   = "\x1b[?62;1;6;22c"
)

// chunkReader hands out the stream in fixed-size pieces so the incremental
// scanner is exercised across sequence boundaries, then reports err.
type chunkReader struct {
	s   string
	n   int
	err error
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.s == "" {
		if c.err != nil {
			return 0, c.err
		}
		return 0, io.EOF
	}
	n := c.n
	if n <= 0 || n > len(c.s) {
		n = len(c.s)
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, c.s[:n])
	c.s = c.s[n:]
	return n, nil
}

func TestDecideDA1Ordering(t *testing.T) {
	cases := []struct {
		name                    string
		stream                  string
		kitty, sync, tc, probed bool
		wantErr                 error
	}{
		{
			name:   "kitty-supporting terminal answers everything",
			stream: replyKitty + replySync + replyTC + replyDA1,
			kitty:  true, sync: true, tc: true, probed: true,
		},
		{
			// The whole point of the trick: a terminal that stays silent on a
			// query is answered for by DA1 arriving first.
			name:   "kitty-silent terminal answers only DA1",
			stream: replyDA1,
			probed: true,
		},
		{
			name:   "silent on kitty, answers the other two",
			stream: replySync + replyTC + replyDA1,
			sync:   true, tc: true, probed: true,
		},
		{
			// Nothing after DA1 counts, however supportive it looks: it is
			// either a late reply from a non-conforming terminal or the user's
			// first keystrokes, and neither is evidence.
			name:   "replies after DA1 are not evidence",
			stream: replyDA1 + replyKitty + replySync + replyTC,
			probed: true,
		},
		{
			name:   "out of order: kitty answered late but still before DA1",
			stream: replySync + replyKitty + replyDA1,
			kitty:  true, sync: true, probed: true,
		},
		{
			name:   "DECRPM says the mode is unknown",
			stream: replyKitty + replyNoMod + replyDA1,
			kitty:  true, probed: true,
		},
		{
			name:   "DECRPM says permanently reset",
			stream: replyPerm + replyDA1,
			probed: true,
		},
		{
			name:   "XTGETTCAP invalid response is not truecolor",
			stream: replyNoTC + replyDA1,
			probed: true,
		},
		{
			name:   "type-ahead keystrokes between replies are skipped",
			stream: "hello" + replyKitty + "j" + replySync + "\x7f" + replyDA1,
			kitty:  true, sync: true, probed: true,
		},
		{
			name:    "garbage only: no DA1, nothing concluded",
			stream:  "not a terminal reply at all\r\n",
			wantErr: ErrNoTerminator,
		},
		{
			name:    "truncated: kitty answered, stream cut before DA1",
			stream:  replyKitty + replySync + "\x1b[?62;1",
			kitty:   true,
			sync:    true,
			wantErr: ErrNoTerminator,
		},
		{
			name:    "truncated mid-APC: nothing is concluded from a partial reply",
			stream:  "\x1b_Gi=31;O",
			wantErr: ErrNoTerminator,
		},
		{
			name:   "a bare ESC in the stream does not derail the scan",
			stream: "\x1b" + replyKitty + replyDA1,
			kitty:  true, probed: true,
		},
	}

	// Every case is run at several chunk sizes: a reply split across two reads
	// must decide identically to one delivered whole.
	for _, tc := range cases {
		for _, chunk := range []int{0, 1, 3, 7} {
			t.Run(tc.name, func(t *testing.T) {
				caps, err := Decide(&chunkReader{s: tc.stream, n: chunk})
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("chunk=%d err = %v, want %v", chunk, err, tc.wantErr)
				}
				if caps.KittyGraphics != tc.kitty {
					t.Errorf("chunk=%d KittyGraphics = %v, want %v", chunk, caps.KittyGraphics, tc.kitty)
				}
				if caps.SyncOutput != tc.sync {
					t.Errorf("chunk=%d SyncOutput = %v, want %v", chunk, caps.SyncOutput, tc.sync)
				}
				if caps.TrueColor != tc.tc {
					t.Errorf("chunk=%d TrueColor = %v, want %v", chunk, caps.TrueColor, tc.tc)
				}
				if caps.Probed != tc.probed {
					t.Errorf("chunk=%d Probed = %v, want %v", chunk, caps.Probed, tc.probed)
				}
			})
		}
	}
}

// TestScanStopsAtDA1 pins that scanning stops at the terminator and hands back
// everything after it unexamined. (Whatever already sat in the same read buffer
// is unavoidably consumed — a Reader cannot be un-read — which is exactly why
// the probe runs before bubbletea owns stdin rather than alongside it.)
func TestScanStopsAtDA1(t *testing.T) {
	var caps Caps
	rest, done := scan(&caps, []byte(replyKitty+replyDA1+"typed"+replySync))
	if !done {
		t.Fatal("scan did not report the DA1 terminator")
	}
	if !caps.KittyGraphics {
		t.Error("the kitty reply before DA1 should have been folded in")
	}
	if caps.SyncOutput {
		t.Error("a reply after DA1 must not be counted")
	}
	if string(rest) != "typed"+replySync {
		t.Errorf("remainder = %q, want everything after DA1 left alone", rest)
	}
}

func TestDecidePropagatesReadErrors(t *testing.T) {
	want := errors.New("boom")
	if _, err := Decide(&chunkReader{s: "", err: want}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestDecideBoundsUnboundedGarbage(t *testing.T) {
	// A stream that never terminates must not grow the buffer forever.
	huge := strings.Repeat("\x1b[1;1H", maxReplyBytes/2)
	caps, err := Decide(&chunkReader{s: huge + huge, n: 4096})
	if !errors.Is(err, ErrNoTerminator) {
		t.Fatalf("err = %v, want ErrNoTerminator", err)
	}
	if caps.KittyGraphics || caps.SyncOutput || caps.TrueColor {
		t.Errorf("cursor-move noise concluded a capability: %+v", caps)
	}
}

func TestQueryBatchEndsWithDA1(t *testing.T) {
	if !strings.HasSuffix(QueryBatch, queryDA1) {
		t.Fatal("the batch must end with DA1 — it is the terminator the whole design rests on")
	}
	if strings.Count(QueryBatch, queryDA1) != 1 {
		t.Fatal("exactly one DA1 request, or the ordering rule has two terminators")
	}
	for _, q := range []string{queryKitty, querySync, queryTrueColor} {
		if !strings.Contains(QueryBatch, q) {
			t.Fatalf("query %q missing from the batch", q)
		}
	}
}

func TestOverride(t *testing.T) {
	cases := []struct {
		env               string
		ok                bool
		kitty, sync, tcol bool
	}{
		{env: "", ok: false},
		{env: "AEGIS_TERM_CAPS=", ok: false},
		{env: "AEGIS_TERM_CAPS=auto", ok: false},
		{env: "AEGIS_TERM_CAPS=off", ok: true},
		{env: "AEGIS_TERM_CAPS=none", ok: true},
		{env: "AEGIS_TERM_CAPS=kitty", ok: true, kitty: true},
		{env: "AEGIS_TERM_CAPS=kitty,sync,truecolor", ok: true, kitty: true, sync: true, tcol: true},
		{env: "AEGIS_TERM_CAPS=KITTY, RGB", ok: true, kitty: true, tcol: true},
		{env: "AEGIS_TERM_CAPS=nonsense", ok: true},
	}
	for _, c := range cases {
		var environ []string
		if c.env != "" {
			environ = []string{"TERM=xterm", c.env}
		}
		caps, ok := Override(environ)
		if ok != c.ok {
			t.Fatalf("%q: ok = %v, want %v", c.env, ok, c.ok)
		}
		if !ok {
			continue
		}
		if caps.KittyGraphics != c.kitty || caps.SyncOutput != c.sync || caps.TrueColor != c.tcol {
			t.Errorf("%q: caps = %+v, want kitty=%v sync=%v truecolor=%v", c.env, caps, c.kitty, c.sync, c.tcol)
		}
		if caps.Probed {
			t.Errorf("%q: an override must never claim to have been probed", c.env)
		}
		if caps.Source == "" {
			t.Errorf("%q: an override must say so in Source", c.env)
		}
	}
}

// TestProbeNonTTY covers the piped/CI/`aegis serve` path: no terminal, no
// queries written, no hang, and an answer that admits it is not an answer.
func TestProbeNonTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	done := make(chan Caps, 1)
	go func() { done <- Probe(r, w, nil) }()
	select {
	case caps := <-done:
		if caps.KittyGraphics || caps.SyncOutput || caps.TrueColor || caps.Probed {
			t.Errorf("a pipe reported capabilities: %+v", caps)
		}
		if !strings.Contains(caps.Source, "not probed") {
			t.Errorf("Source = %q, want it to say the probe did not run", caps.Source)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Probe hung on a non-TTY — it must return immediately")
	}

	// Nothing may have been written to the "terminal": no query bytes can
	// escape into a redirected stdout.
	if err := r.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err == nil {
		buf := make([]byte, 1)
		if n, _ := r.Read(buf); n != 0 {
			t.Errorf("Probe wrote %d byte(s) to a non-terminal stdout", n)
		}
	}
}

func TestProbeHonorsEnvOverrideWithoutATerminal(t *testing.T) {
	caps := Probe(nil, nil, []string{"AEGIS_TERM_CAPS=kitty,sync"})
	if !caps.KittyGraphics || !caps.SyncOutput || caps.TrueColor {
		t.Fatalf("caps = %+v, want kitty+sync only", caps)
	}
}

func TestProbeNilHandles(t *testing.T) {
	caps := Probe(nil, nil, nil)
	if caps.Probed || caps.KittyGraphics {
		t.Fatalf("caps = %+v, want the zero posture", caps)
	}
}

func TestSummary(t *testing.T) {
	got := Caps{KittyGraphics: true, TrueColor: true}.Summary()
	want := "kitty graphics=yes, synchronized output=no, truecolor=yes"
	if got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func TestCachedIsStable(t *testing.T) {
	// go test's stdin/stdout are not terminals, so this is also the non-TTY
	// path — the point here is only that the answer is computed once.
	a, b := Cached(), Cached()
	if a != b {
		t.Fatalf("Cached() changed between calls: %+v vs %+v", a, b)
	}
}
