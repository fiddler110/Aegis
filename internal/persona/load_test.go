package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePersona(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Restore the package-global loaded set after the test so test personas
	// do not leak into other tests that iterate Names().
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		delete(loaded, stem)
		for i, n := range loadedOrder {
			if n == stem {
				loadedOrder = append(loadedOrder[:i], loadedOrder[i+1:]...)
				break
			}
		}
		refreshSig = ""
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
	if !p.Loaded {
		t.Error("expected Loaded=true for a persona parsed from a file (P7.5)")
	}
	if !strings.Contains(p.System, "You are a strict secure code reviewer.") {
		t.Errorf("system = %q", p.System)
	}
	if !strings.Contains(p.System, "persona_untrusted_content") || !strings.Contains(p.System, "untrusted data") {
		t.Errorf("expected file-loaded persona body to be wrapped in an untrusted-provenance marker, got %q", p.System)
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

func TestRefreshPicksUpAddUpdateDelete(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		mu.Lock()
		loaded = map[string]Persona{}
		loadedOrder = nil
		refreshSig = ""
		mu.Unlock()
	})

	// Add.
	path := filepath.Join(dir, "hotswap.md")
	if err := os.WriteFile(path, []byte("---\ndescription: v1\n---\nFirst body."), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, changed := Refresh(dir); n != 1 || !changed {
		t.Fatalf("after add: n=%d changed=%v", n, changed)
	}
	p, ok := Get("hotswap")
	if !ok || !strings.Contains(p.System, "First body.") {
		t.Fatalf("persona after add: ok=%v system=%q", ok, p.System)
	}

	// Unchanged directory short-circuits.
	if _, changed := Refresh(dir); changed {
		t.Error("Refresh reported a rebuild for an unchanged directory")
	}

	// Update. Backdate then rewrite so the mtime signature always differs even
	// on coarse filesystem clocks.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, changed := Refresh(dir); !changed {
		t.Fatal("Refresh missed an mtime change")
	}
	if err := os.WriteFile(path, []byte("---\ndescription: v2\n---\nSecond body."), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed := Refresh(dir); !changed {
		t.Fatal("Refresh missed a file update")
	}
	if p, _ := Get("hotswap"); !strings.Contains(p.System, "Second body.") {
		t.Errorf("persona not updated: system=%q", p.System)
	}

	// Delete.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if n, changed := Refresh(dir); n != 0 || !changed {
		t.Fatalf("after delete: n=%d changed=%v", n, changed)
	}
	if _, ok := Get("hotswap"); ok {
		t.Error("deleted persona still resolvable")
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
