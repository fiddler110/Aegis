# Diagram Conventions

Lightweight, fixed conventions for the two diagram types this skill
produces, so a reader can tell a component diagram from a DFD at a glance
and so repeated runs don't drift in color/shape choice. Rendered with the
`render_diagram` tool (`type: "mermaid"`); pass `path` under the run's output
directory to also save a viewable SVG alongside the `.mmd` source.

## Data Flow Diagram (`1.1-model.mmd` / `1-model.md`)

Use `flowchart LR` (left-to-right) — always this direction, not `TB`, so
repeated runs on the same system produce a comparable layout.

**Shapes:**
- External entity (a user, an external service): `ComponentId(["Label"])`
- Process (a service, an agent, a handler): `ComponentId["Label"]`
- Data store (a database, a file store, a cache): `ComponentId[("Label")]`

**Colors** — fixed `classDef`s, copied verbatim, no other fill/stroke pairs:

```
classDef process fill:#6baed6,stroke:#2171b5,color:#000
classDef external fill:#fdae61,stroke:#d94701,color:#000
classDef datastore fill:#74c476,stroke:#238b45,color:#000
```

Apply with `class ComponentId process` (or `external`/`datastore`) per node.

**Trust boundaries** are `subgraph` blocks, one per boundary identified in
SKILL.md §2 — a boundary that exists only in prose doesn't count as drawn.
Boundary titles match the Trust Boundary Table in `1-model.md` exactly.

**Flows:** label every edge with what actually moves (`"HTTP POST /login"`,
not an unlabeled arrow). Use `<-->` for a single bidirectional
request/response interaction (see the flow modeling rule in
`output-formats.md`) and `-->` only when the two directions are genuinely
asymmetric in protocol or cadence.

**Scale:** if the diagram grows past roughly 15 elements or 4 boundaries,
split it into one diagram per trust boundary or subsystem rather than
producing one unreadable diagram — note in `1-model.md` which sub-diagram
covers which boundary.

## Component / architecture diagram (`0.1-architecture.md`)

A higher-level view than the DFD — subsystems and how they call each other,
not every data flow. Use the same three shapes and `classDef`s above for
visual consistency with the DFD, but it is acceptable (and often clearer) to
collapse several DFD elements into one box here if they form one deployable
unit (e.g. a service and its sidecar).

## Sequence diagrams (`0.1-architecture.md` Top Scenarios)

Standard Mermaid `sequenceDiagram`. Name participants with the exact
component IDs from the Key Components table — not generic `Client`/`Server`
unless those really are the component names. Use `alt`/`opt` blocks for
error paths worth showing (an auth failure, a timeout) rather than only the
happy path.

## Pre-render checklist

Before calling `render_diagram`:
- [ ] Every element name matches, verbatim, its use in `0.1-architecture.md`'s Key Components table and `2-<framework>-analysis.md`'s per-element sections
- [ ] Every `subgraph` boundary has a row in the Trust Boundary Table
- [ ] Only the three `classDef`s above are used, no other fill/stroke pairs
- [ ] `flowchart LR` for the DFD (never `TB`)
- [ ] No unlabeled edges

If the render fails, fix the Mermaid syntax and retry — per the
`architecture-diagram` skill's guidance, a syntax error is almost always a
small fixable mistake, not a reason to drop the diagram.
