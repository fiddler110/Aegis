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

## Output template

```
# PASTA Threat Model — <system/feature name>

## Stage 1 — Business objectives
<what the system does; business consequences of compromise (revenue, compliance, reputation, safety)>

## Stage 2 — Technical scope
<architecture, stack, dependencies, deployment environment>

## Stage 3 — Application decomposition
<data-flow diagram: components, trust boundaries, entry points, actors, use cases>

## Stage 4 — Threat analysis
<enumerated threats, each tied to a known attack pattern/CVE/CWE class where applicable>

## Stage 5 — Vulnerability & weakness analysis
<concrete vulnerabilities in this system per threat, scanner findings cross-referenced>

## Stage 6 — Attack modeling
<attack tree or attack path per credible threat, entry point → impact>

## Stage 7 — Risk & impact analysis

| Attack path | Likelihood | Business impact | Risk rating | Mitigation | Priority |
|---|---|---|---|---|---|
| <path> | <low/med/high> | <consequence from stage 1> | <rating> | <control> | <rank> |

## Summary
<top-ranked risks; any that remain unmitigated and why>
```
