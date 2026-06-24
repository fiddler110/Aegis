package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottymacleod/agentharness/internal/tool"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteReadEdit(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	w := &writeTool{root: root}
	res, err := w.Execute(ctx, mustJSON(t, map[string]any{"path": "sub/a.txt", "content": "hello\nworld\n"}))
	if err != nil || res.IsError {
		t.Fatalf("write: %v %+v", err, res)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "a.txt")); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	r := &readTool{root: root}
	res, _ = r.Execute(ctx, mustJSON(t, map[string]any{"path": "sub/a.txt"}))
	if !strings.Contains(res.Content, "1\thello") || !strings.Contains(res.Content, "2\tworld") {
		t.Errorf("read output missing numbered lines: %q", res.Content)
	}

	e := &editTool{root: root}
	res, _ = e.Execute(ctx, mustJSON(t, map[string]any{"path": "sub/a.txt", "old_string": "world", "new_string": "gophers"}))
	if res.IsError {
		t.Fatalf("edit failed: %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(root, "sub", "a.txt"))
	if !strings.Contains(string(data), "gophers") {
		t.Errorf("edit not applied: %q", data)
	}
}

func TestEditAmbiguous(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("x x x"), 0o644)
	e := &editTool{root: root}
	res, _ := e.Execute(context.Background(), mustJSON(t, map[string]any{"path": "f.txt", "old_string": "x", "new_string": "y"}))
	if !res.IsError {
		t.Error("expected error for ambiguous edit without replace_all")
	}
}

func TestPathEscapeRejected(t *testing.T) {
	root := t.TempDir()
	r := &readTool{root: root}
	res, err := r.Execute(context.Background(), mustJSON(t, map[string]any{"path": "../../etc/passwd"}))
	if err == nil && !res.IsError {
		t.Errorf("expected path-escape rejection, got %+v", res)
	}
}

func TestGlobAndGrep(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Foo() {}\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "util.go"), []byte("package pkg\nfunc Bar() {}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("docs"), 0o644)

	g := &globTool{root: root}
	res, _ := g.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
	if !strings.Contains(res.Content, "main.go") || !strings.Contains(res.Content, "pkg/util.go") {
		t.Errorf("glob missed go files: %q", res.Content)
	}
	if strings.Contains(res.Content, "readme.md") {
		t.Errorf("glob matched non-go file: %q", res.Content)
	}

	gr := &grepTool{root: root}
	res, _ = gr.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "func \\w+\\("}))
	if !strings.Contains(res.Content, "Foo") || !strings.Contains(res.Content, "Bar") {
		t.Errorf("grep missed matches: %q", res.Content)
	}
}

func TestShellEcho(t *testing.T) {
	root := t.TempDir()
	sh := newShellTool(root, 30, nil)
	res, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{"command": "echo harness-ok"}))
	if err != nil || res.IsError {
		t.Fatalf("shell: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "harness-ok") {
		t.Errorf("shell output = %q", res.Content)
	}
}

func TestRegisterAll(t *testing.T) {
	reg := tool.NewRegistry()
	if err := Register(reg, Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "glob", "grep", "shell", "web_fetch", "web_search"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}
