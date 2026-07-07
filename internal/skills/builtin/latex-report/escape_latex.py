#!/usr/bin/env python3
"""Escape LaTeX special characters in prose pulled from markdown source docs.

Markdown source text often contains characters that are meaningful to LaTeX
(# $ % & _ { } ~ ^ \\) — copying it into a section verbatim (SKILL.md step 4)
is a common cause of a failed latex_build. This escapes plain prose safely;
it does NOT touch text you intend to keep as literal LaTeX (equations,
existing commands) — run it only over the markdown-derived spans, not over
LaTeX you've already written by hand.

Usage:
    python escape_latex.py < notes.txt
    python escape_latex.py "some raw text with a % sign and a & in it"
    echo "50% of $100 costs #2 & up" | python escape_latex.py
"""
import sys

REPLACEMENTS = [
    ("\\", r"\textbackslash{}"),
    ("&", r"\&"),
    ("%", r"\%"),
    ("$", r"\$"),
    ("#", r"\#"),
    ("_", r"\_"),
    ("{", r"\{"),
    ("}", r"\}"),
    ("~", r"\textasciitilde{}"),
    ("^", r"\textasciicircum{}"),
]


def escape(text):
    # Backslash must be replaced first so later replacements' own backslashes
    # aren't double-escaped.
    for char, repl in REPLACEMENTS:
        text = text.replace(char, repl)
    return text


def main(argv):
    if argv:
        sys.stdout.write(escape(" ".join(argv)) + "\n")
        return 0
    data = sys.stdin.read()
    sys.stdout.write(escape(data))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
