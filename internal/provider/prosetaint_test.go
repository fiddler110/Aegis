package provider

import (
	"testing"

	"github.com/fiddler110/aegis/internal/trust"
)

func TestProseTaintIndex_NilWhenNoUntrustedContent(t *testing.T) {
	idx := buildProseTaintIndex([]Message{
		{Role: RoleUser, Content: []Block{TextBlock{Text: "ordinary message"}}},
		{Role: RoleUser, Content: []Block{ToolResultBlock{ToolUseID: "tu_0", Content: "plain tool output, not wrapped"}}},
	})
	if idx != nil {
		t.Fatalf("expected a nil index when no message carries the untrusted-content marker, got %+v", idx)
	}
	if idx.reproducesUntrustedContent("anything, since idx is nil") {
		t.Error("a nil index must never report reproduction")
	}
}

func TestProseTaintIndex_DetectsReproducedSpan(t *testing.T) {
	wrapped := trust.Wrap("web_untrusted_output", nil, "a URL fetched from the web",
		"some preamble text padding this out past the shingle window, then: SECRET_TOOL_CALL_MARKER_0123456789 and more padding after it too.", false)
	idx := buildProseTaintIndex([]Message{
		{Role: RoleUser, Content: []Block{ToolResultBlock{ToolUseID: "tu_0", Content: wrapped}}},
	})
	if idx == nil {
		t.Fatal("expected a non-nil index once a wrapped tool result is present")
	}
	if !idx.reproducesUntrustedContent("then: SECRET_TOOL_CALL_MARKER_0123456789 and more") {
		t.Error("a span copied verbatim from the untrusted content was not detected")
	}
	if idx.reproducesUntrustedContent("this text never appeared anywhere in the untrusted content at all") {
		t.Error("unrelated text was reported as reproduced")
	}
}

func TestProseTaintIndex_ShortSpanNeverFlagged(t *testing.T) {
	wrapped := trust.Wrap("web_untrusted_output", nil, "a URL fetched from the web", "short", false)
	idx := buildProseTaintIndex([]Message{
		{Role: RoleUser, Content: []Block{ToolResultBlock{ToolUseID: "tu_0", Content: wrapped}}},
	})
	// The untrusted content itself is shorter than taintWindowBytes, so
	// nothing was indexed; idx may be non-nil (a wrapped result was seen) but
	// must still report false for everything.
	if idx.reproducesUntrustedContent("short") {
		t.Error("content shorter than the shingle window must never be flagged")
	}
}
