#!/usr/bin/env python3
"""Threat-model inventory builder — deterministic `inventory.yaml` from the docs.

Companion script for the `threat-modeling` skill's final phase. It builds (and
validates) the `inventory.yaml` sidecar for a completed run *directly from the
run's own markdown files*, never from the model's memory. The sibling
`threat-model-analyst` skill records two recurring quality failures when the
model hand-writes this sidecar: (1) truncated arrays (a threat or flow silently
dropped) and (2) field-name drift (`name` for `id`, `type` for `category`).
Both vanish when a script parses the tables that already exist and emits the
sidecar mechanically.

The sidecar exists for *matching* — a future run diffing itself against this
one, or an incremental update locating its baseline (skeleton-inventory.md) —
so every entry is one line and every list is sorted by `id`.

Contract:
  * Facts from the docs only. Field names are taken verbatim from
    `references/skeletons/skeleton-inventory.md`; `tier` is DERIVED from
    `prerequisite` (never trusted from a table) per the mapping in
    `references/output-formats.md`.
  * Python 3.7+, standard library only — no third-party imports (no pyyaml).
    The schema is flat lists of scalar maps, so a hand-rolled emitter/reader
    is sufficient and keeps the dependency posture of the skill's other
    bundled scripts (recon.py).
  * Deterministic output: every list is sorted by a natural-order id key; two
    runs over the same docs produce byte-identical YAML (git commit aside).

Files parsed (all inside <run-dir>, formats defined by the cited reference):
  * `0.1-architecture.md`  — Key Components table + Deployment Classification
                             (references/output-formats.md)
  * `1-model.md`           — Data Flow Table (references/output-formats.md)
  * `2-*-analysis.md`      — per-component threat tables; framework taken from
                             the filename (references/skeletons/skeleton-*.md)
  * `3-findings.md`        — FIND-## attribute tables (CWE/OWASP) + the Threat
                             Coverage Verification table
                             (references/output-formats.md)

Usage:
  python inventory.py <run-dir>            build + write <run-dir>/inventory.yaml
  python inventory.py <run-dir> --check    validate an existing inventory.yaml
                                           against the docs; PASS/FAIL per check,
                                           non-zero exit on any FAIL
"""

import argparse
import os
import re
import subprocess
import sys
import glob as _glob

# --------------------------------------------------------------------------- #
# Fixed vocabularies (skeleton-inventory.md / output-formats.md / skeleton-*)
# --------------------------------------------------------------------------- #

# The four deployment classifications, and the five prerequisite values whose
# mapping to a tier is fixed (output-formats.md's Prerequisite→Tier table).
FIXED_DEPLOY = ["internet-facing", "internal-network", "localhost-service", "local-desktop"]

# prerequisite (normalized to lowercase-hyphenated) -> tier. `none` and any
# value not in the tier-2/tier-3 sets fall through to tier 1.
TIER2_PREREQS = {"authenticated-user", "internal-network"}
TIER3_PREREQS = {"local-process", "host-compromise"}

# The sidecar's fixed component-type axis (skeleton-inventory.md:
# `type: process / external_interactor / data_store`).
CANONICAL_TYPES = ("process", "external_interactor", "data_store")

# Synonyms -> that axis. The architecture table asks for
# `Process / External Interactor / Data Store` (output-formats.md), but every
# *other* document in this skill teaches the standard DFD vocabulary, so a
# model writing the table from what it just read writes the textbook word:
#   * `external entity`   — stride.md ("*External entity*: Spoofing,
#                           Repudiation"; "external entities, processes, data
#                           stores, data flows"), diagram-conventions.md
#                           ("External entity (a user, an external service)"),
#                           linddun.md ("processes, data stores ... entities")
#   * `external` / `datastore` — the fixed `classDef` names every diagram in
#                           this skill applies (diagram-conventions.md:
#                           "class ComponentId process (or external/datastore)")
#   * `actor`             — trike.md's Actors ("every distinct class of
#                           user/system/service that interacts with the
#                           system") and pasta.md's actors: the same concept
#   * `service` / `handler` — diagram-conventions.md: "Process (a service, an
#                           agent, a handler)"
#   * `database` / `file store` / `cache` — diagram-conventions.md: "Data store
#                           (a database, a file store, a cache)"
#   * `external service` / `user` — diagram-conventions.md's two examples of an
#                           external entity
# Plus the plain plural/spacing variants of each. That is a *vocabulary*
# difference, not a disagreement about the model, and it cost a real run a
# full model round-trip at ~80k context to fix by hand.
#
# Deliberately NOT mapped, because their meaning is genuinely distinct:
#   * `data flow` / `flow` and `trust boundary` — separate DFD element kinds
#     (stride.md lists all five); a flow that landed in the component table is
#     a real modeling error and must still fail.
#   * `agent` — ambiguous: diagram-conventions.md files it under Process, but
#     an agent this system merely talks to is an external interactor. An
#     ambiguous word should fail loudly rather than be silently canonicalized.
TYPE_SYNONYMS = {
    # -> external_interactor
    "external_entity": "external_interactor",
    "external_entities": "external_interactor",
    "entity": "external_interactor",
    "entities": "external_interactor",
    "external": "external_interactor",
    "external_actor": "external_interactor",
    "external_actors": "external_interactor",
    "actor": "external_interactor",
    "actors": "external_interactor",
    "interactor": "external_interactor",
    "interactors": "external_interactor",
    "external_interactors": "external_interactor",
    "external_service": "external_interactor",
    "external_services": "external_interactor",
    "user": "external_interactor",
    "users": "external_interactor",
    # -> process
    "processes": "process",
    "service": "process",
    "services": "process",
    "handler": "process",
    "handlers": "process",
    # -> data_store
    "data_stores": "data_store",
    "datastore": "data_store",
    "datastores": "data_store",
    "store": "data_store",
    "stores": "data_store",
    "database": "data_store",
    "databases": "data_store",
    "file_store": "data_store",
    "file_stores": "data_store",
    "cache": "data_store",
    "caches": "data_store",
}


# --------------------------------------------------------------------------- #
# Cell / id / value normalization
# --------------------------------------------------------------------------- #

def clean_cell(s):
    """Strip a markdown table cell to its plain text: unwrap `[text](url)`
    links to their text, drop backticks and bold markers, collapse edges."""
    s = s.strip()
    s = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", s)  # [text](url) -> text
    s = s.replace("`", "").replace("*", "")
    return s.strip()


def extract_id(cell):
    """Pull the literal id token out of an ID cell. IDs appear bare (`T1`,
    `T01`, `L3`) and composite (`T04.S`, from the coverage table). We never
    decompose — whatever literal token the table carries is the canonical id
    (a leading letter, word chars, optional single dotted suffix)."""
    cell = clean_cell(cell)
    m = re.search(r"[A-Za-z][\w]*(?:\.\w+)?", cell)
    return m.group(0) if m else ""


def norm_type(s):
    """Component Type -> the sidecar's fixed axis: process / external_interactor
    / data_store (architecture writes them Title-Cased with a space).

    Mechanical case/separator folding first, then the DFD-synonym table above
    (`External Entity` -> `external_interactor`), so a document written in the
    standard DFD vocabulary agrees with a sidecar written in the skeleton's.
    Anything unrecognized is returned folded but unchanged — an unknown type is
    still a real mismatch and must still fail."""
    t = clean_cell(s).lower().replace(" ", "_").replace("-", "_")
    t = re.sub(r"_+", "_", t).strip("_")
    return TYPE_SYNONYMS.get(t, t)


# A trailing parenthetical annotation on an Anchor cell: `foo.jsx (active
# entrypoint per README.md)`. The leading `\s+` is load-bearing — it keeps a
# method/callable anchor (`NewProxy()`, `Router.handle(req)`) intact, since
# those parentheses are part of the artifact's own name, not a note about it.
ANCHOR_NOTE_RE = re.compile(r"\s+\([^()]*\)\s*$")


def norm_anchor(s):
    """Component Anchor -> the bare artifact path/class it names.

    The Anchor is an identity ("the real file/class/manifest this component is
    named after", skeleton-inventory.md) and is what a future run matches on, so
    a trailing editorial note — `... (active entrypoint per README.md)`,
    `... (lines 40-120)` — is annotation, not identity. Stripping it here, at
    the one place the anchor is extracted, keeps the emitted sidecar canonical
    and keeps the --check comparison from failing on prose. A *different* path
    is untouched and still fails."""
    a = clean_cell(s)
    while True:
        m = ANCHOR_NOTE_RE.search(a)
        if not m:
            return a
        head = a[:m.start()].rstrip()
        if not head:
            return a      # the cell is nothing but the parenthetical — keep it
        a = head


def norm_prereq(s):
    """Prerequisite -> lowercase-hyphenated (`Authenticated User` ->
    `authenticated-user`, `None` -> `none`)."""
    return clean_cell(s).lower().replace(" ", "-")


def tier_for(prereq):
    """Tier DERIVED from prerequisite — never read from a table. Same mapping
    output-formats.md and every framework skeleton use."""
    if prereq in TIER2_PREREQS:
        return 2
    if prereq in TIER3_PREREQS:
        return 3
    return 1  # none, or anything unrecognized, floors at Tier 1


def map_status(s):
    """Coverage-table Status text -> the sidecar's three-value `status`.
    `Covered` is an open gap; `Mitigated (FIND-XX)` is handled by this repo's
    own control; `Mitigated by platform` is external (no finding)."""
    sl = clean_cell(s).lower()
    if sl.startswith("mitigated by platform"):
        return "mitigated-by-platform"
    if sl.startswith("mitigated"):
        return "mitigated"
    if sl.startswith("covered"):
        return "open"
    return "open"


def natkey(s):
    """Natural sort key so T2 sorts before T10 and DF2 before DF10 — a stable,
    human-sensible order for the sorted-by-id arrays."""
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r"(\d+)", str(s))]


# --------------------------------------------------------------------------- #
# Markdown table scanning
# --------------------------------------------------------------------------- #

def split_row(line):
    """Split a `| a | b | c |` row into stripped cells. A `|` inside a
    backtick-quoted code span (e.g. an Evidence cell citing `` `a || b` ``)
    or escaped as `\\|` is literal cell content, not a delimiter — content
    phases routinely cite JS/shell snippets with `||` in this column."""
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


def is_table_row(line):
    return line.strip().startswith("|")


def is_separator(line):
    if not is_table_row(line):
        return False
    cells = split_row(line)
    return bool(cells) and all(re.fullmatch(r":?-{2,}:?", c or "") for c in cells)


def find_table_rows(text, required):
    """Return the rows of the FIRST table whose (lowercased) headers contain
    all `required` header names, as a list of {header_lower: cell} dicts.
    Rows that are pure skeleton placeholders (`[FILL...`, `[REPEAT...`) are
    skipped. `required` is a set of lowercase header names."""
    lines = text.splitlines()
    n = len(lines)
    i = 0
    while i < n:
        if is_table_row(lines[i]) and i + 1 < n and is_separator(lines[i + 1]):
            headers = [h.lower() for h in split_row(lines[i])]
            if required.issubset(set(headers)):
                rows = []
                j = i + 2
                while j < n and is_table_row(lines[j]):
                    if not is_separator(lines[j]):
                        cells = split_row(lines[j])
                        row = {}
                        for k, h in enumerate(headers):
                            row[h] = cells[k] if k < len(cells) else ""
                        if not _is_placeholder_row(row):
                            rows.append(row)
                    j += 1
                return rows
            i += 1
        else:
            i += 1
    return []


def _is_placeholder_row(row):
    joined = " ".join(row.values())
    return bool(re.search(r"\[(FILL|REPEAT|END-REPEAT)", joined))


def section_body(text, name):
    """The text between a `## <name>` heading and the next `## ` heading."""
    out = []
    grab = False
    for line in text.splitlines():
        if line.startswith("## "):
            grab = clean_cell(line[3:]).lower() == name.lower()
            continue
        if grab:
            out.append(line)
    return "\n".join(out)


# --------------------------------------------------------------------------- #
# Per-document parsers
# --------------------------------------------------------------------------- #

def read_file(run_dir, name):
    path = os.path.join(run_dir, name)
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return fh.read()
    except (OSError, IOError):
        return ""


def find_analysis_file(run_dir):
    """Locate `2-<framework>-analysis.md` generically (any framework) and
    return (path, framework_short_name) or (None, None)."""
    hits = sorted(_glob.glob(os.path.join(run_dir, "2-*-analysis.md")))
    if not hits:
        return None, None
    base = os.path.basename(hits[0])
    m = re.match(r"2-(.+)-analysis\.md$", base)
    return hits[0], (m.group(1) if m else "")


def parse_components(arch_text):
    """Key Components table (Component|Type|Anchor|Description) -> component
    entries. The Component cell is the id (matches the diagram and analysis
    `## <Component>` headings); Type is normalized to the fixed axis and Anchor
    to the bare artifact it names (see norm_type / norm_anchor)."""
    comps = []
    for r in find_table_rows(arch_text, {"component", "type", "anchor"}):
        cid = clean_cell(r.get("component", ""))
        if not cid:
            continue
        comps.append({
            "id": cid,
            "anchor": norm_anchor(r.get("anchor", "")),
            "type": norm_type(r.get("type", "")),
        })
    comps.sort(key=lambda c: natkey(c["id"]))
    return comps


def parse_flows(model_text):
    """Data Flow Table (ID|Source|Target|Protocol|Description) -> flow entries."""
    flows = []
    for r in find_table_rows(model_text, {"id", "source", "target"}):
        fid = clean_cell(r.get("id", ""))
        if not fid:
            continue
        flows.append({
            "id": fid,
            "from": clean_cell(r.get("source", "")),
            "to": clean_cell(r.get("target", "")),
        })
    flows.sort(key=lambda f: natkey(f["id"]))
    return flows


def _has_threat_cols(headers):
    """A per-component threat table, generically across frameworks: it always
    carries an ID column plus Category, Prerequisite, and Severity (the fixed
    columns of skeleton-<framework>.md's threat table)."""
    return {"id", "category", "prerequisite", "severity"}.issubset(set(headers))


def parse_threats_raw(analysis_text):
    """Walk the analysis file tracking the nearest preceding `## <Component>`
    heading, and for every row of a threat table emit
    {id, component, category, tier, prerequisite, severity}. `tier` is derived
    from `prerequisite`, not read. Returns entries plus the ordered set of ids
    encountered (for the completeness check)."""
    lines = analysis_text.splitlines()
    n = len(lines)
    comp = None
    threats = []
    i = 0
    while i < n:
        line = lines[i]
        # Level-2 heading exactly ("## " but not "### ").
        if line.startswith("## "):
            comp = clean_cell(line[3:])
            i += 1
            continue
        if is_table_row(line) and i + 1 < n and is_separator(lines[i + 1]):
            headers = [h.lower() for h in split_row(line)]
            if _has_threat_cols(headers):
                j = i + 2
                while j < n and is_table_row(lines[j]):
                    if not is_separator(lines[j]):
                        cells = split_row(lines[j])
                        row = {}
                        for k, h in enumerate(headers):
                            row[h] = cells[k] if k < len(cells) else ""
                        if not _is_placeholder_row(row):
                            tid = extract_id(row.get("id", ""))
                            if tid:
                                prereq = norm_prereq(row.get("prerequisite", ""))
                                threats.append({
                                    "id": tid,
                                    "component": comp or "",
                                    "category": clean_cell(row.get("category", "")).lower(),
                                    "tier": tier_for(prereq),
                                    "prerequisite": prereq,
                                    "severity": clean_cell(row.get("severity", "")).lower(),
                                })
                    j += 1
                i = j
                continue
        i += 1
    return threats


def parse_coverage(find_text):
    """Threat Coverage Verification table (Threat ID|Finding ID|Status) ->
    {threat_id: (status, finding_id_or_None)}."""
    cov = {}
    for r in find_table_rows(find_text, {"threat id", "finding id", "status"}):
        tid = extract_id(r.get("threat id", ""))
        if not tid:
            continue
        fm = re.search(r"FIND-\d+", clean_cell(r.get("finding id", "")))
        cov[tid] = (map_status(r.get("status", "")), fm.group(0) if fm else None)
    return cov


def parse_finding_attrs(find_text):
    """Per-finding attribute tables -> {FIND-##: {cwe, owasp}}. CWE/OWASP flow
    onto the threat that maps to the finding. `N/A — ...` values yield None."""
    attrs = {}
    cur = None
    for line in find_text.splitlines():
        m = re.match(r"###\s+(FIND-\d+)", line)
        if m:
            cur = m.group(1)
            attrs.setdefault(cur, {"cwe": None, "owasp": None})
            continue
        if cur and is_table_row(line) and not is_separator(line):
            cells = split_row(line)
            if len(cells) < 2:
                continue
            key = cells[0].lower().strip()
            val = cells[1]
            na = val.strip().lower().startswith("n/a")
            if key == "cwe" and not na:
                mm = re.search(r"CWE-\d+", val)
                if mm:
                    attrs[cur]["cwe"] = mm.group(0)
            elif key.startswith("owasp") and not na:
                mm = re.search(r"[A-Z]\d+:\d{4}", val)  # A07:2025, P2:2021
                if mm:
                    attrs[cur]["owasp"] = mm.group(0)
    return attrs


def finding_ids(find_text):
    return set(re.findall(r"^###\s+(FIND-\d+)", find_text, re.M))


# --------------------------------------------------------------------------- #
# Metadata
# --------------------------------------------------------------------------- #

def parse_dir_slug(directory, framework):
    """A run directory is `<framework>-<target>-<YYYY-MM-DD-HHMM>`. Strip the
    known framework prefix and the trailing timestamp to recover target/date."""
    target, date = "", ""
    name = directory
    if framework and name.startswith(framework + "-"):
        name = name[len(framework) + 1:]
    m = re.search(r"-(\d{4}-\d{2}-\d{2})-\d{4}$", name)
    if m:
        date = m.group(1)
        target = name[: m.start()]
    else:
        # No trailing timestamp — best effort: whole remainder is the target.
        target = name
    return target, date


def git_commit(run_dir):
    """Short SHA via `git -C <run-dir> rev-parse --short HEAD`, best-effort.
    Returns None (key omitted) when not in a git repo."""
    try:
        out = subprocess.run(
            ["git", "-C", run_dir, "rev-parse", "--short", "HEAD"],
            capture_output=True, text=True, timeout=10,
        )
        if out.returncode == 0:
            c = out.stdout.strip()
            return c or None
    except (OSError, subprocess.SubprocessError):
        pass
    return None


def _strip_html_comments(text):
    """Drop <!-- ... --> blocks so a leftover template comment (whose 'One of:
    internet-facing / ...' line names every class) can't seed a false match."""
    return re.sub(r"<!--.*?-->", "", text, flags=re.DOTALL)


def _labeled_deployment(text):
    """An explicit 'Deployment classification: <value>' assertion line — the
    binding single-value form the skeletons put in the analysis header. Wins
    over any prose scan wherever it appears."""
    m = re.search(r"[Dd]eployment classification:\s*`?([a-z][a-z\-]+)`?",
                  _strip_html_comments(text))
    if m and m.group(1) in FIXED_DEPLOY:
        return m.group(1)
    return None


def _first_deployment_in_order(text):
    """The fixed deployment class appearing EARLIEST by document position — not
    by FIXED_DEPLOY list order. The section states its binding classification
    first; evidence prose that names another class (e.g. an overridden recon
    suggestion, which SKILL.md §2 requires documenting) comes after and so must
    not outrank it. List-order matching was the D2 bug: a rejected
    'internet-facing' mentioned anywhere beat the asserted 'internal-network'
    purely because it sorts first."""
    body = _strip_html_comments(text)
    best_pos, best_val = None, None
    for v in FIXED_DEPLOY:
        m = re.search(r"\b" + re.escape(v) + r"\b", body)
        if m and (best_pos is None or m.start() < best_pos):
            best_pos, best_val = m.start(), v
    return best_val


def parse_deployment(arch_text, analysis_text):
    """Deployment classification. Precedence: (1) an explicit 'Deployment
    classification:' label line (analysis header first, then architecture) —
    the unambiguous binding assertion; (2) the architecture file's Deployment
    Classification section, taking the first class token in DOCUMENT order;
    (3) the analysis file, same document-order scan. Never list-order (see
    _first_deployment_in_order)."""
    for t in (analysis_text, arch_text):
        v = _labeled_deployment(t)
        if v:
            return v
    v = _first_deployment_in_order(section_body(arch_text, "Deployment Classification"))
    if v:
        return v
    return _first_deployment_in_order(analysis_text)


# --------------------------------------------------------------------------- #
# Build the full inventory from the docs
# --------------------------------------------------------------------------- #

def build_inventory(run_dir):
    """Parse the suite and assemble the inventory dict. Also returns the raw
    supporting sets the --check evidence needs (finding ids, whether the
    analysis file was found)."""
    arch_text = read_file(run_dir, "0.1-architecture.md")
    model_text = read_file(run_dir, "1-model.md")
    find_text = read_file(run_dir, "3-findings.md")
    analysis_path, framework = find_analysis_file(run_dir)
    analysis_text = ""
    if analysis_path:
        try:
            with open(analysis_path, "r", encoding="utf-8") as fh:
                analysis_text = fh.read()
        except (OSError, IOError):
            analysis_text = ""

    directory = os.path.basename(os.path.abspath(run_dir))
    target, date = parse_dir_slug(directory, framework)

    metadata = {
        "framework": framework or "",
        "target": target,
        "date": date,
        "commit": git_commit(run_dir),  # may be None -> key omitted
        "directory": directory,
        "deployment_classification": parse_deployment(arch_text, analysis_text) or "",
    }

    components = parse_components(arch_text)
    flows = parse_flows(model_text)

    cov = parse_coverage(find_text)
    fattrs = parse_finding_attrs(find_text)
    threats = []
    for t in parse_threats_raw(analysis_text):
        status, fid = cov.get(t["id"], ("open", None))
        entry = {
            "id": t["id"],
            "component": t["component"],
            "category": t["category"],
            "tier": t["tier"],
            "prerequisite": t["prerequisite"],
            "severity": t["severity"],
            "status": status,
        }
        if fid and status != "mitigated-by-platform":
            entry["finding"] = fid
            fa = fattrs.get(fid, {})
            if fa.get("cwe"):
                entry["cwe"] = fa["cwe"]
            if fa.get("owasp"):
                entry["owasp"] = fa["owasp"]
        threats.append(entry)
    threats.sort(key=lambda e: natkey(e["id"]))

    inv = {
        "metadata": metadata,
        "components": components,
        "flows": flows,
        "threats": threats,
    }
    support = {
        "finding_ids": finding_ids(find_text),
        "analysis_found": bool(analysis_path),
        "analysis_file": os.path.basename(analysis_path) if analysis_path else "",
        "coverage": cov,
    }
    return inv, support


# --------------------------------------------------------------------------- #
# YAML emission (by hand — flat lists of scalar maps, one line per entry)
# --------------------------------------------------------------------------- #

# Field order per skeleton-inventory.md — extra keys never appear, so this is
# the total order for each list's one-line flow map.
COMPONENT_ORDER = ["id", "anchor", "type"]
FLOW_ORDER = ["id", "from", "to"]
THREAT_ORDER = ["id", "component", "category", "tier", "prerequisite",
                "severity", "cwe", "owasp", "status", "finding"]
METADATA_ORDER = ["framework", "target", "date", "commit", "directory",
                  "deployment_classification"]


def _yaml_escape(s):
    return s.replace("\\", "\\\\").replace('"', '\\"')


def _flow_map(d, order):
    """Render one entry as a single-line YAML flow mapping: `{k: v, ...}`.
    String values are double-quoted (so commas/colons never break parsing);
    ints (tier) are bare. Keys whose value is None/absent are omitted."""
    parts = []
    for k in order:
        if k not in d or d[k] is None:
            continue
        v = d[k]
        if isinstance(v, int) and not isinstance(v, bool):
            parts.append("%s: %d" % (k, v))
        else:
            parts.append('%s: "%s"' % (k, _yaml_escape(str(v))))
    return "{" + ", ".join(parts) + "}"


def emit_yaml(inv):
    L = []
    L.append("metadata:")
    md = inv["metadata"]
    for k in METADATA_ORDER:
        v = md.get(k)
        if v is None or v == "":
            # commit is legitimately absent outside a git repo; other empties
            # still emit as "" so field presence is stable for diffs — but
            # commit omits its key entirely per skeleton-inventory.md.
            if k == "commit":
                continue
        L.append('  %s: %s' % (k, md.get(k, "")))
    L.append("")
    L.append("components:")
    for c in inv["components"]:
        L.append("  - " + _flow_map(c, COMPONENT_ORDER))
    L.append("")
    L.append("flows:")
    for f in inv["flows"]:
        L.append("  - " + _flow_map(f, FLOW_ORDER))
    L.append("")
    L.append("threats:")
    for t in inv["threats"]:
        L.append("  - " + _flow_map(t, THREAT_ORDER))
    return "\n".join(L) + "\n"


# --------------------------------------------------------------------------- #
# YAML reading (minimal, for the on-disk one-line-entry shape we emit)
# --------------------------------------------------------------------------- #

def _split_top_commas(s):
    parts, buf, inq, i = [], [], False, 0
    while i < len(s):
        c = s[i]
        if c == '"':
            inq = not inq
            buf.append(c)
        elif c == "\\" and inq and i + 1 < len(s):
            buf.append(c)
            buf.append(s[i + 1])
            i += 2
            continue
        elif c == "," and not inq:
            parts.append("".join(buf))
            buf = []
        else:
            buf.append(c)
        i += 1
    if buf:
        parts.append("".join(buf))
    return parts


def _unescape(s):
    out, i = [], 0
    while i < len(s):
        if s[i] == "\\" and i + 1 < len(s):
            out.append(s[i + 1])
            i += 2
        else:
            out.append(s[i])
            i += 1
    return "".join(out)


def _parse_scalar(v):
    v = v.strip()
    if v.startswith('"'):
        return _unescape(v[1:].rsplit('"', 1)[0])
    if re.fullmatch(r"-?\d+", v):
        return int(v)
    return v


def _parse_flow_map(s):
    s = s.strip()
    if s.startswith("{"):
        s = s[1:]
    if s.endswith("}"):
        s = s[:-1]
    d = {}
    for kv in _split_top_commas(s):
        if ":" not in kv:
            continue
        key, _, val = kv.partition(":")  # first colon = separator (keys have none)
        key = key.strip()
        if key:
            d[key] = _parse_scalar(val)
    return d


def read_inventory(path):
    """Read the one-line-entry inventory.yaml we emit into the same dict shape
    build_inventory produces (metadata map + components/flows/threats lists)."""
    inv = {"metadata": {}, "components": [], "flows": [], "threats": [], "baseline": {}}
    section = None
    with open(path, "r", encoding="utf-8") as fh:
        for raw in fh:
            line = raw.rstrip("\n")
            if not line.strip():
                continue
            if re.match(r"^\S", line):  # top-level key
                m = re.match(r"^([A-Za-z_][\w]*):", line)
                section = m.group(1) if m else None
                continue
            stripped = line.strip()
            if stripped.startswith("- "):
                if isinstance(inv.get(section), list):
                    inv[section].append(_parse_flow_map(stripped[2:]))
            elif ":" in stripped and section in ("metadata", "baseline"):
                k, _, v = stripped.partition(":")
                inv[section][k.strip()] = v.strip()
    return inv


# --------------------------------------------------------------------------- #
# --check: named checks with file evidence
# --------------------------------------------------------------------------- #

def canon_component(entry):
    """One component entry, both fields put through the same canonicalizer the
    docs side already uses. The generated side comes from `parse_components`
    and is canonical by construction; the on-disk side is whatever the sidecar
    literally says, and a hand-written sidecar spells the axis in the DFD
    vocabulary (`External Entity`) or copies the Anchor cell's trailing note
    just as readily as the architecture document does. Comparing both sides on
    the canonical form makes the check test *agreement*, not spelling — a
    genuinely different type or a different anchor path is untouched."""
    out = dict(entry)
    if "anchor" in out:
        out["anchor"] = norm_anchor(str(out["anchor"]))
    if "type" in out:
        out["type"] = norm_type(str(out["type"]))
    return out


def _diff_lists(disk, gen):
    """Field-level diff of two id-keyed entry lists, in natural id order."""
    dm = {d.get("id"): d for d in disk}
    gm = {g.get("id"): g for g in gen}
    problems = []
    for i in sorted(set(dm) | set(gm), key=natkey):
        if i not in dm:
            problems.append("  present in docs, missing from inventory: %s" % i)
            continue
        if i not in gm:
            problems.append("  present in inventory, not in docs: %s" % i)
            continue
        a, b = dm[i], gm[i]
        for k in sorted(set(a) | set(b)):
            if a.get(k) != b.get(k):
                problems.append("  %s.%s: inventory=%r docs=%r" % (i, k, a.get(k), b.get(k)))
    return problems


def run_checks(run_dir):
    """Return (results, ok). results is a list of (name, passed, evidence[])."""
    results = []
    inv_path = os.path.join(run_dir, "inventory.yaml")

    if not os.path.isfile(inv_path):
        results.append(("inventory.yaml exists", False, ["  %s not found" % inv_path]))
        return results, False

    try:
        disk = read_inventory(inv_path)
    except (OSError, IOError, ValueError) as e:
        results.append(("inventory.yaml parses", False, ["  %s" % e]))
        return results, False
    results.append(("inventory.yaml parses", True, []))

    gen, support = build_inventory(run_dir)
    directory = gen["metadata"]["directory"]

    # 1. metadata.directory matches the run dir basename.
    got = disk["metadata"].get("directory", "")
    results.append((
        "metadata.directory matches run dir",
        got == directory,
        [] if got == directory else ["  inventory=%r expected=%r" % (got, directory)],
    ))

    # 2. metadata agrees with the documents (commit compared only if present
    #    on both sides — it comes from git, not the docs).
    md_problems = []
    for k in ["framework", "target", "date", "deployment_classification"]:
        if disk["metadata"].get(k, "") != gen["metadata"].get(k, ""):
            md_problems.append("  metadata.%s: inventory=%r docs=%r"
                               % (k, disk["metadata"].get(k, ""), gen["metadata"].get(k, "")))
    if disk["metadata"].get("commit") and gen["metadata"].get("commit") \
            and disk["metadata"]["commit"] != gen["metadata"]["commit"]:
        md_problems.append("  metadata.commit: inventory=%r git=%r"
                           % (disk["metadata"]["commit"], gen["metadata"]["commit"]))
    results.append(("metadata matches documents", not md_problems, md_problems))

    # 3. Analysis file present at all (every later threat check depends on it).
    results.append((
        "analysis file (2-*-analysis.md) present",
        support["analysis_found"],
        [] if support["analysis_found"] else ["  no 2-*-analysis.md in %s" % run_dir],
    ))

    # 4-6. components / flows / threats agree with the documents, field by
    #      field. (0.1-architecture.md, 1-model.md, 2-*-analysis.md +
    #      3-findings.md for status/finding/cwe/owasp.)
    cp = _diff_lists([canon_component(c) for c in disk["components"]],
                     gen["components"])
    results.append(("components match 0.1-architecture.md", not cp, cp))
    fp = _diff_lists(disk["flows"], gen["flows"])
    results.append(("flows match 1-model.md", not fp, fp))
    tp = _diff_lists(disk["threats"], gen["threats"])
    results.append(("threats match 2-*-analysis.md / 3-findings.md", not tp, tp))

    # 7. Threat-id parity: every threats[].id appears in the analysis file and
    #    vice-versa (skeleton-inventory.md POST-WRITE check 1).
    disk_ids = {t.get("id") for t in disk["threats"]}
    doc_ids = {g["id"] for g in gen["threats"]}
    parity = []
    for i in sorted(doc_ids - disk_ids, key=natkey):
        parity.append("  in %s, missing from inventory: %s" % (support["analysis_file"], i))
    for i in sorted(disk_ids - doc_ids, key=natkey):
        parity.append("  in inventory, not in %s: %s" % (support["analysis_file"] or "analysis", i))
    results.append(("threat ids parity with analysis file", not parity, parity))

    # 8. Reference integrity: every component / flows.from / flows.to / finding
    #    reference resolves to an existing id (skeleton-inventory.md checks 2-3).
    comp_ids = {c.get("id") for c in disk["components"]}
    fids = support["finding_ids"]
    ref = []
    for t in disk["threats"]:
        if t.get("component") not in comp_ids:
            ref.append("  threat %s.component=%r has no matching component id"
                       % (t.get("id"), t.get("component")))
        if t.get("finding") and t.get("finding") not in fids:
            ref.append("  threat %s.finding=%r not found in 3-findings.md"
                       % (t.get("id"), t.get("finding")))
    for f in disk["flows"]:
        for end in ("from", "to"):
            if f.get(end) not in comp_ids:
                ref.append("  flow %s.%s=%r has no matching component id"
                           % (f.get("id"), end, f.get(end)))
    results.append(("references resolve to existing ids", not ref, ref))

    # 9. Counts consistent between inventory and documents.
    counts = []
    for name, dl, gl in (("components", disk["components"], gen["components"]),
                         ("flows", disk["flows"], gen["flows"]),
                         ("threats", disk["threats"], gen["threats"])):
        if len(dl) != len(gl):
            counts.append("  %s: inventory=%d docs=%d" % (name, len(dl), len(gl)))
    results.append(("counts consistent with documents", not counts, counts))

    ok = all(p for _, p, _ in results)
    return results, ok


# --------------------------------------------------------------------------- #
# Entry point
# --------------------------------------------------------------------------- #

def main(argv=None):
    ap = argparse.ArgumentParser(
        description="Build or validate a threat-model inventory.yaml from a run's docs.")
    ap.add_argument("run_dir", help="the completed run's output directory")
    ap.add_argument("--check", action="store_true",
                    help="validate an existing inventory.yaml against the docs "
                         "(PASS/FAIL per check; non-zero exit on any FAIL)")
    args = ap.parse_args(argv)

    # The output uses a few non-ASCII glyphs; default Windows console encoding
    # (cp1252) can't encode them. Force UTF-8 so output is identical on every
    # platform, with a graceful fallback (same posture as recon.py).
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    if not os.path.isdir(args.run_dir):
        sys.stderr.write("inventory.py: not a directory: %s\n" % args.run_dir)
        return 2

    if args.check:
        results, ok = run_checks(args.run_dir)
        for name, passed, evidence in results:
            sys.stdout.write("%s  %s\n" % ("PASS" if passed else "FAIL", name))
            for line in evidence:
                sys.stdout.write(line + "\n")
        sys.stdout.write("\n%s: %d checks, %d passed, %d failed\n" % (
            "OK" if ok else "FAILED",
            len(results), sum(1 for _, p, _ in results if p),
            sum(1 for _, p, _ in results if not p)))
        return 0 if ok else 1

    inv, support = build_inventory(args.run_dir)
    if not support["analysis_found"]:
        sys.stderr.write(
            "inventory.py: warning: no 2-*-analysis.md in %s — threats will be empty\n"
            % args.run_dir)
    out_path = os.path.join(args.run_dir, "inventory.yaml")
    try:
        with open(out_path, "w", encoding="utf-8") as fh:
            fh.write(emit_yaml(inv))
    except OSError as e:
        sys.stderr.write("inventory.py: could not write %s: %s\n" % (out_path, e))
        return 1
    sys.stderr.write(
        "inventory.py: wrote %s (%d components, %d flows, %d threats)\n"
        % (out_path, len(inv["components"]), len(inv["flows"]), len(inv["threats"])))
    return 0


if __name__ == "__main__":
    sys.exit(main())
