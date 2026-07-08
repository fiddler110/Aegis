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

The data-flow diagram, same mechanics as STRIDE/LINDDUN: components, trust
boundaries, data flows — but annotated against the permission matrix so each
flow is traceable to the (actor, asset, action) cell it implements.

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

## Output template

```
# Trike Threat Model — <system/feature name>

## Requirements model

### Actors
<list, one row per distinct actor class>

### Assets
<data/capabilities worth protecting>

### Permission matrix

| Actor | Asset | Action | Allowed? |
|---|---|---|---|
| <actor> | <asset> | <action> | <allowed/denied> |

## Implementation model
<data-flow diagram, trust boundaries, each flow annotated with the permission-matrix cell(s) it implements>

## Risk model

| ID | Denied action attempted / allowed action misused | Actor | Asset | Probability | Impact | Risk | Decision (mitigate/accept) | Decided by |
|---|---|---|---|---|---|---|---|---|
| R1 | <description> | <actor> | <asset> | <low/med/high> | <low/med/high> | <rating> | <mitigate: control / accept: reason> | <owner> |

## Summary
<count of accepted vs. mitigated risks; any accepted risk lacking a named owner or reason — flag as incomplete>
```
