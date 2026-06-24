package memory

import (
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
}

func TestSaveSkillAndLoad(t *testing.T) {
	s := Sources{ProjectRoot: t.TempDir(), DataDir: t.TempDir()}
	path, err := s.SaveSkill("Deploy Steps", "1. build\n2. ship")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "deploy-steps.md" {
		t.Errorf("skill filename = %q, want deploy-steps.md", filepath.Base(path))
	}
	got := s.Load()
	if !strings.Contains(got, "Skills") || !strings.Contains(got, "deploy-steps") || !strings.Contains(got, "1. build") {
		t.Errorf("skill not loaded: %q", got)
	}
}
