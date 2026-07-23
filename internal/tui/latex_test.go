package tui

import "testing"

func TestRenderMathUnicode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no dollar", "plain text", "plain text"},
		{"inline superscript", `Einstein: $E=mc^2$.`, "Einstein: E=mc²."},
		{"braced superscript", `$x^{10}$`, "x¹⁰"},
		{"subscript", `$a_1 + a_2$`, "a₁ + a₂"},
		{"greek and relation", `$$\alpha \geq \beta$$`, "α ≥ β"},
		{"times", `$3 \times 4$`, "3 × 4"},
		{"frac multi-char", `$\frac{a+b}{c}$`, "(a+b)/c"},
		{"frac single tokens", `$\frac{1}{2}$`, "1/2"},
		{"arrow", `$A \rightarrow B$`, "A → B"},
		{"sum", `$\sum_{i=0}^{n} i$`, "∑ᵢ₌₀ⁿ i"},
		{"nonrepresentable exponent kept literal", `$x^{a-}$`, "xᵃ⁻"},
		// Currency must not be treated as math (no math markers in span).
		{"currency untouched", `It costs $5 and $10 total.`, `It costs $5 and $10 total.`},
		{"single trailing dollar", `Price is $5.`, `Price is $5.`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderMathUnicode(c.in); got != c.want {
				t.Errorf("renderMathUnicode(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRenderMathUnicodePreservesCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"inline code shell var", "run `echo $HOME` now"},
		{"fenced block", "```sh\nexport X=$PATH\necho $VAR\n```"},
		{"inline code with caret", "the `x^2` operator literally"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderMathUnicode(c.in); got != c.in {
				t.Errorf("renderMathUnicode(%q) mutated code to %q", c.in, got)
			}
		})
	}
}

func TestRenderMathUnicodeUnbalanced(t *testing.T) {
	// A lone unmatched `$` must be emitted verbatim, not swallowed.
	in := `a $ b c`
	if got := renderMathUnicode(in); got != in {
		t.Errorf("renderMathUnicode(%q) = %q, want unchanged", in, got)
	}
}

func TestRenderMathUnicodeEscapedDollar(t *testing.T) {
	in := `cost \$5`
	want := `cost $5`
	if got := renderMathUnicode(in); got != want {
		t.Errorf("renderMathUnicode(%q) = %q, want %q", in, got, want)
	}
}
