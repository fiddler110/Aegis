# Aegis Competitive Analysis
**Date:** 2026-06-29

---

## Key Findings

- **Multi-agent orchestration is the largest gap.** Every framework (LangGraph, AutoGen, CrewAI, Smolagents) and several coding agents (Devin, Claude Code) support spawning sub-agents or parallel workers. Aegis runs a single agent loop with no delegation primitive.
- **Aegis has no sandboxed execution.** Codex CLI, Gemini CLI, and Devin all run untrusted code in isolated environments (Docker/gVisor/VMs). Aegis's permission gate approves commands but does not contain their blast radius.
- **Observability is minimal.** LangGraph, AutoGen, and CrewAI ship per-step tracing and cost accounting. Aegis tracks cost totals but has no per-turn token breakdown, no trace export, and no audit log.
- **Provider lock-in is a risk.** Aider supports 100+ models; Gemini CLI and Claude Code are single-provider. Aegis supports Anthropic + Ollama but lacks a model-router abstraction to add providers without code changes.
- **Session resumability is underdeveloped.** Aegis has SQLite-backed session storage, but there is no first-class "continue session by ID" UX comparable to Claude Code's `--resume` or LangGraph's checkpoint/time-travel.

---

## Comparison Matrix

Legend: ✅ full · ⚠️ partial · ❌ absent · — not applicable

| Dimension | **Aegis** | Claude Code | Aider | Codex CLI | Cursor | Devin | Gemini CLI | Windsurf | Amazon Q | LangGraph | AutoGen | CrewAI | Smolagents |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **1. Core execution model** | ReAct loop, mode gate | ReAct loop, interrupt/resume | ReAct loop, edit-apply | ReAct loop, sandbox+approval | ReAct + Background Agent | Agentic loop, multi-VM | ReAct loop, gVisor sandbox | ReAct + Fast Context | ReAct loop | Graph nodes, checkpointed | Actor model (rounds) | Crew → Task DAG | CodeAgent / ToolCallingAgent |
| **2. Tool ecosystem** | ~15 builtins, registry | ~20 builtins + MCP | git/edit/LSP | shell, files, web | shell, files, web, Tab | browser, code, web | shell, files, Google Search | shell, files, search | shell, files, AWS SDK | Any LangChain tool | Any tool, serialized | Per-task restricted tools | Any tool, code-as-actions |
| **3. Multi-agent / orchestration** | ❌ | ⚠️ Agent tool (subagents) | ❌ | ❌ | ❌ | ✅ multi-VM parallel | ❌ | ❌ | ❌ | ✅ graph-based | ✅ actor model, GroupChat | ✅ Crews + Flows | ✅ ManagedAgent-as-tool |
| **4. Memory & context management** | SQLite sessions, CLAUDE.md | CLAUDE.md hierarchy, `--resume` | .aider.conf, repo-map | config files | codebase index | DeepWiki, task memory | 1M context window | Fast Context indexing | workspace index | Per-node state, shared graph | Agent state dict | Task context, crew memory tiers | Step-level memory mutation |
| **5. Permission / safety model** | Mode gate (plan/build), per-call approval | Permission rules, MDM-managed | ❌ no permission layer | Dual: sandbox + per-cmd approval | Setting toggle | Scoped task permissions | gVisor sandbox | ❌ | IAM-scoped | Interrupt before/after node | Human-in-loop per turn | Dual guardrails (fn + LLM) | ❌ |
| **6. IDE / editor integration** | ❌ | ⚠️ VS Code ext (alpha) | ❌ | ❌ | ✅ native IDE | ✅ browser-based | ⚠️ VS Code ext | ✅ native VS Code | ✅ VS Code + JetBrains | — | — | — | — |
| **7. Provider support** | Anthropic + Ollama | Anthropic only | ✅ 100+ via litellm | OpenAI + Azure | Anthropic + OpenAI + Azure | Proprietary + OpenAI | Google Gemini only | SWE-1 + OpenAI | Amazon Bedrock | Any LangChain LLM | Any LLM | Any LLM | Any HF / OpenAI-compat |
| **8. UI surfaces** | TUI (bubbletea), CLI | TUI + IDE ext + web | CLI only | CLI only | IDE | Web (SaaS) | CLI | IDE | IDE + CLI | Python API | Python API | Python API + CLI | Python API |
| **9. Extensibility** | Tool registry, personas, skills | MCP servers, CLAUDE.md hooks | Custom commands | Custom tools | Extensions | ❌ (SaaS) | MCP servers | Extensions | Plugins | Custom nodes/tools | Custom agents/tools | Custom agents/tools | Custom agents/tools |
| **10. Observability** | Cost total only | Cost + usage stats | ❌ | ❌ | ❌ | Task timeline (SaaS) | Cost + token usage | ❌ | ❌ | ✅ LangSmith tracing | ✅ per-turn trace | ⚠️ task-level logs | ⚠️ step logs |
| **11. Local LLM support** | ✅ Ollama | ❌ | ✅ Ollama + OpenAI-compat | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ via LangChain | ✅ any adapter | ✅ any adapter | ✅ HF models |
| **12. Security-specific features** | ✅ 17 security personas, threat-model skills | ❌ generic | ❌ generic | ❌ generic | ❌ generic | ❌ generic | ❌ generic | ❌ generic | ⚠️ SAST scan | ❌ generic | ❌ generic | ❌ generic | ❌ generic |
| **13. Cost / token management** | Budget cap, cost tracker | Token counts shown | ❌ | ❌ | ❌ | ACU billing (SaaS) | Token counts shown | ❌ | ❌ | ⚠️ via LangSmith | ⚠️ manual | ⚠️ manual | ⚠️ manual |
| **14. Session persistence & resumability** | ✅ SQLite sessions | ✅ `--resume`, `--continue` | ⚠️ chat history file | ❌ | ❌ | ✅ persistent SaaS tasks | ⚠️ conversation file | ❌ | ❌ | ✅ checkpoints + time-travel | ⚠️ state serialization | ⚠️ state output | ❌ |

---

## Per-Competitor Deep Dives

### Claude Code (Anthropic)

**Overview:** Terminal-first agent CLI built by Anthropic. Ships as a binary, runs an interrupt-capable ReAct loop, and supports CLAUDE.md instruction files at project and user levels. The closest architectural relative to Aegis.

**Standout capabilities:**
- CLAUDE.md hierarchy: project root, parent dirs, and `~/.claude/CLAUDE.md` are all loaded and merged — persistent, version-controlled agent instructions
- MCP server support: any MCP-compliant tool server plugs in via config, dramatically expanding the tool surface without shipping new code
- Permission rules in CLAUDE.md: `allow`/`deny` patterns for tool calls without touching code
- MDM-managed settings (enterprise): admins push default configs org-wide
- `--resume` / `--continue` flags for reattaching to sessions by ID
- Subagent spawning via the `Agent` tool — coordinator can fork off parallel sub-tasks

**Weaknesses:** Anthropic-only provider; no sandbox isolation for shell commands; IDE extension is still alpha.

**What Aegis could adopt:** Text-based permission rules (versionable `allow`/`deny`), MCP server support, `--resume` UX by session ID, subagent spawning primitive.

---

### Aider

**Overview:** Open-source coding agent built around git-native edit workflows. Integrates with 100+ LLM providers via litellm. Widely used and respected for its architect/editor model separation and repo-map context building.

**Standout capabilities:**
- litellm routing: switch provider/model with a flag — no code changes required
- Architect + Editor mode: one (capable) LLM plans changes, a cheaper LLM applies edits — cost-efficient
- Repo-map: generates a compact symbol index of the codebase for context injection, scaling to large repos
- Git-native: every edit is a commit; `--dry-run` for safe preview
- Watch mode: file watcher that re-runs the agent on save

**Weaknesses:** No permission layer; no session persistence; no TUI; no multi-agent.

**What Aegis could adopt:** Model-router abstraction (provider + model as config, not code), repo-map-style codebase summarization for large projects, dry-run / preview-commit workflow.

---

### OpenAI Codex CLI

**Overview:** OpenAI's terminal agent. Security is the design focus: all shell execution runs inside a hardened Docker sandbox with explicit per-command approval before any execution.

**Standout capabilities:**
- Dual safety layer: hardened Docker sandbox + per-command human approval before any shell execution
- Tiered approval modes: `suggest` (all requires approval), `auto-edit` (file edits auto-apply, shell requires approval), `full-auto` (all auto inside sandbox)
- Minimal surface area — small footprint reduces attack surface

**Weaknesses:** OpenAI/Azure only; no memory/session persistence; no observability; no multi-agent; sparse tool set.

**What Aegis could adopt:** Sandboxed execution mode (Docker/WSL container) as an opt-in permission tier; tiered approval modes instead of binary plan/build.

---

### Cursor

**Overview:** IDE-native coding agent (VS Code fork). Tight editor integration is the differentiator — the agent runs inside the editor and has full IDE context: diagnostics, symbol trees, open tabs.

**Standout capabilities:**
- Background Agent: long-running tasks run in a cloud VM, results returned to the editor asynchronously
- Sonic Tab: sub-100ms autocomplete trained on the repo's context
- Fast codebase indexing: semantic + keyword search across the whole repo
- MCP support for external tool servers
- Native diff UI: edits shown inline before acceptance

**Weaknesses:** No CLAUDE.md equivalent for versionable instructions; no formal permission rule syntax; Background Agent is Cursor-hosted only; no local LLM support.

**What Aegis could adopt:** Async/background task execution model (task runs detached, results polled or notified); inline diff review before commit.

---

### Devin (Cognition)

**Overview:** SaaS-first autonomous software engineer. Runs each task in a dedicated cloud VM with browser, shell, and editor. Designed for long-horizon tasks and parallelism.

**Standout capabilities:**
- Multi-VM parallel tasks: multiple Devins work in parallel on separate branches simultaneously
- DeepWiki: auto-generated, queryable knowledge base built from the repo — agents query it for architecture context without re-reading all files
- Persistent task memory across sessions
- Browser automation built-in (not just shell)
- ACU (Autonomous Compute Units) billing tracks compute consumed per task rather than per token

**Weaknesses:** SaaS-only, no self-hosted option; Cognition/OpenAI provider; expensive at scale; closed platform, no extensibility.

**What Aegis could adopt:** Persistent task-scoped knowledge base (DeepWiki-equivalent built from README + code index); browser/HTTP tool in the default tool set.

---

### Gemini CLI (Google)

**Overview:** Google's terminal agent, open-sourced mid-2025. Ships with 1M token context, native Google Search grounding, and a gVisor sandbox for shell execution.

**Standout capabilities:**
- 1M context window: effectively no context truncation for most repos
- Google Search tool built-in: grounded web retrieval without an external API key
- gVisor sandbox: lightweight OS-level kernel isolation for all shell tool calls
- MCP server support
- GEMINI.md instruction file: per-project config analogous to CLAUDE.md

**Weaknesses:** Officially Gemini-only (community OpenAI-compat adapter exists but unsupported); no multi-agent; no session persistence beyond a conversation file.

**What Aegis could adopt:** Grounded web search as a first-class built-in tool; sandboxed shell tier using gVisor or Docker; the instruction file pattern (Aegis already has CLAUDE.md support — this is already in parity).

---

### Windsurf (Codeium / acquired by Cognition)

**Overview:** IDE agent (VS Code fork) originally by Codeium, acquired by Cognition in 2025. Effectively becoming "Devin Desktop." Ships SWE-1, a coding-specialist fine-tune.

**Standout capabilities:**
- SWE-1 model: purpose-trained for software engineering, claims better SWE-bench results than general models
- Fast Context: rolling context window that up-weights recently-touched files
- Cascade: multi-step task execution with automatic rollback on tool failure
- Deep repo indexing similar to Cursor

**Weaknesses:** No local LLM; no formal permission layer; IDE-only surface; increasingly tied to Cognition/Devin platform lock-in.

**What Aegis could adopt:** Rolling context that up-weights recently edited files; automatic rollback on tool failure (compensating transaction in the agent loop).

---

### Amazon Q Developer

**Overview:** AWS-integrated coding agent in VS Code and JetBrains. End-of-life announced: support ends April 2027 as AWS migrates to Kiro. Included for completeness; not recommended as a model to follow.

**Standout capabilities:**
- Native AWS SDK tool: direct IAM-scoped AWS operations from the agent
- Built-in SAST scanning: security vulnerability detection as a first-class tool
- Deep AWS documentation grounding

**Weaknesses:** Deprecated (EOL April 2027); Amazon Bedrock only; IDE-only.

**What Aegis could adopt:** Built-in SAST scan tool as part of the security persona tool set (especially relevant given Aegis's security focus).

---

### LangGraph (LangChain)

**Overview:** Graph-based multi-agent orchestration framework. Each node is an agent or function; edges define control flow. Checkpointing makes every transition resumable and debuggable.

**Standout capabilities:**
- Per-node checkpointing: any graph state can be saved and restored; supports time-travel debugging (rewind to any past node state)
- 5-mode streaming: token, value, update, debug, custom — fine-grained event emission per step
- Human-in-the-loop at any edge: interrupt before/after any node without changing agent code
- LangSmith integration: full distributed trace per run, visualized in a web UI
- Subgraph composition: graphs embed inside other graphs as reusable modules

**Weaknesses:** Python-only; significant boilerplate for simple use cases; LangSmith observability is a paid SaaS add-on.

**What Aegis could adopt:** Structured trace events per turn (not just cost totals); checkpoint-based session resumability with rewind; configurable human-in-the-loop interruption points in the agent loop.

---

### AutoGen (Microsoft)

**Overview:** Actor-model multi-agent framework. Agents are stateful actors exchanging messages in rounds. Supports GroupChat (N agents discuss to consensus), nested conversations, and component serialization.

**Standout capabilities:**
- Actor model: agents are long-lived message-passing actors — natural for parallelism and isolation
- GroupChat: multiple agents debate and vote to reach consensus before producing output
- Component serialization: full agent config (tools, model, prompts) exportable as JSON for reproducibility and sharing
- GraphFlow: DAG-based workflow over actors, combining LangGraph-style structure with AutoGen's actor model
- AssistantAgent + UserProxyAgent pattern: separates LLM reasoning from tool execution cleanly

**Weaknesses:** Python-only; complex for simple tasks; message-passing overhead; observability requires external tooling.

**What Aegis could adopt:** Component serialization — agent configs (persona + tools + model) exported as reusable JSON templates; structured multi-turn conversation state separate from raw message history.

---

### CrewAI

**Overview:** Crew-based multi-agent framework. Crews are named groups of Agents with Roles; Tasks are assigned to agents with explicit tool restrictions. Dual guardrails and Flows add safety and workflow structure.

**Standout capabilities:**
- Dual guardrails: function guardrails (deterministic rule checks) + LLM guardrails (semantic output validation) — two independent safety layers on every task output
- Task-level tool restriction: each task declares exactly which tools its assigned agent may use — least-privilege by default
- Flows: event-driven workflow layer on top of Crews for conditional branching and loops
- Crew memory tiers: short-term (session), long-term (persistent), entity (extracted facts), and contextual memory
- Knowledge sources: structured RAG layer attached to agents or crews

**Weaknesses:** Python-only; YAML config becomes unwieldy at scale; some implicit routing magic in crew assignment.

**What Aegis could adopt:** Task-level tool restriction (persona + task pair declares the allowed tool subset); dual-layer output validation (rule-based + LLM semantic check); tiered memory model (short-term session vs. long-term persistent vs. entity graph).

---

### Smolagents (HuggingFace)

**Overview:** Minimal multi-agent framework from HuggingFace. The distinctive design: agents write and execute Python code as their "actions" rather than calling structured JSON tool schemas.

**Standout capabilities:**
- Code-as-actions: instead of `{"tool": "bash", "cmd": "ls"}`, the agent writes and executes Python directly — dramatically more expressive and composable without per-tool scaffolding
- ManagedAgent-as-tool: a sub-agent is just another callable tool from the coordinator's perspective — no special orchestration API required
- Step-level memory mutation: the agent can read and modify its own memory dict at each step, enabling dynamic context management
- Model-agnostic: works with HuggingFace Hub models, OpenAI-compat APIs, or any callable
- Minimal codebase: easy to fork and extend

**Weaknesses:** Code execution is a large attack surface (requires strong sandboxing); Python-only; no TUI or CLI surface; no session persistence.

**What Aegis could adopt:** ManagedAgent-as-tool pattern — expose a "run sub-agent" built-in tool that accepts a system prompt + user prompt and returns output, enabling orchestration without a dedicated orchestration layer; step-level scratchpad the agent can mutate.
