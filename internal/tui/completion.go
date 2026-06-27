package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

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
	{"persona", "Pick a persona from an interactive list"},
	{"mode", "Switch permission mode (plan / build / auto)"},
	{"clear", "Clear the conversation transcript"},
	{"config", "Open configuration wizard"},
	{"memory", "Show saved memories"},
	{"remember", "Save a memory entry"},
	{"skills", "List saved skills"},
	{"commands", "List custom commands"},
	{"models", "Show current model info"},
	{"session", "Show session info or list sessions"},
	{"rewind", "List or restore checkpoints"},
	{"share", "Export session to a shareable file"},
	{"exit", "Exit Aegis"},
}

// commandsNeedingArgs are completed with a trailing space so the user can
// immediately type arguments rather than running the bare command.
// "persona" is intentionally absent: Tab/Enter on /persona dispatches it
// immediately, which opens the interactive picker.
var commandsNeedingArgs = map[string]bool{
	"mode":     true,
	"remember": true,
	"help":     true,
	"session":  true,
	"rewind":   true,
	"share":    true,
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

// completionKind distinguishes slash-command completion from @file mention
// completion; they share the popup but differ in matching and acceptance.
type completionKind int

const (
	compSlash completionKind = iota
	compFile
)

// completionState tracks the inline completion popup (slash commands or @file
// mentions).
type completionState struct {
	active     bool
	kind       completionKind
	items      []cmdEntry
	selected   int
	tokenStart int // byte index where the replaced token begins (for @file)
}

const maxFileMatches = 50

// refTypes are the non-file reference kinds offered in the @ popup. @image:
// attaches an image (the daemon reads it); the others are textual references the
// agent resolves with its tools (LSP diagnostics, web fetch, symbol search).
var refTypes = []cmdEntry{
	{name: "image:", desc: "Attach an image file (vision models)"},
	{name: "diagnostics", desc: "Pull current LSP diagnostics"},
	{name: "url:", desc: "Reference a URL to fetch"},
	{name: "symbol:", desc: "Reference a code symbol to locate"},
}

// matchRefs returns ref-type entries whose keyword starts with query.
func matchRefs(query string) []cmdEntry {
	q := strings.ToLower(query)
	var out []cmdEntry
	for _, r := range refTypes {
		if q == "" || strings.HasPrefix(r.name, q) {
			out = append(out, r)
		}
	}
	return out
}

// computeCompletion derives popup state from the current textarea value.
//
//   - Slash command: active while the value is a single "/token" with no space.
//   - @file mention: active while the final whitespace-delimited token starts
//     with "@" and is still being typed.
func computeCompletion(value string, all []cmdEntry, files []string) completionState {
	if strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \t\n") {
		return slashCompletion(value, all)
	}
	if start := atTokenStart(value); start >= 0 {
		query := value[start+1:]
		// Once the token carries a "ref:" value (e.g. @image:path), the user is
		// typing the value freely — close the popup and don't suggest.
		if !strings.ContainsAny(query, " \t\n") && !strings.Contains(query, ":") {
			// Ref-type kinds (@image:, @diagnostics, …) lead, then file matches.
			items := append(matchRefs(query), matchFiles(files, query)...)
			if len(items) > 0 {
				return completionState{active: true, kind: compFile, items: items, tokenStart: start}
			}
		}
	}
	return completionState{}
}

func slashCompletion(value string, all []cmdEntry) completionState {
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
	return completionState{active: true, kind: compSlash, items: matches}
}

// atTokenStart returns the byte index of the "@" beginning the trailing mention
// token (one at the start of the value or preceded by whitespace), or -1.
func atTokenStart(value string) int {
	i := strings.LastIndex(value, "@")
	if i < 0 {
		return -1
	}
	if i > 0 {
		switch value[i-1] {
		case ' ', '\t', '\n':
		default:
			return -1
		}
	}
	return i
}

// matchFiles returns file entries matching query: path-prefix and base-name
// matches first, then substring matches, capped at maxFileMatches.
func matchFiles(files []string, query string) []cmdEntry {
	q := strings.ToLower(query)
	var prefix, substr []cmdEntry
	for _, f := range files {
		lf := strings.ToLower(f)
		switch {
		case q == "" || strings.HasPrefix(lf, q) || strings.HasPrefix(strings.ToLower(filepath.Base(f)), q):
			prefix = append(prefix, cmdEntry{name: f})
		case strings.Contains(lf, q):
			substr = append(substr, cmdEntry{name: f})
		}
		if len(prefix)+len(substr) >= maxFileMatches {
			break
		}
	}
	return append(prefix, substr...)
}

// maxIndexedFiles caps the workspace file index to keep @-completion responsive
// in large repositories.
const maxIndexedFiles = 5000

// buildFileIndex walks root and returns workspace-relative, slash-separated file
// paths, skipping VCS/dependency/build directories and dotfiles dirs.
func buildFileIndex(root string) []string {
	if root == "" {
		return nil
	}
	skip := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, ".aegis": true,
		"dist": true, "build": true, "target": true, ".idea": true, ".vscode": true,
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (skip[name] || (strings.HasPrefix(name, ".") && len(name) > 1)) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= maxIndexedFiles {
			return fs.SkipAll
		}
		return nil
	})
	return out
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
	sep := lipgloss.NewStyle().Foreground(colSeparator).Render(strings.Repeat("─", sepW)) + pageCtx

	// Build rows. Selected item gets a brand-coloured background spanning the
	// full width so the highlight is instantly obvious without any extra
	// marker character. Unselected items are plain text.
	lines := make([]string, 0, completionVisibleRows)
	for i := start; i < end; i++ {
		e := c.items[i]
		selected := i == c.selected

		// @file rows are single-column full-width paths; command rows use a
		// two-column name/description layout.
		if c.kind == compFile {
			label := "@" + e.name
			if selected {
				lines = append(lines, lipgloss.NewStyle().
					Background(colBrandBg).Foreground(colBrandFg).Bold(true).
					Width(width).Render(" "+truncate(label, max(width-2, 4))))
			} else {
				lines = append(lines, lipgloss.NewStyle().
					Foreground(colTextDim).Render("  "+truncate(label, max(width-2, 4))))
			}
			continue
		}

		name := "/" + e.name
		namePad := strings.Repeat(" ", max(nameCol-len(name), 1))
		desc := truncate(e.desc, max(width-nameCol-2, 4))

		var row string
		if selected {
			row = lipgloss.NewStyle().
				Background(colBrandBg).
				Foreground(colBrandFg).
				Bold(true).
				Width(nameCol).
				Render(name) +
				lipgloss.NewStyle().
					Background(colBrandBg).
					Foreground(colTextDim).
					Width(width-nameCol).
					Render(" "+desc)
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
