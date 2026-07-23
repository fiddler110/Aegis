package tui

import (
	"strings"
)

// renderMathUnicode is a preprocessing pass (P40.8) that turns common LaTeX
// math markup into a Unicode approximation before glamour/goldmark — which has
// no math awareness — ever sees the text. It is deliberately NOT a TeX
// typesetter: it converts `$E=mc^2$` to `E=mc²`, `$$\alpha \geq \beta$$` to
// `α ≥ β`, and leaves anything it can't represent as legible fallback text.
//
// Two safety rules keep it from mangling non-math prose:
//   - Fenced code blocks (``` … ```) and inline code spans (`…`) are passed
//     through untouched, so a shell `$HOME` or a code sample is never rewritten.
//   - A single-`$…$` span is only treated as math when its content actually
//     looks like math (a backslash command, or a `^`/`_` script). This leaves
//     currency like "$5 and $10" alone — its span content has no math markers.
//
// `$$…$$` display spans are always converted (the double delimiter is an
// unambiguous math signal that has no currency false-positive).
func renderMathUnicode(s string) string {
	if !strings.ContainsRune(s, '$') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	n := len(s)
	for i < n {
		// Preserve fenced code blocks verbatim.
		if strings.HasPrefix(s[i:], "```") {
			end := strings.Index(s[i+3:], "```")
			if end < 0 {
				b.WriteString(s[i:])
				break
			}
			stop := i + 3 + end + 3
			b.WriteString(s[i:stop])
			i = stop
			continue
		}
		// Preserve inline code spans verbatim.
		if s[i] == '`' {
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				b.WriteString(s[i:])
				break
			}
			stop := i + 1 + end + 1
			b.WriteString(s[i:stop])
			i = stop
			continue
		}
		// Escaped dollar — emit the literal `$` and move on.
		if s[i] == '\\' && i+1 < n && s[i+1] == '$' {
			b.WriteString("$")
			i += 2
			continue
		}
		if s[i] == '$' {
			// Display math: $$ … $$
			if strings.HasPrefix(s[i:], "$$") {
				end := strings.Index(s[i+2:], "$$")
				if end >= 0 {
					inner := s[i+2 : i+2+end]
					if !strings.Contains(inner, "\n\n") {
						b.WriteString(convertMath(inner))
						i = i + 2 + end + 2
						continue
					}
				}
				// Unbalanced or paragraph-spanning — leave literal.
				b.WriteByte(s[i])
				i++
				continue
			}
			// Inline math: $ … $ (single line, math-looking content only).
			if end := strings.IndexByte(s[i+1:], '$'); end >= 0 {
				inner := s[i+1 : i+1+end]
				if inner != "" && !strings.ContainsRune(inner, '\n') && looksLikeMath(inner) {
					b.WriteString(convertMath(inner))
					i = i + 1 + end + 1
					continue
				}
			}
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// looksLikeMath reports whether an inline `$…$` span's content is math rather
// than incidental prose between two dollar signs (currency, etc.). A backslash
// command or a super/subscript marker is a strong signal.
func looksLikeMath(s string) bool {
	return strings.ContainsAny(s, `\^_`)
}

// convertMath transforms the inside of a math span to its Unicode
// approximation: \frac first, then symbol names, then super/subscripts.
func convertMath(s string) string {
	s = convertFrac(s)
	s = replaceMathSymbols(s)
	s = convertScripts(s, '^', superscripts)
	s = convertScripts(s, '_', subscripts)
	// A few residual TeX spacing/grouping tokens read better stripped.
	s = strings.ReplaceAll(s, `\,`, " ")
	s = strings.ReplaceAll(s, `\;`, " ")
	s = strings.ReplaceAll(s, `\ `, " ")
	s = strings.ReplaceAll(s, `\!`, "")
	s = strings.ReplaceAll(s, `\left`, "")
	s = strings.ReplaceAll(s, `\right`, "")
	return strings.TrimSpace(collapseSpaces(s))
}

// convertFrac rewrites \frac{a}{b} to (a)/(b), leaving a bare numerator/
// denominator unparenthesized when it is a single token.
func convertFrac(s string) string {
	for {
		idx := strings.Index(s, `\frac`)
		if idx < 0 {
			return s
		}
		rest := s[idx+len(`\frac`):]
		num, after, ok := readBraceGroup(rest)
		if !ok {
			return s // malformed — leave the rest untouched
		}
		den, after2, ok := readBraceGroup(after)
		if !ok {
			return s
		}
		s = s[:idx] + wrapFracPart(num) + "/" + wrapFracPart(den) + after2
	}
}

func wrapFracPart(p string) string {
	if len(p) <= 1 {
		return p
	}
	return "(" + p + ")"
}

// readBraceGroup reads a leading {…} (skipping leading spaces) and returns the
// contents, the remainder after the closing brace, and whether it succeeded.
func readBraceGroup(s string) (group, rest string, ok bool) {
	s = strings.TrimLeft(s, " ")
	if s == "" || s[0] != '{' {
		return "", s, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
	}
	return "", s, false
}

// convertScripts converts marker-prefixed groups (`^{...}` / `^x`, or the `_`
// equivalents) to Unicode super/subscripts. When any character in a group has
// no Unicode form the whole group is left as literal TeX so the reader still
// sees the exponent unambiguously.
func convertScripts(s string, marker byte, table map[rune]rune) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != marker {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Braced group: ^{...}
		if i+1 < len(s) && s[i+1] == '{' {
			grp, rest, ok := readBraceGroup(s[i+1:])
			if ok {
				if conv, ok := mapRunes(grp, table); ok {
					b.WriteString(conv)
					i = len(s) - len(rest)
					continue
				}
				// Not representable — keep literal ^{...}.
				b.WriteByte(marker)
				b.WriteString("{" + grp + "}")
				i = len(s) - len(rest)
				continue
			}
		}
		// Single following rune.
		if i+1 < len(s) {
			r := rune(s[i+1])
			// Only ASCII single-char scripts; anything else stays literal.
			if r < 128 {
				if conv, ok := table[r]; ok {
					b.WriteRune(conv)
					i += 2
					continue
				}
			}
		}
		b.WriteByte(marker)
		i++
	}
	return b.String()
}

// mapRunes maps every rune of s through table; ok is false if any rune has no
// mapping (caller then keeps the literal form).
func mapRunes(s string, table map[rune]rune) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		c, ok := table[r]
		if !ok {
			return "", false
		}
		b.WriteRune(c)
	}
	return b.String(), true
}

// replaceMathSymbols swaps known TeX command names for their Unicode glyph.
// Longer names are replaced before shorter prefixes (e.g. \leftarrow before
// \left) via the pre-sorted mathSymbolOrder.
func replaceMathSymbols(s string) string {
	for _, name := range mathSymbolOrder {
		if strings.Contains(s, name) {
			s = strings.ReplaceAll(s, name, mathSymbols[name])
		}
	}
	return s
}

func collapseSpaces(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵',
	'6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'n': 'ⁿ', 'i': 'ⁱ',
	'a': 'ᵃ', 'b': 'ᵇ', 'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ', 'f': 'ᶠ',
	'g': 'ᵍ', 'h': 'ʰ', 'j': 'ʲ', 'k': 'ᵏ', 'l': 'ˡ', 'm': 'ᵐ',
	'o': 'ᵒ', 'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ', 't': 'ᵗ', 'u': 'ᵘ',
	'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ', 'z': 'ᶻ',
}

var subscripts = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅',
	'6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ',
	'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ', 'p': 'ₚ', 'r': 'ᵣ',
	's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ', 'v': 'ᵥ', 'x': 'ₓ',
}

// mathSymbols maps TeX command names to their Unicode glyph.
var mathSymbols = map[string]string{
	// Lowercase Greek.
	`\alpha`: "α", `\beta`: "β", `\gamma`: "γ", `\delta`: "δ",
	`\epsilon`: "ε", `\varepsilon`: "ε", `\zeta`: "ζ", `\eta`: "η",
	`\theta`: "θ", `\vartheta`: "ϑ", `\iota`: "ι", `\kappa`: "κ",
	`\lambda`: "λ", `\mu`: "μ", `\nu`: "ν", `\xi`: "ξ", `\pi`: "π",
	`\varpi`: "ϖ", `\rho`: "ρ", `\varrho`: "ϱ", `\sigma`: "σ",
	`\varsigma`: "ς", `\tau`: "τ", `\upsilon`: "υ", `\phi`: "φ",
	`\varphi`: "φ", `\chi`: "χ", `\psi`: "ψ", `\omega`: "ω",
	// Uppercase Greek.
	`\Gamma`: "Γ", `\Delta`: "Δ", `\Theta`: "Θ", `\Lambda`: "Λ",
	`\Xi`: "Ξ", `\Pi`: "Π", `\Sigma`: "Σ", `\Upsilon`: "Υ",
	`\Phi`: "Φ", `\Psi`: "Ψ", `\Omega`: "Ω",
	// Operators & relations.
	`\times`: "×", `\div`: "÷", `\cdot`: "·", `\pm`: "±", `\mp`: "∓",
	`\leq`: "≤", `\le`: "≤", `\geq`: "≥", `\ge`: "≥", `\neq`: "≠",
	`\ne`: "≠", `\approx`: "≈", `\equiv`: "≡", `\sim`: "∼",
	`\propto`: "∝", `\ll`: "≪", `\gg`: "≫",
	`\infty`: "∞", `\partial`: "∂", `\nabla`: "∇", `\sqrt`: "√",
	`\sum`: "∑", `\prod`: "∏", `\int`: "∫", `\oint`: "∮",
	`\forall`: "∀", `\exists`: "∃", `\nexists`: "∄", `\emptyset`: "∅",
	`\in`: "∈", `\notin`: "∉", `\ni`: "∋", `\subset`: "⊂",
	`\subseteq`: "⊆", `\supset`: "⊃", `\supseteq`: "⊇",
	`\cup`: "∪", `\cap`: "∩", `\setminus`: "∖",
	`\wedge`: "∧", `\vee`: "∨", `\neg`: "¬", `\oplus`: "⊕",
	`\otimes`: "⊗", `\perp`: "⊥", `\parallel`: "∥", `\angle`: "∠",
	// Arrows.
	`\leftrightarrow`: "↔", `\Leftrightarrow`: "⇔",
	`\leftarrow`: "←", `\Leftarrow`: "⇐",
	`\rightarrow`: "→", `\Rightarrow`: "⇒",
	`\to`: "→", `\gets`: "←", `\mapsto`: "↦",
	`\uparrow`: "↑", `\downarrow`: "↓",
	// Dots & misc.
	`\ldots`: "…", `\cdots`: "⋯", `\dots`: "…",
	`\prime`: "′", `\degree`: "°", `\aleph`: "ℵ",
	`\Re`: "ℜ", `\Im`: "ℑ", `\hbar`: "ℏ", `\ell`: "ℓ",
}

// mathSymbolOrder lists mathSymbols keys longest-first so a longer command name
// (\leftrightarrow) is matched before a shorter prefix (\leftarrow, \le).
var mathSymbolOrder = buildMathSymbolOrder()

func buildMathSymbolOrder() []string {
	keys := make([]string, 0, len(mathSymbols))
	for k := range mathSymbols {
		keys = append(keys, k)
	}
	// Insertion sort by descending length (small, fixed-size slice).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && len(keys[j]) > len(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
