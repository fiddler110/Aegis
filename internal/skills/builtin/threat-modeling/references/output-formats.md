# Output Formats — the Report File Suite

Every threat model, whichever of the six frameworks was chosen, is delivered
as a **suite of files in one timestamped directory**, not a single document.
This file defines the structure and content of the five framework-agnostic
files in that suite. The one framework-specific file —
`2-<framework>-analysis.md` — has its own skeleton per framework under
`references/skeletons/skeleton-<framework>.md`; this file's job is the other
five, which are identical in shape no matter which framework is running.

**⛔ SELF-CORRECT DIRECTIVE:** after writing any file from a template below,
immediately run that section's "Post-write checks" before moving to the next
file. Fix failures now — do not carry them into the final §5 review round of
SKILL.md, which is for cross-file seams, not single-file defects this pass
would have caught immediately.

## Output directory

Created once, at the very start (SKILL.md §4.1):

```
.aegis/security/threat-model/<framework>-<target>-<YYYY-MM-DD-HHMM>/
```

`<framework>` is the chosen framework's short name (`stride`, `linddun`,
`pasta`, `trike`, `vast`, `nist-800-154`); `<target>` is the mandatory slug
from SKILL.md §4.1. All seven files below live directly in this directory —
never in a nested subdirectory, never alongside unrelated files.

## File list

| File | Purpose | Always written? |
|---|---|---|
| `0-assessment.md` | Executive front page: risk rating, action summary, scope/assumptions, references, metadata | Yes |
| `0.1-architecture.md` | Architecture overview: components, scenarios, tech stack, deployment classification | Yes |
| `1.1-model.mmd` | Raw Mermaid DFD source — source of truth for the diagram | Yes |
| `1-model.md` | Element/flow/boundary tables + the diagram rendered inline | Yes |
| `2-<framework>-analysis.md` | The framework's own threat analysis (own skeleton, see `skeletons/skeleton-<framework>.md`) | Yes |
| `3-findings.md` | Prioritized findings: CVSS/CWE/OWASP, tier, remediation, coverage verification | Yes |
| `inventory.yaml` | Machine-matchable index for future update runs (see `skeletons/skeleton-inventory.md`) | Yes |

**⛔ File content formatting.** `write_file` writes raw content — it is not
a chat message, so never wrap `.md` content in a ` ```markdown ` fence or
`.mmd` content in a ` ```mermaid ` fence. A file's first line is its first
real heading (`# Heading`) or, for `1.1-model.mmd`, the literal Mermaid
diagram-type keyword (`flowchart`, `graph`, `sequenceDiagram`). If you catch
yourself about to write three backticks as the first characters of a file
body, stop — that fence becomes literal text in the file, not formatting.

---

## 0.1-architecture.md

**Purpose:** grounds every later file in the real system. Written **first**,
right after the skeleton stubs for all seven files exist (SKILL.md §4.1)
and before any threat is enumerated.

```markdown
# Architecture Overview

## System Purpose
<!-- 2-4 sentences: what the system does, the problem it solves, who its users/operators are -->

## Key Components
| Component | Type | Anchor | Description |
|-----------|------|--------|-------------|
[REPEAT: one row per component]
| [FILL: name, matches diagram and analysis file] | [FILL: Process / External Interactor / Data Store] | [FILL: real file/class/manifest — SKILL.md §2.3] | [FILL: one-line role] |
[END-REPEAT]

## Component Diagram
<!-- A component/architecture-level Mermaid diagram (NOT the DFD — that is 1.1-model.mmd/1-model.md). See diagram-conventions.md for the architecture-diagram style. -->

## Top Scenarios
<!-- 3-5 of the most important workflows. The first scenario MUST include a Mermaid sequenceDiagram; the rest may use prose if a diagram adds nothing. -->

### Scenario 1: [FILL: name]
[FILL: 2-3 sentence description]
<!-- sequenceDiagram here -->

### Scenario 2: [FILL: name]
### Scenario 3: [FILL: name]

## Technology Stack
| Layer | Technologies |
|-------|--------------|
| Languages | [FILL] |
| Frameworks | [FILL] |
| Data Stores | [FILL] |
| Infrastructure | [FILL] |
| Security | [FILL] |

## Deployment Classification
<!-- One of: internet-facing / internal-network / localhost-service / local-desktop (SKILL.md §2.4). State the specific evidence: bind addresses, listeners, ingress config, ports. This classification is BINDING on every threat's prerequisite floor in 2-<framework>-analysis.md and every finding's tier/CVSS AV in 3-findings.md. -->

## Component Exposure Table
| Component | Listen Address | Auth Barrier | External Reachability | Min Prerequisite |
|-----------|-----------------|--------------|------------------------|-------------------|
[REPEAT: one row per component from Key Components]
| [FILL] | [FILL: e.g. 127.0.0.1:8080, or "no listener"] | [FILL: what gates reaching it] | [FILL: none / internal-network / external] | [FILL: one of the five fixed prerequisite values] |
[END-REPEAT]

## Security Infrastructure Inventory
| Component | Security Role | Configuration | Notes |
|-----------|----------------|----------------|-------|
[REPEAT: one row per security-relevant component found — SKILL.md §3's "inventory the security infrastructure first" step lands here]

## Repository Structure
| Directory | Purpose |
|-----------|---------|
```

**Processing rules:**

1. Derive every cell from what you actually found exploring the workspace
   (SKILL.md §2) — never speculate a component, port, or control into
   existence. If a sub-section genuinely cannot be determined, write that
   explicitly rather than leaving it blank.
2. **Component Exposure Table is the single source of truth for prerequisite
   floors.** No threat in `2-<framework>-analysis.md` and no finding in
   `3-findings.md` may carry a prerequisite below what this table permits for
   its component. A `localhost-service` classification with a component whose
   Min Prerequisite reads `None` is a contradiction — fix the table, not the
   downstream threat.
3. Every component listed here **must** reappear as a section/row in
   `2-<framework>-analysis.md` — an architecture component with no
   corresponding analysis coverage is a dropped component, not a summary
   omission.
4. The first scenario's sequence diagram must name real participants (the
   components from the table above), not generic placeholders like
   "Client"/"Server" unless those really are the component names.

**Post-write checks:**
- [ ] Every row in Key Components has a real anchor (file/class/manifest) — no `TBD`, no invented abstraction
- [ ] Deployment Classification is one of the four fixed values, stated with evidence
- [ ] Component Exposure Table has one row per Key Component, no gaps
- [ ] First scenario has a Mermaid `sequenceDiagram` block

---

## 1.1-model.mmd + 1-model.md

**Purpose:** the DFD (or the framework's equivalent structural diagram —
Trike's implementation model, VAST's process-flow/data-flow view — all use
this same two-file shape).

**Step 1 — `1.1-model.mmd`:** pure Mermaid source, no wrapper, no fence.
Every trust boundary drawn as an explicit `subgraph`, styled per
`diagram-conventions.md`. Reuse the exact component names from
`0.1-architecture.md` — a name that differs by so much as casing between
the two files is the single most common seam a reviewer will catch.

**Step 2 — `1-model.md`:**

```markdown
# Threat Model — Data Flow

## Diagram
<!-- Copy the EXACT contents of 1.1-model.mmd, wrapped in a ```mermaid fence. Copy, do not regenerate — the two must be byte-identical modulo the fence. -->

## Element Table
| Element | Type | Description | Trust Boundary |
|---------|------|-------------|-----------------|
[REPEAT: one row per element in the diagram]

## Data Flow Table
| ID | Source | Target | Protocol | Description |
|----|--------|--------|----------|-------------|
[REPEAT: DF01, DF02, ... — one row per flow in the diagram]

## Trust Boundary Table
| Boundary | Description | Contains |
|----------|-------------|----------|
[REPEAT: one row per subgraph in the diagram]
```

**Flow modeling rule:** a request/response pair between two components is
**one** bidirectional flow (`<-->` in Mermaid, one `DF##` row), not two —
splitting every interaction into a request flow and a separate response flow
inflates the flow count without adding information. Model two separate flows
only when the two directions genuinely differ in protocol or semantics (an
HTTP request versus an async push-back on a different channel).

**Post-write checks:**
- [ ] `1.1-model.mmd` and the fenced block in `1-model.md` are identical
- [ ] Every element name in the diagram matches, verbatim, the Key Components table in `0.1-architecture.md` and the element names used in `2-<framework>-analysis.md`
- [ ] Every `DF##` used in `2-<framework>-analysis.md`'s "Affected Flow" column exists in the Data Flow Table
- [ ] Every `subgraph` in the diagram has a matching row in the Trust Boundary Table

---

## 3-findings.md

**Purpose:** the prioritized, remediation-ready findings — this is what a
reader who has ten minutes reads, and the file most downstream tooling
(ticket import, a follow-up `security-audit` pass) would consume.

### Structure — organized by Exploitability Tier, not by severity

```
## Tier 1 — Direct Exposure (No Prerequisites)
## Tier 2 — Conditional Risk (Single Prerequisite)
## Tier 3 — Defense-in-Depth (Prior Compromise / Host Access)
```

All three headings are always present, even with zero findings in a tier —
write `*No Tier N findings identified for this target.*` under an empty one.
Sort findings within a tier by severity (Critical → High → Medium → Low),
then by CVSS 4.0 score descending within a severity band.

**⛔ Tier is derived, not chosen freely** — from the finding's prerequisite,
using the same mapping SKILL.md and every framework skeleton use:

| Prerequisite | Tier |
|---|---|
| `None` | Tier 1 |
| `Authenticated User` or `Internal Network` | Tier 2 |
| `Local Process` or `Host Compromise` | Tier 3 |

A finding whose CVSS vector has `AV:L` (local attack vector) or `PR:H` (high
privileges required) cannot sit in Tier 1 — if you find one there, the
prerequisite or the CVSS vector is wrong; fix whichever one is inconsistent
with the Component Exposure Table in `0.1-architecture.md`.

### Finding ID numbering

`FIND-01`, `FIND-02`, ... — sequential, in document order, no gaps, no
reuse. If you reorder findings after sorting, renumber every ID so the
document reads top-to-bottom in ID order.

### Finding attributes (all mandatory)

| Attribute | Value |
|---|---|
| Severity | `Critical` / `High` / `Medium` / `Low` |
| Exploitability Tier | `Tier 1` / `Tier 2` / `Tier 3` (derived — see table above) |
| CVSS 4.0 | score **and** full vector string, e.g. `7.1 (CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:L/VI:L/VA:N/SC:N/SI:N/SA:N)` — both mandatory, or `N/A — <why>` (see below) |
| CWE | ID + name + hyperlink, e.g. `[CWE-306](https://cwe.mitre.org/data/definitions/306.html): Missing Authentication for Critical Function` — or `N/A — <why>` |
| OWASP | see the per-framework mapping below — or `N/A — <why>` |
| Exploitation Prerequisites | one of the five fixed values |
| Remediation Effort | `Low` / `Medium` / `High` — never a time estimate (see Prohibited Content) |
| Component | the affected component, matching `0.1-architecture.md` |
| Related Threats | one hyperlink per threat ID to `2-<framework>-analysis.md#<anchor>` |

**⛔ CVSS/CWE/OWASP are mandatory where they genuinely apply, `N/A —
<one-line why>` where they don't — never silently omitted.** Applicability
varies by framework, because not every framework's threats are
CVE-shaped vulnerabilities:

- **STRIDE, NIST 800-154:** CVSS/CWE/OWASP Top 10:2025 apply directly to
  almost every finding — these are concrete technical vulnerabilities.
- **LINDDUN:** many privacy harms have no CWE (linkability from an
  over-broad data model isn't a "weakness" in the CWE sense) — write
  `CWE: N/A — privacy design issue, not a code weakness` when that's true.
  Map OWASP to the **OWASP Top 10 Privacy Risks** list (`P1`-`P10`), not the
  web-application Top 10 — e.g. `P2:2021 – Insufficient Data Breach
  Response` — since that is the standard actually built for this category
  of harm.
- **PASTA:** findings come out of stage 5 (vulnerability/weakness analysis)
  and usually do map to CVSS/CWE/OWASP directly — treat PASTA findings like
  STRIDE findings here, with the attack-simulation path (stage 6) folded
  into the Description.
- **Trike:** a finding's Severity is derived from Trike's own
  Probability × Impact, not assigned freestanding — see the mapping in
  `skeletons/skeleton-trike.md`. CVSS/CWE/OWASP still apply when the
  underlying denied-action-succeeding is a concrete technical weakness;
  write `N/A — access-control design gap, not a single CWE` when it's a
  broader permission-model issue than one weakness class captures.
- **VAST:** apply whichever companion mapping fits the model type in play
  (STRIDE-shaped for an Application Threat Model, NIST-800-154-shaped for a
  data-centric Operational one) — VAST itself doesn't mandate a taxonomy,
  so borrow the nearest one rather than leaving the fields empty.

**OWASP Top 10:2025 suffix is always `:2025`** (e.g. `A01:2025 – Broken
Access Control`) for the web-application mapping; the Privacy Risks list
uses its own `P#:2021` numbering (it has not been revised since).

### Full finding example

```markdown
### FIND-01: Missing Authentication on Internal Status Endpoint

| Attribute | Value |
|-----------|-------|
| Severity | Critical |
| Exploitability Tier | Tier 2 |
| CVSS 4.0 | 8.2 (CVSS:4.0/AV:A/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-306](https://cwe.mitre.org/data/definitions/306.html): Missing Authentication for Critical Function |
| OWASP | A07:2025 – Authentication Failures |
| Exploitation Prerequisites | Internal Network |
| Remediation Effort | Medium |
| Component | StatusServer |
| Related Threats | [T04.S](2-stride-analysis.md#statusserver), [T04.I](2-stride-analysis.md#statusserver) |

#### Description
[FILL: what the finding is and why it matters]

#### Evidence
[FILL: file:line, config key, or command output that proves this]

#### Remediation
[FILL: concrete fix]

#### Verification
[FILL: how to confirm the fix worked — a request to send, a config to check]
```

### Related Threats link format

Each threat ID is its own hyperlink: `[T04.S](2-stride-analysis.md#component-anchor)`.
**Wrong:** `T04, T07` (plain text) or `[T04, T07](2-stride-analysis.md)`
(grouped, no anchor). Anchor derivation: the target heading, lowercased,
spaces to hyphens, everything else stripped — so headings in
`2-<framework>-analysis.md` must avoid `&`, `/`, `(`, `)`, `:`, `'`, `"`,
`+`, `@`, `!` (replace `&` with `and`, `/` with `-`, drop parentheses)
or the anchor these links depend on won't resolve.

### Threat Coverage Verification table — the completeness feedback loop

At the end of `3-findings.md`:

```markdown
## Threat Coverage Verification

| Threat ID | Finding ID | Status |
|-----------|------------|--------|
| T01.S | FIND-01 | Covered |
| T01.T | FIND-03 | Mitigated (team implemented TLS) |
| T02.I | — | Mitigated by platform (Azure AD token signing) |
```

Every threat ID from `2-<framework>-analysis.md` appears exactly once here.
Status is one of exactly three values:

- **`Covered (FIND-XX)`** — the threat is an open gap; the finding documents
  it and its remediation.
- **`Mitigated (FIND-XX)`** — the threat is already handled by *this
  codebase's own* control (auth middleware the team wrote, TLS the team
  configured, file permissions the team set); the finding documents the
  existing control so the team gets credit for security work already done,
  not just gaps.
- **`Mitigated by platform`** — a system entirely outside this codebase,
  managed by a different team, handles it (cloud IdP token signing, K8s
  RBAC, hardware TPM) and it cannot be weakened by changing this repo's
  code. No finding is created for these.

**⛔ There is no fourth option.** `Accepted Risk` and `Needs Review` are
forbidden here — SKILL.md's cross-framework rule ("risk acceptance is not
yours to make") means every threat resolves to one of the three statuses
above; Trike's own owner-attributed accept decision (recorded in
`2-trike-analysis.md`, never invented here) is the sole documented exception
and still gets a `Mitigated (FIND-XX)` row citing that decision, not a
fourth status value.

**This table is a check, not documentation** — after filling it:
1. Any row with `—` in Finding ID and a status other than `Mitigated by
   platform` means a finding is missing. Create it now.
2. If more than ~20% of rows are `Mitigated by platform`, re-examine each —
   most "platform" claims turn out to be the team's own code (auth
   middleware, TLS config, localhost binding are still *this* team's work,
   not an external platform) and belong in `Mitigated (FIND-XX)` instead.
3. If any threat has a non-empty Mitigation column in
   `2-<framework>-analysis.md` and no corresponding finding, that mitigation
   text is exactly the remediation guidance for a new finding — write it.

### Prohibited content (all output files, not just findings)

Never generate: sprint/phase-based remediation roadmaps, time-to-fix
estimates (`~2 hours`, `1-2 days`), or scheduling language (`immediately`,
`within 30 days`). Remediation Effort is `Low`/`Medium`/`High` only — the
report says **what** to fix and **why**; **when** is the owning team's call.

### Post-write checks

- [ ] Finding IDs sequential, no gaps, in document order
- [ ] Every finding has Tier consistent with its Prerequisite (table above)
- [ ] No `AV:N` CVSS vector on a component whose Exposure Table row says
      `External Reachability: none`
- [ ] Every threat in `2-<framework>-analysis.md` appears in Threat Coverage
      Verification exactly once
- [ ] Zero `Accepted Risk` / `Needs Review` statuses anywhere in the file
- [ ] Related Threats cells are hyperlinks, not plain text

---

## 0-assessment.md

**Purpose:** the front page. Written **last**, after every other file is
complete, since its counts and links depend on them.

### Section order — all seven mandatory, even when a section has no data

```markdown
# Threat Model Assessment — [FILL: target]

## Report Files

| File | Description |
|------|-------------|
| [0-assessment.md](0-assessment.md) | This document — executive summary, risk rating, action plan, metadata |
| [0.1-architecture.md](0.1-architecture.md) | Architecture overview, components, scenarios, deployment classification |
| [1-model.md](1-model.md) | Data flow diagram with element, flow, and boundary tables |
| [1.1-model.mmd](1.1-model.mmd) | Raw Mermaid diagram source |
| [2-<framework>-analysis.md](2-<framework>-analysis.md) | Full <FRAMEWORK> analysis |
| [3-findings.md](3-findings.md) | Prioritized findings with remediation |

---

## Executive Summary

[FILL: framework used, and why if inferred rather than requested (SKILL.md §1). Deployment classification. Overall risk rating as plain text with no emoji: "Risk Rating: Elevated" not "Risk Rating: 🟠 Elevated".]

> **Note on threat counts:** This analysis identified [N] threats across [M] components. This reflects analysis coverage, not systemic insecurity. Of these, **[T1 count] are directly exploitable** without prerequisites (Tier 1); the remaining [T2+T3 count] are conditional risks and defense-in-depth considerations.

---

## Action Summary

| Tier | Description | Threats | Findings | Priority |
|------|-------------|---------|----------|----------|
| Tier 1 | Directly exploitable | [N] | [N] | Critical |
| Tier 2 | Requires a prerequisite | [N] | [N] | Elevated |
| Tier 3 | Requires prior compromise | [N] | [N] | Moderate |
| **Total** | | **[N]** | **[N]** | |

### Quick Wins
<!-- Tier 1 findings with Remediation Effort: Low. If none, state that plainly and, if useful, list the best-ratio Tier 2 Low-effort items instead. -->

| Finding | Title | Why quick |
|---------|-------|-----------|

---

## Analysis Context & Assumptions

### Analysis Scope
| Constraint | Description |
|------------|-------------|
| Scope | [FILL] |
| Excluded | [FILL] |

### Needs Verification
| Item | Question | What to check | Why uncertain |
|------|----------|----------------|----------------|

### Finding Overrides
| Finding ID | Original Severity | Override | Justification | New Status |
|------------|--------------------|----------|----------------|------------|
| — | — | No overrides applied. | — | — |

---

## References Consulted

### Security Standards
| Standard | URL | How Used |
|----------|-----|----------|
| CVSS 4.0 | https://www.first.org/cvss/v4.0/specification-document | Risk scoring |
| CWE | https://cwe.mitre.org/ | Weakness classification |
| OWASP Top 10:2025 | https://owasp.org/Top10/2025/ | Threat categorization (STRIDE/PASTA/NIST-800-154/VAST) |
| OWASP Top 10 Privacy Risks | https://owasp.org/www-project-top-10-privacy-risks/ | Threat categorization (LINDDUN) |
[FILL: add the chosen framework's own standard reference, e.g. "STRIDE — https://learn.microsoft.com/en-us/azure/security/develop/threat-modeling-tool-threats"]

### Component Documentation
| Component | Documentation URL | Relevant Section |
|-----------|--------------------|--------------------|

---

## Report Metadata

| Field | Value |
|-------|-------|
| Source Location | `[FILL: workspace path]` |
| Git Repository | `[FILL: git remote get-url origin, or "Unavailable"]` |
| Git Branch | `[FILL: git branch --show-current]` |
| Git Commit | `[FILL: short SHA]` |
| Analysis Started | `[FILL: UTC timestamp — shell "date -u +%Y-%m-%dT%H:%M:%SZ" at SKILL.md §4.1]` |
| Analysis Completed | `[FILL: UTC timestamp — same command, run now]` |
| Output Directory | `[FILL: the full .aegis/security/threat-model/... path]` |

---

## Classification Reference

| Term | Meaning |
|------|---------|
| Tier 1 — Direct Exposure | Exploitable with no prerequisite |
| Tier 2 — Conditional Risk | Requires one of: authenticated user, internal network access |
| Tier 3 — Defense-in-Depth | Requires local process access or host compromise |
| Deployment: internet-facing | Reachable from the public internet |
| Deployment: internal-network | Reachable only from a private/internal network |
| Deployment: localhost-service | Daemon bound to 127.0.0.1 — no remote listener |
| Deployment: local-desktop | No network listener at all |
```

**Processing rules:**

1. `0-assessment.md` is always the first row in its own Report Files table —
   it is the front page and lists itself first.
2. Counts in Executive Summary and Action Summary must match the actual
   totals in `2-<framework>-analysis.md` and `3-findings.md` — recount
   before writing, don't carry a stale number from mid-analysis.
3. `---` horizontal rule between every pair of `##` sections.
4. All Report Metadata values wrapped in backticks; any command that fails
   (no git repository, etc.) gets `Unavailable`, never a guessed value.
5. Get timestamps via the `shell` tool (`date -u +%Y-%m-%dT%H:%M:%SZ`), git
   fields via the `git` tool's `remote`/`branch`/`rev-parse` subcommands —
   never estimate either from the directory name.

**Post-write checks:**
- [ ] All seven sections present, in order, even where a table is empty
- [ ] Report Files lists `0-assessment.md` first
- [ ] Counts match `2-<framework>-analysis.md` / `3-findings.md` totals
- [ ] No emoji in the Risk Rating line
