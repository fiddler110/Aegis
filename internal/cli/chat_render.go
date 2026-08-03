package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/fiddler110/aegis/internal/termsafe"
)

// renderMode controls whether `aegis chat`'s default (text) output is rendered
// as markdown or written through byte-for-byte.
type renderMode int

const (
	renderAuto renderMode = iota // markdown when stdout is a terminal
	renderOn                     // markdown always (useful for `| less -R`)
	renderOff                    // raw stream, the pre-P56.1 behavior
)

func parseRenderMode(s string) (renderMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return renderAuto, nil
	case "on", "true", "yes", "always":
		return renderOn, nil
	case "off", "false", "no", "never", "plain", "raw":
		return renderOff, nil
	}
	return renderAuto, fmt.Errorf("invalid render mode %q (want auto, on, or off)", s)
}

// chatRenderer turns the engine's event stream into terminal output. With
// markdown rendering off it is a pass-through and reproduces the original
// behavior exactly; with it on, prose is buffered and flushed through glamour
// at *block* boundaries, so headings, tables, lists and fenced code arrive as
// structure rather than as one undifferentiated wall of text.
//
// Buffering is the price of rendering: markdown cannot be styled a token at a
// time, because the meaning of a line ("| a | b |", "  - x") is not knowable
// until the block it belongs to is complete. Flushing per block rather than
// per turn keeps output arriving progressively — every paragraph, not every
// answer — which is what a plain terminal without a repaintable region can
// offer.
type chatRenderer struct {
	w io.Writer
	r *glamour.TermRenderer // nil when rendering is off
	// buf holds prose that has arrived but has not yet reached a safe block
	// boundary. It is bounded by one assistant turn, which the caller already
	// accumulates in full anyway.
	buf   strings.Builder
	width int

	toolStyle   lipgloss.Style
	okStyle     lipgloss.Style
	errStyle    lipgloss.Style
	noticeStyle lipgloss.Style
	bodyStyle   lipgloss.Style
}

// newChatRenderer builds a renderer for out under mode. out is inspected for a
// terminal only in renderAuto; anything that is not an *os.File (a test buffer,
// a pipe) is treated as not a terminal.
func newChatRenderer(out io.Writer, mode renderMode) *chatRenderer {
	c := &chatRenderer{w: out, width: 80}
	if !markdownWanted(out, mode) {
		return c
	}

	if f, ok := out.(*os.File); ok {
		if w, _, err := term.GetSize(f.Fd()); err == nil && w > 20 {
			// Cap the measured width: prose word-wrapped at 200+ columns is
			// physically hard to track back to the next line's start.
			c.width = min(w, 100)
		}
	}

	// NO_COLOR governs both halves of the output — glamour's markdown styling
	// and this file's own chrome. Styling only one of them is the worst
	// outcome: a terminal that asked for no color still gets some.
	color := os.Getenv("NO_COLOR") == ""
	style := "notty"
	if color {
		style = glamourEnvStyle()
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(c.width),
	)
	if err != nil {
		// A renderer we could not construct is not a fatal condition — it
		// downgrades to the raw stream, which is still the whole answer.
		return c
	}
	c.r = r
	if color {
		// A zero lipgloss.Style renders text unchanged, so leaving these unset
		// under NO_COLOR needs no second code path.
		c.toolStyle = lipgloss.NewStyle().Bold(true)
		c.okStyle = lipgloss.NewStyle().Faint(true)
		c.errStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
		c.noticeStyle = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("3"))
		c.bodyStyle = lipgloss.NewStyle().Faint(true)
	}
	return c
}

// glamourEnvStyle honors GLAMOUR_STYLE the way every other glamour-based CLI
// does, defaulting to the dark style (glamour's own default) rather than
// probing the terminal background, which is unreliable over ssh and in
// multiplexers.
func glamourEnvStyle() string {
	if s := strings.TrimSpace(os.Getenv("GLAMOUR_STYLE")); s != "" {
		return s
	}
	return "dark"
}

func markdownWanted(out io.Writer, mode renderMode) bool {
	switch mode {
	case renderOff:
		return false
	case renderOn:
		return true
	}
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}

// enabled reports whether markdown rendering is active. Callers use it to keep
// the raw path byte-identical to what it was before rendering existed.
func (c *chatRenderer) enabled() bool { return c != nil && c.r != nil }

// Text accepts a chunk of streamed assistant prose.
func (c *chatRenderer) Text(s string) {
	if !c.enabled() {
		fmt.Fprint(c.w, s)
		return
	}
	c.buf.WriteString(s)
	c.drain()
}

// drain renders and emits every complete block currently buffered, leaving the
// incomplete tail behind.
func (c *chatRenderer) drain() {
	for {
		src := c.buf.String()
		cut := safeSplit(src)
		if cut <= 0 {
			return
		}
		c.emit(src[:cut])
		rest := src[cut:]
		c.buf.Reset()
		c.buf.WriteString(rest)
	}
}

// Flush renders whatever prose is still buffered. Called at a tool call, the
// end of a turn, and on error — anywhere the next thing printed is not more of
// this paragraph.
func (c *chatRenderer) Flush() {
	if !c.enabled() {
		return
	}
	src := c.buf.String()
	c.buf.Reset()
	if strings.TrimSpace(src) == "" {
		return
	}
	c.emit(src)
}

func (c *chatRenderer) emit(src string) {
	// The source here is the model's own output: sanitize before glamour or
	// the fallback writes any of it to a terminal (P24.20, FIND-17).
	src = termsafe.StripControlSeqs(src)
	if strings.TrimSpace(src) == "" {
		return
	}
	rendered, err := c.r.Render(src)
	if err != nil {
		fmt.Fprint(c.w, src)
		return
	}
	fmt.Fprint(c.w, strings.TrimRight(rendered, "\n")+"\n")
}

// ToolCall prints a tool invocation. The raw argument JSON was previously
// emitted as one unbroken line, which for a write_file or an edit meant the
// file's entire contents ran off the screen inside a `{"path":...}` wrapper.
func (c *chatRenderer) ToolCall(name string, input json.RawMessage) {
	if !c.enabled() {
		fmt.Fprintf(c.w, "\n[tool: %s %s]\n", name, string(input))
		return
	}
	c.Flush()
	fmt.Fprintf(c.w, "\n%s %s\n", c.toolStyle.Render("▸ tool"), c.toolStyle.Render(name))
	if body := prettyToolInput(input, c.width); body != "" {
		fmt.Fprintln(c.w, c.bodyStyle.Render(indentBlock(body, "  ")))
	}
}

// ToolResult prints a tool's result, indented under the call it belongs to.
func (c *chatRenderer) ToolResult(result string, isErr bool) {
	tag := "ok"
	if isErr {
		tag = "error"
	}
	if !c.enabled() {
		fmt.Fprintf(c.w, "[tool result (%s): %s]\n", tag, truncate(result, 500))
		return
	}
	label := c.okStyle.Render("  └ ok")
	if isErr {
		label = c.errStyle.Render("  └ error")
	}
	body := strings.TrimRight(termsafe.StripDangerousSeqs(truncate(result, 500)), "\n")
	if body == "" {
		fmt.Fprintln(c.w, label)
		return
	}
	if strings.Contains(body, "\n") {
		fmt.Fprintf(c.w, "%s\n%s\n", label, c.bodyStyle.Render(indentBlock(body, "    ")))
		return
	}
	fmt.Fprintf(c.w, "%s %s\n", label, c.bodyStyle.Render(body))
}

// Notice prints daemon/CLI narration (phase boundaries, stalls, advisories).
func (c *chatRenderer) Notice(s string) {
	if !c.enabled() {
		fmt.Fprintf(c.w, "\n[notice: %s]\n", s)
		return
	}
	c.Flush()
	fmt.Fprintf(c.w, "\n%s\n", c.noticeStyle.Render("● "+s))
}

// Done ends the turn.
func (c *chatRenderer) Done() {
	if !c.enabled() {
		fmt.Fprintln(c.w)
		return
	}
	c.Flush()
}

// safeSplit returns the byte offset at which src can be cut so that everything
// before the cut is a complete sequence of markdown blocks, or 0 if it cannot
// be cut yet.
//
// "Complete" is stricter than "ends in a blank line", because two constructs
// span blank lines and render wrongly when severed:
//
//   - a fenced code block, whose body may contain blank lines and whose
//     content is not markdown at all;
//   - a loose list, where cutting between items restarts an ordered list's
//     numbering at 1 and turns one list into several.
//
// So a candidate cut is rejected while an odd number of ``` fences precede it,
// and rejected when the text that follows it continues a list. Everything else
// — headings, paragraphs, tables (which a blank line terminates), block quotes
// — is safe to render on its own.
func safeSplit(src string) int {
	best := 0
	fenced := false
	inList := false
	offset := 0

	lines := strings.SplitAfter(src, "\n")
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(body)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			inList = false
			offset += len(line)
			continue
		}

		// The last element of SplitAfter is the not-yet-newline-terminated
		// tail; a block boundary needs the newline to have arrived.
		isComplete := i < len(lines)-1

		if !fenced && trimmed == "" && isComplete {
			// A blank line closes the preceding block. It is a valid cut only
			// if what comes next does not continue a list.
			end := offset + len(line)
			if !inList || !continuesList(src[end:]) {
				best = end
			}
			offset = end
			continue
		}

		if !fenced && trimmed != "" {
			inList = isListItem(trimmed) || (inList && strings.HasPrefix(body, " "))
		}
		offset += len(line)
	}

	if fenced {
		// An unterminated fence means the code block is still streaming; the
		// last safe cut may sit inside it, so refuse to cut at all.
		return 0
	}
	return best
}

func isListItem(trimmed string) bool {
	if len(trimmed) >= 2 {
		switch trimmed[0] {
		case '-', '*', '+':
			if trimmed[1] == ' ' {
				return true
			}
		}
	}
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(trimmed) && (trimmed[i] == '.' || trimmed[i] == ')') && trimmed[i+1] == ' '
}

// continuesList reports whether rest picks up a list that was already open —
// either another item or an indented continuation line.
func continuesList(rest string) bool {
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return isListItem(trimmed) || strings.HasPrefix(line, "  ")
	}
	// Nothing but blank lines so far: the list may yet continue, so treat the
	// boundary as unsafe until real text arrives.
	return true
}

// prettyToolInput re-indents a tool's argument JSON and clips long scalars, so
// a write_file call shows its path and the shape of its payload instead of the
// payload itself. Non-JSON input is returned trimmed and clipped.
func prettyToolInput(input json.RawMessage, width int) string {
	raw := strings.TrimSpace(string(input))
	if raw == "" || raw == "{}" || raw == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return truncate(raw, 400)
	}
	clipStrings(&v, max(width-8, 40))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return truncate(raw, 400)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// clipStrings shortens every string leaf in a decoded JSON value to limit
// bytes, in place. Long leaves are what make a tool call unreadable: the keys
// are the information, the 4KB file body is not.
func clipStrings(v *any, limit int) {
	switch t := (*v).(type) {
	case string:
		if len(t) > limit {
			*v = truncate(t, limit)
		}
	case map[string]any:
		for k, val := range t {
			clipStrings(&val, limit)
			t[k] = val
		}
	case []any:
		for i, val := range t {
			clipStrings(&val, limit)
			t[i] = val
		}
	}
}

func indentBlock(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
