# PASTA (Process for Attack Simulation and Threat Analysis)

**Focus:** risk-centric, 7-stage process that explicitly ties technical
threats back to business impact and includes an attack-simulation stage.
**Best for:** enterprise contexts where a threat needs to be traceable to a
business consequence and prioritized accordingly — not just "this is a
Tampering threat" but "this is a Tampering threat that could cost the
business X."

## The seven stages

1. **Define business objectives** — what does the system exist to do, and
   what business consequences (revenue, compliance, reputation, safety)
   would a successful attack produce? Ground this in what the system
   actually does, not a generic list.
2. **Define the technical scope** — architecture, technology stack,
   dependencies, integration points, deployment environment. Read the actual
   code/config/infra-as-code for this rather than assuming a stack.
3. **Application decomposition** — build the data-flow diagram: components,
   trust boundaries, entry points, data flows, and identify use cases and
   actors.
4. **Threat analysis** — enumerate threats against the decomposed
   application using threat-intelligence sources: known attack patterns for
   this stack/tech (CVE history, CWE categories, relevant advisories), not
   only invented-from-first-principles threats.
5. **Vulnerability and weakness analysis** — map identified threats to
   concrete vulnerabilities/weaknesses in *this* system (run `security_scan`
   if available and cross-reference its findings here; correlate scanner
   output with the threats from stage 4 rather than treating them as a
   separate list).
6. **Attack modeling / simulation** — for each credible threat, build an
   attack tree (see `references/companion-techniques.md`) or walk the
   concrete attack path an adversary would take, from entry point to impact.
   This is the stage that distinguishes PASTA from STRIDE/LINDDUN — it asks
   not just "what could go wrong" but "how would an attacker actually get
   there."
7. **Risk and impact analysis** — quantify/qualify risk (likelihood ×
   business impact from stage 1) per attack path, prioritize, and
   recommend mitigations ranked by risk reduction per effort.

## Process notes

- Stages build on each other — don't skip stage 1 and jump to a generic
  threat list; the business-impact framing from stage 1 is what stage 7's
  prioritization depends on.
- Stage 4/5 is where scanner output (SAST/SCA/secrets findings) belongs —
  correlate rather than duplicate the `security-audit` skill's output if
  both are in play for the same system.

## Where each stage lands in the seven-file suite

PASTA is the one framework whose native process has stages that overlap
the suite's framework-agnostic files (SKILL.md §4, `output-formats.md`).
Don't duplicate — split the work:

- **Stages 2 and 3** (technical scope, application decomposition) are what
  `0.1-architecture.md` and `1-model.md`/`1.1-model.mmd` already capture for
  every framework — write the components, trust boundaries, DFD, and
  Component Exposure Table there, not a second time in
  `2-pasta-analysis.md`. `2-pasta-analysis.md` only needs a brief
  cross-reference plus any PASTA-specific decomposition detail those files
  don't ask for (e.g. an explicit use-case/actor list, if it clarifies
  stage 4's threat enumeration).
- **Stage 1** (business objectives) has no home in the framework-agnostic
  files — it's PASTA's own distinguishing content — so it opens
  `2-pasta-analysis.md` as this framework's preamble.
- **Stages 4 through 7** (threat analysis, vulnerability analysis, attack
  modeling, risk/impact) are the actual per-threat content of
  `2-pasta-analysis.md`.

**Findings and CVSS/CWE/OWASP:** stage 5's vulnerability analysis produces
concrete technical weaknesses the same way STRIDE's per-element threats do
— treat PASTA findings in `3-findings.md` like STRIDE findings for CVSS
4.0/CWE/OWASP Top 10:2025 mapping purposes (`output-formats.md`'s
mandatory-fields table), with stage 6's attack-path narrative folded into
each finding's Description rather than left only in
`2-pasta-analysis.md`. A threat's Tier in `3-findings.md` is derived from
its prerequisite exactly as for every other framework; its Severity comes
from stage 7's Risk rating.

## Skeleton

The exact structure of `2-pasta-analysis.md` — verbatim skeleton, fill-in
table shapes, fixed value lists, and inline self-check comments — lives in
`references/skeletons/skeleton-pasta.md`. Read it before writing
anything; do not improvise the structure from the process description
above.
