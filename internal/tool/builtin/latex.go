package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/tool"
)

// ─── latex_build ──────────────────────────────────────────────────────────────

type latexBuildTool struct{ root string }

func (t *latexBuildTool) Name() string                { return "latex_build" }
func (t *latexBuildTool) Capability() tool.Capability { return tool.CapExecute }
func (t *latexBuildTool) Description() string {
	return "Compile a LaTeX (.tex) file to PDF using xelatex, pdflatex, or lualatex. " +
		"Runs multiple passes to resolve cross-references and table-of-contents entries. " +
		"Returns a structured build report: errors with context lines, deduplicated warnings, " +
		"page count, and the output PDF path. Use check_only for a fast syntax check."
}
func (t *latexBuildTool) InputSchema() json.RawMessage {
	return schema(`{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"workspace-relative path to the .tex file"},
			"compiler":{"type":"string","enum":["xelatex","pdflatex","lualatex"],"description":"LaTeX compiler (default: xelatex)"},
			"runs":{"type":"integer","description":"compiler passes to resolve references and TOC (1–3, default 2)"},
			"check_only":{"type":"boolean","description":"draft-mode syntax check — detects errors without writing a PDF"},
			"output_dir":{"type":"string","description":"workspace-relative directory for output files (default: same folder as the .tex file)"}
		},
		"required":["path"]
	}`)
}

func (t *latexBuildTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path      string `json:"path"`
		Compiler  string `json:"compiler"`
		Runs      int    `json:"runs"`
		CheckOnly bool   `json:"check_only"`
		OutputDir string `json:"output_dir"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.Compiler == "" {
		args.Compiler = "xelatex"
	}
	if args.Runs < 1 || args.Runs > 3 {
		args.Runs = 2
	}
	if args.CheckOnly {
		args.Runs = 1
	}

	root := effectiveRoot(ctx, t.root)
	texAbs, err := resolvePath(root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if _, statErr := os.Stat(texAbs); os.IsNotExist(statErr) {
		return tool.Result{Content: "file not found: " + args.Path, IsError: true}, nil
	}

	texDir := filepath.Dir(texAbs)
	outDir := texDir
	if args.OutputDir != "" {
		outDir, err = resolvePath(root, args.OutputDir)
		if err != nil {
			return tool.Result{}, err
		}
		if err := os.MkdirAll(outDir, 0o750); err != nil {
			return tool.Result{Content: "cannot create output dir: " + err.Error(), IsError: true}, nil
		}
	}

	// A TeX run over model-authored source is an execution boundary, not just a
	// build step — refuse it before spending a subprocess if the source reaches
	// outside the workspace (P52.2).
	if violations := checkLatexConfinement(root, texAbs); len(violations) > 0 {
		return tool.Result{
			Content: "refusing to compile — the source reads files outside the workspace:\n  · " +
				strings.Join(violations, "\n  · ") +
				"\n\nlatex_build is confined to the workspace root. Copy the file into the " +
				"workspace and reference it with a workspace-relative path, then retry.",
			IsError: true,
		}, nil
	}

	compPath, lookErr := exec.LookPath(args.Compiler)
	if lookErr != nil {
		return tool.Result{
			Content: fmt.Sprintf(
				"compiler %q not found in PATH.\n\nInstall a LaTeX distribution:\n"+
					"  • TeX Live (Linux/Mac): https://tug.org/texlive/\n"+
					"  • MiKTeX (Windows):     https://miktex.org/\n"+
					"  • Homebrew (Mac):        brew install --cask mactex\n\n"+
					"Then run: tlmgr install %s",
				args.Compiler, args.Compiler,
			),
			IsError: true,
		}, nil
	}

	flags := latexHardenedFlags(outDir, texAbs, args.CheckOnly)
	env := latexHardenedEnv(os.Environ())

	var lastLog string
	var runErr error
	for pass := 0; pass < args.Runs; pass++ {
		runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		var buf bytes.Buffer
		cmd := exec.CommandContext(runCtx, compPath, flags...)
		cmd.Dir = texDir
		cmd.Env = env
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		runErr = cmd.Run()
		cancel()
		lastLog = buf.String()
		if runErr != nil && pass == 0 {
			break // abort multi-pass early on first-pass failure
		}
	}

	// Derive workspace-relative PDF path for the summary.
	base := strings.TrimSuffix(filepath.Base(texAbs), ".tex")
	pdfRel, _ := filepath.Rel(root, filepath.Join(outDir, base+".pdf"))

	summary := parseLatexLog(lastLog, pdfRel, args.CheckOnly)
	if runErr != nil && len(summary.errors) == 0 {
		summary.errors = append(summary.errors, "compiler exited: "+runErr.Error())
	}

	return tool.Result{
		Content: formatBuildResult(summary, args.Compiler, args.Runs),
		IsError: !summary.success,
	}, nil
}

// ─── workspace confinement (P52.2) ────────────────────────────────────────────
//
// A TeX compiler is a general-purpose interpreter with file I/O attached, so
// running one over source the model itself authored is an execution boundary,
// not a build step. Two layers keep it inside the workspace root:
//
//  1. Process hardening (latexHardenedFlags / latexHardenedEnv) —
//     `-no-shell-escape` closes the restricted `\write18` whitelist, and
//     `openin_any`/`openout_any=p` (paranoid) ask TeX itself to refuse dot
//     files, parent directories and absolute paths.
//  2. A static scan of the source, and of the sources it pulls in, for file
//     references that resolve outside the workspace root — the same
//     `sandbox.ValidatePath` boundary every other file-touching builtin uses.
//
// Layer 2 exists because the read half of layer 1 is no longer dependable: as
// of TeX Live 2026 `openin_any` is a documented upstream no-op (kpathsea's
// `kpse_in_name_ok` and friends always return true), so on a current
// distribution nothing but this scan stops a model-authored
// `\input{~/.ssh/id_rsa}` from being typeset into the PDF. The environment
// variables are still set — they are honoured by TeX Live 2025 and earlier and
// by MiKTeX, and they cost nothing.
//
// The scan is a heuristic layered on a hardened process, not a sandbox: TeX can
// assemble filenames at run time from macros and this cannot evaluate those. It
// does catch every *literal* path escape, which is the shape reading a host file
// actually takes.

// latexHardenedFlags builds the compiler invocation for one pass.
// `-no-shell-escape` is placed first so it can never be mistaken for the input
// filename, which is always last.
func latexHardenedFlags(outDir, texAbs string, checkOnly bool) []string {
	flags := []string{
		"-no-shell-escape",
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-output-directory=" + outDir,
	}
	if checkOnly {
		flags = append(flags, "-draftmode")
	}
	return append(flags, texAbs)
}

// latexHardenedEnv returns base with TeX's own file-access settings pinned to
// paranoid. Any inherited value is dropped rather than shadowed, so the result
// carries exactly one entry per key regardless of the host's environment.
func latexHardenedEnv(base []string) []string {
	out := make([]string, 0, len(base)+3)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "openin_any="),
			strings.HasPrefix(kv, "openout_any="),
			strings.HasPrefix(kv, "shell_escape="):
			continue
		}
		out = append(out, kv)
	}
	return append(out, "openin_any=p", "openout_any=p", "shell_escape=f")
}

// latexReadCommands maps the LaTeX/TeX commands whose braced argument names a
// file to whether that file is itself TeX source worth following.
var latexReadCommands = map[string]bool{
	"input":             true,
	"include":           true,
	"InputIfFileExists": true,
	"subfile":           true,
	"subfileinclude":    true,
	"usepackage":        true,
	"RequirePackage":    true,
	"documentclass":     true,
	"LoadClass":         true,
	"includeonly":       false,
	"IfFileExists":      false,
	"lstinputlisting":   false,
	"verbatiminput":     false,
	"includegraphics":   false,
	"includepdf":        false,
	"addbibresource":    false,
	"addglobalbib":      false,
	"bibliography":      false,
	"bibliographystyle": false,
	"pdfximage":         false,
}

// latexVerbatimEnvs is the alternation of environments whose body is displayed
// verbatim rather than interpreted.
const latexVerbatimEnvs = `(?:verbatim|Verbatim|lstlisting|minted|alltt)`

var (
	// A command, an optional bracketed option list, and one brace-delimited
	// argument containing no nested braces. Names are filtered against
	// latexReadCommands after the match.
	latexBracedRefRE = regexp.MustCompile(`\\([A-Za-z@]+)\s*(?:\[[^\]]*\])?\s*\{([^{}]*)\}`)
	// TeX's brace-less form: `\input /etc/passwd`.
	latexBareInputRE = regexp.MustCompile(`\\(input|include)\s+([^\s{}%\\]+)`)
	// `\openin\stream=path` / `\openin1=path`.
	latexOpenInRE = regexp.MustCompile(`\\openin\s*[0-9]*\s*(?:\\[A-Za-z@]+\s*)?=\s*([^\s{}%\\]+)`)
	// import's two-argument form: a directory then a file.
	latexImportRE = regexp.MustCompile(`\\(?:sub)?(?:import|inputfrom|includefrom)\*?\s*\{([^{}]*)\}\s*\{([^{}]*)\}`)
	// `\graphicspath{{dir/}{other/}}` adds search roots for \includegraphics.
	latexGraphicsPathRE = regexp.MustCompile(`(?s)\\graphicspath\s*\{((?:\s*\{[^{}]*\}\s*)+)\}`)
	latexBraceItemRE    = regexp.MustCompile(`\{([^{}]*)\}`)
	// Blocks whose contents are shown, not executed — a report that quotes
	// `\input{/etc/passwd}` in a listing must still build.
	// RE2 has no backreferences, so the closing tag is any verbatim-like \end
	// rather than the matching one — good enough to skip the block's contents.
	latexVerbatimRE = regexp.MustCompile(`(?s)\\begin\{` + latexVerbatimEnvs + `\*?\}.*?\\end\{` + latexVerbatimEnvs + `\*?\}`)
)

const (
	latexScanMaxFiles = 128
	latexScanMaxBytes = 4 << 20
)

// latexFileRef is one file reference found in TeX source.
type latexFileRef struct {
	cmd     string
	arg     string
	recurse bool
}

// checkLatexConfinement scans texAbs, and every in-workspace TeX source it
// pulls in, for file references that resolve outside root. It returns one
// human-readable message per distinct violation; an empty slice means the
// document reads only from inside the workspace.
func checkLatexConfinement(root, texAbs string) []string {
	// The paths in flight here are already symlink-resolved (they come back out
	// of resolvePath), so resolve root the same way before comparing: on macOS a
	// workspace under /tmp or /var is reached through a symlink, and validating
	// a /private/... path against the unresolved root would flag the document's
	// own chapters as escapes.
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}

	var out []string
	reported := make(map[string]bool)
	seen := make(map[string]bool)
	queue := []string{texAbs}

	for len(queue) > 0 && len(seen) < latexScanMaxFiles {
		cur := queue[0]
		queue = queue[1:]
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			cur = real // same namespace as root, so relative refs compare cleanly
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
		src := latexVerbatimRE.ReplaceAllString(stripTeXComments(string(data)), "")

		for _, ref := range latexFileRefs(src) {
			cand, ok := latexResolveRef(filepath.Dir(cur), ref.arg)
			if !ok {
				continue
			}
			abs, err := resolvePath(root, cand)
			if err != nil {
				where := cur
				if rel, relErr := filepath.Rel(root, cur); relErr == nil {
					where = rel
				}
				msg := fmt.Sprintf("%s: \\%s{%s} resolves outside the workspace root", where, ref.cmd, ref.arg)
				if !reported[msg] {
					reported[msg] = true
					out = append(out, msg)
				}
				continue
			}
			if ref.recurse {
				queue = append(queue, latexSourceCandidates(abs)...)
			}
		}
	}
	return out
}

// latexFileRefs extracts every file reference from comment-stripped source.
func latexFileRefs(src string) []latexFileRef {
	var refs []latexFileRef
	add := func(cmd, arg string, recurse bool) {
		// Comma lists (\usepackage{a,b}, \bibliography{a,b}) name one file each.
		for _, item := range strings.Split(arg, ",") {
			if strings.TrimSpace(item) != "" {
				refs = append(refs, latexFileRef{cmd: cmd, arg: item, recurse: recurse})
			}
		}
	}

	for _, m := range latexBracedRefRE.FindAllStringSubmatch(src, -1) {
		if recurse, ok := latexReadCommands[m[1]]; ok {
			add(m[1], m[2], recurse)
		}
	}
	for _, m := range latexBareInputRE.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2], true)
	}
	for _, m := range latexOpenInRE.FindAllStringSubmatch(src, -1) {
		add("openin", m[1], false)
	}
	for _, m := range latexImportRE.FindAllStringSubmatch(src, -1) {
		add("import", m[1], false)
		add("import", filepath.Join(m[1], m[2]), true)
	}
	for _, m := range latexGraphicsPathRE.FindAllStringSubmatch(src, -1) {
		for _, item := range latexBraceItemRE.FindAllStringSubmatch(m[1], -1) {
			add("graphicspath", item[1], false)
		}
	}
	return refs
}

// latexResolveRef turns a raw TeX file argument into a path to validate.
// ok is false when the argument names nothing checkable — an empty argument, or
// a name assembled from macros that only the compiler can expand.
func latexResolveRef(baseDir, arg string) (string, bool) {
	arg = strings.Trim(strings.TrimSpace(arg), `"`)
	if arg == "" {
		return "", false
	}
	if strings.HasPrefix(arg, "~") {
		// kpathsea expands a leading tilde, so it is a real escape vector.
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		arg = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(arg, "~"), "/"))
	}
	if filepath.IsAbs(arg) {
		return arg, true
	}
	if strings.ContainsAny(arg, `\#`) {
		return "", false // macro-built and not already rooted — unresolvable here
	}
	return filepath.Join(baseDir, arg), true
}

// latexSourceCandidates returns the existing files abs could name as TeX
// source, honouring TeX's habit of supplying an extension when none is given.
func latexSourceCandidates(abs string) []string {
	exists := func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}
	if ext := strings.ToLower(filepath.Ext(abs)); ext != "" {
		switch ext {
		case ".tex", ".ltx", ".sty", ".cls", ".def", ".clo", ".tikz", ".bib":
			if exists(abs) {
				return []string{abs}
			}
		}
		return nil
	}
	var out []string
	for _, ext := range []string{".tex", ".sty", ".cls"} {
		if exists(abs + ext) {
			out = append(out, abs+ext)
		}
	}
	return out
}

// stripTeXComments removes everything from an unescaped % to end of line, so a
// commented-out reference is not mistaken for a live one.
func stripTeXComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, line := range strings.Split(s, "\n") {
		esc := false
		cut := -1
		for i, r := range line {
			switch {
			case esc:
				esc = false
			case r == '\\':
				esc = true
			case r == '%':
				cut = i
			}
			if cut >= 0 {
				break
			}
		}
		if cut >= 0 {
			line = line[:cut]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// latexLogSummary is parsed from the compiler's stdout/stderr.
type latexLogSummary struct {
	success  bool
	errors   []string
	warnings []string
	pdfPath  string
	pages    int
}

// parseLatexLog extracts errors, warnings, page count, and success status from
// a raw LaTeX compiler log. Deduplicates repeated warnings and caps at 15.
func parseLatexLog(log, pdfPath string, checkOnly bool) latexLogSummary {
	s := latexLogSummary{pdfPath: pdfPath}
	seen := make(map[string]bool)
	lines := strings.Split(log, "\n")

	for i, line := range lines {
		tr := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(tr, "! "):
			// LaTeX error — grab the next non-trivial line for context.
			msg := strings.TrimPrefix(tr, "! ")
			if i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				if next != "" && !strings.HasPrefix(next, "l.") && len(next) < 120 {
					msg += "  →  " + next
				}
			}
			if !seen[msg] {
				seen[msg] = true
				s.errors = append(s.errors, msg)
			}

		case strings.Contains(tr, "Warning:") && !strings.HasPrefix(tr, "%"):
			if !seen[tr] && len(s.warnings) < 15 {
				seen[tr] = true
				s.warnings = append(s.warnings, tr)
			}

		case strings.HasPrefix(tr, "Output written on"):
			s.success = true
			// "Output written on foo.pdf (3 pages, 123 bytes)."
			if idx := strings.Index(tr, "("); idx >= 0 {
				fmt.Sscanf(tr[idx+1:], "%d pages", &s.pages)
			}
		}
	}

	// In check_only mode the compiler doesn't write "Output written on",
	// so infer success from the absence of fatal errors.
	if checkOnly && !strings.Contains(log, "Emergency stop") &&
		!strings.Contains(log, "Fatal error") {
		s.success = len(s.errors) == 0
	}

	if len(s.warnings) == 15 {
		extra := strings.Count(log, "Warning:") - 15
		if extra > 0 {
			s.warnings = append(s.warnings, fmt.Sprintf("… and %d more warnings (see .log file)", extra))
		}
	}
	return s
}

func formatBuildResult(s latexLogSummary, compiler string, runs int) string {
	var b strings.Builder
	if s.success {
		fmt.Fprintf(&b, "BUILD SUCCESS  (%s, %d pass(es))\n", compiler, runs)
		if s.pages > 0 {
			fmt.Fprintf(&b, "Output: %s  (%d pages)\n", s.pdfPath, s.pages)
		} else {
			fmt.Fprintf(&b, "Output: %s\n", s.pdfPath)
		}
	} else {
		fmt.Fprintf(&b, "BUILD FAILED  (%s)\n", compiler)
	}

	if len(s.errors) > 0 {
		fmt.Fprintf(&b, "\n%d error(s):\n", len(s.errors))
		for i, e := range s.errors {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, e)
		}
	}
	if len(s.warnings) > 0 {
		fmt.Fprintf(&b, "\n%d warning(s):\n", len(s.warnings))
		for _, w := range s.warnings {
			fmt.Fprintf(&b, "  · %s\n", w)
		}
	}
	if s.success && len(s.errors) == 0 && len(s.warnings) == 0 {
		b.WriteString("Clean build — no errors or warnings.\n")
	}
	return b.String()
}

// ─── latex_new_document ───────────────────────────────────────────────────────

type latexNewDocumentTool struct{ root string }

func (t *latexNewDocumentTool) Name() string                { return "latex_new_document" }
func (t *latexNewDocumentTool) Capability() tool.Capability { return tool.CapWrite }
func (t *latexNewDocumentTool) Description() string {
	return "Create a new LaTeX document (.tex) with a production-quality preamble ready for " +
		"enterprise reports, white papers, and technical documents. The generated file includes " +
		"professional typography, semantic heading colours, tables, code listings, callout boxes, " +
		"figure captions, hyperlinks with PDF metadata, and a scaffolded document structure. " +
		"Fill in section content with write_file or edit_file, then compile with latex_build. " +
		"Ideal starting point when synthesising multiple markdown notes into a formal report."
}
func (t *latexNewDocumentTool) InputSchema() json.RawMessage {
	return schema(`{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"workspace-relative path for the new .tex file (e.g. \"reports/main.tex\")"},
			"title":{"type":"string","description":"document title"},
			"author":{"type":"string","description":"author name(s)"},
			"date":{"type":"string","description":"document date (default: \\today)"},
			"abstract":{"type":"string","description":"executive summary or abstract text to pre-fill"},
			"style":{"type":"string","enum":["report","whitepaper","article","book"],"description":"document style preset (default: report)"},
			"compiler":{"type":"string","enum":["xelatex","pdflatex"],"description":"intended compiler; adjusts font preamble (default: xelatex)"},
			"page_size":{"type":"string","enum":["a4paper","letterpaper"],"description":"paper size (default: a4paper)"},
			"sections":{"type":"array","items":{"type":"string"},"description":"top-level section or chapter titles to pre-scaffold with TODO placeholders"}
		},
		"required":["path","title"]
	}`)
}

func (t *latexNewDocumentTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path     string   `json:"path"`
		Title    string   `json:"title"`
		Author   string   `json:"author"`
		Date     string   `json:"date"`
		Abstract string   `json:"abstract"`
		Style    string   `json:"style"`
		Compiler string   `json:"compiler"`
		PageSize string   `json:"page_size"`
		Sections []string `json:"sections"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.Title == "" {
		return tool.Result{Content: "title is required", IsError: true}, nil
	}
	if args.Author == "" {
		args.Author = "Author"
	}
	if args.Date == "" {
		args.Date = `\today`
	}
	if args.Style == "" {
		args.Style = "report"
	}
	if args.Compiler == "" {
		args.Compiler = "xelatex"
	}
	if args.PageSize == "" {
		args.PageSize = "a4paper"
	}

	abs, err := resolvePath(effectiveRoot(ctx, t.root), args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return tool.Result{Content: "mkdir failed: " + err.Error(), IsError: true}, nil
	}

	content := buildLatexDocument(docParams{
		title:    args.Title,
		author:   args.Author,
		date:     args.Date,
		abstract: args.Abstract,
		style:    args.Style,
		compiler: args.Compiler,
		pageSize: args.PageSize,
		sections: args.Sections,
	})

	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return tool.Result{Content: "write failed: " + err.Error(), IsError: true}, nil
	}

	lines := strings.Count(content, "\n") + 1
	return tool.Result{Content: fmt.Sprintf(
		"Created %s  (%d lines, style=%s, compiler=%s)\n\n"+
			"Next steps:\n"+
			"  1. Fill in section content with write_file or edit_file\n"+
			"  2. Add figures to the same directory and reference with \\includegraphics{}\n"+
			"  3. Compile:  latex_build {\"path\":\"%s\", \"runs\":2}\n\n"+
			"Tip: pass the contents of your markdown notes to the model and ask it to\n"+
			"fill in each section by searching for the %%TODO%% markers.",
		args.Path, lines, args.Style, args.Compiler, args.Path,
	)}, nil
}

// docParams collects template generation arguments.
type docParams struct {
	title, author, date, abstract string
	style, compiler, pageSize     string
	sections                      []string
}

// buildLatexDocument generates the complete .tex source for the requested
// document type. It produces a self-contained file with no external
// .sty dependencies beyond standard TeX Live / MiKTeX distributions.
func buildLatexDocument(p docParams) string {
	isArticle := p.style == "article" || p.style == "whitepaper"
	isBook := p.style == "book"

	docClass := "report"
	if isArticle {
		docClass = "article"
	} else if isBook {
		docClass = "book"
	}

	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	// ── Document class ──────────────────────────────────────────────────────
	w("%% Generated by Aegis  ·  compiler: %s  ·  style: %s\n", p.compiler, p.style)
	w("%% Compile: %s -interaction=nonstopmode -halt-on-error -output-directory=. main.tex\n\n", p.compiler)
	w("\\documentclass[11pt,%s]{%s}\n\n", p.pageSize, docClass)

	// ── Fonts & encoding ────────────────────────────────────────────────────
	w("%%%% ── FONTS & ENCODING ───────────────────────────────────────────────────\n")
	if p.compiler == "pdflatex" {
		w("\\usepackage[T1]{fontenc}\n")
		w("\\usepackage[utf8]{inputenc}\n")
		w("\\usepackage{lmodern}\n")
	} else {
		w("\\usepackage{fontspec}\n")
		w("\\defaultfontfeatures{Ligatures=TeX,Scale=MatchLowercase}\n")
		w("%% Uncomment to override fonts:\n")
		w("%% \\setmainfont{Georgia}\\setsansfont{Helvetica Neue}\\setmonofont{Fira Code}\n")
	}
	w("\n")

	// ── Layout ──────────────────────────────────────────────────────────────
	w("%%%% ── LAYOUT ────────────────────────────────────────────────────────────\n")
	w("\\usepackage[%s,top=2.5cm,bottom=2.5cm,left=3.0cm,right=2.5cm,headheight=14pt]{geometry}\n", p.pageSize)
	w("\\usepackage{microtype}\n")
	w("\\usepackage{setspace}\n")
	w("\\onehalfspacing\n")
	w("\\setlength{\\parindent}{0pt}\n")
	w("\\setlength{\\parskip}{6pt plus 2pt minus 1pt}\n\n")

	// ── Colors ──────────────────────────────────────────────────────────────
	w("%%%% ── COLOURS ───────────────────────────────────────────────────────────\n")
	w("\\usepackage[dvipsnames,svgnames,x11names]{xcolor}\n")
	w("\\definecolor{headblue}{RGB}{30, 58, 95}\n")
	w("\\definecolor{accentblue}{RGB}{52,120,180}\n")
	w("\\definecolor{rulecolor}{RGB}{180,200,220}\n")
	w("\\definecolor{codebg}{RGB}{248,249,250}\n")
	w("\\definecolor{codefg}{RGB}{ 36, 41, 47}\n\n")

	// ── Headings ────────────────────────────────────────────────────────────
	w("%%%% ── HEADINGS ──────────────────────────────────────────────────────────\n")
	w("\\usepackage{titlesec}\n")
	if !isArticle {
		w("\\titleformat{\\chapter}[display]\n")
		w("  {\\color{headblue}\\LARGE\\bfseries\\sffamily}{\\chaptertitlename~\\thechapter}{12pt}\n")
		w("  {\\Huge\\bfseries\\color{headblue}}\n")
		w("\\titlespacing{\\chapter}{0pt}{-20pt}{20pt}\n")
	}
	w("\\titleformat{\\section}{\\color{headblue}\\Large\\bfseries\\sffamily}{\\thesection}{0.8em}{}\n")
	w("\\titleformat{\\subsection}{\\color{headblue}\\large\\bfseries\\sffamily}{\\thesubsection}{0.8em}{}\n")
	w("\\titleformat{\\subsubsection}{\\color{headblue}\\normalsize\\bfseries\\sffamily}{\\thesubsubsection}{0.8em}{}\n\n")

	// ── Headers & footers ───────────────────────────────────────────────────
	w("%%%% ── HEADERS & FOOTERS ─────────────────────────────────────────────────\n")
	w("\\usepackage{fancyhdr}\n")
	w("\\pagestyle{fancy}\\fancyhf{}\n")
	w("\\renewcommand{\\headrulewidth}{0.4pt}\\renewcommand{\\footrulewidth}{0pt}\n")
	w("\\fancyhead[L]{\\small\\sffamily\\color{headblue}\\nouppercase{\\leftmark}}\n")
	w("\\fancyhead[R]{\\small\\sffamily\\color{headblue}\\thepage}\n")
	w("\\fancypagestyle{plain}{\\fancyhf{}\\fancyfoot[C]{\\small\\sffamily\\thepage}\\renewcommand{\\headrulewidth}{0pt}}\n\n")

	// ── Tables ──────────────────────────────────────────────────────────────
	w("%%%% ── TABLES ────────────────────────────────────────────────────────────\n")
	w("\\usepackage{booktabs,tabularx,longtable,array,multirow}\n")
	w("\\renewcommand{\\arraystretch}{1.3}\n\n")

	// ── Figures ─────────────────────────────────────────────────────────────
	w("%%%% ── FIGURES ───────────────────────────────────────────────────────────\n")
	w("\\usepackage{graphicx,float,subcaption,caption}\n")
	w("\\captionsetup{font=small,labelfont={bf,color=headblue},textfont=it}\n\n")

	// ── Code listings ───────────────────────────────────────────────────────
	w("%%%% ── CODE LISTINGS ─────────────────────────────────────────────────────\n")
	w("\\usepackage{listings}\n")
	w("\\lstset{\n")
	w("  backgroundcolor=\\color{codebg},\n")
	w("  basicstyle=\\small\\ttfamily\\color{codefg},\n")
	w("  breaklines=true,captionpos=b,\n")
	w("  commentstyle=\\color{ForestGreen}\\itshape,\n")
	w("  frame=single,framerule=0pt,rulecolor=\\color{rulecolor},\n")
	w("  keywordstyle=\\color{accentblue}\\bfseries,\n")
	w("  numbers=left,numberstyle=\\tiny\\color{gray}\\ttfamily,stepnumber=1,\n")
	w("  showstringspaces=false,stringstyle=\\color{RedOrange},\n")
	w("  tabsize=4,xleftmargin=2em,\n")
	w("}\n\n")

	// ── Mathematics ─────────────────────────────────────────────────────────
	w("%%%% ── MATHEMATICS ───────────────────────────────────────────────────────\n")
	w("\\usepackage{amsmath,amssymb,amsthm}\n\n")

	// ── Lists ───────────────────────────────────────────────────────────────
	w("%%%% ── LISTS ─────────────────────────────────────────────────────────────\n")
	w("\\usepackage{enumitem}\n")
	w("\\setlist[itemize]{leftmargin=*,label=\\textcolor{accentblue}{\\textbullet}}\n")
	w("\\setlist[enumerate]{leftmargin=*}\n\n")

	// ── Callout boxes ───────────────────────────────────────────────────────
	w("%%%% ── CALLOUT BOXES (tcolorbox) ─────────────────────────────────────────\n")
	w("\\usepackage[most,breakable]{tcolorbox}\n")
	w("\\newtcolorbox{notebox}[1][Note]{colback=accentblue!5!white,colframe=accentblue!50!white,\n")
	w("  fonttitle=\\bfseries\\sffamily\\small,title=#1,breakable,enhanced}\n")
	w("\\newtcolorbox{warnbox}[1][Warning]{colback=Goldenrod!8!white,colframe=Goldenrod!70!black,\n")
	w("  fonttitle=\\bfseries\\sffamily\\small,title=#1,breakable,enhanced}\n")
	w("\\newtcolorbox{keybox}[1][Key Finding]{colback=headblue!5!white,colframe=headblue!60!white,\n")
	w("  fonttitle=\\bfseries\\sffamily\\small,title=#1,breakable,enhanced}\n\n")

	// ── Hyperlinks & PDF metadata ────────────────────────────────────────────
	w("%%%% ── HYPERLINKS & PDF METADATA ─────────────────────────────────────────\n")
	w("\\usepackage[hidelinks,colorlinks=true,linkcolor=headblue,urlcolor=accentblue,\n")
	w("  citecolor=accentblue,pdftitle={%s},pdfauthor={%s},\n", p.title, p.author)
	w("  pdfsubject={Report},pdfpagemode=UseOutlines,\n")
	w("  bookmarksopen=true,bookmarksnumbered=true]{hyperref}\n")
	w("\\usepackage{bookmark}\n\n")

	// ── Optional bibliography ────────────────────────────────────────────────
	w("%%%% ── BIBLIOGRAPHY (uncomment to enable) ─────────────────────────────────\n")
	w("%% \\usepackage[backend=biber,style=ieee,sorting=nyt]{biblatex}\n")
	w("%% \\addbibresource{references.bib}\n\n")

	// ── Document body ────────────────────────────────────────────────────────
	w("\\begin{document}\n\n")

	// Title page
	w("%%%% ── TITLE PAGE ────────────────────────────────────────────────────────\n")
	w("\\begin{titlepage}\n")
	w("  \\centering\n")
	w("  \\vspace*{2.5cm}\n")
	w("  {\\color{rulecolor}\\rule{\\linewidth}{2pt}}\\\\[0.6cm]\n")
	if p.style == "whitepaper" {
		w("  {\\large\\sffamily\\color{gray} WHITE PAPER}\\\\[0.4cm]\n")
	}
	w("  {\\huge\\bfseries\\sffamily\\color{headblue} %s}\\\\[0.4cm]\n", p.title)
	w("  {\\color{rulecolor}\\rule{\\linewidth}{0.5pt}}\\\\[0.8cm]\n")
	w("  {\\large\\sffamily %s}\\\\[0.3cm]\n", p.author)
	w("  {\\normalsize\\sffamily\\color{gray} %s}\n", p.date)
	w("  \\vfill\n")
	w("  {\\small\\sffamily\\color{gray} CONFIDENTIAL}\n")
	w("\\end{titlepage}\n\n")

	// Front matter (roman numerals)
	if !isArticle {
		w("\\pagenumbering{roman}\\setcounter{page}{1}\n\n")
		if p.abstract != "" {
			w("%%%% ── EXECUTIVE SUMMARY ──────────────────────────────────────────────────\n")
			w("\\chapter*{Executive Summary}\n")
			w("\\addcontentsline{toc}{chapter}{Executive Summary}\n")
			w("%s\n\n", p.abstract)
		} else {
			w("%%%% ── EXECUTIVE SUMMARY ──────────────────────────────────────────────────\n")
			w("\\chapter*{Executive Summary}\n")
			w("\\addcontentsline{toc}{chapter}{Executive Summary}\n")
			w("%%TODO: Write a 1–2 paragraph executive summary of the key findings and recommendations.\n\n")
		}
		w("\\tableofcontents\n")
		w("\\listoffigures\n")
		w("\\listoftables\n")
		w("\\clearpage\n")
		w("\\pagenumbering{arabic}\\setcounter{page}{1}\n\n")
	} else {
		// article / whitepaper: abstract before TOC
		if p.abstract != "" {
			w("\\begin{abstract}\n%s\n\\end{abstract}\n\n", p.abstract)
		} else {
			w("\\begin{abstract}\n%%TODO: Write a concise abstract (150–250 words).\n\\end{abstract}\n\n")
		}
		w("\\tableofcontents\n\\clearpage\n\n")
	}

	// ── Body ────────────────────────────────────────────────────────────────
	w("%%%% ── DOCUMENT BODY ─────────────────────────────────────────────────────\n\n")
	sections := p.sections
	if len(sections) == 0 {
		if isArticle {
			sections = []string{"Introduction", "Background", "Methodology", "Results", "Discussion", "Conclusion"}
		} else if isBook {
			sections = []string{"Introduction", "Background", "Analysis", "Recommendations", "Conclusion"}
		} else {
			sections = []string{"Introduction", "Background", "Analysis", "Findings", "Recommendations", "Conclusion"}
		}
	}

	cmd := "\\section"
	if !isArticle {
		cmd = "\\chapter"
	}
	for _, sec := range sections {
		slug := strings.ToLower(strings.ReplaceAll(sec, " ", "-"))
		slug = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return -1
		}, slug)
		if !isArticle {
			w("%s{%s}\n\\label{chap:%s}\n\n", cmd, sec, slug)
		} else {
			w("%s{%s}\n\\label{sec:%s}\n\n", cmd, sec, slug)
		}
		w("%%TODO: Write the %q section content here.\n\n", sec)
		// Scaffold a couple of sub-sections for the first and last sections
		if sec == sections[0] {
			w("%% Example sub-section (remove or rename as needed):\n")
			w("%% \\subsection{Scope and Objectives}\n\n")
		}
	}

	// ── Bibliography ────────────────────────────────────────────────────────
	w("%%%% ── BIBLIOGRAPHY (uncomment to enable) ─────────────────────────────────\n")
	w("%% \\printbibliography[heading=bibintoc]\n\n")

	w("\\end{document}\n")
	return b.String()
}
