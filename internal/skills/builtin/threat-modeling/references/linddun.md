# LINDDUN

**Focus:** a 7-category privacy-threat taxonomy applied per element of a
data-flow diagram, the same DFD-driven mechanics as STRIDE but aimed at
privacy harm instead of security harm. **Best for:** systems handling PII or
otherwise privacy-regulated data (GDPR/CCPA-scoped systems, health/financial
data, anything where the risk that matters is a person being harmed by data
*about* them, not just a system being compromised).

## The seven categories

| Letter | Category | Concern |
|---|---|---|
| L | Linkability | Can two or more items (data, actions, identities) be linked to the same data subject when they shouldn't be linkable? |
| I | Identifiability | Can a data subject be identified from data that was meant to be anonymous/pseudonymous? |
| N | Non-repudiation | Can a data subject be prevented from denying an action/statement — which is a privacy *harm* here (they may need deniability), the mirror image of STRIDE's Repudiation where non-repudiation is desirable |
| D | Detectability | Can the mere existence of an item of interest (a record, a message, a data subject's presence) be inferred, even without reading its content? |
| D | Disclosure of information | Can data be exposed to a party not authorized to see it? (Same concern as STRIDE's Information Disclosure, evaluated here specifically against privacy/consent scope rather than general confidentiality.) |
| U | Unawareness | Is the data subject unaware of data collection, processing, or their rights over it? |
| N | Non-compliance | Does processing violate a stated privacy policy, consent scope, or applicable regulation? |

## Process

1. **Build the DFD** the same way STRIDE does — external entities,
   processes, data stores, data flows, trust boundaries — but annotate which
   flows/stores carry personal or otherwise privacy-sensitive data. Only
   those need the full LINDDUN pass; flows carrying no personal data can be
   noted as out of scope for this framework.
2. **Apply each category per element** carrying personal data: does this
   element's handling of the data create linkability, identifiability risk,
   unwanted detectability, disclosure beyond consent scope, subject
   unawareness, or non-compliance with the stated policy?
3. **Trace consent and purpose scope explicitly.** A disclosure that's
   technically "authorized" (the recipient has a valid API key) can still be
   a LINDDUN finding if it exceeds the data subject's consent scope or the
   system's stated purpose for collecting the data — this is the dimension
   STRIDE has no equivalent for.
4. **Map a mitigation to every identified threat** — technical (anonymization,
   pseudonymization, aggregation, differential privacy, minimization) and/or
   procedural (consent flow, retention policy, access review).

## Findings: CWE and OWASP mapping

When a LINDDUN threat becomes a finding in `3-findings.md`, CWE and OWASP
don't map the way they do for STRIDE — many privacy harms aren't a
technical "weakness" in the CWE sense at all (linkability created by an
over-broad data model, for instance, is a design choice, not a defect
class). Write `CWE: N/A — privacy design issue, not a code weakness` when
that's genuinely true, and reserve a real CWE ID for the cases where the
harm *is* a concrete weakness (e.g. `CWE-359: Exposure of Private
Information` for an outright disclosure). For OWASP, map to the **OWASP
Top 10 Privacy Risks** list (`P1`-`P10`, e.g. `P2:2021 – Insufficient Data
Breach Response`) — not the web-application OWASP Top 10:2025, which is
built for a different category of harm entirely. See
`references/output-formats.md`'s per-framework applicability notes for the
full mapping across all six frameworks.

## Skeleton

The exact structure for this framework's file in the report suite —
`2-linddun-analysis.md` — verbatim skeleton, fill-in table shapes, fixed
value lists, and inline self-check comments, lives in
`references/skeletons/skeleton-linddun.md`. Read it before writing
anything; do not improvise the structure from the process description
above. (The suite's other six files — architecture, DFD, findings,
assessment, inventory — are framework-agnostic; their shape is in
`references/output-formats.md`.)
