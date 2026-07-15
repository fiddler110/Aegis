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

## CVSS / CWE / OWASP mapping

VAST itself doesn't mandate a taxonomy, so `3-findings.md`'s mandatory
CVSS/CWE/OWASP fields (`output-formats.md`) borrow whichever mapping fits
the model type in play rather than being left empty: an **Application
Threat Model** borrows the STRIDE mapping (CVSS 4.0 + CWE + OWASP Top
10:2025 — the same as a STRIDE finding, since the enumeration method used is
usually STRIDE categories per element anyway); a data-centric **Operational
Threat Model** borrows the NIST 800-154 mapping (attack vector per data
location, control/gap per vector). State which one was borrowed alongside
the "Enumeration method used" line in `2-vast-analysis.md` — a reader needs
to know which standard the mapping is drawing from, since VAST doesn't fix
one itself.

## Skeleton

The exact structure of this framework's own analysis file,
`2-vast-analysis.md` — verbatim skeleton, fill-in table shapes, fixed value
lists, and inline self-check comments — lives in
`references/skeletons/skeleton-vast.md`. Read it before writing anything;
do not improvise the structure from the process description above. The
other six files in the run's output directory (architecture, DFD, findings,
assessment, inventory) are framework-agnostic and covered by
`references/output-formats.md` instead.
