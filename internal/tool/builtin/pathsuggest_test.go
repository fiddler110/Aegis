package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A not-found error must carry the answer. Reproduces the live stall: a drive
// asked for the run-dir file by bare name against the workspace root, 40 times,
// and gave up because nothing in the error said where the file was.
func TestReadNotFoundSuggestsRealPath(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".aegis", "security", "threat-model", "run-1")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "2-stride-analysis.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&readTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "2-stride-analysis.md"}))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a not-found error")
	}
	if !strings.Contains(res.Content, "did you mean") {
		t.Errorf("error carried no suggestion: %q", res.Content)
	}
	if !strings.Contains(res.Content, ".aegis/security/threat-model/run-1/2-stride-analysis.md") {
		t.Errorf("suggestion should name the real path, got %q", res.Content)
	}
}

// fill_marker resolves paths the same way and must get the same help.
func TestFillMarkerNotFoundSuggestsRealPath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "out")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "report.md"), []byte("<!-- PENDING -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := (&fillMarkerTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "report.md"}))
	if !res.IsError || !strings.Contains(res.Content, "out/report.md") {
		t.Errorf("expected a suggestion naming out/report.md, got %q", res.Content)
	}
}

// No match means no noise: the error stays exactly what it was.
func TestNoSuggestionWhenNothingMatches(t *testing.T) {
	root := t.TempDir()
	res, _ := (&readTool{root: root}).Execute(context.Background(), mustJSON(t, map[string]any{"path": "nowhere.md"}))
	if !res.IsError {
		t.Fatal("expected a not-found error")
	}
	if strings.Contains(res.Content, "did you mean") {
		t.Errorf("invented a suggestion: %q", res.Content)
	}
}

// The search backends skip .aegis; this hint must not, since a drive's whole
// output suite lives there.
func TestSuggestionSearchesInsideAegis(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, ".aegis", "builtin-skills", "threat-modeling")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "recon.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hint := suggestPathHint(root, "recon.py"); !strings.Contains(hint, "recon.py") {
		t.Errorf("hint should reach inside .aegis, got %q", hint)
	}
	// Genuinely noisy trees stay skipped.
	nm := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(nm, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hint := suggestPathHint(root, "index.js"); hint != "" {
		t.Errorf("should not suggest from node_modules, got %q", hint)
	}
}

// A missing file must not read as an empty one. looksBinary reports size 0 for
// anything it cannot open, so the zero-byte branch used to swallow ENOENT and
// answer "is empty (0 bytes)" — which tells a model a file it invented exists
// and is blank, and invites it to fill or overwrite something that was never
// there. An actually-empty file still reports empty.
func TestReadDistinguishesMissingFromEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &readTool{root: root}
	ctx := context.Background()

	res, _ := r.Execute(ctx, mustJSON(t, map[string]any{"path": "missing.md"}))
	if !res.IsError {
		t.Errorf("missing file reported as %q, want an error", res.Content)
	}
	if strings.Contains(res.Content, "is empty") {
		t.Errorf("missing file described as empty: %q", res.Content)
	}

	res, err := r.Execute(ctx, mustJSON(t, map[string]any{"path": "empty.md"}))
	if err != nil || res.IsError {
		t.Fatalf("empty file should read cleanly: err=%v res=%q", err, res.Content)
	}
	if !strings.Contains(res.Content, "is empty (0 bytes)") {
		t.Errorf("empty file = %q, want the empty notice", res.Content)
	}

	res, _ = r.Execute(ctx, mustJSON(t, map[string]any{"path": "."}))
	if !res.IsError || !strings.Contains(res.Content, "is a directory") {
		t.Errorf("directory read = %q, want a directory error", res.Content)
	}
}

// Skill documentation names files as `skeleton-<framework>.md`; a model that
// copies the notation literally must be told what the placeholder is and what
// the real candidates are, on both the read and the write path.
func TestPlaceholderPathSuggestsRealCandidates(t *testing.T) {
	root := t.TempDir()
	skel := filepath.Join(root, ".aegis", "builtin-skills", "threat-modeling", "references", "skeletons")
	if err := os.MkdirAll(skel, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"skeleton-stride.md", "skeleton-linddun.md"} {
		if err := os.WriteFile(filepath.Join(skel, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	hint := suggestPathHint(root, "references/skeletons/skeleton-<framework>.md")
	if !strings.Contains(hint, "placeholder") {
		t.Errorf("hint should name the placeholder: %q", hint)
	}
	if !strings.Contains(hint, "skeleton-stride.md") && !strings.Contains(hint, "skeleton-linddun.md") {
		t.Errorf("hint should offer real candidates: %q", hint)
	}

	// A placeholder matching nothing still explains itself rather than going
	// silent, since the placeholder is the bug regardless.
	if h := suggestPathHint(root, "2-<framework>-analysis.md"); !strings.Contains(h, "placeholder") {
		t.Errorf("unmatched placeholder should still be explained: %q", h)
	}

	// Ordinary names keep the plain behaviour.
	if h := suggestPathHint(root, "skeleton-stride.md"); strings.Contains(h, "placeholder") {
		t.Errorf("real name misreported as a placeholder: %q", h)
	}
}

func TestPlaceholderPatternOnlyMatchesWithinOneSegment(t *testing.T) {
	re := placeholderPattern("2-<framework>-analysis.md")
	if re == nil {
		t.Fatal("expected a pattern")
	}
	if !re.MatchString("2-stride-analysis.md") {
		t.Error("should match a substituted name")
	}
	if re.MatchString("2-a/b-analysis.md") {
		t.Error("a placeholder must not span a path separator")
	}
	if placeholderPattern("plain.md") != nil {
		t.Error("a name with no placeholder should yield no pattern")
	}
}
