package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Rendering budgets for tool activity in the transcript. These keep large
// outputs from flooding the scrollback while still showing meaningfully more
// than a single truncated line.
const (
	maxDiffLines       = 24 // diff lines shown for an edit/write before collapsing
	maxToolResultLines = 16 // body lines shown for multi-line tool output
)

// renderToolCall renders the header (and, for file-mutating tools, an inline
// diff) shown when a tool is about to run. File edits are diffed from the tool
// input — old_string/new_string for edits, content for writes — so the user
// sees exactly what is changing without waiting for the result.
func renderToolCall(th theme, name string, input json.RawMessage, width int) string {
	switch name {
	case "edit_file":
		if s, ok := renderEditDiff(th, name, input, width); ok {
			return s
		}
	case "multi_edit":
		if s, ok := renderMultiEditDiff(th, name, input, width); ok {
			return s
		}
	case "write_file":
		if s, ok := renderWriteDiff(th, name, input, width); ok {
			return s
		}
	case "shell":
		if s, ok := renderShellCall(th, name, input, width); ok {
			return s
		}
	}
	// Generic: tool name plus a compact one-line view of the input.
	budget := max(width-len(name)-4, 20)
	return th.tool.Render(fmt.Sprintf("⚙ %s  %s", name, truncate(oneLine(string(input)), budget)))
}

// renderToolResult renders a finished tool call. Short, single-line results are
// shown inline; multi-line output (shell, read, search) is shown as a capped,
// gutter-marked block instead of being collapsed to one truncated line.
func renderToolResult(th theme, name, result string, isErr bool, width int) string {
	tag, style := "✓", th.tool
	if isErr {
		tag, style = "✗", th.toolErr
	}
	result = strings.TrimRight(result, "\n")

	if !strings.Contains(result, "\n") {
		budget := max(width-len(name)-6, 20)
		return style.Render(fmt.Sprintf("%s %s → %s", tag, name, truncate(oneLine(result), budget)))
	}

	var b strings.Builder
	b.WriteString(style.Render(fmt.Sprintf("%s %s", tag, name)) + "\n")
	b.WriteString(renderBlock(th, result, maxToolResultLines, width))
	return strings.TrimRight(b.String(), "\n")
}

// renderBlock renders text as an indented, gutter-marked block capped at max
// lines, with a "… N more lines" footer when truncated.
func renderBlock(th theme, text string, maxLines, width int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	hidden := 0
	if len(lines) > maxLines {
		hidden = len(lines) - maxLines
		lines = lines[:maxLines]
	}
	gutter := th.toolGut.Render("│ ")
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString("  " + gutter + th.toolBody.Render(truncate(ln, max(width-6, 16))) + "\n")
	}
	if hidden > 0 {
		b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("… %d more line(s)", hidden)) + "\n")
	}
	return b.String()
}

// diffLines turns an old→new string pair into styled, prefixed diff lines
// (removed first, then added), capped at budget with a hidden-line count.
func diffLines(th theme, oldS, newS string, width, budget int) (lines []string, hidden int) {
	add := func(prefix string, st lipgloss.Style, text string) {
		if text == "" {
			return
		}
		for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			lines = append(lines, st.Render(prefix+truncate(ln, max(width-6, 16))))
		}
	}
	add("- ", th.diffDel, oldS)
	add("+ ", th.diffAdd, newS)
	if len(lines) > budget {
		hidden = len(lines) - budget
		lines = lines[:budget]
	}
	return lines, hidden
}

// assembleDiff builds a tool header followed by an indented diff body.
func assembleDiff(th theme, name, path string, lines []string, hidden int) string {
	var b strings.Builder
	b.WriteString(th.tool.Render("⚙ "+name+" ") + th.diffMeta.Render(path) + "\n")
	for _, ln := range lines {
		b.WriteString("  " + ln + "\n")
	}
	if hidden > 0 {
		b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("… %d more diff line(s)", hidden)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderEditDiff(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if json.Unmarshal(input, &a) != nil || a.Path == "" {
		return "", false
	}
	lines, hidden := diffLines(th, a.OldString, a.NewString, width, maxDiffLines)
	return assembleDiff(th, name, a.Path, lines, hidden), true
}

func renderMultiEditDiff(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Edits []struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if json.Unmarshal(input, &a) != nil || len(a.Edits) == 0 {
		return "", false
	}
	var b strings.Builder
	budget := maxDiffLines
	for i, e := range a.Edits {
		if budget <= 0 {
			b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("… %d more edit(s)", len(a.Edits)-i)) + "\n")
			break
		}
		lines, hidden := diffLines(th, e.OldString, e.NewString, width, budget)
		budget -= len(lines)
		if i == 0 {
			b.WriteString(th.tool.Render("⚙ "+name+" ") + th.diffMeta.Render(e.Path) + "\n")
		} else {
			b.WriteString("  " + th.diffMeta.Render(e.Path) + "\n")
		}
		for _, ln := range lines {
			b.WriteString("  " + ln + "\n")
		}
		if hidden > 0 {
			b.WriteString("  " + th.diffMeta.Render(fmt.Sprintf("… %d more diff line(s)", hidden)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n"), true
}

func renderWriteDiff(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if json.Unmarshal(input, &a) != nil || a.Path == "" {
		return "", false
	}
	lines, hidden := diffLines(th, "", a.Content, width, maxDiffLines)
	return assembleDiff(th, name, a.Path, lines, hidden), true
}

func renderShellCall(th theme, name string, input json.RawMessage, width int) (string, bool) {
	var a struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if json.Unmarshal(input, &a) != nil || a.Command == "" {
		return "", false
	}
	header := th.tool.Render("⚙ " + name)
	if a.Background {
		header += " " + th.diffMeta.Render("(background)")
	}
	var b strings.Builder
	b.WriteString(header + "\n")
	b.WriteString(renderBlock(th, a.Command, maxToolResultLines, width))
	return strings.TrimRight(b.String(), "\n"), true
}
