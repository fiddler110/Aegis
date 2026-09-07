# Skeleton: 2b-maestro-layers.md

> **⛔ Copy the template content below VERBATIM (excluding the outer code fence). Replace `[FILL]` placeholders.**
> **⛔ This file is generated ONLY when Step 1c of `orchestrator.md` determined the system is agentic/AI-driven. If not applicable, do NOT create this file — do NOT create an empty placeholder either.**
> **⛔ Tiers, Status values, and ID numbering reuse the SAME rules as `2-stride-analysis.md` — see `analysis-principles.md`. Do not invent a separate risk scale.**
> **⛔ Threat ID format: `M{NN}.L{layer}` for single-layer (e.g. `M03.L3`), `M{NN}.LX` for cross-layer. Sequential NN across the whole file.**

---

```markdown
# MAESTRO — Agentic AI Layer Analysis

> This analysis uses the **MAESTRO** framework (Multi-Agent Environment, Security, Threat, Risk,
> and Outcome — Cloud Security Alliance) to model threats specific to this system's AI-agent
> architecture, organized by layer instead of by component. It supplements the component-level
> STRIDE-A analysis in [2-stride-analysis.md](2-stride-analysis.md) — a threat that is really a
> component-level STRIDE threat stays there; this file is for threats that only make sense in
> terms of the model/agent stack (prompt injection, tool misuse, goal manipulation, sub-agent
> trust, agent-ecosystem threats). Tiers and Status values are identical to STRIDE-A's.

## Applicability

**Detected agentic signals:** [FILL: list which Applicability Detection signals from
`maestro-framework.md` were found, e.g. "LLM orchestration loop (internal/engine), tool-calling
(internal/tool/builtin), multi-agent delegation (internal/swarm), MCP client (internal/mcp)"]

### Layer Applicability Table

| Layer | Applicable? | Justification |
|-------|-------------|----------------|
| L1 — Foundation Models | [FILL: Yes/No] | [FILL] |
| L2 — Data Operations | [FILL: Yes/No] | [FILL] |
| L3 — Agent Frameworks | [FILL: Yes/No] | [FILL] |
| L4 — Deployment and Infrastructure | [FILL: Yes/No] | [FILL] |
| L5 — Evaluation and Observability | [FILL: Yes/No] | [FILL] |
| L6 — Security and Compliance | [FILL: Yes/No] | [FILL] |
| L7 — Agent Ecosystem | [FILL: Yes/No] | [FILL] |

<!-- ⛔ POST-TABLE CHECK: Every layer marked "No" MUST have a justification citing the absence of
  the relevant signal (e.g., "No skill/persona marketplace or third-party registry exists in this
  codebase"). Do NOT skip N/A layers — the table must always have 7 rows. -->

## Summary

| Layer | Link | Threats | T1 | T2 | T3 | Risk |
|-------|------|---------|----|----|----|------|
[REPEAT: one row per APPLICABLE layer only — omit N/A layers from this table]
| [FILL: L#. Layer Name] | [Link](#[FILL: anchor]) | [FILL] | [FILL] | [FILL] | [FILL] | [FILL: Low/Medium/High/Critical] |
[END-REPEAT]
| Cross-Layer (LX) | [Link](#cross-layer-threats) | [FILL] | [FILL] | [FILL] | [FILL] | [FILL] |
| **Totals** | | **[FILL]** | **[FILL]** | **[FILL]** | **[FILL]** | |

<!-- ⛔ POST-TABLE CHECK:
  1. T1+T2+T3 = Threats for every row
  2. Only layers marked "Yes" in the Applicability Table appear here
  3. Totals row sums match column-wise
  If ANY check fails → FIX NOW before writing layer sections. -->

---

[REPEAT: one section per layer marked "Yes" in the Layer Applicability Table, in L1→L7 order]

## L[FILL: #] — [FILL: Layer Name]

**Components in this layer:** [FILL: component names from 0.1-architecture.md that participate in this layer — every threat below must cite one of these]

### Tier 1 — Direct Exposure (No Prerequisites)

| ID | Threat | Component | Prerequisites | Mitigation | Status |
|----|--------|-----------|---------------|------------|--------|
[REPEAT: threat rows or "*No Tier 1 threats identified.*"]
| [FILL: M##.L#] | [FILL] | [FILL: component name] | [FILL] | [FILL] | [FILL: Open/Mitigated/Platform] |
[END-REPEAT]

### Tier 2 — Conditional Risk

| ID | Threat | Component | Prerequisites | Mitigation | Status |
|----|--------|-----------|---------------|------------|--------|
[REPEAT: threat rows or "*No Tier 2 threats identified.*"]
| [FILL] | [FILL] | [FILL] | [FILL] | [FILL] | [FILL] |
[END-REPEAT]

### Tier 3 — Defense-in-Depth

| ID | Threat | Component | Prerequisites | Mitigation | Status |
|----|--------|-----------|---------------|------------|--------|
[REPEAT: threat rows or "*No Tier 3 threats identified.*"]
| [FILL] | [FILL] | [FILL] | [FILL] | [FILL] | [FILL] |
[END-REPEAT]

<!-- ⛔ POST-LAYER CHECK: Verify this layer:
  1. Every threat cites a real component from the component list (not an invented one)
  2. Status column uses ONLY: Open, Mitigated, Platform
  3. All 3 tier sub-sections present (even if empty with '*No Tier N threats*')
  If ANY check fails → FIX NOW before moving to next layer. -->

[END-REPEAT]

---

## Cross-Layer Threats

| ID | Threat | Layers | Components | Prerequisites | Mitigation | Status |
|----|--------|--------|------------|---------------|------------|--------|
[REPEAT: LX threat rows or "*No cross-layer threats identified.*"]
| [FILL: M##.LX] | [FILL] | [FILL: e.g. L3 → L4 → L7] | [FILL: component names] | [FILL] | [FILL] | [FILL] |
[END-REPEAT]

<!-- ⛔ POST-TABLE CHECK: Every LX threat names at least 2 layers. A threat naming only 1 layer
  belongs in that layer's own section, not here. -->
```
