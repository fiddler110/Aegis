package builtin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scottymacleod/aegis/internal/memory"
	"github.com/scottymacleod/aegis/internal/tool"
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
	sh := newShellTool(root, 30, nil, nil)
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
	for _, name := range []string{"read_file", "write_file", "edit_file", "glob", "grep", "shell", "web_fetch", "web_search", "latex_build", "latex_new_document"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestMultiEdit(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Create a test file.
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644)

	me := &multieditTool{root: root}
	res, err := me.Execute(ctx, mustJSON(t, map[string]any{
		"edits": []map[string]any{
			{"path": "f.txt", "old_string": "alpha", "new_string": "ALPHA"},
			{"path": "f.txt", "old_string": "gamma", "new_string": "GAMMA"},
		},
	}))
	if err != nil || res.IsError {
		t.Fatalf("multi_edit: %v %+v", err, res)
	}

	data, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	content := string(data)
	if !strings.Contains(content, "ALPHA") || !strings.Contains(content, "GAMMA") || !strings.Contains(content, "beta") {
		t.Errorf("multi_edit result = %q", content)
	}
}

func TestRememberAndSkill(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	src := memory.Sources{ProjectRoot: root, DataDir: filepath.Join(root, "data")}

	rem := &rememberTool{src: src}
	res, err := rem.Execute(ctx, mustJSON(t, map[string]any{"note": "testing memory save"}))
	if err != nil || res.IsError {
		t.Fatalf("remember: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "saved") {
		t.Errorf("remember result = %q", res.Content)
	}

	// Verify memory was saved.
	loaded := src.Load()
	if !strings.Contains(loaded, "testing memory save") {
		t.Errorf("memory not loaded back: %q", loaded)
	}

	sk := &saveSkillTool{src: src}
	res, err = sk.Execute(ctx, mustJSON(t, map[string]any{"name": "test-skill", "content": "step 1: do the thing"}))
	if err != nil || res.IsError {
		t.Fatalf("save_skill: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "test-skill") {
		t.Errorf("save_skill result = %q", res.Content)
	}
}

func TestDiagramToolInline(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Use a fake Kroki server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte("<svg>test</svg>"))
	}))
	defer ts.Close()

	dt := &diagramTool{root: root, krokiURL: ts.URL}
	res, err := dt.Execute(ctx, mustJSON(t, map[string]any{"type": "mermaid", "source": "graph TD; A-->B"}))
	if err != nil || res.IsError {
		t.Fatalf("diagram: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "<svg>") {
		t.Errorf("expected inline SVG, got %q", res.Content)
	}
}

func TestDiagramToolToFile(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte("<svg>file-test</svg>"))
	}))
	defer ts.Close()

	dt := &diagramTool{root: root, krokiURL: ts.URL}
	res, err := dt.Execute(ctx, mustJSON(t, map[string]any{"type": "mermaid", "source": "graph TD; A-->B", "path": "out.svg"}))
	if err != nil || res.IsError {
		t.Fatalf("diagram to file: %v %+v", err, res)
	}
	data, err := os.ReadFile(filepath.Join(root, "out.svg"))
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(data) != "<svg>file-test</svg>" {
		t.Errorf("output file content = %q", data)
	}
}

func TestModelsToolRegistered(t *testing.T) {
	m := &modelsTool{}
	if m.Name() != "list_models" {
		t.Errorf("name = %q", m.Name())
	}
	if m.Capability() != tool.CapNetwork {
		t.Errorf("capability = %v", m.Capability())
	}
}

func TestSecurityScanToolRegistered(t *testing.T) {
	s := &securityScanTool{root: t.TempDir()}
	if s.Name() != "security_scan" {
		t.Errorf("name = %q", s.Name())
	}
}

func TestWriteLargeContentRejected(t *testing.T) {
	root := t.TempDir()
	w := &writeTool{root: root}
	bigContent := strings.Repeat("x", 11<<20) // 11 MiB
	res, _ := w.Execute(context.Background(), mustJSON(t, map[string]any{"path": "big.txt", "content": bigContent}))
	if !res.IsError {
		t.Error("expected error for oversized content")
	}
}

func TestReadWithLimitAndOffset(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "lines.txt"), []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644)

	r := &readTool{root: root}
	// offset is 1-based: offset=3 starts at line 3.
	res, _ := r.Execute(context.Background(), mustJSON(t, map[string]any{"path": "lines.txt", "offset": 3, "limit": 2}))
	if !strings.Contains(res.Content, "line3") || !strings.Contains(res.Content, "line4") {
		t.Errorf("read with offset/limit = %q", res.Content)
	}
	if strings.Contains(res.Content, "line1") || strings.Contains(res.Content, "line5") {
		t.Errorf("read returned lines outside range: %q", res.Content)
	}
}

func TestLatexNewDocument(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	lt := &latexNewDocumentTool{root: root}

	tests := []struct {
		name     string
		args     map[string]any
		wantLine string // snippet that must appear in the generated .tex
	}{
		{
			name:     "report xelatex",
			args:     map[string]any{"path": "report.tex", "title": "Test Report", "author": "Alice"},
			wantLine: `\documentclass[11pt,a4paper]{report}`,
		},
		{
			name:     "whitepaper pdflatex",
			args:     map[string]any{"path": "wp.tex", "title": "White Paper", "style": "whitepaper", "compiler": "pdflatex"},
			wantLine: `\usepackage[T1]{fontenc}`,
		},
		{
			name:     "article with sections",
			args:     map[string]any{"path": "art.tex", "title": "My Article", "style": "article", "sections": []string{"Intro", "Methods", "Results"}},
			wantLine: `\section{Intro}`,
		},
		{
			name:     "report with abstract",
			args:     map[string]any{"path": "full.tex", "title": "Full Report", "abstract": "Key findings go here."},
			wantLine: "Key findings go here.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := lt.Execute(ctx, mustJSON(t, tc.args))
			if err != nil || res.IsError {
				t.Fatalf("latex_new_document: %v %+v", err, res)
			}
			path, _ := tc.args["path"].(string)
			data, readErr := os.ReadFile(filepath.Join(root, path))
			if readErr != nil {
				t.Fatalf("generated file not found: %v", readErr)
			}
			if !strings.Contains(string(data), tc.wantLine) {
				t.Errorf("generated .tex missing %q\nfirst 400 chars:\n%s", tc.wantLine, string(data[:min(400, len(data))]))
			}
		})
	}
}

func TestLatexBuildMissingCompiler(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "doc.tex"), []byte(`\documentclass{article}\begin{document}hello\end{document}`), 0o644)

	lt := &latexBuildTool{root: root}
	res, err := lt.Execute(context.Background(), mustJSON(t, map[string]any{
		"path":     "doc.tex",
		"compiler": "definitely-not-a-real-latex-compiler-xyz",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when compiler is missing")
	}
	if !strings.Contains(res.Content, "not found in PATH") {
		t.Errorf("expected install hint, got: %q", res.Content)
	}
}

func TestLatexBuildMissingFile(t *testing.T) {
	root := t.TempDir()
	lt := &latexBuildTool{root: root}
	res, err := lt.Execute(context.Background(), mustJSON(t, map[string]any{"path": "nonexistent.tex"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "file not found") {
		t.Errorf("expected file-not-found error, got: %+v", res)
	}
}

func TestParseLatexLog(t *testing.T) {
	log := `
This is xelatex
! Undefined control sequence.
\mycommand ->
l.15 \mycommand

! Missing $ inserted.
<inserted text>

LaTeX Warning: Reference 'sec:foo' on page 3 undefined.
LaTeX Warning: Reference 'sec:foo' on page 3 undefined.
Output written on doc.pdf (12 pages, 98304 bytes).
`
	s := parseLatexLog(log, "doc.pdf", false)
	if !s.success {
		t.Error("expected success=true (Output written on)")
	}
	if s.pages != 12 {
		t.Errorf("pages = %d, want 12", s.pages)
	}
	if len(s.errors) != 2 {
		t.Errorf("errors = %d, want 2: %v", len(s.errors), s.errors)
	}
	// Duplicate warning should be deduplicated to 1
	if len(s.warnings) != 1 {
		t.Errorf("warnings = %d, want 1 (deduplicated): %v", len(s.warnings), s.warnings)
	}
}

func TestSSRFBlocksPrivateIPs(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1"} {
		parsed := net.ParseIP(ip)
		if !isPrivateIP(parsed) {
			t.Errorf("isPrivateIP(%s) = false, want true", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1"} {
		parsed := net.ParseIP(ip)
		if isPrivateIP(parsed) {
			t.Errorf("isPrivateIP(%s) = true, want false", ip)
		}
	}
}
