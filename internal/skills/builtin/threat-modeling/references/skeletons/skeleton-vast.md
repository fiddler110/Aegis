# Skeleton: VAST threat model document

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order, each
> body replaced with `<!-- PENDING -->`. Then, section by section (§4.2),
> replace one `<!-- PENDING -->` at a time with the filled content shown
> beneath that section's heading here.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Model type: exactly `Application` or `Operational` (§ "Two model
>   types" in `references/vast.md`) — pick one per run; don't blend both
>   into a single document.
> - Threat ID prefix: `V` (`V1`, `V2`, …).

---

## Skeleton (initial, before any analysis)

```markdown
# VAST Threat Model — [FILL: feature/service name] ([FILL: Application or Operational])

> Framework: VAST — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of internet-facing/internal-network/localhost-service/local-desktop]
> Date: [FILL: YYYY-MM-DD]

## Scope

<!-- PENDING -->

## Diagram

<!-- PENDING -->

## Threats (backlog-shaped)

<!-- PENDING -->

## Enumeration method used

<!-- PENDING -->

## Summary

<!-- PENDING -->
```

## Fill-in shape per section

### Scope

Prose: the single feature/service (Application model) or infra boundary
(Operational model) this model covers, and why that scope — not the
whole enterprise. VAST's scaling premise depends on staying small enough
for one team to own; if the ask is actually enterprise-wide, that's a
signal to reconsider the framework choice (say so) rather than force a
giant VAST document.

### Diagram

Process-flow diagram (Application model: how the feature works, screen by
screen / call by call) or data-flow/infra diagram (Operational model:
what's deployed, attacker-centric). Match the model type chosen in the
title above — do not draw a generic architecture diagram that ignores
which of the two model types was picked.

### Threats (backlog-shaped)

```markdown
| ID | Title | Description | Evidence | Severity | Acceptance criteria for mitigation |
|----|-------|--------------|----------|----------|--------------------------------------|
[REPEAT: one row per threat found via the enumeration method below]
| [FILL: V##] | [FILL: short, ticket-ready title] | [FILL] | [FILL: file/config making it real] | [FILL: one of the four fixed severity values] | [FILL: what "fixed" looks like — a testable statement, not "improve security"] |
[END-REPEAT]
```

**⛔ Columns are exactly these six, in this order.** `Title` must be
short enough to paste directly into an issue tracker as-is (VAST's
defining trait: each row is a ready-to-file ticket, not a document
paragraph needing translation later). `Acceptance criteria` must be
checkable ("rejects requests without a valid session token"), never a
vague goal ("more secure").

<!-- ⛔ POST-TABLE CHECK: run before writing Enumeration method —
  1. Every row's Title reads like a ticket summary, not a sentence
     fragment lifted from the Description.
  2. Every row's Acceptance criteria is a testable statement.
  3. Threat IDs are sequential (`V1`, `V2`, …) with no gaps or reuse.
  If any check fails, fix the table now. -->

### Enumeration method used

One line stating the method (e.g. "STRIDE categories per diagram
element" — the default per `references/vast.md`, unless something more
specific to the system fits better). VAST doesn't mandate a taxonomy, so
this line is what makes the Threats table's coverage auditable — a reader
must be able to tell what "done" meant for this pass.

### Summary

Count by severity, plus an explicit note that this model should be
revisited when the scoped feature/infra changes materially (VAST assumes
iteration, not a one-time document — the inventory sidecar from
`references/verification-and-updates.md` is what makes that revisit
cheap).

<!-- ⛔ POST-SECTION CHECK: the Summary's total count equals the number of
rows in the Threats table. -->
