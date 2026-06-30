package repomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildExtractsGoSymbols(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

import "fmt"

type Server struct {
	addr string
}

func Run(addr string) error {
	fmt.Println(addr)
	return nil
}

func helperUnexported() {}
`)
	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	render := m.Render()
	if !strings.Contains(render, "main.go") {
		t.Errorf("render missing file path:\n%s", render)
	}
	if !strings.Contains(render, "type Server struct") {
		t.Errorf("render missing type symbol:\n%s", render)
	}
	if !strings.Contains(render, "func Run(addr string) error") {
		t.Errorf("render missing func symbol:\n%s", render)
	}
}

func TestBuildExtractsPythonSymbols(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.py", `import os

class Widget:
    def method(self):
        pass

def top_level(x):
    return x
`)
	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	render := m.Render()
	if !strings.Contains(render, "class Widget") {
		t.Errorf("render missing class:\n%s", render)
	}
	if !strings.Contains(render, "def top_level(x)") {
		t.Errorf("render missing def:\n%s", render)
	}
}

func TestBuildIgnoresVendorAndHidden(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "keep.go", "package main\nfunc Keep() {}\n")
	writeFile(t, dir, "node_modules/dep/index.js", "function Dep() {}\n")
	writeFile(t, dir, ".git/config", "func ShouldNotAppear() {}\n")
	writeFile(t, dir, "vendor/x/y.go", "package x\nfunc Vendored() {}\n")

	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	render := m.Render()
	if !strings.Contains(render, "keep.go") {
		t.Errorf("expected keep.go in render:\n%s", render)
	}
	for _, bad := range []string{"node_modules", "Dep", "ShouldNotAppear", "vendor", "Vendored"} {
		if strings.Contains(render, bad) {
			t.Errorf("render should not contain %q:\n%s", bad, render)
		}
	}
}

func TestRenderCappedAtMaxBytes(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package main\n")
	for i := 0; i < 500; i++ {
		sb.WriteString("func F")
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteString("() {}\n")
	}
	writeFile(t, dir, "big.go", sb.String())

	m, err := Build(dir, Options{MaxBytes: 500})
	if err != nil {
		t.Fatal(err)
	}
	render := m.Render()
	if len(render) > 700 { // 500 cap + small truncation notice margin
		t.Errorf("render exceeded cap: %d bytes", len(render))
	}
	if !strings.Contains(render, "truncated") {
		t.Errorf("expected truncation notice when capped:\n%s", render)
	}
}

func TestFingerprintChangesWhenFileChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\nfunc A() {}\n")
	m1, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Modify the file's content (and bump mtime).
	writeFile(t, dir, "a.go", "package main\nfunc A() {}\nfunc B() {}\n")
	m2, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m1.Fingerprint == m2.Fingerprint {
		t.Error("fingerprint should change when a file changes")
	}
}

func TestSaveLoadFreshness(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, ".aegis", "repomap.json")
	writeFile(t, dir, "a.go", "package main\nfunc A() {}\n")

	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Save(cache); err != nil {
		t.Fatal(err)
	}

	render, fresh, err := Load(dir, cache, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !fresh {
		t.Error("freshly built cache should report fresh")
	}
	if !strings.Contains(render, "func A()") {
		t.Errorf("loaded render missing symbol:\n%s", render)
	}

	// Change a file; the cache should now be reported stale.
	writeFile(t, dir, "a.go", "package main\nfunc A() {}\nfunc C() {}\n")
	_, fresh, err = Load(dir, cache, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Error("cache should be stale after a file change")
	}
}

func TestLoadMissingCache(t *testing.T) {
	dir := t.TempDir()
	render, fresh, err := Load(dir, filepath.Join(dir, "nope.json"), Options{})
	if err != nil {
		t.Fatalf("missing cache should not error: %v", err)
	}
	if fresh || render != "" {
		t.Errorf("missing cache should return empty/not-fresh, got fresh=%v render=%q", fresh, render)
	}
}

func TestBlockWrapsWhenNonEmpty(t *testing.T) {
	if Block("") != "" {
		t.Error("Block(\"\") should be empty")
	}
	b := Block("path.go\n  func X()")
	if !strings.Contains(b, "<repo_map>") || !strings.Contains(b, "</repo_map>") {
		t.Errorf("Block should wrap in repo_map tags: %q", b)
	}
}
