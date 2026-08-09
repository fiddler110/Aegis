package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const markerDoc = `# Report

## Summary
<!-- PENDING: summary -->

## Findings
<!-- PENDING: findings -->

## Notes
<!-- PENDING -->
`

func TestFillMarkerByIndexAndKey(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "r.md")
	if err := os.WriteFile(path, []byte(markerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fillMarkerTool{root: root}
	ctx := context.Background()

	// By key.
	res, err := f.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "key": "summary", "content": "All good."}))
	if err != nil || res.IsError {
		t.Fatalf("fill by key failed: err=%v res=%q", err, res.Content)
	}
	// Filling renumbers what follows, so a success must hand back the fresh
	// listing — otherwise the model's next index is stale by construction.
	if !strings.Contains(res.Content, "2 remaining") || !strings.Contains(res.Content, "shifted") {
		t.Errorf("expected the refreshed listing after a fill, got %q", res.Content)
	}
	if !strings.Contains(res.Content, `index 1 — key "findings"`) {
		t.Errorf("refreshed listing should renumber findings to 1, got %q", res.Content)
	}

	// By index: after the first fill, findings is marker 1.
	res, err = f.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "index": 1, "content": "Nothing critical."}))
	if err != nil || res.IsError {
		t.Fatalf("fill by index failed: err=%v res=%q", err, res.Content)
	}
	if !strings.Contains(res.Content, "1 remaining") {
		t.Errorf("expected one marker left, got %q", res.Content)
	}

	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "All good.") || !strings.Contains(s, "Nothing critical.") {
		t.Errorf("content not substituted: %q", s)
	}
	if strings.Contains(s, "PENDING: summary") || strings.Contains(s, "PENDING: findings") {
		t.Errorf("marker survived the fill: %q", s)
	}
	// The keyless marker is untouched — a fill targets one marker, never the file.
	if !strings.Contains(s, "<!-- PENDING -->") {
		t.Errorf("unrelated marker was disturbed: %q", s)
	}
}

// Listing is the tool's answer to "which marker do I name?", and must not
// count as a write.
func TestFillMarkerListsWhenNoSelector(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.md"), []byte(markerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&fillMarkerTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "r.md"}))
	if err != nil || res.IsError {
		t.Fatalf("listing failed: err=%v res=%q", err, res.Content)
	}
	for _, want := range []string{"3 remaining", `index 1 — key "summary"`, `index 2 — key "findings"`, "index 3 — (no key"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("listing missing %q:\n%s", want, res.Content)
		}
	}
}

// A bad selector must hand back the answer, not another guess — this is the
// whole reason the tool beats an exact-match edit for a weak model.
func TestFillMarkerBadSelectorEnumerates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.md"), []byte(markerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fillMarkerTool{root: root}
	ctx := context.Background()

	res, _ := f.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "index": 99, "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "out of range") || !strings.Contains(res.Content, "key \"summary\"") {
		t.Errorf("out-of-range error should enumerate markers, got %q", res.Content)
	}

	res, _ = f.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "key": "nope", "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "no marker with key") || !strings.Contains(res.Content, "index 1") {
		t.Errorf("unknown-key error should enumerate markers, got %q", res.Content)
	}

	// An index/key pair that disagrees means the listing is stale; filling the
	// index anyway would write into the wrong section.
	res, _ = f.Execute(ctx, mustJSON(t, map[string]any{"path": "r.md", "index": 1, "key": "findings", "content": "x"}))
	if !res.IsError || !strings.Contains(res.Content, "stale") {
		t.Errorf("mismatched index/key should be refused, got %q", res.Content)
	}

	if got, _ := os.ReadFile(filepath.Join(root, "r.md")); string(got) != markerDoc {
		t.Error("a failed selection modified the file")
	}
}

func TestFillMarkerRequiresContentWhenFilling(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.md"), []byte(markerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := (&fillMarkerTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "r.md", "index": 1}))
	if !res.IsError || !strings.Contains(res.Content, "content is required") {
		t.Errorf("expected a content-required error, got %q", res.Content)
	}
}

// Empty content is a legitimate fill (a section that is genuinely empty), and
// must not be confused with "no content supplied".
func TestFillMarkerAcceptsEmptyContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.md"), []byte(markerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&fillMarkerTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "r.md", "key": "summary", "content": ""}))
	if err != nil || res.IsError {
		t.Fatalf("empty content should fill: err=%v res=%q", err, res.Content)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "r.md")); strings.Contains(string(got), "PENDING: summary") {
		t.Error("marker survived an empty fill")
	}
}

// The last fill says so plainly rather than printing an empty listing — the
// drive's done-condition is "no markers remain", so that sentence is the one
// the model most needs to read correctly.
func TestFillMarkerFinalFillReportsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.md"), []byte("x\n<!-- PENDING: only -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := (&fillMarkerTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "one.md", "index": 1, "content": "done"}))
	if err != nil || res.IsError {
		t.Fatalf("fill failed: err=%v res=%q", err, res.Content)
	}
	if !strings.Contains(res.Content, "No markers remain") {
		t.Errorf("expected an explicit empty statement, got %q", res.Content)
	}
}
