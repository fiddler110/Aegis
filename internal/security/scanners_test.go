package security

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestFindDockerfiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("FROM scratch\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile")
	write("Dockerfile.prod")
	write("services/api/Dockerfile")
	write("images/base.dockerfile")
	write("README.md")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(".git/Dockerfile") // must be skipped: inside .git

	got, err := findDockerfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"Dockerfile", "Dockerfile.prod", "images/base.dockerfile", "services/api/Dockerfile"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestFindDockerfilesNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findDockerfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected no Dockerfiles, got %v", got)
	}
}
