# Skeleton: `2-vast-analysis.md`

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order, each
> body replaced with `<!-- PENDING -->`. Then, section by section (§4.2),
> replace one `<!-- PENDING -->` at a time with the filled content shown
> beneath that section's heading here. This skeleton produces **only**
> `2-vast-analysis.md` — scope, the process-flow/DFD diagram, and
> deployment classification live in `0.1-architecture.md` and
> `1-model.md`/`1.1-model.mmd` per `references/output-formats.md`; don't
> re-derive them here, just cite them.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every backlog item): exactly one of `None`,
>   `Authenticated User`, `Internal Network`, `Local Process`,
>   `Host Compromise`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low`.
> - Tier (derived from Prerequisite, never assigned independently):
>   `None` → **Tier 1**; `Authenticated User` or `Internal Network` →
>   **Tier 2**; `Local Process` or `Host Compromise` → **Tier 3**.
> - Model type: exactly `Application Threat Model` or
>   `Operational Threat Model` (`references/vast.md` "Two model types") —
>   pick one per run; don't blend both into a single file.
> - Threat ID prefix: `V` (`V1`, `V2`, …) — never `TH`, `BL-1`, or a
>   per-severity counter.
>
> **Reminder from `references/vast.md`:** scope to one feature/service
> (Application) or one infra boundary (Operational) — VAST's scaling
> premise depends on staying small enough for one team to own. If the ask
> is genuinely enterprise-wide, say so and reconsider the framework choice
> rather than forcing one giant VAST file.

---

## Skeleton (initial, before any analysis)

```markdown
# VAST Analysis — [FILL: feature/service or infra boundary name]

> Model Type: [FILL: Application Threat Model or Operational Threat Model]
> Enumeration method: [FILL: e.g. "STRIDE categories per diagram element" — the default per references/vast.md — or another method, and why]
> CVSS/CWE/OWASP mapping borrowed: [FILL: STRIDE mapping (Application) or NIST-800-154 mapping (Operational) — see references/vast.md]

## Summary

<!-- PENDING -->

## Backlog Items

<!-- PENDING -->
```

## Fill-in shape per section

### Summary

```markdown
| ID | Title | Severity | Tier | Ticket-ready |
|----|-------|----------|------|---------------|
[REPEAT: one row per backlog item below, same order]
| [FILL: V##] | [FILL: matches the item's heading title] | [FILL: one of the four fixed severity values] | [FILL: Tier 1/2/3 — derived from Prerequisite] | [FILL: Yes, or "Needs refinement — <why>" if the acceptance criteria aren't concretely testable yet] |
[END-REPEAT]
| **Total** | | | | |
```

Placed at the top, before the Backlog Items, so a reader (or a ticket-import
script) can scan the whole set without opening every item.

### Backlog Items

One `### V##: <title>` block per threat — the primary content of this file.
**Anchor-safe headings**: letters, numbers, spaces, and hyphens only, since
`3-findings.md` links here as `[V04](2-vast-analysis.md#anchor)` — no `&`,
`/`, `(`, `)`, `:`, `'`, `"`, `+`, `@`, `!` in the title.

```markdown
### V1: [FILL: short, ticket-ready title — pastes directly into an issue tracker]

| Severity | Tier | Prerequisite | Evidence | Acceptance Criteria |
|----------|------|--------------|----------|-----------------------|
| [FILL: one of the four fixed severity values] | [FILL: Tier 1/2/3] | [FILL: one of the five fixed prerequisite values] | [FILL: file/config/code path making this real] | [FILL: the single most important testable criterion] |

[FILL: one-paragraph description — what the threat is, in the context of the borrowed enumeration method's category]

**Acceptance criteria for mitigation:**
- [FILL: testable statement, e.g. "rejects requests without a valid session token" — never a vague goal like "improve security"]
- [FILL: additional testable statement, if the mitigation has more than one condition]
```

**⛔ Columns are exactly these five, in this order**, and `Acceptance
Criteria` (both the table cell and the bullet list below it) must be
checkable statements, never restated threats ("fix the missing auth check"
is not testable; "rejects requests without a valid session token" is).

<!-- ⛔ POST-ITEM-CHECK: run immediately after writing each V## item, before starting the next —
  1. Tier matches Prerequisite via the fixed mapping above; a Tier 1 item
     cannot carry a Prerequisite other than `None`.
  2. Title is short enough to paste into an issue tracker as-is, not a
     sentence fragment lifted from the Description.
  3. Every Acceptance Criteria entry (table cell and bullets) is a testable
     statement, not a restated threat or a vague goal.
  4. Threat ID is the next sequential `V##` — no gaps, no reuse.
  If any check fails, fix this item now before moving to the next. -->

<!-- ⛔ POST-SECTION-CHECK: run once, after the last Backlog Item —
  1. Summary table's row count equals the number of V## items.
  2. Every V## item here will need a row in 3-findings.md's Threat
     Coverage Verification table (output-formats.md) — none may be
     silently dropped between this file and the findings file.
  If either check fails, fix now. -->
