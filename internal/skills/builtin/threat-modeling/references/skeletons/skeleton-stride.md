# Skeleton: `2-stride-analysis.md`

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order, each
> body replaced with `<!-- PENDING -->`. Then, section by section (§4.2),
> replace one `<!-- PENDING -->` at a time with the filled table shown
> beneath that section's heading here — copy the table shape verbatim too,
> only the `[FILL]` tokens and repeated rows change.
>
> This skeleton produces **one file** in the seven-file suite. Scope,
> assets, and the data-flow diagram live in `0.1-architecture.md` and
> `1.1-model.mmd`/`1-model.md` instead (`references/output-formats.md`) —
> don't recreate them here.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every threat): exactly one of `None`, `Authenticated User`,
>   `Internal Network`, `Local Process`, `Host Compromise`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop`.
> - Threat ID prefix: `T` (`T1`, `T2`, …), sequential across the **whole
>   file** — never restart per component or per tier, and never `TH`,
>   `S-1`, or a per-category counter. `3-findings.md` links to these IDs
>   (`output-formats.md`), so a renumber after the fact means updating every
>   link too — get the numbering right the first pass.
> - **Tier is derived from Prerequisite, never assigned independently:**
>   `None` → Tier 1; `Authenticated User` or `Internal Network` → Tier 2;
>   `Local Process` or `Host Compromise` → Tier 3. This is the same mapping
>   `output-formats.md` uses for `3-findings.md` — keep them consistent.

---

## Skeleton (initial, before any analysis)

```markdown
# STRIDE Threat Analysis — [FILL: target]

> Framework: STRIDE — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of the four fixed values above — must match 0.1-architecture.md]
> Date: [FILL: YYYY-MM-DD]

## Summary

<!-- PENDING -->

## [FILL: Component 1 name]

<!-- PENDING -->

## [FILL: Component 2 name]

<!-- PENDING -->
```

Repeat the `## <Component Name>` heading for every component listed in
`0.1-architecture.md`'s Key Components table — same names, same casing, no
synonyms. An architecture component with no matching section here is a
dropped component, not an acceptable omission (`output-formats.md`).

If the ask calls for STRIDE-A (SKILL.md's optional seventh category — see
`references/stride.md`), title this file's `#` heading
`# STRIDE-A Threat Analysis — [FILL: target]` instead, and include `A` rows
wherever a process exposes an abusable legitimate feature. Plain
authorization failures still belong under `E`, never `A`.

## Fill-in shape per section

### Summary

**Write this section last**, once every component section below is
complete — but it stays physically first in the file, immediately after the
title block and before any `## <Component Name>` section. A reader opens
this file for the totals; making them scroll past every component to find
it is the one thing this section exists to prevent.

```markdown
| Component | S | T | R | I | D | E | Total | Tier 1 | Tier 2 | Tier 3 |
|-----------|---|---|---|---|---|---|-------|--------|--------|--------|
[REPEAT: one row per component, in the same order as the sections below]
| [FILL] | [FILL] | [FILL] | [FILL] | [FILL] | [FILL] | [FILL: sum] | [FILL] | [FILL] | [FILL] |
[END-REPEAT]
| **Total** | [FILL] | [FILL] | [FILL] | [FILL] | [FILL] | [FILL] | **[FILL]** | **[FILL]** | **[FILL]** | **[FILL]** |
```

Add an `A` column between `E` and `Total` when this is a STRIDE-A run. Each
category cell is a **count of concrete threat rows**, not a checkmark —
`none identified` rows don't count toward it. `Tier 1`/`Tier 2`/`Tier 3`
columns must sum to that row's `Total`, and `S+T+R+I+D+E(+A)` must also sum
to `Total` — two different partitions of the same count, so they must
agree.

<!-- ⛔ POST-SUMMARY CHECK: for every component row, Tier 1 + Tier 2 + Tier 3
= Total, and the per-category counts also sum to Total. The Totals row is
the column-wise sum of every component row above it. If either check fails,
recount the component's threat table below rather than adjusting the
Summary numbers to fit — the table is the source of truth. -->

### Per component

```markdown
**Trust Boundary:** [FILL: boundary name from 1-model.md's Trust Boundary Table]
**Data Flows:** [FILL: DF## ids from 1-model.md's Data Flow Table this component participates in]

#### Tier 1 — Direct Exposure (No Prerequisites)

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|---------------|------------|-----------------|----------|
[REPEAT: one row per applicable (category, Tier 1) pair — omit the row entirely if this component has zero Tier 1 threats and write the line below instead]

*No Tier 1 threats identified for this component.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|---------------|------------|-----------------|----------|
[REPEAT: same shape, Tier 2 rows]

*No Tier 2 threats identified for this component.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|----------|--------|----------|---------------|------------|-----------------|----------|
[REPEAT: same shape, Tier 3 rows]

*No Tier 3 threats identified for this component.*
```

**All three tier sub-sections are always present**, in this order, even
when a tier is empty for this component — write the italic "*No Tier N
threats identified...*" line in place of the table, never omit the
heading itself.

**⛔ Columns are exactly these eight, in this order.** Do not rename
`Category` to `Type`, drop `Residual risk` because a row's mitigation looks
complete, or fold `Evidence` into the `Threat` cell.

**No missing cells.** Every applicable (component, category) pair from
`references/stride.md`'s per-element-type rule needs a row somewhere across
this component's three tiers — if no concrete threat exists, still write
the row with `Threat: none identified — [FILL: one-line why]` and whatever
Tier its own would-be prerequisite implies, rather than omitting it
entirely.

**Component headings must be anchor-safe** — `3-findings.md` links to them
as `[T04.S](2-stride-analysis.md#component-name)`. Use only letters,
numbers, spaces, and hyphens in the `## <Component Name>` heading; never
`&`, `/`, `(`, `)`, `:`, `'`, `"`, `+`, `@`, `!` (replace `&` with `and`,
`/` with `-`, drop parentheses).

<!-- ⛔ POST-TABLE CHECK: run before moving to the next component —
  1. Every row's Prerequisite is one of the five fixed values (not "N/A",
     not "attacker", not a free-text sentence).
  2. Every row sits under the Tier its Prerequisite maps to (None→Tier 1,
     Authenticated User/Internal Network→Tier 2, Local Process/Host
     Compromise→Tier 3) — a Tier 1 sub-section containing a row whose
     Prerequisite is "Authenticated User" is misfiled; move it to Tier 2.
  3. No row's Prerequisite sits below the Deployment classification's floor
     from 0.1-architecture.md's Component Exposure Table (a
     `localhost-service` component cannot have a `None`-prerequisite
     threat — the floor is `Local Process` at minimum).
  4. Every row has a Severity from the four fixed values.
  5. Threat IDs are sequential (`T1`, `T2`, …) across the whole file, no
     gaps or reuse between components.
  6. Every "Evidence" cell names an actual file/config path, not "assumed"
     or "likely".
  If any check fails, fix the table now before moving to the next
  component. -->

## Handing off to findings

Every threat row with a non-empty Mitigation column (open or already
mitigated by this codebase's own controls) becomes a finding in
`3-findings.md`, carrying this file's Tier forward plus CVSS 4.0, CWE, and
OWASP Top 10:2025 (`output-formats.md`'s mandatory-fields table — STRIDE
threats are concrete technical vulnerabilities, so these apply directly to
almost every finding). Every threat ID from this file must appear exactly
once in `3-findings.md`'s Threat Coverage Verification table before the run
is done — that check happens there, not in this file, but it depends on
every `T##` here being real and evidenced.
