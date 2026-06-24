package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommandFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.md")
	os.WriteFile(path, []byte(`---
name: review
description: Review a file for issues
args: [file, focus]
---
Please review {{file}} with a focus on {{focus}}.
`), 0o644)

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cmd.Name != "review" {
		t.Errorf("expected name 'review', got %q", cmd.Name)
	}
	if cmd.Description != "Review a file for issues" {
		t.Errorf("unexpected description: %q", cmd.Description)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "file" || cmd.Args[1] != "focus" {
		t.Errorf("unexpected args: %v", cmd.Args)
	}
	if cmd.Body == "" {
		t.Error("expected non-empty body")
	}
}

func TestParseCommandFileFallbackName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-cmd.md")
	os.WriteFile(path, []byte("---\ndescription: test\n---\nbody here\n"), 0o644)

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "my-cmd" {
		t.Errorf("expected fallback name 'my-cmd', got %q", cmd.Name)
	}
}

func TestParseCommandFileNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	os.WriteFile(path, []byte("no frontmatter here"), 0o644)

	_, err := parseCommandFile(path)
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestExpand(t *testing.T) {
	cmd := &Command{
		Args: []string{"file", "query"},
		Body: "Search {{file}} for {{query}} and report.",
	}
	expanded := cmd.Expand(map[string]string{"file": "main.go", "query": "TODO"})
	if expanded != "Search main.go for TODO and report." {
		t.Errorf("unexpected expansion: %q", expanded)
	}
}

func TestDiscover(t *testing.T) {
	global := filepath.Join(t.TempDir(), "commands")
	project := filepath.Join(t.TempDir(), "commands")
	os.MkdirAll(global, 0o755)
	os.MkdirAll(project, 0o755)

	os.WriteFile(filepath.Join(global, "global-cmd.md"), []byte("---\nname: global\n---\nglobal body\n"), 0o644)
	os.WriteFile(filepath.Join(project, "project-cmd.md"), []byte("---\nname: project\n---\nproject body\n"), 0o644)
	// Override: project overrides global.
	os.WriteFile(filepath.Join(project, "global-cmd.md"), []byte("---\nname: global\n---\noverridden body\n"), 0o644)

	reg := Discover(global, project)
	cmds := reg.List()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	g, ok := reg.Get("global")
	if !ok {
		t.Fatal("expected to find 'global' command")
	}
	if g.Body != "overridden body" {
		t.Errorf("expected project override, got %q", g.Body)
	}

	_, ok = reg.Get("project")
	if !ok {
		t.Error("expected to find 'project' command")
	}
}

func TestDiscoverEmptyDir(t *testing.T) {
	reg := Discover("/nonexistent/dir")
	if len(reg.List()) != 0 {
		t.Error("expected 0 commands from nonexistent dir")
	}
}

func TestCommandDirs(t *testing.T) {
	dirs := CommandDirs("/data", "/project")
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
}
