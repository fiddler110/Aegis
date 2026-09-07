# MAESTRO Framework — Agentic AI Threat Analysis

This file defines the **MAESTRO** (Multi-Agent Environment, Security, Threat, Risk, and Outcome)
methodology, a threat framework published by the Cloud Security Alliance (Feb 2025) purpose-built
for agentic AI systems. It supplements — never replaces — the STRIDE-A analysis in
`2-stride-analysis.md`. STRIDE-A models threats per *component*; MAESTRO models threats per
*layer of the AI agent stack*, catching categories STRIDE-A's component framing tends to miss:
model-level attacks, tool/goal manipulation, sub-agent collusion, and agent-marketplace threats.

**This file is loaded only when Step 1c of `orchestrator.md` determines the target system is
agentic/AI-driven.** For a conventional web app, CLI tool, or service with no LLM orchestration,
skip this file entirely — do not force a MAESTRO section into the report.

---

## Applicability Detection (Step 1c)

Run this check immediately after Step 1 component identification, before deciding whether to
produce `2b-maestro-layers.md`.

**The target is in scope for MAESTRO if the codebase exhibits ANY of:**

| Signal | What to look for |
|--------|-------------------|
| LLM orchestration loop | A component that sends prompts/messages to an LLM and acts on structured tool-call responses in a loop (an "agent loop") |
| Tool-calling / function-calling | Model output is parsed into structured calls that execute code, hit APIs, or touch the filesystem |
| Multi-agent / sub-agent delegation | One agent spawns, dispatches to, or coordinates other agents (goroutine, subprocess, or remote sub-agents) |
| MCP client or server | Implements or consumes the Model Context Protocol (tool/resource discovery across a boundary) |
| Agent/skill/persona marketplace or registry | Discoverable, installable, or third-party-authored agent definitions, skills, or plugins loaded at runtime |
| RAG / vector retrieval feeding a model | Retrieval pipeline whose output becomes part of a prompt |

**If NONE apply:** the system is conventional software. Do not generate `2b-maestro-layers.md`.
Note the negative determination in `0-assessment.md` → Analysis Context & Assumptions
(one line: "MAESTRO: not applicable — no agentic/LLM-orchestration components identified").

**If ANY apply:** generate `2b-maestro-layers.md` per Step 5b of `orchestrator.md`. Record which
signals were found — this becomes the applicability justification in the report's Layer
Applicability table.

**Partial applicability is normal.** A system with an LLM orchestration loop but no multi-agent
delegation, no marketplace, and no external evaluation tooling will mark Layers 3, 4, 6 fully
applicable, and Layers 5 and 7 N/A. Justify every N/A the same way STRIDE-A requires — see
`### Layer Applicability Table` in `skeletons/skeleton-maestro.md`.

---

## Shared Machinery — Reused, Not Duplicated

MAESTRO analysis in this skill reuses the existing STRIDE-A machinery rather than inventing a
parallel one:

- **Exploitability Tiers (T1/T2/T3):** identical definitions and assignment rules as
  `analysis-principles.md` → Exploitability Tiers. A MAESTRO threat's tier follows from the same
  canonical prerequisite table.
- **Threat Status:** `Open` | `Mitigated` | `Platform` — same three values, same distinction rules.
- **Findings pipeline:** MAESTRO threats feed into the SAME `3-findings.md`, sorted into the same
  three tier sections alongside STRIDE-A findings. There is no separate MAESTRO findings file.
- **Threat Coverage Verification table:** extended to include MAESTRO threat IDs alongside STRIDE
  threat IDs — one table, one feedback loop.
- **CVSS / CWE / OWASP mapping:** every MAESTRO finding still needs all three, exactly as STRIDE-A
  findings do. Layer 1 (Foundation Model) threats often map to OWASP's ML/LLM-specific entries or
  to CWE categories for input validation / resource exhaustion — see the per-layer tables below for
  suggested mappings.

**What's different:** the threat ID prefix (`M` instead of `T`), the organizing axis (layer instead
of component), and the threat catalog itself (below).

**Threat ID format:** `M{NN}.L{layer}` for a single-layer threat (e.g. `M03.L3`), or `M{NN}.LX` for
a threat that spans multiple layers (Cross-Layer Threats section). Sequential `NN` across the whole
MAESTRO file, not per-layer.

---

## The Seven Layers

Map each component already identified in `0.1-architecture.md` to the layer(s) it participates in.
A single component can span layers (e.g., a sub-agent dispatcher is both Layer 3 and, if it talks
to a registry, Layer 7). Do not invent components — every MAESTRO threat must cite a real
component from the locked component list, exactly as STRIDE-A requires.

### Layer 1 — Foundation Models

The LLM(s) the system calls or hosts, and the boundary where prompts go in and completions come out.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Prompt injection (direct) | User input placed in the prompt overrides system instructions | CWE-77 (Command Injection variant) | A05:2025 – Injection |
| Prompt injection (indirect) | Content the model retrieves/reads (a file, a web page, a tool result) contains instructions the model follows | CWE-77 | A05:2025 – Injection |
| Adversarial inputs | Crafted inputs designed to elicit a specific wrong output or bypass a safety filter | CWE-20 | A06:2025 – Insecure Design |
| Model DoS via expensive queries | Unbounded prompt size, unbounded output length, or repeated calls exhaust budget/compute | CWE-400, CWE-770 | A10:2025 – Mishandling of Exceptional Conditions |
| Sensitive data in prompts | Secrets, credentials, or PII sent to an external model provider | CWE-200 | A04:2025 – Cryptographic Failures / data exposure |
| Model/provider spoofing | Endpoint or API key swapped to redirect calls to an attacker-controlled model | CWE-290 | A07:2025 – Authentication Failures |

**Evidence to look for:** the adapter/`Stream` seam, prompt-construction code, any system-prompt
assembly, budget/`MaxTokens` enforcement, provider failover logic.

### Layer 2 — Data Operations

Data the system ingests, stores, retrieves, or feeds back into a model: RAG pipelines, vector
stores, session/conversation history, knowledge bases, memory systems.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Data poisoning | Attacker-controlled content enters a knowledge base / memory store and is later retrieved into a prompt | CWE-345 | A08:2025 – Software/Data Integrity Failures |
| RAG/retrieval injection | Retrieved document contains instructions the model executes (a variant of indirect prompt injection, rooted here) | CWE-77 | A05:2025 – Injection |
| Session/history tampering | Conversation history or checkpoint state is modified to alter future model behavior | CWE-345 | A08:2025 |
| Sensitive data exfiltration via retrieval | A retrieval query surfaces data the requesting context shouldn't see (cross-session, cross-tenant) | CWE-200, CWE-863 | A01:2025 – Broken Access Control |
| Unbounded ingestion / DoS | No limit on data volume fed into embeddings/storage pipeline | CWE-770 | A10:2025 |

**Evidence to look for:** vector store / embedding code, session store schema, compaction and
memory/knowledge packages, any file the model is allowed to read into context.

### Layer 3 — Agent Frameworks

The orchestration code itself: the agent loop, tool dispatch, sub-agent spawning, skill/persona
loading.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Tool-description poisoning | A tool's name/description (sent to the model as part of its schema) contains hidden instructions | CWE-77 | A05:2025 |
| Goal / instruction manipulation | Attacker-controlled input changes the agent's effective objective mid-run | CWE-284 | A06:2025 |
| Excessive agency / tool misuse | Agent granted broader tool capability than the task requires, used destructively | CWE-284, CWE-269 | A01:2025 |
| Sub-agent trust boundary bypass | A sub-agent inherits or escalates privileges beyond what its parent granted | CWE-269 | A01:2025 |
| Supply-chain compromise of framework deps | A malicious dependency in the agent framework itself | CWE-1104 | A03:2025 – Software Supply Chain Failures |
| Loop / evasion of internal controls | Agent behavior designed (or manipulated) to evade the framework's own safety/permission checks | CWE-693 | A06:2025 |

**Evidence to look for:** the engine's tool-dispatch loop, permission gate, sub-agent/swarm spawn
code, skill and persona loaders, any place a tool schema or persona file is read and trusted.

### Layer 4 — Deployment and Infrastructure

Where the agent process(es) actually run: containers, sandboxes, host processes, orchestration.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Sandbox escape | Agent-executed code breaks out of its intended execution sandbox | CWE-269 | A01:2025 |
| Compromised container/image | Base image or scanner container image is tampered with | CWE-1104 | A03:2025 |
| Resource hijacking | Compromised agent process used for cryptomining or as a pivot | CWE-400 | A10:2025 |
| Infra-as-code manipulation | Container build scripts / Terraform-equivalent tampered to weaken isolation | CWE-1188 | A02:2025 – Security Misconfiguration |
| Persistent workspace container abuse | A long-lived per-workspace sandbox container becomes a stepping stone across sessions | CWE-668 | A01:2025 |

**Evidence to look for:** sandbox implementation (local/Docker/Podman/WSL/Apple), container
lifecycle management, any privilege the execution environment holds beyond the task at hand.

### Layer 5 — Evaluation and Observability

Logging, tracing, cost/telemetry tracking, eval harnesses, and anything that watches agent behavior.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Log/trace data leakage | Prompts, tool outputs, or secrets captured in logs/traces without redaction | CWE-532 | A09:2025 – Security Logging & Alerting Failures |
| Evaluation metric gaming | Eval harness can be manipulated to report false-positive quality scores | CWE-345 | A08:2025 |
| Observability tool compromise | The logging/tracing sink itself becomes an exfiltration channel | CWE-200 | A09:2025 |
| Evasion of detection | Agent behavior crafted to avoid tripping loop-detection / guard / anomaly checks | CWE-693 | A09:2025 |

**Evidence to look for:** session/trace storage, cost tracking, the output guard, loop-detection
logic, eval harness code.

### Layer 6 — Security and Compliance (Cross-Cutting)

The controls meant to constrain agent behavior: permission modes, gates, guards, workspace trust,
secrets handling. This layer cuts across all others — analyze it against every layer's threats, not
in isolation.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Permission gate bypass | A tool call is misclassified (e.g., a downgraded capability check) and escapes the intended mode restriction | CWE-863 | A01:2025 |
| Guard/evaluator poisoning | The rubric or model used to validate agent output is itself manipulable | CWE-345 | A08:2025 |
| Workspace trust confusion | A trust grant is reused across a directory whose security-relevant config has changed | CWE-346 | A01:2025 |
| Security-agent model extraction | An attacker probes the guard/permission model to learn what it will and won't allow, then crafts a bypass | CWE-200 | A01:2025 |

**Evidence to look for:** permission gate, output guard, workspace-trust fingerprinting, secrets
handling (`.aegis/.env` and equivalent), any component whose job is to say "no."

### Layer 7 — Agent Ecosystem

Where agents, skills, personas, or sub-agents are discovered, installed, or interoperate with
external/third-party agents.

| Threat pattern | Example manifestation | Typical CWE | Typical OWASP |
|---|---|---|---|
| Malicious skill/persona | A third-party or project-level skill/persona file contains instructions that redirect agent behavior | CWE-829 | A03:2025 |
| Agent/tool impersonation | An MCP server or tool presents itself as trusted but is attacker-controlled | CWE-290 | A07:2025 |
| Registry/marketplace poisoning | A skill/persona/MCP registry is tampered with to promote a malicious entry | CWE-494 | A03:2025 |
| Capability misrepresentation | A tool or agent's advertised capability doesn't match what it actually does, leading to over-trust | CWE-436 | A06:2025 |

**Evidence to look for:** MCP client connection code, skill/persona discovery and precedence
(project > user > embedded), any mechanism that loads agent-shaped content not authored by this
codebase's own team.

---

## Cross-Layer Threats (LX)

Threats that only make sense as an interaction between layers. Do not force every threat into a
single layer — if the attack path crosses layers, use `LX` and name every layer involved.

| Threat pattern | Layers involved | Example |
|---|---|---|
| Supply-chain compromise cascades | L3 → L4 → L7 | A compromised agent-framework dependency ships in the container image and later masquerades as a legitimate skill |
| Goal-manipulation cascade | L1 → L3 → L7 | Prompt injection at L1 changes agent goal at L3, which is then propagated to sub-agents/other agents at L7 |
| Privilege escalation via lateral movement | L4 → L6 | Infrastructure access lets an attacker weaken the permission gate itself |
| Data leakage across the pipeline | L2 → L5 | Retrieved sensitive data is later leaked through unredacted observability logs |

---

## Applying MAESTRO — Six-Step Method (Adapted)

1. **System decomposition** — reuse the component list already locked in Step 1 of the STRIDE-A
   workflow; do not re-derive it. Map each component to the layer(s) it belongs to.
2. **Layer-specific threat modeling** — for each applicable layer, walk its threat pattern table
   above and identify concrete threats grounded in this codebase's actual components.
3. **Cross-layer analysis** — identify LX threats after the per-layer pass, once you can see the
   full picture.
4. **Risk assessment** — assign Tier (T1/T2/T3) using the same canonical prerequisite mapping as
   STRIDE-A. No separate risk scale.
5. **Mitigation planning** — same `Mitigation Type` enum as STRIDE-A findings (`Redesign` /
   `Standard Mitigation` / `Custom Mitigation` / `Existing Control` / `Accept Risk` / `Transfer Risk`).
6. **Findings** — every `Open` MAESTRO threat becomes a `3-findings.md` entry, exactly like an
   `Open` STRIDE threat. There is no separate implementation/monitoring step in the report — that's
   the team's job after remediation, same as STRIDE-A.

---

## Mitigation Reference (AI-Specific, Supplementary)

These supplement — never replace — the `Mitigation Type` enum. Use them as vocabulary in a
finding's `#### Remediation` section when the fix is AI-specific:

- **Input/output validation at the model boundary** — schema-validate tool-call arguments before
  execution; sanitize/limit what's echoed back into a prompt from tool output.
- **Least-agency tool scoping** — grant only the tools a given task needs, not the full registry.
- **Sub-agent capability inheritance limits** — a sub-agent's effective permission must never exceed
  its parent's.
- **Provenance tagging for retrieved/tool content** — mark content that came from an untrusted
  source (web fetch, tool output, third-party skill) so the model/guard can weight it differently
  than a direct user instruction.
- **Rate/budget limits per agent, not just per session** — a single sub-agent runaway shouldn't be
  able to exhaust a shared budget silently.
