# Skeleton: Trike threat model document

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order, each
> body replaced with `<!-- PENDING -->`. Then, section by section (§4.2),
> replace one `<!-- PENDING -->` at a time with the filled table shown
> beneath that section's heading here. The Risk model depends on the
> Permission matrix existing first — do not fill it out of order.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Allowed?: exactly `allowed` or `denied`.
> - Probability / Impact: exactly one of `Low`, `Medium`, `High`.
> - Decision: exactly `mitigate: [FILL: proposed control]` or
>   `pending decision` — **never** a bare `accept` written by the model
>   itself (see the ⛔ rule under Risk model below).
> - Risk ID prefix: `R` (`R1`, `R2`, …).

---

## Skeleton (initial, before any analysis)

```markdown
# Trike Threat Model — [FILL: system/feature name]

> Framework: Trike — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of internet-facing/internal-network/localhost-service/local-desktop]
> Date: [FILL: YYYY-MM-DD]

## Requirements model

### Actors

<!-- PENDING -->

### Assets

<!-- PENDING -->

### Permission matrix

<!-- PENDING -->

## Implementation model

<!-- PENDING -->

## Risk model

<!-- PENDING -->

## Summary

<!-- PENDING -->
```

## Fill-in shape per section

### Actors

Bullet list, one per distinct actor class: `- [FILL: actor name] —
[FILL: what makes this class distinct from others]`. Split actors
wherever their permitted actions actually differ in the real authz
code — "admin" and "read-only admin" are different actors if the code
distinguishes them.

### Assets

Bullet list: `- [FILL: asset] — [FILL: why it matters]`.

### Permission matrix

```markdown
| Actor | Asset | Action | Allowed? |
|-------|-------|--------|----------|
[REPEAT: one row per (actor, asset, action) triple derived from the real authz code/config]
| [FILL] | [FILL] | [FILL: create/read/update/delete or system-specific action] | [FILL: allowed or denied] |
[END-REPEAT]
```

**⛔ Build this from the actual authz code/config, not assumed intent.**
This table is the artifact everything else derives from — the Risk model
below has no source material if this table is guessed rather than
verified against real permission checks.

<!-- ⛔ POST-TABLE CHECK: every actor from Actors and every asset from
Assets appears at least once in this table; a listed actor or asset with
zero rows is either dead weight (remove it) or a coverage gap (add its
rows). -->

### Implementation model

Data-flow diagram (Mermaid/PlantUML), same mechanics as STRIDE, but each
flow annotated with the Permission-matrix cell(s) it implements (e.g. "DF2
implements the {ReadOnlyUser, Invoice, read} cell").

<!-- ⛔ POST-SECTION CHECK: every flow in this diagram cites at least one
Permission-matrix cell; a flow with no cell reference means either the
matrix is missing a row or the flow doesn't belong in this model. -->

### Risk model

```markdown
| ID | Denied action attempted / allowed action misused | Actor | Asset | Probability | Impact | Risk | Decision (mitigate/accept) | Decided by |
|----|-----------------------------------------------------|-------|-------|--------------|--------|------|------------------------------|-------------|
[REPEAT: one row per threat derived from a Permission-matrix cell — a denied action that could succeed, or an allowed action reachable by the wrong actor]
| [FILL: R##] | [FILL] | [FILL: actor from Actors] | [FILL: asset from Assets] | [FILL: Low/Medium/High] | [FILL: Low/Medium/High] | [FILL: rating derived from Probability × Impact] | [FILL: "mitigate: <control>" or "pending decision" — see ⛔ below] | [FILL: owner, or "—" if Decision is not "accept"] |
[END-REPEAT]
```

**⛔ Columns are exactly these nine, in this order.**

**⛔ The `accept` decision is never the model's to make.** Write
`Decision: accept: [FILL: reason]` **only** when recording a decision a
named human already made — stated by the user in this conversation, or
found in an existing risk register/ADR (cite it in `Decided by`). Every
other unmitigated row gets `mitigate: <proposed control>` (if a fix is
known) or `pending decision` (if it isn't yet) — never a bare `accept`
authored during this analysis.

<!-- ⛔ POST-TABLE CHECK: run before writing Summary —
  1. Every `accept` decision has a non-empty `Decided by` naming a real
     owner and a cited source (user statement or document) — an `accept`
     with `Decided by: —` or a vague "the team" is a violation; downgrade
     it to `pending decision` and flag it as needing a real decision.
  2. Risk ratings are internally consistent: two rows with the same
     Probability/Impact pair have the same Risk rating.
  3. Risk IDs are sequential (`R1`, `R2`, …) with no gaps or reuse.
  If any check fails, fix the table now before writing the Summary. -->

### Summary

Count of accepted vs. mitigated vs. pending risks; explicitly flag any
`accept` row that lacks a named owner or cited reason as incomplete
governance, not a closed item.

<!-- ⛔ POST-SECTION CHECK: the counts here match the Risk model table's
actual Decision values — no row silently recategorized between the table
and the summary. -->
