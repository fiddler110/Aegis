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

	"github.com/fiddler110/aegis/internal/tool"
)

// ─── latex_build ──────────────────────────────────────────────────────────────

type latexBuildTool struct{ root string }

func (t *latexBuildTool) Name() string                { return "latex_build" }
func (t *latexBuildTool) Capability() tool.Capability { return tool.CapExecute }
func (t *latexBuildTool) Description() string {
	return "Compile a LaTeX (.tex) file to PDF using xelatex, pdflatex, or lualatex. " +
		"Runs multiple passes to resolve cross-references and table-of-contents entries, and " +
		"runs biber/bibtex in between when the document declares a bibliography, so \\cite keys " +
		"resolve instead of typesetting as [?]. " +
		"Returns a structured build report: errors with context lines, deduplicated warnings, " +
		"page count, and the output PDF path. Use check_only for a fast syntax check."
}
func (t *latexBuildTool) InputSchema() json.RawMessage {
	return schema(`{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"workspace-relative path to the .tex file"},
			"compiler":{"type":"string","enum":["xelatex","pdflatex","lualatex"],"description":"LaTeX compiler (default: xelatex)"},
			"runs":{"type":"integer","description":"compiler passes to resolve references and TOC (1–4, default 2). Raise it if the report still warns 'Rerun to get cross-references right'"},
			"bib":{"type":"boolean","description":"run the bibliography tool (biber for biblatex, bibtex otherwise) between passes. Omit for auto-detect: the bib pass runs when the source declares \\addbibresource or \\bibliography. Set true to force it, false to suppress it. When it runs, at least three compiler passes happen regardless of 'runs' so citations and the bibliography both settle."},
			"check_only":{"type":"boolean","description":"draft-mode syntax check — detects errors without writing a PDF (skips the bibliography pass)"},
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
		Bib       *bool  `json:"bib"` // nil = auto-detect from the source
		CheckOnly bool   `json:"check_only"`
		OutputDir string `json:"output_dir"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if args.Compiler == "" {
		args.Compiler = "xelatex"
	}
	// 4 is reachable so that a document still reporting "Rerun to get
	// cross-references right" after the bib pass (which itself forces 3) has an
	// escape hatch.
	if args.Runs < 1 || args.Runs > 4 {
		args.Runs = 2
	}
	if args.CheckOnly {
		args.Runs = 1
	}

	root := effectiveRoot(ctx, t.root)
	roots := effectiveRoots(ctx, t.root)
	texAbs, err := resolveRead(ctx, root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	if _, statErr := os.Stat(texAbs); os.IsNotExist(statErr) {
		return tool.Result{Content: "file not found: " + args.Path, IsError: true}, nil
	}

	texDir := filepath.Dir(texAbs)
	outDir := texDir
	if args.OutputDir != "" {
		outDir, err = resolveWrite(ctx, root, args.OutputDir)
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
	if violations := checkLatexConfinement(roots, texAbs); len(violations) > 0 {
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
	base := strings.TrimSuffix(filepath.Base(texAbs), ".tex")

	// Whether a bibliography pass is wanted: the `bib` input forces or suppresses
	// it, and omitting it auto-detects from the source (P52.10). check_only is a
	// draft syntax check, so it never spends the extra subprocess.
	bibWanted := false
	if !args.CheckOnly {
		if args.Bib != nil {
			bibWanted = *args.Bib
		} else {
			bibWanted = latexSourceDeclaresBibliography(roots, texAbs)
		}
	}

	runLatex := func() (string, error) {
		runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		var buf bytes.Buffer
		cmd := exec.CommandContext(runCtx, compPath, flags...)
		cmd.Dir = texDir
		cmd.Env = env
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		return buf.String(), err
	}

	var (
		lastLog    string
		runErr     error
		failedPass int
		bibNote    string
		passes     = args.Runs
		done       int
	)
	for done < passes {
		lastLog, runErr = runLatex()
		done++
		// Abort the sequence on the first failing pass, so the reported log and
		// the reported error always describe the *same* pass. Continuing here
		// used to let a later pass overwrite both (P52.10).
		if runErr != nil {
			failedPass = done
			break
		}
		if done != 1 || !bibWanted {
			continue
		}

		// ── bibliography pass ────────────────────────────────────────────────
		// Pass 1 has written the control file the bib tool consumes. That file
		// is generated, not authored, so the P52.2 source scan never saw its
		// contents — re-validate the resources it names before handing them to
		// a subprocess.
		bibTool, control := latexBibControlFile(outDir, base)
		if bibTool == "" {
			bibNote = "bibliography: no .bcf/.aux bibliography data was produced — bib pass skipped"
			continue
		}
		if violations := checkLatexBibConfinement(roots, texDir, outDir, control); len(violations) > 0 {
			return tool.Result{
				Content: "refusing to run " + bibTool +
					" — the generated " + strings.ToLower(strings.TrimPrefix(filepath.Ext(control), ".")) +
					" file names bibliography resources outside the workspace:\n  · " +
					strings.Join(violations, "\n  · ") +
					"\n\nlatex_build is confined to the workspace root. Copy the .bib file into " +
					"the workspace and reference it with a workspace-relative path, then retry.",
				IsError: true,
			}, nil
		}
		bibPath, bibLookErr := exec.LookPath(bibTool)
		if bibLookErr != nil {
			bibNote = fmt.Sprintf(
				"bibliography: %s not found in PATH — citations will render as [?]. Install it (TeX Live: tlmgr install %s)",
				bibTool, bibTool)
			continue
		}
		bibLog, bibErr := runLatexBibTool(ctx, bibPath, bibTool, base, texDir, outDir, env)
		if bibErr != nil {
			bibNote = fmt.Sprintf("bibliography: %s exited with an error (%v) — citations may render as [?]%s",
				bibTool, bibErr, firstLatexBibLine(bibLog))
		} else {
			bibNote = "bibliography: " + bibTool + " ran over " + filepath.Base(control)
		}
		// A bib run invalidates every cross-reference it just resolved, so two
		// further LaTeX passes are required: one to read the .bbl it wrote and
		// one to settle the labels and page numbers that shifted.
		if passes < done+2 {
			passes = done + 2
		}
	}

	// Derive workspace-relative PDF path for the summary.
	pdfRel, _ := filepath.Rel(root, filepath.Join(outDir, base+".pdf"))

	summary := parseLatexLog(lastLog, pdfRel, args.CheckOnly)
	summary.bibNote = bibNote
	if runErr != nil && len(summary.errors) == 0 {
		summary.errors = append(summary.errors,
			fmt.Sprintf("compiler exited on pass %d: %s", failedPass, runErr.Error()))
	}
	if runErr != nil {
		summary.success = false
	}

	return tool.Result{
		Content: formatBuildResult(summary, args.Compiler, done),
		IsError: !summary.success,
	}, nil
}
