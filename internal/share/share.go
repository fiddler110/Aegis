// Package share renders a stored session into a self-contained, shareable
// transcript — Markdown, JSON, or a single HTML file with embedded styles and
// inline images. It is the local-first equivalent of a "share link": the output
// is a portable artifact, not a hosted page.
package share

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/provider"
	"github.com/scottymacleod/aegis/internal/session"
)

// Format selects the export representation.
type Format string

const (
	FormatHTML     Format = "html"
	FormatMarkdown Format = "md"
	FormatJSON     Format = "json"
)

// maxResultChars bounds how much of a tool result is embedded so a noisy run
// doesn't bloat the artifact.
const maxResultChars = 8000

// Ext returns the file extension (without dot) for a format.
func (f Format) Ext() string {
	switch f {
	case FormatMarkdown:
		return "md"
	case FormatJSON:
		return "json"
	default:
		return "html"
	}
}

// ParseFormat normalizes a user-supplied format string.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "html":
		return FormatHTML, nil
	case "md", "markdown":
		return FormatMarkdown, nil
	case "json":
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unknown format %q (use html, md, or json)", s)
	}
}

// Render produces the transcript bytes for the given format.
func Render(sess *session.Session, format Format) ([]byte, error) {
	switch format {
	case FormatJSON:
		return json.MarshalIndent(sess, "", "  ")
	case FormatMarkdown:
		return []byte(renderMarkdown(sess)), nil
	default:
		return []byte(renderHTML(sess)), nil
	}
}

func title(sess *session.Session) string {
	if strings.TrimSpace(sess.Title) != "" {
		return sess.Title
	}
	return "Session " + sess.ID
}

// toolNameIndex maps tool_use IDs to tool names across the whole transcript so
// tool results can be labelled.
func toolNameIndex(sess *session.Session) map[string]string {
	idx := map[string]string{}
	for _, m := range sess.Messages {
		for _, b := range m.Content {
			if tu, ok := b.(provider.ToolUseBlock); ok {
				idx[tu.ID] = tu.Name
			}
		}
	}
	return idx
}

func truncate(s string) (string, bool) {
	r := []rune(s)
	if len(r) <= maxResultChars {
		return s, false
	}
	return string(r[:maxResultChars]), true
}

// --- Markdown ---

func renderMarkdown(sess *session.Session) string {
	names := toolNameIndex(sess)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title(sess))
	fmt.Fprintf(&b, "_Mode: %s · Exported %s_\n\n", sess.Mode, time.Now().Format("2006-01-02 15:04"))

	for _, m := range sess.Messages {
		switch m.Role {
		case provider.RoleUser:
			renderMarkdownUser(&b, m, names)
		case provider.RoleAssistant:
			renderMarkdownAssistant(&b, m)
		}
	}
	return b.String()
}

func renderMarkdownUser(b *strings.Builder, m provider.Message, names map[string]string) {
	var text strings.Builder
	var images int
	var results []provider.ToolResultBlock
	for _, blk := range m.Content {
		switch v := blk.(type) {
		case provider.TextBlock:
			text.WriteString(v.Text)
		case provider.ImageBlock:
			images++
		case provider.ToolResultBlock:
			results = append(results, v)
		}
	}
	for _, r := range results {
		name := names[r.ToolUseID]
		if name == "" {
			name = "tool"
		}
		status := "✓"
		if r.IsError {
			status = "✗"
		}
		content, cut := truncate(r.Content)
		fmt.Fprintf(b, "%s **%s result**\n\n```\n%s\n```\n", status, name, content)
		if cut {
			b.WriteString("\n_(result truncated)_\n")
		}
		b.WriteString("\n")
	}
	if text.Len() == 0 && images == 0 {
		return
	}
	b.WriteString("### 🧑 User\n\n")
	if text.Len() > 0 {
		b.WriteString(text.String())
		b.WriteString("\n\n")
	}
	if images > 0 {
		fmt.Fprintf(b, "_[%d image(s) attached]_\n\n", images)
	}
}

func renderMarkdownAssistant(b *strings.Builder, m provider.Message) {
	wroteHeader := false
	header := func() {
		if !wroteHeader {
			b.WriteString("### 🤖 Assistant\n\n")
			wroteHeader = true
		}
	}
	for _, blk := range m.Content {
		switch v := blk.(type) {
		case provider.ThinkingBlock:
			if t := strings.TrimSpace(v.Text); t != "" {
				header()
				fmt.Fprintf(b, "<details><summary>💭 thinking</summary>\n\n%s\n\n</details>\n\n", t)
			}
		case provider.TextBlock:
			if v.Text != "" {
				header()
				b.WriteString(v.Text)
				b.WriteString("\n\n")
			}
		case provider.ToolUseBlock:
			header()
			input := strings.TrimSpace(string(v.Input))
			fmt.Fprintf(b, "🔧 **%s**\n\n```json\n%s\n```\n\n", v.Name, input)
		}
	}
}

// --- HTML ---

func renderHTML(sess *session.Session) string {
	names := toolNameIndex(sess)
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title(sess)))
	b.WriteString("<style>\n" + shareCSS + "</style>\n</head>\n<body>\n")
	b.WriteString("<main>\n")
	fmt.Fprintf(&b, "<header><h1>%s</h1><p class=\"meta\">Mode: %s · Exported %s</p></header>\n",
		html.EscapeString(title(sess)), html.EscapeString(sess.Mode), time.Now().Format("2006-01-02 15:04"))

	for _, m := range sess.Messages {
		switch m.Role {
		case provider.RoleUser:
			renderHTMLUser(&b, m, names)
		case provider.RoleAssistant:
			renderHTMLAssistant(&b, m)
		}
	}
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

func renderHTMLUser(b *strings.Builder, m provider.Message, names map[string]string) {
	var text strings.Builder
	var images []provider.ImageBlock
	var results []provider.ToolResultBlock
	for _, blk := range m.Content {
		switch v := blk.(type) {
		case provider.TextBlock:
			text.WriteString(v.Text)
		case provider.ImageBlock:
			images = append(images, v)
		case provider.ToolResultBlock:
			results = append(results, v)
		}
	}
	for _, r := range results {
		name := names[r.ToolUseID]
		if name == "" {
			name = "tool"
		}
		cls := "tool-result"
		if r.IsError {
			cls += " error"
		}
		content, cut := truncate(r.Content)
		fmt.Fprintf(b, "<details class=\"%s\"><summary>%s result</summary><pre>%s</pre>",
			cls, html.EscapeString(name), html.EscapeString(content))
		if cut {
			b.WriteString("<p class=\"muted\">(result truncated)</p>")
		}
		b.WriteString("</details>\n")
	}
	if text.Len() == 0 && len(images) == 0 {
		return
	}
	b.WriteString("<section class=\"msg user\"><div class=\"role\">🧑 User</div><div class=\"body\">\n")
	if text.Len() > 0 {
		fmt.Fprintf(b, "<p>%s</p>\n", htmlText(text.String()))
	}
	for _, img := range images {
		fmt.Fprintf(b, "<img alt=\"attached image\" src=\"data:%s;base64,%s\">\n",
			html.EscapeString(img.MediaType), img.Data)
	}
	b.WriteString("</div></section>\n")
}

func renderHTMLAssistant(b *strings.Builder, m provider.Message) {
	var inner strings.Builder
	for _, blk := range m.Content {
		switch v := blk.(type) {
		case provider.ThinkingBlock:
			if t := strings.TrimSpace(v.Text); t != "" {
				fmt.Fprintf(&inner, "<details class=\"thinking\"><summary>💭 thinking</summary><div>%s</div></details>\n", htmlText(t))
			}
		case provider.TextBlock:
			if v.Text != "" {
				fmt.Fprintf(&inner, "<p>%s</p>\n", htmlText(v.Text))
			}
		case provider.ToolUseBlock:
			input := strings.TrimSpace(string(v.Input))
			fmt.Fprintf(&inner, "<details class=\"tool-call\"><summary>🔧 %s</summary><pre>%s</pre></details>\n",
				html.EscapeString(v.Name), html.EscapeString(input))
		}
	}
	if inner.Len() == 0 {
		return
	}
	b.WriteString("<section class=\"msg assistant\"><div class=\"role\">🤖 Assistant</div><div class=\"body\">\n")
	b.WriteString(inner.String())
	b.WriteString("</div></section>\n")
}

// htmlText escapes text and converts newlines to <br> for readable paragraphs.
func htmlText(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>\n")
}

const shareCSS = `
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { margin: 0; font: 15px/1.6 -apple-system, system-ui, "Segoe UI", Roboto, sans-serif;
  background: #f6f7f9; color: #1c1e21; }
main { max-width: 820px; margin: 0 auto; padding: 24px 16px 80px; }
header h1 { margin: 0 0 4px; font-size: 22px; }
.meta, .muted { color: #6b7280; font-size: 13px; }
.msg { margin: 14px 0; border-radius: 12px; padding: 12px 16px; }
.msg .role { font-weight: 600; font-size: 13px; margin-bottom: 6px; opacity: .8; }
.msg.user { background: #e8f0fe; }
.msg.assistant { background: #ffffff; border: 1px solid #e5e7eb; }
.body p { margin: 6px 0; white-space: normal; }
.body img { max-width: 100%; border-radius: 8px; margin: 8px 0; }
pre { background: #0d1117; color: #e6edf3; padding: 10px 12px; border-radius: 8px;
  overflow-x: auto; font: 13px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
details { margin: 8px 0; }
summary { cursor: pointer; font-size: 13px; color: #374151; }
details.tool-call summary { color: #6d28d9; }
details.tool-result summary { color: #047857; }
details.tool-result.error summary { color: #b91c1c; }
details.thinking summary { color: #92400e; }
details.thinking div { color: #6b7280; font-style: italic; padding: 6px 0; }
@media (prefers-color-scheme: dark) {
  body { background: #0b0d10; color: #e6e8eb; }
  .msg.user { background: #16263f; }
  .msg.assistant { background: #15171a; border-color: #2a2d31; }
  .meta, .muted, summary { color: #9aa1ab; }
}
`
