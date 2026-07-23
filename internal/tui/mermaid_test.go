package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

func TestRenderMermaidBlocks(t *testing.T) {
	in := "Here is a diagram:\n\n```mermaid\ngraph TD\nA-->B\n```\n\nDone."
	out := renderMermaidBlocks(in)
	if strings.Contains(out, "graph TD") {
		t.Fatalf("mermaid source should have been replaced, still present:\n%s", out)
	}
	if !strings.Contains(out, "│ A │") || !strings.Contains(out, "▼") {
		t.Fatalf("expected inline ASCII diagram, got:\n%s", out)
	}
	// Surrounding prose is preserved.
	if !strings.Contains(out, "Here is a diagram:") || !strings.Contains(out, "Done.") {
		t.Fatalf("surrounding prose was lost:\n%s", out)
	}
}

func TestRenderMermaidBlocksLeftUntouched(t *testing.T) {
	// Unsupported diagram type: the block is kept verbatim so the raw source
	// still shows.
	unsupported := "```mermaid\nclassDiagram\nClass01\n```"
	if got := renderMermaidBlocks(unsupported); got != unsupported {
		t.Fatalf("unsupported diagram should be untouched:\n%s", got)
	}

	// Unterminated fence (mid-stream): left as-is for a later pass.
	partial := "```mermaid\ngraph TD\nA-->B"
	if got := renderMermaidBlocks(partial); got != partial {
		t.Fatalf("unterminated fence should be untouched:\n%s", got)
	}

	// No mermaid at all: identity.
	plain := "just some prose with a ```go\ncode block\n```"
	if got := renderMermaidBlocks(plain); got != plain {
		t.Fatalf("non-mermaid content should be untouched:\n%s", got)
	}
}

// TestMermaidRendersInTranscript drives the real flush path: an assistant
// message carrying a mermaid fence lands in the transcript as a box-drawing
// diagram, not raw source.
func TestMermaidRendersInTranscript(t *testing.T) {
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "m", WorkDir: t.TempDir()})
	m = driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m.streaming = true
	m.applyEvent(api.Event{Kind: api.KindText, Text: "Flow:\n\n```mermaid\ngraph TD\nA-->B\n```\n"})
	m.applyEvent(api.Event{Kind: api.KindTurnDone})
	m.refresh()

	view := ansi.Strip(m.render())
	if !strings.Contains(view, "┌") || !strings.Contains(view, "▼") {
		t.Fatalf("expected a rendered diagram in the transcript, got:\n%s", view)
	}
	if strings.Contains(view, "graph TD") {
		t.Fatalf("raw mermaid source leaked into the transcript:\n%s", view)
	}
}
