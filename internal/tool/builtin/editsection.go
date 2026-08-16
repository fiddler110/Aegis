package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/tool"
)

// headingRe matches an ATX markdown heading and captures its level and text.
var headingRe = regexp.MustCompile(`(?m)^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)

// editSectionTool replaces the body of one markdown section, selected by its
// heading rather than by reproducing the text being replaced.
//
// fill_marker removed exact-match editing from the *placeholder* fill loop, and
// the fill phases stopped failing. The phase-6 substance and quality passes then
// hit the identical wall one step later: they revise prose that already exists,
// so they fall back to edit_file, and edit_file wants old_string reproduced byte
// for byte. Measured on qwen3:14b (2026-08-09), a re-opened assessment phase
// spent twelve consecutive edit_file calls failing — ten "old_string not found",
// two "occurs 2 times" — and made no progress at all until the drive reset it.
//
// A document being revised section by section already has unique, stable
// handles: its headings. Naming one and supplying replacement prose asks the
// model for new text only, never for a reproduction. As with fill_marker, a
// wrong selector answers with the headings that do exist, so the retry has the
// answer instead of another guess.
//
// This does not replace edit_file, which remains right for a surgical change
// inside a section (one table row, one number) where rewriting the whole body
// would be wasteful.
type editSectionTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *editSectionTool) Name() string                { return "edit_section" }
func (t *editSectionTool) Capability() tool.Capability { return tool.CapWrite }
func (t *editSectionTool) Description() string {
	return "Replace or extend the body of one markdown section, selected by its `heading` text or its 1-based `index` — no exact-text match required. Call with only `path` to list the file's sections. A section runs to the next same-or-higher heading, so target the deepest heading you actually mean. Pass mode:\"new\" with a `heading` to create a section that does not exist yet. For a file with no markdown headings (source code, config, data), or a surgical change to a single line or table row, use multi_edit instead."
}
func (t *editSectionTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative path to a markdown file"},"heading":{"type":"string","description":"heading text of the section to edit, without the leading #s. Omit both heading and index to list the file's sections instead of editing."},"index":{"type":"integer","description":"1-based position of the section, as reported by listing. Use this when a heading appears more than once."},"content":{"type":"string","description":"replacement body for the section, excluding the heading line itself; required when editing"},"mode":{"type":"string","enum":["replace","append","new"],"description":"replace the section body (default), append to it, or create a new section (\"new\")"},"level":{"type":"integer","description":"heading level 1-6 for mode=new (default 2)"},"after":{"type":"string","description":"for mode=new, insert after this existing section instead of at the end of the file"},"allow_structure_loss":{"type":"boolean","description":"permit a replacement that deletes nested subsections or a markdown table the section currently has; refused by default"}},"required":["path"]}`)
}
func (t *editSectionTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"heading":{"type":"string"}},"required":["path"]}`)
}

// section is one located heading and the span of its body.
type section struct {
	level     int
	text      string
	line      int
	bodyStart int // byte offset just after the heading line
	bodyEnd   int // byte offset of the next same-or-higher heading, or len(content)
}

// findSections locates every ATX heading and the extent of its body. A
// section's body ends at the next heading of the same or higher level, so
// replacing "## Summary" does not disturb a "### Detail" nested beneath a
// later peer.
func findSections(content string) []section {
	locs := headingRe.FindAllStringSubmatchIndex(content, -1)
	out := make([]section, 0, len(locs))
	for _, m := range locs {
		level := m[3] - m[2]
		bodyStart := m[1]
		if bodyStart < len(content) && content[bodyStart] == '\n' {
			bodyStart++
		}
		out = append(out, section{
			level:     level,
			text:      content[m[4]:m[5]],
			line:      1 + strings.Count(content[:m[0]], "\n"),
			bodyStart: bodyStart,
		})
	}
	for i := range out {
		out[i].bodyEnd = len(content)
		for j := i + 1; j < len(out); j++ {
			if out[j].level <= out[i].level {
				// Body ends where the next peer/parent heading begins. Walk back
				// over that heading's own start rather than its body start.
				out[i].bodyEnd = headingStart(content, out[j])
				break
			}
		}
	}
	return out
}

// headingStart recovers the byte offset of a section's heading line.
func headingStart(content string, s section) int {
	idx := s.bodyStart
	if idx > 0 && idx <= len(content) {
		// bodyStart sits just past the heading line's newline; step back to the
		// start of that line.
		cut := strings.LastIndexByte(content[:max(idx-1, 0)], '\n')
		return cut + 1
	}
	return idx
}

func describeSections(path string, sections []section) string {
	if len(sections) == 0 {
		// Name the tool that *does* work here (P62.10). Measured live on
		// qwen3:14b: with edit_file deferred under the local profile, a model
		// asked to fix a two-line bug in a .py file reached for edit_section
		// three times running and got this message three times, tripping the
		// tool-failure breaker before recovering — the error stated what had
		// failed and nothing about what to do instead, which is the P39.16
		// finding (a tool that holds the information the model needs and returns
		// an error without it) in its smallest possible form. multi_edit is
		// named rather than edit_file because it is exposed under both prompt
		// profiles.
		return fmt.Sprintf("%s has no markdown headings, so it has no sections to edit — use multi_edit (or write_file for a whole small file) to change a file without headings", path)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d section(s):\n", path, len(sections))
	// The index is printed because it is a selector, not decoration: a document
	// may repeat a heading, and index is the only way to name the second one.
	for i, s := range sections {
		fmt.Fprintf(&b, "  index %d — %s %q (line %d)\n", i+1, strings.Repeat("#", s.level), s.text, s.line)
	}
	return strings.TrimRight(b.String(), "\n")
}

// selectSection resolves a heading name to exactly one section, preferring an
// exact match and falling back to a unique case-insensitive one.
func selectSection(path string, sections []section, heading string) (int, string) {
	want := strings.TrimSpace(heading)
	var exact, fold []int
	for i, s := range sections {
		switch {
		case s.text == want:
			exact = append(exact, i)
		case strings.EqualFold(s.text, want):
			fold = append(fold, i)
		}
	}
	hits := exact
	if len(hits) == 0 {
		hits = fold
	}
	switch len(hits) {
	case 1:
		return hits[0], ""
	case 0:
		return 0, fmt.Sprintf("no section titled %q.\n%s", heading, describeSections(path, sections))
	default:
		// Naming the indices matters more than naming the problem. The earlier
		// wording ("rename or disambiguate them") asked for something the model
		// cannot do — a report's headings are fixed by what the verifier expects
		// — so it invented "Executive Summary 1" / "Executive Summary 2" and
		// looped (qwen3:14b, 2026-08-09). An ambiguous selector has to resolve
		// to a choice the caller can actually make.
		idx := make([]string, 0, len(hits))
		for _, h := range hits {
			idx = append(idx, fmt.Sprintf("%d", h+1))
		}
		return 0, fmt.Sprintf("%q matches %d sections — pass index %s instead of heading.\n%s",
			heading, len(hits), strings.Join(idx, " or "), describeSections(path, sections))
	}
}

func (t *editSectionTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path    string  `json:"path"`
		Heading *string `json:"heading"`
		Index   *int    `json:"index"`
		Content *string `json:"content"`
		Mode    string  `json:"mode"`
		Level   int     `json:"level"`
		After   *string `json:"after"`

		AllowStructureLoss bool `json:"allow_structure_loss"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	abs, err := resolveWrite(ctx, t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	content, errMsg := readTextForEdit(abs, args.Path, effectiveRoot(ctx, t.root))
	if errMsg != "" {
		return tool.Result{Content: errMsg, IsError: true}, nil
	}
	sections := findSections(content)

	if args.Heading == nil && args.Index == nil {
		return tool.Result{Content: describeSections(args.Path, sections)}, nil
	}
	if args.Content == nil {
		return tool.Result{Content: "content is required when editing a section (omit heading to list sections instead)", IsError: true}, nil
	}
	// mode=new creates a section that does not exist yet. Without it a fill
	// phase can only ever rewrite what the scaffold produced: fill_marker needs
	// a marker, edit_section needs an existing heading, and write_file is
	// withheld from fill phases on purpose. Live (qwen3:14b, 2026-08-09) that
	// left the model unable to close component-name-consistency at all — the
	// fix is authoring eleven new component sections — so it rewrote the one
	// section it could reach, over and over, until the drive reset it.
	if args.Mode == "new" {
		return t.createSection(ctx, abs, args.Path, content, sections, *args.Heading, args.Level, args.After, *args.Content)
	}
	if len(sections) == 0 {
		return tool.Result{Content: describeSections(args.Path, sections), IsError: true}, nil
	}
	var idx int
	if args.Index != nil {
		i := *args.Index - 1
		if i < 0 || i >= len(sections) {
			return tool.Result{Content: fmt.Sprintf("index %d is out of range.\n%s", *args.Index, describeSections(args.Path, sections)), IsError: true}, nil
		}
		idx = i
	} else {
		var errMsg string
		idx, errMsg = selectSection(args.Path, sections, *args.Heading)
		if errMsg != "" {
			return tool.Result{Content: errMsg, IsError: true}, nil
		}
	}
	if args.Mode != "" && args.Mode != "replace" && args.Mode != "append" && args.Mode != "new" {
		return tool.Result{Content: fmt.Sprintf("unknown mode %q: use \"replace\", \"append\" or \"new\"", args.Mode), IsError: true}, nil
	}

	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	sec := sections[idx]
	body := *args.Content
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	// A replacement that silently drops a table is the tool's own worst failure
	// mode, and it happened on the first live run: replacing the "Action
	// Summary" section with prose deleted the Tier|Threats|Findings table the
	// verifier requires, turning a section edit into a structural regression
	// nothing downstream could attribute (qwen3:14b, 2026-08-09). Losing a
	// table is occasionally intended, so this refuses rather than forbids —
	// allow_structure_loss carries the intent explicitly.
	if args.Mode != "append" && !args.AllowStructureLoss {
		oldBody := content[sec.bodyStart:sec.bodyEnd]
		// Nested subsections are the more dangerous loss, because nothing in
		// the call hints they exist. A section's body runs to the next
		// same-or-higher heading, so replacing a `## Summary` that has `###`
		// children takes all of them. Live (qwen3:14b, 2026-08-09): replacing
		// one section's summary table deleted every component subsection under
		// it — 4409 bytes to 1481 — and reported plain success.
		if nested := nestedHeadings(oldBody, sec.level); len(nested) > 0 && len(nestedHeadings(body, sec.level)) == 0 {
			return tool.Result{Content: fmt.Sprintf(
				"refusing: the %q section contains %d nested subsection(s) (%s) that your replacement would delete. Target the subsection you mean directly, include them in `content`, or pass allow_structure_loss:true if removing them is intended.",
				sec.text, len(nested), strings.Join(nested, ", ")), IsError: true}, nil
		}
		if lost := tableRowsLost(oldBody, body); lost > 0 {
			return tool.Result{Content: fmt.Sprintf(
				"refusing: the %q section contains a markdown table (%d row(s)) and your replacement has none. Include the table in `content`, use edit_file to change individual rows, or pass allow_structure_loss:true if removing it is intended.",
				sec.text, lost), IsError: true}, nil
		}
	}
	var updated string
	if args.Mode == "append" {
		existing := strings.TrimRight(content[sec.bodyStart:sec.bodyEnd], "\n")
		if existing != "" {
			existing += "\n"
		}
		updated = content[:sec.bodyStart] + existing + body + content[sec.bodyEnd:]
	} else {
		updated = content[:sec.bodyStart] + body + content[sec.bodyEnd:]
	}

	checkpoint.SnapshotterFrom(ctx).Capture(abs)
	if err := writePreservingMode(abs, []byte(updated)); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
		t.tracker.RecordAgentWrite(abs, content, updated)
	}
	verb := "replaced"
	if args.Mode == "append" {
		verb = "extended"
	}
	return tool.Result{Content: fmt.Sprintf("%s the %q section of %s (%d bytes)", verb, sec.text, args.Path, len(body))}, nil
}

// tableRowRe matches a markdown table row: a line whose first non-space
// character is a pipe. Separator rows (|---|---|) count too — a table needs
// them, so losing one is losing the table.
var tableRowRe = regexp.MustCompile(`(?m)^[ \t]*\|.*\|[ \t]*$`)

// tableRowsLost reports how many table rows the old body had when the new one
// has none at all. It deliberately does not compare counts: shrinking a table
// is ordinary editing, while replacing a table with prose that contains no
// table at all is the structural regression worth stopping.
func tableRowsLost(oldBody, newBody string) int {
	oldRows := len(tableRowRe.FindAllString(oldBody, -1))
	if oldRows == 0 {
		return 0
	}
	if len(tableRowRe.FindAllString(newBody, -1)) > 0 {
		return 0
	}
	return oldRows
}

// nestedHeadings returns the heading texts inside body that sit deeper than
// parentLevel — the subsections a replacement of that body would delete.
// Capped, since the message names them and a long list helps nobody.
func nestedHeadings(body string, parentLevel int) []string {
	var out []string
	for _, m := range headingRe.FindAllStringSubmatch(body, -1) {
		if len(m[1]) > parentLevel {
			out = append(out, m[2])
			if len(out) == 5 {
				out = append(out, "…")
				break
			}
		}
	}
	return out
}

// createSection inserts a brand-new section, at the end of the file or right
// after a named existing one. Refusing a duplicate heading matters as much as
// creating the section: two sections with the same title are exactly what makes
// a later edit ambiguous, and the model that needs mode=new is the one least
// able to recover from that.
func (t *editSectionTool) createSection(ctx context.Context, abs, display, content string, sections []section, heading string, level int, after *string, body string) (tool.Result, error) {
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return tool.Result{Content: "heading is required when creating a section", IsError: true}, nil
	}
	if level <= 0 {
		level = 2
	}
	if level > 6 {
		return tool.Result{Content: fmt.Sprintf("level %d is out of range: markdown headings are 1-6", level), IsError: true}, nil
	}
	for _, s := range sections {
		if strings.EqualFold(s.text, heading) {
			return tool.Result{Content: fmt.Sprintf("a section titled %q already exists (line %d) — edit it instead of creating a second one.\n%s", s.text, s.line, describeSections(display, sections)), IsError: true}, nil
		}
	}

	block := strings.Repeat("#", level) + " " + heading + "\n" + body
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}

	var updated string
	if after == nil || strings.TrimSpace(*after) == "" {
		prefix := content
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		if prefix != "" && !strings.HasSuffix(prefix, "\n\n") {
			prefix += "\n"
		}
		updated = prefix + block
	} else {
		idx, errMsg := selectSection(display, sections, *after)
		if errMsg != "" {
			return tool.Result{Content: errMsg, IsError: true}, nil
		}
		at := sections[idx].bodyEnd
		sep := ""
		if at > 0 && !strings.HasSuffix(content[:at], "\n\n") {
			sep = "\n"
		}
		updated = content[:at] + sep + block + content[at:]
	}

	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	checkpoint.SnapshotterFrom(ctx).Capture(abs)
	if err := writePreservingMode(abs, []byte(updated)); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
		t.tracker.RecordAgentWrite(abs, content, updated)
	}
	return tool.Result{Content: fmt.Sprintf("created section %q in %s (%d bytes)", heading, display, len(block))}, nil
}
