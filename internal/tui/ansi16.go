package tui

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"
)

// sgrSeq matches a CSI SGR ("select graphic rendition") sequence — ESC [ … m —
// with semicolon-separated numeric parameters. Sequences using colon
// sub-parameters (rare in shell output) won't match and pass through untouched.
var sgrSeq = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// remapANSI16 rewrites basic ANSI-16 colour SGR codes (30–37/40–47 and the
// bright 90–97/100–107 ranges) to explicit 24-bit truecolor drawn from palette.
// Programs emit e.g. \x1b[31m and trust the terminal to choose the red; on a
// dark TUI background those defaults are often illegible, so we pin them to
// legible, on-brand colours instead. Extended-colour introducers (38/48/58 with
// 5;n or 2;r;g;b args) and all non-colour attributes pass through unchanged.
//
// This is Aegis's port of Crush's common.RemapANSI16; it uses a regex rather
// than the x/ansi low-level parser so it is independent of that package's
// version, which is sufficient for the standard sequences shell tools emit.
func remapANSI16(s string, palette [16]color.Color) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	return sgrSeq.ReplaceAllStringFunc(s, func(seq string) string {
		inner := seq[2 : len(seq)-1] // strip leading ESC[ and trailing m
		if inner == "" {
			return seq // bare reset (ESC[m) — leave as-is
		}
		parts := strings.Split(inner, ";")
		out := make([]string, 0, len(parts))
		for i := 0; i < len(parts); i++ {
			p, err := strconv.Atoi(parts[i])
			if err != nil {
				out = append(out, parts[i])
				continue
			}
			switch {
			// Extended-colour introducers consume their following args; copy
			// them through verbatim so they aren't misread as base colours.
			case p == 38 || p == 48 || p == 58:
				out = append(out, parts[i])
				if i+1 >= len(parts) {
					break
				}
				sub, _ := strconv.Atoi(parts[i+1])
				switch sub {
				case 5: // 256-colour: <intro>;5;n
					out = append(out, parts[i+1])
					if i+2 < len(parts) {
						out = append(out, parts[i+2])
						i += 2
					} else {
						i++
					}
				case 2: // truecolor: <intro>;2;r;g;b
					out = append(out, parts[i+1])
					for j := 2; j <= 4 && i+j < len(parts); j++ {
						out = append(out, parts[i+j])
					}
					i += min(4, len(parts)-i-1)
				default:
					i++
				}
			case p >= 30 && p <= 37:
				out = append(out, truecolorSGR(38, palette[p-30]))
			case p >= 90 && p <= 97:
				out = append(out, truecolorSGR(38, palette[8+p-90]))
			case p >= 40 && p <= 47:
				out = append(out, truecolorSGR(48, palette[p-40]))
			case p >= 100 && p <= 107:
				out = append(out, truecolorSGR(48, palette[8+p-100]))
			default:
				out = append(out, parts[i])
			}
		}
		return "\x1b[" + strings.Join(out, ";") + "m"
	})
}

// truecolorSGR renders an SGR parameter group "<introducer>;2;r;g;b" for a
// colour (38 = foreground, 48 = background). A nil colour yields the bare
// introducer so the terminal default applies.
func truecolorSGR(introducer int, c color.Color) string {
	if c == nil {
		return strconv.Itoa(introducer)
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;2;%d;%d;%d", introducer, r>>8, g>>8, b>>8)
}
