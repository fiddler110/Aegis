# Trike

**Focus:** a requirements/access-control model that derives threats from
*explicit, denied* actions rather than an abstract taxonomy — every threat
traces back to a permission someone decided to grant or deny, with an
auditable risk-acceptance record. **Best for:** governance-heavy contexts
where a decision to accept a risk (rather than mitigate it) needs to be
explicit and attributable — audits, regulated environments, anywhere "who
decided this risk was acceptable, and when" needs a paper trail.

## The two models

### 1. Requirements model

Define, explicitly:
- **Actors** — every distinct class of user/system/service that interacts
  with the system (not just "user" and "admin" — separate actors wherever
  their permitted actions actually differ).
- **Assets** — data and capabilities worth protecting.
- **Intended actions** — for each (actor, asset) pair, the actions that
  actor is *allowed* to take (create/read/update/delete, or system-specific
  actions).
- **Permission matrix** — a table of actor × asset × action, each cell
  marked allowed or denied. This is the artifact everything else derives
  from — build it from the actual authz code/config, not from assumed
  intent.

### 2. Implementation model

This is the shared `1.1-model.mmd` / `1-model.md` DFD every framework in
this skill produces (`output-formats.md`) — components, trust boundaries,
data flows — but for Trike, each flow must also be traceable to the
(actor, asset, action) cell of the Permission Matrix it implements. The
Permission Matrix itself has no equivalent in the shared files; it lives
in `2-trike-analysis.md`, Trike's own analysis file, alongside the Risk
model below.

## Risk model — deriving threats from the permission matrix

Trike's key move: **a threat is a denied action succeeding, or an allowed
action being performed by the wrong actor.** For every "denied" cell in the
permission matrix, ask: what would it take for that denied action to
actually succeed given the real implementation? For every "allowed" cell,
ask: does the implementation correctly restrict this action to *only* the
actor(s) it's allowed for?

For each threat found this way, assign:
- **Probability** — how feasible is it given the real implementation
  (not a generic guess).
- **Impact** — consequence to the asset if it succeeds.
- **Risk = probability × impact**, and an explicit **accept / mitigate**
  decision with who made it. An "accept" decision without an attributable
  owner and a reason is incomplete — Trike's governance value comes from
  that record existing, not just the risk number.

**The accept decision is never yours.** When producing the model, fill the
decision column with `mitigate: <proposed control>` or `pending decision`;
write `accept` only when recording a decision a named human already made
(stated by the user, or found in an existing risk register/ADR), with that
owner and reason attributed. A model-authored `accept` defeats the entire
point of Trike's paper trail.

**CVSS/CWE/OWASP in `3-findings.md`:** a Trike finding's Severity is derived
from Probability × Impact, not assigned freestanding (the mapping table
lives in `skeletons/skeleton-trike.md`). Still map CVSS/CWE/OWASP when the
underlying denied-action-succeeding is a concrete technical weakness (a
missing authorization check has a real CWE); write
`CWE: N/A — access-control design gap, not a single CWE` when the threat is
a broader permission-model issue — e.g. an entire actor class with no
granular action-level restriction — than one weakness class captures. An
`accept` decision surfaces in `3-findings.md`'s Threat Coverage Verification
as `Mitigated (FIND-XX)` citing the owner and reason, never as a bare
"Accepted Risk" status (`output-formats.md` forbids that status outright).

## Skeleton

The exact structure of `2-trike-analysis.md` — verbatim skeleton, fill-in
table shapes, fixed value lists, the Probability×Impact→Severity mapping,
and inline self-check comments — lives in
`references/skeletons/skeleton-trike.md`. Read it before writing
anything; do not improvise the structure from the process description
above.
