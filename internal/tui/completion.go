package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/scottymacleod/aegis/internal/api"
)

const (
	completionVisibleRows = 6
	completionBoxH        = completionVisibleRows + 2 // inner rows + top/bottom border
)

// cmdEntry is one selectable command in the completion popup / palette.
type cmdEntry struct {
	name string
	desc string
}

// builtinCommands is the single source of truth for built-in slash commands,
// shared by the inline completion popup and the command palette.
var builtinCommands = []cmdEntry{
	{"help", "Show help or detail for a command"},
	{"persona", "List or switch active persona"},
	{"mode", "Switch permission mode (plan / build / auto)"},
	{"clear", "Clear the conversation transcript"},
	{"config", "Open configuration wizard"},
	{"memory", "Show saved memories"},
	{"remember", "Save a memory entry"},
	{"skills", "List saved skills"},
	{"commands", "List custom commands"},
	{"models", "Show current model info"},
	{"session", "Show session info or list sessions"},
	{"quit", "Exit Aegis"},
}

// commandsNeedingArgs are completed with a trailing space so the user can
// immediately type arguments rather than running the bare command.
var commandsNeedingArgs = map[string]bool{
	"persona":  true,
	"mode":     true,
	"remember": true,
	"help":     true,
	"session":  true,
}

// allCommandEntries returns built-in commands followed by custom ones.
func allCommandEntries(customs []api.CommandInfo) []cmdEntry {
	out := make([]cmdEntry, len(builtinCommands), len(builtinCommands)+len(customs))
	copy(out, builtinCommands)
	for _, c := range customs {
		out = append(out, cmdEntry{name: strings.ToLower(c.Name), desc: c.Description})
	}
	return out
}

// completionState tracks the inline slash-command completion popup.
type completionState struct {
	active   bool
	items    []cmdEntry
	selected int
}

// computeCompletion derives popup state from the current textarea value. The
// popup is active only while the value is a single slash token with no space
// yet typed — i.e. the user is still naming the command.
func computeCompletion(value string, all []cmdEntry) completionState {
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t\n") {
		return completionState{}
	}
	query := strings.ToLower(value[1:])

	var prefix, substr []cmdEntry
	for _, e := range all {
		ln := strings.ToLower(e.name)
		switch {
		case strings.HasPrefix(ln, query):
			prefix = append(prefix, e)
		case query != "" && strings.Contains(ln+" "+strings.ToLower(e.desc), query):
			substr = append(substr, e)
		}
	}
	matches := append(prefix, substr...)
	if len(matches) == 0 {
		return completionState{}
	}
	return completionState{active: true, items: matches}
}

func (c *completionState) move(delta int) {
	if !c.active || len(c.items) == 0 {
		return
	}
	c.selected = (c.selected + delta + len(c.items)) % len(c.items)
}

func (c completionState) current() (cmdEntry, bool) {
	if !c.active || c.selected < 0 || c.selected >= len(c.items) {
		return cmdEntry{}, false
	}
	return c.items[c.selected], true
}

// view renders the fixed-height popup box (completionBoxH lines tall).
func (c completionState) view(width int) string {
	innerW := max(width-4, 20) // border (2) + horizontal padding (2)

	// Scroll window so the selected item stays visible.
	start := 0
	if c.selected >= completionVisibleRows {
		start = c.selected - completionVisibleRows + 1
	}
	end := min(start+completionVisibleRows, len(c.items))

	nameStyleSel := lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(colTextDim)
	descStyleSel := lipgloss.NewStyle().Foreground(colTextDim)
	descStyle := lipgloss.NewStyle().Foreground(colTextMuted)

	const nameCol = 14
	lines := make([]string, 0, completionVisibleRows)
	for i := start; i < end; i++ {
		e := c.items[i]
		name := "/" + e.name
		pad := ""
		if n := nameCol - len(name); n > 0 {
			pad = strings.Repeat(" ", n)
		}
		descBudget := max(innerW-nameCol-2, 4)
		desc := truncate(e.desc, descBudget)

		var row string
		if i == c.selected {
			row = nameStyleSel.Render("▌ "+name) + pad + " " + descStyleSel.Render(desc)
		} else {
			row = nameStyle.Render("  "+name) + pad + " " + descStyle.Render(desc)
		}
		lines = append(lines, row)
	}
	for len(lines) < completionVisibleRows {
		lines = append(lines, "")
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Background(colSurface).
		Padding(0, 1).
		Width(innerW).
		Render(body)
}
