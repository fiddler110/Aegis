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

// TestHadolintRelevantGatesOnDockerfilePresence is the "don't run hadolint
// when there's no Dockerfile" case: RelevanceChecker must say no on a repo
// with none, yes on one with a Dockerfile.
func TestHadolintRelevantGatesOnDockerfilePresence(t *testing.T) {
	dir := t.TempDir()
	if ok, reason := (hadolintScanner{}).Relevant(dir); ok || reason == "" {
		t.Errorf("Relevant() = %v, %q, want false with a reason for an empty dir", ok, reason)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := (hadolintScanner{}).Relevant(dir); !ok {
		t.Error("Relevant() = false, want true once a Dockerfile exists")
	}
}

func TestFindK8sManifests(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy.yaml", "apiVersion: apps/v1\nkind: Deployment\n")
	write("docker-compose.yaml", "version: \"3\"\nservices:\n  web:\n    image: nginx\n") // not a k8s manifest
	write("charts/app/Chart.yaml", "name: app\nversion: 0.1.0\n")                         // marker by filename, no content check
	write("README.md", "hello")

	got, err := findK8sManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 manifests (deploy.yaml, charts/app/Chart.yaml)", got)
	}
}

// TestKubescapeRelevantGatesOnManifestPresence mirrors
// TestHadolintRelevantGatesOnDockerfilePresence for kubescape.
func TestKubescapeRelevantGatesOnManifestPresence(t *testing.T) {
	dir := t.TempDir()
	if ok, reason := (kubescapeScanner{}).Relevant(dir); ok || reason == "" {
		t.Errorf("Relevant() = %v, %q, want false with a reason for an empty dir", ok, reason)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy.yaml"), []byte("apiVersion: v1\nkind: Pod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := (kubescapeScanner{}).Relevant(dir); !ok {
		t.Error("Relevant() = false, want true once a manifest exists")
	}
}
