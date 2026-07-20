#!/usr/bin/env python3
"""DFD lint — validate a Mermaid data-flow diagram against the skill's fixed
conventions before anyone tries to render it.

Companion script for the `threat-modeling` skill's model phase (the
`1.1-model.mmd` / `1-model.md` pair — SKILL.md §4.2, output-formats.md). The
skill fixes exactly one diagram shape for the DFD: `flowchart LR`, three
`classDef`s copied verbatim, trust boundaries as `subgraph` blocks, and every
edge labeled (references/diagram-conventions.md). This script turns that prose
checklist into a deterministic gate so a nonconforming diagram is caught
mechanically instead of at render time — and so repeated runs on the same
system stay comparable.

Why a script: the same determinism argument as recon.py. A fixed convention is
a rule a script can enforce byte-for-byte; leaving it to the model invites
drift (a `TB` here, an off-palette `fill:#ff0000` there, a bare unlabeled
arrow). The lint reads the diagram once, outside the model's context, and
reports each convention as PASS/FAIL with line evidence.

Checks (each maps to a diagram-conventions.md rule / pre-render checklist item):
  1. Direction is `flowchart LR` (never `TB`, never `graph`, never missing).
  2. Only the three allowed `classDef` fill/stroke pairs appear — no other
     fill:#.../stroke:#... pair.
  3. `.mmd` content carries no ```/```mermaid fence and its first non-blank
     line is a Mermaid diagram-type keyword (output-formats.md: a `.mmd` is
     raw Mermaid, not a chat code block).
  4. `subgraph` / `end` are balanced (every trust boundary closed).
  5. No unlabeled edges — every `-->` / `<-->` carries a label, either
     `-->|"..."|` or the `-- "..." -->` text-on-link form.
  6. (run-dir mode only) `1.1-model.mmd` and the ```mermaid block in
     `1-model.md` are identical modulo the fence and surrounding whitespace.

Contract:
  * Python 3.7+, standard library only — same dependency posture as recon.py.
  * Deterministic: same input -> same output, checks always in the same order.
  * Cross-platform: forces UTF-8 stdout so the report glyphs are identical on
    Windows (cp1252) and elsewhere.

Usage:
  python lint_dfd.py <file.mmd | 1-model.md | run-dir>

  <file.mmd>   a raw Mermaid file (checks 1-5)
  <1-model.md> a report file — the ```mermaid block is extracted and linted
  <run-dir>    a threat-model run directory — lints `1.1-model.mmd` (checks
               1-5) AND verifies it equals the fenced block in `1-model.md`
               (check 6)

Exit status is 0 iff every check passed, non-zero otherwise.
"""

import argparse
import os
import re
import sys

# --------------------------------------------------------------------------- #
# Fixed conventions (references/diagram-conventions.md is the authority)
# --------------------------------------------------------------------------- #

# The three — and only three — allowed classDef fill/stroke hex pairs, copied
# verbatim from diagram-conventions.md. Compared case-insensitively on the hex
# so #6BAED6 and #6baed6 are the same pair; any other pair is a FAIL.
ALLOWED_CLASSDEF_PAIRS = {
    ("6baed6", "2171b5"),   # process
    ("fdae61", "d94701"),   # external
    ("74c476", "238b45"),   # datastore
}
ALLOWED_PAIR_NAMES = {
    ("6baed6", "2171b5"): "process",
    ("fdae61", "d94701"): "external",
    ("74c476", "238b45"): "datastore",
}

# Mermaid diagram-type keywords a `.mmd` first line may legitimately be. The DFD
# is always `flowchart`, but check 3 only asserts "some diagram keyword" so it
# stays useful for the skill's other diagrams (sequence, etc.).
DIAGRAM_KEYWORDS = (
    "flowchart", "graph", "sequenceDiagram", "classDiagram", "stateDiagram",
    "stateDiagram-v2", "erDiagram", "journey", "gantt", "pie", "gitGraph",
    "mindmap", "timeline", "quadrantChart", "requirementDiagram", "C4Context",
    "sankey-beta", "xychart-beta", "block-beta",
)

# --------------------------------------------------------------------------- #
# Regexes
# --------------------------------------------------------------------------- #

FILL_RE = re.compile(r"fill\s*:\s*#([0-9a-fA-F]{3,8})")
STROKE_RE = re.compile(r"stroke\s*:\s*#([0-9a-fA-F]{3,8})")
# Any colour declaration at all — used to decide a line is making a style claim
# we must validate (so a lone `fill:#...` with no stroke is still caught).
HAS_COLOUR_RE = re.compile(r"(?:fill|stroke)\s*:\s*#", re.I)

FLOWCHART_LR_RE = re.compile(r"^flowchart\s+LR\b")

# An edge exists if the line carries one of these arrow operators.
ARROW_RE = re.compile(r"<-->|-->|<--")
# A pipe-delimited label with non-empty content: `-->|"HTTP POST"|`.
PIPE_LABEL_RE = re.compile(r"\|\s*\S[^|]*\|")
# The text-on-link form `-- "label" -->` / `-- calls -->`. The leading `--`
# must be followed by real text (not immediately by `>`), so a bare `A --> B`
# (a single `-->`) never matches.
TEXT_LINK_RE = re.compile(r'--\s*(?:"[^"]*"|[^|>"]+?)\s+-->')

MERMAID_FENCE_OPEN_RE = re.compile(r"^\s*```+\s*mermaid\s*$", re.I)
ANY_FENCE_RE = re.compile(r"^\s*```")
DIRECTIVE_RE = re.compile(r"^\s*%%\{.*\}%%\s*$")


# --------------------------------------------------------------------------- #
# Line preparation
# --------------------------------------------------------------------------- #

def strip_comment(text):
    """Return the content of a line with any Mermaid `%%` comment removed.

    A `%%{init: ...}%%` directive line carries no diagram content (no arrows,
    no classDef, no header) so it collapses to empty — that keeps it from being
    mistaken for the header line and from tripping any content check.
    """
    if DIRECTIVE_RE.match(text):
        return ""
    idx = text.find("%%")
    if idx >= 0:
        return text[:idx]
    return text


class Check:
    """One PASS/FAIL result with human-readable evidence lines."""

    def __init__(self, name):
        self.name = name
        self.passed = True
        self.evidence = []

    def fail(self, msg):
        self.passed = False
        self.evidence.append(msg)

    def note(self, msg):
        self.evidence.append(msg)


# --------------------------------------------------------------------------- #
# Individual checks — each takes the numbered content lines
# --------------------------------------------------------------------------- #

def first_content_line(lines):
    """First (lineno, raw, content) whose comment-stripped text is non-blank."""
    for lineno, raw in lines:
        content = strip_comment(raw).strip()
        if content:
            return lineno, raw, content
    return None


def check_direction(lines):
    c = Check("1. Direction is `flowchart LR`")
    first = first_content_line(lines)
    if first is None:
        c.fail("no diagram content found (empty diagram)")
        return c
    lineno, raw, content = first
    if FLOWCHART_LR_RE.match(content):
        c.note("line %d: `%s`" % (lineno, content))
        return c
    if content.startswith("flowchart"):
        c.fail("line %d: direction must be `LR`, found `%s`" % (lineno, content))
    elif content.startswith("graph"):
        c.fail("line %d: must be `flowchart LR`, found `graph ...`: `%s`"
               % (lineno, content))
    else:
        c.fail("line %d: first line is not `flowchart LR`: `%s`"
               % (lineno, content))
    return c


def check_classdefs(lines):
    c = Check("2. Only the three allowed classDef fill/stroke pairs")
    seen_pairs = set()
    for lineno, raw in lines:
        content = strip_comment(raw)
        if not HAS_COLOUR_RE.search(content):
            continue
        fills = FILL_RE.findall(content)
        strokes = STROKE_RE.findall(content)
        fill = fills[0].lower() if fills else None
        stroke = strokes[0].lower() if strokes else None
        if fill and stroke:
            pair = (fill, stroke)
            if pair in ALLOWED_CLASSDEF_PAIRS:
                seen_pairs.add(pair)
                c.note("line %d: `#%s`/`#%s` (%s)"
                       % (lineno, fill, stroke, ALLOWED_PAIR_NAMES[pair]))
            else:
                c.fail("line %d: disallowed fill/stroke pair `#%s`/`#%s` "
                       "-- not one of process/external/datastore"
                       % (lineno, fill, stroke))
        elif fill and not stroke:
            c.fail("line %d: `fill:#%s` with no matching `stroke:#...` "
                   "-- only the three fixed classDef pairs are allowed"
                   % (lineno, fill))
        elif stroke and not fill:
            c.fail("line %d: `stroke:#%s` with no matching `fill:#...` "
                   "-- only the three fixed classDef pairs are allowed"
                   % (lineno, stroke))
    if c.passed and not seen_pairs:
        c.note("(no classDef colour declarations found)")
    return c


def check_no_fence_and_keyword(lines):
    c = Check("3. No code fence; first line is a diagram keyword")
    for lineno, raw in lines:
        if ANY_FENCE_RE.match(raw):
            c.fail("line %d: unexpected ``` fence -- a .mmd is raw Mermaid, "
                   "not a fenced code block: `%s`" % (lineno, raw.strip()))
    first = first_content_line(lines)
    if first is None:
        c.fail("no diagram content found (no diagram-type keyword)")
        return c
    lineno, raw, content = first
    token = content.split()[0] if content.split() else content
    if any(token == kw or content.startswith(kw + " ") or content == kw
           for kw in DIAGRAM_KEYWORDS):
        if c.passed:
            c.note("line %d: starts with `%s`" % (lineno, token))
    else:
        c.fail("line %d: first non-blank line is not a Mermaid diagram-type "
               "keyword: `%s`" % (lineno, content))
    return c


def check_subgraph_balance(lines):
    c = Check("4. `subgraph` / `end` balanced")
    depth = 0
    opens = []
    for lineno, raw in lines:
        content = strip_comment(raw)
        stripped = content.strip()
        # `subgraph` opener: the keyword at the start of the statement.
        if re.match(r"^subgraph\b", stripped):
            depth += 1
            opens.append(lineno)
        elif re.match(r"^end\b", stripped) or stripped == "end":
            depth -= 1
            if depth < 0:
                c.fail("line %d: `end` with no open `subgraph`" % lineno)
                depth = 0
            elif opens:
                opens.pop()
    if depth > 0:
        for lineno in opens:
            c.fail("line %d: `subgraph` never closed by a matching `end`" % lineno)
    if c.passed:
        c.note("all subgraph blocks closed (%d boundary block(s))"
               % len([l for l, r in lines
                      if re.match(r"^subgraph\b", strip_comment(r).strip())]))
    return c


def edge_is_labeled(content):
    if PIPE_LABEL_RE.search(content):
        return True
    if TEXT_LINK_RE.search(content):
        return True
    return False


def check_labeled_edges(lines):
    c = Check("5. Every edge is labeled")
    edge_count = 0
    for lineno, raw in lines:
        content = strip_comment(raw)
        if not ARROW_RE.search(content):
            continue
        edge_count += 1
        if not edge_is_labeled(content):
            c.fail("line %d: unlabeled edge -- add a `|\"label\"|` or "
                   "`-- \"label\" -->` label: `%s`" % (lineno, content.strip()))
    if c.passed:
        c.note("%d edge(s), all labeled" % edge_count)
    return c


def check_mmd_matches_md(mmd_text, md_block_text):
    """Check 6: the .mmd and the fenced block are identical modulo fence and
    surrounding whitespace."""
    c = Check("6. `1.1-model.mmd` == fenced block in `1-model.md`")
    a = [ln.rstrip() for ln in mmd_text.strip("\n").split("\n")]
    b = [ln.rstrip() for ln in md_block_text.strip("\n").split("\n")]
    if a == b:
        c.note("%d line(s), byte-identical modulo fence/whitespace" % len(a))
        return c
    if len(a) != len(b):
        c.fail("line counts differ: 1.1-model.mmd has %d line(s), the "
               "1-model.md fenced block has %d" % (len(a), len(b)))
    for i in range(min(len(a), len(b))):
        if a[i] != b[i]:
            c.fail("first difference at block line %d:" % (i + 1))
            c.note("    1.1-model.mmd: `%s`" % a[i])
            c.note("    1-model.md   : `%s`" % b[i])
            break
    return c


# --------------------------------------------------------------------------- #
# Input handling
# --------------------------------------------------------------------------- #

def read_file(path):
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        return fh.read()


def number_lines(text):
    """Turn text into [(lineno, raw), ...] with 1-based line numbers."""
    return [(i + 1, line) for i, line in enumerate(text.split("\n"))]


def extract_mermaid_block(md_text):
    """Return (block_lines, block_text, error).

    block_lines is [(md_lineno, raw), ...] for the lines *inside* the first
    ```mermaid fence (line numbers are the .md file's own), block_text is the
    joined inner text. error is a string if no well-formed block was found.
    """
    lines = md_text.split("\n")
    open_idx = None
    for i, line in enumerate(lines):
        if MERMAID_FENCE_OPEN_RE.match(line):
            open_idx = i
            break
    if open_idx is None:
        return [], "", "no ```mermaid fenced block found"
    close_idx = None
    for j in range(open_idx + 1, len(lines)):
        if ANY_FENCE_RE.match(lines[j]):
            close_idx = j
            break
    if close_idx is None:
        return [], "", "```mermaid fence opened at line %d but never closed" \
            % (open_idx + 1)
    inner = [(k + 1, lines[k]) for k in range(open_idx + 1, close_idx)]
    inner_text = "\n".join(lines[open_idx + 1:close_idx])
    return inner, inner_text, None


def render_report(checks, source_desc):
    out = []
    w = out.append
    w("# DFD Lint — %s" % source_desc)
    w("")
    all_pass = True
    for c in checks:
        status = "PASS" if c.passed else "FAIL"
        if not c.passed:
            all_pass = False
        w("[%s] %s" % (status, c.name))
        for ev in c.evidence:
            w("       %s" % ev)
    w("")
    passed = sum(1 for c in checks if c.passed)
    w("Summary: %d/%d checks passed -- %s"
      % (passed, len(checks), "ALL PASS" if all_pass else "FAILURES PRESENT"))
    return "\n".join(out), all_pass


def lint_mermaid_lines(lines):
    """Run checks 1-5 over numbered Mermaid content lines."""
    return [
        check_direction(lines),
        check_classdefs(lines),
        check_no_fence_and_keyword(lines),
        check_subgraph_balance(lines),
        check_labeled_edges(lines),
    ]


def run(path):
    """Dispatch on the input kind and return (report_text, all_pass, exit_hint)."""
    if os.path.isdir(path):
        return _run_dir(path)

    lower = path.lower()
    text = read_file(path)
    if lower.endswith(".mmd"):
        checks = lint_mermaid_lines(number_lines(text))
        report, ok = render_report(checks, "`%s` (raw Mermaid)" % path)
        return report, ok
    if lower.endswith(".md") or "```mermaid" in text:
        inner, _inner_text, err = extract_mermaid_block(text)
        if err:
            c = Check("0. Extract ```mermaid block from Markdown")
            c.fail(err)
            report, ok = render_report([c], "`%s`" % path)
            return report, ok
        checks = lint_mermaid_lines(inner)
        report, ok = render_report(
            checks, "`%s` (```mermaid block)" % path)
        return report, ok

    # Fall back to treating it as raw Mermaid (unknown extension, no fence).
    checks = lint_mermaid_lines(number_lines(text))
    report, ok = render_report(checks, "`%s` (raw Mermaid)" % path)
    return report, ok


def _run_dir(path):
    mmd_path = os.path.join(path, "1.1-model.mmd")
    md_path = os.path.join(path, "1-model.md")
    checks = []
    problems = []

    if not os.path.isfile(mmd_path):
        c = Check("0. `1.1-model.mmd` present in run directory")
        c.fail("missing file: %s" % mmd_path)
        checks.append(c)
        problems.append("mmd")
        mmd_text = ""
    else:
        mmd_text = read_file(mmd_path)
        checks.extend(lint_mermaid_lines(number_lines(mmd_text)))

    if not os.path.isfile(md_path):
        c = Check("6. `1.1-model.mmd` == fenced block in `1-model.md`")
        c.fail("missing file: %s (cannot compare)" % md_path)
        checks.append(c)
    else:
        md_text = read_file(md_path)
        _inner, inner_text, err = extract_mermaid_block(md_text)
        if err:
            c = Check("6. `1.1-model.mmd` == fenced block in `1-model.md`")
            c.fail("1-model.md: %s" % err)
            checks.append(c)
        elif "mmd" in problems:
            c = Check("6. `1.1-model.mmd` == fenced block in `1-model.md`")
            c.fail("1.1-model.mmd missing -- cannot compare")
            checks.append(c)
        else:
            checks.append(check_mmd_matches_md(mmd_text, inner_text))

    report, ok = render_report(checks, "run directory `%s`" % path)
    return report, ok


def main(argv=None):
    ap = argparse.ArgumentParser(
        description="Lint a Mermaid DFD against the threat-modeling skill's "
                    "fixed diagram conventions.")
    ap.add_argument(
        "path",
        help="a .mmd file, a 1-model.md report file, or a threat-model run "
             "directory (lints 1.1-model.mmd and checks it matches 1-model.md)")
    args = ap.parse_args(argv)

    # The report uses a few non-ASCII glyphs; force UTF-8 so output is identical
    # on every platform (Windows console defaults to cp1252 and would crash).
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    if not os.path.exists(args.path):
        sys.stderr.write("lint_dfd.py: no such file or directory: %s\n"
                         % args.path)
        return 2

    report, ok = run(args.path)
    sys.stdout.write(report + "\n")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
