package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/termcaps"
)

func smallPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestDetectKittyGraphics(t *testing.T) {
	cases := []struct {
		env  []string
		want bool
	}{
		{[]string{"KITTY_WINDOW_ID=1"}, true},
		{[]string{"TERM=xterm-kitty"}, true},
		{[]string{"TERM_PROGRAM=ghostty"}, true},
		{[]string{"TERM_PROGRAM=WezTerm"}, true},
		{[]string{"KONSOLE_VERSION=220400"}, true},
		{[]string{"TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := detectKittyGraphics(c.env); got != c.want {
			t.Errorf("detectKittyGraphics(%v) = %v, want %v", c.env, got, c.want)
		}
	}
}

func TestImageProtoForKitty(t *testing.T) {
	if got := imageProtoFor("kitty"); got != protocolKitty {
		t.Errorf(`imageProtoFor("kitty") = %v, want protocolKitty`, got)
	}
	if got := imageProtoFor("off"); got != protocolNone {
		t.Errorf(`imageProtoFor("off") = %v, want protocolNone`, got)
	}
	// "halfblock" forces the safe tier however the terminal answered.
	if got := imageProtoFor("halfblock"); got == protocolKitty {
		t.Error(`imageProtoFor("halfblock") must never resolve to protocolKitty`)
	}
	// Under `go test` stdin/stdout are pipes, so the P67.9 probe cannot run and
	// "auto" has no affirmative answer to act on: it must not guess kitty.
	if got := imageProtoFor("auto"); got == protocolKitty {
		t.Error(`imageProtoFor("auto") auto-selected protocolKitty with no probe answer`)
	}
}

// TestAutoImageProtoNeedsAnAnswer is P67.9's rule: the kitty tier is chosen
// from what the terminal *said*, never from what TERM looks like, and the
// colour floor still vetoes inline images entirely.
func TestAutoImageProtoNeedsAnAnswer(t *testing.T) {
	kittyish := []string{"TERM=xterm-kitty", "KITTY_WINDOW_ID=1", "COLORTERM=truecolor"}
	plain := []string{"TERM=xterm-256color"}

	cases := []struct {
		name    string
		caps    termcaps.Caps
		environ []string
		want    imageProtocol
	}{
		{"answered yes", termcaps.Caps{KittyGraphics: true, Probed: true}, plain, protocolKitty},
		{"answered no, TERM says kitty", termcaps.Caps{Probed: true}, kittyish, protocolHalfBlock},
		{"never asked, TERM says kitty", termcaps.Caps{}, kittyish, protocolHalfBlock},
		{"answered yes but NO_COLOR", termcaps.Caps{KittyGraphics: true, Probed: true},
			[]string{"TERM=xterm-256color", "NO_COLOR=1"}, protocolNone},
		{"answered yes but dumb terminal", termcaps.Caps{KittyGraphics: true, Probed: true},
			[]string{"TERM=dumb"}, protocolNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoImageProto(tc.caps, tc.environ); got != tc.want {
				t.Errorf("autoImageProto(%+v, %v) = %v, want %v", tc.caps, tc.environ, got, tc.want)
			}
		})
	}
}

func TestKittyGraphicsSequenceStructure(t *testing.T) {
	seq := kittyGraphicsSequence(smallPNG(t, 4, 4), 6, 3)
	if !strings.HasPrefix(seq, "\x1b_G") {
		t.Fatalf("sequence should start with the APC _G introducer, got %q", seq[:8])
	}
	if !strings.Contains(seq, "f=100,a=T,c=6,r=3") {
		t.Fatalf("first control block missing/incorrect:\n%q", seq)
	}
	if !strings.HasSuffix(seq, "\x1b\\") {
		t.Fatal("sequence should end with the ST terminator")
	}
}

func TestKittyGraphicsSequenceChunking(t *testing.T) {
	// A payload larger than one chunk must split into multiple APC envelopes,
	// each ≤ kittyChunk of base64, with m=1 on all but the last (m=0).
	big := bytes.Repeat([]byte{0xAB}, 3*kittyChunk) // ~4096*4 base64 chars → 4 chunks
	seq := kittyGraphicsSequence(big, 10, 5)

	envelopes := strings.Split(strings.TrimSuffix(seq, "\x1b\\"), "\x1b\\")
	if len(envelopes) < 3 {
		t.Fatalf("expected multiple envelopes for a large payload, got %d", len(envelopes))
	}
	for i, e := range envelopes {
		semi := strings.IndexByte(e, ';')
		if semi < 0 {
			t.Fatalf("envelope %d has no control/payload separator: %q", i, e)
		}
		control, payload := e[:semi], e[semi+1:]
		if len(payload) > kittyChunk {
			t.Errorf("envelope %d payload %d exceeds chunk cap %d", i, len(payload), kittyChunk)
		}
		wantMore := i < len(envelopes)-1
		hasMore := strings.Contains(control, "m=1")
		if wantMore != hasMore {
			t.Errorf("envelope %d: m=1 present=%v, want %v (control=%q)", i, hasMore, wantMore, control)
		}
	}
	if !strings.Contains(envelopes[len(envelopes)-1], "m=0") {
		t.Error("final envelope should carry m=0")
	}
}

func TestRenderImageThumbnailKitty(t *testing.T) {
	out := renderImageThumbnail(smallPNG(t, 20, 10), protocolKitty)
	if out == "" {
		t.Fatal("expected a kitty escape for a decodable PNG")
	}
	if !strings.HasPrefix(out, "\x1b_G") {
		t.Fatal("kitty thumbnail should begin with the graphics escape")
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatal("kitty thumbnail should reserve vertical space with trailing newlines")
	}
	// Undecodable bytes still return "" (never-error contract).
	if renderImageThumbnail([]byte("not an image"), protocolKitty) != "" {
		t.Error("undecodable data should render nothing")
	}
}
