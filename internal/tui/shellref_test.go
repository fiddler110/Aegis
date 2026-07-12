package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestExtractShellRefsNoRunYetSubstitutesPlaceholder(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	got := extractShellRefs("what happened here? @shell", tp)
	want := "what happened here? (no terminal output yet)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractShellRefsDefaultLineCount(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	tp.beginRun("go test ./...")
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, "line"+strconv.Itoa(i))
	}
	tp.handleOutput(strings.Join(lines, "\n") + "\n")
	tp.handleDone(nil)

	got := extractShellRefs("explain @shell please", tp)

	if !strings.Contains(got, "go test ./...") {
		t.Errorf("expected command in output, got %q", got)
	}
	if !strings.Contains(got, "succeeded") {
		t.Errorf("expected success framing, got %q", got)
	}
	if strings.Contains(got, "line1\n") {
		t.Errorf("expected only the last 50 lines, but line1 is still present: %q", got)
	}
	if !strings.Contains(got, "line60") || !strings.Contains(got, "line11") {
		t.Errorf("expected last 50 lines (11..60) present, got %q", got)
	}
}

func TestExtractShellRefsExplicitLineCount(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	tp.beginRun("ls")
	tp.handleOutput("a\nb\nc\nd\n")
	tp.handleDone(nil)

	got := extractShellRefs("@shell:2", tp)
	if !strings.Contains(got, "Output (last 2 lines)") {
		t.Errorf("expected explicit line count reflected, got %q", got)
	}
	if strings.Contains(got, "\na\n") || strings.Contains(got, "```\na") {
		t.Errorf("expected line 'a' trimmed away, got %q", got)
	}
	if !strings.Contains(got, "c\nd") {
		t.Errorf("expected last 2 lines 'c' and 'd', got %q", got)
	}
}

func TestExtractShellRefsFailedCommand(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	tp.beginRun("go build ./bad")
	tp.handleOutput("undefined: foo\n")
	tp.handleDone(errWrap(errors.New("exit status 2")))

	got := extractShellRefs("@shell", tp)
	if !strings.Contains(got, "failed with exit code") {
		t.Errorf("expected failure framing, got %q", got)
	}
	if !strings.Contains(got, "undefined: foo") {
		t.Errorf("expected output included, got %q", got)
	}
}

func TestExtractShellRefsMultipleOccurrences(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	tp.beginRun("echo hi")
	tp.handleOutput("hi\n")
	tp.handleDone(nil)

	got := extractShellRefs("first @shell then again @shell:1", tp)
	if strings.Count(got, "echo hi") != 2 {
		t.Fatalf("expected the command spliced in twice, got %q", got)
	}
}

func TestExtractShellRefsNoTokenLeavesTextUntouched(t *testing.T) {
	tp := newTermPane(t.TempDir(), 10)
	text := "no ref here, just @shellac (a word, not a token)"
	if got := extractShellRefs(text, tp); got != text {
		t.Fatalf("expected pass-through, got %q", got)
	}
}
