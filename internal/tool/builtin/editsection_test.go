package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sectionDoc = `# Report

## Summary
Old summary text.

### Detail
Nested detail.

## Findings
| ID | Threat |
|----|--------|
| T1 | x |

## Notes
Trailing notes.
`

// Replacing a section must take the body up to the next same-or-higher
// heading, leaving nested subsections beneath it intact and later peers alone.
func TestEditSectionReplacesBody(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "r.md")
	if err := os.WriteFile(path, []byte(sectionDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&editSectionTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "r.md", "heading": "Findings", "content": "No findings after review.", "allow_structure_loss": true,
	}))
	if err != nil || res.IsError {
		t.Fatalf("edit_section failed: err=%v res=%q", err, res.Content)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "No findings after review.") {
		t.Errorf("replacement not applied: %q", s)
	}
	if strings.Contains(s, "| T1 | x |") {
		t.Errorf("old body survived: %q", s)
	}
	for _, keep := range []string{"## Summary", "Old summary text.", "### Detail", "## Notes", "Trailing notes."} {
		if !strings.Contains(s, keep) {
			t.Errorf("edit disturbed %q:\n%s", keep, s)
		}
	}
}

// A parent section owns its nested subsections, so replacing it takes them
// too — but only when the caller says so. The span semantics are what make
// this destructive by default, which is why allow_structure_loss gates it (see
// TestEditSectionRefusesSilentSubsectionLoss); this test pins the span itself.
func TestEditSectionReplacesNestedSubsections(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "r.md")
	if err := os.WriteFile(path, []byte(sectionDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&editSectionTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "r.md", "heading": "Summary", "content": "Rewritten.", "allow_structure_loss": true,
	})); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if strings.Contains(string(s), "### Detail") {
		t.Errorf("nested subsection should have been replaced with its parent:\n%s", s)
	}
	if !strings.Contains(string(s), "## Findings") {
		t.Errorf("replacement ran past the next peer heading:\n%s", s)
	}
}

func TestEditSectionAppendMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "r.md")
	if err := os.WriteFile(path, []byte(sectionDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&editSectionTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "r.md", "heading": "Notes", "content": "Added line.", "mode": "append",
	})); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if !strings.Contains(string(s), "Trailing notes.") || !strings.Contains(string(s), "Added line.") {
		t.Errorf("append should keep the old body and add to it:\n%s", s)
	}
}

// Listing and failed selection both answer with the headings that exist — the
// property that makes this usable by a model that guessed wrong.
func TestEditSectionListsAndEnumeratesOnMiss(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.md"), []byte(sectionDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &editSectionTool{root: root}
	ctx := context.Background()

	res, err := e.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md"}))
	if err != nil || res.IsError {
		t.Fatalf("listing failed: %v %q", err, res.Content)
	}
	for _, want := range []string{`"Summary"`, `"Findings"`, `"Detail"`, "5 section(s)", "index 1 —"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("listing missing %s:\n%s", want, res.Content)
		}
	}

	res, _ = e.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "heading": "Nope", "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "no section titled") || !strings.Contains(res.Content, `"Summary"`) {
		t.Errorf("miss should enumerate real headings: %q", res.Content)
	}

	// Case-insensitive match is accepted when unambiguous — a model that
	// lowercases a heading should not be stopped by it.
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "heading": "findings", "content": "y", "allow_structure_loss": true})); err != nil || res.IsError {
		t.Errorf("case-insensitive heading should resolve: %v %q", err, res.Content)
	}
}

func TestEditSectionRejectsBadMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.md"), []byte(sectionDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := (&editSectionTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "r.md", "heading": "Notes", "content": "x", "mode": "prepend",
	}))
	if !res.IsError || !strings.Contains(res.Content, "unknown mode") {
		t.Errorf("expected an unknown-mode error, got %q", res.Content)
	}
}

// A repeated heading must stay selectable. The first version of this tool
// answered an ambiguous heading with "rename or disambiguate them" — advice the
// caller cannot act on, since a report's headings are fixed by what the
// verifier expects. A real model responded by inventing "Executive Summary 1"
// and "Executive Summary 2" and looping until the drive reset it.
func TestEditSectionDuplicateHeadingSelectableByIndex(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "d.md")
	doc := "## Executive Summary\nFirst.\n\n## Middle\nx\n\n## Executive Summary\nSecond.\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &editSectionTool{root: root}
	ctx := context.Background()

	res, _ := e.Execute(ctx, mustJSON(t, map[string]any{"path": "d.md", "heading": "Executive Summary", "content": "x"}))
	if !res.IsError {
		t.Fatal("an ambiguous heading should not silently pick one")
	}
	if !strings.Contains(res.Content, "pass index 1 or 3") {
		t.Errorf("ambiguity error must name the usable indices, got %q", res.Content)
	}
	if strings.Contains(res.Content, "rename") {
		t.Errorf("error should not ask for something the caller cannot do: %q", res.Content)
	}

	// Selecting the second one by index works and leaves the first alone.
	res, err := e.Execute(ctx, mustJSON(t, map[string]any{"path": "d.md", "index": 3, "content": "Rewritten second."}))
	if err != nil || res.IsError {
		t.Fatalf("index selection failed: %v %q", err, res.Content)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "First.") {
		t.Errorf("first duplicate was disturbed:\n%s", got)
	}
	if !strings.Contains(string(got), "Rewritten second.") || strings.Contains(string(got), "Second.") {
		t.Errorf("second duplicate not replaced:\n%s", got)
	}

	res, _ = e.Execute(ctx, mustJSON(t, map[string]any{"path": "d.md", "index": 99, "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "out of range") {
		t.Errorf("expected an out-of-range error naming the sections, got %q", res.Content)
	}
}

// Replacing a section that holds a table with prose that holds none is the
// tool's worst failure: it turns a section edit into a structural regression
// the verifier later reports as a missing table with no hint of what removed
// it. Live on qwen3:14b (2026-08-09) this deleted the assessment's required
// Tier|Threats|Findings table.
func TestEditSectionRefusesSilentTableLoss(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.md")
	doc := "## Action Summary\n| Tier | Threats | Findings |\n|------|---------|----------|\n| 1 | 3 | 2 |\n\n## Next\nx\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &editSectionTool{root: root}
	ctx := context.Background()

	res, _ := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Action Summary", "content": "Some prose with no table.",
	}))
	if !res.IsError {
		t.Fatal("replacing a table with prose should be refused")
	}
	for _, want := range []string{"markdown table", "allow_structure_loss"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("refusal should mention %q, got %q", want, res.Content)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != doc {
		t.Error("a refused edit modified the file")
	}

	// A replacement that keeps a table is fine.
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Action Summary",
		"content": "| Tier | Threats | Findings |\n|------|---------|----------|\n| 1 | 9 | 9 |",
	})); err != nil || res.IsError {
		t.Errorf("table-preserving replacement refused: %v %q", err, res.Content)
	}

	// Explicit intent overrides.
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Action Summary", "content": "Deliberately prose.", "allow_structure_loss": true,
	})); err != nil || res.IsError {
		t.Errorf("allow_structure_loss should permit the removal: %v %q", err, res.Content)
	}

	// Append never loses anything, so it is never gated.
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Action Summary", "content": "note", "mode": "append",
	})); err != nil || res.IsError {
		t.Errorf("append should not be gated: %v %q", err, res.Content)
	}
}

// A section with no table is unaffected by the guard.
func TestEditSectionTableGuardIgnoresProseSections(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "p.md"), []byte("## Notes\nplain prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&editSectionTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "p.md", "heading": "Notes", "content": "different prose",
	}))
	if err != nil || res.IsError {
		t.Errorf("prose-only replacement should not be gated: %v %q", err, res.Content)
	}
}

// Deleting nested subsections is the more dangerous structure loss, because
// nothing in the call hints they exist: a section's body runs to the next
// same-or-higher heading, so replacing a parent takes every child. Live on
// qwen3:14b (2026-08-09) this removed every component subsection from the
// analysis file — 4409 bytes to 1481 — and reported success.
func TestEditSectionRefusesSilentSubsectionLoss(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.md")
	doc := "## Summary\nintro\n\n### AppState\nbody one\n\n### GatewayDB\nbody two\n\n## Next\nx\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &editSectionTool{root: root}
	ctx := context.Background()

	res, _ := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Summary", "content": "just a new intro",
	}))
	if !res.IsError {
		t.Fatal("replacing a parent section should not silently delete its children")
	}
	for _, want := range []string{"nested subsection", "AppState", "GatewayDB", "allow_structure_loss"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("refusal should mention %q, got %q", want, res.Content)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != doc {
		t.Error("a refused edit modified the file")
	}

	// Targeting the child directly is the intended route and stays allowed.
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "AppState", "content": "rewritten",
	})); err != nil || res.IsError {
		t.Errorf("editing the subsection itself should work: %v %q", err, res.Content)
	}

	// A replacement that carries the subsections forward is fine.
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Summary",
		"content": "new intro\n\n### AppState\nkept\n\n### GatewayDB\nkept\n",
	})); err != nil || res.IsError {
		t.Errorf("structure-preserving replacement refused: %v %q", err, res.Content)
	}

	// Explicit intent still overrides.
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "heading": "Summary", "content": "flattened", "allow_structure_loss": true,
	})); err != nil || res.IsError {
		t.Errorf("allow_structure_loss should permit it: %v %q", err, res.Content)
	}
}

// mode=new is what lets a fill phase author a section that the scaffold never
// created. Without it the phase is stuck: fill_marker needs a marker,
// edit_section needs an existing heading, and write_file is withheld from fill
// phases — so a model asked to add eleven component sections rewrote the one
// section it could reach, repeatedly, until the drive reset it.
func TestEditSectionCreatesNewSection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.md")
	if err := os.WriteFile(path, []byte("# Title\n\n## Summary\nintro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &editSectionTool{root: root}
	ctx := context.Background()

	res, err := e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "mode": "new", "heading": "AppState", "content": "Threats for AppState.",
	}))
	if err != nil || res.IsError {
		t.Fatalf("creating a section failed: %v %q", err, res.Content)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "## AppState\nThreats for AppState.") {
		t.Errorf("new section not appended correctly:\n%s", got)
	}
	if !strings.Contains(string(got), "## Summary\nintro") {
		t.Errorf("existing content disturbed:\n%s", got)
	}

	// A duplicate heading is refused — two same-named sections are exactly what
	// makes a later edit ambiguous.
	res, _ = e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "mode": "new", "heading": "AppState", "content": "again",
	}))
	if !res.IsError || !strings.Contains(res.Content, "already exists") {
		t.Errorf("duplicate creation should be refused, got %q", res.Content)
	}

	// Positional insertion, and a custom level.
	res, err = e.Execute(ctx, mustJSON(t, map[string]any{
		"path": "a.md", "mode": "new", "heading": "Tier 1", "level": 3,
		"content": "none", "after": "Summary",
	}))
	if err != nil || res.IsError {
		t.Fatalf("insert-after failed: %v %q", err, res.Content)
	}
	s := string(mustRead(t, path))
	if !strings.Contains(s, "### Tier 1") {
		t.Errorf("level not honored:\n%s", s)
	}
	if strings.Index(s, "### Tier 1") > strings.Index(s, "## AppState") {
		t.Errorf("section was not inserted after Summary:\n%s", s)
	}

	// Guardrails on the inputs themselves.
	res, _ = e.Execute(ctx, mustJSON(t, map[string]any{"path": "a.md", "mode": "new", "heading": "", "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "heading is required") {
		t.Errorf("blank heading should be refused, got %q", res.Content)
	}
	res, _ = e.Execute(ctx, mustJSON(t, map[string]any{"path": "a.md", "mode": "new", "heading": "Deep", "level": 9, "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "out of range") {
		t.Errorf("level 9 should be refused, got %q", res.Content)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A file with no headings is the one case where this tool cannot help at all,
// and the message it returns is the model's only clue about what to do next.
// Measured live (P62.10, qwen3:14b, `aegis chat` with edit_file deferred under
// the local profile): a model asked to fix a two-line bug in a .py file called
// edit_section three times running and got "temps.py has no markdown headings"
// three times, tripping the tool-failure breaker before it recovered. The name
// of a tool that *does* apply has to be in the error, and it has to be one that
// is exposed under both prompt profiles — which edit_file is not.
func TestEditSectionOnHeadinglessFileNamesTheToolThatWorks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "temps.py"), []byte("total = 0\ncount = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&editSectionTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "temps.py",
	}))
	if err != nil {
		t.Fatalf("edit_section listing failed: %v", err)
	}
	if !strings.Contains(res.Content, "multi_edit") {
		t.Errorf("the no-headings message does not name a tool that works on this file: %q", res.Content)
	}
	// edit_file is deferred under the local profile, so pointing at it here
	// costs the model a tool_search turn at exactly the moment it is already
	// failing. The tool's Description carries the same rule.
	if strings.Contains(res.Content, "edit_file") {
		t.Errorf("the no-headings message points at a tool the local profile defers: %q", res.Content)
	}
	if desc := (&editSectionTool{root: root}).Description(); !strings.Contains(desc, "multi_edit") {
		t.Errorf("edit_section's description does not name the fallback for a headingless file: %q", desc)
	}
}
