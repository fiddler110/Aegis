---
name: latex-report
description: Use when asked to consolidate a number of existing markdown research/planning/audit docs into one coherent LaTeX report or PDF. Triggers on "consolidate these into a report", "write this up as a LaTeX report", "turn these docs into a PDF report".
---

# LaTeX Report Skill

Consolidates several existing markdown documents (research notes, audit
findings, planning docs) into a single, formally structured LaTeX report,
built on the `latex_new_document`/`latex_build` tools
(`internal/tool/builtin/latex.go`). Use this instead of the `html-report`
skill when the deliverable needs to be a PDF (formal review, print
distribution, offline reading) rather than a shareable web page.

This is a general-purpose skill, not tied to any one persona — load it
whenever the task matches, regardless of which persona is active.

## Steps

1. **Gather the sources.** Identify every markdown doc the report should
   draw from — the user may name them, or you may need `glob`/`grep` to find
   them (e.g. everything under `research/`). Read each one in full before
   outlining; do not summarize from filenames or headings alone.

2. **Synthesize a section outline.** Run `analyze_sources.py` (Python 3,
   stdlib only, bundled with this skill) over the source files first —
   `python analyze_sources.py file1.md file2.md ...` — it prints each doc's
   heading tree, word count, table/code-block counts, and any open
   TODO/FIXME markers, so you're outlining from an accurate structural map
   instead of whatever you happened to notice while skimming. Then don't
   just concatenate the source docs in file order — merge overlapping
   material, resolve contradictions between docs (flag ones you can't
   resolve rather than picking silently), and order sections for a reader
   encountering the consolidated report for the first time (typically:
   executive summary → background → the substantive sections →
   recommendations/conclusion → appendices for raw detail that doesn't
   belong in the main narrative). Decide the section list before calling the
   tool below — it takes `sections` up front.

3. **Scaffold the document:**
   `latex_new_document({"path": "...", "title": "...", "style": "report", "sections": [...]}))`
   Use `style: "report"` (the default) — it's the style built for this case:
   chaptered structure, executive summary front-matter, table of contents,
   bibliography support. Pass your outline as `sections` so each chapter is
   pre-scaffolded with a `%TODO%` marker rather than writing the whole
   preamble by hand.

4. **Fill in each section** with `write_file`/`edit_file`, replacing the
   `%TODO%` markers with real content synthesized from the source docs —
   not copy-pasted markdown. Convert markdown constructs to their LaTeX
   equivalents as you go: tables → `tabularx`/`booktabs`, fenced code blocks
   → `lstlisting`, bullet/numbered lists → `itemize`/`enumerate`, callouts or
   "note:"/"warning:" asides → the `notebox`/`warnbox`/`keybox` environments
   the scaffold already defines. Escape LaTeX special characters
   (`# $ % & _ { } ~ ^ \`) in prose pulled from source docs — pipe spans of
   plain prose through the bundled `escape_latex.py` (stdin or an argument)
   rather than escaping by hand; it's easy to miss one `_` or `%` in a long
   paragraph and that's a guaranteed `latex_build` failure.

5. **Build:** `latex_build({"path": "...", "runs": 2})`. If it fails, read
   the reported errors (with context lines) and fix the offending LaTeX
   rather than stripping content — a common failure is an unescaped special
   character or an unbalanced environment introduced during the markdown
   conversion in step 4. Re-run until `BUILD SUCCESS`.

6. **Report the output PDF path** from the build result. Don't just say
   "done" — name the path so the user can open it directly.

## Notes

- If `latex_build` reports the compiler isn't installed, relay its
  install-guidance verbatim rather than improvising a workaround.
- For a short, single-source write-up, `html-report` is usually a better
  fit (no LaTeX toolchain dependency, renders in any browser). Reach for
  this skill specifically for the multi-doc consolidation case, or when a
  PDF is a hard requirement.
