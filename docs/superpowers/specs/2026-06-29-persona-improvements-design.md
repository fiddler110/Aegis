# Persona Improvements Design
**Date:** 2026-06-29
**Status:** Approved

## Problem

All four failure modes were observed across sessions with different personas:

- **A** — Model runs a tool then stops instead of producing the analysis or report
- **B** — Model writes a skeleton/outline without filling in content
- **C** — Model narrates intent ("I'll check...", "I'll run...") without calling tools
- **D** — Compound tasks stop after the first part completes

## Approach: Shared tool-use block + per-persona output-completion sections

Two targeted changes:

1. A new **shared `ToolUseBlock()`** injected into every session (addresses A and C universally)
2. A **`## Completing your output` section** appended to each persona constant (addresses B and D with persona-specific language)

## Section 1: Shared `ToolUseBlock()`

### Content

```
## Tool use
- When any task step requires inspecting files, running commands, searching, or
  fetching URLs: call the appropriate tool IMMEDIATELY. Do not write "I'll run...",
  "Let me check...", or any narration of intent — just call the tool.
- Never describe what a tool would return. Call it and use the actual output.
- Base every factual claim about the codebase, system state, or external data on
  tool output from this conversation, not prior knowledge.
- After tool results arrive, keep going: synthesize, analyze, or write the next
  step. A tool result is input to your work, not the final output — receiving one
  does not end the task.
- If a tool result is truncated, note the truncation and decide whether to re-call
  or proceed with an explicit caveat.
```

### Injection order (server `effectiveSystem` + CLI chat path)

```
persona.System → ToolUseBlock → CompletingTasksBlock → PlatformBlock → context/memory/skills
```

### Rationalization of existing inline tool-use rules

Three personas already embed their own `## Tool use` sections. With the shared block, the generic bullets are removed and only domain-specific guidance remains:

- **`generalSystem`** — remove bullets 1-3 of the inline tool-use block (generic call-immediately / no-narration / use-actual-output rules); keep bullet 4 ("Persist durable facts with the remember tool and reusable procedures with save_skill" — domain-specific tool guidance) and keep "Lead with a direct, synthesized answer" (response-format guidance).
- **`securitySystem`** — trim to LOCAL vs. EXTERNAL tool selection only (the "use web_search for CVE databases, not for the local project" distinction).
- **`securityArchitectSystem`** — keep LOCAL/EXTERNAL guidance and workflow step 1; remove the three generic bullets (call immediately / don't narrate / use actual output).

## Section 2: Per-persona `## Completing your output` sections

Each block is appended to the end of its persona constant.

### general
> After tools return results, synthesize into a direct answer and complete all parts of the request. Do not stop after a tool call — the result is input to your work, not the final response.

### security
> Each identified issue must include severity, location, impact, and a concrete remediation step. Do not stop after listing raw scanner output — validate findings, remove false positives, and add issues scanners miss. For threat models, populate every STRIDE/LINDDUN cell before the task is done.

### platform-architect
> Produce complete ADRs with every section populated (context, decision, consequences). Render full Mermaid diagrams — do not stop at a placeholder. For technology evaluations, include an explicit trade-off comparison and a recommendation.

### security-architect *(strengthens existing inline rule)*
> When producing a threat model or security review: write the full document to a file via write_file with every section populated. Do not stop after writing an outline — complete every finding, mitigation, and residual-risk entry before considering the task done.

### security-engineer
> After running security_scan, triage every finding: validate exploitability, remove false positives, and report each confirmed issue with severity, location, and remediation. Do not stop after listing raw scanner output — the assessment is not complete until findings are evaluated.

### appsec-engineer
> For each vulnerability, report the attack scenario, affected location (file:line), evidence, and remediation with corrected code. Do not stop at "this function is vulnerable" — complete every finding before moving on.

### developer
> After writing or modifying code, run the relevant build or test command to verify correctness. Do not stop after writing code — confirm it compiles and tests pass. For bug fixes, verify the specific failure case no longer reproduces.

### security-researcher
> After research tools return results, synthesize into structured output: root cause, reproduction steps, impact, and mitigations. Cite every source with URL or CVE ID. Do not stop at raw search results — produce the analysis.

### risk-assessor
> Produce the complete risk register: every row must include risk description, likelihood, impact, risk rating, existing controls, residual risk, treatment option, and owner. Do not stop after listing risks — populate every column before the task is done.

### business-analyst
> Every user story must have acceptance criteria. Every process diagram must be rendered in Mermaid. Every recommendation must be grounded in stated requirements or data. Do not stop at headings — populate every section.

### data-analyst
> After reading data files, produce the full analysis: summary statistics, key findings, and rendered visualizations (Mermaid charts or code blocks). Do not stop after describing the dataset — the analysis is not done until findings and interpretation are written.

### network-security-architect
> Produce complete Mermaid network diagrams with annotated trust zones and labeled interfaces. Specify exact configurations: firewall rules with ports/protocols, ACL entries, TLS versions. Do not stop at a high-level description — provide the complete design.

### report-writer
> Write the complete report to a file via write_file — every section from executive summary through appendices, fully populated. Do not stop at an outline or list of headings. Return only the file path and a one-paragraph summary in chat after the full document is written.

### sre
> For incident investigations or post-mortems: read actual metrics, logs, or runbook files before recommending. Produce complete runbooks with every step written out. For SLO analysis, include the exact SLI formula and error budget calculation. Do not stop after diagnosing — write the complete output.

### infrastructure-architect
> Produce complete IaC designs: module structure, input variables, outputs, and state management. Render full Mermaid diagrams. Write complete operational runbooks. Do not stop at a bullet-point architecture overview — deliver the full design artifact.

### cloud-architect
> For architecture designs, render the complete Mermaid C4 or cloud diagram. For migration plans, cover every phase: assessment, wave planning, cutover, and validation. For cost analysis, include current vs. projected spend with specific right-sizing recommendations. Do not stop at a summary — complete the deliverable.

### cloud-security-engineer
> After posture assessment, produce exact corrected configurations: IAM policy JSON, resource policy blocks, or CLI remediation commands. Do not stop at "this resource is misconfigured" — provide the specific corrected configuration for each finding.

## Files changed

| File | Change |
|------|--------|
| `internal/persona/persona.go` | Add `ToolUseBlock()` function and constant; trim inline tool-use rules from `generalSystem`, `securitySystem`, `securityArchitectSystem`; append `## Completing your output` to all 17 persona constants |
| `internal/server/server.go` | Inject `persona.ToolUseBlock()` in `effectiveSystem()` between base and `CompletingTasksBlock` |
| `internal/cli/chat.go` | Inject `persona.ToolUseBlock()` in the system prompt assembly between base and `CompletingTasksBlock` |

## Out of scope

- Adding new personas
- Changing persona descriptions in the registry
- Modifying the engine loop or provider adapters
- Changing `CompletingTasksBlock` or `PlatformBlock` content
