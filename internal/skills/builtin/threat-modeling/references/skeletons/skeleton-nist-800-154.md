# Skeleton: `2-nist-800-154-analysis.md`

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md) for this run's `2-nist-800-154-analysis.md`
> — one of the seven files in the report suite; the other six
> (`0-assessment.md`, `0.1-architecture.md`, `1.1-model.mmd`, `1-model.md`,
> `3-findings.md`, `inventory.yaml`) are framework-agnostic and covered by
> `references/output-formats.md`, not here. Every heading below, in this
> order, each body replaced with `<!-- PENDING -->`. Then, section by
> section (§4.2), replace one `<!-- PENDING -->` at a time with the filled
> table shown beneath that section's heading here — copy the table shape
> verbatim too, only the `[FILL]` tokens and repeated rows change.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every attack vector): exactly one of `None`,
>   `Authenticated User`, `Internal Network`, `Local Process`,
>   `Host Compromise`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop`.
> - Data Location: exactly one of `At Rest`, `In Transit`, `In Use` — never
>   a paraphrase ("stored", "moving", "processing").
> - Control Verified?: exactly `yes` or `no — gap` — never "partially" or
>   "should be" (a partial control is `no — gap` plus a note in the Control
>   cell explaining what's missing).
> - Attack-vector ID prefix: `N` (`N1`, `N2`, …) — never `AV-1`, `NIST-1`,
>   or a per-location counter that restarts.
> - Tier is **derived from Prerequisite**, never chosen independently:
>   `None` → Tier 1, `Authenticated User`/`Internal Network` → Tier 2,
>   `Local Process`/`Host Compromise` → Tier 3 (same mapping every framework
>   uses — `output-formats.md`).

---

## Skeleton (initial, before any analysis)

```markdown
# NIST SP 800-154 Data-Centric Analysis — [FILL: target]

> Framework: NIST SP 800-154 — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of the four fixed values above]
> Date: [FILL: YYYY-MM-DD]
> Sensitive Data Type(s) in Scope: [FILL: the specific dataset(s) this run covers — never "all data"]

## Data Inventory

<!-- PENDING -->

## Summary

<!-- PENDING -->

## Attack Vector Analysis

<!-- PENDING -->
```

## Fill-in shape per section

### Data Inventory

Step 1 of the methodology (`references/nist-800-154.md`): trace every place
the in-scope dataset exists, grounded in the real code/config/schema — this
has no equivalent in `0.1-architecture.md`, so it lives only here.

```markdown
| Data Type | Location | Component | Data Location | Evidence |
|-----------|----------|-----------|-----------------|----------|
[REPEAT: one row per place the in-scope data exists]
| [FILL: the specific dataset, e.g. "user session tokens"] | [FILL: e.g. "Redis cache", "access log", "in-memory during auth handler"] | [FILL: component name, matches 0.1-architecture.md exactly] | [FILL: At Rest / In Transit / In Use] | [FILL: file/config/schema path that proves this location is real] |
[END-REPEAT]
```

**⛔ Columns are exactly these five, in this order.** `Component` must
match a name in `0.1-architecture.md`'s Key Components table — a data
location tied to a component that doesn't exist there is either a missed
architecture component (go add it) or an invented location (delete the
row).

<!-- ⛔ POST-TABLE CHECK: every row's Data Location is one of the three
fixed values, and every row's Component matches 0.1-architecture.md
verbatim (same casing). If either fails, fix the row now — this table is
what Attack Vector Analysis enumerates against, so an error here propagates
into every downstream row. -->

### Summary

Placed immediately after Data Inventory, before Attack Vector Analysis —
this is a navigation aid for the reader, not a recap written at the end.

```markdown
| ID | Data Location | Attack Vector | Tier | Severity | Control Status |
|----|-----------------|----------------|------|----------|------------------|
[REPEAT: one row per attack vector in Attack Vector Analysis, filled in after that section is written]
| [FILL: N##] | [FILL] | [FILL: one-line vector name] | [FILL: 1/2/3] | [FILL] | [FILL: yes / no — gap] |
[END-REPEAT]
```

<!-- ⛔ POST-TABLE CHECK: this table's row count equals the Attack Vector
Analysis table's row count, and every ID/Tier/Severity/Control Status cell
matches its counterpart there exactly — this is a summary view of the same
data, not a second, independently-derived one. If the two disagree, one of
them is stale; recount from Attack Vector Analysis and fix this table. -->

### Attack Vector Analysis

Steps 2-4 of the methodology combined into one table: the plausible attack
vector per (location, state) pair, its prerequisite/tier, the control that
addresses it, whether that control is actually verified, and the resulting
severity.

```markdown
| ID | Data Location | Attack Vector | Evidence | Prerequisite | Tier | Control | Control Verified? | Residual risk | Severity |
|----|-----------------|----------------|----------|---------------|------|---------|----------------------|-----------------|----------|
[REPEAT: one row per (data-location, attack-vector) pair from the Data Inventory locations]
| [FILL: N##] | [FILL: At Rest / In Transit / In Use, matches a Data Inventory row] | [FILL: plausible vector for this specific state — see the per-state list in references/nist-800-154.md] | [FILL: file/config/code path] | [FILL: one of the five fixed prerequisite values] | [FILL: 1/2/3, derived from Prerequisite] | [FILL: the specific control — encryption/access control/DLP/tokenization/audit logging/contractual limit] | [FILL: yes, or "no — gap"] | [FILL] | [FILL: one of the four fixed severity values] |
[END-REPEAT]
```

**⛔ Columns are exactly these ten, in this order.** Not every attack
vector applies to every data location — select the ones plausible for each
specific `(location, state)` pair (per-state guidance in
`references/nist-800-154.md`) rather than repeating a fixed list at every
row; if a location has genuinely no plausible vector beyond what's already
listed, still write one row with
`Attack Vector: none identified — [FILL: one-line why]` rather than
omitting the location entirely — an explicit empty finding is checkable,
a missing row looks like the location was never considered.

**`Control Verified?` is `yes` only when the control was actually checked
against real code/config** — cite what was checked in Evidence, not
assumed present because it "should" be there. A `no — gap` row is, by
definition, an open finding: every one of them must appear in
`3-findings.md`.

<!-- ⛔ POST-TABLE CHECK: run before moving to Summary —
  1. Every row's Data Location matches a location that actually exists in
     the Data Inventory table above — no attack vector invented against a
     location never traced to real code/config.
  2. Every Data Inventory row has at least one Attack Vector Analysis row
     (or an explicit "no plausible vector — why" row) — a location with
     zero rows here means it was never considered for attack, not that it's
     safe.
  3. Every row's Tier matches its Prerequisite via the fixed mapping (`None`
     → 1, `Authenticated User`/`Internal Network` → 2, `Local Process`/
     `Host Compromise` → 3) — a Tier that disagrees with its own row's
     Prerequisite is a defect, not a judgment call.
  4. No row's Prerequisite sits below the Deployment classification's floor
     from SKILL.md §2 (a `localhost-service` system's floor is
     `Local Process` at minimum).
  5. Attack-vector IDs are sequential (`N1`, `N2`, …) with no gaps or reuse.
  If any check fails, fix the table now before writing the Summary. -->

---

Every `no — gap` row here must appear in `3-findings.md`'s Threat Coverage
Verification table (`output-formats.md`) — a gap identified here with no
corresponding finding is a dropped finding, not an implicit "handled
elsewhere". This framework is explicitly iterative (Step 4 of the source
methodology): note in `0-assessment.md`'s Analysis Context & Assumptions
what would trigger a re-run — a new data location the dataset starts
flowing through, a new third-party integration, or a control that was
`yes` here becoming disabled later.
