package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDiffLines_HunkHeaderPrecedesContent(t *testing.T) {
	th := newTheme()
	old := "func add(a, b int) int {\n\treturn a + b\n}\n\nfunc unused() {}\n"
	nw := "func add(a, b int) int {\n\t// sum the two operands\n\treturn a - b\n}\n\nfunc unused() {}\n"
	lines, hidden := diffLines(th, "math.go", old, nw, 80, 24)
	if hidden != 0 {
		t.Fatalf("unexpected hidden count: %d", hidden)
	}
	if len(lines) == 0 {
		t.Fatal("expected diff lines")
	}
	plain0 := ansi.Strip(lines[0])
	if !strings.HasPrefix(plain0, "@@ -") {
		t.Fatalf("expected first line to be a hunk header, got %q", plain0)
	}
	// Real ranges, not the old placeholder "@@ ... @@".
	if !strings.Contains(plain0, ",") {
		t.Errorf("expected hunk header to carry real ranges, got %q", plain0)
	}
	// A gutter with both old and new line numbers must appear on a context line.
	found := false
	for _, l := range lines {
		p := ansi.Strip(l)
		if strings.Contains(p, "1 1") && strings.Contains(p, "func add") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a context line with a two-column line-number gutter, got:\n%s", strings.Join(lines, "\n"))
	}
}

func TestDiffLines_SingletonReplacePairGetsIntralineEmphasis(t *testing.T) {
	th := newTheme()
	old := "func add(a, b int) int {\n\treturn a + b\n}\n"
	nw := "func add(a, b int) int {\n\treturn a - b\n}\n"
	lines, _ := diffLines(th, "math.go", old, nw, 80, 24)
	var delLine, addLine string
	for _, l := range lines {
		p := ansi.Strip(l)
		if strings.Contains(p, "return a + b") {
			delLine = l
		}
		if strings.Contains(p, "return a - b") {
			addLine = l
		}
	}
	if delLine == "" || addLine == "" {
		t.Fatalf("expected both replaced lines present, got:\n%s", strings.Join(lines, "\n"))
	}
	// Underline (SGR 4) marks the emphasized changed span (the "+"/"-" token).
	if !strings.Contains(delLine, "\x1b[") || !strings.Contains(delLine, ";4") && !strings.Contains(delLine, "4;") {
		t.Errorf("expected the removed line to carry an underline-styled changed span, got raw: %q", delLine)
	}
}

func TestDiffLines_NoChangeReturnsEmpty(t *testing.T) {
	th := newTheme()
	lines, hidden := diffLines(th, "math.go", "same\n", "same\n", 80, 24)
	if lines != nil || hidden != 0 {
		t.Errorf("expected no diff for identical content, got lines=%v hidden=%d", lines, hidden)
	}
}

func TestDiffLines_UnknownExtensionFallsBackToPlainColors(t *testing.T) {
	th := newTheme()
	lines, _ := diffLines(th, "data.unknownext12345", "a\n", "b\n", 80, 24)
	if len(lines) == 0 {
		t.Fatal("expected diff lines even without a chroma lexer match")
	}
	// Should still contain the +/- markers and gutter, just without token
	// colour variety — a smoke check that the fallback path renders at all.
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "- a") || !strings.Contains(joined, "+ b") {
		t.Errorf("expected fallback +/- rendering, got:\n%s", joined)
	}
}

func TestDiffLines_WriteFileWholeFileAddition(t *testing.T) {
	th := newTheme()
	lines, hidden := diffLines(th, "new.go", "", "package main\n", 80, maxDiffLines)
	if hidden != 0 {
		t.Fatalf("unexpected hidden: %d", hidden)
	}
	if len(lines) == 0 {
		t.Fatal("expected diff lines for a new file")
	}
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "package main") {
		t.Errorf("expected new file content in diff, got:\n%s", joined)
	}
}

func TestIntralineDiff_EmphasizesChangedWordOnly(t *testing.T) {
	th := newTheme()
	oldR, newR := intralineDiff(th, "return a + b", "return a - b")
	if ansi.Strip(oldR) != "return a + b" || ansi.Strip(newR) != "return a - b" {
		t.Fatalf("intraline diff changed the visible text: old=%q new=%q", ansi.Strip(oldR), ansi.Strip(newR))
	}
	if oldR == ansi.Strip(oldR) || newR == ansi.Strip(newR) {
		t.Error("expected intraline diff output to carry ANSI styling")
	}
}

func TestRenderReadFileResult_ParsesLineNumberPrefix(t *testing.T) {
	th := newTheme()
	result := "1\tpackage main\n2\t\n3\tfunc main() {}\n"
	body, ok := renderReadFileResult(th, "main.go", result, 16, 80)
	if !ok {
		t.Fatal("expected renderReadFileResult to succeed on well-formed input")
	}
	plain := ansi.Strip(body)
	for _, want := range []string{"1", "package main", "3", "func main"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected rendered block to contain %q, got:\n%s", want, plain)
		}
	}
}

func TestRenderReadFileResult_FallsBackOnUnexpectedFormat(t *testing.T) {
	th := newTheme()
	if _, ok := renderReadFileResult(th, "main.go", "no tab prefix here\nanother line\n", 16, 80); ok {
		t.Error("expected renderReadFileResult to report ok=false for non-prefixed content")
	}
}

func TestRenderToolResult_ReadFileUsesPathForHighlighting(t *testing.T) {
	th := newTheme()
	result := "1\tpackage main\n2\tfunc main() {}\n"
	out := renderToolResult(th, "read_file", result, false, 80, 16, "main.go")
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "package main") {
		t.Errorf("expected file content in rendered result, got:\n%s", plain)
	}
	if out == plain {
		t.Error("expected styled (ANSI) output")
	}
}

func TestRenderToolResult_ReadFileWithoutPathFallsBackToGenericBlock(t *testing.T) {
	th := newTheme()
	result := "1\tpackage main\n2\tfunc main() {}\n"
	out := renderToolResult(th, "read_file", result, false, 80, 16, "")
	if !strings.Contains(ansi.Strip(out), "package main") {
		t.Errorf("expected generic block fallback to still show content, got:\n%s", out)
	}
}
