package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/scottymacleod/aegis/internal/api"
)

const (
	completionVisibleRows = 6
	// One separator line only — no border chars. The removed top+bottom
	// border chars (was +2) become +1 so the viewport height is reclaimed.
	completionBoxH = completionVisibleRows + 1
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

// view renders the borderless completion list (completionBoxH lines tall).
//
// Scroll strategy: page-based. The visible window only changes when the
// selected item crosses a page boundary — so within a page only the
// highlight moves; the list text stays completely fixed. This avoids the
// "everything drifts by one row" feel of per-item scrolling.
func (c completionState) view(width int) string {
	const nameCol = 14

	// Determine which page the selected item is on.
	page := c.selected / completionVisibleRows
	start := page * completionVisibleRows
	end := min(start+completionVisibleRows, len(c.items))

	// Separator line. When there are multiple pages, append a compact
	// page indicator so the user can see there are more items.
	totalPages := (len(c.items) + completionVisibleRows - 1) / completionVisibleRows
	pageCtx := ""
	if totalPages > 1 {
		pageCtx = lipgloss.NewStyle().
			Foreground(colTextMuted).
			Render(fmt.Sprintf(" %d/%d ", page+1, totalPages))
	}
	sepW := max(width-lipgloss.Width(pageCtx), 0)
	sep := lipgloss.NewStyle().Foreground(colBorder).Render(strings.Repeat("─", sepW)) + pageCtx

	// Build rows. Selected item gets a brand-coloured background spanning the
	// full width so the highlight is instantly obvious without any extra
	// marker character. Unselected items are plain text.
	lines := make([]string, 0, completionVisibleRows)
	for i := start; i < end; i++ {
		e := c.items[i]
		name := "/" + e.name
		namePad := strings.Repeat(" ", max(nameCol-len(name), 1))
		desc := truncate(e.desc, max(width-nameCol-2, 4))

		var row string
		if i == c.selected {
			row = lipgloss.NewStyle().
				Background(colBrandBg).
				Foreground(colBrandFg).
				Bold(true).
				Width(nameCol).
				Render(name) +
				lipgloss.NewStyle().
				Background(colBrandBg).
				Foreground(colTextDim).
				Width(width - nameCol).
				Render(" " + desc)
		} else {
			row = lipgloss.NewStyle().Foreground(colTextDim).Render("  "+name) +
				namePad +
				lipgloss.NewStyle().Foreground(colTextMuted).Render(desc)
		}
		lines = append(lines, row)
	}
	// Pad to fixed height so the layout never shifts when the last page is
	// shorter than completionVisibleRows.
	for len(lines) < completionVisibleRows {
		lines = append(lines, "")
	}

	return sep + "\n" + strings.Join(lines, "\n")
}
