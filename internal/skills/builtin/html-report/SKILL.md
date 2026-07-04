---
name: html-report
description: Use when asked to produce a polished, shareable report (audit findings, roadmap, comparison, review) as a standalone HTML file instead of plain markdown. Triggers on "write this up as a report", "make it look nice", "html report", "shareable page".
---

# HTML Report Skill

Produces a single self-contained `.html` file — no external CDN, fonts, or
scripts — styled as a professional report. This skill bundles a working
starting point instead of describing the design system from scratch each
time: read the assets below before writing anything.

## Steps

1. Read `template.html` in this skill's directory (see `<skill_assets>`
   below for the exact path) — it is a complete, valid report with dummy
   content in every section (masthead, summary card, grouped finding cards
   with severity chips, pattern callouts, a comparative table, footer).
2. Copy it and replace the dummy content with the real report. Keep the
   `<style>` block, the CSS custom properties, and the light/dark theme
   overrides intact — that's the part that's easy to get subtly wrong
   (missing a `:root[data-theme=...]` override, hardcoding a color outside
   the variables) and the template already gets it right.
3. Only add or remove component types the content actually needs. Don't
   force severity chips onto a list that has no ordinal scale, and don't
   invent extra sections beyond what fits: masthead → summary → grouped
   content → (optional pattern/comparative sections) → footer.
4. Write the finished file to disk with your normal file-write tool.
5. Run `validate_report.py` (Python 3, stdlib only) against the finished
   file before telling the user you're done:
   `python validate_report.py path/to/report.html`
   It exits non-zero with a specific message if something structural is
   wrong (not self-contained, missing dark-mode override, unclosed tags,
   etc.) — fix and re-run until it passes.
6. Tell the user the output path. Opening it in any browser renders the
   final page directly — no build step, no server.

## Design notes (why the template looks the way it does)

- **Serif headings / sans body** is the single biggest lever for "reads as
  a report, not a UI screen." Don't drop it for a generic sans-everywhere
  look.
- **Theme variables must be overridden three ways**: `@media
  (prefers-color-scheme: dark)` for OS-level dark mode, plus
  `:root[data-theme="dark"]` and `:root[data-theme="light"]` for embedded
  viewers that stamp a theme attribute rather than relying on OS
  preference. The template's validator checks for all three.
- **Severity/status chips** get their own hue per level and a matching
  `border-left` on the parent card — that pairing is what makes a long
  list of findings scannable at a glance. Keep hues distinguishable in
  both themes (shift lightness/saturation per-theme, don't reuse one pair).
- **Wide tables** go inside `div.table-wrap { overflow-x: auto }` so they
  scroll instead of breaking the page on narrow viewports — never shrink
  font size to force a fit.
- **One file, no exceptions.** If you're tempted to link a second CSS or
  JS file, inline it instead. The validator checks for this.
