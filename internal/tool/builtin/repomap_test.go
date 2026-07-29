package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func writeRepoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepomapFixture builds a small multi-language repo: a Go module whose
// cmd/app/main.go imports a module-local package, and a TS file with a relative
// import — enough to exercise both edge-resolution shapes (Go package dir,
// JS extension-less path).
func newRepomapFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeRepoFile(t, dir, "go.mod", "module example.com/proj\n\ngo 1.22\n")
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n\nfunc Open() error { return nil }\n")
	writeRepoFile(t, dir, "cmd/app/main.go", `package main

import (
	"fmt"
	"example.com/proj/internal/store"
)

func main() { fmt.Println(store.Open()) }
`)
	writeRepoFile(t, dir, "web/src/app.ts", `import { util } from "./lib/util";

export function App() { return util(); }
`)
	writeRepoFile(t, dir, "web/src/lib/util.ts", "export function util() { return 1; }\n")
	return dir
}

func TestRepomapToolIdentity(t *testing.T) {
	rt := &repomapTool{root: t.TempDir()}
	if rt.Name() != "repomap" {
		t.Errorf("Name() = %q", rt.Name())
	}
	if rt.Capability() != tool.CapRead {
		t.Errorf("Capability() = %q, want read", rt.Capability())
	}
}

func TestRepomapMapAction(t *testing.T) {
	rt := &repomapTool{root: newRepomapFixture(t)}
	ctx := context.Background()

	res, err := rt.Execute(ctx, mustJSON(t, map[string]any{"action": "map"}))
	if err != nil || res.IsError {
		t.Fatalf("map: %v %+v", err, res)
	}
	for _, want := range []string{"func Open() error", "cmd/app/main.go", "→ ", "internal/store"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("map output missing %q:\n%s", want, res.Content)
		}
	}
}

func TestRepomapMapGlobFilters(t *testing.T) {
	rt := &repomapTool{root: newRepomapFixture(t)}
	ctx := context.Background()

	res, err := rt.Execute(ctx, mustJSON(t, map[string]any{"action": "map", "glob": "internal/store/*"}))
	if err != nil || res.IsError {
		t.Fatalf("map+glob: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "internal/store/store.go") {
		t.Errorf("glob should keep matching file:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "cmd/app/main.go") {
		t.Errorf("glob should exclude non-matching file:\n%s", res.Content)
	}

	// A glob that matches nothing is a clean (non-error) empty result.
	res, err = rt.Execute(ctx, mustJSON(t, map[string]any{"action": "map", "glob": "nope/*"}))
	if err != nil || res.IsError {
		t.Fatalf("empty glob: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "no files match") {
		t.Errorf("expected no-match message, got:\n%s", res.Content)
	}
}

func TestRepomapSkeletonAction(t *testing.T) {
	rt := &repomapTool{root: newRepomapFixture(t)}
	ctx := context.Background()

	res, err := rt.Execute(ctx, mustJSON(t, map[string]any{"action": "skeleton", "path": "cmd/app/main.go"}))
	if err != nil || res.IsError {
		t.Fatalf("skeleton: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "func main()") {
		t.Errorf("skeleton missing symbol:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "internal/store") {
		t.Errorf("skeleton missing resolved import edge:\n%s", res.Content)
	}

	// A path with no indexed symbols returns a clean message, not an error.
	res, err = rt.Execute(ctx, mustJSON(t, map[string]any{"action": "skeleton", "path": "go.mod"}))
	if err != nil || res.IsError {
		t.Fatalf("skeleton non-source: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "no symbols indexed") {
		t.Errorf("expected no-symbols message, got:\n%s", res.Content)
	}

	// Missing path is a usage error.
	res, err = rt.Execute(ctx, mustJSON(t, map[string]any{"action": "skeleton"}))
	if err != nil {
		t.Fatalf("skeleton missing path errored hard: %v", err)
	}
	if !res.IsError {
		t.Errorf("skeleton without path should be an IsError result:\n%s", res.Content)
	}
}

func TestRepomapImportersAction(t *testing.T) {
	rt := &repomapTool{root: newRepomapFixture(t)}
	ctx := context.Background()

	// Go case: main.go imports the store *package*, whose edge resolves to the
	// directory "internal/store"; querying importers of the package's file must
	// still match via the directory rule.
	res, err := rt.Execute(ctx, mustJSON(t, map[string]any{"action": "importers", "path": "internal/store/store.go"}))
	if err != nil || res.IsError {
		t.Fatalf("importers (go): %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "cmd/app/main.go") {
		t.Errorf("expected main.go as importer of store:\n%s", res.Content)
	}

	// JS case: app.ts imports "./lib/util", which resolves to the extension-less
	// "web/src/lib/util"; querying importers of util.ts must match via the
	// extension-stripped rule.
	res, err = rt.Execute(ctx, mustJSON(t, map[string]any{"action": "importers", "path": "web/src/lib/util.ts"}))
	if err != nil || res.IsError {
		t.Fatalf("importers (js): %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "web/src/app.ts") {
		t.Errorf("expected app.ts as importer of util.ts:\n%s", res.Content)
	}

	// Nothing imports main.go — clean message.
	res, err = rt.Execute(ctx, mustJSON(t, map[string]any{"action": "importers", "path": "cmd/app/main.go"}))
	if err != nil || res.IsError {
		t.Fatalf("importers (none): %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "no importers found") {
		t.Errorf("expected no-importers message, got:\n%s", res.Content)
	}
}

func TestRepomapUnknownAction(t *testing.T) {
	rt := &repomapTool{root: newRepomapFixture(t)}
	res, err := rt.Execute(context.Background(), mustJSON(t, map[string]any{"action": "bogus"}))
	if err != nil {
		t.Fatalf("unknown action errored hard: %v", err)
	}
	if !res.IsError {
		t.Errorf("unknown action should be an IsError result:\n%s", res.Content)
	}
}
