# Skeleton: `2-trike-analysis.md`

> Copy the structure below **verbatim** as the initial skeleton for
> `2-trike-analysis.md` (`write_file`, SKILL.md §4.1): every heading, in
> this order, each body replaced with `<!-- PENDING -->`. Then, in
> dependency order (SKILL.md §4.2), replace one `<!-- PENDING -->` at a
> time with the filled table shown beneath that section's heading here.
> The Risk Analysis depends on the Permission Matrix existing first — do
> not fill it out of order. This file is Trike's *own* analysis file; the
> DFD (Implementation model) lives in the shared `1.1-model.mmd` /
> `1-model.md` files (`output-formats.md`), annotated against the
> Permission Matrix below.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Allowed?: exactly `allowed` or `denied`.
> - Prerequisite (every risk row): exactly one of `None`,
>   `Authenticated User`, `Internal Network`, `Local Process`,
>   `Host Compromise` — the same five values every framework in this skill
>   uses (`output-formats.md`).
> - Tier: exactly `1`, `2`, or `3` — **derived from Prerequisite**, never
>   assigned independently: `None` → 1; `Authenticated User` or
>   `Internal Network` → 2; `Local Process` or `Host Compromise` → 3.
> - Probability / Impact: exactly one of `Low`, `Medium`, `High`.
> - Severity: exactly one of `Critical`, `High`, `Medium`, `Low` —
>   **derived from Probability × Impact**, never assigned independently
>   (mapping table below).
> - Decision: exactly `mitigate: [FILL: proposed control]` or
>   `pending decision` — **never** a bare `accept` written by the model
>   itself (see the ⛔ rule under Risk Analysis below).
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop`.
> - Risk ID prefix: `R` (`R1`, `R2`, …) — never `T` (STRIDE owns that
>   prefix elsewhere in this suite) and never a per-asset counter.

---

## Skeleton (initial, before any analysis)

```markdown
# Trike Risk Analysis — [FILL: system/feature name]

> Framework: Trike — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of the four fixed values above]
> Date: [FILL: YYYY-MM-DD]

## Actors

<!-- PENDING -->

## Assets

<!-- PENDING -->

## Permission Matrix

<!-- PENDING -->

## Summary

<!-- PENDING -->

## Risk Analysis

<!-- PENDING -->
```

## Fill-in shape per section

### Actors

Bullet list, one per distinct actor class: `- [FILL: actor name] —
[FILL: what makes this class distinct from others]`. Split actors
wherever their permitted actions actually differ in the real authz code —
"admin" and "read-only admin" are different actors if the code
distinguishes them.

### Assets

Bullet list: `- [FILL: asset] — [FILL: why it matters]`.

### Permission Matrix

```markdown
| Actor | Asset | Action | Allowed? |
|-------|-------|--------|----------|
[REPEAT: one row per (actor, asset, action) triple derived from the real authz code/config]
| [FILL] | [FILL] | [FILL: create/read/update/delete or system-specific action] | [FILL: allowed or denied] |
[END-REPEAT]
```

**⛔ Build this from the actual authz code/config, not assumed intent.**
This table is the artifact everything else derives from — the Risk
Analysis below has no source material if this table is guessed rather
than verified against real permission checks. This is Trike's
foundational artifact and has no equivalent in `0.1-architecture.md` — it
lives here, in full, not summarized elsewhere.

Once this table exists, go back to `1.1-model.mmd` and annotate each flow
with the Permission Matrix cell(s) it implements (e.g. "DF2 implements
{ReadOnlyUser, Invoice, read}") — the flow's description field in
`1-model.md`'s Data Flow Table is where that annotation lives.

<!-- ⛔ POST-TABLE CHECK: every actor from Actors and every asset from
Assets appears at least once in this table; a listed actor or asset with
zero rows is either dead weight (remove it) or a coverage gap (add its
rows). -->

### Summary

Placed **before** the Risk Analysis table, as a navigation aid — a reader
should see the shape of the risk landscape before the row-by-row detail.

```markdown
| ID | Actor | Asset | Action | Risk | Tier | Decision |
|----|-------|-------|--------|------|------|----------|
[REPEAT: one row per risk in the Risk Analysis table below, same order]
| [FILL: R##] | [FILL] | [FILL] | [FILL] | [FILL: Critical/High/Medium/Low] | [FILL: 1/2/3] | [FILL: mitigate / pending decision / accept] |
[END-REPEAT]
```

<!-- ⛔ POST-SECTION CHECK: this table cannot be filled before Risk
Analysis exists — if you find yourself guessing Risk/Tier/Decision values
here, stop and fill Risk Analysis first, then copy its values back into
this summary. -->

### Risk Analysis

Organized into one subsection per **asset** — this is what the Related
Threats links in `3-findings.md` anchor to
(`[R04](2-trike-analysis.md#asset-invoice-data)`), since Trike's natural
grouping is by what's being protected, not by DFD component. Anchor-safe
heading: letters, numbers, spaces, and hyphens only — no `&`, `/`, `(`,
`)`, `:`, `'`, `"`.

```markdown
### Asset: [FILL: asset name, matches Assets list]

| ID | Actor | Action | Threat | Evidence | Prerequisite | Tier | Probability | Impact | Risk | Decision | Severity |
|----|-------|--------|--------|----------|---------------|------|--------------|--------|------|----------|----------|
[REPEAT: one row per threat derived from a Permission Matrix cell involving this asset — a denied action that could succeed, or an allowed action reachable by the wrong actor]
| [FILL: R##] | [FILL: actor from Actors] | [FILL: action from Permission Matrix] | [FILL] | [FILL: file/config/code path] | [FILL: one of the five fixed prerequisite values] | [FILL: 1/2/3, derived from Prerequisite] | [FILL: Low/Medium/High] | [FILL: Low/Medium/High] | [FILL: rating from the mapping table below] | [FILL: "mitigate: <control>" or "pending decision" or "accept: <owner> — <reason>" — see ⛔ below] | [FILL: derived from Risk, see mapping table] |
[END-REPEAT]

[REPEAT: one subsection per asset]
```

**⛔ Columns are exactly these twelve, in this order.** Do not rename
`Threat` to `Description`, drop `Evidence`, or merge `Probability`/
`Impact` into a single free-text `Risk` cell — Risk and Severity are both
*derived*, and a reader needs the two inputs visible to check the
derivation.

**Probability × Impact → Risk mapping (fixed, mechanical — not
judgment-based):**

| Probability \ Impact | Low | Medium | High |
|---|---|---|---|
| **Low** | Low | Low | Medium |
| **Medium** | Low | Medium | High |
| **High** | Medium | High | Critical |

**Risk → Severity:** identical scale — a `Risk` of `Critical` is a
`Severity` of `Critical`, and so on down the same four-value list. Two
distinct columns exist because Risk is Trike's own vocabulary (feeds the
Summary table above) and Severity is what `3-findings.md` consumes
alongside CVSS — keep both, don't collapse them into one column.

**Prerequisite → Tier mapping (fixed, same as every other framework in
this skill):**

| Prerequisite | Tier |
|---|---|
| `None` | 1 |
| `Authenticated User` or `Internal Network` | 2 |
| `Local Process` or `Host Compromise` | 3 |

**⛔ The `accept` decision is never the model's to make.** Write
`Decision: accept: [FILL: owner] — [FILL: reason]` **only** when recording
a decision a named human already made — stated by the user in this
conversation, or found in an existing risk register/ADR (name that source
in the reason). Every other unmitigated row gets `mitigate: <proposed
control>` (if a fix is known) or `pending decision` (if it isn't yet) —
never a bare `accept` authored during this analysis, and never `accept`
with no owner named.

<!-- ⛔ POST-TABLE CHECK: run after each asset subsection, and again after
the last one, before writing/reconciling Summary —
  1. Every `accept` decision has a named owner and a cited source (user
     statement or document) in its reason — an `accept` with no owner, or
     a vague "the team decided", is a violation; downgrade it to
     `pending decision` and flag it as needing a real decision in
     0-assessment.md's Needs Verification table.
  2. Every row's Tier matches its Prerequisite per the mapping above.
  3. Every row's Risk matches its Probability/Impact pair per the mapping
     above, and Severity equals Risk.
  4. Risk IDs (`R1`, `R2`, …) are sequential across the whole file, no
     gaps or reuse, even though rows are split across asset subsections.
  5. No prerequisite sits below the deployment classification's floor
     (SKILL.md §2.4) — a `localhost-service` system's floor is `Local
     Process` at minimum.
  If any check fails, fix the table now — do not carry the defect into
  the Summary table above or into 3-findings.md. -->

Every risk row here must appear exactly once in `3-findings.md`'s Threat
Coverage Verification table (`output-formats.md`) — `Covered (FIND-XX)`
for an open `mitigate`/`pending decision` row, `Mitigated (FIND-XX)` for a
row whose control is already implemented in this codebase (including an
`accept` row, whose finding documents the owner-attributed decision
itself, not a fix), or `Mitigated by platform` only for the rare case
where an entirely external system already forecloses the denied action.
