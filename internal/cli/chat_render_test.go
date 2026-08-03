package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/termsafe"
)

func TestParseRenderMode(t *testing.T) {
	cases := map[string]renderMode{
		"":      renderAuto,
		"auto":  renderAuto,
		"on":    renderOn,
		"TRUE":  renderOn,
		"off":   renderOff,
		"plain": renderOff,
		"raw":   renderOff,
	}
	for in, want := range cases {
		got, err := parseRenderMode(in)
		if err != nil || got != want {
			t.Errorf("parseRenderMode(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseRenderMode("markdown-ish"); err == nil {
		t.Error("expected an error for an unknown mode")
	}
}

// TestChatRendererOffIsByteIdentical is the compatibility guarantee: anything
// consuming `aegis chat` output through a pipe (a script, a CI job, another
// agent) must see exactly what it saw before rendering existed. A non-*os.File
// writer is never a terminal, so renderAuto lands here too.
func TestChatRendererOffIsByteIdentical(t *testing.T) {
	for _, mode := range []renderMode{renderOff, renderAuto} {
		var buf bytes.Buffer
		c := newChatRenderer(&buf, mode)
		if c.enabled() {
			t.Fatalf("mode %v: renderer must not be enabled writing to a buffer", mode)
		}
		c.Text("# Heading\n\nbody")
		c.ToolCall("read_file", json.RawMessage(`{"path":"x"}`))
		c.ToolResult("contents", false)
		c.ToolResult("boom", true)
		c.Notice("cold load")
		c.Done()

		want := "# Heading\n\nbody" +
			"\n[tool: read_file {\"path\":\"x\"}]\n" +
			"[tool result (ok): contents]\n" +
			"[tool result (error): boom]\n" +
			"\n[notice: cold load]\n" +
			"\n"
		if buf.String() != want {
			t.Errorf("mode %v: raw output drifted\n got: %q\nwant: %q", mode, buf.String(), want)
		}
	}
}

func TestChatRendererOnRendersMarkdown(t *testing.T) {
	var buf bytes.Buffer
	c := newChatRenderer(&buf, renderOn)
	if !c.enabled() {
		t.Fatal("renderOn must force rendering on regardless of the writer")
	}
	c.Text("| tool | status |\n|---|---|\n| trivy | ok |\n")
	c.Done()

	out := buf.String()
	// glamour draws table borders; the point of the test is that the table did
	// not arrive as its literal pipe-delimited source.
	if strings.Contains(out, "|---|---|") {
		t.Errorf("table was passed through as raw markdown source:\n%s", out)
	}
	if !strings.Contains(out, "trivy") {
		t.Errorf("table content lost entirely:\n%s", out)
	}
}

// TestChatRendererFlushesEverything guards the one way rendering can be worse
// than not rendering: buffered prose that is never emitted.
func TestChatRendererFlushesEverything(t *testing.T) {
	var buf bytes.Buffer
	c := newChatRenderer(&buf, renderOn)
	for _, chunk := range []string{"first para", ".\n", "\nsecond", " para.\n\nthird para.\n"} {
		c.Text(chunk)
	}
	c.Done()
	// glamour interleaves SGR codes between words, so compare against the
	// stripped text rather than the styled bytes.
	got := termsafe.StripControlSeqs(buf.String())
	for _, want := range []string{"first para", "second para", "third para"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from output:\n%s", want, got)
		}
	}
}

func TestSafeSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int // 0 means "not splittable yet"
	}{
		{"incomplete paragraph", "hello world", 0},
		{"one closed paragraph", "hello.\n\nworld", len("hello.\n\n")},
		{"blank line not yet terminated", "hello.\n\n", len("hello.\n\n")},
		// A fence that has not closed may contain blank lines that are code,
		// not block boundaries — cutting there would render half a program as
		// prose.
		{"inside an open fence", "intro.\n\n```go\nfunc main() {\n\n\tx := 1\n", 0},
		{"after a closed fence", "```go\nx := 1\n```\n\nafter", len("```go\nx := 1\n```\n\n")},
		// Cutting between loose list items renders each item as its own list,
		// which restarts an ordered list's numbering at 1 — so the only legal
		// cut in this input is after the list has ended, taking both items.
		{"loose list cuts only after it ends", "1. one\n\n2. two\n\nafter", len("1. one\n\n2. two\n\n")},
		{"mid loose list", "1. one\n\n2. two", 0},
		{"list continuation line", "- one\n\n  still one\n\ntail", len("- one\n\n  still one\n\n")},
		{"after a list ends", "- one\n- two\n\nA new paragraph.\n\ntail", len("- one\n- two\n\nA new paragraph.\n\n")},
		{"heading then paragraph", "# Title\n\nbody", len("# Title\n\n")},
		// A table is terminated by a blank line, so it never needs special
		// handling — but it must not be cut mid-row.
		{"unterminated table", "| a | b |\n|---|---|\n| 1 | 2 |\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeSplit(tt.in); got != tt.want {
				t.Errorf("safeSplit(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestSafeSplitPreservesContent is the invariant that matters more than any
// individual boundary: repeatedly cutting and emitting must never drop or
// duplicate a byte of the model's output.
func TestSafeSplitPreservesContent(t *testing.T) {
	src := "# Title\n\nIntro para.\n\n```go\nfunc f() {\n\n\treturn\n}\n```\n\n1. one\n\n2. two\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\ndone.\n"
	var got strings.Builder
	rest := ""
	for i := 0; i < len(src); i++ {
		rest += string(src[i])
		for {
			cut := safeSplit(rest)
			if cut <= 0 {
				break
			}
			got.WriteString(rest[:cut])
			rest = rest[cut:]
		}
	}
	got.WriteString(rest) // the final Flush
	if got.String() != src {
		t.Errorf("byte-for-byte round trip failed:\n got: %q\nwant: %q", got.String(), src)
	}
}

func TestPrettyToolInput(t *testing.T) {
	if got := prettyToolInput(json.RawMessage(`{}`), 80); got != "" {
		t.Errorf("empty args should print nothing, got %q", got)
	}
	got := prettyToolInput(json.RawMessage(`{"path":"a.go","content":"`+strings.Repeat("x", 500)+`"}`), 80)
	if !strings.Contains(got, "\n") {
		t.Errorf("expected indented multi-line JSON, got %q", got)
	}
	if !strings.Contains(got, `"path": "a.go"`) {
		t.Errorf("expected the informative key to survive, got %q", got)
	}
	// The 500-byte body is the thing that made a tool call unreadable; the
	// path is the thing the reader actually wants.
	if strings.Contains(got, strings.Repeat("x", 200)) {
		t.Errorf("long scalar was not clipped:\n%s", got)
	}
}

func TestPrettyToolInputNonJSON(t *testing.T) {
	if got := prettyToolInput(json.RawMessage(`not json at all`), 80); got != "not json at all" {
		t.Errorf("non-JSON input should pass through trimmed, got %q", got)
	}
}
