package persona

import (
	"strings"
	"testing"
)

// richFrontmatter is a persona body exercising every control field P27.7
// (FIND-09) cares about: mode, tools, rules, and a mapping-form output_guard
// override (not just the scalar "none" disable path already covered by
// TestLoadGuardDisabledScalar / the pre-existing P7.5 checks).
const richFrontmatter = `---
description: Strict secure code reviewer
model: claude-opus-4-8
mode: auto
tools: [read_file, grep, shell]
rules:
  - "deny write(*)"
  - "allow shell(git diff*)"
output_guard:
  mode: llm
  rubric: "Every finding cites file:line."
  max_retries: 2
---
You are a strict secure code reviewer.`

// TestProjectPersonaControlFieldsIgnoredWhenUntrusted covers the core
// P27.7/FIND-09 fix: a persona loaded from the project-sourced directory
// (projectDir) has its control fields dropped when the workspace is not
// trusted, even though the same file would be honored in full from any
// other directory.
func TestProjectPersonaControlFieldsIgnoredWhenUntrusted(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "hostile.md", richFrontmatter)

	n := LoadFromDirs(dir, false, dir)
	if n != 1 {
		t.Fatalf("expected 1 persona loaded, got %d", n)
	}
	p, ok := Get("hostile")
	if !ok {
		t.Fatal("persona not registered")
	}

	if p.Mode != "" {
		t.Errorf("mode = %q, want empty (untrusted project persona)", p.Mode)
	}
	if p.Tools != nil {
		t.Errorf("tools = %v, want nil (untrusted project persona)", p.Tools)
	}
	if p.Rules != nil {
		t.Errorf("rules = %v, want nil (untrusted project persona)", p.Rules)
	}
	if p.Guard != nil {
		t.Errorf("guard = %+v, want nil (untrusted project persona)", p.Guard)
	}

	// Non-control fields are unaffected: description, model, and the
	// (separately, already-wrapped per P24.4) system-prompt body still come
	// through.
	if p.Model != "claude-opus-4-8" {
		t.Errorf("model = %q, want claude-opus-4-8 (not a control field)", p.Model)
	}
	if !strings.Contains(p.System, "You are a strict secure code reviewer.") {
		t.Errorf("system = %q, want body preserved", p.System)
	}
	if !p.Loaded {
		t.Error("expected Loaded=true")
	}
}

// TestProjectPersonaControlFieldsHonoredWhenTrusted covers the flip side:
// once the workspace is trusted, the same project-sourced persona file's
// control fields are applied exactly as before this fix.
func TestProjectPersonaControlFieldsHonoredWhenTrusted(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "trusted-reviewer.md", richFrontmatter)

	n := LoadFromDirs(dir, true, dir)
	if n != 1 {
		t.Fatalf("expected 1 persona loaded, got %d", n)
	}
	p, ok := Get("trusted-reviewer")
	if !ok {
		t.Fatal("persona not registered")
	}

	if p.Mode != "auto" {
		t.Errorf("mode = %q, want auto (trusted project persona)", p.Mode)
	}
	if len(p.Tools) != 3 {
		t.Errorf("tools = %v, want 3 entries (trusted project persona)", p.Tools)
	}
	if len(p.Rules) != 2 {
		t.Errorf("rules = %v, want 2 entries (trusted project persona)", p.Rules)
	}
	if p.Guard == nil || p.Guard.Mode != "llm" || p.Guard.MaxRetries != 2 {
		t.Errorf("guard = %+v, want honored mapping-form override", p.Guard)
	}
}

// TestUserPersonaControlFieldsUnaffectedByProjectTrust covers the
// user/global side of P27.7: a persona loaded from a directory that is not
// the project-sourced directory keeps its control fields regardless of
// whether the (unrelated) project directory is trusted — the operator
// authored it directly, so it is never gated.
func TestUserPersonaControlFieldsUnaffectedByProjectTrust(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir() // distinct directory, never scanned here
	writePersona(t, userDir, "my-reviewer.md", richFrontmatter)

	for _, trusted := range []bool{false, true} {
		n := LoadFromDirs(projectDir, trusted, userDir)
		if n != 1 {
			t.Fatalf("trusted=%v: expected 1 persona loaded, got %d", trusted, n)
		}
		p, ok := Get("my-reviewer")
		if !ok {
			t.Fatalf("trusted=%v: persona not registered", trusted)
		}
		if p.Mode != "auto" {
			t.Errorf("trusted=%v: mode = %q, want auto (user persona always trusted)", trusted, p.Mode)
		}
		if len(p.Tools) != 3 {
			t.Errorf("trusted=%v: tools = %v, want 3 entries", trusted, p.Tools)
		}
		if len(p.Rules) != 2 {
			t.Errorf("trusted=%v: rules = %v, want 2 entries", trusted, p.Rules)
		}
		if p.Guard == nil || p.Guard.Mode != "llm" {
			t.Errorf("trusted=%v: guard = %+v, want honored mapping-form override", trusted, p.Guard)
		}
	}
}

// TestRefreshGatesProjectSourcedControlFields exercises the same scenario
// through Refresh (the hot-reload path servers actually call), not just
// LoadFromDirs, across both a project directory and an unrelated user
// directory scanned together — the realistic DiscoverDirs shape.
func TestRefreshGatesProjectSourcedControlFields(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	writePersona(t, userDir, "user-persona.md", richFrontmatter)
	writePersona(t, projectDir, "project-persona.md", richFrontmatter)

	n, changed := Refresh(projectDir, false, userDir, projectDir)
	if !changed || n != 2 {
		t.Fatalf("n=%d changed=%v, want 2/true", n, changed)
	}

	userP, ok := Get("user-persona")
	if !ok {
		t.Fatal("user-persona not registered")
	}
	if userP.Mode != "auto" || len(userP.Tools) != 3 || len(userP.Rules) != 2 || userP.Guard == nil {
		t.Errorf("user persona control fields unexpectedly stripped: %+v", userP)
	}

	projectP, ok := Get("project-persona")
	if !ok {
		t.Fatal("project-persona not registered")
	}
	if projectP.Mode != "" || projectP.Tools != nil || projectP.Rules != nil || projectP.Guard != nil {
		t.Errorf("untrusted project persona control fields not stripped: %+v", projectP)
	}
}

// TestProjectDirMatchesDiscoverDirsSecondEntry pins the ProjectDir helper to
// what DiscoverDirs already computes for the project-local directory, since
// callers (server.go, cli/persona.go) rely on this to identify which
// directory to gate.
func TestProjectDirMatchesDiscoverDirsSecondEntry(t *testing.T) {
	dirs := DiscoverDirs("/data", "/project")
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
	if got := ProjectDir("/project"); got != dirs[1] {
		t.Errorf("ProjectDir(%q) = %q, want %q (DiscoverDirs' second entry)", "/project", got, dirs[1])
	}
}
