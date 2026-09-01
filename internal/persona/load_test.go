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

	n := LoadFromDirs("", false, dir)
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

// TestLoadFromDirsToolsEnforced is P81.20/FIND-20 item 4: a persona file can
// opt its Tools list into enforcing mode via tools_enforced, and — like
// mode/tools/rules/output_guard — the field is a frontmatter control field
// gated by honorControlFields (P27.7/FIND-09), so an untrusted project dir's
// persona cannot silently turn on a hard containment boundary any more than
// it can silently change Mode.
func TestLoadFromDirsToolsEnforced(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "locked-down.md", `---
description: A persona that treats its tool list as a hard boundary
tools: [read_file, grep]
tools_enforced: true
---
Body.`)

	// Trusted load (honorControlFields=true): the field takes effect.
	n := LoadFromDirs("", true, dir)
	if n != 1 {
		t.Fatalf("expected 1 persona loaded, got %d", n)
	}
	p, ok := Get("locked-down")
	if !ok {
		t.Fatal("persona not registered")
	}
	if !p.ToolsEnforced {
		t.Error("expected ToolsEnforced=true for a trusted load of tools_enforced: true")
	}

	// Untrusted load (honorControlFields=false): every control field,
	// ToolsEnforced included, is dropped — the same rule Mode/Tools/Rules
	// already follow.
	t.Cleanup(func() {
		mu.Lock()
		delete(loaded, "locked-down")
		for i, name := range loadedOrder {
			if name == "locked-down" {
				loadedOrder = append(loadedOrder[:i], loadedOrder[i+1:]...)
				break
			}
		}
		refreshSig = ""
		mu.Unlock()
	})
	LoadFromDirs(dir, false, dir)
	p2, ok := Get("locked-down")
	if !ok {
		t.Fatal("persona not registered (untrusted load)")
	}
	if p2.ToolsEnforced {
		t.Error("untrusted project dir: ToolsEnforced should be dropped (control fields disabled)")
	}
	if len(p2.Tools) != 0 {
		t.Errorf("untrusted project dir: Tools should also be dropped, got %v", p2.Tools)
	}
}

func TestLoadGuardDisabledScalar(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "fast.md", "---\noutput_guard: none\n---\nBody.")
	LoadFromDirs("", false, dir)
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
	if n, changed := Refresh("", false, dir); n != 1 || !changed {
		t.Fatalf("after add: n=%d changed=%v", n, changed)
	}
	p, ok := Get("hotswap")
	if !ok || !strings.Contains(p.System, "First body.") {
		t.Fatalf("persona after add: ok=%v system=%q", ok, p.System)
	}

	// Unchanged directory short-circuits.
	if _, changed := Refresh("", false, dir); changed {
		t.Error("Refresh reported a rebuild for an unchanged directory")
	}

	// Update. Backdate then rewrite so the mtime signature always differs even
	// on coarse filesystem clocks.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, changed := Refresh("", false, dir); !changed {
		t.Fatal("Refresh missed an mtime change")
	}
	if err := os.WriteFile(path, []byte("---\ndescription: v2\n---\nSecond body."), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed := Refresh("", false, dir); !changed {
		t.Fatal("Refresh missed a file update")
	}
	if p, _ := Get("hotswap"); !strings.Contains(p.System, "Second body.") {
		t.Errorf("persona not updated: system=%q", p.System)
	}

	// Delete.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if n, changed := Refresh("", false, dir); n != 0 || !changed {
		t.Fatalf("after delete: n=%d changed=%v", n, changed)
	}
	if _, ok := Get("hotswap"); ok {
		t.Error("deleted persona still resolvable")
	}
}

func TestLoadNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "my-helper.md", "---\ndescription: x\n---\nBody.")
	LoadFromDirs("", false, dir)
	if _, ok := Get("my-helper"); !ok {
		t.Error("persona should be registered under its filename stem")
	}
}

// TestGetForRootDoesNotMutateSharedState is the P25.9 regression: GetForRoot
// must let a session on a foreign root see that root's own project persona
// without ever touching the shared loaded/loadedOrder/refreshSig state
// Refresh manages — a naive per-session Refresh call would instead evict
// whatever the daemon's own project (or a concurrent session's root) just
// loaded, since Refresh atomically replaces the whole set.
func TestGetForRootDoesNotMutateSharedState(t *testing.T) {
	// Seed the shared state as if the daemon's own project had already
	// loaded a persona via the normal Refresh path.
	daemonDir := t.TempDir()
	writePersona(t, daemonDir, "daemon-persona.md", "---\ndescription: daemon\n---\nDaemon body.")
	if _, changed := Refresh("", false, daemonDir); !changed {
		t.Fatal("Refresh did not pick up the daemon persona")
	}
	namesBefore := append([]string{}, Names()...)
	sigBefore := refreshSig

	// A foreign root (a session Workdir different from the daemon's own)
	// with its own project persona of a different name.
	foreignRoot := t.TempDir()
	writePersona(t, ProjectDir(foreignRoot), "foreign-persona.md", "---\ndescription: foreign\n---\nForeign body.")

	p, ok := GetForRoot(foreignRoot, true, "foreign-persona")
	if !ok {
		t.Fatal("expected foreign-persona to resolve via GetForRoot")
	}
	if !strings.Contains(p.System, "Foreign body.") {
		t.Errorf("got system %q, want it to contain the foreign persona's body", p.System)
	}

	// The shared state must be byte-for-byte unchanged: no eviction, no
	// leakage of the foreign persona into Names()/Get().
	if refreshSig != sigBefore {
		t.Error("GetForRoot must not touch refreshSig")
	}
	if got := Names(); strings.Join(got, ",") != strings.Join(namesBefore, ",") {
		t.Errorf("Names() changed: before=%v after=%v", namesBefore, got)
	}
	if _, ok := Get("foreign-persona"); ok {
		t.Error("foreign-persona must not leak into the shared Get() lookup")
	}
	if p, ok := Get("daemon-persona"); !ok || !strings.Contains(p.System, "Daemon body.") {
		t.Error("daemon's own persona must still resolve unchanged via Get()")
	}

	// A name not present in the foreign root's project dir falls through to
	// the shared set (still sees the daemon's own project persona).
	p2, ok := GetForRoot(foreignRoot, true, "daemon-persona")
	if !ok || !strings.Contains(p2.System, "Daemon body.") {
		t.Error("GetForRoot should fall through to Get for names not in the foreign project dir")
	}

	// Untrusted foreign project dir: control fields are dropped, matching
	// LoadFromDirs' honorControlFields semantics for an untrusted project dir.
	writePersona(t, ProjectDir(foreignRoot), "foreign-mode.md", "---\ndescription: x\nmode: build\n---\nBody.")
	p3, ok := GetForRoot(foreignRoot, false, "foreign-mode")
	if !ok {
		t.Fatal("expected foreign-mode to resolve")
	}
	if p3.Mode != "" {
		t.Errorf("untrusted foreign project dir: mode = %q, want empty (control fields dropped)", p3.Mode)
	}
}
