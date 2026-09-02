package builtin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/memory"
	"github.com/fiddler110/aegis/internal/netblock"
	"github.com/fiddler110/aegis/internal/tool"
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

// TestWriteFilePresentationCarriesPriorContent is P64.4's producer-side
// check: write_file's Result must attach a Presentation payload carrying the
// file's content immediately before an overwrite — the only way a presenter
// can show an accurate diff instead of the "everything added" preview its
// own call input alone would produce — but must not attach one for a brand
// new file (nothing to diff against) or a no-op write (old == new).
func TestWriteFilePresentationCarriesPriorContent(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	w := &writeTool{root: root}

	// New file: nothing to diff against.
	res, err := w.Execute(ctx, mustJSON(t, map[string]any{"path": "a.txt", "content": "v1\n"}))
	if err != nil || res.IsError {
		t.Fatalf("write: %v %+v", err, res)
	}
	if res.Presentation != nil {
		t.Errorf("expected no Presentation for a new file, got %s", res.Presentation)
	}

	// Overwrite: Presentation must carry the prior content.
	res, err = w.Execute(ctx, mustJSON(t, map[string]any{"path": "a.txt", "content": "v2\n"}))
	if err != nil || res.IsError {
		t.Fatalf("overwrite: %v %+v", err, res)
	}
	var payload struct {
		Old string `json:"old"`
	}
	if err := json.Unmarshal(res.Presentation, &payload); err != nil {
		t.Fatalf("expected valid Presentation JSON, got %s: %v", res.Presentation, err)
	}
	if payload.Old != "v1\n" {
		t.Errorf("expected Presentation.Old %q, got %q", "v1\n", payload.Old)
	}

	// No-op rewrite (content unchanged): nothing meaningful to diff.
	res, err = w.Execute(ctx, mustJSON(t, map[string]any{"path": "a.txt", "content": "v2\n"}))
	if err != nil || res.IsError {
		t.Fatalf("no-op write: %v %+v", err, res)
	}
	if res.Presentation != nil {
		t.Errorf("expected no Presentation for an unchanged rewrite, got %s", res.Presentation)
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

// TestShellFailedScriptHintsInterpreter is P39.2: a failing command whose
// first token is a bare script path (e.g. recon.py) should get an
// interpreter-prefix hint appended, since small local models were observed
// invoking scripts this way and failing to self-correct.
func TestShellFailedScriptHintsInterpreter(t *testing.T) {
	root := t.TempDir()
	sh := newShellTool(root, 30, nil, nil)
	res, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{"command": "nonexistent_recon.py --flag"}))
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected the bare script invocation to fail, got %+v", res)
	}
	if !strings.Contains(res.Content, "did you mean to run this with an interpreter") {
		t.Errorf("expected interpreter hint in failure content, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "python nonexistent_recon.py") {
		t.Errorf("expected hint to name python and the script path, got %q", res.Content)
	}
}

// TestShellFailedNonScriptNoHint guards against over-eager hinting: a
// failing command with no recognizable script extension must not get an
// interpreter suggestion appended.
func TestShellFailedNonScriptNoHint(t *testing.T) {
	root := t.TempDir()
	sh := newShellTool(root, 30, nil, nil)
	res, err := sh.Execute(context.Background(), mustJSON(t, map[string]any{"command": "nonexistent_command_xyz --flag"}))
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected the bad command to fail, got %+v", res)
	}
	if strings.Contains(res.Content, "did you mean to run this with an interpreter") {
		t.Errorf("did not expect an interpreter hint for a non-script command, got %q", res.Content)
	}
}

func TestRegisterAll(t *testing.T) {
	reg := tool.NewRegistry()
	if err := Register(reg, Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "glob", "grep", "shell", "web_fetch", "web_search", "latex_build", "latex_new_document", "dast_scan", "recon_scan", "security_advise"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

// TestRegisterLocalProfileDefersNetworkAndScanTools covers P25.6(a): under
// the local prompt profile, web_search/web_fetch/security_scan/git_pr are
// registered but not exposed by default (moved to the tool_search deferral
// path) to cut always-exposed schema tokens for small local models; the
// default profile keeps them always-exposed, unchanged from before P25.6.
func TestRegisterLocalProfileDefersNetworkAndScanTools(t *testing.T) {
	deferCandidates := []string{"web_fetch", "web_search", "security_scan", "git_pr", "edit_file"}

	regDefault := tool.NewRegistry()
	if err := Register(regDefault, Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, name := range deferCandidates {
		if _, ok := regDefault.Get(name); !ok {
			t.Errorf("default profile: tool %q not registered at all", name)
		}
	}
	defaultSchemaNames := map[string]bool{}
	for _, s := range regDefault.Schemas() {
		defaultSchemaNames[s.Name] = true
	}
	for _, name := range deferCandidates {
		if !defaultSchemaNames[name] {
			t.Errorf("default profile: %q should be always-exposed, is not in Schemas()", name)
		}
	}

	regLocal := tool.NewRegistry()
	if err := Register(regLocal, Options{Root: t.TempDir(), LocalProfile: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range deferCandidates {
		if _, ok := regLocal.Get(name); !ok {
			t.Errorf("local profile: tool %q not registered at all", name)
		}
	}
	localSchemaNames := map[string]bool{}
	for _, s := range regLocal.Schemas() {
		localSchemaNames[s.Name] = true
	}
	for _, name := range deferCandidates {
		if localSchemaNames[name] {
			t.Errorf("local profile: %q should be deferred, but is always-exposed in Schemas()", name)
		}
	}
	deferredNames := map[string]bool{}
	for _, d := range regLocal.Deferred() {
		deferredNames[d.Name] = true
	}
	for _, name := range deferCandidates {
		if !deferredNames[name] {
			t.Errorf("local profile: %q not advertised via Deferred()", name)
		}
	}

	if len(regLocal.Schemas()) >= len(regDefault.Schemas()) {
		t.Errorf("local profile should expose fewer schemas than default: local=%d default=%d", len(regLocal.Schemas()), len(regDefault.Schemas()))
	}

	// P62.9's direction is the non-obvious half of the editing-surface cut, and
	// it is the half a later "save more tokens" pass would undo: the handle-
	// based editors cost MORE than edit_file (edit_section 407, multi_edit 276,
	// fill_marker 226 against 185) and are the ones that must stay, because
	// P39.16 measured a small model failing edit_file's byte-exact match 12
	// times running where edit_section took 7 clean calls. Deferring them to
	// save the bigger number would re-create exactly that failure.
	for _, name := range []string{"write_file", "edit_section", "fill_marker", "multi_edit"} {
		if !localSchemaNames[name] {
			t.Errorf("local profile: %q must stay always-exposed — it is what edit_file was deferred in favour of", name)
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

func TestReconScanToolRegistered(t *testing.T) {
	rs := &reconScanTool{}
	if rs.Name() != "recon_scan" {
		t.Errorf("name = %q", rs.Name())
	}
	if rs.Capability() != tool.CapExecute {
		t.Errorf("capability = %q, want execute", rs.Capability())
	}
}

// TestReconScanToolRejectsDisallowedTargetBeforeAnyScanner proves the tool
// wiring surfaces security.RunRecon's hard target-authorization gate as an
// error (P13.5) rather than swallowing it — no nmap/nuclei binary needs to
// exist for this rejection to happen deterministically.
func TestReconScanToolRejectsDisallowedTargetBeforeAnyScanner(t *testing.T) {
	rs := &reconScanTool{}
	_, err := rs.Execute(context.Background(), mustJSON(t, map[string]any{
		"targets": []string{"evil.example.com"},
	}))
	if err == nil {
		t.Fatal("expected an error for a disallowed target")
	}
	if !strings.Contains(err.Error(), "evil.example.com") {
		t.Errorf("err = %q, want it to name the disallowed target", err)
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

// A directory-shaped path must be refused rather than silently cleaned into a
// file name. Observed live: a model used write_file with a trailing separator
// as a mkdir, which created an empty file exactly where the run directory
// belonged and made every later write beneath it fail with an opaque MkdirAll
// error — unrecoverable without deleting the stray file out of band.
func TestWriteRejectsDirectoryShapedPath(t *testing.T) {
	root := t.TempDir()
	w := &writeTool{root: root}

	for _, p := range []string{"out/rundir/", `out\rundir\`} {
		res, err := w.Execute(context.Background(), mustJSON(t, map[string]any{"path": p, "content": ""}))
		if err != nil {
			t.Fatalf("path %q: unexpected transport error: %v", p, err)
		}
		if !res.IsError {
			t.Errorf("path %q: expected an error result, got %q", p, res.Content)
		}
		if _, statErr := os.Stat(filepath.Join(root, "out", "rundir")); statErr == nil {
			t.Errorf("path %q: write created an entry at the directory path", p)
		}
	}

	// An existing directory is refused too, even without a trailing separator.
	if err := os.MkdirAll(filepath.Join(root, "existing"), 0o750); err != nil {
		t.Fatal(err)
	}
	res, err := w.Execute(context.Background(), mustJSON(t, map[string]any{"path": "existing", "content": "x"}))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected an error result for an existing directory, got %q", res.Content)
	}
}

// The materialized built-in skill tree is harness-owned: readable so the model
// can reach a skill's scripts and skeletons, never writable. Observed live
// (P38.1 re-test, 2026-08-09): a model replaced recon.py with the command line
// it meant to run, leaving the phase with tooling that could only raise
// SyntaxError and no copy to restore from.
func TestSkillAssetsAreReadOnlyToTools(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".aegis", "builtin-skills", "threat-modeling")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	recon := filepath.Join(skillDir, "recon.py")
	const original = "#!/usr/bin/env python3\nprint('recon')\n"
	if err := os.WriteFile(recon, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	relRecon := ".aegis/builtin-skills/threat-modeling/recon.py"
	ctx := context.Background()

	// write_file, edit and multi_edit must all refuse it.
	if _, err := (&writeTool{root: root}).Execute(ctx, mustJSON(t, map[string]any{
		"path": relRecon, "content": "clobbered",
	})); err == nil {
		t.Error("write_file: expected an error writing a built-in skill file")
	}
	if _, err := (&editTool{root: root}).Execute(ctx, mustJSON(t, map[string]any{
		"path": relRecon, "old_string": "recon", "new_string": "clobbered",
	})); err == nil {
		t.Error("edit: expected an error editing a built-in skill file")
	}

	if got, _ := os.ReadFile(recon); string(got) != original {
		t.Errorf("recon.py was modified: %q", got)
	}

	// Reading it must still work — that is why the tree is inside the root.
	res, err := (&readTool{root: root}).Execute(ctx, mustJSON(t, map[string]any{"path": relRecon}))
	if err != nil {
		t.Fatalf("read of a skill asset failed: %v", err)
	}
	if res.IsError || !strings.Contains(res.Content, "print('recon')") {
		t.Errorf("read of a skill asset = %q, want the file content", res.Content)
	}

	// A sibling that merely shares a path prefix stays writable, and so does
	// .aegis/skills/ — user-authored skills are ordinary workspace content.
	for _, p := range []string{".aegis/builtin-skills-notes.md", ".aegis/skills/mine.md", "recon.py"} {
		res, err := (&writeTool{root: root}).Execute(ctx, mustJSON(t, map[string]any{"path": p, "content": "ok"}))
		if err != nil || res.IsError {
			t.Errorf("write to %q was refused (err=%v, res=%q), want it allowed", p, err, res.Content)
		}
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
		if !netblock.IsPrivate(parsed) {
			t.Errorf("netblock.IsPrivate(%s) = false, want true", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1"} {
		parsed := net.ParseIP(ip)
		if netblock.IsPrivate(parsed) {
			t.Errorf("netblock.IsPrivate(%s) = true, want false", ip)
		}
	}
}

// A template placeholder that reached a real path must be refused with a
// message naming the problem. Observed live: a model wrote the literal
// `2-<framework>-analysis.md` from its skill's documentation and retried the
// identical call, because Windows' own error ("The filename, directory name,
// or volume label syntax is incorrect") names neither the character nor what
// to do about it.
func TestWriteRejectsInvalidWindowsFilename(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only filename rules")
	}
	root := t.TempDir()
	w := &writeTool{root: root}
	res, err := w.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "2-<framework>-analysis.md", "content": "x",
	}))
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a refusal, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "placeholder") {
		t.Errorf("error should point at the placeholder cause, got %q", res.Content)
	}
	// An ordinary name, and an absolute path whose drive colon is legitimate,
	// must still be accepted.
	for _, p := range []string{"2-stride-analysis.md", filepath.Join(root, "ok.md")} {
		if res, err := w.Execute(context.Background(), mustJSON(t, map[string]any{"path": p, "content": "x"})); err != nil || res.IsError {
			t.Errorf("write to %q refused (err=%v res=%q)", p, err, res.Content)
		}
	}
}
