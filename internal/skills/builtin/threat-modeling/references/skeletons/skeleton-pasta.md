# Skeleton: `2-pasta-analysis.md`

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, SKILL.md §4.1): every heading, in this order, each body
> replaced with `<!-- PENDING -->`. Then, section by section (§4.2), replace
> one `<!-- PENDING -->` at a time with the filled content shown beneath
> that section's heading here. PASTA's stages build on each other
> (`references/pasta.md`) — do not fill Stage 7 before Stage 1 exists,
> since its risk ratings cite Stage 1's business-impact categories.
>
> This file is `2-pasta-analysis.md` in the seven-file suite
> (`output-formats.md`) — Stages 2 and 3 belong in `0.1-architecture.md` and
> `1-model.md`/`1.1-model.mmd` instead of here (see `references/pasta.md`
> "Where each stage lands"); don't duplicate the component/DFD tables in
> this file.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Prerequisite (every threat, same five values every framework in this
>   skill uses): `None`, `Authenticated User`, `Internal Network`,
>   `Local Process`, `Host Compromise`.
> - Tier — **derived from Prerequisite, never assigned independently**:
>   `None` → Tier 1, `Authenticated User`/`Internal Network` → Tier 2,
>   `Local Process`/`Host Compromise` → Tier 3.
> - Likelihood: exactly one of `Low`, `Medium`, `High`.
> - Severity / Risk Rating: exactly one of `Critical`, `High`, `Medium`,
>   `Low` (this is the value `3-findings.md` sources as a PASTA finding's
>   Severity — see `output-formats.md`).
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop` — must match
>   `0.1-architecture.md`'s Deployment Classification, not be re-derived.
> - Threat ID prefix: `P` (`P1`, `P2`, …) — never `TH`, `T-1`, or a
>   per-stage counter. Attack path ID prefix: `AP` (`AP1`, `AP2`, …).

---

## Skeleton (initial, before any analysis)

```markdown
# PASTA Risk Analysis — [FILL: system/feature name]

> Framework: PASTA — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: must match 0.1-architecture.md exactly]
> Date: [FILL: YYYY-MM-DD]

## Stage 1 — Business Objectives

<!-- PENDING -->

## Stage 2-3 — Technical Scope and Decomposition

<!-- PENDING -->

## Summary

<!-- PENDING -->

## Stage 4-5 — Threat and Vulnerability Analysis

<!-- PENDING -->

## Stage 6 — Attack Modeling

<!-- PENDING -->

## Stage 7 — Risk and Impact Analysis

<!-- PENDING -->
```

**⛔ Section order:** Summary sits right after Stage 1/2-3 and **before**
Stage 4-5's detail table — the same "summary before detail" convention
`3-findings.md`'s Threat Coverage table and `2-stride-analysis.md`'s Summary
table both use elsewhere in this suite. Fill Summary last even though it
appears early in reading order (it recaps Stage 7's finished output).

## Fill-in shape per section

### Stage 1 — Business Objectives

Prose: what the system does, and the specific business consequences
(revenue, compliance, reputation, safety) a successful attack would
produce. Name concrete consequence *categories* here (e.g. "regulatory fine
under GDPR", "customer-facing outage during peak hours") — Stage 7's
Business Impact column must cite one of these categories, not invent a new
one at the end. This section is PASTA's own distinguishing content; it has
no equivalent in `0.1-architecture.md`.

<!-- ⛔ POST-SECTION CHECK: at least one consequence category is named per
plausible impact axis this system actually has (don't write only
"reputation damage" if the system also handles payments — add a
compliance/financial category too). -->

### Stage 2-3 — Technical Scope and Decomposition

Brief cross-reference, not a duplicate: "Architecture, components, trust
boundaries, and the DFD are in `0.1-architecture.md` and `1-model.md`." Add
only what those files don't already ask for and Stage 4 needs — e.g. an
explicit use-case/actor list, or a PASTA-specific scoping note (what's
explicitly out of scope for this risk analysis and why).

<!-- ⛔ POST-SECTION CHECK: every component named here already appears in
0.1-architecture.md's Key Components table — this section adds context,
it does not introduce a component that isn't anchored there. -->

### Summary

```markdown
| ID | Threat | Business Impact | Tier | Risk Rating |
|----|--------|-------------------|------|-------------|
[REPEAT: one row per threat, sorted by Risk Rating descending then Tier ascending]
| [FILL: P##] | [FILL: one-line] | [FILL: a Stage 1 category] | [FILL: 1/2/3] | [FILL: Critical/High/Medium/Low] |
[END-REPEAT]
```

### Stage 4-5 — Threat and Vulnerability Analysis

```markdown
| ID | Threat | Attack Vector | Evidence | Prerequisite | Tier | Mitigation | Residual Risk | Severity |
|----|--------|-----------------|----------|---------------|------|------------|------------------|----------|
[REPEAT: one row per (component, credible threat) pair]
| [FILL: P##] | [FILL] | [FILL: cite a CVE/CWE/advisory where the threat matches a known pattern from threat intel, else "novel — no known-pattern match"] | [FILL: file/config/code path, or a security_scan finding ID if one exists — correlate, don't duplicate as a separate list] | [FILL: one of the five fixed prerequisite values] | [FILL: derived from Prerequisite per the mapping above] | [FILL: control, or "none — open finding"] | [FILL] | [FILL: one of the four fixed severity values] |
[END-REPEAT]
```

**⛔ Prefer citing a real CWE/CVE/advisory class over an invented threat
description** — PASTA's distinguishing trait versus STRIDE is grounding
threats in known attack patterns for this stack (stage 4), then confirming
them as concrete vulnerabilities in *this* system (stage 5) — not
first-principles brainstorming alone. If `security_scan` (or the
`security-audit` skill) already ran for this system, cross-reference its
findings in the Evidence column rather than re-deriving them.

<!-- ⛔ POST-TABLE CHECK:
  1. Every row's Prerequisite is one of the five fixed values, and Tier
     agrees with the Prerequisite→Tier mapping above.
  2. No row's Prerequisite sits below the deployment classification's
     floor from 0.1-architecture.md's Component Exposure Table.
  3. Every row has a Severity from the four fixed values.
  4. Threat IDs are sequential (P1, P2, …) with no gaps or reuse.
  5. Every Evidence cell names an actual file/config path or scanner
     finding ID, not "assumed" or "likely".
  If any check fails, fix the table now before Stage 6. -->

### Stage 6 — Attack Modeling

For each Tier 1 threat and every other threat whose Severity is `Critical`
or `High`, either an attack tree (`references/companion-techniques.md`) or
a written attack path: entry point → each intermediate step → impact. This
is the stage that distinguishes PASTA from STRIDE/LINDDUN — it asks not
just "what could go wrong" but "how would an attacker actually get there",
so keep it substantive, not a token restatement of the threat row. Give
each attack path an ID (`AP1`, `AP2`, …); Stage 7 references these IDs.

<!-- ⛔ POST-SECTION CHECK: every attack path has an AP## ID, and every
Tier-1/Critical/High threat from Stage 4-5 has a corresponding attack path
— a high-severity threat with no attack path either isn't actually
reachable end-to-end (say so explicitly and reconsider its tier/severity)
or is a missing Stage 6 entry. -->

### Stage 7 — Risk and Impact Analysis

```markdown
| Attack Path | Threat | Likelihood | Business Impact | Risk Rating | Priority |
|--------------|--------|------------|--------------------|-------------|----------|
[REPEAT: one row per attack path from Stage 6, sorted by Priority ascending]
| [FILL: AP##] | [FILL: P## it derives from] | [FILL: Low/Medium/High] | [FILL: one of Stage 1's named consequence categories] | [FILL: Critical/High/Medium/Low] | [FILL: rank, 1 = highest] |
[END-REPEAT]
```

**⛔ Columns are exactly these six, in this order.** `Business Impact` must
be a category actually named in Stage 1 — if none fits, that's a sign
Stage 1 was incomplete; go back and add the missing category rather than
writing a Stage-7-only impact. The Risk Rating here is the authoritative
Severity for this threat everywhere else in the suite — if it disagrees
with the Severity written in Stage 4-5's table, Stage 7 wins; go back and
fix Stage 4-5.

<!-- ⛔ POST-TABLE CHECK:
  1. Every AP## from Stage 6 appears exactly once in this table.
  2. Every Threat (P##) referenced here exists in Stage 4-5.
  3. Business Impact values trace back to a Stage 1 category (search for
     the exact phrase).
  4. Priority ranks are unique and dense (1..N, no gaps, no ties left
     unresolved).
  5. Risk Rating here matches (or has overridden) the Severity in Stage
     4-5's table for the same threat.
  If any check fails, fix the table now before writing Summary. -->

**Reminder:** every threat in this file must appear exactly once in
`3-findings.md`'s Threat Coverage Verification table (`output-formats.md`)
— a Stage 4-5 threat with a non-empty Mitigation column and no
corresponding finding is a dropped finding, not a summary omission.
