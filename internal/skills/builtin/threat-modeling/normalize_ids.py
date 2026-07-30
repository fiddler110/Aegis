#!/usr/bin/env python3
"""Threat-model ID canonicalizer — the deterministic mechanical repair pass.

Companion script for the `threat-modeling` skill (roadmap P50.2). Where
`verify.py` *detects* cross-file ID defects and `inventory.py` *records* the
finished suite, this script *repairs* the two mechanical ID defects a weak
model reliably introduces on a live run — both observed on the P38.1 STRIDE
run — without touching any judgment-bound content:

  (A) Invented threat-ID suffixes. The analysis file defines plain threats
      `T1..Tn`, but the model sometimes writes non-canonical `T<n>.<suffix>`
      tokens (`T1.S`, `T2.T`, `T04.S`) in the coverage table's Threat-ID
      column and in findings' "Related Threats" rows — a phantom decomposition
      of a threat by its STRIDE category. Every such token is rewritten to its
      bare `T<n>` form, everywhere in the analysis and findings files. The
      analysis file's set of `T<n>` is the source of truth; threats are never
      resequenced or renumbered, only their invented suffixes stripped. Bare
      `T<n>` is already canonical, so the sub is a no-op on a clean suite.

  (B) Gapped / duplicated finding numbers. `### FIND-##` headings must be a
      gapless `FIND-01, FIND-02, ... FIND-NN` sequence in document order (the
      exact shape `verify.py`'s `check_finding_ids_sequential` demands). A
      hand-renumber can leave a gap or a duplicate heading (the duplicate
      `FIND-07` regression this script exists to prevent). Headings are
      renumbered by document position — which naturally de-duplicates a
      repeated id (the first `### FIND-07` becomes FIND-07, the next FIND-08) —
      and every *reference* to a finding (the coverage table's Finding-ID
      column and any inline `FIND-##` cross-reference) is remapped in lockstep
      through the same old->new map, so a heading and its coverage row can
      never disagree. Because the coverage table references findings 1:1, the
      old->new map is built from the UNIQUE old ids in heading order; a
      genuinely ambiguous reference (its old id appeared as two headings) is
      still remapped to the first occurrence's new id and flagged in the
      report rather than silently guessed.

Both canonicalizations run in one pass and edit the two ID-bearing files
(`2-<framework>-analysis.md`, `3-findings.md`) in place. The other suite
files carry no threat/finding IDs and are never touched.

Contract (same posture as the sibling `verify.py` / `inventory.py`):
  * Python 3.7+, standard library only — no third-party imports.
  * Deterministic: same input -> byte-identical output and a sorted report.
  * Idempotent: a second run over a canonicalized directory is a no-op
    (0 changes) — asserted by `--selftest`.
  * Mechanical only. It rewrites literal ID tokens with targeted regex subs;
    it never reflows a table, renumbers a *threat*, or invents content.

Usage:
  python normalize_ids.py <run-dir>            normalize in place; print every
                                               change; exit 0 (no-op is success)
  python normalize_ids.py <run-dir> --check    report what WOULD change without
                                               writing; exit 1 if any change is
                                               needed, 0 if already canonical
  python normalize_ids.py --selftest           run built-in assertions on a
                                               synthetic fixture; exit 0/1

Exit code: 0 on success (including a no-op), 1 for `--check` when a change is
needed (or a failed `--selftest`), 2 on a usage/IO error (missing run dir, or
the analysis / findings file absent).
"""

import argparse
import os
import re
import sys
import tempfile
from collections import Counter

# Sibling script (ships in the same skill dir). We reuse its Suite loader,
# file-role location, and ID regexes so this repair pass and the verifier can
# never disagree about what a threat/finding token is or where the files live.
# Ensure the script's own directory is importable when invoked by path from an
# arbitrary cwd, exactly as verify.py does for `import inventory`.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
try:
    import verify
except Exception as exc:  # pragma: no cover - defensive; can't run without it
    sys.stderr.write("normalize_ids.py: cannot import sibling verify.py: %s\n" % exc)
    raise

# An invented threat-ID suffix: a bare `T<n>` immediately followed by a
# `.<alnum>` decomposition. The leading/trailing guards keep the match a whole
# token (never a slice of `DT1.S` or `T1.Sx`-into-something-longer). Mirrors
# verify.TID_RE's suffix vocabulary (`[A-Za-z0-9]+`). The captured `\1` is the
# canonical bare id.
STRIP_RE = re.compile(r"(?<![A-Za-z0-9])(T\d+)\.[A-Za-z0-9]+")

# A finding reference token (heading token, coverage Finding-ID cell, or an
# inline cross-reference). Same shape verify.FIND_HEADING_RE anchors.
FIND_REF_RE = re.compile(r"FIND-\d+")


def _ordinal(n):
    """1 -> '1st', 2 -> '2nd', 3 -> '3rd', 11 -> '11th' — for duplicate notes."""
    if 10 <= (n % 100) <= 20:
        suffix = "th"
    else:
        suffix = {1: "st", 2: "nd", 3: "rd"}.get(n % 10, "th")
    return "%d%s" % (n, suffix)


# --------------------------------------------------------------------------- #
# (A) threat-ID suffix stripping — applied to every line of both files
# --------------------------------------------------------------------------- #

def strip_suffixes(line):
    """Rewrite every `T<n>.<suffix>` token in `line` to bare `T<n>`.

    Returns (new_line, [(old_token, new_token), ...]) — the substitution list
    is empty (and new_line is line unchanged) when the line is already
    canonical, which makes the whole operation idempotent.
    """
    subs = []

    def repl(m):
        old = m.group(0)
        new = m.group(1)
        subs.append((old, new))
        return new

    return STRIP_RE.sub(repl, line), subs


def process_analysis(lines):
    """Strip invented threat suffixes across the analysis file.

    Returns (new_lines, [(lineno, old, new), ...]).
    """
    out = []
    changes = []
    for i, line in enumerate(lines, 1):
        new_line, subs = strip_suffixes(line)
        for old, new in subs:
            changes.append((i, old, new))
        out.append(new_line)
    return out, changes


# --------------------------------------------------------------------------- #
# (B) finding renumber (+ threat suffix strip) — the findings file
# --------------------------------------------------------------------------- #

def process_findings(lines):
    """Strip threat suffixes AND renumber findings gaplessly in the findings file.

    Returns (new_lines, changes, notes) where:
      * changes is a list of (lineno, old_display, new_display) tuples, each a
        single applied rewrite (threat-suffix strip, heading renumber, or
        finding-reference remap);
      * notes is a list of (lineno, old_id) for finding references whose old id
        was a duplicated heading — remapped to the first occurrence's new id
        but genuinely ambiguous, so surfaced rather than silently trusted.
    """
    # Pass 1: collect finding headings in document order.
    heading_ids = []
    for line in lines:
        m = verify.FIND_HEADING_RE.match(line.strip())
        if m:
            heading_ids.append(m.group(1))

    new_ids = ["FIND-%02d" % k for k in range(1, len(heading_ids) + 1)]
    counts = Counter(heading_ids)

    # Reference map: UNIQUE old ids in heading order -> the first occurrence's
    # new id. A coverage table references findings 1:1, so this is the correct
    # remap for every unambiguous reference; ids that appeared as two headings
    # are recorded in dup_old and noted where referenced.
    old_to_new = {}
    for old_id, new_id in zip(heading_ids, new_ids):
        old_to_new.setdefault(old_id, new_id)
    dup_old = {oid for oid, c in counts.items() if c > 1}

    out = []
    changes = []
    notes = []
    heading_index = 0
    seen_occurrence = Counter()

    for i, line in enumerate(lines, 1):
        # (A) strip threat suffixes first (Threat-ID column / Related Threats).
        line, subs = strip_suffixes(line)
        for old, new in subs:
            changes.append((i, old, new))

        m = verify.FIND_HEADING_RE.match(line.strip())
        if m:
            # (B) heading: renumber by document position (de-dupes duplicates).
            old_id = m.group(1)
            new_id = new_ids[heading_index]
            heading_index += 1
            seen_occurrence[old_id] += 1
            if old_id != new_id:
                line = FIND_REF_RE.sub(new_id, line, count=1)
                disp = old_id
                if counts[old_id] > 1:
                    disp = "%s (%s)" % (old_id, _ordinal(seen_occurrence[old_id]))
                changes.append((i, disp, new_id))
            out.append(line)
            continue

        # (B) non-heading: remap every finding reference through the map.
        def repl(mm, _lineno=i):
            old = mm.group(0)
            if old not in old_to_new:
                return old
            new = old_to_new[old]
            if old in dup_old:
                notes.append((_lineno, old))
            if new != old:
                changes.append((_lineno, old, new))
                return new
            return old

        out.append(FIND_REF_RE.sub(repl, line))

    return out, changes, notes


# --------------------------------------------------------------------------- #
# Orchestration
# --------------------------------------------------------------------------- #

class Result:
    """Everything a normalize pass produced: per-file new content + a report."""

    def __init__(self):
        self.changes = []   # list of formatted "base:line  OLD -> NEW" strings
        self.notes = []     # list of formatted informational note strings
        self.writes = {}    # abs path -> new file content (only changed files)

    @property
    def changed(self):
        return bool(self.writes)


def _content(lines):
    """Join rewritten lines the way the sibling scripts write files: LF joined,
    exactly one trailing newline (matches scaffold.py's normalization)."""
    return "\n".join(lines) + "\n"


def load_suite(run_dir):
    """Build a verify.Suite and return (suite, relevant_errors).

    Only load errors that concern the two ID-bearing files this script edits
    (2-*-analysis.md, 3-findings.md) are relevant — a missing 0-assessment.md
    has no threat/finding IDs and must not block a normalization. A non-empty
    relevant_errors means we must not partially rewrite.
    """
    suite = verify.Suite(run_dir)
    relevant = [e for e in suite.errors if "analysis" in e or "findings" in e]
    return suite, relevant


def normalize(suite):
    """Compute the canonicalization for a loaded suite. Returns a Result."""
    res = Result()

    a_base = suite.base("analysis")
    f_base = suite.base("findings")
    a_path = suite.paths.get("analysis")
    f_path = suite.paths.get("findings")

    # Analysis: suffix strip only.
    a_lines = suite.lines("analysis")
    a_out, a_changes = process_analysis(a_lines)

    # Findings: suffix strip + finding renumber/remap.
    f_lines = suite.lines("findings")
    f_out, f_changes, f_notes = process_findings(f_lines)

    formatted = []
    for lineno, old, new in a_changes:
        formatted.append((a_base, lineno, old, new))
    for lineno, old, new in f_changes:
        formatted.append((f_base, lineno, old, new))
    # Deterministic order: by file, then line, then the tokens.
    formatted.sort(key=lambda t: (t[0], t[1], t[2], t[3]))
    res.changes = ["%s:%d  %s -> %s" % (b, ln, old, new)
                   for b, ln, old, new in formatted]

    note_rows = sorted(set(f_notes))
    res.notes = [
        "note: %s:%d  reference %s was a duplicated heading id — remapped to "
        "its first occurrence's new id (ambiguous — verify by hand)"
        % (f_base, ln, old)
        for ln, old in note_rows
    ]

    if a_changes and a_path is not None:
        res.writes[a_path] = _content(a_out)
    if f_changes and f_path is not None:
        res.writes[f_path] = _content(f_out)
    return res


def apply_writes(res):
    """Write every changed file back in place (LF newlines). Raises OSError."""
    for path in sorted(res.writes):
        with open(path, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(res.writes[path])


# --------------------------------------------------------------------------- #
# Self-test — synthetic fixture, no pytest
# --------------------------------------------------------------------------- #

_SELFTEST_ANALYSIS = "\n".join([
    "# STRIDE Threat Analysis — selftest",
    "",
    "## Widget",
    "",
    "#### Tier 1",
    "| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |",
    "|----|----------|--------|----------|--------------|------------|---------------|----------|",
    "| T1 | Spoofing | a | e | None | m | r | Low |",
    "| T2 | Tampering | b | e | None | m | r | Low |",
    "| T3 | Repudiation | c | e | None | m | r | Low |",
    "",
]) + "\n"

# Exercises BOTH defects: an invented `T3.S` suffix (Related Threats + coverage
# Threat-ID column) and a duplicate `### FIND-07` heading.
_SELFTEST_FINDINGS = "\n".join([
    "# Findings",
    "",
    "## Tier 1 — Direct Exposure (No Prerequisites)",
    "",
    "### FIND-07",
    "| Attribute | Value |",
    "|-----------|-------|",
    "| Component | Widget |",
    "| Related Threats | T1, T3.S |",
    "",
    "### FIND-07",
    "| Attribute | Value |",
    "|-----------|-------|",
    "| Component | Widget |",
    "| Related Threats | T2 |",
    "",
    "## Threat Coverage Verification",
    "| Threat ID | Finding ID | Status |",
    "|-----------|------------|--------|",
    "| T1 | FIND-07 | Covered (FIND-07) |",
    "| T2 | FIND-07 | Covered (FIND-07) |",
    "| T3.S | FIND-07 | Covered (FIND-07) |",
    "",
]) + "\n"


def _run_on(run_dir):
    suite, relevant = load_suite(run_dir)
    assert not relevant, "unexpected load errors: %s" % relevant
    res = normalize(suite)
    apply_writes(res)
    return res


def selftest():
    """Assert the normalizer fixes both defects and is idempotent. Returns bool."""
    tmp = tempfile.mkdtemp(prefix="normalize_ids_selftest_")
    try:
        ana = os.path.join(tmp, "2-stride-analysis.md")
        fnd = os.path.join(tmp, "3-findings.md")
        with open(ana, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(_SELFTEST_ANALYSIS)
        with open(fnd, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(_SELFTEST_FINDINGS)

        # First run: must make changes.
        res1 = _run_on(tmp)
        assert res1.changed, "first run made no changes on a defective fixture"

        with open(ana, encoding="utf-8") as fh:
            ana_text = fh.read()
        with open(fnd, encoding="utf-8") as fh:
            fnd_text = fh.read()

        # (A) no invented threat suffix survives anywhere.
        assert not STRIP_RE.search(ana_text), "T<n>.<suffix> left in analysis"
        assert not STRIP_RE.search(fnd_text), "T<n>.<suffix> left in findings"

        # (B) headings are a gapless FIND-01.. sequence in document order.
        headings = re.findall(r"^###\s+(FIND-\d+)", fnd_text, re.M)
        assert headings == ["FIND-01", "FIND-02"], \
            "headings not gapless-sequential: %r" % headings

        # The duplicate has been de-duplicated; no FIND-07 remains at all.
        assert "FIND-07" not in fnd_text, "duplicate FIND-07 not remapped"

        # verify.py agrees the sequential check now passes.
        suite = verify.Suite(tmp)
        name, ok, _ = verify.check_finding_ids_sequential(suite)
        assert ok, "verify.%s still fails after normalize" % name

        # Idempotence: a second run over the canonical dir changes nothing.
        res2 = _run_on(tmp)
        assert not res2.changed, "second run was not a no-op (not idempotent)"

        return True
    except AssertionError as exc:
        sys.stderr.write("selftest assertion failed: %s\n" % exc)
        return False
    finally:
        for name in os.listdir(tmp):
            try:
                os.remove(os.path.join(tmp, name))
            except OSError:
                pass
        try:
            os.rmdir(tmp)
        except OSError:
            pass


# --------------------------------------------------------------------------- #
# Main
# --------------------------------------------------------------------------- #

def _emit(res, header):
    lines = [header] if header else []
    for c in res.changes:
        lines.append("  " + c)
    for n in res.notes:
        lines.append("  " + n)
    sys.stdout.write("\n".join(lines) + ("\n" if lines else ""))


def main(argv=None):
    ap = argparse.ArgumentParser(
        description="Deterministically canonicalize threat/finding IDs in a "
                    "completed threat-model run directory.")
    ap.add_argument("run_dir", nargs="?",
                    help="completed threat-model run directory")
    ap.add_argument("--check", action="store_true",
                    help="report what WOULD change without writing; exit 1 if "
                         "any change is needed, 0 if already canonical")
    ap.add_argument("--selftest", action="store_true",
                    help="run built-in assertions on a synthetic fixture")
    args = ap.parse_args(argv)

    # Force UTF-8 so the report's few glyphs are identical on every platform
    # (same posture as verify.py / inventory.py / recon.py).
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    if args.selftest:
        ok = selftest()
        sys.stdout.write("selftest: %s\n" % ("PASS" if ok else "FAIL"))
        return 0 if ok else 1

    if not args.run_dir:
        sys.stderr.write("normalize_ids.py: a run directory is required "
                         "(or --selftest)\n")
        return 2
    if not os.path.isdir(args.run_dir):
        sys.stderr.write("normalize_ids.py: not a directory: %s\n" % args.run_dir)
        return 2

    suite, relevant = load_suite(args.run_dir)
    if relevant:
        sys.stderr.write("normalize_ids.py: cannot normalize — missing input:\n")
        for e in relevant:
            sys.stderr.write("  - %s\n" % e)
        return 2

    res = normalize(suite)

    if args.check:
        if res.changed:
            _emit(res, "normalize_ids: %d change(s) needed:" % len(res.changes))
            sys.stdout.write("normalize_ids: NOT canonical\n")
            return 1
        sys.stdout.write("normalize_ids: already canonical (0 changes)\n")
        return 0

    if res.changed:
        try:
            apply_writes(res)
        except OSError as exc:
            sys.stderr.write("normalize_ids.py: write failed: %s\n" % exc)
            return 2
        _emit(res, "normalize_ids: applied %d change(s):" % len(res.changes))
        sys.stdout.write("normalize_ids: wrote %d file(s)\n" % len(res.writes))
    else:
        sys.stdout.write("normalize_ids: already canonical (0 changes)\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
