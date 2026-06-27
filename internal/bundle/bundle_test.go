package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ManifestName), "name: sec-pack\nversion: 1.2.0\ndescription: security tools\nauthor: tester\n")
	writeFile(t, filepath.Join(dir, "commands", "review.md"), "Review the diff")
	writeFile(t, filepath.Join(dir, "agents", "threat.md"), "Threat modeler")
	writeFile(t, filepath.Join(dir, "skills", "nmap", "skill.md"), "nmap skill")
	return dir
}

func TestLoad(t *testing.T) {
	b, err := Load(makeBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	if b.Manifest.Name != "sec-pack" || b.Manifest.Version != "1.2.0" {
		t.Errorf("manifest = %+v", b.Manifest)
	}
	if len(b.Artifacts) != 3 {
		t.Fatalf("artifacts = %+v", b.Artifacts)
	}
	// Sorted by kind then path: agents, commands, skills.
	if b.Artifacts[0].Kind != "agents" || b.Artifacts[2].Kind != "skills" {
		t.Errorf("unexpected order: %+v", b.Artifacts)
	}
	if b.Artifacts[2].Rel != "nmap/skill.md" {
		t.Errorf("nested skill rel = %q", b.Artifacts[2].Rel)
	}
}

func TestLoadErrors(t *testing.T) {
	// Missing manifest.
	empty := t.TempDir()
	if _, err := Load(empty); err == nil {
		t.Error("expected error for missing manifest")
	}
	// Manifest but no artifacts.
	noArt := t.TempDir()
	writeFile(t, filepath.Join(noArt, ManifestName), "name: x\n")
	if _, err := Load(noArt); err == nil {
		t.Error("expected error for no artifacts")
	}
	// Missing name.
	noName := t.TempDir()
	writeFile(t, filepath.Join(noName, ManifestName), "version: 1\n")
	writeFile(t, filepath.Join(noName, "commands", "a.md"), "x")
	if _, err := Load(noName); err == nil {
		t.Error("expected error for missing name")
	}
}

func TestInstall(t *testing.T) {
	b, err := Load(makeBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	written, err := b.Install(dest, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 3 {
		t.Fatalf("written = %v", written)
	}
	if _, err := os.Stat(filepath.Join(dest, "commands", "review.md")); err != nil {
		t.Errorf("command not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skills", "nmap", "skill.md")); err != nil {
		t.Errorf("nested skill not installed: %v", err)
	}

	// Re-install without overwrite skips everything.
	written, err = b.Install(dest, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Errorf("expected no rewrites, got %v", written)
	}

	// With overwrite, all are rewritten.
	written, err = b.Install(dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 3 {
		t.Errorf("overwrite wrote %v", written)
	}
}
