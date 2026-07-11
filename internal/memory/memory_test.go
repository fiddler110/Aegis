package memory

import (
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
