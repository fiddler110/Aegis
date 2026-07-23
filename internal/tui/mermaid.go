package tui

import (
	"strings"

	"github.com/fiddler110/aegis/internal/mermaidascii"
)

// renderMermaidBlocks (P40.9) is an mdRender preprocessing pass that replaces
// each complete ```mermaid fenced block with an inline ASCII / box-drawing
// rendering of the diagram, wrapped back in a plain code fence so glamour
// styles it like any other code block. Until now a model that inlined a small
// mermaid snippet (rather than calling the render_diagram tool) just showed it
// as an unstyled code block; this turns the common flowchart/sequence shapes
// into something viewable in the transcript itself.
//
// It is deliberately conservative: a block whose diagram type is unsupported or
// whose source doesn't parse is left byte-for-byte untouched (the raw mermaid
// source still shows, exactly the pre-P40.9 behavior), and an unterminated
// fence — what a block looks like mid-stream before its closing ``` arrives —
// is also left as-is, so it simply renders on the next pass once complete.
func renderMermaidBlocks(s string) string {
	// Cheap bail-out: the vast majority of assistant messages have no mermaid.
	if !strings.Contains(strings.ToLower(s), "mermaid") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		if !isMermaidFenceOpen(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}
		// Collect the body up to the matching closing fence.
		var body []string
		j := i + 1
		closed := false
		for j < len(lines) {
			t := strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
			if t == "```" || t == "~~~" {
				closed = true
				break
			}
			body = append(body, strings.TrimRight(lines[j], "\r"))
			j++
		}
		if closed {
			if art, ok := mermaidascii.Render(strings.Join(body, "\n")); ok {
				out = append(out, "```")
				out = append(out, strings.Split(art, "\n")...)
				out = append(out, "```")
				i = j + 1
				continue
			}
			// Recognized fence but the diagram didn't render: keep the original
			// block (open fence … close fence) verbatim.
			out = append(out, lines[i:j+1]...)
			i = j + 1
			continue
		}
		// Unterminated fence: emit the rest unchanged and stop.
		out = append(out, lines[i:]...)
		break
	}
	return strings.Join(out, "\n")
}

// isMermaidFenceOpen reports whether line opens a fenced block whose info
// string is exactly "mermaid" (``` or ~~~ fences, case-insensitive).
func isMermaidFenceOpen(line string) bool {
	t := strings.TrimSpace(strings.TrimRight(line, "\r"))
	for _, p := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, p) {
			return strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(t, p)), "mermaid")
		}
	}
	return false
}
