# Skeleton: LINDDUN threat model document

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order, each
> body replaced with `<!-- PENDING -->`. Then, section by section (§4.2),
> replace one `<!-- PENDING -->` at a time with the filled table shown
> beneath that section's heading here.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every threat): exactly one of `None`, `Authenticated User`,
>   `Internal Network`, `Local Process`, `Host Compromise`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop`.
> - Threat ID prefix: `P` (`P1`, `P2`, …) — never `L-1`, `T1` (that's
>   STRIDE's prefix), or a per-category counter.
> - Category names are the seven from `references/linddun.md` exactly:
>   `Linkability`, `Identifiability`, `Non-repudiation`, `Detectability`,
>   `Disclosure of information`, `Unawareness`, `Non-compliance`.

---

## Skeleton (initial, before any analysis)

```markdown
# LINDDUN Privacy Threat Model — [FILL: system/feature name]

> Framework: LINDDUN — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of the four fixed values above]
> Date: [FILL: YYYY-MM-DD]

## Scope

<!-- PENDING -->

## Personal data inventory

<!-- PENDING -->

## Data-flow diagram

<!-- PENDING -->

## Threats

<!-- PENDING -->

## Summary

<!-- PENDING -->
```

## Fill-in shape per section

### Scope

Prose: which data flows/stores carry personal data and were modeled;
what's explicitly out of scope (flows carrying no personal data at all).

### Personal data inventory

```markdown
| Data category | Source | Purpose of collection | Retention | Legal basis |
|----------------|--------|------------------------|-----------|--------------|
[REPEAT: one row per distinct category of personal data found]
| [FILL] | [FILL: where it enters the system] | [FILL] | [FILL: duration, or "unbounded — flag as gap"] | [FILL: if known, else "not documented — flag as gap"] |
[END-REPEAT]
```

**⛔ Columns are exactly these five, in this order.** `Retention` of
"unbounded"/"not documented" and `Legal basis` of "not documented" are
themselves findings — carry them into the Threats table under
Non-compliance, don't just note them here and drop them.

### Data-flow diagram

Same as STRIDE: Mermaid/PlantUML, trust boundaries drawn explicitly,
personal-data-carrying flows visually distinguished (e.g. a distinct edge
style or label), element names matching the Threats table verbatim.

<!-- ⛔ POST-DIAGRAM CHECK: every personal-data flow named in the Personal
data inventory above appears in this diagram, and every element name here
matches the Threats table's Element column verbatim. -->

### Threats

```markdown
| ID | Element | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|---------|----------|--------|----------|--------------|------------|----------------|----------|
[REPEAT: one row per (personal-data element, applicable LINDDUN category) pair]
| [FILL: P##] | [FILL: element name, matches diagram] | [FILL: Linkability/Identifiability/Non-repudiation/Detectability/Disclosure of information/Unawareness/Non-compliance] | [FILL] | [FILL: code/config/schema showing the data handling] | [FILL: one of the five fixed prerequisite values] | [FILL: control — technical or procedural — or "none — open finding"] | [FILL] | [FILL: one of the four fixed severity values] |
[END-REPEAT]
```

**⛔ Columns are exactly these nine, in this order** — same shape as
STRIDE's Threats table, category values swapped for LINDDUN's seven.
`Non-repudiation` here is a privacy *harm* (a subject unable to deny an
action) — the mirror image of STRIDE's Repudiation, not a duplicate row of
it.

**No missing cells.** Every personal-data element needs a row per
applicable category, or an explicit
`Threat: none identified — [FILL: one-line why]`.

<!-- ⛔ POST-TABLE CHECK: run before moving to Summary —
  1. Every row's Prerequisite is one of the five fixed values.
  2. No row's Prerequisite sits below the Deployment classification's
     floor.
  3. Every row has a Severity from the four fixed values.
  4. Threat IDs are sequential (`P1`, `P2`, …) with no gaps or reuse.
  5. A disclosure that's technically authorized (valid credential/API key)
     but exceeds the data subject's consent scope is still present as a
     row — access control alone does not close a LINDDUN finding.
  If any check fails, fix the table now before writing the Summary. -->

### Summary

Count of threats by category and by severity, plus any consent/purpose-
scope gaps found (recorded here even if not turned into a full Threats
row, e.g. an undocumented legal basis from the Personal data inventory).

<!-- ⛔ POST-SECTION CHECK: the Summary's total count equals the number of
rows in the Threats table. -->
