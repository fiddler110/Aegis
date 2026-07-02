package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFrontmatter(t *testing.T) {
	sk := parseSkill("file", "---\nname: deploy\ndescription: Ship the app\n---\n\nStep 1. build\n")
	if sk.Name != "deploy" {
		t.Errorf("name = %q, want deploy", sk.Name)
	}
	if sk.Description != "Ship the app" {
		t.Errorf("description = %q", sk.Description)
	}
	if strings.Contains(sk.Content, "---") || !strings.Contains(sk.Content, "Step 1") {
		t.Errorf("body not stripped: %q", sk.Content)
	}
}

func TestBuildIndexProgressiveDisclosure(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, ".aegis", "skills")
	writeSkill(t, sd, "described.md", "---\ndescription: Does a thing\n---\nfull body here\n")
	writeSkill(t, sd, "legacy.md", "no frontmatter, eager inject\n")

	idx := BuildIndex(dir)
	if !strings.Contains(idx, "<skills_available>") {
		t.Errorf("expected skills_available block, got:\n%s", idx)
	}
	if !strings.Contains(idx, "described: Does a thing") {
		t.Errorf("described skill missing from index:\n%s", idx)
	}
	if strings.Contains(idx, "full body here") {
		t.Errorf("described skill body should NOT be eager-injected:\n%s", idx)
	}
	if !strings.Contains(idx, "eager inject") {
		t.Errorf("legacy skill should be eager-injected:\n%s", idx)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, ".aegis", "skills")
	writeSkill(t, sd, "deploy.md", "---\ndescription: Ship\n---\nrun make deploy\n")

	sk, ok := Load(dir, "deploy")
	if !ok {
		t.Fatal("skill not found")
	}
	if !strings.Contains(sk.Content, "run make deploy") {
		t.Errorf("wrong body: %q", sk.Content)
	}
	if _, ok := Load(dir, "missing"); ok {
		t.Error("expected missing skill to not load")
	}
}
