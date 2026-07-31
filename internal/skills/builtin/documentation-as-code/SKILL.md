---
name: documentation-as-code
description: Use when producing a formal document (report, process, runbook, or slide deck) through a Documentation-as-Code repository that ships its own LaTeX/YAML template families and a `docforge.py` scaffolding CLI. Prefer this over hand-authoring LaTeX whenever such a repository is available, because its templates carry the owning organization's house style, metadata, and build wiring. Triggers on "scaffold a report", "docforge", "documentation as code", "make this a runbook/process doc", "turn these notes into a formal report", "build a slide deck from this".
---

# Documentation-as-Code Skill

A Documentation-as-Code (DaC) repository is a document toolchain: a set of
template families, a scaffolding CLI (`docforge.py`), a Markdown→LaTeX
converter (`md2report.py`), and per-document `Makefile`s. When one is
available, it — not you — owns the document's structure, styling, and
metadata defaults.

**Your job is to supply content and drive the tooling, never to reproduce
the house style.** The templates already encode it. Hand-authoring a LaTeX
preamble when a template family fits produces a document that looks wrong
next to every other document the organization publishes, and silently drops
whatever metadata, classification, or build wiring the template carried.

## 0. Confidentiality boundary — read this first

A DaC repository belongs to an organization and its templates typically
carry that organization's branding: logos, image assets, colour palettes,
reference documents, classification banners, team names, and example
documents about real internal systems.

**None of that is yours to copy, summarize, restate, or carry outside the
repository it lives in.** Concretely, while using this skill:

- **Never copy branding into anything you author.** No logo files, no image
  assets, no organization names, no team names, no colour-hex palettes
  lifted from a template, no reference `.docx`/`.pptx`, no verbatim
  boilerplate. If a template needs a logo, it already references it — leave
  that reference alone and do not reproduce or relocate the asset.
- **Never hard-code metadata defaults.** Author, subtitle, classification,
  and organization identity come from the repository's own
  `.docforge_config.json` at run time. Read it if you need to know what is
  already set; do not restate its values in a summary, a commit message, a
  generated document you place elsewhere, or a chat response beyond what the
  user asked for.
- **Never treat example documents as content sources.** A DaC repo's
  `education/`, `examples/`, and `research/` directories usually contain real
  internal material (named systems, architectures, assessments). They are
  there to demonstrate *template shape*. Read them for structure only, and do
  not carry their subject matter into the document you are building.
- **Keep generated documents inside the repository.** Scaffold into the DaC
  repo (or the `--dest` the user names). Do not copy a finished branded
  document into an unrelated workspace.

If the user asks for a document *outside* any DaC repository, do not
reconstruct the house style from memory. Use the `latex-report` skill and the
generic `latex_new_document` preamble instead — an unbranded document is the
correct output there.

## 1. Locate the toolchain and confirm the family

Find the DaC repository root: it holds `docforge.py`, `md2report.py`, and a
`_templates/` directory. If the user did not name a path, ask rather than
guess — running `docforge.py` from the wrong directory scaffolds into the
wrong place.

Read `.docforge_config.json` at that root (it may sit in a parent of your
working directory — `docforge.py` discovers the nearest one). It supplies
defaults for `type`, `author`, `subtitle`, `classification`, and `dest`.
**Anything already set there must not be re-specified on the command line**
unless the user explicitly overrides it for this document.

Then pick the template family. `docforge.py --type` accepts exactly four:

| `--type` | Produces | Use for |
|---|---|---|
| `report` | `<name>.tex` + `Makefile` + `assets/` + `build/` | Assessments, findings, white papers, analyses — anything with sections and a narrative |
| `process` | `<name>.tex` + build wiring | A defined, repeatable procedure with roles and hand-offs |
| `runbook` | `<name>.tex` + build wiring | Operational steps someone executes under time pressure |
| `slides` | `<name>.yaml` + `assets/` | A presentation deck, rendered from structured YAML rather than LaTeX |

If the ask is ambiguous between `report` and `process`/`runbook`, ask.
The three are structurally different documents, and converting after the
fact means re-scaffolding.

## 2. Two routes in — pick deliberately

### Route A: `--from-md` (preferred when content already exists)

This is the strength-matched path and should be your default. **You draft
Markdown — which you are good at — and the toolchain converts it**, so you
never author LaTeX and the house style is applied by `md2report.py` rather
than approximated by you.

```
python3 docforge.py --type report --from-md notes.md --name <slug>
```

Write or assemble the Markdown first (ordinary `write_file`), get the user's
agreement on the content, then convert. Front matter in the Markdown supplies
metadata, but the active config/flag values for `--title`, `--author`,
`--subtitle`, and `--classification` **override** front matter — so do not
fight the config by putting a competing value in front matter.

`--from-md` is **only valid with `--type report`**. For `process`, `runbook`,
or `slides`, use Route B.

### Route B: scaffold empty, then fill

```
python3 docforge.py --type runbook --name <slug> --title "…"
```

This copies the template folder, renames the main source file to your slug,
points the `Makefile`'s `MAIN` target at it, applies title/author/
classification substitutions, and copies diagram starter `.mmd` files into
`assets/`. You then fill the placeholders (`TODO_TITLE`, `TODO_AUTHOR`, and
the template's own section stubs) with `edit_file`.

Fill **one section per `edit_file` call**. A whole-file rewrite is slow, risks
truncating into a malformed tool call, and throws away the template structure
you just scaffolded — which is the entire value of having scaffolded it.

## 3. Always `--dry-run` first

```
python3 docforge.py --type report --name <slug> --title "…" --dry-run
```

`--dry-run` prints every planned operation without writing. Run it, read the
planned output paths, and only then re-run without the flag. This is not
optional politeness — it is the guard rail against the two failure modes that
cost the most turns:

- **`<dest>/<name>` already exists.** `docforge.py` fails hard on collision
  (deliberately — it will not clobber an existing document). If you discover
  this only on the real run, you have to stop and re-decide the slug anyway.
  Never "work around" it by deleting the existing directory; pick a different
  slug or ask the user.
- **Wrong `--dest`.** Blank `dest` means the current directory, and output
  lands at `<dest>/<name>` — easy to get wrong when you are not in the repo
  root.

`--name` must be a filesystem-safe slug: letters, numbers, hyphens,
underscores. Derive it from the title (lowercase, spaces→hyphens, strip
punctuation), and keep it short.

## 4. Build

Each scaffolded document ships a `Makefile`. Prefer it over calling a LaTeX
compiler directly — it knows the diagram pipeline and the build directory
layout:

| Target | Does |
|---|---|
| `make` / `make all` | Render `.mmd` diagrams to images, then build the PDF |
| `make diagrams` | Diagram render only |
| `make pdf` | Full multi-pass PDF build |
| `make quick` | Single-pass build; fast, but cross-references and TOC may be stale |
| `make clean` | Remove build intermediates |
| `make distclean` | `clean` plus generated outputs |

Run it with the `shell` tool from the document directory. If `make` is
unavailable, fall back to `latex_build` on the `.tex` file — but say that you
did, because the diagram pipeline will not have run.

For `--type slides`, there is no LaTeX build: the `.yaml` is rendered by the
repository's own deck renderer. Read that renderer's README before invoking
it rather than assuming a command line.

## 5. Slide decks: structured YAML, not prose

A `slides` document is a `.yaml` file with a `meta:` block and a `slides:`
list. Each entry has a `type:` selecting a layout. The types the template
family defines:

`title`, `divider`, `bullets`, `table`, `cards`, `card_grid`, `code_panel`,
`comparison_pair_row`, `composed`, `decision_risk`, `decisions_heatmap`,
`dependency`, `metrics`, `pipeline_step`, `quote`, `textbox`, `timeline`

Read the scaffolded `.yaml` before editing — it ships commented examples of
each type, and those examples are the authority on each type's fields. Two
rules:

- **Do not invent a `type:` value.** An unrecognized type is a render-time
  failure, and the error usually names only the slide index.
- **Bullet nesting is leading-space-encoded** (2 spaces = level 1, 4 = level
  2) inside the quoted string. YAML will not warn you if you get it wrong.

Validate the file after every edit — a broken indent in YAML is invisible
until the renderer fails, which on a slow local model costs several turns to
localize. If a `yaml_validate` tool is available, use it; otherwise
`python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" <file>`.

Do not carry example decks' subject matter into a new deck (see §0).

## 6. Diagrams

Template families copy starter `.mmd` (Mermaid) files into `assets/`. Author
diagrams there and let `make diagrams` render them; do not embed raster
images you generated elsewhere, and do not reuse a template's example diagram
content — replace it.

The `diagram` tool can help you draft Mermaid source before you write it into
`assets/`.

## 7. Report

State plainly:

- which template family was used and why
- the exact output path (`<dest>/<name>/`)
- whether the build succeeded, and the PDF/deck path if so
- any placeholder you left unfilled (`TODO_*` markers), so the user knows the
  document is not finished

Do not paste the document's full content back into chat — the file on disk is
the deliverable. Summarize its structure instead.
