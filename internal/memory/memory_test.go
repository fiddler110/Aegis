package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if got := s.Load(); got != "" {
		t.Errorf("expected empty memory, got %q", got)
	}
}

func TestAppendAndLoad(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if err := Append(s.ProjectMemoryPath(), "prefers Go over Python"); err != nil {
		t.Fatal(err)
	}
	if err := Append(s.GlobalMemoryPath(), "name is Scott"); err != nil {
		t.Fatal(err)
	}
	got := s.Load()
	if !strings.Contains(got, "prefers Go over Python") {
		t.Errorf("project memory missing: %q", got)
	}
	if !strings.Contains(got, "name is Scott") {
		t.Errorf("user memory missing: %q", got)
	}
	if !strings.Contains(got, "Project memory") || !strings.Contains(got, "User memory") {
		t.Errorf("section headers missing: %q", got)
	}
	if strings.Contains(got, integrityWarning) {
		t.Errorf("freshly-Append()ed memory should round-trip with no integrity warning: %q", got)
	}
}

// TestLoadWrapsUntrustedProvenance verifies both project and user/global
// memory.md content are wrapped in the same untrusted-provenance marker
// used for file-loaded personas/skills/context files (FIND-05/P24.4,
// P27.6). The persona/skill precedent doesn't distinguish project from
// user/global provenance — only compiled-in built-ins are exempt — so both
// memory scopes are expected to carry the marker here too.
func TestLoadWrapsUntrustedProvenance(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	if err := Append(s.ProjectMemoryPath(), "prefers Go over Python"); err != nil {
		t.Fatal(err)
	}
	if err := Append(s.GlobalMemoryPath(), "name is Scott"); err != nil {
		t.Fatal(err)
	}
	got := s.Load()

	if !strings.Contains(got, "memory_untrusted_content") {
		t.Errorf("expected memory content to carry the untrusted-provenance marker, got %q", got)
	}
	if !strings.Contains(got, "untrusted data") {
		t.Errorf("expected the untrusted-provenance framing text, got %q", got)
	}
	if !strings.Contains(got, `scope="project"`) || !strings.Contains(got, `scope="user"`) {
		t.Errorf("expected both project and user scopes to be wrapped, got %q", got)
	}
	if !strings.Contains(got, "prefers Go over Python") || !strings.Contains(got, "name is Scott") {
		t.Errorf("expected original memory content to still be present verbatim, got %q", got)
	}
}

// TestAppendPrunesOldestEntriesWhenOverCap verifies Append enforces
// maxMemoryFileSize by dropping the oldest entries (FIFO) once the file
// would grow past it, rather than letting memory.md grow unbounded (P32.8).
func TestAppendPrunesOldestEntriesWhenOverCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")

	// Fill well past the cap with distinguishable entries.
	entrySize := 200
	count := (maxMemoryFileSize / entrySize) + 20
	for i := range count {
		text := fmt.Sprintf("entry-%05d ", i) + strings.Repeat("x", entrySize)
		if err := Append(path, text); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxMemoryFileSize {
		t.Errorf("file size %d exceeds cap %d after pruning", len(data), maxMemoryFileSize)
	}

	got := string(data)
	lastText := fmt.Sprintf("entry-%05d ", count-1)
	if !strings.Contains(got, lastText) {
		t.Errorf("most recent entry was pruned, expected it to survive: %q missing", lastText)
	}
	firstText := "entry-00000 "
	if strings.Contains(got, firstText) {
		t.Errorf("oldest entry should have been pruned (FIFO), still present")
	}
}

// TestSaveSkillAndLoad checks that SaveSkill writes the file where
// internal/skills expects to find it (that package, not this one, is
// responsible for loading and injecting skill content with progressive
// disclosure — see skills.Discover/BuildIndex).
func TestSaveSkillAndLoad(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	path, err := s.SaveSkill("Deploy Steps", "1. build\n2. ship")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "deploy-steps.md" {
		t.Errorf("skill filename = %q, want deploy-steps.md", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "1. build") {
		t.Errorf("skill content wrong: %q", data)
	}
	if got := s.Load(); strings.Contains(got, "1. build") {
		t.Errorf("Load() should no longer eager-inject skill content (that's internal/skills' job): %q", got)
	}
}
