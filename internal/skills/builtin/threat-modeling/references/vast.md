# VAST (Visual, Agile, Simple Threat modeling)

**Focus:** designed to scale threat modeling across many teams and services
on an Agile/DevSecOps cadence, rather than producing one deep document for
one system. **Best for:** large organizations with many services/teams
where threat modeling needs to fit into existing sprint/backlog workflows
and be repeatable by people who aren't security specialists, not a
one-off deep-dive by a single expert.

## Two model types

VAST splits threat modeling into two views, matching who consumes each one:

1. **Application Threat Models** — built from process-flow diagrams
   (how the application actually works, feature by feature), consumed by
   development teams. Granularity matches a feature or service, not the
   whole enterprise — sized so a single team can own and update it as the
   feature evolves.
2. **Operational Threat Models** — built from data-flow/infrastructure
   diagrams, attacker-centric (what would an attacker actually target given
   the deployed infrastructure), consumed by operations/infrastructure
   teams.

Model whichever view (or both) matches what was asked: a specific
feature/service ask → Application Threat Model; an infrastructure/deployment
ask → Operational Threat Model.

## Process

1. **Scope to one feature/service (Application) or one infra
   boundary (Operational)** — VAST's scaling premise depends on models
   staying small enough that a team can own and update theirs without a
   security specialist gatekeeping every change. Don't produce one
   enterprise-wide model when the ask is scoped to a single service.
2. **Build the process-flow or data-flow diagram** from the real
   code/infra, matching the model type chosen in step 1.
3. **Enumerate threats** against the diagram — VAST doesn't mandate a fixed
   taxonomy the way STRIDE/LINDDUN do; use STRIDE categories per element as
   the default enumeration method unless something more specific to the
   system fits better, and say which method was used.
4. **Convert each threat into a backlog-shaped item**: a title, description,
   severity, and acceptance criteria for its mitigation — written so it can
   be dropped directly into the team's existing issue tracker as a ticket,
   not just left as a document row. This is VAST's defining trait: threats
   are actionable backlog items from the start, not a separate artifact that
   needs manual translation into tickets later.
5. **Keep it current** — note in the output that this model should be
   revisited when the feature/infra it covers changes materially, since
   VAST's Agile framing assumes iteration rather than a one-time document.
   The inventory sidecar (`references/verification-and-updates.md`) is what
   makes that revisit cheap — the next run diffs against it instead of
   starting over, which is exactly the cadence VAST assumes.

## Output template

```
# VAST Threat Model — <feature/service name> (<Application|Operational>)

## Scope
<the single feature/service or infra boundary this model covers, and why that scope>

## Diagram
<process-flow diagram (Application) or data-flow/infra diagram (Operational)>

## Threats (backlog-shaped)

| ID | Title | Description | Evidence | Severity | Acceptance criteria for mitigation |
|---|---|---|---|---|---|
| V1 | <short title, ticket-ready> | <threat description> | <file/config making it real> | <critical/high/medium/low> | <what "fixed" looks like> |

## Enumeration method used
<e.g. "STRIDE per diagram element" — state it since VAST doesn't mandate one>

## Summary
<count by severity; note that this model should be revisited on material change to the scoped feature/infra>
```
