# Skeleton: STRIDE threat model document

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order, each
> body replaced with `<!-- PENDING -->`. Then, section by section (§4.2),
> replace one `<!-- PENDING -->` at a time with the filled table shown
> beneath that section's heading here — copy the table shape verbatim too,
> only the `[FILL]` tokens and repeated rows change.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every threat): exactly one of `None`, `Authenticated User`,
>   `Internal Network`, `Local Process`, `Host Compromise`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop`.
> - Threat ID prefix: `T` (`T1`, `T2`, …) — never `TH`, `S-1`, or a
>   per-category counter.

---

## Skeleton (initial, before any analysis)

```markdown
# STRIDE Threat Model — [FILL: system/feature name]

> Framework: STRIDE — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of the four fixed values above]
> Date: [FILL: YYYY-MM-DD]

## Scope

<!-- PENDING -->

## Assets

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

State what was modeled and what was explicitly excluded, in prose — no
table, no `[FILL]` structure beyond ordinary prose.

### Assets

A bullet list: `- [FILL: asset] — [FILL: why it matters]`. Never leave this
as prose paragraphs; one bullet per asset keeps it checkable against the
inventory sidecar.

### Data-flow diagram

A Mermaid or PlantUML diagram with every trust boundary drawn explicitly
(a boundary that exists only in prose doesn't count). Every element in the
diagram must reuse the exact same name it will have in the Threats table
below — no synonyms between the picture and the table.

<!-- ⛔ POST-DIAGRAM CHECK: every element name in this diagram appears
verbatim (same casing, same spelling) in the Threats table's Element
column. If a name differs, fix the table or the diagram now — do not
leave two spellings of the same component. -->

### Threats

```markdown
| ID | Element | Category | Threat | Evidence | Prerequisite | Mitigation | Residual risk | Severity |
|----|---------|----------|--------|----------|--------------|------------|----------------|----------|
[REPEAT: one row per (element, applicable STRIDE category) pair]
| [FILL: T##] | [FILL: element name, matches diagram] | [FILL: Spoofing/Tampering/Repudiation/Information Disclosure/Denial of Service/Elevation of Privilege — add Abuse only if STRIDE-A was chosen] | [FILL] | [FILL: file/config/code path] | [FILL: one of the five fixed prerequisite values] | [FILL: control, or "none — open finding"] | [FILL] | [FILL: one of the four fixed severity values] |
[END-REPEAT]
```

**⛔ Columns are exactly these nine, in this order.** Do not rename
`Element` to `Component`, `Threat` to `Description`, or drop the
`Residual risk` column because a row's mitigation looks complete.

**No missing cells.** Every applicable (element, category) pair from
`references/stride.md`'s per-element-type rule needs a row — if no
concrete threat exists, still write the row with
`Threat: none identified — [FILL: one-line why]` rather than omitting it.

<!-- ⛔ POST-TABLE CHECK: run before moving to Summary —
  1. Every row's Prerequisite is one of the five fixed values (not "N/A",
     not "attacker", not a free-text sentence).
  2. No row's Prerequisite sits below the Deployment classification's
     floor (a `localhost-service` system cannot have a `None`-prerequisite
     threat — the floor is `Local Process` at minimum).
  3. Every row has a Severity from the four fixed values.
  4. Threat IDs are sequential (`T1`, `T2`, …) with no gaps or reuse.
  5. Every "Evidence" cell names an actual file/config path, not "assumed"
     or "likely".
  If any check fails, fix the table now before writing the Summary. -->

### Summary

Prose or a short bullet list: count of threats by category and by
severity, and which ones (if any) remain unmitigated open findings. State
the count as a sanity-checkable number (e.g. "9 threats: 2 Critical
(open), 4 High (3 mitigated, 1 open), 3 Medium (mitigated)") — a summary
that doesn't add up to the Threats table's row count is itself a defect.

<!-- ⛔ POST-SECTION CHECK: the Summary's total count equals the number of
rows in the Threats table. If they disagree, a row was added or dropped
after the summary was written — recount and fix. -->
