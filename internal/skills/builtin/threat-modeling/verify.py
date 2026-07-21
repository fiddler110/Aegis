#!/usr/bin/env python3
"""Threat-model suite verifier — the mechanical half of the phase-6 self-check.

Companion script for the `threat-modeling` skill's final review round
(SKILL.md §5, phase 6 of §4.2) and the "Final self-check" in
`references/verification-and-updates.md`. That checklist mixes two kinds of
check: *judgment-bound* ones (does control X really contradict Y, is this
severity defensible) that only the model can make, and *mechanical* ones
(does every `DF##` a threat cites actually exist, do the counts agree, is
any skeleton placeholder left unfilled) that are pure cross-file
bookkeeping. This script runs the mechanical subset deterministically so the
phased build's cross-file consistency is a cheap grep-and-compare instead of
an expensive, non-deterministic model re-read of the whole seven-file suite.

It operates on a **completed run directory** — the
`.aegis/security/threat-model/<framework>-<target>-<timestamp>/` folder the
phases wrote — and reads only the markdown suite. It deliberately does NOT
read or require `inventory.yaml`; the sidecar's own consistency is a
separate concern (`skeleton-inventory.md`'s post-write check).

Each check prints `PASS`/`FAIL <check-name>` with `file:line` evidence for
every failure, and the process exits non-zero if any check fails — so it can
gate a run in CI or a script, not just inform a human.

Contract (same posture as the sibling `recon.py`):
  * Python 3.7+, standard library only — no third-party imports.
  * Deterministic: same run directory → byte-identical output (every list
    sorted), so two invocations never disagree.
  * Mechanical only. Every check here is a grep/parse/compare a human could
    reproduce by hand; genuinely judgment-bound checks stay with the model.

Usage:
  python verify.py <run-dir> [--quiet]

  <run-dir>   a completed threat-model run directory (contains
              0-assessment.md, 0.1-architecture.md, 1-model.md,
              2-<framework>-analysis.md, 3-findings.md)
  --quiet     print only FAIL checks and the summary line

Exit code: 0 iff every check passed, 1 otherwise (2 on a usage/IO error).
"""

import argparse
import glob
import os
import re
import sys

# Sibling script (ships in the same skill dir). Reused for its hardened
# deployment-classification parse so verify.py and inventory.py can never
# disagree about how the class is read from the docs.
try:
    import inventory as _inv
except Exception:   # pragma: no cover - defensive; check degrades to pass
    _inv = None

# --------------------------------------------------------------------------- #
# Fixed vocabulary (from the skeletons / output-formats.md)
# --------------------------------------------------------------------------- #

# Prerequisite -> derived Tier. output-formats.md's tier-derivation table and
# skeleton-stride.md's POST-TABLE-CHECK both use exactly this mapping.
PREREQ_TIER = {
    "none": 1,
    "authenticated user": 2,
    "internal network": 2,
    "local process": 3,
    "host compromise": 3,
}

# Coverage-table statuses that are forbidden outright (output-formats.md:
# "There is no fourth option" — Accepted Risk / Needs Review are banned).
FORBIDDEN_STATUSES = ("accepted risk", "needs review")

# A threat ID is a literal token: T1, T01, T04.S — never decomposed.
TID_RE = re.compile(r"T\d+(?:\.[A-Za-z0-9]+)?")
TID_FULL_RE = re.compile(r"^T\d+(?:\.[A-Za-z0-9]+)?$")
DF_RE = re.compile(r"DF\d+")
FIND_HEADING_RE = re.compile(r"^###\s+(FIND-\d+)\b")

# Leftover skeleton syntax — any hit is a placeholder never filled in. The
# PENDING entry is a *prefix* (P38.7): scaffold.py now emits keyed markers
# (`<!-- PENDING: deployment-classification -->`), so matching the bare
# `<!-- PENDING -->` would miss every unfilled section; the prefix catches them
# all, keyed or not.
SKELETON_MARKERS = ("[FILL", "[REPEAT", "[END-REPEAT", "<!-- PENDING")

# Files scanned for leftover skeleton syntax (check 1). The whole suite.
TEXT_EXTS = (".md", ".mmd", ".yaml", ".yml", ".txt")


# --------------------------------------------------------------------------- #
# Small markdown helpers
# --------------------------------------------------------------------------- #

def read_lines(path):
    """Read a file as UTF-8 lines (no trailing newline), tolerant of encoding."""
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read().splitlines()


def split_row(line):
    """Split a markdown table row on unescaped pipes, dropping the outer pipes."""
    s = line.strip()
    if s.startswith("|"):
        s = s[1:]
    if s.endswith("|"):
        s = s[:-1]
    return [c.strip() for c in s.split("|")]


def is_separator_line(line):
    """True for a markdown header/body separator like `|---|:--:|`."""
    s = line.strip()
    if not s.startswith("|"):
        return False
    cells = [c for c in split_row(line) if c.strip()]
    return bool(cells) and all(re.fullmatch(r":?-{2,}:?", c) for c in cells)


def clean_cell(cell):
    """Strip whitespace, surrounding backticks, and bold markers from a cell."""
    c = cell.strip()
    c = re.sub(r"\*\*(.*?)\*\*", r"\1", c)
    c = c.strip().strip("`").strip()
    return c


def is_placeholder(cells):
    joined = " ".join(cells)
    return any(m in joined for m in ("[FILL", "[REPEAT", "[END-REPEAT"))


def parse_tables(lines):
    """Return every markdown table: {header:[...], rows:[(cells, lineno)]}.

    lineno is 1-based. A table is a `|...|` header line immediately followed
    by a separator line, then contiguous `|...|` data rows.
    """
    tables = []
    n = len(lines)
    i = 0
    while i < n:
        line = lines[i]
        if line.strip().startswith("|") and i + 1 < n and is_separator_line(lines[i + 1]):
            header = split_row(line)
            rows = []
            j = i + 2
            while j < n and lines[j].strip().startswith("|"):
                if is_separator_line(lines[j]):
                    j += 1
                    continue
                rows.append((split_row(lines[j]), j + 1))
                j += 1
            tables.append({"header": header, "rows": rows, "line": i + 1})
            i = j
        else:
            i += 1
    return tables


def col_index(header, name):
    """Index of the header cell whose cleaned text equals `name` (case-insensitive)."""
    want = name.lower()
    for i, h in enumerate(header):
        if clean_cell(h).lower() == want:
            return i
    return None


def find_table(tables, *required):
    """First table whose header contains every `required` name (case-insensitive)."""
    for t in tables:
        if all(col_index(t["header"], r) is not None for r in required):
            return t
    return None


def col_values(table, name):
    """Cleaned, non-placeholder values of a named column, with line numbers."""
    idx = col_index(table["header"], name)
    out = []
    if idx is None:
        return out
    for cells, lineno in table["rows"]:
        if is_placeholder(cells) or idx >= len(cells):
            continue
        out.append((clean_cell(cells[idx]), lineno))
    return out


# --------------------------------------------------------------------------- #
# Suite loading
# --------------------------------------------------------------------------- #

class Suite:
    """Lazily-parsed handle on the five markdown files a run must contain."""

    def __init__(self, run_dir):
        self.run_dir = run_dir
        self.errors = []            # hard load errors (missing files etc.)
        self._lines = {}            # basename -> lines
        self.paths = {}             # logical name -> path

        def locate(name, pattern):
            matches = sorted(glob.glob(os.path.join(run_dir, pattern)))
            if not matches:
                self.errors.append("missing file: %s" % pattern)
                return None
            if len(matches) > 1 and name == "analysis":
                self.errors.append(
                    "multiple analysis files match 2-*-analysis.md: %s"
                    % ", ".join(os.path.basename(m) for m in matches))
            self.paths[name] = matches[0]
            return matches[0]

        locate("assessment", "0-assessment.md")
        locate("architecture", "0.1-architecture.md")
        locate("model", "1-model.md")
        locate("analysis", "2-*-analysis.md")
        locate("findings", "3-findings.md")

    def lines(self, name):
        path = self.paths.get(name)
        if not path:
            return []
        base = os.path.basename(path)
        if base not in self._lines:
            self._lines[base] = read_lines(path)
        return self._lines[base]

    def base(self, name):
        path = self.paths.get(name)
        return os.path.basename(path) if path else name

    def tables(self, name):
        return parse_tables(self.lines(name))


def ev(base, lineno, text):
    """Format one `file:line  text` evidence line."""
    return "%s:%d  %s" % (base, lineno, text)


# --------------------------------------------------------------------------- #
# Analysis-file scan (component headings, threat rows with tier + prerequisite)
# --------------------------------------------------------------------------- #

# `##` headings in the analysis file that are structural, not components.
NON_COMPONENT_HEADINGS = {"summary", "handing off to findings"}


def analysis_components(lines):
    """`## <Component>` headings in the analysis file, minus structural ones."""
    out = []
    for i, line in enumerate(lines):
        m = re.match(r"^##(?!#)\s+(.+)$", line.strip())
        if not m:
            continue
        name = clean_cell(m.group(1))
        if name.lower() in NON_COMPONENT_HEADINGS or name.startswith("[FILL"):
            continue
        out.append((name, i + 1))
    return out


def analysis_threats(lines):
    """Threat rows from the per-component tier tables.

    Returns list of dicts: {id, tier (1/2/3 or None), prereq, severity, comp,
    line}. A threat row is a data row whose first (ID) column is exactly a
    threat-ID token, sitting under a `#### Tier N` sub-section.
    """
    out = []
    n = len(lines)
    comp = None
    tier = None
    header = None
    for i in range(n):
        s = lines[i].strip()

        mc = re.match(r"^##(?!#)\s+(.+)$", s)
        if mc:
            comp = clean_cell(mc.group(1))
            tier = None
            header = None
            continue

        mt = re.match(r"^####\s+Tier\s+([123])\b", s)
        if mt:
            tier = int(mt.group(1))
            header = None
            continue
        if s.startswith("####"):
            tier = None
            header = None
            continue

        if s.startswith("|"):
            if i + 1 < n and is_separator_line(lines[i + 1]):
                header = split_row(lines[i])
                continue
            if is_separator_line(lines[i]):
                continue
            cells = split_row(lines[i])
            if not cells or is_placeholder(cells):
                continue
            id0 = clean_cell(cells[0])
            if not TID_FULL_RE.match(id0):
                continue
            prereq = ""
            sev = ""
            if header:
                pi = col_index(header, "Prerequisite")
                si = col_index(header, "Severity")
                if pi is not None and pi < len(cells):
                    prereq = clean_cell(cells[pi])
                if si is not None and si < len(cells):
                    sev = clean_cell(cells[si])
            out.append({"id": id0, "tier": tier, "prereq": prereq,
                        "severity": sev, "comp": comp, "line": i + 1})
    return out


def parse_findings(lines):
    """Finding blocks: [{id, lineno, component, cvss}] from `### FIND-##` sections."""
    out = []
    cur = None
    for i, line in enumerate(lines):
        m = FIND_HEADING_RE.match(line.strip())
        if m:
            cur = {"id": m.group(1), "lineno": i + 1, "component": None, "cvss": None}
            out.append(cur)
            continue
        if cur is not None and line.strip().startswith("|"):
            cells = split_row(line)
            if len(cells) >= 2:
                key = clean_cell(cells[0]).lower()
                val = clean_cell(cells[1])
                if key == "component" and cur["component"] is None:
                    cur["component"] = val
                elif key.startswith("cvss") and cur["cvss"] is None:
                    cur["cvss"] = val
    return out


# --------------------------------------------------------------------------- #
# Checks — each returns (name, passed, [evidence lines])
# --------------------------------------------------------------------------- #

def check_no_skeleton_syntax(suite):
    """1. No leftover skeleton placeholders anywhere in the suite."""
    fails = []
    files = sorted(
        p for p in glob.glob(os.path.join(suite.run_dir, "*"))
        if os.path.isfile(p) and os.path.splitext(p)[1].lower() in TEXT_EXTS
    )
    for path in files:
        base = os.path.basename(path)
        for lineno, line in enumerate(read_lines(path), 1):
            for marker in SKELETON_MARKERS:
                if marker in line:
                    fails.append(ev(base, lineno, "leftover `%s`" % marker))
    return ("no-leftover-skeleton-syntax", not fails, fails)


def check_component_name_consistency(suite):
    """2. Component names identical across architecture / model / analysis."""
    fails = []
    arch_tbl = find_table(suite.tables("architecture"), "Component", "Anchor")
    model_tbl = find_table(suite.tables("model"), "Element", "Trust Boundary")

    if arch_tbl is None:
        fails.append(ev(suite.base("architecture"), 0, "Key Components table (Component|Anchor) not found"))
    if model_tbl is None:
        fails.append(ev(suite.base("model"), 0, "Element Table (Element|Trust Boundary) not found"))

    arch = {v: ln for v, ln in (col_values(arch_tbl, "Component") if arch_tbl else [])}
    model = {v: ln for v, ln in (col_values(model_tbl, "Element") if model_tbl else [])}
    ana = {name: ln for name, ln in analysis_components(suite.lines("analysis"))}

    if arch or model or ana:
        names = sorted(set(arch) | set(model) | set(ana))
        for name in names:
            present = []
            if name in arch:
                present.append("architecture")
            if name in model:
                present.append("model")
            if name in ana:
                present.append("analysis")
            if len(present) == 3:
                continue
            missing = [f for f in ("architecture", "model", "analysis") if f not in present]
            fails.append("component %r present in {%s} but missing from {%s}"
                         % (name, ", ".join(present), ", ".join(missing)))
    return ("component-name-consistency", not fails, fails)


def check_dataflow_refs(suite):
    """3. Every DF## cited in the analysis exists in the model's Data Flow Table."""
    fails = []
    df_tbl = find_table(suite.tables("model"), "ID", "Source", "Target")
    defined = set()
    if df_tbl is None:
        fails.append(ev(suite.base("model"), 0, "Data Flow Table (ID|Source|Target) not found"))
    else:
        for v, _ in col_values(df_tbl, "ID"):
            for m in DF_RE.findall(v):
                defined.add(m)

    seen = {}  # df -> first line in analysis
    for i, line in enumerate(suite.lines("analysis"), 1):
        for m in DF_RE.finditer(line):
            seen.setdefault(m.group(0), i)
    for df in sorted(seen, key=lambda d: (int(d[2:]), d)):
        if df not in defined:
            fails.append(ev(suite.base("analysis"), seen[df],
                            "%s referenced but not in model Data Flow Table" % df))
    return ("dataflow-refs-defined", not fails, fails)


def check_threat_coverage_bijection(suite):
    """4. Every analysis threat appears exactly once in coverage, and vice-versa."""
    fails = []
    threats = analysis_threats(suite.lines("analysis"))
    ana_lines = {}
    ana_dupes = []
    for t in threats:
        if t["id"] in ana_lines:
            ana_dupes.append((t["id"], t["line"]))
        else:
            ana_lines[t["id"]] = t["line"]

    cov_tbl = find_table(suite.tables("findings"), "Threat ID", "Status")
    cov_counts = {}
    cov_lines = {}
    if cov_tbl is None:
        fails.append(ev(suite.base("findings"), 0, "Threat Coverage Verification table (Threat ID|Status) not found"))
    else:
        for v, ln in col_values(cov_tbl, "Threat ID"):
            m = TID_RE.search(v)
            if not m:
                continue
            tid = m.group(0)
            cov_counts[tid] = cov_counts.get(tid, 0) + 1
            cov_lines.setdefault(tid, ln)

    for tid, ln in ana_dupes:
        fails.append(ev(suite.base("analysis"), ln,
                        "threat %s defined more than once in analysis" % tid))
    for tid in sorted(ana_lines, key=_tid_sort):
        c = cov_counts.get(tid, 0)
        if c == 0:
            fails.append(ev(suite.base("analysis"), ana_lines[tid],
                            "threat %s missing from coverage table" % tid))
        elif c > 1:
            fails.append(ev(suite.base("findings"), cov_lines[tid],
                            "threat %s appears %d times in coverage table" % (tid, c)))
    for tid in sorted(cov_counts, key=_tid_sort):
        if tid not in ana_lines:
            fails.append(ev(suite.base("findings"), cov_lines[tid],
                            "coverage row %s has no matching threat in analysis" % tid))
    return ("threat-coverage-bijection", not fails, fails)


def _tid_sort(tid):
    m = re.match(r"T(\d+)(?:\.(.+))?", tid)
    if not m:
        return (0, "", tid)
    return (int(m.group(1)), m.group(2) or "", tid)


def check_finding_ids_sequential(suite):
    """5. FIND-## ids are sequential from 1, no gaps, in document order."""
    fails = []
    ids = []
    for i, line in enumerate(suite.lines("findings"), 1):
        m = FIND_HEADING_RE.match(line.strip())
        if m:
            ids.append((int(m.group(1).split("-")[1]), m.group(1), i))
    if not ids:
        fails.append(ev(suite.base("findings"), 0, "no `### FIND-##` finding headings found"))
    else:
        expected = 1
        for num, fid, ln in ids:
            if num != expected:
                fails.append(ev(suite.base("findings"), ln,
                                "expected FIND-%02d in document order, found %s" % (expected, fid)))
            expected = num + 1
    return ("finding-ids-sequential", not fails, fails)


def check_tier_prerequisite_consistency(suite):
    """6. Each threat row sits under the Tier its Prerequisite maps to."""
    fails = []
    for t in analysis_threats(suite.lines("analysis")):
        prereq = t["prereq"].lower()
        if prereq not in PREREQ_TIER:
            continue  # unknown/blank prereq is a different check's concern
        derived = PREREQ_TIER[prereq]
        if t["tier"] is not None and t["tier"] != derived:
            fails.append(ev(suite.base("analysis"), t["line"],
                            "%s under Tier %d but Prerequisite %r derives Tier %d"
                            % (t["id"], t["tier"], t["prereq"], derived)))
    return ("tier-prerequisite-consistency", not fails, fails)


def check_count_consistency(suite):
    """7. Assessment Action Summary Total matches real threat/finding counts."""
    fails = []
    threat_count = len(analysis_threats(suite.lines("analysis")))
    finding_count = len(parse_findings(suite.lines("findings")))

    total_tbl = find_table(suite.tables("assessment"), "Tier", "Threats", "Findings")
    if total_tbl is None:
        fails.append(ev(suite.base("assessment"), 0, "Action Summary table (Tier|Threats|Findings) not found"))
        return ("count-consistency", False, fails)

    ti = col_index(total_tbl["header"], "Threats")
    fi = col_index(total_tbl["header"], "Findings")
    total_row = None
    for cells, ln in total_tbl["rows"]:
        if cells and clean_cell(cells[0]).lower() == "total":
            total_row = (cells, ln)
            break
    if total_row is None:
        fails.append(ev(suite.base("assessment"), total_tbl["line"], "no Total row in Action Summary table"))
        return ("count-consistency", False, fails)

    cells, ln = total_row
    stated_t = _as_int(cells[ti]) if ti is not None and ti < len(cells) else None
    stated_f = _as_int(cells[fi]) if fi is not None and fi < len(cells) else None
    if stated_t != threat_count:
        fails.append(ev(suite.base("assessment"), ln,
                        "Total threats = %s but analysis has %d threat rows"
                        % (_show(stated_t, cells, ti), threat_count)))
    if stated_f != finding_count:
        fails.append(ev(suite.base("assessment"), ln,
                        "Total findings = %s but findings file has %d FIND-## entries"
                        % (_show(stated_f, cells, fi), finding_count)))
    return ("count-consistency", not fails, fails)


def _as_int(cell):
    m = re.search(r"\d+", clean_cell(cell))
    return int(m.group(0)) if m else None


def _show(parsed, cells, idx):
    if parsed is not None:
        return str(parsed)
    raw = clean_cell(cells[idx]) if idx is not None and idx < len(cells) else ""
    return "%r (unparseable)" % raw


def check_no_forbidden_statuses(suite):
    """8. Coverage table uses no Accepted Risk / Needs Review status."""
    fails = []
    cov_tbl = find_table(suite.tables("findings"), "Threat ID", "Status")
    if cov_tbl is None:
        fails.append(ev(suite.base("findings"), 0, "Threat Coverage Verification table (Threat ID|Status) not found"))
        return ("no-forbidden-coverage-statuses", False, fails)
    for v, ln in col_values(cov_tbl, "Status"):
        low = v.lower()
        for bad in FORBIDDEN_STATUSES:
            if bad in low:
                fails.append(ev(suite.base("findings"), ln, "forbidden status %r" % v))
                break
    return ("no-forbidden-coverage-statuses", not fails, fails)


def check_external_av_consistency(suite):
    """9. (best-effort) No AV:N finding on a component whose exposure is none."""
    fails = []
    exp_tbl = find_table(suite.tables("architecture"), "Component", "External Reachability")
    if exp_tbl is None:
        # Best-effort: can't evaluate without the exposure table, treat as pass.
        return ("external-av-consistency", True, [])
    ci = col_index(exp_tbl["header"], "Component")
    ri = col_index(exp_tbl["header"], "External Reachability")
    reach = {}
    for cells, _ in exp_tbl["rows"]:
        if is_placeholder(cells) or ci >= len(cells) or ri >= len(cells):
            continue
        reach[clean_cell(cells[ci])] = clean_cell(cells[ri]).lower()

    for f in parse_findings(suite.lines("findings")):
        comp = f["component"]
        cvss = f["cvss"] or ""
        if not comp or "AV:N" not in cvss.upper():
            continue
        if reach.get(comp) == "none":
            fails.append(ev(suite.base("findings"), f["lineno"],
                            "%s has AV:N but component %r External Reachability = none"
                            % (f["id"], comp)))
    return ("external-av-consistency", not fails, fails)


def check_deployment_classification_consistency(suite):
    """10. 0.1-architecture.md and 2-<framework>-analysis.md must assert the
    SAME deployment classification. The class is binding on every threat's
    prerequisite floor and every finding's CVSS AV, so a silent divergence
    (architecture says internal-network, the analysis header says
    internet-facing) skews every derived tier — and it is caught by no other
    check. inventory.py reads the sidecar's classification from these same
    files, so a disagreement here also means a wrong sidecar."""
    if _inv is None:
        return ("deployment-classification-consistency", True, [])

    def cls(text):
        v = _inv._labeled_deployment(text)
        if v:
            return v
        v = _inv._first_deployment_in_order(
            _inv.section_body(text, "Deployment Classification"))
        return v or _inv._first_deployment_in_order(text)

    arch_text = "\n".join(suite.lines("architecture"))
    ana_text = "\n".join(suite.lines("analysis"))
    a, b = cls(arch_text), cls(ana_text)
    if a and b and a != b:
        anchor = 0
        for i, line in enumerate(suite.lines("architecture"), 1):
            if clean_cell(line).lower().startswith("deployment classification"):
                anchor = i
                break
        return ("deployment-classification-consistency", False,
                [ev(suite.base("architecture"), anchor,
                    "deployment classification %r here disagrees with %r in %s"
                    % (a, b, suite.base("analysis")))])
    return ("deployment-classification-consistency", True, [])


ALL_CHECKS = [
    check_no_skeleton_syntax,
    check_component_name_consistency,
    check_dataflow_refs,
    check_threat_coverage_bijection,
    check_finding_ids_sequential,
    check_tier_prerequisite_consistency,
    check_count_consistency,
    check_no_forbidden_statuses,
    check_external_av_consistency,
    check_deployment_classification_consistency,
]


# --------------------------------------------------------------------------- #
# Main
# --------------------------------------------------------------------------- #

def main(argv=None):
    ap = argparse.ArgumentParser(
        description="Deterministic mechanical self-check for a completed "
                    "threat-model run directory.")
    ap.add_argument("run_dir", help="completed threat-model run directory")
    ap.add_argument("--quiet", action="store_true",
                    help="print only FAIL checks and the summary line")
    args = ap.parse_args(argv)

    # The report uses a few Unicode glyphs; force UTF-8 so output is identical
    # on every platform (Windows cp1252 would otherwise crash on them).
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    if not os.path.isdir(args.run_dir):
        sys.stderr.write("verify.py: not a directory: %s\n" % args.run_dir)
        return 2

    suite = Suite(args.run_dir)

    out = []
    passed = 0
    failed = 0

    # Hard load errors (missing required files) fail the whole run up front.
    if suite.errors:
        failed += 1
        out.append("FAIL suite-files-present")
        for e in suite.errors:
            out.append("       - %s" % e)

    for check in ALL_CHECKS:
        try:
            name, ok, evidence = check(suite)
        except Exception as exc:  # a parser crash is itself a failure, not a stop
            name = getattr(check, "__name__", "check").replace("check_", "").replace("_", "-")
            ok, evidence = False, ["internal error: %s: %s" % (type(exc).__name__, exc)]
        if ok:
            passed += 1
            if not args.quiet:
                out.append("PASS %s" % name)
        else:
            failed += 1
            out.append("FAIL %s" % name)
            for line in sorted(evidence):
                out.append("       - %s" % line)

    out.append("")
    out.append("%d passed, %d failed" % (passed, failed))
    sys.stdout.write("\n".join(out) + "\n")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
