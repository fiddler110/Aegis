#!/usr/bin/env python3
"""Structural sanity checks for a report produced by the html-report skill.

Stdlib only, no dependencies. Exits 0 and prints "OK" when every check
passes; otherwise prints one line per failure and exits 1. This does not
validate visual design or content quality -- only the structural rules the
skill depends on (self-contained, theme-aware, no obviously broken markup).
"""
import re
import sys


def check(path: str) -> list[str]:
    with open(path, "r", encoding="utf-8") as f:
        html = f.read()

    errors = []

    if "<!doctype html>" not in html.lower():
        errors.append("missing <!doctype html>")

    if "<title>" not in html.lower():
        errors.append("missing <title>")

    # Self-contained: no external stylesheet/script/font references.
    for pattern, label in [
        (r'<link[^>]+rel=["\']stylesheet["\'][^>]*href=["\']https?://', "external stylesheet <link>"),
        (r'<script[^>]+src=["\']https?://', "external <script src=...>"),
        (r'@import\s+url\(["\']?https?://', "external CSS @import"),
    ]:
        if re.search(pattern, html, re.IGNORECASE):
            errors.append(f"not self-contained: found {label}")

    # Theme awareness: all three override paths should be present.
    theme_checks = [
        ("@media (prefers-color-scheme: dark)", "prefers-color-scheme dark media query"),
        ('[data-theme="dark"]', 'data-theme="dark" override'),
        ('[data-theme="light"]', 'data-theme="light" override'),
    ]
    for needle, label in theme_checks:
        if needle not in html:
            errors.append(f"missing {label}")

    # Wide tables should scroll, not overflow the page.
    if "<table" in html.lower() and "table-wrap" not in html and "overflow-x" not in html:
        errors.append("has a <table> but no scrollable wrapper (table-wrap / overflow-x)")

    # Basic tag-balance sanity check on a few structural tags.
    for tag in ["div", "section", "table", "style"]:
        opens = len(re.findall(rf"<{tag}\b", html, re.IGNORECASE))
        closes = len(re.findall(rf"</{tag}>", html, re.IGNORECASE))
        if opens != closes:
            errors.append(f"unbalanced <{tag}>: {opens} opening vs {closes} closing")

    return errors


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: validate_report.py <path-to-report.html>", file=sys.stderr)
        return 2

    errors = check(sys.argv[1])
    if errors:
        for e in errors:
            print(f"FAIL: {e}")
        return 1

    print("OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
