package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePersona(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Restore the package-global registry after the test so loaded test
	// personas do not leak into other tests that iterate Names()/registry.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	t.Cleanup(func() {
		delete(registry, stem)
		for i, n := range nameOrder {
			if n == stem {
				nameOrder = append(nameOrder[:i], nameOrder[i+1:]...)
				break
			}
		}
	})
}

func TestLoadFromDirsRichFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "secure-reviewer.md", `---
description: Strict secure code reviewer
model: claude-opus-4-8
mode: build
tools: [read_file, grep, shell]
rules:
  - "deny write(*)"
  - "allow shell(git diff*)"
output_guard:
  mode: llm
  rubric: "Every finding cites file:line."
  max_retries: 2
---
You are a strict secure code reviewer.`)

	n := LoadFromDirs(dir)
	if n != 1 {
		t.Fatalf("expected 1 persona loaded, got %d", n)
	}
	p, ok := Get("secure-reviewer")
	if !ok {
		t.Fatal("persona not registered")
	}
	if p.Model != "claude-opus-4-8" || p.Mode != "build" {
		t.Errorf("model/mode = %q/%q", p.Model, p.Mode)
	}
	if len(p.Tools) != 3 || len(p.Rules) != 2 {
		t.Errorf("tools=%v rules=%v", p.Tools, p.Rules)
	}
	if p.Guard == nil || p.Guard.Mode != "llm" || p.Guard.Rubric == "" || p.Guard.MaxRetries != 2 {
		t.Errorf("guard = %+v", p.Guard)
	}
	if p.System != "You are a strict secure code reviewer." {
		t.Errorf("system = %q", p.System)
	}
}

func TestLoadGuardDisabledScalar(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "fast.md", "---\noutput_guard: none\n---\nBody.")
	LoadFromDirs(dir)
	p, _ := Get("fast")
	if p.Guard == nil || !p.Guard.Disabled {
		t.Errorf("expected disabled guard, got %+v", p.Guard)
	}
}

func TestLoadNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "my-helper.md", "---\ndescription: x\n---\nBody.")
	LoadFromDirs(dir)
	if _, ok := Get("my-helper"); !ok {
		t.Error("persona should be registered under its filename stem")
	}
}
