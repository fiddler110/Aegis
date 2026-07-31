package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func newYAMLFixture(t *testing.T, name, content string) (*yamlValidateTool, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return &yamlValidateTool{root: root}, root
}

func TestYAMLValidateIdentity(t *testing.T) {
	yt := &yamlValidateTool{root: t.TempDir()}
	if yt.Name() != "yaml_validate" {
		t.Errorf("Name() = %q, want yaml_validate", yt.Name())
	}
	if yt.Capability() != tool.CapRead {
		t.Errorf("Capability() = %q, want read", yt.Capability())
	}
}

// TestYAMLValidateOutlinesTopLevelKeys is the "structural probe" contract from
// P52.9: a valid file must come back with its top-level keys, their value
// kinds and their line numbers, so the model can use the tool *before* editing.
func TestYAMLValidateOutlinesTopLevelKeys(t *testing.T) {
	yt, _ := newYAMLFixture(t, "deck.yaml", `title: Threat Model
version: 2
authors:
  - alice
  - bob
theme:
  font: serif
  color: navy
notes:
`)
	res, err := yt.Execute(context.Background(), mustJSON(t, map[string]any{"path": "deck.yaml"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("valid YAML reported as error: %s", res.Content)
	}
	for _, want := range []string{
		"valid YAML: deck.yaml",
		"5 top-level key(s)",
		"title: scalar (line 1)",
		"authors: list[2] (line 3)",
		"theme: map{2} (line 6)",
		"notes: empty (line 9)",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("outline missing %q; got:\n%s", want, res.Content)
		}
	}
}

// TestYAMLValidateReportsParseLocation covers the failure path: the parse error
// must carry the line it happened on plus an excerpt marking that line, which
// is the whole reason the tool beats "edit blind and wait for the consumer to
// fail".
func TestYAMLValidateReportsParseLocation(t *testing.T) {
	yt, _ := newYAMLFixture(t, "broken.yaml", "a: 1\nb:\n  c: d: e\nf: 2\n")
	res, err := yt.Execute(context.Background(), mustJSON(t, map[string]any{"path": "broken.yaml"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Fatalf("malformed YAML not reported as an error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "INVALID YAML: broken.yaml") {
		t.Errorf("missing verdict header; got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "line 3") {
		t.Errorf("parse error does not name the failing line 3; got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "mapping values are not allowed") {
		t.Errorf("parse error text not propagated; got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, ">    3 |   c: d: e") {
		t.Errorf("excerpt does not mark the failing source line; got:\n%s", res.Content)
	}
}

// TestYAMLValidateCatchesSecondDocument guards the decoder loop: yaml.Unmarshal
// silently ignores everything after the first `---`, so a validator built on it
// would call this file valid.
func TestYAMLValidateCatchesSecondDocument(t *testing.T) {
	yt, _ := newYAMLFixture(t, "multi.yaml", "a: 1\n---\nb:\n  c: d: e\n")
	res, _ := yt.Execute(context.Background(), mustJSON(t, map[string]any{"path": "multi.yaml"}))
	if !res.IsError {
		t.Fatalf("broken second document reported as valid: %s", res.Content)
	}
	if !strings.Contains(res.Content, "line 4") {
		t.Errorf("wrong line for second-document failure; got:\n%s", res.Content)
	}
}

func TestYAMLValidateRejectsPathOutsideRoot(t *testing.T) {
	yt, root := newYAMLFixture(t, "ok.yaml", "a: 1\n")
	// A real YAML file exists outside the workspace, so a rejection cannot be
	// mistaken for "file not found".
	outside := filepath.Join(filepath.Dir(root), "outside-"+filepath.Base(root)+".yaml")
	if err := os.WriteFile(outside, []byte("secret: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	for _, p := range []string{"../" + filepath.Base(outside), outside} {
		res, err := yt.Execute(context.Background(), mustJSON(t, map[string]any{"path": p}))
		if err == nil && !res.IsError {
			t.Errorf("path %q outside the workspace root was accepted: %+v", p, res)
		}
		if err == nil && strings.Contains(res.Content, "secret") {
			t.Errorf("path %q leaked out-of-workspace content: %s", p, res.Content)
		}
	}
}

// TestYAMLValidateHonorsSessionWorkdir pins the tool to the same per-session
// root override every other file builtin uses (P25.1), rather than only its
// construction-time root.
func TestYAMLValidateHonorsSessionWorkdir(t *testing.T) {
	yt, _ := newYAMLFixture(t, "a.yaml", "a: 1\n")
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "b.yaml"), []byte("b:\n  - 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithWorkdir(context.Background(), other)
	res, err := yt.Execute(ctx, mustJSON(t, map[string]any{"path": "b.yaml"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError || !strings.Contains(res.Content, "b: list[1] (line 1)") {
		t.Errorf("session workdir not honored; got: %+v", res)
	}
}
