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

// TestBrakemanRelevantGatesOnRailsApp mirrors the hadolint/kubescape cases
// for brakeman (P34.6). The stakes are higher here: without the gate brakeman
// doesn't report zero findings on a non-Rails repo, it exits 4 and the scan
// reports an error that looks like a broken tool.
func TestBrakemanRelevantGatesOnRailsApp(t *testing.T) {
	write := func(dir, rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("# ruby\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	if ok, reason := (brakemanScanner{}).Relevant(dir); ok || reason == "" {
		t.Errorf("Relevant() = %v, %q, want false with a reason for an empty dir", ok, reason)
	}

	// A Ruby project that isn't a Rails app: brakeman refuses these too.
	write(dir, "Rakefile")
	write(dir, "lib/thing.rb")
	if ok, reason := (brakemanScanner{}).Relevant(dir); ok || reason == "" {
		t.Errorf("Relevant() = %v, %q, want false for a non-Rails Ruby project", ok, reason)
	}

	// config/environment.rb alone isn't enough — brakeman wants a
	// config/application.rb or a Rakefile alongside it.
	bare := t.TempDir()
	write(bare, "config/environment.rb")
	if ok, reason := (brakemanScanner{}).Relevant(bare); ok || reason == "" {
		t.Errorf("Relevant() = %v, %q, want false for config/environment.rb with no application.rb or Rakefile", ok, reason)
	}

	write(dir, "config/environment.rb") // dir already has a Rakefile
	if ok, _ := (brakemanScanner{}).Relevant(dir); !ok {
		t.Error("Relevant() = false, want true for config/environment.rb + Rakefile")
	}

	rails3 := t.TempDir()
	write(rails3, "config/environment.rb")
	write(rails3, "config/application.rb")
	if ok, _ := (brakemanScanner{}).Relevant(rails3); !ok {
		t.Error("Relevant() = false, want true for config/environment.rb + config/application.rb")
	}
}

// TestTrivyScanArgsIncludeDevDeps pins P34.10's scope decision. trivy's
// default skips npm/yarn/gradle dev dependencies, which on this repo's own
// frontend lockfile means cataloging 1 of 140 packages. The decision is not
// "more findings is better" — it's that osv-scanner already includes dev
// deps, so without this flag the two SCA scanners disagree about what the
// scan covers and trivy contributes nothing to the ecosystem osv is already
// reporting on alone.
func TestTrivyScanArgsIncludeDevDeps(t *testing.T) {
	var hasDevDeps, hasScanners bool
	for _, a := range trivyScanArgs {
		switch a {
		case "--include-dev-deps":
			hasDevDeps = true
		case "vuln,secret,misconfig":
			hasScanners = true
		}
	}
	if !hasDevDeps {
		t.Error("trivyScanArgs must pass --include-dev-deps: without it trivy silently skips npm dev dependencies that osv-scanner does scan (P34.10)")
	}
	if !hasScanners {
		t.Error("trivyScanArgs lost its explicit scanner list (P11.6)")
	}
}
