#!/usr/bin/env python3
"""Threat-model scaffolder — pre-write the seven report files *from the
skeletons* so a weak model fills sections instead of authoring structure.

Companion script for the `threat-modeling` skill's setup step (SKILL.md §4.1).
It replaces the "hand-write a bare stub per file" step with a
deterministic pass that writes each of the seven files with its **real
structure already in place** — every fixed heading, every table's header row and
separator, and every fixed-value reference — leaving a unique, self-describing
`<!-- PENDING: <section-key> -->` marker (P38.7) wherever run-specific content
(a table's rows, a prose section, the per-component sections) must be filled in.

Why a script: the P38.1 live test (qwen3:14b) showed the linear build *runs* but
its output does not conform — a 14B model skips the skeleton templates entirely,
so its files have no Element/Data-Flow tables, no `### FIND-##` headings, use
`graph TD` instead of `flowchart LR`, and it re-authors freeform structure on
every self-correction pass instead of converging. Moving the structure out of the
model's hands fixes that: the model edits cells into a fixed table rather than
inventing the table, `verify.py`/`lint_dfd.py` check a real structure from turn
one, and the `<!-- PENDING: … -->` markers the P38.2 drive-to-completion keys on
are written for it rather than skipped — each marker keyed to its section so an
`edit_file` targets exactly one site (P38.7).

The structure written here is the *same* structure the reference skeletons
(`references/skeletons/skeleton-<framework>.md`) and `references/output-formats.md`
define — this script is their machine-applied form. The five framework-agnostic
files (`0-assessment.md`, `0.1-architecture.md`, `1.1-model.mmd`, `1-model.md`,
`3-findings.md`) come from `output-formats.md`; the one framework-specific file
(`2-<framework>-analysis.md`) from that framework's skeleton.

Contract (same posture as the sibling `recon.py`/`verify.py`):
  * Python 3.7+, standard library only — no third-party imports.
  * Deterministic: same run dir + framework + target/date -> byte-identical
    files (no clock reads; timestamps stay `<!-- PENDING -->` for the model).
  * Facts only, never decisions. It writes empty structure; every judgment cell
    is a `<!-- PENDING -->` the model fills. It never invents a component, a
    threat, a severity, or a deployment class.
  * Never clobbers work. A file that already exists and no longer carries a
    `<!-- PENDING -->` marker (i.e. filling has started) is left untouched unless
    --force is given, so re-running on an in-progress directory is safe.

Usage:
  python scaffold.py <run-dir> --framework <name> [--target SLUG] [--date DATE]
                     [--force] [--quiet]

  <run-dir>       the run directory to scaffold (created if missing) —
                  `.aegis/security/threat-model/<framework>-<target>-<ts>/`
  --framework     one of: stride, stride-a, linddun, pasta, trike, vast,
                  nist-800-154 (aliases: nist, 800-154)
  --target SLUG   the target slug for the file titles; omit to leave a
                  `<!-- PENDING -->` in the title for the model to fill
  --date DATE     the analysis date (YYYY-MM-DD) for the title block; omit to
                  leave a `<!-- PENDING -->` (this script reads no clock, so the
                  date is never guessed)
  --force         overwrite existing files even if filling has started
  --quiet         print only the summary line

Besides the seven report files this also writes a hidden sidecar,
`.scaffold-manifest.json` (P52.7): the list of every section it left a
`<!-- PENDING: <key> -->` marker in, keyed by the enclosing heading. `verify.py`
reads it to assert *"every site that had a marker now has substance"* rather
than only *"no marker remains"* — see `derive_manifest` below.

Exit code: 0 on success, 2 on a usage/IO error.
"""

import argparse
import json
import os
import re
import sys

# P38.7: every fillable section gets a *unique, self-describing* marker
# (`<!-- PENDING: deployment-classification -->`) rather than an identical bare
# `<!-- PENDING -->` repeated across the file. A weak model filling
# section-by-section reaches for `edit_file(old="<!-- PENDING -->", ...)`, which
# matched every section at once; a `replace_all: true` retry then overwrote a
# whole file's distinct sections with one wrong string (this is exactly what
# nuked architecture.md in the P38.1 re-test). A keyed marker's old-string
# naturally targets exactly one site. The shared prefix keeps the drive-oracle
# (internal/cli/chat.go) and verify.py substring scans matching every marker.
PENDING_PREFIX = "<!-- PENDING"


def pending(key):
    """A unique, self-describing PENDING marker naming one fillable section."""
    return "%s: %s -->" % (PENDING_PREFIX, key)

# The seven files every run produces (output-formats.md's File list). The
# analysis file name is templated with the framework short name.
AGNOSTIC_FILES = (
    "0-assessment.md",
    "0.1-architecture.md",
    "1.1-model.mmd",
    "1-model.md",
    "3-findings.md",
    "inventory.yaml",
)

# framework arg -> (short name used in the filename, display title, is STRIDE-A)
FRAMEWORKS = {
    "stride": ("stride", "STRIDE", False),
    "stride-a": ("stride", "STRIDE-A", True),
    "linddun": ("linddun", "LINDDUN", False),
    "pasta": ("pasta", "PASTA", False),
    "trike": ("trike", "Trike", False),
    "vast": ("vast", "VAST", False),
    "nist-800-154": ("nist-800-154", "NIST SP 800-154", False),
    "nist": ("nist-800-154", "NIST SP 800-154", False),
    "800-154": ("nist-800-154", "NIST SP 800-154", False),
}


# --------------------------------------------------------------------------- #
# Small builders — real structure out, `<!-- PENDING -->` where content goes
# --------------------------------------------------------------------------- #

def _title(target):
    """A title token: the given slug, or a PENDING marker for the model."""
    return target if target else pending("target")


def _date(date):
    return date if date else pending("date")


def table(headers, key, guidance=None):
    """A markdown table with a real header + separator and a PENDING body.

    The header and separator are fixed structure (the thing weak models get
    wrong); the single `<!-- PENDING: <key> -->` line below the separator is
    where the model edits real rows in — `key` names the section so that marker
    is unique in the file (P38.7). Guidance, if given, is prose inside a plain
    HTML comment — never a pipe-table shape, so verify.py's table parser can't
    mistake it for a real (mis-placed) table.
    """
    head = "| " + " | ".join(headers) + " |"
    sep = "|" + "|".join("-" * max(3, len(h) + 2) for h in headers) + "|"
    lines = [head, sep]
    if guidance:
        lines.append("<!-- guidance: %s -->" % guidance)
    lines.append(pending(key))
    return "\n".join(lines)


def prose(key, guidance=None):
    """A prose section body: optional guidance comment then a keyed PENDING marker."""
    if guidance:
        return "<!-- guidance: %s -->\n%s" % (guidance, pending(key))
    return pending(key)


# --------------------------------------------------------------------------- #
# The shared Mermaid DFD stub (1.1-model.mmd) — a lint-clean skeleton the model
# grows into the real diagram, then mirrors verbatim into 1-model.md's fence.
# It carries the three fixed classDefs and a `flowchart LR` header so lint_dfd
# passes checks 1-5 from turn one; the PENDING marker (inside a %% comment so
# lint ignores it but verify/the drive-oracle still see it) keeps the drive
# alive until the model draws the graph.
# --------------------------------------------------------------------------- #

MMD_STUB = "\n".join([
    "flowchart LR",
    "%% guidance: replace this stub with the real DFD, then copy it verbatim",
    "%% into 1-model.md's ```mermaid fence (the two must stay byte-identical).",
    "%% shapes: external Id([\"Label\"]); process Id[\"Label\"]; store Id[(\"Label\")]",
    "%% one subgraph per trust boundary; label every edge; use <--> for a single",
    "%% bidirectional request/response; keep the three classDefs below and add",
    "%% `class <Id> process|external|datastore` per node.",
    "classDef process fill:#6baed6,stroke:#2171b5,color:#000",
    "classDef external fill:#fdae61,stroke:#d94701,color:#000",
    "classDef datastore fill:#74c476,stroke:#238b45,color:#000",
    "%% " + pending("dfd") + " add nodes, subgraphs, and labeled edges above",
])


# --------------------------------------------------------------------------- #
# The five framework-agnostic files (output-formats.md)
# --------------------------------------------------------------------------- #

def build_architecture():
    return "\n".join([
        "# Architecture Overview",
        "",
        "## System Purpose",
        prose("system-purpose",
              "2-4 sentences: what the system does, the problem it solves, "
              "who its users/operators are"),
        "",
        "## Key Components",
        table(["Component", "Type", "Anchor", "Description"], "key-components",
              "one row per component; Type is Process / External Interactor / "
              "Data Store; Anchor is a real file/class/manifest; names must "
              "match the diagram and the analysis file"),
        "",
        "## Component Diagram",
        prose("component-diagram",
              "a component/architecture-level Mermaid diagram (NOT the DFD — "
              "that is 1.1-model.mmd/1-model.md); see diagram-conventions.md"),
        "",
        "## Top Scenarios",
        prose("top-scenarios",
              "3-5 key workflows; scenario 1 MUST include a Mermaid "
              "sequenceDiagram naming real components; the rest may be prose"),
        "",
        "### Scenario 1",
        prose("scenario-1", "name + 2-3 sentence description + a sequenceDiagram"),
        "",
        "### Scenario 2",
        prose("scenario-2"),
        "",
        "### Scenario 3",
        prose("scenario-3"),
        "",
        "## Technology Stack",
        table(["Layer", "Technologies"], "technology-stack",
              "rows: Languages, Frameworks, Data Stores, Infrastructure, "
              "Security — one per layer"),
        "",
        "## Deployment Classification",
        prose("deployment-classification",
              "exactly one of internet-facing / internal-network / "
              "localhost-service / local-desktop, with the specific evidence "
              "(bind addresses, listeners, ingress, ports). BINDING on every "
              "threat's prerequisite floor and every finding's CVSS AV"),
        "",
        "## Component Exposure Table",
        table(["Component", "Listen Address", "Auth Barrier",
               "External Reachability", "Min Prerequisite"], "component-exposure",
              "one row per Key Component; External Reachability is none / "
              "internal-network / external; Min Prerequisite is one of the "
              "five fixed values. This table is the single source of truth for "
              "prerequisite floors"),
        "",
        "## Security Infrastructure Inventory",
        table(["Component", "Security Role", "Configuration", "Notes"],
              "security-infrastructure",
              "one row per security-relevant component found (SKILL.md §3)"),
        "",
        "## Coverage Ledger",
        table(["Directory", "Status", "Notes"], "coverage-ledger",
              "one row per top-level directory from recon.py's \"Top-level "
              "directories\" section, including its auto-excluded ones -- "
              "every directory must be accounted for. Status is either "
              "\"Covered -- <component>\" (the Key Component whose Anchor "
              "lives under it) or \"Excluded -- <reason>\" (vendored, "
              "generated, build output, docs, tests, lockfiles, or genuinely "
              "out of scope). A directory missing from this table is a "
              "dropped scope, not a summary omission (SKILL.md §2 step 6)"),
        "",
    ])


def build_model():
    diagram = "## Diagram\n<!-- guidance: keep this block byte-identical to " \
              "1.1-model.mmd (copy, do not regenerate). -->\n```mermaid\n" \
              + MMD_STUB + "\n```"
    return "\n".join([
        "# Threat Model — Data Flow",
        "",
        diagram,
        "",
        "## Element Table",
        table(["Element", "Type", "Description", "Trust Boundary"], "element-table",
              "one row per element in the diagram; names match "
              "0.1-architecture.md's Key Components verbatim"),
        "",
        "## Data Flow Table",
        table(["ID", "Source", "Target", "Protocol", "Description"],
              "data-flow-table",
              "one row per flow, ids DF01, DF02, ...; a request/response pair "
              "is one bidirectional flow, not two"),
        "",
        "## Trust Boundary Table",
        table(["Boundary", "Description", "Contains"], "trust-boundary-table",
              "one row per subgraph in the diagram"),
        "",
    ])


def build_findings():
    return "\n".join([
        "# Findings",
        "",
        "<!-- guidance: findings are organized by Exploitability Tier, then "
        "sorted within a tier by severity (Critical->Low) then CVSS 4.0 "
        "descending. All three tier headings are always present. Each finding "
        "is a `### FIND-##` block (sequential, no gaps, document order) with "
        "the mandatory attribute table (Severity, Exploitability Tier, CVSS "
        "4.0 score+vector, CWE, OWASP, Exploitation Prerequisites, Remediation "
        "Effort, Component, Related Threats) followed by Description / Evidence "
        "/ Remediation / Verification sub-sections. Tier is DERIVED from the "
        "prerequisite (None->1; Authenticated User/Internal Network->2; Local "
        "Process/Host Compromise->3). See output-formats.md for the exact "
        "finding template. -->",
        "",
        "## Tier 1 — Direct Exposure (No Prerequisites)",
        prose("tier-1-findings",
              "`### FIND-##` blocks whose prerequisite is None, or "
              "\"*No Tier 1 findings identified for this target.*\""),
        "",
        "## Tier 2 — Conditional Risk (Single Prerequisite)",
        prose("tier-2-findings",
              "`### FIND-##` blocks whose prerequisite is Authenticated User "
              "or Internal Network, or the empty-tier line"),
        "",
        "## Tier 3 — Defense-in-Depth (Prior Compromise / Host Access)",
        prose("tier-3-findings",
              "`### FIND-##` blocks whose prerequisite is Local Process or "
              "Host Compromise, or the empty-tier line"),
        "",
        "## Threat Coverage Verification",
        table(["Threat ID", "Finding ID", "Status"], "threat-coverage",
              "one row per threat id in the analysis file, exactly once; "
              "Status is Covered (FIND-XX) / Mitigated (FIND-XX) / Mitigated "
              "by platform — no other value (Accepted Risk and Needs Review "
              "are forbidden)"),
        "",
    ])


def build_assessment(target):
    def h(name):
        return "## " + name

    return "\n".join([
        "# Threat Model Assessment — " + _title(target),
        "",
        h("Report Files"),
        table(["File", "Description"], "report-files",
              "one row per file in the suite; 0-assessment.md is listed first"),
        "",
        "---",
        "",
        h("Executive Summary"),
        prose("executive-summary",
              "framework used (and why, if inferred), deployment "
              "classification, and the overall risk rating as plain text (no "
              "emoji). Include the standard \"Note on threat counts\" "
              "paragraph"),
        "",
        "---",
        "",
        h("Action Summary"),
        table(["Tier", "Description", "Threats", "Findings", "Priority"],
              "action-summary",
              "one row per tier (Tier 1/2/3) plus a bold **Total** row; counts "
              "must match the analysis and findings files"),
        "",
        "### Quick Wins",
        table(["Finding", "Title", "Why quick"], "quick-wins",
              "Tier 1 findings with Remediation Effort: Low, or a plain "
              "statement that there are none"),
        "",
        "---",
        "",
        h("Analysis Context & Assumptions"),
        "",
        "### Analysis Scope",
        table(["Constraint", "Description"], "analysis-scope",
              "rows: Scope, Excluded"),
        "",
        "### Needs Verification",
        table(["Item", "Question", "What to check", "Why uncertain"],
              "needs-verification",
              "one row per un-evidenced claim held back from the threat "
              "tables"),
        "",
        "### Finding Overrides",
        table(["Finding ID", "Original Severity", "Override", "Justification",
               "New Status"], "finding-overrides",
              "one row per override, or a single \"No overrides applied.\" "
              "row"),
        "",
        "---",
        "",
        h("References Consulted"),
        "",
        "### Security Standards",
        table(["Standard", "URL", "How Used"], "references-standards",
              "CVSS 4.0, CWE, OWASP, plus the chosen framework's own standard "
              "reference"),
        "",
        "### Component Documentation",
        table(["Component", "Documentation URL", "Relevant Section"],
              "component-documentation",
              "one row per component with external docs, if any"),
        "",
        "---",
        "",
        h("Report Metadata"),
        table(["Field", "Value"], "report-metadata",
              "rows: Source Location, Git Repository, Git Branch, Git Commit, "
              "Analysis Started, Analysis Completed, Output Directory — all "
              "backtick-wrapped; get git/timestamp values from the tools, "
              "never guess"),
        "",
        "---",
        "",
        h("Classification Reference"),
        table(["Term", "Meaning"], "classification-reference",
              "the fixed tier and deployment-class glossary "
              "(output-formats.md)"),
        "",
    ])


def build_inventory():
    # inventory.yaml is generated wholesale by inventory.py at phase 5 — this is
    # a placeholder that keeps the file present (and PENDING, so the drive keeps
    # going) until that run overwrites it. It is intentionally not valid YAML;
    # nothing parses it before inventory.py replaces it.
    return "\n".join([
        "# threat-model inventory sidecar",
        "# Do NOT hand-write this file. It is generated deterministically at",
        "# phase 5 by:  python inventory.py <run-dir>",
        "# which overwrites this placeholder wholesale from the finished docs.",
        "# " + pending("inventory-sidecar")
        + " run inventory.py to generate the real sidecar.",
    ])


# --------------------------------------------------------------------------- #
# The framework-specific analysis file (skeletons/skeleton-<framework>.md)
# --------------------------------------------------------------------------- #

# The 8-column per-component threat table STRIDE and LINDDUN share.
_TIER_COLS = ["ID", "Category", "Threat", "Evidence", "Prerequisite",
              "Mitigation", "Residual risk", "Severity"]

_FIXED_VALUES = (
    "Fixed values — Prerequisite is one of None / Authenticated User / "
    "Internal Network / Local Process / Host Compromise; Severity is one of "
    "Critical / High / Medium / Low; Tier is DERIVED from Prerequisite "
    "(None->1; Authenticated User/Internal Network->2; Local Process/Host "
    "Compromise->3), never assigned independently."
)


def _analysis_header(display, target, date, extra_lines=()):
    lines = [
        "# %s Threat Analysis — %s" % (display, _title(target)),
        "",
        "> Framework: %s — <!-- guidance: why chosen, or "
        "\"default — no stronger signal\" -->" % display,
        "> Deployment classification: <!-- guidance: one of the four fixed "
        "values; must match 0.1-architecture.md -->",
        "> Date: %s" % _date(date),
    ]
    lines.extend(extra_lines)
    lines.append("")
    lines.append("<!-- guidance: %s -->" % _FIXED_VALUES)
    lines.append("")
    return lines


def _tiered_component_guidance(id_prefix, categories, subhead):
    """Prose describing the per-component three-tier structure to repeat."""
    return (
        "below, add one component section per component in "
        "0.1-architecture.md's Key Components table, using the exact "
        "(anchor-safe) name as a `## <Component Name>` heading. Each component "
        "section has all three tier sub-sections in order, always present even "
        "when empty: %s Tier 1 — Direct Exposure (No Prerequisites), %s Tier 2 "
        "— Conditional Risk, %s Tier 3 — Defense-in-Depth. Each holds an "
        "8-column table (%s) or the italic \"*No Tier N threats identified for "
        "this component.*\" line. Category is one of: %s. Threat ids are %s1, "
        "%s2, ... sequential across the whole file." % (
            subhead, subhead, subhead, ", ".join(_TIER_COLS),
            categories, id_prefix, id_prefix)
    )


def build_stride(target, date, stride_a):
    display = "STRIDE-A" if stride_a else "STRIDE"
    cats = "Spoofing, Tampering, Repudiation, Information Disclosure, Denial " \
           "of Service, Elevation of Privilege"
    summary_cols = ["Component", "S", "T", "R", "I", "D", "E"]
    if stride_a:
        cats += ", Abuse of Functionality (A — only for abusable legitimate " \
                "features; plain authz failures stay under E)"
        summary_cols.append("A")
    summary_cols += ["Total", "Tier 1", "Tier 2", "Tier 3"]

    lines = _analysis_header(display, target, date)
    lines += [
        "## Summary",
        "<!-- guidance: write this section LAST; one row per component (same "
        "order as the sections below) plus a bold **Total** row. Each category "
        "cell is a count of concrete threat rows; per-category counts and "
        "Tier 1/2/3 counts must each sum to Total. -->",
        table(summary_cols, "summary"),
        "",
        "<!-- guidance: %s -->" % _tiered_component_guidance("T", cats, "####"),
        pending("component-sections"),
        "",
    ]
    return "\n".join(lines)


def build_linddun(target, date):
    cats = "Linkability, Identifiability, Non-repudiation, Detectability, " \
           "Disclosure of information, Unawareness, Non-compliance"
    lines = _analysis_header("LINDDUN", target, date)
    lines += [
        "## Personal Data Inventory",
        table(["Data category", "Source", "Purpose of collection",
               "Retention", "Legal basis"], "personal-data-inventory",
              "one row per distinct category of personal data; an unbounded "
              "Retention or undocumented Legal basis is itself a finding — "
              "carry it into a Non-compliance row"),
        "",
        "## In-Scope Elements",
        prose("in-scope-elements",
              "which components/flows carry personal data (get a full LINDDUN "
              "section below) and which are scoped out (one line each, why)"),
        "",
        "## Summary",
        "<!-- guidance: write LAST; one row per in-scope component plus a bold "
        "**Total** row. The seven category counts and the Tier 1/2/3 counts "
        "must each sum to Total. Ids prefix L (L1, L2, ...). -->",
        table(["Component", "L", "I", "N-Rep", "D-Det", "D-Disc", "U",
               "N-Comp", "Total", "Tier 1", "Tier 2", "Tier 3"], "summary"),
        "",
        "<!-- guidance: %s -->" % _tiered_component_guidance("L", cats, "###"),
        pending("component-sections"),
        "",
    ]
    return "\n".join(lines)


def build_pasta(target, date):
    lines = _analysis_header("PASTA", target, date)
    # PASTA titles its file "PASTA Risk Analysis"; fix the first heading.
    lines[0] = "# PASTA Risk Analysis — %s" % _title(target)
    lines += [
        "## Stage 1 — Business Objectives",
        prose("stage-1",
              "prose: what the system does and the concrete business "
              "consequence categories (revenue, compliance, reputation, "
              "safety) a successful attack produces; Stage 7 cites these"),
        "",
        "## Stage 2-3 — Technical Scope and Decomposition",
        prose("stage-2-3",
              "brief cross-reference to 0.1-architecture.md / 1-model.md plus "
              "any PASTA-specific scoping (actor/use-case list, out-of-scope "
              "notes) — do not duplicate the component/DFD tables"),
        "",
        "## Summary",
        "<!-- guidance: write LAST; one row per threat, sorted by Risk Rating "
        "descending then Tier ascending. Ids prefix P (P1, P2, ...). -->",
        table(["ID", "Threat", "Business Impact", "Tier", "Risk Rating"],
              "summary"),
        "",
        "## Stage 4-5 — Threat and Vulnerability Analysis",
        table(["ID", "Threat", "Attack Vector", "Evidence", "Prerequisite",
               "Tier", "Mitigation", "Residual Risk", "Severity"], "stage-4-5",
              "one row per (component, credible threat) pair; prefer citing a "
              "real CWE/CVE/advisory class in Attack Vector; ids P1, P2, ..."),
        "",
        "## Stage 6 — Attack Modeling",
        prose("stage-6",
              "for each Tier 1 threat and every Critical/High threat, an "
              "attack tree or written attack path (entry -> steps -> impact); "
              "give each path an id AP1, AP2, ... that Stage 7 references"),
        "",
        "## Stage 7 — Risk and Impact Analysis",
        table(["Attack Path", "Threat", "Likelihood", "Business Impact",
               "Risk Rating", "Priority"], "stage-7",
              "one row per attack path, sorted by Priority ascending; Business "
              "Impact must be a Stage 1 category; Risk Rating here is the "
              "authoritative severity"),
        "",
    ]
    return "\n".join(lines)


def build_trike(target, date):
    lines = _analysis_header("Trike", target, date)
    lines[0] = "# Trike Risk Analysis — %s" % _title(target)
    lines += [
        "## Actors",
        prose("actors",
              "bullet list, one per distinct actor class (split where the real "
              "authz code distinguishes them): `- <actor> — <what makes this "
              "class distinct>`"),
        "",
        "## Assets",
        prose("assets", "bullet list: `- <asset> — <why it matters>`"),
        "",
        "## Permission Matrix",
        table(["Actor", "Asset", "Action", "Allowed?"], "permission-matrix",
              "one row per (actor, asset, action) triple from the REAL authz "
              "code/config; Allowed? is exactly allowed or denied. This is "
              "Trike's foundational artifact — everything below derives from "
              "it"),
        "",
        "## Summary",
        "<!-- guidance: write after Risk Analysis; one row per risk, same "
        "order. Ids prefix R (R1, R2, ...). -->",
        table(["ID", "Actor", "Asset", "Action", "Risk", "Tier", "Decision"],
              "summary"),
        "",
        "## Risk Analysis",
        "<!-- guidance: one `### Asset: <name>` subsection per asset (findings "
        "anchor to these), each with a 12-column table: %s. Risk is DERIVED "
        "from Probability x Impact and Severity equals Risk; Tier is derived "
        "from Prerequisite; Decision is \"mitigate: <control>\" or \"pending "
        "decision\" — never a bare model-authored accept. Ids R1, R2, ... "
        "sequential across the whole file. -->" % ", ".join([
            "ID", "Actor", "Action", "Threat", "Evidence", "Prerequisite",
            "Tier", "Probability", "Impact", "Risk", "Decision", "Severity"]),
        pending("risk-analysis"),
        "",
    ]
    return "\n".join(lines)


def build_vast(target, date):
    extra = [
        "> Model Type: <!-- guidance: Application Threat Model or Operational "
        "Threat Model — pick one -->",
        "> Enumeration method: <!-- guidance: e.g. STRIDE categories per "
        "diagram element (the default) -->",
        "> CVSS/CWE/OWASP mapping borrowed: <!-- guidance: STRIDE mapping "
        "(Application) or NIST-800-154 mapping (Operational) -->",
    ]
    lines = _analysis_header("VAST", target, date, extra)
    # VAST's file title has no "Threat Analysis" suffix in its skeleton.
    lines[0] = "# VAST Analysis — %s" % _title(target)
    lines += [
        "## Summary",
        "<!-- guidance: one row per backlog item below, same order, plus a "
        "bold **Total** row. Ids prefix V (V1, V2, ...). Ticket-ready is Yes "
        "or \"Needs refinement — <why>\". -->",
        table(["ID", "Title", "Severity", "Tier", "Ticket-ready"], "summary"),
        "",
        "## Backlog Items",
        "<!-- guidance: one `### V##: <ticket-ready title>` block per threat "
        "(anchor-safe title). Each block has a 5-column table (Severity, Tier, "
        "Prerequisite, Evidence, Acceptance Criteria), a one-paragraph "
        "description, and a bulleted \"Acceptance criteria for mitigation:\" "
        "list of testable statements. Tier is derived from Prerequisite; ids "
        "V1, V2, ... sequential. -->",
        pending("backlog-items"),
        "",
    ]
    return "\n".join(lines)


def build_nist(target, date):
    extra = [
        "> Sensitive Data Type(s) in Scope: <!-- guidance: the specific "
        "dataset(s) this run covers — never \"all data\" -->",
    ]
    lines = _analysis_header("NIST SP 800-154 Data-Centric", target, date,
                             extra)
    lines[0] = "# NIST SP 800-154 Data-Centric Analysis — %s" % _title(target)
    lines += [
        "## Data Inventory",
        table(["Data Type", "Location", "Component", "Data Location",
               "Evidence"], "data-inventory",
              "one row per place the in-scope data exists; Data Location is "
              "exactly At Rest / In Transit / In Use; Component matches "
              "0.1-architecture.md verbatim"),
        "",
        "## Summary",
        "<!-- guidance: write after Attack Vector Analysis; one row per attack "
        "vector, matching that table exactly. Ids prefix N (N1, N2, ...). -->",
        table(["ID", "Data Location", "Attack Vector", "Tier", "Severity",
               "Control Status"], "summary"),
        "",
        "## Attack Vector Analysis",
        table(["ID", "Data Location", "Attack Vector", "Evidence",
               "Prerequisite", "Tier", "Control", "Control Verified?",
               "Residual risk", "Severity"], "attack-vector-analysis",
              "one row per (data-location, attack-vector) pair; Control "
              "Verified? is exactly yes or \"no — gap\" (every no — gap is an "
              "open finding); Tier derived from Prerequisite; ids N1, N2, ..."),
        "",
    ]
    return "\n".join(lines)


ANALYSIS_BUILDERS = {
    "stride": build_stride,   # takes stride_a flag, handled specially
    "linddun": build_linddun,
    "pasta": build_pasta,
    "trike": build_trike,
    "vast": build_vast,
    "nist-800-154": build_nist,
}


def build_analysis(short, display, stride_a, target, date):
    if short == "stride":
        return build_stride(target, date, stride_a)
    return ANALYSIS_BUILDERS[short](target, date)


# --------------------------------------------------------------------------- #
# Assembly + file writing
# --------------------------------------------------------------------------- #

def build_all(short, display, stride_a, target, date):
    """Return {filename: content} for the full seven-file suite."""
    files = {
        "0-assessment.md": build_assessment(target),
        "0.1-architecture.md": build_architecture(),
        "1.1-model.mmd": MMD_STUB + "\n",
        "1-model.md": build_model(),
        "3-findings.md": build_findings(),
        "inventory.yaml": build_inventory(),
        "2-%s-analysis.md" % short: build_analysis(
            short, display, stride_a, target, date),
    }
    # Normalize: every file ends with exactly one trailing newline.
    return {name: content.rstrip("\n") + "\n" for name, content in files.items()}


# --------------------------------------------------------------------------- #
# The section manifest (P52.7) — what verify.py asserts substance against
# --------------------------------------------------------------------------- #
#
# check 1 (no-leftover-skeleton-syntax) proves only that no `<!-- PENDING -->`
# marker survived. A weak model can satisfy it by *deleting* the marker and
# writing nothing, leaving a heading over empty space — structurally intact,
# substantively blank. P47.9's `finding-bodies-nonempty` caught that for
# `3-findings.md`'s per-finding subsections; every other file was unguarded.
#
# This script is the only thing that knows every site it left a marker in, so it
# records them. The manifest is *derived from the built content* rather than
# collected inside `table()`/`prose()` on purpose: the builders stay pure string
# assembly, and any future section added to any builder shows up in the manifest
# automatically with no second place to keep in sync.
#
# It is written to a hidden `.json` sidecar in the run directory, mirroring the
# established `.quality-stamp.json` convention (internal/cli/chat_verify.go):
# `.json` is outside the {.md,.mmd,.yaml,.yml,.txt} extension set that
# verify.py's check 1, the drive's PENDING scanner, the suite file count and the
# suite fingerprint all read, and it never contains the literal marker text — so
# no existing scanner can trip on it. It lives in the *run* directory, not the
# skill directory, so the built-in-skill self-heal path (which only reconciles
# `.aegis/builtin-skills/<skill>/` against the embedded copy) cannot disturb it,
# and an upgraded binary refreshing scaffold.py/verify.py mid-run leaves every
# already-written manifest exactly as it was.
#
# The entry shape is deliberately a superset of what the hollowness check needs,
# so the substance floor (P52.8) can reuse it without a second manifest: `kind`
# + `columns` locate a scaffolded table and name its columns (so a rule can say
# "the Evidence column must carry a line number, symbol, or config key", or cap
# the fraction of `None identified` cells per table), and the `kind: "prose"`
# entries are exactly the narrative sections a minimum-prose-length rule applies
# to. Adding a field is backward compatible in both directions: verify.py
# validates each field independently and ignores what it doesn't know.

MANIFEST_FILE = ".scaffold-manifest.json"
MANIFEST_VERSION = 1

_HEADING_RE = re.compile(r"^(#{1,6})\s+(.*\S)\s*$")
# A marker that is a line to itself — the body of a fillable section. An
# *inline* marker (`# Title — <!-- PENDING: target -->`, `> Date: …`) is a
# fill-in-the-blank inside a line, not a section body, and is not a manifest
# entry: check 1 already owns it and there is no body region to measure.
_PENDING_LINE_RE = re.compile(r"^<!--\s*PENDING:\s*([^\s>]+)\s*-->$")
_GUIDANCE_LINE_RE = re.compile(r"^<!--.*-->$")
_SEP_CELL_RE = re.compile(r":?-{2,}:?$")


def _row_cells(line):
    s = line.strip()
    if s.startswith("|"):
        s = s[1:]
    if s.endswith("|"):
        s = s[:-1]
    return [c.strip() for c in s.split("|")]


def _is_separator_line(line):
    """True for a markdown header/body separator like `|---|:--:|`."""
    s = line.strip()
    if not s.startswith("|"):
        return False
    cells = [c for c in _row_cells(line) if c]
    return bool(cells) and all(_SEP_CELL_RE.match(c) for c in cells)


def derive_manifest(files, framework=""):
    """Derive the section manifest from the freshly-built file contents.

    Returns the manifest dict written to `.scaffold-manifest.json`. One entry
    per standalone `<!-- PENDING: <key> -->` marker in a `.md` file:

      file          the file the section lives in
      key           the marker key (unique within the file)
      heading       the enclosing heading's text — verify.py's anchor, since the
                    marker itself is gone by the time verification runs
      level         that heading's `#` depth
      kind          "table" (the marker sits under a header+separator pair) or
                    "prose"
      columns       for a table, its scaffolded column names (null for prose)
      to_eof        the body region runs to end-of-file rather than stopping at
                    the next same-or-higher heading — true only for a *prose*
                    marker with no scaffolded heading after it, i.e. one whose
                    fill is a run of new sibling sections (STRIDE/LINDDUN's
                    per-component sections, Trike's per-asset risk analysis,
                    VAST's backlog items). A table is always measured over the
                    tight region so a hollow summary table can't be excused by
                    the component sections written below it.
      scaffold_line the marker's line number as scaffolded (informational; the
                    file is rewritten by the fill, so verify.py re-locates by
                    heading and never trusts this)

    `.mmd`/`.yaml` files are excluded: they carry no markdown headings and their
    content is owned by `lint_dfd.py` / `inventory.py`.

    Deterministic: same inputs -> byte-identical manifest.
    """
    sections = []
    for name in sorted(files):
        if not name.lower().endswith(".md"):
            continue
        lines = files[name].split("\n")
        headings = []           # (line_no_1based, level)
        entries = []
        cur = None              # (level, text, line_no) of the enclosing heading
        for i, raw in enumerate(lines):
            hm = _HEADING_RE.match(raw)
            if hm:
                cur = (len(hm.group(1)), hm.group(2).strip(), i + 1)
                headings.append((i + 1, len(hm.group(1))))
                continue
            pm = _PENDING_LINE_RE.match(raw.strip())
            if not pm or cur is None:
                continue
            # Look back past any guidance comment(s) for a table header +
            # separator pair; that is what `table()` writes above its marker.
            kind, columns = "prose", None
            j = i - 1
            while j >= 0 and _GUIDANCE_LINE_RE.match(lines[j].strip()):
                j -= 1
            if j >= 1 and _is_separator_line(lines[j]) and lines[j - 1].strip().startswith("|"):
                kind = "table"
                columns = _row_cells(lines[j - 1])
            entries.append({
                "file": name,
                "key": pm.group(1),
                "heading": cur[1],
                "level": cur[0],
                "kind": kind,
                "columns": columns,
                "to_eof": False,
                "scaffold_line": i + 1,
            })
        for e in entries:
            if e["kind"] != "prose":
                continue
            e["to_eof"] = not any(
                ln > e["scaffold_line"] and lvl <= e["level"] for ln, lvl in headings)
        sections.extend(entries)

    return {
        "manifest_version": MANIFEST_VERSION,
        "generator": "scaffold.py",
        "framework": framework,
        "sections": sections,
    }


def write_manifest(run_dir, manifest):
    """Write the manifest sidecar. Deterministic bytes; one trailing newline."""
    path = os.path.join(run_dir, MANIFEST_FILE)
    body = json.dumps(manifest, indent=2, sort_keys=True, ensure_ascii=False)
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(body + "\n")
    return path


def should_write(path, force):
    """True if the file may be (over)written without destroying started work.

    Writes a missing file, or an existing one that still reads as an untouched
    scaffold (every meaningful line is blank, a heading, a table skeleton, or a
    PENDING marker — i.e. it still contains a PENDING marker and no filled
    content). --force overrides. A file with no PENDING marker left is assumed
    to be in-progress/finished and is preserved.
    """
    if force or not os.path.exists(path):
        return True
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            data = fh.read()
    except OSError:
        return True
    return PENDING_PREFIX in data


def main(argv=None):
    ap = argparse.ArgumentParser(
        description="Scaffold the seven threat-model report files with real "
                    "structure and per-section PENDING markers.")
    ap.add_argument("run_dir", help="run directory to scaffold (created if "
                                     "missing)")
    ap.add_argument("--framework", required=True,
                    help="stride, stride-a, linddun, pasta, trike, vast, or "
                         "nist-800-154")
    ap.add_argument("--target", default="",
                    help="target slug for the file titles (default: leave "
                         "PENDING)")
    ap.add_argument("--date", default="",
                    help="analysis date YYYY-MM-DD for the title block "
                         "(default: leave PENDING — this script reads no clock)")
    ap.add_argument("--force", action="store_true",
                    help="overwrite files even if filling has started")
    ap.add_argument("--quiet", action="store_true",
                    help="print only the summary line")
    args = ap.parse_args(argv)

    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    key = args.framework.strip().lower()
    if key not in FRAMEWORKS:
        sys.stderr.write(
            "scaffold.py: unknown framework %r — choose one of: "
            "stride, stride-a, linddun, pasta, trike, vast, nist-800-154\n"
            % args.framework)
        return 2
    short, display, stride_a = FRAMEWORKS[key]

    try:
        os.makedirs(args.run_dir, exist_ok=True)
    except OSError as exc:
        sys.stderr.write("scaffold.py: cannot create %s: %s\n"
                         % (args.run_dir, exc))
        return 2

    files = build_all(short, display, stride_a, args.target, args.date)

    out = []
    written = 0
    skipped = 0
    for name in sorted(files):
        path = os.path.join(args.run_dir, name)
        if should_write(path, args.force):
            try:
                with open(path, "w", encoding="utf-8", newline="\n") as fh:
                    fh.write(files[name])
            except OSError as exc:
                sys.stderr.write("scaffold.py: cannot write %s: %s\n"
                                 % (path, exc))
                return 2
            written += 1
            out.append("WROTE %s" % name)
        else:
            skipped += 1
            out.append("KEPT  %s (already has filled content — use --force to "
                       "overwrite)" % name)

    # The section manifest (P52.7). Refreshed whenever this run actually wrote
    # structure, and backfilled when it is missing — an in-progress suite from a
    # pre-manifest scaffold.py gains one on the next scaffold call, which can
    # only add checks (verify.py skips any manifest heading the file doesn't
    # have). Left alone when every file was kept and a manifest already exists,
    # so a re-run can never replace a filled suite's manifest with a different
    # framework's sections.
    manifest_path = os.path.join(args.run_dir, MANIFEST_FILE)
    if written or args.force or not os.path.exists(manifest_path):
        try:
            write_manifest(args.run_dir,
                           derive_manifest(files, framework=short))
        except OSError as exc:
            sys.stderr.write("scaffold.py: cannot write %s: %s\n"
                             % (manifest_path, exc))
            return 2
        out.append("WROTE %s (verify.py section manifest)" % MANIFEST_FILE)
    else:
        out.append("KEPT  %s" % MANIFEST_FILE)

    if not args.quiet:
        sys.stdout.write("\n".join(out) + "\n")
    sys.stdout.write(
        "scaffolded %s in %s — %d written, %d kept\n"
        % (display, args.run_dir, written, skipped))
    return 0


if __name__ == "__main__":
    sys.exit(main())
