# Skeleton: `2-linddun-analysis.md`

> This is one file in the seven-file report suite (`output-formats.md`) —
> it covers only LINDDUN's own privacy analysis. Scope, the personal-data
> inventory that bounds this file, and the DFD live here because they are
> LINDDUN-specific scoping decisions; the system's components, architecture,
> and the DFD *diagram* itself live in `0.1-architecture.md` and
> `1-model.md`/`1.1-model.mmd` — reuse those exact component names, don't
> re-derive or rename them here.
>
> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, SKILL.md §4.1): every heading, in this order, each body
> replaced with `<!-- PENDING -->`. Then, section by section (§4.2), replace
> one `<!-- PENDING -->` at a time with the filled table shown beneath that
> section's heading here — copy the table shape verbatim too, only the
> `[FILL]` tokens and repeated rows change.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every threat): exactly one of `None`, `Authenticated User`,
>   `Internal Network`, `Local Process`, `Host Compromise`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Threat ID prefix: `L` (`L1`, `L2`, …) — never `P-1` (PASTA's threat
>   prefix), `T1` (STRIDE's), or a per-category counter. (`L` for
>   *LINDDUN*/*Linkability* — chosen to avoid colliding with any other
>   framework's prefix: PASTA uses `P` for threats and `AP` for attack paths,
>   Trike `R`, VAST `V`, NIST 800-154 `N`.)
> - Category names are the seven from `references/linddun.md` exactly:
>   `Linkability`, `Identifiability`, `Non-repudiation`, `Detectability`,
>   `Disclosure of information`, `Unawareness`, `Non-compliance`.
> - Tier is derived from Prerequisite, never assigned independently:
>   `None`→Tier 1, `Authenticated User`/`Internal Network`→Tier 2,
>   `Local Process`/`Host Compromise`→Tier 3 (`output-formats.md`).

---

## Skeleton (initial, before any analysis)

```markdown
# LINDDUN Privacy Threat Analysis — [FILL: target]

## Personal Data Inventory

<!-- PENDING -->

## In-Scope Elements

<!-- PENDING -->

## Summary

<!-- PENDING -->

---

[ONE SECTION PER IN-SCOPE COMPONENT]
```

## Fill-in shape per section

### Personal Data Inventory

```markdown
| Data category | Source | Purpose of collection | Retention | Legal basis |
|----------------|--------|------------------------|-----------|--------------|
[REPEAT: one row per distinct category of personal data found]
| [FILL] | [FILL: where it enters the system — matches a component/flow in 0.1-architecture.md / 1-model.md] | [FILL] | [FILL: duration, or "unbounded — flag as gap"] | [FILL: if known, else "not documented — flag as gap"] |
[END-REPEAT]
```

**⛔ Columns are exactly these five, in this order.** A `Retention` of
"unbounded" or a `Legal basis` of "not documented" is itself a finding —
carry it forward into a component's Non-compliance row below; don't just
note it here and drop it.

<!-- ⛔ POST-TABLE CHECK: every data category here traces to a real
component/flow name from 0.1-architecture.md or 1-model.md's Element/Data
Flow tables — no category sourced from an element that doesn't exist in
those files. -->

### In-Scope Elements

Prose or a short list: which components/flows from `0.1-architecture.md`
and `1-model.md` carry personal data (per the inventory above) and
therefore get a full LINDDUN section below, and which were explicitly
scoped **out** because they carry no personal data at all — one line each
for the excluded ones, e.g. "`MetricsCollector` — aggregate counters only,
no per-subject data."

<!-- ⛔ POST-SECTION CHECK: every component/flow in this list is a real
name from 0.1-architecture.md's Key Components table or 1-model.md's
Element Table — no invented element, no name that differs by casing or
wording from those files. -->

### Summary

Table first, before any component section — LINDDUN has two category
letters that repeat (`D` for both Detectability and Disclosure of
information, `N` for both Non-repudiation and Non-compliance), so the
summary header disambiguates with short sub-labels:

```markdown
| Component | L | I | N-Rep | D-Det | D-Disc | U | N-Comp | Total | Tier 1 | Tier 2 | Tier 3 |
|-----------|---|---|-------|-------|--------|---|--------|-------|--------|--------|--------|
[REPEAT: one row per in-scope component]
| [FILL] | [FILL: count] | [FILL: count] | [FILL: count] | [FILL: count] | [FILL: count] | [FILL: count] | [FILL: count] | [FILL: sum] | [FILL: count] | [FILL: count] | [FILL: count] |
[END-REPEAT]
| **Total** | | | | | | | | **[FILL]** | **[FILL]** | **[FILL]** | **[FILL]** |
```

<!-- ⛔ POST-TABLE CHECK: for every row, the seven category counts sum to
Total, and Tier 1 + Tier 2 + Tier 3 also sum to Total. Row counts, once the
component sections below are written, must match the actual number of
threat rows for that component. Fix any mismatch before moving on — this
table is a self-check, not decoration. -->

### Per-component sections

One `## <Component Name>` section per in-scope component, using the exact
name from `0.1-architecture.md`. **Anchor-safe headings** — letters,
numbers, spaces, and hyphens only (no `&`, `/`, `(`, `)`, `:`, `'`, `"`)
— so `3-findings.md` can link `[L4](2-linddun-analysis.md#component-name)`.

Each component section splits into three tier sub-sections, **all three
always present** even when empty:

```markdown
## <Component Name>

### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|----------------|----------|
[REPEAT: one row per Tier 1 threat, or a single row "*No Tier 1 threats identified.*" spanning the table if none]

### Tier 2 — Conditional Risk
| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|----------------|----------|

### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|--------------|------------|----------------|----------|
```

**⛔ Columns are exactly these eight, in this order** — same shape STRIDE
uses, category values swapped for LINDDUN's seven.
`Non-repudiation` here is a privacy *harm* (a subject unable to deny an
action when they may need that deniability) — the mirror image of
STRIDE's Repudiation, never a duplicate of it.

**No missing cells.** Every in-scope component needs a row per applicable
category across its three tiers, or an explicit
`Threat: none identified — [FILL: one-line why]` row in the appropriate
tier. Every row's tier must match its Prerequisite via the fixed mapping
above — a `None`-prerequisite row does not belong under Tier 2 or Tier 3.

A disclosure that's technically *authorized* (the recipient holds a valid
API key or role) but exceeds the data subject's consent scope or the
system's stated collection purpose is still a row here — access control
alone does not close a LINDDUN finding; that is the dimension STRIDE has
no equivalent for (`references/linddun.md` §3).

<!-- ⛔ POST-TABLE CHECK: run after each component's three tier tables —
  1. Every row's Prerequisite is one of the five fixed values.
  2. Every row's Tier matches its Prerequisite (None→1, Authenticated
     User/Internal Network→2, Local Process/Host Compromise→3) and neither
     sits below the component's floor in 0.1-architecture.md's Component
     Exposure Table.
  3. Every row has a Severity from the four fixed values.
  4. Threat IDs are sequential (`L1`, `L2`, …) across the whole file, no
     gaps or reuse between components.
  5. Every "Evidence" cell names an actual file/config/schema path, not
     "assumed" or "likely".
  6. This component's row in the Summary table (category counts, Total,
     Tier 1/2/3 counts) matches the rows actually written here — if the
     analysis grew or shrank since the Summary was drafted, fix the
     Summary now, not at the final review round.
  If any check fails, fix the section now before moving to the next
  component. -->

---

Every threat ID (`L1`, `L2`, …) written here must appear exactly once in
`3-findings.md`'s Threat Coverage Verification table (`output-formats.md`)
— either as a `Covered`/`Mitigated` finding or a `Mitigated by platform`
row. A threat with a non-empty Mitigation cell above is exactly the
remediation text that finding needs; don't leave it stranded in this file
with no corresponding finding.
