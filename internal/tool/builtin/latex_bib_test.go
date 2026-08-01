package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// ─── source-level bibliography auto-detection (P52.10) ───────────────────────

func TestLatexSourceDeclaresBibliography(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"addbibresource", `\addbibresource{references.bib}`, true},
		{"addbibresource with options", `\addbibresource[datatype=bibtex]{references.bib}`, true},
		{"addglobalbib", `\addglobalbib{shared.bib}`, true},
		{"bibtex bibliography", `\bibliography{refs}`, true},
		{"bibliography comma list", `\bibliography{refs,more}`, true},
		{"bibliographystyle alone", `\bibliographystyle{plain}`, false},
		{"printbibliography alone", `\printbibliography[heading=bibintoc]`, false},
		{"commented out scaffold", "%% \\addbibresource{references.bib}", false},
		{"no bibliography at all", `\section{Hello}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLatexFile(t, root, "references.bib", "@misc{x, title={x}}\n")
			writeLatexFile(t, root, "shared.bib", "@misc{y, title={y}}\n")
			tex := writeLatexFile(t, root, "main.tex",
				"\\documentclass{article}\n"+tc.body+"\n\\begin{document}\nx\n\\end{document}\n")

			if got := latexSourceDeclaresBibliography(soleRoot(root), tex); got != tc.want {
				t.Errorf("latexSourceDeclaresBibliography(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// The bibliography can be declared in an included chapter rather than the root
// file, which is exactly where the auto-detection would otherwise miss it.
func TestLatexSourceDeclaresBibliographyThroughInclude(t *testing.T) {
	root := t.TempDir()
	writeLatexFile(t, root, "preamble/bib.tex", "\\addbibresource{references.bib}\n")
	tex := writeLatexFile(t, root, "main.tex",
		"\\documentclass{article}\n\\input{preamble/bib}\n\\begin{document}\nx\n\\end{document}\n")

	if !latexSourceDeclaresBibliography(soleRoot(root), tex) {
		t.Error("a bibliography declared in an included file should be detected")
	}
}

// The P52.2 source scan already validates \addbibresource / \bibliography
// arguments; this pins that, since the bib pass now depends on it.
func TestLatexConfinementRejectsOutOfWorkspaceBibSource(t *testing.T) {
	root := t.TempDir()
	tex := writeLatexFile(t, root, "main.tex",
		"\\documentclass{article}\n\\addbibresource{/etc/refs.bib}\n\\begin{document}\nx\n\\end{document}\n")

	if v := checkLatexConfinement(soleRoot(root), tex); len(v) == 0 {
		t.Fatal("an out-of-workspace \\addbibresource must be refused by the source scan")
	}
}

// latex_new_document scaffolds a commented-out biblatex block into every
// preamble. Detection must stay quiet until the user uncomments it, and must
// fire once they do — the exact workflow P52.10 is about.
func TestLatexScaffoldBibliographyDetection(t *testing.T) {
	root := t.TempDir()
	res, err := (&latexNewDocumentTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "report.tex", "title": "Test Report",
	}))
	if err != nil || res.IsError {
		t.Fatalf("latex_new_document: %v %+v", err, res)
	}
	tex := filepath.Join(root, "report.tex")
	if latexSourceDeclaresBibliography(soleRoot(root), tex) {
		t.Error("the scaffolded (commented-out) bibliography block must not trigger a bib pass")
	}

	data, err := os.ReadFile(tex)
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ReplaceAll(string(data), "% \\addbibresource{references.bib}", "\\addbibresource{references.bib}")
	if src == string(data) {
		t.Fatal("scaffold no longer contains the commented \\addbibresource line")
	}
	if err := os.WriteFile(tex, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLatexFile(t, root, "references.bib", "@misc{k, title={t}}\n")
	if !latexSourceDeclaresBibliography(soleRoot(root), tex) {
		t.Error("uncommenting \\addbibresource must enable the bib pass")
	}
}

// ─── generated-artefact confinement (.bcf / .aux) ────────────────────────────

const testBCFHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<bcf:controlfile version="3.11" bltxversion="3.19" xmlns:bcf="https://sourceforge.net/projects/biblatex">
`

func bcfWithDatasources(paths ...string) string {
	var b strings.Builder
	b.WriteString(testBCFHeader)
	b.WriteString("  <bcf:bibdata section=\"0\">\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "    <bcf:datasource type=\"file\" datatype=\"bibtex\" glob=\"false\">%s</bcf:datasource>\n", p)
	}
	b.WriteString("  </bcf:bibdata>\n</bcf:controlfile>\n")
	return b.String()
}

func TestCheckLatexBibConfinementBCF(t *testing.T) {
	outside := t.TempDir()

	cases := []struct {
		name       string
		datasource string
		wantBad    bool
	}{
		{"workspace relative", "references.bib", false},
		{"workspace subdir", "bib/references.bib", false},
		{"absolute host path", "/etc/passwd", true},
		{"absolute temp path", filepath.Join(outside, "secret.bib"), true},
		{"parent escape", "../../elsewhere/refs.bib", true},
		{"home relative", "~/.ssh/id_rsa", true},
		{"remote datasource", "https://example.com/refs.bib", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLatexFile(t, root, "main.bcf", bcfWithDatasources(tc.datasource))
			control := filepath.Join(root, "main.bcf")

			v := checkLatexBibConfinement(soleRoot(root), root, root, control)
			if tc.wantBad && len(v) == 0 {
				t.Fatalf("datasource %q should be refused", tc.datasource)
			}
			if !tc.wantBad && len(v) != 0 {
				t.Fatalf("datasource %q wrongly refused: %v", tc.datasource, v)
			}
		})
	}
}

// A .bcf can name a resource the source never spelled out — biblatex expands
// macros before writing it. This is the case the P52.2 source scan cannot see.
func TestCheckLatexBibConfinementCatchesResourceNotInSource(t *testing.T) {
	root := t.TempDir()
	// The source is clean: a workspace-relative \addbibresource.
	tex := writeLatexFile(t, root, "main.tex",
		"\\documentclass{article}\n\\addbibresource{\\bibfile}\n\\begin{document}\nx\n\\end{document}\n")
	if v := checkLatexConfinement(soleRoot(root), tex); len(v) != 0 {
		t.Fatalf("macro-built source reference should not be flagged by the source scan: %v", v)
	}
	// The generated control file is not.
	writeLatexFile(t, root, "main.bcf", bcfWithDatasources("/etc/passwd"))
	v := checkLatexBibConfinement(soleRoot(root), root, root, filepath.Join(root, "main.bcf"))
	if len(v) == 0 {
		t.Fatal("the .bcf scan must catch what the source scan could not resolve")
	}
	if !strings.Contains(strings.Join(v, "\n"), "main.bcf") {
		t.Errorf("violation should name the control file, got %v", v)
	}
}

func TestCheckLatexBibConfinementAUX(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		root := t.TempDir()
		writeLatexFile(t, root, "main.aux", "\\bibstyle{plain}\n\\bibdata{refs}\n\\citation{a}\n")
		if v := checkLatexBibConfinement(soleRoot(root), root, root, filepath.Join(root, "main.aux")); len(v) != 0 {
			t.Fatalf("ordinary .aux flagged: %v", v)
		}
	})

	t.Run("bibdata escape", func(t *testing.T) {
		root := t.TempDir()
		writeLatexFile(t, root, "main.aux", "\\bibstyle{plain}\n\\bibdata{refs,/etc/passwd}\n")
		v := checkLatexBibConfinement(soleRoot(root), root, root, filepath.Join(root, "main.aux"))
		if len(v) == 0 {
			t.Fatal("an out-of-workspace \\bibdata must be refused")
		}
	})

	t.Run("bibstyle escape", func(t *testing.T) {
		root := t.TempDir()
		writeLatexFile(t, root, "main.aux", "\\bibstyle{/etc/evil}\n\\bibdata{refs}\n")
		if v := checkLatexBibConfinement(soleRoot(root), root, root, filepath.Join(root, "main.aux")); len(v) == 0 {
			t.Fatal("an out-of-workspace \\bibstyle must be refused")
		}
	})

	// bibtex follows \@input into the .aux files of \include-d chapters, so the
	// scan has to as well.
	t.Run("nested aux escape", func(t *testing.T) {
		root := t.TempDir()
		writeLatexFile(t, root, "main.aux", "\\bibdata{refs}\n\\@input{sub/chap.aux}\n")
		writeLatexFile(t, root, "sub/chap.aux", "\\bibdata{/etc/passwd}\n")
		v := checkLatexBibConfinement(soleRoot(root), root, root, filepath.Join(root, "main.aux"))
		if len(v) == 0 {
			t.Fatal("an escape in a nested .aux must be refused")
		}
		if !strings.Contains(strings.Join(v, "\n"), "chap.aux") {
			t.Errorf("violation should name the offending .aux, got %v", v)
		}
	})
}

func TestLatexBibControlFilePrefersBCF(t *testing.T) {
	root := t.TempDir()

	if tl, _ := latexBibControlFile(root, "main"); tl != "" {
		t.Errorf("no artefacts should mean no bib tool, got %q", tl)
	}

	writeLatexFile(t, root, "main.aux", "\\citation{a}\n\\relax\n")
	if tl, _ := latexBibControlFile(root, "main"); tl != "" {
		t.Errorf("an .aux without \\bibdata should mean no bib tool, got %q", tl)
	}

	writeLatexFile(t, root, "main.aux", "\\citation{a}\n\\bibdata{refs}\n\\bibstyle{plain}\n")
	tl, control := latexBibControlFile(root, "main")
	if tl != "bibtex" || filepath.Base(control) != "main.aux" {
		t.Errorf("expected bibtex over main.aux, got %q %q", tl, control)
	}

	writeLatexFile(t, root, "main.bcf", bcfWithDatasources("refs.bib"))
	tl, control = latexBibControlFile(root, "main")
	if tl != "biber" || filepath.Base(control) != "main.bcf" {
		t.Errorf("a .bcf must win: got %q %q", tl, control)
	}
}

func TestLatexBibEnvKeepsHardening(t *testing.T) {
	base := latexHardenedEnv([]string{"PATH=/usr/bin", "BIBINPUTS=/evil"})
	env := latexBibEnv(base, "/ws/src", "/ws/out")

	for _, want := range []string{"openin_any=p", "openout_any=p", "shell_escape=f", "PATH=/usr/bin"} {
		if !containsString(env, want) {
			t.Errorf("latexBibEnv dropped %q: %v", want, env)
		}
	}
	if containsString(env, "BIBINPUTS=/evil") {
		t.Errorf("an inherited BIBINPUTS must be replaced, not shadowed: %v", env)
	}
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "BIBINPUTS=") {
			n++
			if !strings.Contains(kv, "/ws/src") || !strings.Contains(kv, "/ws/out") {
				t.Errorf("BIBINPUTS should cover both directories, got %q", kv)
			}
		}
	}
	if n != 1 {
		t.Errorf("expected exactly one BIBINPUTS entry, got %d in %v", n, env)
	}
}

// ─── end-to-end over fake TeX binaries ───────────────────────────────────────
//
// latex_build's bibliography handling is about which subprocesses run, in what
// order, and what is checked before each. That is fully observable with stub
// binaries on PATH, so these tests need no TeX installation at all — unlike the
// live-compiler tier in latex_test.go, which is about what a real compiler is
// allowed to read.

const fakeLatexScript = `#!/bin/sh
outdir=""
tex=""
for a in "$@"; do
  case "$a" in
    -output-directory=*) outdir=${a#-output-directory=} ;;
    -*) ;;
    *) tex=$a ;;
  esac
done
[ -n "$outdir" ] || outdir=$(dirname "$tex")
base=$(basename "$tex" .tex)

n=0
[ -f "$FAKE_LATEX_PASSES" ] && n=$(cat "$FAKE_LATEX_PASSES")
n=$((n+1))
echo "$n" > "$FAKE_LATEX_PASSES"

[ -n "$FAKE_LATEX_BCF" ] && printf '%s' "$FAKE_LATEX_BCF" > "$outdir/$base.bcf"
[ -n "$FAKE_LATEX_AUX" ] && printf '%s' "$FAKE_LATEX_AUX" > "$outdir/$base.aux"

if [ -n "$FAKE_LATEX_FAIL_PASS" ] && [ "$n" = "$FAKE_LATEX_FAIL_PASS" ]; then
  echo "! Undefined control sequence."
  echo "AEGIS-FAKE-PASS-$n-FAILED"
  exit 1
fi
echo "Output written on $base.pdf (1 pages, 100 bytes)."
exit 0
`

const fakeBibScript = `#!/bin/sh
echo "$FAKE_BIB_NAME $*" >> "$FAKE_BIB_LOG"
exit 0
`

// fakeTeX installs stub xelatex/biber/bibtex binaries at the front of PATH and
// returns the temp directory holding the bookkeeping files they write.
func fakeTeX(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake TeX binaries are POSIX shell scripts")
	}
	bin := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(bin, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("xelatex", fakeLatexScript)
	write("biber", "#!/bin/sh\nFAKE_BIB_NAME=biber\n"+strings.TrimPrefix(fakeBibScript, "#!/bin/sh\n"))
	write("bibtex", "#!/bin/sh\nFAKE_BIB_NAME=bibtex\n"+strings.TrimPrefix(fakeBibScript, "#!/bin/sh\n"))

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LATEX_PASSES", filepath.Join(bin, "passes"))
	t.Setenv("FAKE_BIB_LOG", filepath.Join(bin, "bibruns"))
	t.Setenv("FAKE_LATEX_BCF", "")
	t.Setenv("FAKE_LATEX_AUX", "")
	t.Setenv("FAKE_LATEX_FAIL_PASS", "")
	return bin
}

func fakePasses(t *testing.T, bin string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(bin, "passes"))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("unreadable pass counter %q: %v", data, err)
	}
	return n
}

func fakeBibRuns(t *testing.T, bin string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(bin, "bibruns"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// bibWorkspace writes a citation-bearing document whose preamble line is caller
// supplied, so the same helper serves the detected/forced/suppressed cases.
func bibWorkspace(t *testing.T, preamble string) string {
	t.Helper()
	root := t.TempDir()
	writeLatexFile(t, root, "references.bib", "@misc{key, title={A Title}, year={2026}}\n")
	writeLatexFile(t, root, "main.tex", "\\documentclass{article}\n"+preamble+
		"\n\\begin{document}\nText \\cite{key}.\n\\end{document}\n")
	return root
}

func TestLatexBuildAutoRunsBiberWhenSourceDeclaresBibliography(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_BCF", bcfWithDatasources("references.bib"))
	root := bibWorkspace(t, `\addbibresource{references.bib}`)

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("build should succeed: %s", res.Content)
	}

	runs := fakeBibRuns(t, bin)
	if len(runs) != 1 || !strings.HasPrefix(runs[0], "biber ") {
		t.Fatalf("expected exactly one biber run, got %v", runs)
	}
	if !strings.Contains(runs[0], "main") {
		t.Errorf("biber should be handed the jobname, got %q", runs[0])
	}
	// runs=1 was requested, but a bib pass forces two further compiler passes.
	if got := fakePasses(t, bin); got != 3 {
		t.Errorf("expected 3 compiler passes after the bib pass, got %d", got)
	}
	if !strings.Contains(res.Content, "biber ran over main.bcf") {
		t.Errorf("report should mention the bib pass, got:\n%s", res.Content)
	}
}

func TestLatexBuildAutoRunsBibtexForPlainBibliography(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_AUX", "\\citation{key}\n\\bibstyle{plain}\n\\bibdata{references}\n")
	root := bibWorkspace(t, "\\bibliographystyle{plain}\n\\bibliography{references}")

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("build should succeed: %s", res.Content)
	}
	runs := fakeBibRuns(t, bin)
	if len(runs) != 1 || !strings.HasPrefix(runs[0], "bibtex ") {
		t.Fatalf("expected exactly one bibtex run, got %v", runs)
	}
	if got := fakePasses(t, bin); got != 3 {
		t.Errorf("expected 3 compiler passes after the bib pass, got %d", got)
	}
}

func TestLatexBuildBibFalseSuppressesTheBibPass(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_BCF", bcfWithDatasources("references.bib"))
	root := bibWorkspace(t, `\addbibresource{references.bib}`)

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 2, "bib": false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("build should succeed: %s", res.Content)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 0 {
		t.Fatalf("bib:false must suppress the bib tool, got %v", runs)
	}
	if got := fakePasses(t, bin); got != 2 {
		t.Errorf("expected the requested 2 passes, got %d", got)
	}
}

func TestLatexBuildBibTrueForcesTheBibPass(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_BCF", bcfWithDatasources("references.bib"))
	// No \addbibresource anywhere — auto-detection would find nothing.
	root := bibWorkspace(t, `\usepackage{geometry}`)
	if tex := filepath.Join(root, "main.tex"); latexSourceDeclaresBibliography(soleRoot(root), tex) {
		t.Fatal("test setup: the source must not declare a bibliography")
	}

	tl := &latexBuildTool{root: root}
	res, err := tl.Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 1, "bib": true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("build should succeed: %s", res.Content)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 1 {
		t.Fatalf("bib:true must force the bib tool, got %v", runs)
	}
}

// Auto-detection must not fire on the commented-out block latex_new_document
// scaffolds into every generated preamble.
func TestLatexBuildSkipsBibForUndeclaredDocument(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_BCF", bcfWithDatasources("references.bib"))
	root := bibWorkspace(t, "%% \\usepackage[backend=biber]{biblatex}\n%% \\addbibresource{references.bib}")

	if _, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 1,
	})); err != nil {
		t.Fatal(err)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 0 {
		t.Fatalf("a commented-out bibliography must not trigger a bib pass, got %v", runs)
	}
}

// bib:true with nothing for the tool to read is reported, not silently ignored.
func TestLatexBuildBibTrueWithoutControlFile(t *testing.T) {
	bin := fakeTeX(t)
	root := bibWorkspace(t, `\usepackage{geometry}`)

	res, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 1, "bib": true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 0 {
		t.Fatalf("no control file means no bib tool, got %v", runs)
	}
	if !strings.Contains(res.Content, "no .bcf/.aux") {
		t.Errorf("expected a skipped-bib note, got:\n%s", res.Content)
	}
}

// The confinement test that matters: the .bcf names a resource outside the
// workspace, and biber must never be started.
func TestLatexBuildRefusesOutOfWorkspaceBCFResource(t *testing.T) {
	bin := fakeTeX(t)
	outside := t.TempDir()
	writeLatexFile(t, outside, "secret.bib", "@misc{leak, title={leak}}\n")
	t.Setenv("FAKE_LATEX_BCF", bcfWithDatasources(filepath.Join(outside, "secret.bib")))
	// The source itself is clean — only the generated control file escapes.
	root := bibWorkspace(t, `\addbibresource{references.bib}`)

	res, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected a refusal, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "refusing to run biber") {
		t.Errorf("expected a biber refusal message, got:\n%s", res.Content)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 0 {
		t.Fatalf("biber must not run when the .bcf escapes the workspace, got %v", runs)
	}
	// Refused before the second compiler pass, too.
	if got := fakePasses(t, bin); got != 1 {
		t.Errorf("expected the build to stop after pass 1, got %d passes", got)
	}
}

func TestLatexBuildRefusesOutOfWorkspaceAUXResource(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_AUX", "\\citation{key}\n\\bibdata{/etc/passwd}\n")
	root := bibWorkspace(t, "\\bibliography{references}")

	res, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content, "refusing to run bibtex") {
		t.Fatalf("expected a bibtex refusal, got: %s", res.Content)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 0 {
		t.Fatalf("bibtex must not run when the .aux escapes the workspace, got %v", runs)
	}
}

// ─── multi-pass failure reporting (P52.10) ───────────────────────────────────

// A pass that fails mid-sequence must be the one reported. Before the fix the
// loop kept going, so pass 3's successful log overwrote pass 2's failure and
// the tool reported BUILD SUCCESS for a build that had errored.
func TestLatexBuildReportsTheFailingPassNotTheLast(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_FAIL_PASS", "2")
	root := bibWorkspace(t, `\usepackage{geometry}`)

	res, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 3,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("a failing pass must fail the build, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "AEGIS-FAKE-PASS-2-FAILED") {
		t.Errorf("the failing pass's log must be the one reported, got:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "BUILD SUCCESS") {
		t.Errorf("a later successful pass must not mask the failure:\n%s", res.Content)
	}
	if got := fakePasses(t, bin); got != 2 {
		t.Errorf("the sequence should stop at the failing pass, got %d passes", got)
	}
}

// The first-pass abort still holds.
func TestLatexBuildAbortsOnFirstPassFailure(t *testing.T) {
	bin := fakeTeX(t)
	t.Setenv("FAKE_LATEX_FAIL_PASS", "1")
	t.Setenv("FAKE_LATEX_BCF", bcfWithDatasources("references.bib"))
	root := bibWorkspace(t, `\addbibresource{references.bib}`)

	res, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
		"path": "main.tex", "compiler": "xelatex", "runs": 3,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected failure, got: %s", res.Content)
	}
	if got := fakePasses(t, bin); got != 1 {
		t.Errorf("expected a single pass, got %d", got)
	}
	if runs := fakeBibRuns(t, bin); len(runs) != 0 {
		t.Errorf("no bib pass after a failed first pass, got %v", runs)
	}
}

// ─── live compiler tier (skipped when the real tools are absent) ─────────────

// TestLatexBuildResolvesCitationsLive is the claim the stub tests cannot make:
// that the bib pass, as wired here, actually resolves \cite keys against a real
// biber/bibtex. Before P52.10 both sub-cases produced a PDF carrying
// "Citation ... undefined" and an empty bibliography.
func TestLatexBuildResolvesCitationsLive(t *testing.T) {
	compiler := latexCompiler(t)

	const bib = "@misc{aegis2026,\n  title  = {A Cited Work},\n  author = {Doe, Jane},\n  year   = {2026}\n}\n"

	cases := []struct {
		name     string
		bibTool  string
		preamble string
		body     string
	}{
		{
			name:     "biber",
			bibTool:  "biber",
			preamble: "\\usepackage[backend=biber,style=numeric]{biblatex}\n\\addbibresource{refs.bib}",
			body:     "Hello \\cite{aegis2026}.\n\\printbibliography",
		},
		{
			name:     "bibtex",
			bibTool:  "bibtex",
			preamble: "",
			body:     "Hello \\cite{aegis2026}.\n\\bibliographystyle{plain}\n\\bibliography{refs}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.bibTool); err != nil {
				t.Skipf("%s not on PATH", tc.bibTool)
			}
			root := t.TempDir()
			writeLatexFile(t, root, "refs.bib", bib)
			writeLatexFile(t, root, "main.tex", "\\documentclass{article}\n"+tc.preamble+
				"\n\\begin{document}\n"+tc.body+"\n\\end{document}\n")

			res, err := (&latexBuildTool{root: root}).Execute(context.Background(), latexInput(t, map[string]any{
				"path": "main.tex", "compiler": compiler, "runs": 1,
			}))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(res.Content, "not found") && strings.Contains(res.Content, ".sty") {
				t.Skipf("required LaTeX package missing: %s", res.Content)
			}
			if res.IsError {
				t.Fatalf("build failed: %s", res.Content)
			}
			if !strings.Contains(res.Content, tc.bibTool+" ran over") {
				t.Errorf("expected %s to have run, got:\n%s", tc.bibTool, res.Content)
			}
			if strings.Contains(res.Content, "Citation") && strings.Contains(res.Content, "undefined") {
				t.Errorf("citations still unresolved:\n%s", res.Content)
			}
			if strings.Contains(res.Content, "Empty bibliography") {
				t.Errorf("bibliography still empty:\n%s", res.Content)
			}
			if _, err := os.Stat(filepath.Join(root, "main.bbl")); err != nil {
				t.Errorf("expected a .bbl from the bib pass: %v", err)
			}
		})
	}
}

// ─── warning cap ─────────────────────────────────────────────────────────────

func TestParseLatexLogWarningCap(t *testing.T) {
	var b strings.Builder
	const total = latexMaxWarnings + 7
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, "LaTeX Warning: Reference 'sec:n%d' on page 1 undefined.\n", i)
	}
	// A duplicate must not inflate the dropped count.
	b.WriteString("LaTeX Warning: Reference 'sec:n0' on page 1 undefined.\n")
	b.WriteString("Output written on doc.pdf (2 pages, 100 bytes).\n")

	s := parseLatexLog(b.String(), "doc.pdf", false)
	if len(s.warnings) != latexMaxWarnings+1 {
		t.Fatalf("expected %d warnings + 1 summary line, got %d: %v",
			latexMaxWarnings, len(s.warnings), s.warnings)
	}
	last := s.warnings[len(s.warnings)-1]
	want := fmt.Sprintf("… and %d more warnings", total-latexMaxWarnings)
	if !strings.Contains(last, want) {
		t.Errorf("expected %q in the summary line, got %q", want, last)
	}
}

// Exactly at the cap there is nothing left over, so no summary line is added —
// the case the old `len(s.warnings) == 15` comparison got wrong once the
// summary line itself had been appended.
func TestParseLatexLogWarningCapExact(t *testing.T) {
	var b strings.Builder
	for i := 0; i < latexMaxWarnings; i++ {
		fmt.Fprintf(&b, "LaTeX Warning: Reference 'sec:n%d' on page 1 undefined.\n", i)
	}
	s := parseLatexLog(b.String(), "doc.pdf", false)
	if len(s.warnings) != latexMaxWarnings {
		t.Fatalf("expected exactly %d warnings, got %d: %v", latexMaxWarnings, len(s.warnings), s.warnings)
	}
	for _, w := range s.warnings {
		if strings.Contains(w, "and 0 more") || strings.HasPrefix(w, "…") {
			t.Errorf("unexpected summary line at the cap: %q", w)
		}
	}
}
