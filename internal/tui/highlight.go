package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// buildChromaStyle constructs a chroma.Style from the active colour scheme's
// package-level roles (the same TQ10 palette that backs glamour and the
// ANSI-16 remap) so chroma-highlighted code reads as part of one coherent
// theme instead of an unrelated built-in highlighter style. Rebuilt by
// newTheme whenever the theme changes.
func buildChromaStyle() *chroma.Style {
	entries := chroma.StyleEntries{
		chroma.Background:      hexColor(colFgBase),
		chroma.Text:            hexColor(colFgBase),
		chroma.Keyword:         hexColor(colKeyword) + " bold",
		chroma.NameFunction:    hexColor(colInfo),
		chroma.NameClass:       hexColor(colSecondary) + " bold",
		chroma.NameBuiltin:     hexColor(colAccentAlt),
		chroma.NameTag:         hexColor(colKeyword),
		chroma.NameAttribute:   hexColor(colInfo),
		chroma.NameDecorator:   hexColor(colAccentAlt),
		chroma.LiteralString:   hexColor(colSuccessRole),
		chroma.LiteralNumber:   hexColor(colWarn),
		chroma.Comment:         hexColor(colFgMost) + " italic",
		chroma.Operator:        hexColor(colFgSub),
		chroma.Punctuation:     hexColor(colFgSub),
		chroma.GenericDeleted:  hexColor(colDestructive),
		chroma.GenericInserted: hexColor(colSuccessRole),
		chroma.Error:           hexColor(colError),
	}
	st, err := chroma.NewStyle("aegis", entries)
	if err != nil {
		return nil
	}
	return st
}

// hexColor renders c as a "#rrggbb" pygments-style colour descriptor.
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// lipglossFromEntry converts a chroma style entry into the equivalent
// lipgloss style (foreground + bold/italic only — chroma entries this
// package builds never set background or underline).
func lipglossFromEntry(e chroma.StyleEntry) lipgloss.Style {
	st := lipgloss.NewStyle()
	if e.Colour.IsSet() {
		st = st.Foreground(lipgloss.Color(e.Colour.String()))
	}
	if e.Bold == chroma.Yes {
		st = st.Bold(true)
	}
	if e.Italic == chroma.Yes {
		st = st.Italic(true)
	}
	return st
}

// highlightSource tokenizes source with the lexer chroma matches to path (by
// extension/filename) and renders each token through th.chroma, returning
// one already-ANSI-styled string per line. ok is false when no lexer matched,
// the theme has no chroma style, or tokenizing failed — callers fall back to
// their existing plain-text rendering in that case.
//
// bgForLine, if non-nil, supplies a per-line background tint (0-indexed) —
// used by the diff view (P16.3) to lay chroma's token colours "under" a
// tinted add/removed row without a second, background-blind render pass.
// Tokenizing the whole source in one lexer pass (rather than per displayed
// line) keeps multi-line constructs — block comments, triple-quoted strings —
// lexed with correct context.
func highlightSource(th theme, path, source string, bgForLine func(line int) color.Color) (lines []string, ok bool) {
	if th.chroma == nil || source == "" {
		return nil, false
	}
	lexer := lexers.Match(path)
	if lexer == nil {
		return nil, false
	}
	lexer = chroma.Coalesce(lexer)
	iter, err := lexer.Tokenise(nil, source)
	if err != nil {
		return nil, false
	}

	lineIdx := 0
	var cur strings.Builder
	for _, tok := range iter.Tokens() {
		entry := th.chroma.Get(tok.Type)
		segs := strings.Split(tok.Value, "\n")
		for i, seg := range segs {
			if seg != "" {
				st := lipglossFromEntry(entry)
				if bgForLine != nil {
					if bg := bgForLine(lineIdx); bg != nil {
						st = st.Background(bg)
					}
				}
				cur.WriteString(st.Render(seg))
			}
			if i < len(segs)-1 {
				lines = append(lines, cur.String())
				cur.Reset()
				lineIdx++
			}
		}
	}
	if cur.Len() > 0 || len(lines) == 0 {
		lines = append(lines, cur.String())
	}
	return lines, true
}
