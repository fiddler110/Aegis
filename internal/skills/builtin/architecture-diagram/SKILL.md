---
name: architecture-diagram
description: Use when asked to diagram or visualize a system — architecture, request/data flow, sequence of calls, entity/data model, or dependency graph. Triggers on "diagram this", "draw the architecture", "show me how this flows", "visualize the dependencies", "sequence diagram", "ER diagram".
---

# Architecture Diagram Skill

A diagram is a claim about structure, and like any claim it should be
verified against the code before it's drawn — not sketched from a
half-remembered mental model. This skill covers picking the right notation,
grounding it in the actual codebase, and rendering it with the `render_diagram`
tool.

## 1. Match the notation to what's being shown

Don't default to one diagram type for every request — the shape of the
question determines the shape of the diagram:

- **Components/subsystems and how they call each other** → a component or
  C4-style diagram (mermaid `graph`/`flowchart`, or `c4plantuml` for a
  formal C4 model with explicit container/component levels).
- **A sequence of calls across time** (request lifecycle, an auth handshake,
  a multi-step tool-call loop) → a sequence diagram (mermaid `sequenceDiagram`
  or plantuml).
- **Data shape and relationships** (database schema, entity model) → an ER
  diagram (mermaid `erDiagram`).
- **Package/module dependency structure** → a dependency graph
  (`graphviz`/`dot` handles large graphs and clustering better than mermaid).
- **A specific, elaborate architecture doc meant to stand alone** →
  `structurizr` or `c4plantuml`, which are built for that register; reach
  for mermaid by default otherwise since it renders fastest and embeds
  cleanly in markdown.

## 2. Gather ground truth before drawing

Read the actual code the diagram claims to represent — package imports and
call sites for a component diagram, the handler chain for a sequence
diagram, the actual struct/table definitions for an ER diagram. Don't
invent a box that doesn't correspond to a real package, service, or table,
and don't omit an edge that exists in the code just because it complicates
the picture — a diagram that's simpler than reality is actively misleading.

## 3. Keep it readable

- Collapse leaf-level detail that doesn't serve the question being asked
  (a component diagram of the whole system doesn't need every internal
  function; a sequence diagram of one request doesn't need every package).
- Label edges with what actually flows or what the relationship actually is
  ("HTTP POST /sessions", "owns", "implements") — an unlabeled arrow forces
  the reader to guess.
- If the true graph is large, either split it into a few focused diagrams
  (one per subsystem/flow) rather than one unreadable diagram, or cluster
  related nodes explicitly (graphviz subgraphs, mermaid subgraph blocks).

## 4. Render and deliver

Call `render_diagram` with the notation `type` (`mermaid`, `plantuml`,
`c4plantuml`, `structurizr`, `graphviz`, etc.) and the diagram `source`.
Pass a `path` to save SVG/PNG/draw.io output into the workspace when the
user wants a file; omit it to get SVG/draw.io content back inline (useful
when you want to double-check the render before saving, or the user just
wants to see it in chat). PNG/PDF output requires a `path` — it can't be
returned inline.

If the render fails (invalid syntax for the chosen notation), fix the
source and retry rather than switching to a plainer notation just to make
it succeed — a mermaid syntax error is almost always a small fixable
mistake, not a sign mermaid can't express the diagram.
