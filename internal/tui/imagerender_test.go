package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestDetectImageProtocol(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want imageProtocol
	}{
		{"dumb terminal", []string{"TERM=dumb"}, protocolNone},
		{"no color", []string{"TERM=xterm-256color", "NO_COLOR=1"}, protocolNone},
		{"basic ansi only", []string{"TERM=xterm"}, protocolNone},
		{"256 color", []string{"TERM=xterm-256color"}, protocolHalfBlock},
		{"truecolor via COLORTERM", []string{"TERM=xterm", "COLORTERM=truecolor"}, protocolHalfBlock},
		{"kitty", []string{"TERM=xterm-kitty"}, protocolHalfBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectImageProtocol(tc.env); got != tc.want {
				t.Errorf("detectImageProtocol(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestImageProtoFor(t *testing.T) {
	if got := imageProtoFor("off"); got != protocolNone {
		t.Errorf("imageProtoFor(off) = %v, want protocolNone", got)
	}
	// "auto" delegates to detectImageProtocol(os.Environ()); just verify it
	// doesn't panic and returns a valid value.
	switch imageProtoFor("auto") {
	case protocolNone, protocolHalfBlock:
	default:
		t.Error("imageProtoFor(auto) returned an unrecognized protocol")
	}
}

func TestThumbnailBox(t *testing.T) {
	cases := []struct {
		name       string
		w, h       int
		wantCols   int
		wantRowsLE int // upper bound; exact value depends on rounding
	}{
		{"zero size", 0, 0, 0, 0},
		{"square", 100, 100, thumbnailMaxCols, thumbnailMaxRows},
		{"wide", 1600, 400, thumbnailMaxCols, thumbnailMaxRows},
		{"tall", 200, 4000, thumbnailMaxCols, thumbnailMaxRows},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cols, rows := thumbnailBox(tc.w, tc.h)
			if tc.wantCols == 0 {
				if cols != 0 || rows != 0 {
					t.Fatalf("thumbnailBox(%d,%d) = (%d,%d), want (0,0)", tc.w, tc.h, cols, rows)
				}
				return
			}
			if cols < 1 || cols > thumbnailMaxCols {
				t.Errorf("cols=%d out of bounds [1,%d]", cols, thumbnailMaxCols)
			}
			if rows < 1 || rows > thumbnailMaxRows {
				t.Errorf("rows=%d out of bounds [1,%d]", rows, thumbnailMaxRows)
			}
		})
	}
}

// solidPNG builds a w×h PNG of a single color, for decode round-trip tests.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture PNG: %v", err)
	}
	return buf.Bytes()
}

func TestRenderImageThumbnail(t *testing.T) {
	data := solidPNG(t, 64, 64, color.RGBA{R: 200, G: 50, B: 50, A: 255})

	if got := renderImageThumbnail(data, protocolNone); got != "" {
		t.Errorf("protocolNone should render nothing, got %q", got)
	}

	out := renderImageThumbnail(data, protocolHalfBlock)
	if out == "" {
		t.Fatal("expected non-empty thumbnail for a valid PNG")
	}
	if !strings.Contains(out, "\x1b[38;2;200;50;50;48;2;200;50;50m") {
		t.Errorf("expected solid-red half-block styling in output, got:\n%s", out)
	}
	wantRows := strings.Count(out, "\n")
	if wantRows < 1 {
		t.Errorf("expected at least one rendered row, got %d", wantRows)
	}
	// Every line must reset styling so it doesn't bleed into whatever the
	// transcript renders next.
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("row missing trailing reset: %q", line)
		}
	}

	if got := renderImageThumbnail([]byte("not an image"), protocolHalfBlock); got != "" {
		t.Errorf("garbage input should render nothing, got %q", got)
	}
}

func TestResizeBoxAvg(t *testing.T) {
	// Two-tone image: left half red, right half blue. Averaging each output
	// column into one of the two halves should keep the colors pure since
	// the split falls on a column boundary.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	out := resizeBoxAvg(img, 2, 1)
	if got := out.RGBAAt(0, 0); got.R != 255 || got.B != 0 {
		t.Errorf("left column = %v, want pure red", got)
	}
	if got := out.RGBAAt(1, 0); got.B != 255 || got.R != 0 {
		t.Errorf("right column = %v, want pure blue", got)
	}
}
