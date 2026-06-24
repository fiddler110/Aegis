package agentdef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBuiltins(t *testing.T) {
	ClearCustom()
	for _, name := range []string{"general", "explore", "plan", "build"} {
		def, ok := Resolve(name)
		if !ok {
			t.Errorf("expected builtin %q to resolve", name)
		}
		if def.Name != name {
			t.Errorf("Resolve(%q).Name = %q", name, def.Name)
		}
	}
}

func TestResolveDefaultForEmpty(t *testing.T) {
	ClearCustom()
	def, ok := Resolve("")
	if !ok {
		t.Error("empty name should resolve to default")
	}
	if def.Name != DefaultType {
		t.Errorf("expected %q, got %q", DefaultType, def.Name)
	}
}

func TestResolveUnknownFallsBack(t *testing.T) {
	ClearCustom()
	_, ok := Resolve("nonexistent")
	if ok {
		t.Error("unknown name should return ok=false")
	}
}

func TestRegisterCustomOverrides(t *testing.T) {
	ClearCustom()
	defer ClearCustom()

	Register(Definition{
		Name:         "custom-agent",
		Description:  "A custom agent",
		SystemPrompt: "You are custom.",
		Mode:         "plan",
	})

	def, ok := Resolve("custom-agent")
	if !ok {
		t.Fatal("custom agent should resolve")
	}
	if def.SystemPrompt != "You are custom." {
		t.Errorf("unexpected prompt: %q", def.SystemPrompt)
	}
	if def.Mode != "plan" {
		t.Errorf("unexpected mode: %q", def.Mode)
	}

	// Custom should appear in Names.
	names := Names()
	found := false
	for _, n := range names {
		if n == "custom-agent" {
			found = true
		}
	}
	if !found {
		t.Error("custom-agent should appear in Names()")
	}
}

func TestRegisterCustomOverridesBuiltin(t *testing.T) {
	ClearCustom()
	defer ClearCustom()

	Register(Definition{
		Name:         "explore",
		Description:  "Overridden explore",
		SystemPrompt: "Custom explore prompt.",
		Mode:         "build",
	})

	def, ok := Resolve("explore")
	if !ok {
		t.Fatal("overridden explore should resolve")
	}
	if def.SystemPrompt != "Custom explore prompt." {
		t.Error("custom should override builtin")
	}
}

func TestLoadFromDirs(t *testing.T) {
	ClearCustom()
	defer ClearCustom()

	dir := filepath.Join(t.TempDir(), "agents")
	os.MkdirAll(dir, 0o755)

	os.WriteFile(filepath.Join(dir, "researcher.md"), []byte(`---
name: researcher
description: Deep research agent
mode: plan
---
You are a deep research agent. Investigate thoroughly.
`), 0o644)

	os.WriteFile(filepath.Join(dir, "coder.md"), []byte(`---
name: coder
description: Coding agent
mode: build
tools: [read_file, write_file, edit_file, shell]
---
You are a coding agent.
`), 0o644)

	n := LoadFromDirs(dir)
	if n != 2 {
		t.Fatalf("expected 2 loaded, got %d", n)
	}

	def, ok := Resolve("researcher")
	if !ok {
		t.Fatal("researcher should resolve")
	}
	if def.Mode != "plan" {
		t.Errorf("expected plan mode, got %q", def.Mode)
	}
	if def.SystemPrompt == "" {
		t.Error("expected non-empty system prompt")
	}

	def, ok = Resolve("coder")
	if !ok {
		t.Fatal("coder should resolve")
	}
	if len(def.Tools) != 4 {
		t.Errorf("expected 4 tools, got %d: %v", len(def.Tools), def.Tools)
	}
}

func TestLoadFromDirsNoFrontmatter(t *testing.T) {
	ClearCustom()
	defer ClearCustom()

	dir := filepath.Join(t.TempDir(), "agents")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("no frontmatter"), 0o644)

	n := LoadFromDirs(dir)
	if n != 0 {
		t.Errorf("expected 0 loaded for bad file, got %d", n)
	}
}

func TestLoadFromDirsFallbackName(t *testing.T) {
	ClearCustom()
	defer ClearCustom()

	dir := filepath.Join(t.TempDir(), "agents")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "my-agent.md"), []byte("---\nmode: plan\n---\nprompt\n"), 0o644)

	LoadFromDirs(dir)
	_, ok := Resolve("my-agent")
	if !ok {
		t.Error("expected fallback name from filename")
	}
}

func TestDiscoverDirs(t *testing.T) {
	dirs := DiscoverDirs("/data", "/project")
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
}
