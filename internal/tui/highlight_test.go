package tui

import (
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHighlightSource_MatchedLexer(t *testing.T) {
	th := newTheme()
	src := "package main\n\nfunc main() {}\n"
	lines, ok := highlightSource(th, "main.go", src, nil)
	if !ok {
		t.Fatal("expected a lexer match for main.go")
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %#v", len(lines), lines)
	}
	if plain := ansi.Strip(lines[0]); plain != "package main" {
		t.Errorf("line 0 plain text = %q, want %q", plain, "package main")
	}
	// The rendered line must carry styling (some ANSI escape), otherwise
	// highlighting silently did nothing.
	if lines[2] == ansi.Strip(lines[2]) {
		t.Error("expected highlighted line to carry ANSI styling")
	}
}

func TestHighlightSource_NoLexerMatch(t *testing.T) {
	th := newTheme()
	if _, ok := highlightSource(th, "weird.zzznotalang", "abc\n", nil); ok {
		t.Error("expected no lexer match for an unknown extension")
	}
}

func TestHighlightSource_EmptySource(t *testing.T) {
	th := newTheme()
	if _, ok := highlightSource(th, "main.go", "", nil); ok {
		t.Error("expected empty source to report ok=false")
	}
}

func TestHighlightSource_BgForLine(t *testing.T) {
	th := newTheme()
	src := "func f() {}\nfunc g() {}\n"
	var seen []int
	lines, ok := highlightSource(th, "x.go", src, func(l int) color.Color {
		seen = append(seen, l)
		if l == 1 {
			return colDiffAddBg
		}
		return nil
	})
	if !ok {
		t.Fatal("expected a lexer match for x.go")
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if len(seen) == 0 {
		t.Error("expected bgForLine to be consulted while rendering tokens")
	}
}

func TestHighlightUnifiedDiff(t *testing.T) {
	th := newTheme()
	src := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-old line\n+new line\n"
	lines, ok := highlightUnifiedDiff(th, src)
	if !ok {
		t.Fatal("expected the built-in diff lexer to match")
	}
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d: %#v", len(lines), lines)
	}
	if plain := ansi.Strip(lines[4]); plain != "-old line" {
		t.Errorf("line 4 plain text = %q, want %q", plain, "-old line")
	}
	if lines[4] == ansi.Strip(lines[4]) {
		t.Error("expected the deleted line to carry ANSI styling")
	}
	if lines[5] == ansi.Strip(lines[5]) {
		t.Error("expected the added line to carry ANSI styling")
	}
}

func TestHighlightUnifiedDiff_Empty(t *testing.T) {
	th := newTheme()
	if _, ok := highlightUnifiedDiff(th, ""); ok {
		t.Error("expected empty source to report ok=false")
	}
}

func TestHexColor(t *testing.T) {
	if got := hexColor(nil); got != "" {
		t.Errorf("hexColor(nil) = %q, want empty", got)
	}
}
