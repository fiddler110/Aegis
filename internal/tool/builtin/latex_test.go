package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// latexInput marshals a latex_build argument map.
func latexInput(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeLatexFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ─── process hardening (compiler-independent) ────────────────────────────────

// TestLatexHardenedFlags pins the shape of the compiler invocation: shell
// escape off, and the flag ahead of the input filename so it can never be
// swallowed as one (P52.2).
func TestLatexHardenedFlags(t *testing.T) {
	flags := latexHardenedFlags("/ws/out", "/ws/main.tex", false)

	if len(flags) == 0 || flags[0] != "-no-shell-escape" {
		t.Fatalf("expected -no-shell-escape first, got %v", flags)
	}
	if got := flags[len(flags)-1]; got != "/ws/main.tex" {
		t.Errorf("expected the .tex path last, got %q (%v)", got, flags)
	}
	for _, want := range []string{"-interaction=nonstopmode", "-halt-on-error", "-output-directory=/ws/out"} {
		if !containsString(flags, want) {
			t.Errorf("missing flag %q in %v", want, flags)
		}
	}
	if containsString(flags, "-draftmode") {
		t.Errorf("unexpected -draftmode outside check_only: %v", flags)
	}
	if containsString(flags, "-shell-escape") {
		t.Errorf("shell escape must never be enabled: %v", flags)
	}

	check := latexHardenedFlags("/ws/out", "/ws/main.tex", true)
	if !containsString(check, "-draftmode") {
		t.Errorf("check_only should add -draftmode, got %v", check)
	}
	if check[0] != "-no-shell-escape" || check[len(check)-1] != "/ws/main.tex" {
		t.Errorf("check_only must keep flag ordering, got %v", check)
	}
}

// TestLatexHardenedEnvPinsFileAccess asserts the TeX file-access variables are
// pinned to paranoid and that a hostile or merely permissive inherited value
// cannot shadow them — the dev host ships openin_any=a.
func TestLatexHardenedEnvPinsFileAccess(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"openin_any=a",
		"openout_any=a",
		"shell_escape=t",
		"HOME=/home/user",
	}
	env := latexHardenedEnv(base)

	for _, want := range []string{"openin_any=p", "openout_any=p", "shell_escape=f"} {
		if !containsString(env, want) {
			t.Errorf("missing %q in %v", want, env)
		}
	}
	for _, gone := range []string{"openin_any=a", "openout_any=a", "shell_escape=t"} {
		if containsString(env, gone) {
			t.Errorf("inherited %q must be dropped, not shadowed: %v", gone, env)
		}
	}
	// Exactly one entry per key, so lookup order in the child cannot matter.
	for _, key := range []string{"openin_any=", "openout_any=", "shell_escape="} {
		n := 0
		for _, kv := range env {
			if strings.HasPrefix(kv, key) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("expected exactly one %s entry, got %d in %v", key, n, env)
		}
	}
	// The rest of the environment must survive — TeX needs PATH/HOME/TEXMF*.
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/user"} {
		if !containsString(env, want) {
			t.Errorf("hardening dropped unrelated var %q: %v", want, env)
		}
	}
}

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// ─── source confinement scan (compiler-independent) ──────────────────────────

func TestLatexConfinementRejectsOutOfWorkspaceReads(t *testing.T) {
	outside := t.TempDir()
	writeLatexFile(t, outside, "secret.tex", "TOPSECRET\n")

	cases := []struct {
		name string
		body string
	}{
		{"absolute input", `\input{/etc/passwd}`},
		{"absolute input with extension", `\input{` + filepath.Join(outside, "secret.tex") + `}`},
		{"home-relative input", `\input{~/.ssh/id_rsa}`},
		{"parent escape", `\include{../../elsewhere/notes}`},
		{"InputIfFileExists", `\InputIfFileExists{/etc/hosts}{}{}`},
		{"lstinputlisting", `\lstinputlisting{/etc/hosts}`},
		{"includegraphics", `\includegraphics[width=\linewidth]{/etc/hosts}`},
		{"addbibresource", `\addbibresource{/etc/refs.bib}`},
		{"bare input, no braces", `\input /etc/passwd`},
		{"openin stream", `\newread\r \openin\r=/etc/passwd`},
		{"graphicspath search root", `\graphicspath{{/etc/}{img/}}`},
		{"import directory", `\import{/etc/}{passwd}`},
		{"local sty", `\usepackage{/etc/passwd}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tex := writeLatexFile(t, root, "main.tex",
				"\\documentclass{article}\n\\begin{document}\n"+tc.body+"\n\\end{document}\n")

			v := checkLatexConfinement(soleRoot(root), tex)
			if len(v) == 0 {
				t.Fatalf("expected a confinement violation for %s", tc.body)
			}
		})
	}
}

func TestLatexConfinementAllowsOrdinaryDocuments(t *testing.T) {
	root := t.TempDir()
	writeLatexFile(t, root, "chapters/intro.tex", "Hello.\n")
	writeLatexFile(t, root, "img/logo.png", "not really a png")
	tex := writeLatexFile(t, root, "main.tex", `\documentclass[11pt,a4paper]{report}
\usepackage{geometry,booktabs}
\usepackage[hidelinks]{hyperref}
\graphicspath{{img/}{figures/}}
\begin{document}
\input{chapters/intro}
\include{chapters/intro}
\includegraphics[width=0.5\linewidth]{img/logo.png}
\bibliography{refs}
% \input{/etc/passwd}
\begin{verbatim}
\input{/etc/passwd}
\end{verbatim}
\begin{lstlisting}
\lstinputlisting{/etc/shadow}
\end{lstlisting}
\input{\jobname-generated}
\end{document}
`)

	if v := checkLatexConfinement(soleRoot(root), tex); len(v) != 0 {
		t.Fatalf("ordinary document flagged: %v", v)
	}
}

// TestLatexConfinementFollowsIncludes covers the one-hop bypass: main.tex stays
// clean and a chapter it pulls in does the reading.
func TestLatexConfinementFollowsIncludes(t *testing.T) {
	root := t.TempDir()
	writeLatexFile(t, root, "chapters/one.tex", "Body.\n\\input{sub/two}\n")
	writeLatexFile(t, root, "chapters/sub/two.tex", "\\input{/etc/passwd}\n")
	tex := writeLatexFile(t, root, "main.tex",
		"\\documentclass{article}\n\\begin{document}\n\\input{chapters/one}\n\\end{document}\n")

	v := checkLatexConfinement(soleRoot(root), tex)
	if len(v) == 0 {
		t.Fatal("expected the transitively-included escape to be caught")
	}
	if !strings.Contains(strings.Join(v, "\n"), filepath.Join("chapters", "sub", "two.tex")) {
		t.Errorf("violation should name the offending file, got %v", v)
	}
}

// A local .sty is workspace source too, and gets pulled in by name.
func TestLatexConfinementFollowsLocalStyle(t *testing.T) {
	root := t.TempDir()
	writeLatexFile(t, root, "aegisreport.sty", "\\input{/etc/passwd}\n")
	tex := writeLatexFile(t, root, "main.tex",
		"\\documentclass{article}\n\\usepackage{aegisreport}\n\\begin{document}\nx\n\\end{document}\n")

	if v := checkLatexConfinement(soleRoot(root), tex); len(v) == 0 {
		t.Fatal("expected the escape inside the local .sty to be caught")
	}
}

// TestLatexBuildRefusesEscapingSource drives the tool end to end. It needs no
// TeX installed: the confinement scan runs before the compiler is looked up, so
// the refusal is the same on a machine with no LaTeX at all.
func TestLatexBuildRefusesEscapingSource(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := writeLatexFile(t, outside, "secret.tex", "AEGIS-P52-2-SECRET\n")
	writeLatexFile(t, root, "main.tex",
		"\\documentclass{article}\n\\begin{document}\n\\input{"+
			strings.TrimSuffix(secret, ".tex")+"}\n\\end{document}\n")

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{"path": "main.tex"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "outside the workspace") {
		t.Errorf("expected a confinement message, got: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "main.pdf")); err == nil {
		t.Error("no PDF should have been produced")
	}
}

// ─── live compiler tier (skipped when no TeX is installed) ───────────────────

func latexCompiler(t *testing.T) string {
	t.Helper()
	for _, c := range []string{"pdflatex", "lualatex", "xelatex"} {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	t.Skip("no LaTeX compiler on PATH")
	return ""
}

// TestLatexBuildDoesNotEmbedOutOfWorkspaceFile is the regression test for the
// P52.2 defect itself: before the fix this produced a PDF with the contents of
// a file from outside the workspace typeset into it. Verified against the real
// compiler, since the whole defect was about what the subprocess is allowed to
// read — note that on TeX Live 2026 the openin_any hardening alone would not
// stop this (upstream made it a no-op), so the source scan is load-bearing.
func TestLatexBuildDoesNotEmbedOutOfWorkspaceFile(t *testing.T) {
	compiler := latexCompiler(t)
	root := t.TempDir()
	outside := t.TempDir()

	const marker = "AEGISP52SECRETMARKER"
	writeLatexFile(t, outside, "secret.tex", marker+"\n")
	writeLatexFile(t, root, "main.tex", `\documentclass{article}
\pdfcompresslevel=0
\pdfobjcompresslevel=0
\begin{document}
\input{`+filepath.Join(outside, "secret")+`}
\end{document}
`)

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": compiler, "runs": 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("build of an escaping document should fail, got: %s", res.Content)
	}

	// Whatever the build did, the host file's contents must not be in any output.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "main.tex" {
			continue // the reference itself lives here; the leak is what matters
		}
		data, readErr := os.ReadFile(filepath.Join(root, e.Name()))
		if readErr != nil {
			continue
		}
		if strings.Contains(string(data), marker) {
			t.Errorf("out-of-workspace content leaked into %s", e.Name())
		}
	}
}

// TestLatexBuildCompilesConfinedDocument is the other half: the hardening must
// not break an ordinary multi-file build.
func TestLatexBuildCompilesConfinedDocument(t *testing.T) {
	compiler := latexCompiler(t)
	root := t.TempDir()

	writeLatexFile(t, root, "parts/body.tex", "Confined and compiling.\n")
	writeLatexFile(t, root, "main.tex", `\documentclass{article}
\begin{document}
\section{Hello}
\input{parts/body}
\end{document}
`)

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": compiler, "runs": 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("confined build should succeed, got: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "main.pdf")); err != nil {
		t.Errorf("expected main.pdf: %v", err)
	}
}

// soleRoot is the single-writable-root set the confinement scanners see for a
// session with no workspace.additional_roots configured (P52.13) — i.e. every
// case these tests exercise.
func soleRoot(root string) []sandbox.Root {
	return []sandbox.Root{{Path: root, Writable: true}}
}
