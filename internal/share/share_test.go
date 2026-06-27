package share

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/session"
)

func sampleSession() *session.Session {
	return &session.Session{
		ID:    "abcdef123456",
		Title: "Fix the parser",
		Mode:  "build",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{
				provider.TextBlock{Text: "read main.go"},
			}},
			{Role: provider.RoleAssistant, Content: []provider.Block{
				provider.ThinkingBlock{Text: "I should read the file", Signature: "sig"},
				provider.TextBlock{Text: "Reading it now."},
				provider.ToolUseBlock{ID: "t1", Name: "read_file", Input: json.RawMessage(`{"path":"main.go"}`)},
			}},
			{Role: provider.RoleUser, Content: []provider.Block{
				provider.ToolResultBlock{ToolUseID: "t1", Content: "package main"},
			}},
		},
	}
}

func TestParseFormat(t *testing.T) {
	cases := map[string]Format{"": FormatHTML, "html": FormatHTML, "md": FormatMarkdown, "markdown": FormatMarkdown, "json": FormatJSON}
	for in, want := range cases {
		got, err := ParseFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseFormat("pdf"); err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestRenderHTML(t *testing.T) {
	data, err := Render(sampleSession(), FormatHTML)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"<!doctype html>", "Fix the parser", "🧑 User", "🤖 Assistant", "read_file", "package main", "💭 thinking"} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestRenderHTMLEscapes(t *testing.T) {
	sess := &session.Session{
		ID:   "x",
		Mode: "build",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "<script>alert(1)</script>"}}},
		},
	}
	data, _ := Render(sess, FormatHTML)
	if strings.Contains(string(data), "<script>alert(1)</script>") {
		t.Error("user text was not HTML-escaped")
	}
	if !strings.Contains(string(data), "&lt;script&gt;") {
		t.Error("expected escaped script tag")
	}
}

func TestRenderMarkdownAndJSON(t *testing.T) {
	md, err := Render(sampleSession(), FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "# Fix the parser") || !strings.Contains(string(md), "read_file") {
		t.Errorf("markdown missing expected content:\n%s", md)
	}

	js, err := Render(sampleSession(), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var round session.Session
	if err := json.Unmarshal(js, &round); err != nil {
		t.Fatalf("json not valid: %v", err)
	}
	if round.Title != "Fix the parser" || len(round.Messages) != 3 {
		t.Errorf("json round-trip lost data: %+v", round)
	}
}
