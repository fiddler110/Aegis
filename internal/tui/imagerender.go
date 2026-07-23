package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// imageProtocol identifies how attached images are shown inline in the
// transcript (P16.9), detected once at startup from the environment.
//
// The half-block tier is ordinary SGR-styled text: it flows through the
// cell-grid renderer like any other content, needs no out-of-band terminal
// state, and is the default/auto pick everywhere truecolor is available.
//
// protocolKitty (P40.4) is the real kitty-graphics APC protocol. It remains
// EXPERIMENTAL and strictly opt-in (image_rendering: "kitty") — never chosen by
// auto-detection. The reason is the one the original descope recorded: this
// screen is a cell grid diffed and redrawn every frame by bubbletea/
// ultraviolet, which has no primitive for "this span is opaque, out-of-band
// terminal state" (contrast ultraviolet's Cell.Link for the far simpler OSC 8
// hyperlink case), and there is no kitty terminal in this environment to verify
// against. So the encoder and detector below are built and unit-tested as the
// prototype the roadmap called for, but a user has to knowingly turn the tier
// on; the safe half-block default is untouched. Placement-in-the-render-loop is
// the remaining step that needs a real terminal to validate.
type imageProtocol int

const (
	protocolNone imageProtocol = iota
	protocolHalfBlock
	protocolKitty
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

// imageProtoFor resolves the tui.Config.ImageRendering setting to a protocol,
// called once at startup:
//   - "off"    → no inline images
//   - "kitty"  → the experimental kitty-graphics tier (P40.4), an explicit
//     opt-in that is deliberately never reached by auto-detection
//   - "auto"/"" and anything else → the half-block tier when the terminal is
//     truecolor/256-color capable, else none
func imageProtoFor(setting string) imageProtocol {
	switch setting {
	case "off":
		return protocolNone
	case "kitty":
		return protocolKitty
	default:
		return detectImageProtocol(os.Environ())
	}
}

// detectKittyGraphics reports whether the environment looks like a terminal
// that speaks the kitty graphics protocol (kitty itself, Ghostty, WezTerm,
// Konsole). It is intentionally NOT consulted by imageProtoFor's "auto" path —
// the kitty tier is opt-in only (see imageProtocol's doc) — but it lets
// `aegis doctor` and the config UI tell the user whether "kitty" is plausible
// on their terminal before they enable it.
func detectKittyGraphics(environ []string) bool {
	env := map[string]string{}
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	if env["KITTY_WINDOW_ID"] != "" {
		return true
	}
	if strings.Contains(env["TERM"], "kitty") {
		return true
	}
	switch strings.ToLower(env["TERM_PROGRAM"]) {
	case "ghostty", "wezterm":
		return true
	}
	// Konsole advertises graphics support via this variable since 22.04.
	return env["KONSOLE_VERSION"] != ""
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
	case protocolKitty:
		return renderKitty(img, cols, rows)
	default:
		return ""
	}
}

// kittyChunk is the maximum base64 payload per kitty APC escape; the protocol
// requires transmissions be split into chunks no larger than 4096 bytes.
const kittyChunk = 4096

// kittyGraphicsSequence encodes pngData as a kitty graphics protocol
// transmit-and-display (a=T) escape, scaled by the terminal into a cols×rows
// cell box (c=,r=). The payload is base64-encoded and split into ≤4096-byte
// chunks joined by the protocol's m=1/m=0 continuation flag, each wrapped in an
// APC `_G … ST` envelope. Format per
// https://sw.kovidgoyal.net/kitty/graphics-protocol/. Pure and deterministic so
// it can be unit-tested without a terminal (see imagerender_test.go).
func kittyGraphicsSequence(pngData []byte, cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(pngData)
	var sb strings.Builder
	for first := true; len(payload) > 0; first = false {
		chunk := payload
		if len(chunk) > kittyChunk {
			chunk = chunk[:kittyChunk]
		}
		payload = payload[len(chunk):]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		sb.WriteString("\x1b_G")
		if first {
			// f=100: PNG data; a=T: transmit and display; c/r: target cell box.
			fmt.Fprintf(&sb, "f=100,a=T,c=%d,r=%d,m=%d", cols, rows, more)
		} else {
			fmt.Fprintf(&sb, "m=%d", more)
		}
		sb.WriteByte(';')
		sb.WriteString(chunk)
		sb.WriteString("\x1b\\")
	}
	return sb.String()
}

// renderKitty re-encodes img to PNG and returns the kitty graphics escape,
// followed by rows newlines so the cell-grid layout reserves vertical space for
// the image the terminal paints out-of-band. Returns "" on an encode failure,
// matching renderImageThumbnail's never-error contract. EXPERIMENTAL — see
// imageProtocol's doc: this tier is opt-in and its render-loop placement is
// unverified against a real terminal.
func renderKitty(img image.Image, cols, rows int) string {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return kittyGraphicsSequence(buf.Bytes(), cols, rows) + strings.Repeat("\n", rows)
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
