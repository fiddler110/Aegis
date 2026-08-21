package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sgrBeforeWord matches an SGR CSI sequence immediately preceding "red",
// used to confirm colour survives sanitization+remapping without pinning
// down remapANSI16's specific truecolor rewrite of the original code.
var sgrBeforeWord = regexp.MustCompile("\x1b\\[[0-9;]*mred")

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

// TestRenderToolResult_SanitizesDangerousSeqs covers P28.1: untrusted tool
// output reaching renderToolResult must have OSC/cursor/alt-screen sequences
// stripped, across every branch (single-line, multi-line generic block, and
// read_file), while legitimate SGR colour still passes through.
func TestRenderToolResult_SanitizesDangerousSeqs(t *testing.T) {
	th := newTheme()

	t.Run("single-line shell output with OSC 52 clipboard hijack", func(t *testing.T) {
		result := "safe output\x1b]52;c;ZXZpbA==\x07"
		out := renderToolResult(th, "shell", result, false, 80, 16, "")
		if strings.Contains(out, "\x1b]52") {
			t.Errorf("expected OSC 52 sequence stripped, got: %q", out)
		}
	})

	t.Run("multi-line shell output with cursor manipulation and alt screen", func(t *testing.T) {
		result := "line one\x1b[2J\x1b[?1049hline two\nline three\x1b]0;spoofed title\x07"
		out := renderToolResult(th, "shell", result, false, 80, 16, "")
		for _, bad := range []string{"\x1b[2J", "\x1b[?1049h", "\x1b]0;"} {
			if strings.Contains(out, bad) {
				t.Errorf("expected dangerous sequence %q stripped, got: %q", bad, out)
			}
		}
		if !strings.Contains(ansi.Strip(out), "line two") || !strings.Contains(ansi.Strip(out), "line three") {
			t.Errorf("expected surrounding text preserved, got:\n%s", ansi.Strip(out))
		}
	})

	t.Run("multi-line shell output preserves SGR color", func(t *testing.T) {
		result := "\x1b[31mred\x1b[0m line\nsecond line"
		out := renderToolResult(th, "shell", result, false, 80, 16, "")
		// remapANSI16 legitimately rewrites 31 (basic ANSI red) to an explicit
		// truecolor SGR — the point here is that stripDangerousSeqs doesn't
		// remove the *colour* CSI outright, not that the raw code 31 survives
		// verbatim, so check for an SGR sequence directly preceding "red".
		if !sgrBeforeWord.MatchString(out) {
			t.Errorf("expected an SGR colour sequence still applied to \"red\", got: %q", out)
		}
	})

	t.Run("read_file content with hyperlink spoofing sequence", func(t *testing.T) {
		result := "1\tpackage main\n2\t\x1b]8;;http://evil.example\x1b\\func main() {}\x1b]8;;\x1b\\\n"
		out := renderToolResult(th, "read_file", result, false, 80, 16, "main.go")
		if strings.Contains(out, "evil.example") {
			t.Errorf("expected OSC 8 hyperlink target stripped, got: %q", out)
		}
		if !strings.Contains(ansi.Strip(out), "func main") {
			t.Errorf("expected file content preserved, got:\n%s", ansi.Strip(out))
		}
	})
}

// TestRenderToolResult_HeaderDropsRepeatedName covers P74.3: the result line
// must not repeat the tool name (the call block already carries it) and must
// hang off a "⎿" continuation gutter instead, for both the single-line and
// multi-line branches.
func TestRenderToolResult_HeaderDropsRepeatedName(t *testing.T) {
	th := newTheme()

	t.Run("single-line", func(t *testing.T) {
		out := renderToolResult(th, "read_file", "3 files found", false, 80, 16, "")
		plain := ansi.Strip(out)
		if strings.Contains(plain, "read_file") {
			t.Errorf("expected no repeated tool name, got: %q", plain)
		}
		if !strings.Contains(plain, "⎿") {
			t.Errorf("expected a ⎿ continuation gutter, got: %q", plain)
		}
	})

	t.Run("multi-line within cap", func(t *testing.T) {
		out := renderToolResult(th, "shell", "line one\nline two\nline three", false, 80, 16, "")
		plain := ansi.Strip(out)
		if strings.Contains(plain, "shell") {
			t.Errorf("expected no repeated tool name, got: %q", plain)
		}
		if !strings.Contains(plain, "⎿") || !strings.Contains(plain, "line one") {
			t.Errorf("expected a gutter header followed by the body, got:\n%s", plain)
		}
	})
}

// TestRenderToolResult_CollapsesToSummaryWhenOverCap covers P74.3: output that
// would need truncating collapses to a single one-line summary rather than a
// chopped body, and the existing /tools full expand path (a raised
// maxBodyLines) shows the full body again.
func TestRenderToolResult_CollapsesToSummaryWhenOverCap(t *testing.T) {
	th := newTheme()
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	result := strings.Join(lines, "\n")

	compact := renderToolResult(th, "shell", result, false, 80, 10, "")
	plainCompact := ansi.Strip(compact)
	if strings.Contains(plainCompact, "line 1\n") || strings.Count(plainCompact, "\n") > 0 {
		t.Errorf("expected a single collapsed summary line, got:\n%s", plainCompact)
	}
	if !strings.Contains(plainCompact, "20 lines") || !strings.Contains(plainCompact, "/tools full") {
		t.Errorf("expected a line-count summary naming the expand path, got: %q", plainCompact)
	}

	full := renderToolResult(th, "shell", result, false, 80, 9999, "")
	plainFull := ansi.Strip(full)
	if !strings.Contains(plainFull, "line 1") || !strings.Contains(plainFull, "line 20") {
		t.Errorf("expected /tools full to show the complete body, got:\n%s", plainFull)
	}
}

// TestRenderShellCall_SanitizesDangerousSeqs covers P28.1 for the tool-call
// preview path (renderShellCall), which highlights the model-supplied
// command via chroma and bypasses renderToolResult entirely.
func TestRenderShellCall_SanitizesDangerousSeqs(t *testing.T) {
	th := newTheme()
	command := "echo hi" + string(rune(0x1b)) + "]52;c;ZXZpbA==" + string(rune(0x07))
	input, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	out, ok := renderShellCall(th, "shell", input, 80)
	if !ok {
		t.Fatal("expected renderShellCall to succeed")
	}
	if strings.Contains(out, "\x1b]52") {
		t.Errorf("expected OSC 52 sequence stripped from rendered command, got: %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "echo hi") {
		t.Errorf("expected command text preserved, got:\n%s", ansi.Strip(out))
	}
}
