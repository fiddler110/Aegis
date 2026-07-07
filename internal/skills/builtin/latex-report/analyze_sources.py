#!/usr/bin/env python3
"""Summarize the structure of one or more markdown source docs before
consolidating them into a LaTeX report.

Prints, per file: the heading tree (with line numbers), word count, table
count, code-block languages used, and any TODO/FIXME/XXX markers. Run this
before synthesizing the section outline (SKILL.md step 2) instead of
re-deriving structure by eye — it's easy to miss a buried heading or an
open TODO when skimming a long doc.

Usage:
    python analyze_sources.py <file.md> [file2.md ...]
    python analyze_sources.py research/*.md
"""
import re
import sys

HEADING_RE = re.compile(r"^(#{1,6})\s+(.*)")
CODE_FENCE_RE = re.compile(r"^```\s*([\w+-]*)")
TABLE_ROW_RE = re.compile(r"^\s*\|.*\|\s*$")
MARKER_RE = re.compile(r"\b(TODO|FIXME|XXX)\b[:\s]*(.*)", re.IGNORECASE)


def analyze(path):
    with open(path, encoding="utf-8") as f:
        lines = f.readlines()

    headings = []
    code_langs = {}
    markers = []
    table_rows = 0
    in_table = False
    in_code = False
    word_count = 0

    for i, raw in enumerate(lines, start=1):
        line = raw.rstrip("\n")

        fence = CODE_FENCE_RE.match(line)
        if fence:
            if not in_code:
                lang = fence.group(1) or "(unlabeled)"
                code_langs[lang] = code_langs.get(lang, 0) + 1
            in_code = not in_code
            continue
        if in_code:
            continue

        m = HEADING_RE.match(line)
        if m:
            depth = len(m.group(1))
            headings.append((depth, m.group(2).strip(), i))

        if TABLE_ROW_RE.match(line):
            if not in_table:
                table_rows += 1  # count tables, not rows
                in_table = True
        else:
            in_table = False

        marker = MARKER_RE.search(line)
        if marker:
            markers.append((i, marker.group(1).upper(), marker.group(2).strip()))

        word_count += len(line.split())

    return {
        "headings": headings,
        "code_langs": code_langs,
        "markers": markers,
        "table_count": table_rows,
        "word_count": word_count,
        "line_count": len(lines),
    }


def render(path, info):
    print(f"=== {path} ===")
    print(f"{info['line_count']} lines, ~{info['word_count']} words, "
          f"{info['table_count']} table(s)")
    if info["code_langs"]:
        langs = ", ".join(f"{k}×{v}" for k, v in sorted(info["code_langs"].items()))
        print(f"code blocks: {langs}")
    if info["headings"]:
        print("headings:")
        for depth, text, lineno in info["headings"]:
            print(f"  {'  ' * (depth - 1)}L{lineno}: {'#' * depth} {text}")
    else:
        print("headings: (none — unstructured doc)")
    if info["markers"]:
        print("open markers:")
        for lineno, kind, text in info["markers"]:
            suffix = f" — {text}" if text else ""
            print(f"  L{lineno}: {kind}{suffix}")
    print()


def main(argv):
    if not argv:
        print(__doc__)
        return 1
    for path in argv:
        try:
            info = analyze(path)
        except OSError as e:
            print(f"=== {path} ===\nERROR: {e}\n")
            continue
        render(path, info)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
