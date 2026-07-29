package repomap

import (
	"encoding/json"
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

func TestBuildExtractsGoImportEdges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module github.com/acme/proj\n\ngo 1.22\n")
	writeFile(t, dir, "internal/store/store.go", "package store\nfunc Open() {}\n")
	writeFile(t, dir, "cmd/app/main.go", `package main

import (
	"fmt"
	"github.com/acme/proj/internal/store"
	_ "github.com/acme/proj/internal/driver"
)

func main() { fmt.Println(store.Open) }
`)
	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var main *FileEntry
	for i := range m.Files {
		if m.Files[i].Path == "cmd/app/main.go" {
			main = &m.Files[i]
		}
	}
	if main == nil {
		t.Fatalf("main.go entry missing: %+v", m.Files)
	}
	got := strings.Join(main.Imports, ",")
	// Module-local imports resolve to repo-relative package dirs; stdlib stays bare.
	if !strings.Contains(got, "internal/store") {
		t.Errorf("expected module-local import resolved to internal/store, got %q", got)
	}
	if !strings.Contains(got, "internal/driver") {
		t.Errorf("expected blank-underscore import internal/driver, got %q", got)
	}
	if !strings.Contains(got, "fmt") {
		t.Errorf("expected stdlib import fmt kept as bare token, got %q", got)
	}
	if !strings.Contains(m.Render(), "→ ") {
		t.Errorf("render missing edge line:\n%s", m.Render())
	}
}

func TestBuildResolvesRelativeImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src/app.ts", `import { util } from "./lib/util";
import React from "react";
const x = require("../shared/const");
export function App() {}
`)
	writeFile(t, dir, "app.py", `from .pkg import thing
from ..other import stuff
import os
def top(): pass
`)
	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	imports := map[string][]string{}
	for _, f := range m.Files {
		imports[f.Path] = f.Imports
	}
	ts := strings.Join(imports["src/app.ts"], ",")
	if !strings.Contains(ts, "src/lib/util") {
		t.Errorf("expected ./lib/util resolved to src/lib/util, got %q", ts)
	}
	if !strings.Contains(ts, "react") {
		t.Errorf("expected bare package react retained, got %q", ts)
	}
	// ../shared/const from src/ resolves to shared/const (repo-relative).
	if !strings.Contains(ts, "shared/const") {
		t.Errorf("expected require('../shared/const') resolved to shared/const, got %q", ts)
	}
	py := strings.Join(imports["app.py"], ",")
	if !strings.Contains(py, "pkg") {
		t.Errorf("expected relative from .pkg resolved, got %q", py)
	}
	if !strings.Contains(py, "os") {
		t.Errorf("expected absolute import os kept, got %q", py)
	}
	// from ..other escapes the repo root (app.py is at root) — kept as raw token.
	if !strings.Contains(py, "..other") {
		t.Errorf("expected escaping relative import kept raw, got %q", py)
	}
}

func TestSchemaVersionInvalidatesOldCache(t *testing.T) {
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
	// Rewrite the cache with an old-schema fingerprint (no schema line mixed in),
	// simulating a v1 cache from an older binary; it must be reported stale.
	stale := struct {
		Fingerprint string `json:"fingerprint"`
		Rendered    string `json:"rendered"`
	}{Fingerprint: "v1-fingerprint-without-schema", Rendered: "# Repository map\na.go\n  func A()\n"}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(cache, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, fresh, err := Load(dir, cache, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Error("a cache written under an older schema version must be reported stale")
	}
}

func TestRenderDropsEdgesBeforeSymbols(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	writeFile(t, dir, "b/dep.go", "package b\nfunc Dep() {}\n")
	writeFile(t, dir, "a.go", `package main

import "example.com/m/b"

func A() { b.Dep() }
`)
	m, err := Build(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Budget large enough for symbols but not the trailing edge line of a.go.
	full := m.Render()
	edgeLine := "  → b\n"
	if !strings.Contains(full, edgeLine) {
		t.Fatalf("precondition: full render should contain edge line:\n%s", full)
	}
	tight := &Map{Root: m.Root, Files: m.Files, maxBytes: strings.Index(full, edgeLine) + len("  → b")}
	got := tight.Render()
	if !strings.Contains(got, "func A()") {
		t.Errorf("symbols must be preserved under a tight budget:\n%s", got)
	}
	if strings.Contains(got, "→ b\n") {
		t.Errorf("edge line should have been dropped under a tight budget:\n%s", got)
	}
}

func TestLoadOrBuildUsesCacheAndRebuilds(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, ".aegis", "repomap.json")
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	writeFile(t, dir, "a.go", "package main\nimport \"example.com/m/b\"\nfunc A() {}\n")
	writeFile(t, dir, "b/b.go", "package b\nfunc B() {}\n")

	// First call: no cache yet — builds and writes one.
	m1, err := LoadOrBuild(dir, cache, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(cache); statErr != nil {
		t.Fatalf("LoadOrBuild should have written the cache: %v", statErr)
	}
	var aImports []string
	for _, f := range m1.Files {
		if f.Path == "a.go" {
			aImports = f.Imports
		}
	}
	if strings.Join(aImports, ",") != "b" {
		t.Errorf("expected a.go import resolved to b, got %v", aImports)
	}

	// Second call with the file unchanged: served from cache (same fingerprint).
	m2, err := LoadOrBuild(dir, cache, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m2.Fingerprint != m1.Fingerprint {
		t.Errorf("unchanged repo should reuse the cached fingerprint")
	}

	// Change a file: LoadOrBuild must rebuild (new fingerprint).
	writeFile(t, dir, "a.go", "package main\nimport \"example.com/m/b\"\nfunc A() {}\nfunc C() {}\n")
	m3, err := LoadOrBuild(dir, cache, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m3.Fingerprint == m1.Fingerprint {
		t.Errorf("changed repo should rebuild with a new fingerprint")
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
