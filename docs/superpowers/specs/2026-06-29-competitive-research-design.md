# Competitive Research & Roadmap Design
**Date:** 2026-06-29
**Status:** Approved

## Goal

Evaluate Aegis against 12 frontier agent harnesses across two categories — user-facing coding agents and multi-agent orchestration frameworks — to identify capability gaps and produce a prioritized roadmap.

## Deliverables

Two files written to `./research/`:

### `research/competitive-analysis.md`
1. **Key Findings** — 3–5 bullet executive summary of the most important gaps
2. **Comparison Matrix** — feature × competitor grid (Aegis + 12 competitors as columns)
3. **Per-Competitor Deep Dives** — one section per competitor: overview, standout capabilities, weaknesses, and what Aegis could adopt

### `research/roadmap.md`
1. **Gap Summary** — what Aegis is missing, grouped by category
2. **P1 — Critical** — gaps present across most competitors that Aegis lacks entirely
3. **P2 — Meaningful** — present in 2–3 competitors, worth adding
4. **P3 — Exploratory** — long-horizon or niche ideas worth tracking

## Competitors

**User-facing coding agents (8):**
- Claude Code (Anthropic)
- Aider
- OpenAI Codex CLI
- Cursor
- Devin (Cognition)
- Gemini CLI (Google)
- Windsurf (Codeium)
- Amazon Q Developer

**Multi-agent orchestration frameworks (4):**
- LangGraph (LangChain)
- AutoGen (Microsoft)
- CrewAI
- Smolagents (HuggingFace)

## Matrix Feature Dimensions (rows)

1. Core execution model (loop, tools, interruption)
2. Tool ecosystem (built-in tools + extensibility)
3. Multi-agent / orchestration
4. Memory & context management
5. Permission / safety model
6. IDE / editor integration
7. Provider support (cloud + local LLMs)
8. UI surfaces (TUI, web, CLI, IDE)
9. Extensibility (plugins, MCP, custom tools)
10. Observability (tracing, cost, audit)
11. Local LLM support
12. Security-specific features
13. Cost / token management
14. Session persistence & resumability

## Out of Scope

- Implementation of any gap items (roadmap only)
- Benchmarking or quantitative performance comparison
- Pricing comparison (changes too frequently)
