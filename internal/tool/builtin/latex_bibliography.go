package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// ─── bibliography pass (P52.10) ───────────────────────────────────────────────
//
// Why there is no latexmk path here, despite it being the roadmap's first
// preference. latexmk does compute the compile/bib/index fixpoint correctly,
// and one of the two objections to it is answerable: it evaluates
// `./latexmkrc`, `./.latexmkrc` and `~/.latexmkrc` as *Perl*, which over a
// model-authored workspace is arbitrary code execution that no TeX source scan
// can see — but `-norc` suppresses exactly that.
//
// The objection that is not answerable is where the confinement check has to
// sit. latexmk decides for itself, mid-run, when to invoke biber/bibtex, and it
// invokes them over the `.bcf`/`.aux` it has just generated. checkLatexBibConfinement
// must run between those two events, and latexmk exposes no seam there: its only
// interposition point is the `$biber`/`$bibtex` command *string*, so honouring
// it would mean shipping a separate wrapper executable that re-implements the
// check out of process — a second binary and a second trust boundary bolted on
// to reach a fixpoint this tool does not otherwise need. Running latexmk with
// its bib step disabled and doing the bib pass out here leaves only its rerun
// heuristic, at the cost of a third external tool on the critical path plus a
// flag-translation layer to keep the P52.2 hardening (`-no-shell-escape` and
// friends) reaching the compiler. Neither trade is worth it.
//
// So the hand-rolled loop above stays, extended to run the bib tool itself,
// with the check in the one place it can be enforced. It computes the same
// fixpoint latexmk would for the ordinary case (pass → bib → two more passes);
// what it does not do is iterate to convergence for a document that needs a
// fourth pass. That is the residual gap, and it surfaces as LaTeX's own
// "Rerun to get cross-references right" warning in the build report rather than
// as silent breakage — the model can raise `runs` and rebuild.

// latexBibControlFile reports which bibliography tool the just-compiled
// document needs and the absolute path of the control file it would read.
// Detection is from the generated artefacts rather than the source, because
// they are what the tool actually consumes: biblatex writes a `.bcf` for biber,
// and plain bibtex leaves `\bibdata` in the `.aux`. An empty tool name means
// the document produced no bibliography data at all.
func latexBibControlFile(outDir, jobname string) (bibTool, control string) {
	bcf := filepath.Join(outDir, jobname+".bcf")
	if info, err := os.Stat(bcf); err == nil && !info.IsDir() {
		return "biber", bcf
	}
	aux := filepath.Join(outDir, jobname+".aux")
	if data, err := os.ReadFile(aux); err == nil && latexAuxBibDataRE.Match(data) {
		return "bibtex", aux
	}
	return "", ""
}

var (
	// biblatex's control file lists its inputs as
	// `<bcf:datasource type="file" datatype="bibtex">refs.bib</bcf:datasource>`.
	// The namespace prefix is not fixed, so it is matched loosely.
	latexBCFDatasourceRE = regexp.MustCompile(`(?s)<(?:[A-Za-z0-9_.-]+:)?datasource\b[^>]*>([^<]*)<`)
	// bibtex reads these three from the .aux it is handed.
	latexAuxBibDataRE  = regexp.MustCompile(`\\bibdata\s*\{([^{}]*)\}`)
	latexAuxBibStyleRE = regexp.MustCompile(`\\bibstyle\s*\{([^{}]*)\}`)
	latexAuxInputRE    = regexp.MustCompile(`\\@input\s*\{([^{}]*)\}`)
	// A datasource biber would fetch rather than open.
	latexRemoteResourceRE = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
)

const latexBibScanMaxFiles = 64

// checkLatexBibConfinement validates every resource the generated control file
// names, before biber/bibtex is allowed to read it.
//
// This is the second half of the P52.2 boundary. That scan covers the
// *source*-level `\addbibresource{...}` / `\bibliography{...}` arguments, which
// is enough for the LaTeX process itself — but the bib tool does not read the
// source. It reads the `.bcf` (biblatex) or the `.aux` (bibtex), and those are
// generated files whose resource lists can name paths no `\addbibresource` in
// the source ever spelled out: biblatex expands macros and `\jobname` before
// writing the `.bcf`, and an `.aux` accumulates `\bibdata` entries from every
// included file plus `\@input` chains to further `.aux` files.
//
// Each name is validated through sandbox.ValidatePath (via resolvePath) against
// both directories the tool could resolve it from — the .tex's own directory
// and the output directory — so a name that escapes from either is refused.
func checkLatexBibConfinement(roots []sandbox.Root, texDir, outDir, control string) []string {
	// roots and the bases must share one namespace before anything is compared;
	// see the note in latexWalkSources.
	resolve := func(p string) string {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return real
		}
		return p
	}
	roots = resolvedRoots(roots)
	texDir, outDir = resolve(texDir), resolve(outDir)
	bases := []string{texDir, outDir}
	if texDir == outDir {
		bases = bases[:1]
	}

	var out []string
	reported := make(map[string]bool)
	flag := func(where, kind, raw, why string) {
		msg := fmt.Sprintf("%s: %s %q %s", filepath.Base(where), kind, raw, why)
		if !reported[msg] {
			reported[msg] = true
			out = append(out, msg)
		}
	}
	check := func(where, kind, raw string, resolveFrom []string) bool {
		bad := false
		for _, b := range resolveFrom {
			cand, ok := latexResolveRef(b, raw)
			if !ok {
				continue
			}
			if _, err := sandbox.ValidatePathIn(roots, cand, sandbox.AccessRead); err != nil {
				bad = true
			}
		}
		if bad {
			flag(where, kind, raw, "resolves outside the workspace root")
		}
		return bad
	}

	seen := make(map[string]bool)
	queue := []string{control}
	for len(queue) > 0 && len(seen) < latexBibScanMaxFiles {
		cur := queue[0]
		queue = queue[1:]
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			cur = real
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true

		info, err := os.Stat(cur)
		if err != nil || info.IsDir() || info.Size() > latexScanMaxBytes {
			continue
		}
		data, err := os.ReadFile(cur)
		if err != nil {
			continue
		}
		src := string(data)

		if strings.EqualFold(filepath.Ext(cur), ".bcf") {
			for _, m := range latexBCFDatasourceRE.FindAllStringSubmatch(src, -1) {
				for _, raw := range splitLatexList(m[1]) {
					// biblatex can declare a remote datasource, which biber
					// fetches over the network. latex_build has no network
					// capability, and a URL is not a path the validator can
					// reason about — refuse it outright.
					if latexRemoteResourceRE.MatchString(raw) {
						flag(cur, "remote datasource", raw, "would be fetched over the network")
						continue
					}
					check(cur, "datasource", raw, bases)
				}
			}
			continue
		}

		// .aux — bibtex's own inputs, plus the chain of included .aux files.
		for _, m := range latexAuxBibDataRE.FindAllStringSubmatch(src, -1) {
			for _, raw := range splitLatexList(m[1]) {
				check(cur, "\\bibdata", raw, bases)
			}
		}
		for _, m := range latexAuxBibStyleRE.FindAllStringSubmatch(src, -1) {
			for _, raw := range splitLatexList(m[1]) {
				check(cur, "\\bibstyle", raw, bases)
			}
		}
		// An .aux always lives in the output directory, so that is the only
		// base a nested \@input can be resolved from.
		for _, m := range latexAuxInputRE.FindAllStringSubmatch(src, -1) {
			for _, raw := range splitLatexList(m[1]) {
				if check(cur, "\\@input", raw, []string{outDir}) {
					continue
				}
				if cand, ok := latexResolveRef(outDir, raw); ok {
					if abs, err := sandbox.ValidatePathIn(roots, cand, sandbox.AccessRead); err == nil {
						queue = append(queue, abs)
					}
				}
			}
		}
	}
	return out
}

// splitLatexList splits a comma-separated braced argument into its non-empty
// entries. `\bibdata{a,b}` and a globbed datasource list both take this form.
func splitLatexList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if t := strings.TrimSpace(item); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// runLatexBibTool invokes biber or bibtex over the job's control file. It runs
// in the output directory (where the .bcf/.aux live and where the .bbl must be
// written) with BIBINPUTS/BSTINPUTS pointed at the source directory too, so a
// `references.bib` sitting next to the .tex is still found when output_dir
// moves the build artefacts elsewhere.
func runLatexBibTool(ctx context.Context, binPath, bibTool, jobname, texDir, outDir string, env []string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	args := []string{jobname}
	if bibTool == "biber" {
		// biber's first config-file location is `biber.conf` in the current
		// directory — which is inside the workspace, i.e. a file the model can
		// write. --noconf drops config discovery entirely so the run is
		// determined by the .bcf this tool just validated and nothing else.
		// The cost is that a human's ~/.biber.conf is ignored here too; for a
		// confined, agent-driven build that is the right trade.
		args = append([]string{"--noconf"}, args...)
	}

	var buf bytes.Buffer
	cmd := exec.CommandContext(runCtx, binPath, args...)
	cmd.Dir = outDir
	cmd.Env = latexBibEnv(env, texDir, outDir)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// latexBibEnv adds the kpathsea search paths the bib tools use, keeping the
// hardened openin_any/openout_any/shell_escape values from base intact. A
// trailing separator asks kpathsea to append its own defaults, so the standard
// .bst files stay reachable.
func latexBibEnv(base []string, texDir, outDir string) []string {
	sep := string(os.PathListSeparator)
	search := texDir + sep + outDir + sep
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "BIBINPUTS="), strings.HasPrefix(kv, "BSTINPUTS="):
			continue
		}
		out = append(out, kv)
	}
	return append(out, "BIBINPUTS="+search, "BSTINPUTS="+search)
}

// firstLatexBibLine picks a short excerpt of a failing bib tool's output for
// the build report.
func firstLatexBibLine(log string) string {
	for _, line := range strings.Split(log, "\n") {
		tr := strings.TrimSpace(line)
		if tr == "" {
			continue
		}
		if len(tr) > 140 {
			tr = tr[:140] + "…"
		}
		return ": " + tr
	}
	return ""
}

// latexBibDeclRE matches the source-level declarations that mean "this document
// has a bibliography". `\bibliographystyle` and `\printbibliography` are
// deliberately not matches: neither names a data source on its own.
var latexBibDeclRE = regexp.MustCompile(`\\(?:addbibresource|addglobalbib|bibliography)\s*(?:\[[^\]]*\])?\s*\{`)

// latexSourceDeclaresBibliography reports whether texAbs, or any in-workspace
// TeX source it pulls in, declares a bibliography. Comments and verbatim blocks
// are already stripped by the walker, so a commented-out `\addbibresource` (the
// exact shape latex_new_document scaffolds) does not trigger a bib pass.
func latexSourceDeclaresBibliography(roots []sandbox.Root, texAbs string) bool {
	found := false
	latexWalkSources(roots, texAbs, func(_, src string) {
		if !found && latexBibDeclRE.MatchString(src) {
			found = true
		}
	}, nil)
	return found
}
