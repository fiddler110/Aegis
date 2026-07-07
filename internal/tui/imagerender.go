package tui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// imageProtocol identifies whether attached images can be shown inline in
// the transcript (P16.9), detected once at startup from the environment.
//
// True kitty-graphics/iTerm2-inline-image protocol support (the roadmap's
// original stretch goal) was deliberately descoped: both protocols place
// content via raw APC/OSC escape sequences interpreted by the physical
// terminal at render time, but this TUI's screen is a cell grid diffed and
// redrawn every frame by bubbletea/ultraviolet — a model with no primitive
// for "this span is opaque, out-of-band terminal state" (contrast
// ultraviolet's Cell.Link, which does have first-class support for the far
// simpler OSC 8 hyperlink case). Embedding raw graphics escapes in transcript
// content would risk redraw-triggered retransmission, duplication, or
// corruption, and there is no kitty/iTerm2 terminal available in this
// environment to verify against. The half-block fallback below needs none of
// that — it is ordinary SGR-styled text — so it is the only tier
// implemented; richer protocols are a candidate follow-up once they can be
// verified against real terminals.
type imageProtocol int

const (
	protocolNone imageProtocol = iota
	protocolHalfBlock
)

// detectImageProtocol inspects the environment (as returned by os.Environ)
// to decide whether the terminal can plausibly render truecolor/256-color
// output. It deliberately reuses colorprofile's env/NO_COLOR/CLICOLOR
// handling rather than re-implementing it.
func detectImageProtocol(environ []string) imageProtocol {
	if colorprofile.Env(environ) < colorprofile.ANSI256 {
		return protocolNone
	}
	return protocolHalfBlock
}

// imageProtoFor resolves the tui.Config.ImageRendering setting ("auto" or
// "off") to a detected protocol, called once at startup.
func imageProtoFor(setting string) imageProtocol {
	if setting == "off" {
		return protocolNone
	}
	return detectImageProtocol(os.Environ())
}

// Thumbnail box bounds, in terminal cells. cellAspect approximates a
// monospace cell's height-to-width pixel ratio, used to convert an image's
// pixel aspect ratio into a cols/rows box that looks proportionate.
const (
	thumbnailMaxCols = 32
	thumbnailMaxRows = 16
	cellAspect       = 2.0
)

// thumbnailBox fits a w×h image into the fixed thumbnail bounds, preserving
// aspect ratio.
func thumbnailBox(w, h int) (cols, rows int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	cols = thumbnailMaxCols
	rows = int(float64(cols) * float64(h) / float64(w) / cellAspect)
	if rows < 1 {
		rows = 1
	}
	if rows > thumbnailMaxRows {
		rows = thumbnailMaxRows
		cols = int(float64(rows) * cellAspect * float64(w) / float64(h))
		if cols < 1 {
			cols = 1
		}
		if cols > thumbnailMaxCols {
			cols = thumbnailMaxCols
		}
	}
	return cols, rows
}

// renderImageThumbnail renders a best-effort inline thumbnail for raw image
// bytes. It returns "" (never an error) when decoding fails — including for
// formats the stdlib image package doesn't support, notably WebP — or when
// proto is protocolNone; callers fall back to the existing text-only
// attachment notice rather than surfacing a rendering failure.
func renderImageThumbnail(data []byte, proto imageProtocol) string {
	if proto == protocolNone {
		return ""
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	b := img.Bounds()
	cols, rows := thumbnailBox(b.Dx(), b.Dy())
	if cols < 1 || rows < 1 {
		return ""
	}
	switch proto {
	case protocolHalfBlock:
		return renderHalfBlocks(img, cols, rows)
	default:
		return ""
	}
}

// renderHalfBlocks downsamples img to cols×(rows*2) pixels and renders it as
// cols×rows terminal cells using the upper-half-block trick: each cell's
// foreground color is its top source pixel, its background color the pixel
// below, doubling vertical resolution relative to one color per cell.
func renderHalfBlocks(img image.Image, cols, rows int) string {
	px := resizeBoxAvg(img, cols, rows*2)
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			top := px.RGBAAt(x, y*2)
			bot := px.RGBAAt(x, y*2+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		sb.WriteString("\x1b[0m\n")
	}
	return sb.String()
}

// resizeBoxAvg downsamples img to outW×outH by averaging every source pixel
// that maps into each destination cell — cheap, dependency-free, and far
// less noisy than nearest-neighbor when shrinking a photo to a handful of
// terminal cells.
func resizeBoxAvg(img image.Image, outW, outH int) *image.RGBA {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	if srcW <= 0 || srcH <= 0 || outW <= 0 || outH <= 0 {
		return dst
	}
	for y := 0; y < outH; y++ {
		sy0 := b.Min.Y + y*srcH/outH
		sy1 := b.Min.Y + (y+1)*srcH/outH
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < outW; x++ {
			sx0 := b.Min.X + x*srcW/outW
			sx1 := b.Min.X + (x+1)*srcW/outW
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var rSum, gSum, bSum, n uint32
			for yy := sy0; yy < sy1 && yy < b.Max.Y; yy++ {
				for xx := sx0; xx < sx1 && xx < b.Max.X; xx++ {
					r, g, bl, _ := img.At(xx, yy).RGBA()
					rSum += r >> 8
					gSum += g >> 8
					bSum += bl >> 8
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(rSum / n), G: uint8(gSum / n), B: uint8(bSum / n), A: 255})
		}
	}
	return dst
}
