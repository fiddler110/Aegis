package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/tool"
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

	texAbs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if _, statErr := os.Stat(texAbs); os.IsNotExist(statErr) {
		return tool.Result{Content: "file not found: " + args.Path, IsError: true}, nil
	}

	texDir := filepath.Dir(texAbs)
	outDir := texDir
	if args.OutputDir != "" {
		outDir, err = resolvePath(t.root, args.OutputDir)
		if err != nil {
			return tool.Result{}, err
		}
		if err := os.MkdirAll(outDir, 0o750); err != nil {
			return tool.Result{Content: "cannot create output dir: " + err.Error(), IsError: true}, nil
		}
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

	flags := []string{
		"-interaction=nonstopmode",
		"-halt-on-error",
		"-output-directory=" + outDir,
	}
	if args.CheckOnly {
		flags = append(flags, "-draftmode")
	}
	flags = append(flags, texAbs)

	var lastLog string
	var runErr error
	for pass := 0; pass < args.Runs; pass++ {
		runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		var buf bytes.Buffer
		cmd := exec.CommandContext(runCtx, compPath, flags...)
		cmd.Dir = texDir
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
	pdfRel, _ := filepath.Rel(t.root, filepath.Join(outDir, base+".pdf"))

	summary := parseLatexLog(lastLog, pdfRel, args.CheckOnly)
	if runErr != nil && len(summary.errors) == 0 {
		summary.errors = append(summary.errors, "compiler exited: "+runErr.Error())
	}

	return tool.Result{
		Content: formatBuildResult(summary, args.Compiler, args.Runs),
		IsError: !summary.success,
	}, nil
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

	abs, err := resolvePath(t.root, args.Path)
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
	style, compiler, pageSize      string
	sections                       []string
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
