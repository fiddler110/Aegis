# Skeleton: PASTA threat model document

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order (one per
> PASTA stage, plus Summary), each body replaced with `<!-- PENDING -->`.
> Then, stage by stage (§4.2), replace one `<!-- PENDING -->` at a time
> with the filled content shown beneath that stage's heading here. PASTA's
> stages build on each other (§ in `references/pasta.md`) — do not fill
> stage 7 before stage 1 exists, since its risk ratings cite stage 1's
> business-impact categories.
>
> **⛔ Fixed values — do not invent alternatives:**
> - Likelihood / Business impact / Risk rating: exactly one of `Low`,
>   `Medium`, `High` (impact and rating may also be `Critical` at the top
>   end; likelihood is only Low/Medium/High).
> - Deployment classification: exactly one of `internet-facing`,
>   `internal-network`, `localhost-service`, `local-desktop`.
> - Attack path ID prefix: `AP` (`AP1`, `AP2`, …).

---

## Skeleton (initial, before any analysis)

```markdown
# PASTA Threat Model — [FILL: system/feature name]

> Framework: PASTA — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of the four fixed values above]
> Date: [FILL: YYYY-MM-DD]

## Stage 1 — Business objectives

<!-- PENDING -->

## Stage 2 — Technical scope

<!-- PENDING -->

## Stage 3 — Application decomposition

<!-- PENDING -->

## Stage 4 — Threat analysis

<!-- PENDING -->

## Stage 5 — Vulnerability & weakness analysis

<!-- PENDING -->

## Stage 6 — Attack modeling

<!-- PENDING -->

## Stage 7 — Risk & impact analysis

<!-- PENDING -->

## Summary

<!-- PENDING -->
```

## Fill-in shape per section

### Stage 1 — Business objectives

Prose: what the system does, and the specific business consequences
(revenue, compliance, reputation, safety) a successful attack would
produce. Name concrete consequence *categories* here (e.g. "regulatory
fine under GDPR", "customer-facing outage during peak hours") — Stage 7's
Business impact column must cite one of these categories, not invent a new
one at the end.

<!-- ⛔ POST-SECTION CHECK: at least one consequence category is named per
plausible impact axis this system actually has (don't write only
"reputation damage" if the system also handles payments — add a
compliance/financial category too). -->

### Stage 2 — Technical scope

Prose: architecture, stack, dependencies, integration points, deployment
environment — read from the real code/config/infra-as-code, not assumed.

### Stage 3 — Application decomposition

Data-flow diagram (Mermaid/PlantUML) plus prose: components, trust
boundaries, entry points, actors, use cases. Same anchor rule as every
framework — every component cites a real file/class/manifest.

### Stage 4 — Threat analysis

```markdown
| Threat | Known pattern / CVE / CWE class | Affected component |
|--------|----------------------------------|---------------------|
[REPEAT: one row per enumerated threat]
| [FILL] | [FILL: cite a CVE/CWE/advisory where the threat matches a known pattern, else "novel — no known-pattern match"] | [FILL: component from Stage 3] |
[END-REPEAT]
```

**⛔ Prefer citing a real CWE/CVE/advisory class over an invented threat
description** — PASTA's distinguishing trait versus STRIDE is grounding
threats in known attack patterns for this stack, not first-principles
brainstorming alone.

### Stage 5 — Vulnerability & weakness analysis

```markdown
| Threat (from Stage 4) | Concrete vulnerability in this system | Scanner finding (if any) |
|-------------------------|------------------------------------------|----------------------------|
[REPEAT: one row per Stage-4 threat with a confirmed concrete vulnerability]
| [FILL] | [FILL: file/config/code path] | [FILL: security_scan finding ID if it exists, else "none — manual finding"] |
[END-REPEAT]
```

If `security_scan` (or the `security-audit` skill) already ran for this
system, cross-reference its findings here rather than duplicating them as
a separate list.

### Stage 6 — Attack modeling

For each threat carried forward from Stage 5, either an attack tree (see
`references/companion-techniques.md`) or a written attack path: entry
point → each intermediate step → impact. Give each attack path an ID
(`AP1`, `AP2`, …) — Stage 7's table references these IDs.

<!-- ⛔ POST-SECTION CHECK: every attack path has an `AP##` ID, and every
Stage-5 vulnerability that's exploitable end-to-end has a corresponding
attack path — a vulnerability with no attack path either isn't reachable
(say so explicitly) or is a missing Stage 6 entry. -->

### Stage 7 — Risk & impact analysis

```markdown
| Attack path | Likelihood | Business impact | Risk rating | Mitigation | Priority |
|-------------|------------|------------------|-------------|------------|----------|
[REPEAT: one row per attack path from Stage 6, sorted by Priority ascending]
| [FILL: AP##] | [FILL: Low/Medium/High] | [FILL: one of Stage 1's named consequence categories] | [FILL: Low/Medium/High/Critical] | [FILL: control, or "none — open finding"] | [FILL: rank, 1 = highest] |
[END-REPEAT]
```

**⛔ Columns are exactly these six, in this order.** `Business impact`
must be a category actually named in Stage 1 — if none fits, that's a
sign Stage 1 was incomplete; go back and add the missing category rather
than writing a Stage-7-only impact.

<!-- ⛔ POST-TABLE CHECK: run before writing Summary —
  1. Every `AP##` from Stage 6 appears exactly once in this table.
  2. Business impact values trace back to a Stage 1 category (search for
     the exact phrase).
  3. Priority ranks are unique and dense (1..N, no gaps, no ties left
     unresolved).
  If any check fails, fix the table now before writing the Summary. -->

### Summary

Top-ranked risks (by Priority), and which remain unmitigated open
findings and why (accepted, deferred, no owner yet — but never mark an
unmitigated risk as accepted on the model's own authority; see SKILL.md
§4's cross-framework rule).

<!-- ⛔ POST-SECTION CHECK: the Summary's top-ranked list matches the top
rows of the Stage 7 table by Priority — it doesn't re-rank informally. -->
