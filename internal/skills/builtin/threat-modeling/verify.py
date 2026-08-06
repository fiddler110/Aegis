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

Checks 16-19 are the one exception to "mechanical bookkeeping only": they are
a deliberately weak *substance* floor (P52.8) — a bare-filename Evidence cell,
a literal `TBD`, a table that is nothing but "none identified", a narrative
section shorter than a sentence. They are still mechanical (no model call), and
every threshold is tunable; see `SUBSTANCE` below.

Usage:
  python verify.py <run-dir> [--quiet] [substance-floor flags]

  <run-dir>   a completed threat-model run directory (contains
              0-assessment.md, 0.1-architecture.md, 1-model.md,
              2-<framework>-analysis.md, 3-findings.md)
  --quiet     print only FAIL checks and the summary line
              (`--help` lists the substance-floor thresholds)

Exit code: 0 iff every check passed, 1 otherwise (2 on a usage/IO error).
"""

import argparse
import glob
import json
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

# Coverage Ledger status: "Covered — <component>" or "Excluded — <reason>" —
# the em-dash-or-hyphen separator plus a non-empty reason is mandatory (a bare
# "Covered" or "Excluded" is a placeholder wearing the right word).
COVERAGE_STATUS_RE = re.compile(r"^(covered|excluded)\b\s*[—-]\s*\S", re.I)

# Leftover skeleton syntax — any hit is a placeholder never filled in. The
# PENDING entry is a *prefix* (P38.7): scaffold.py now emits keyed markers
# (`<!-- PENDING: deployment-classification -->`), so matching the bare
# `<!-- PENDING -->` would miss every unfilled section; the prefix catches them
# all, keyed or not.
SKELETON_MARKERS = ("[FILL", "[REPEAT", "[END-REPEAT", "<!-- PENDING")

# Files scanned for leftover skeleton syntax (check 1). The whole suite.
# NOTE: `.json` is deliberately absent — the run directory's hidden sidecars
# (`.scaffold-manifest.json` here, `.quality-stamp.json` from the phased drive)
# are metadata about the suite, not suite content.
TEXT_EXTS = (".md", ".mmd", ".yaml", ".yml", ".txt")

# The section manifest scaffold.py writes (P52.7): every site it left a
# `<!-- PENDING: <key> -->` marker in, keyed by its enclosing heading. See
# check_section_bodies_nonempty. A suite scaffolded before this existed simply
# has no manifest, and that check degrades to a pass.
MANIFEST_FILE = ".scaffold-manifest.json"

# The five markdown files every check reads, in report order.
SUITE_FILES = ("assessment", "architecture", "model", "analysis", "findings")


# --------------------------------------------------------------------------- #
# Substance floor (P52.8) — every threshold in one tunable place
# --------------------------------------------------------------------------- #
#
# Checks 1-15 are all *structural*: they prove the suite is shaped right and
# that nothing was left visibly unfilled. None of them rejects vacuous content.
# A suite in which every Evidence cell reads `see code`, every Mitigation reads
# `TBD` and every category reads `None identified` passes all fifteen and gets
# stamped. The P38.1 quality pass is the intended backstop, but it is an LLM
# call and is CLI-only (P52.12), so the TUI path has no substance gate at all.
#
# Checks 16-19 are that mechanical floor. They are deliberately *weak*: a false
# failure costs a verify bounce and erodes trust in the whole suite of checks,
# which is worth more than catching every marginal cell. So each rule fires only
# on the unambiguous case — a cell that is a bare filename with nothing else, a
# cell that is *exactly* a placeholder token, a table in which essentially every
# row is "none identified", a narrative section shorter than one sentence.
#
# Every threshold lives here and is overridable from the command line (see
# `_apply_substance_overrides`), so a stricter run is a flag away and a suite
# with a legitimately unusual shape can be let through without editing code.
SUBSTANCE = {
    # Table columns whose cells must carry a real citation (SKILL.md §3: "Every
    # threat entry names the file, config, or code path that makes it real").
    # `Anchor` is deliberately absent — the Key Components table's Anchor cell
    # is *supposed* to be a bare file/class path.
    "evidence_columns": ("evidence",),

    # Table columns the skeletons mark as required-substantive: a literal
    # placeholder token in one of these is never acceptable. Matching is on the
    # column's header text, so a framework that renames a column simply opts out
    # rather than misfiring. Three deliberate omissions: `Prerequisite` (`None`
    # is one of its five fixed legal values), and the broad `Description` /
    # `Configuration` columns, where a bare `N/A` is often the honest answer
    # ("Analysis Scope | Time budget | N/A"). This list is also what makes a
    # table "threat-shaped" for check 18's per-file aggregate.
    "substantive_columns": (
        "evidence", "mitigation", "threat", "residual risk", "control",
        "attack vector", "justification", "business impact",
    ),

    # Cells that are *exactly* one of these (after cleaning, lowercasing and
    # trimming decorative punctuation) are placeholders. Substring matching is
    # deliberately not used: "TBD — owner to confirm by Q3" is a real note, and
    # only a bare "TBD" is a placeholder.
    "placeholder_tokens": (
        "tbd", "t.b.d", "tba", "to be determined", "to be decided", "todo",
        "to do", "n/a", "n.a", "not applicable", "see above", "see below",
        "see code", "see codebase", "see source", "see the code", "as above",
        "same as above", "fixme", "xxx", "?", "??", "???", "placeholder",
        "pending", "unknown", "unspecified", "not specified", "not determined",
    ),

    # "None identified" is an explicitly legitimate, complete entry (SKILL.md
    # §4) — one or two per table proves the pass happened and found nothing.
    # Only a table in which *every* row says it means the pass never ran, so the
    # cap defaults to 0.95: below 1.0 nothing fires. Lower it for a stricter run.
    "none_identified_max_fraction": 0.95,
    # Tables smaller than this are never judged: a 3-row table that is all
    # "none identified" is an ordinary sparse component.
    "none_identified_min_rows": 6,
    # The same cap applied to a whole file's threat-shaped tables, which catches
    # the suite-wide vacuous pass that per-table sparsity hides. Higher floor
    # because it aggregates.
    "none_identified_min_rows_file": 12,

    # Minimum substantive characters in a narrative (`kind: "prose"`) section
    # from the scaffold manifest. One short sentence clears it; "TBD." or
    # "See findings." does not.
    "min_prose_chars": 40,
    # Manifest keys exempt from the prose floor — sections whose complete,
    # correct fill is legitimately a single short token. The deployment
    # classification is exactly one of four fixed words.
    "prose_floor_exempt_keys": ("deployment-classification",),
}

# A cell that says the pass ran and found nothing: "none identified",
# "none identified — no auth surface", "No Tier 1 threats identified".
NONE_IDENTIFIED_RE = re.compile(
    r"\bnone\s+identified\b|\bnot\s+identified\b|\bno\b[\w\s-]{0,40}\bidentified\b",
    re.I)

# A citation that pins a location: `auth.go:88`, `#L88`, `L88`, `lines 20-40`.
LINE_NUMBER_RE = re.compile(r":\d+|#L\d+|\bL\d+\b|\blines?\s+\d+", re.I)

# A single token that is only a path/filename. `internal/server/auth.go` and
# `config.yaml` match; `newEngine()` (parens), `provider.small_model` (no
# short extension) and `.env` (no stem) do not.
PATHY_TOKEN_RE = re.compile(r"^[\w./\\@~+-]+$")
FILE_EXT_RE = re.compile(r"[^/\\.]\.[A-Za-z0-9]{1,6}$")

MD_LINK_RE = re.compile(r"\[([^\]]*)\]\([^)]*\)")


# --------------------------------------------------------------------------- #
# Small markdown helpers
# --------------------------------------------------------------------------- #

def read_lines(path):
    """Read a file as UTF-8 lines (no trailing newline), tolerant of encoding."""
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read().splitlines()


def split_row(line):
    """Split a markdown table row on unescaped pipes, dropping the outer
    pipes. A `|` inside a backtick-quoted code span (e.g. an Evidence cell
    citing `` `a || b` ``) or escaped as `\\|` is literal cell content, not
    a delimiter — content phases routinely cite JS/shell snippets with `||`
    in this column."""
    s = line.strip()
    if s.startswith("|"):
        s = s[1:]
    if s.endswith("|"):
        s = s[:-1]
    cells = []
    cur = []
    in_code = False
    i = 0
    n = len(s)
    while i < n:
        ch = s[i]
        if ch == "\\" and i + 1 < n and s[i + 1] == "|":
            cur.append("|")
            i += 2
            continue
        if ch == "`":
            in_code = not in_code
            cur.append(ch)
            i += 1
            continue
        if ch == "|" and not in_code:
            cells.append("".join(cur).strip())
            cur = []
            i += 1
            continue
        cur.append(ch)
        i += 1
    cells.append("".join(cur).strip())
    return cells


HTML_COMMENT_LINE_RE = re.compile(r"^<!--.*-->$")


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
    by a separator line, then contiguous `|...|` data rows. A single-line HTML
    comment (`<!-- ... -->`, e.g. scaffold.py's `table()` guidance line, which
    the fill step leaves in place — only the `<!-- PENDING: <key> -->` marker
    after it gets replaced) does not end the table; it is skipped so real rows
    written after it still get parsed.
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
            while j < n:
                s = lines[j].strip()
                if s.startswith("|"):
                    if is_separator_line(lines[j]):
                        j += 1
                        continue
                    rows.append((split_row(lines[j]), j + 1))
                    j += 1
                    continue
                if HTML_COMMENT_LINE_RE.match(s):
                    j += 1
                    continue
                break
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
        self._manifest = False      # False = not loaded yet; None = absent

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

    def logical(self, basename):
        """Logical suite name for a real filename, or None if not a suite file.

        The manifest names files as scaffold.py wrote them; the analysis file's
        name embeds the framework, so a manifest from a different framework (or
        a renamed file) simply resolves to nothing and its entries are skipped.
        """
        for name, path in self.paths.items():
            if os.path.basename(path) == basename:
                return name
        return None

    def manifest(self):
        """The parsed section manifest, or None when absent/unreadable/invalid.

        None is the *backward-compatible* answer, not an error: suites
        scaffolded before P52.7 (and resumed ones, which exist in the wild) have
        no sidecar, and a run must still verify. Every field is validated here
        so a truncated or hand-edited manifest degrades to None rather than
        crashing a check or inventing failures.
        """
        if self._manifest is not False:
            return self._manifest
        self._manifest = None
        try:
            with open(os.path.join(self.run_dir, MANIFEST_FILE),
                      encoding="utf-8") as fh:
                data = json.load(fh)
        except Exception:       # missing, unreadable, or not JSON
            return None
        if not isinstance(data, dict) or not isinstance(data.get("sections"), list):
            return None
        out = []
        for s in data["sections"]:
            if not isinstance(s, dict):
                continue
            f, h, k = s.get("file"), s.get("heading"), s.get("key")
            lvl = s.get("level")
            if not (isinstance(f, str) and isinstance(h, str) and isinstance(k, str)):
                continue
            if not isinstance(lvl, int) or not 1 <= lvl <= 6:
                continue
            out.append({
                "file": f,
                "key": k,
                "heading": h,
                "level": lvl,
                "kind": s.get("kind") if s.get("kind") in ("table", "prose") else "prose",
                "columns": s.get("columns") if isinstance(s.get("columns"), list) else None,
                "to_eof": bool(s.get("to_eof")),
            })
        self._manifest = out or None
        return self._manifest


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
    """Finding blocks: [{id, lineno, component, cvss, related}] from `### FIND-##`.

    `related` is the set of threat-ID tokens (T##) linked in the finding's
    "Related Threats" attribute row, or None when that row is absent — mirroring
    the component/cvss capture below. The existing keys are unchanged so the
    other checks that read them keep working.
    """
    out = []
    cur = None
    for i, line in enumerate(lines):
        m = FIND_HEADING_RE.match(line.strip())
        if m:
            cur = {"id": m.group(1), "lineno": i + 1, "component": None,
                   "cvss": None, "related": None}
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
                elif key == "related threats" and cur["related"] is None:
                    cur["related"] = set(TID_RE.findall(val))
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


def check_coverage_ledger(suite):
    """11. Coverage Ledger exists, has rows, and every Status is a properly
    reasoned Covered/Excluded entry — the two-ledger completeness accounting
    from SKILL.md §2 step 6."""
    fails = []
    tbl = find_table(suite.tables("architecture"), "Directory", "Status")
    if tbl is None:
        fails.append(ev(suite.base("architecture"), 0,
                        "Coverage Ledger table (Directory|Status) not found"))
        return ("coverage-ledger-complete", False, fails)

    dirs = col_values(tbl, "Directory")
    if not dirs:
        fails.append(ev(suite.base("architecture"), tbl["line"],
                        "Coverage Ledger has no directory rows"))

    seen = {}
    for v, ln in dirs:
        seen.setdefault(v, []).append(ln)
    for name, lines_ in seen.items():
        if len(lines_) > 1:
            fails.append(ev(suite.base("architecture"), lines_[1],
                            "directory %r appears more than once in the Coverage Ledger" % name))

    for v, ln in col_values(tbl, "Status"):
        if not COVERAGE_STATUS_RE.match(v):
            fails.append(ev(suite.base("architecture"), ln,
                            "Status %r is not \"Covered — <component>\" or "
                            "\"Excluded — <reason>\" with a reason" % v))
    return ("coverage-ledger-complete", not fails, fails)


def check_finding_bodies_nonempty(suite):
    """12. Every `#### <Section>` inside a `### FIND-##` block has real prose.

    Each finding carries `#### Description`, `#### Evidence`, `#### Remediation`,
    `#### Verification` subsections. A weak model can delete the `<!-- PENDING -->`
    marker without writing anything in its place, leaving a heading over empty
    space — structurally intact but substantively blank, which no other check
    notices. A subsection is empty when it has no body line before the next
    heading (`###`/`####`), a horizontal rule (`---`), or EOF that carries
    non-whitespace prose: a blank line, a lone HTML comment, a `---` rule, or a
    stray table/separator artifact does not count as content. A subsection that
    still holds a `<!-- PENDING` marker is left to check 1 (no-leftover-skeleton-
    syntax) so the two never double-report the same site.

    Scope: the `####` subsections *inside* a finding — structure the model
    authors, so scaffold.py never marked it and it cannot be in the section
    manifest. The scaffolded headings across the whole suite (including this
    file's `## Tier N` and `## Threat Coverage Verification`) are check 15's,
    which reads that manifest; the two never look at the same site. This check
    keeps its own name because the phased drive routes its failures back through
    the findings phase by that exact string.
    """
    fails = []
    lines = suite.lines("findings")
    n = len(lines)
    base = suite.base("findings")
    cur_find = None
    i = 0
    while i < n:
        s = lines[i].strip()
        mf = FIND_HEADING_RE.match(s)
        if mf:
            cur_find = mf.group(1)
            i += 1
            continue
        ms = re.match(r"^####\s+(.+)$", s)
        if cur_find and ms:
            section = clean_cell(ms.group(1))
            head_line = i + 1
            has_content = False
            pending = False
            j = i + 1
            while j < n:
                t = lines[j].strip()
                if t.startswith("###") or t == "---":  # `####` starts with `###`
                    break
                if not t:
                    j += 1
                    continue
                if "<!-- PENDING" in t:
                    pending = True      # check 1 owns the unfilled-marker case
                    j += 1
                    continue
                if HTML_COMMENT_LINE_RE.match(t) or t.startswith("|"):
                    j += 1              # comment / table artifact is not prose
                    continue
                has_content = True
                j += 1
            if not pending and not has_content:
                fails.append(ev(base, head_line,
                                '%s "%s" section is empty' % (cur_find, section)))
            i = j
            continue
        i += 1
    return ("finding-bodies-nonempty", not fails, sorted(fails))


HEADING_RE = re.compile(r"^(#{1,6})\s+(.*\S)\s*$")
HRULE_RE = re.compile(r"^(?:-{3,}|\*{3,}|_{3,})$")
FENCE_RE = re.compile(r"^(?:```|~~~)")


def _heading_at(line):
    """(level, cleaned text) for a markdown heading line, else None."""
    m = HEADING_RE.match(line.strip())
    if not m:
        return None
    return len(m.group(1)), clean_cell(m.group(2))


def find_heading(lines, text, level):
    """1-based line of the first `#*level` heading whose text equals `text`."""
    want = clean_cell(text).lower()
    for i, line in enumerate(lines):
        h = _heading_at(line)
        if h and h[0] == level and h[1].lower() == want:
            return i + 1
    return None


def section_region(lines, head_line, level, to_eof):
    """0-based [start, end) body region of the section headed at `head_line`.

    Normally the region ends at the next heading of the same or higher rank —
    a deeper heading the model wrote (a `### FIND-01` under `## Tier 1`) is part
    of the body, not a boundary. `to_eof` (set by scaffold.py for a marker whose
    fill is a run of *sibling* sections, e.g. STRIDE's per-component `##`
    sections written below `## Summary`) runs to end-of-file instead.
    """
    start = head_line          # head_line is 1-based, so this is the next line
    if to_eof:
        return start, len(lines)
    for i in range(start, len(lines)):
        h = _heading_at(lines[i])
        if h and h[0] <= level:
            return start, i
    return start, len(lines)


def region_substance(lines, start, end):
    """(has_content, has_pending) for a body region.

    Content is anything a reader would call substance. Deliberately NOT content
    (same exclusions the P47.9 finding-body check established): a blank line, a
    lone HTML comment (scaffold.py's `<!-- guidance: … -->` survives the fill by
    design), a `---` rule, and a bare table header or separator. A table *data*
    row is content, as is any model-authored heading and anything inside a
    fenced code block (a Mermaid diagram is a filled section). A region still
    holding a `<!-- PENDING` marker reports has_pending so the caller can leave
    it to check 1 and never double-report the same site.
    """
    region = lines[start:end]
    data_rows = set()
    for t in parse_tables(region):
        for _, ln in t["rows"]:
            data_rows.add(ln)               # 1-based within the region
    has_content = False
    has_pending = False
    in_fence = False
    for idx, raw in enumerate(region, 1):
        s = raw.strip()
        if FENCE_RE.match(s):
            in_fence = not in_fence
            has_content = True              # a code/diagram fence is substance
            continue
        if not s:
            continue
        if "<!-- PENDING" in s:
            has_pending = True
            continue
        if in_fence:
            has_content = True
            continue
        if HTML_COMMENT_LINE_RE.match(s) or HRULE_RE.match(s):
            continue
        if s.startswith("|"):
            if idx in data_rows:
                has_content = True
            continue                        # bare header/separator is structure
        has_content = True                  # prose, list, or a model heading
    return has_content, has_pending


def check_section_bodies_nonempty(suite):
    """15. Every section scaffold.py left a PENDING marker in has substance.

    Check 1 only proves no `<!-- PENDING -->` marker survived; a weak model
    satisfies it by deleting the marker and writing nothing, leaving a heading
    over empty space. P47.9's check 12 caught that inside `3-findings.md`'s
    per-finding subsections — but an empty Deployment Classification, an empty
    Security Infrastructure Inventory, an empty PASTA stage or an empty
    Executive Summary all passed clean. This generalizes the same property to
    the whole suite by asserting against `.scaffold-manifest.json`, the record
    scaffold.py writes of every site it marked (P52.7): *every site that had a
    marker now has substance*, not merely *no marker remains*.

    Deliberately conservative, since a false failure costs a verify bounce and
    erodes trust in the whole suite of checks:
      * no manifest (a pre-P52.7 or resumed suite) -> pass, today's behavior;
      * a manifest heading the file no longer has -> skipped (structure is other
        checks' business, and a renamed heading is not a hollow one);
      * a section whose marker is still there -> skipped, check 1 owns it;
      * a `to_eof` section that shares its heading with a scaffolded table
        (STRIDE/LINDDUN's per-component sections, which the skeleton hangs off
        `## Summary`) only fires when that table is empty too — filled summary
        rows count as substance in the shared region. A summary whose rows have
        no component sections beneath them is already reported, loudly and by
        the right check, as a component-name, bijection and count failure.
    """
    manifest = suite.manifest()
    if manifest is None:
        return ("section-bodies-nonempty", True, [])

    fails = []
    for entry in manifest:
        base = entry["file"]
        logical = suite.logical(base)
        if logical is None:
            continue                        # not a file this run has
        lines = suite.lines(logical)
        if not lines:
            continue
        # The unfilled-marker case belongs to check 1, wherever in the file the
        # marker ended up — match on the key, not just the region, so a marker
        # the model moved is still not double-reported.
        marker = "<!-- PENDING: %s -->" % entry["key"]
        if any(marker in ln for ln in lines):
            continue
        head_line = find_heading(lines, entry["heading"], entry["level"])
        if head_line is None:
            continue
        start, end = section_region(lines, head_line, entry["level"],
                                    entry["to_eof"])
        has_content, has_pending = region_substance(lines, start, end)
        if has_pending or has_content:
            continue
        what = "table" if entry["kind"] == "table" else "section"
        fails.append(ev(base, head_line,
                        '%s "%s" %s is empty (marker `%s` removed, nothing '
                        'written in its place)'
                        % ("#" * entry["level"], entry["heading"], what,
                           entry["key"])))
    return ("section-bodies-nonempty", not fails, sorted(fails))


# --------------------------------------------------------------------------- #
# Substance-floor helpers (P52.8)
# --------------------------------------------------------------------------- #

def link_text(text):
    """Replace markdown links `[text](url)` with just their link text."""
    return MD_LINK_RE.sub(r"\1", text)


def norm_cell(cell):
    """Cleaned cell text, lowercased and stripped of decorative punctuation.

    The normal form a placeholder token is compared against: `**TBD**`,
    `` `TBD` ``, `— TBD —` and `TBD.` all reduce to `tbd`, while anything with
    a real word beside the token keeps it and therefore never matches.
    """
    t = link_text(clean_cell(cell)).lower()
    t = re.sub(r"\s+", " ", t).strip()
    return t.strip(" \t*_`~\"'()[]{}<>.,;:!—–-")


def is_none_identified_row(cells):
    """True when any cell of a row records an explicit "none identified" entry."""
    return any(NONE_IDENTIFIED_RE.search(clean_cell(c)) for c in cells)


def evidence_is_cited(cell):
    """True unless the cell is a bare filename with nothing else beside it.

    SKILL.md §3 requires every threat to name "the file, config, or code path
    that makes it real", and the skeletons' POST-TABLE-CHECK spells it out. A
    bare `internal/server/auth.go` names a file but pins nothing inside it; a
    line number, a symbol, a config key or any accompanying prose does.

    Biased hard toward passing — it returns True for anything but the single
    unambiguous shape:
      * empty  -> True (a hollow table is check 15's business, not this one);
      * carries a line number (`auth.go:88`, `#L88`, `lines 20-40`) -> True;
      * more than one word (`config key provider.small_model`, `auth.go —
        Authenticate() has no rate limit`) -> True;
      * a single token that is not path-shaped (`newEngine()`, `Dockerfile`,
        `provider.small_model`, `.env`) -> True.
    """
    text = link_text(clean_cell(cell)).strip()
    if not text:
        return True
    if LINE_NUMBER_RE.search(text):
        return True
    if len(text.split()) > 1:
        return True
    if not PATHY_TOKEN_RE.match(text):
        return True
    return not ("/" in text or "\\" in text or FILE_EXT_RE.search(text))


def substantive_rows(table):
    """Data rows of a table minus the ones other checks own.

    Skips a row still carrying skeleton syntax (check 1's) and a row that is a
    verbatim echo of its own header (check 14's), so a single authoring mistake
    is never reported by three checks at once.
    """
    head = [clean_cell(c) for c in table["header"]]
    out = []
    for cells, lineno in table["rows"]:
        if is_placeholder(cells):
            continue
        if [clean_cell(c) for c in cells] == head:
            continue
        out.append((cells, lineno))
    return out


def region_prose_chars(lines, start, end):
    """Substantive character count of a body region.

    Counts exactly what `region_substance` calls content — prose, list items,
    model-authored headings, table *data* rows and fenced-block bodies — and
    ignores what it calls structure: blank lines, lone HTML comments (the
    scaffold's guidance comments survive the fill by design), horizontal rules,
    bare table headers/separators and the fence markers themselves.
    """
    region = lines[start:end]
    data_rows = set()
    for t in parse_tables(region):
        for _, ln in t["rows"]:
            data_rows.add(ln)               # 1-based within the region
    total = 0
    in_fence = False
    for idx, raw in enumerate(region, 1):
        s = raw.strip()
        if FENCE_RE.match(s):
            in_fence = not in_fence
            continue
        if not s or "<!-- PENDING" in s:
            continue
        if in_fence:
            total += len(s)
            continue
        if HTML_COMMENT_LINE_RE.match(s) or HRULE_RE.match(s):
            continue
        if s.startswith("|"):
            if idx in data_rows:
                total += len(s)
            continue
        total += len(s)
    return total


def check_evidence_cells_cited(suite):
    """16. Every Evidence cell pins a location, not just a file.

    SKILL.md §3 requires each threat to cite "the file, config, or code path
    that makes it real", and every framework skeleton's POST-TABLE-CHECK repeats
    it — but nothing checked it, so a suite whose Evidence column is a column of
    bare filenames passed clean. This asserts the weakest form of the rule that
    is still worth asserting: a cell that is *only* a filename, with no line
    number, symbol, config key or accompanying prose, is not a citation.

    Under-flagging by construction (see `evidence_is_cited`): an empty cell, a
    multi-word cell, a symbol, a config key and anything carrying a line number
    all pass. Rows explicitly recording "none identified" are skipped — a
    complete entry for a category with no threat has nothing to cite.
    """
    fails = []
    columns = tuple(c.lower() for c in SUBSTANCE["evidence_columns"])
    for name in SUITE_FILES:
        base = suite.base(name)
        for t in suite.tables(name):
            for cname in columns:
                idx = col_index(t["header"], cname)
                if idx is None:
                    continue
                label = clean_cell(t["header"][idx])
                for cells, lineno in substantive_rows(t):
                    if idx >= len(cells) or is_none_identified_row(cells):
                        continue
                    if evidence_is_cited(cells[idx]):
                        continue
                    fails.append(ev(base, lineno,
                                    "%s cell %r is a bare filename — cite a line "
                                    "number, symbol, or config key (SKILL.md §3)"
                                    % (label, clean_cell(cells[idx]))))
    return ("evidence-cells-cited", not fails, sorted(fails))


def check_no_placeholder_cells(suite):
    """17. No required-substantive cell is a bare placeholder token.

    `TBD`, `TODO`, `N/A`, `see above`, `see code` in an Evidence, Mitigation,
    Threat, Residual-risk, Control, Attack-vector, Justification or
    Business-impact cell is an unwritten cell wearing a word. Check 1 only
    catches the *skeleton's* markers; a model that types `TBD` itself defeats it.

    Matching is exact against the cleaned, lowercased, punctuation-trimmed cell
    (`SUBSTANCE["placeholder_tokens"]`), never a substring: "TBD — owner to
    confirm" is a real note and passes, "**TBD**" does not. Columns are matched
    by header text, so a renamed column opts out rather than misfiring, and
    "none identified" rows are skipped as the legitimate entries they are.
    """
    fails = []
    wanted = set(c.lower() for c in SUBSTANCE["substantive_columns"])
    tokens = set(t.lower() for t in SUBSTANCE["placeholder_tokens"])
    for name in SUITE_FILES:
        base = suite.base(name)
        for t in suite.tables(name):
            for idx, raw_head in enumerate(t["header"]):
                label = clean_cell(raw_head)
                if label.lower() not in wanted:
                    continue
                for cells, lineno in substantive_rows(t):
                    if idx >= len(cells) or is_none_identified_row(cells):
                        continue
                    if norm_cell(cells[idx]) not in tokens:
                        continue
                    fails.append(ev(base, lineno,
                                    "%s cell %r is a placeholder, not content"
                                    % (label, clean_cell(cells[idx]))))
    return ("no-placeholder-cells", not fails, sorted(fails))


def check_none_identified_fraction(suite):
    """18. A table is not allowed to be nothing but "none identified".

    "none identified" is an explicitly valid, complete entry (SKILL.md §4) and
    the skeletons require one rather than a missing cell — so one or two per
    table is exactly right and must never fail. Twelve out of twelve means the
    analysis pass never happened.

    Two rules, same cap (`none_identified_max_fraction`, default 0.95 — i.e.
    below 1.0 nothing fires at all):
      * per table, once it has at least `none_identified_min_rows` rows, so an
        ordinary sparse 3-row component table is never judged;
      * per file, aggregated over that file's *threat-shaped* tables (the ones
        carrying a required-substantive column, so a Summary table of counts
        does not dilute the measure), once it has at least
        `none_identified_min_rows_file` rows. This catches the suite-wide
        vacuous pass that per-table sparsity would otherwise hide.
    The file rule is suppressed when a table in that file already failed, so one
    problem is reported once, at the tighter location.
    """
    fails = []
    cap = SUBSTANCE["none_identified_max_fraction"]
    min_rows = SUBSTANCE["none_identified_min_rows"]
    min_rows_file = SUBSTANCE["none_identified_min_rows_file"]
    wanted = set(c.lower() for c in SUBSTANCE["substantive_columns"])

    for name in SUITE_FILES:
        base = suite.base(name)
        file_total = 0
        file_none = 0
        table_failed = False
        for t in suite.tables(name):
            head = [clean_cell(c) for c in t["header"]]
            rows = substantive_rows(t)
            total = len(rows)
            none = sum(1 for cells, _ in rows if is_none_identified_row(cells))
            if total >= min_rows and none > cap * total:
                table_failed = True
                fails.append(ev(base, t["line"],
                                "table [%s]: %d of %d rows are \"none identified\" "
                                "(%.0f%% > %.0f%% cap) — the analysis pass never "
                                "ran for this table"
                                % (" | ".join(head), none, total,
                                   100.0 * none / total, 100.0 * cap)))
            if any(h.lower() in wanted for h in head):
                file_total += total
                file_none += none
        if (not table_failed and file_total >= min_rows_file
                and file_none > cap * file_total):
            fails.append(ev(base, 0,
                            "%d of %d threat-table rows in this file are \"none "
                            "identified\" (%.0f%% > %.0f%% cap) — the analysis "
                            "pass never ran"
                            % (file_none, file_total,
                               100.0 * file_none / file_total, 100.0 * cap)))
    return ("none-identified-fraction", not fails, sorted(fails))


def check_prose_sections_substantive(suite):
    """19. Every narrative section the manifest names carries at least a sentence.

    Check 15 proves a scaffolded section is not *empty*; it passes on an
    Executive Summary reading "TBD." or a System Purpose reading "See below."
    This adds the smallest possible length floor on top, over exactly the
    `kind: "prose"` entries of `scaffold.py`'s `.scaffold-manifest.json` (P52.7)
    — the narrative sections, never the tables.

    Same conservative posture as check 15, and the same skips so a site is never
    reported twice:
      * no manifest (a pre-P52.7 or resumed suite) -> pass;
      * a manifest heading the file no longer has -> skipped;
      * a section whose marker is still there -> skipped, check 1 owns it;
      * a section with no substance at all -> skipped, check 15 owns it;
      * a key in `prose_floor_exempt_keys` -> skipped (the deployment
        classification's complete, correct fill is one of four fixed words).
    """
    manifest = suite.manifest()
    if manifest is None:
        return ("prose-sections-substantive", True, [])

    fails = []
    floor = SUBSTANCE["min_prose_chars"]
    exempt = set(SUBSTANCE["prose_floor_exempt_keys"])
    for entry in manifest:
        if entry["kind"] != "prose" or entry["key"] in exempt:
            continue
        base = entry["file"]
        logical = suite.logical(base)
        if logical is None:
            continue
        lines = suite.lines(logical)
        if not lines:
            continue
        marker = "<!-- PENDING: %s -->" % entry["key"]
        if any(marker in ln for ln in lines):
            continue                        # check 1 owns the unfilled case
        head_line = find_heading(lines, entry["heading"], entry["level"])
        if head_line is None:
            continue
        start, end = section_region(lines, head_line, entry["level"],
                                    entry["to_eof"])
        has_content, has_pending = region_substance(lines, start, end)
        if has_pending or not has_content:
            continue                        # checks 1 and 15 own these
        chars = region_prose_chars(lines, start, end)
        if chars >= floor:
            continue
        fails.append(ev(base, head_line,
                        '%s "%s" section holds %d characters of content '
                        "(minimum %d) — write the narrative, not a stub"
                        % ("#" * entry["level"], entry["heading"], chars,
                           floor)))
    return ("prose-sections-substantive", not fails, sorted(fails))


def check_coverage_matches_related_threats(suite):
    """13. Every coverage-table mapping Tx -> FIND-n agrees with FIND-n's own
    "Related Threats" attribute row.

    The plain bijection check (check 4) only counts threat ids across the
    coverage table; it never reads the per-finding Related-Threats attribute, so
    a coverage row that files a threat under the WRONG finding still balances. In
    the fixture T8 (a Frontend Proxy threat) is mapped to FIND-01 (the Database
    finding, whose Related Threats are T22-T26) — a real contradiction this
    cross-reference catches. A pairing whose finding has no Related-Threats row,
    or a missing coverage/finding table, degrades to a skip rather than a crash
    (mirroring the None-guards in the checks above); check 4 already reports a
    wholly absent coverage table, so its absence is not double-reported here.
    """
    fails = []
    cov_tbl = find_table(suite.tables("findings"), "Threat ID", "Finding ID", "Status")
    if cov_tbl is None:
        return ("coverage-matches-related-threats", True, [])

    related = {f["id"]: f["related"]
               for f in parse_findings(suite.lines("findings"))
               if f["related"] is not None}

    ti = col_index(cov_tbl["header"], "Threat ID")
    fi = col_index(cov_tbl["header"], "Finding ID")
    if ti is None or fi is None:
        return ("coverage-matches-related-threats", True, [])
    for cells, ln in cov_tbl["rows"]:
        if is_placeholder(cells) or ti >= len(cells) or fi >= len(cells):
            continue
        tm = TID_RE.search(clean_cell(cells[ti]))
        fm = re.search(r"FIND-\d+", clean_cell(cells[fi]))
        if not tm or not fm:
            continue
        tid, fid = tm.group(0), fm.group(0)
        if fid not in related:          # no Related Threats row to corroborate
            continue
        if tid not in related[fid]:
            fails.append(ev(suite.base("findings"), ln,
                            "%s mapped to %s in coverage table but not in %s "
                            "Related Threats" % (tid, fid, fid)))
    return ("coverage-matches-related-threats", not fails, sorted(fails))


def check_no_duplicate_header_rows(suite):
    """14. No table carries a data row that is a verbatim copy of its own header.

    scaffold.py's table() writes each header + separator exactly ONCE, then an
    optional guidance HTML comment and a single `<!-- PENDING: key -->` marker
    (a full-line comment parse_tables skips) — so a freshly-scaffolded suite has
    no data rows and never trips this. The duplicate is the model's: filling a
    table it re-emitted the `| File | Description |` header + separator a second
    time below the guidance comment, so parse_tables reads that echoed header as
    a data row. Comparing every data row's cleaned cells to the cleaned header
    catches it across all five files; the intentional guidance comments are not
    table rows and are never flagged.
    """
    fails = []
    for name in ("assessment", "architecture", "model", "analysis", "findings"):
        base = suite.base(name)
        for t in suite.tables(name):
            head = [clean_cell(c) for c in t["header"]]
            for cells, ln in t["rows"]:
                if [clean_cell(c) for c in cells] == head:
                    fails.append(ev(base, ln,
                                    "data row duplicates the table header %r"
                                    % " | ".join(head)))
    return ("no-duplicate-header-rows", not fails, sorted(fails))


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
    check_coverage_ledger,
    check_finding_bodies_nonempty,
    check_coverage_matches_related_threats,
    check_no_duplicate_header_rows,
    # P52.7. Appended rather than folded into check 12 so the existing check
    # names stay stable: the phased drive routes a `finding-bodies-nonempty`
    # failure back through the findings phase by name
    # (internal/cli/chat_phased.go's contentSubstanceChecks), and renaming it
    # would silently drop that routing.
    check_section_bodies_nonempty,
    # P52.8 — the mechanical substance floor. Appended, again, so no existing
    # check name moves. These are the only checks that judge *content* rather
    # than structure, and they are deliberately weak: see SUBSTANCE.
    check_evidence_cells_cited,
    check_no_placeholder_cells,
    check_none_identified_fraction,
    check_prose_sections_substantive,
]


def _apply_substance_overrides(args):
    """Fold the `--*` substance-floor flags into the module-level SUBSTANCE dict.

    Overriding the module dict (rather than threading a config object through
    every check) keeps the `check(suite) -> (name, ok, evidence)` signature that
    all nineteen checks share. A flag left unset changes nothing; a
    comma-separated list flag *replaces* the default tuple, and an empty value
    disables that rule entirely.
    """
    def csv(value):
        return tuple(p.strip().lower() for p in value.split(",") if p.strip())

    if args.min_prose_chars is not None:
        SUBSTANCE["min_prose_chars"] = args.min_prose_chars
    if args.none_identified_max_fraction is not None:
        SUBSTANCE["none_identified_max_fraction"] = args.none_identified_max_fraction
    if args.none_identified_min_rows is not None:
        SUBSTANCE["none_identified_min_rows"] = args.none_identified_min_rows
    if args.none_identified_min_rows_file is not None:
        SUBSTANCE["none_identified_min_rows_file"] = args.none_identified_min_rows_file
    if args.evidence_columns is not None:
        SUBSTANCE["evidence_columns"] = csv(args.evidence_columns)
    if args.substantive_columns is not None:
        SUBSTANCE["substantive_columns"] = csv(args.substantive_columns)
    if args.placeholder_tokens is not None:
        SUBSTANCE["placeholder_tokens"] = csv(args.placeholder_tokens)
    if args.prose_floor_exempt_keys is not None:
        SUBSTANCE["prose_floor_exempt_keys"] = csv(args.prose_floor_exempt_keys)


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

    sub = ap.add_argument_group(
        "substance floor (P52.8)",
        "Thresholds for checks 16-19. Defaults are deliberately permissive — "
        "a false failure costs a verify bounce and erodes trust in every other "
        "check. Lower them for a stricter run.")
    sub.add_argument("--min-prose-chars", type=int, default=None, metavar="N",
                     help="minimum substantive characters in a manifest prose "
                          "section (default %d)" % SUBSTANCE["min_prose_chars"])
    sub.add_argument("--none-identified-max-fraction", type=float, default=None,
                     metavar="F",
                     help="max fraction of a table's rows that may read \"none "
                          "identified\" (default %.2f)"
                          % SUBSTANCE["none_identified_max_fraction"])
    sub.add_argument("--none-identified-min-rows", type=int, default=None,
                     metavar="N",
                     help="smallest table the fraction cap applies to "
                          "(default %d)" % SUBSTANCE["none_identified_min_rows"])
    sub.add_argument("--none-identified-min-rows-file", type=int, default=None,
                     metavar="N",
                     help="smallest per-file row total the fraction cap applies "
                          "to (default %d)"
                          % SUBSTANCE["none_identified_min_rows_file"])
    sub.add_argument("--evidence-columns", default=None, metavar="A,B",
                     help="columns whose cells must carry a citation "
                          "(default %s)" % ",".join(SUBSTANCE["evidence_columns"]))
    sub.add_argument("--substantive-columns", default=None, metavar="A,B",
                     help="columns in which a bare placeholder token is a "
                          "failure (default: %s)"
                          % ",".join(SUBSTANCE["substantive_columns"]))
    sub.add_argument("--placeholder-tokens", default=None, metavar="A,B",
                     help="cell texts treated as placeholders "
                          "(default: %s)" % ",".join(SUBSTANCE["placeholder_tokens"]))
    sub.add_argument("--prose-floor-exempt-keys", default=None, metavar="A,B",
                     help="manifest keys exempt from the prose length floor "
                          "(default: %s)"
                          % ",".join(SUBSTANCE["prose_floor_exempt_keys"]))

    args = ap.parse_args(argv)
    _apply_substance_overrides(args)

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
