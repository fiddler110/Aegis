# Aegis Capability Roadmap
**Date:** 2026-06-29
**Source:** Competitive analysis against 12 frontier agent harnesses

---

## Gap Summary

| Category | Gap | Missing vs. |
|---|---|---|
| Orchestration | No sub-agent / delegation primitive | Claude Code, Devin, LangGraph, AutoGen, CrewAI, Smolagents |
| Security | No sandboxed execution environment | Codex CLI, Gemini CLI, Devin |
| Observability | No per-turn trace or audit log | LangGraph, AutoGen, CrewAI |
| Providers | No model-router abstraction for easy provider switching | Aider, LangGraph, AutoGen, CrewAI, Smolagents |
| Session UX | No `--resume <id>` CLI flag for named session reattach | Claude Code, Devin, LangGraph |
| Permission | No text-based allow/deny rules (versionable) | Claude Code |
| Context | No repo-map / codebase index for large repos | Aider, Cursor, Windsurf |
| Tooling | No grounded web search built-in | Gemini CLI, Devin |
| Tooling | No SAST scan tool | Amazon Q Developer |
| Memory | No tiered long-term / entity memory | CrewAI, Devin |
| Safety | No output validation layer | CrewAI |
| Async | No background / detached task execution | Cursor Background Agent, Devin |

---

## P1 — Critical

These gaps exist in the majority of competitors and represent table-stakes capabilities for a production agent harness.

### P1.1 — Sub-agent / orchestration primitive

**Gap:** Aegis has a single agent loop. There is no way for a persona to delegate a sub-task to another instance, run tasks in parallel, or compose specialized agents.

**Present in:** Claude Code (Agent tool), Devin (multi-VM), LangGraph, AutoGen, CrewAI, Smolagents.

**Recommended approach:** Add a `run_agent` built-in tool that accepts `system`, `prompt`, and optional `tools` list. The tool starts a nested engine run, streams events to the parent, and returns the final text output. This follows the Smolagents ManagedAgent-as-tool pattern — no new orchestration layer required. The existing `engine.Run` can be called recursively with depth limiting.

**Acceptance criteria:**
- `run_agent` tool available in all personas
- Nested runs respect the parent's budget and permission gate
- Depth limited to prevent infinite recursion (default max 3)

---

### P1.2 — Per-turn structured observability

**Gap:** Aegis tracks a cost total and turn count via `cost.Tracker`, but there is no per-turn token breakdown, no structured event log, and no way to replay or audit a session.

**Present in:** LangGraph (LangSmith), AutoGen (per-turn trace), CrewAI (task logs), Claude Code (usage stats per turn).

**Recommended approach:** Emit a structured `TurnTrace` record after each engine turn containing: turn index, model, input tokens, output tokens, cost USD, tool calls (name + duration), and wall time. Write records to the session's SQLite row as a JSON array. Expose a `aegis session trace <id>` CLI command to print them.

**Acceptance criteria:**
- `TurnTrace` struct with all fields above
- Traces written to SQLite alongside session messages
- `aegis session trace <id>` prints a table of turns
- Existing cost tracker remains the source of truth for budget enforcement

---

### P1.3 — Sandboxed execution tier

**Gap:** Aegis's permission gate approves or denies shell commands but does not contain their blast radius. An approved command runs with full host privileges.

**Present in:** Codex CLI (hardened Docker), Gemini CLI (gVisor), Devin (cloud VM).

**Recommended approach:** Add an optional `sandbox` permission mode alongside `plan` and `build`. In sandbox mode, shell tool calls (`bash`, `run_command`) are executed inside a Docker container that is created per-session, given a copy of the workspace, and destroyed after the session ends. File writes inside the sandbox are copied back to the host only after user review. On Windows, use WSL2 as the container backend.

**Acceptance criteria:**
- `--mode sandbox` flag accepted by CLI and daemon
- Shell commands run inside an isolated container
- Workspace changes staged for review before applying to host
- Sandbox mode documented in `--help`

---

### P1.4 — `--resume` UX for named session reattach

**Gap:** Sessions are stored in SQLite but there is no ergonomic way to resume a specific session from the CLI. Users must know the session UUID and there is no autocomplete or listing UX.

**Present in:** Claude Code (`--resume`, `--continue`), Devin (persistent SaaS tasks), LangGraph (checkpoint restore).

**Recommended approach:** Add `aegis session list` (already partially exists) and `aegis --resume <id-or-index>` to the TUI and chat commands. The TUI should offer a session picker on startup (similar to the persona picker) when no prompt is given. Session rows should store a human-readable title (already implemented via P1 title gen).

**Acceptance criteria:**
- `aegis session list` prints id, title, date, turn count
- `aegis --resume <id>` (or prefix match) reattaches in TUI
- TUI startup screen shows recent sessions when no prompt given

---

## P2 — Meaningful

Present in 2–4 competitors; high value but not table-stakes.

### P2.1 — Model-router abstraction

**Gap:** Adding a new LLM provider (e.g., Gemini, Mistral, Groq) requires writing a new adapter and wiring it into `providerfactory`. There is no config-level provider switching.

**Present in:** Aider (litellm, 100+ providers), LangGraph/AutoGen/CrewAI (any LangChain LLM), Smolagents (any HF or OpenAI-compat).

**Recommended approach:** Introduce an OpenAI-compatible adapter that accepts a base URL, API key, and model name from config. This single adapter covers Gemini (via OpenAI-compat endpoint), Groq, Mistral, Together.ai, and any other provider that exposes an OpenAI-compatible API. Ollama already uses this pattern — generalize it.

**Acceptance criteria:**
- `provider.type = "openai-compat"` in config with `base_url`, `api_key`, `model`
- Gemini and Groq confirmed working via this adapter
- Existing Anthropic and Ollama adapters unchanged

---

### P2.2 — Text-based permission rules

**Gap:** Permission mode is set globally (plan/build/sandbox). There is no way to write `allow bash(*)` or `deny write(/etc/*)` rules without code changes.

**Present in:** Claude Code (CLAUDE.md `allow`/`deny` patterns), Codex CLI (approval mode tiers).

**Recommended approach:** Parse an optional `[permissions]` section in the project CLAUDE.md (or a `.aegis/permissions.toml`). Rules are `allow <tool>(<pattern>)` or `deny <tool>(<pattern>)` and evaluated before the global mode gate. Glob patterns on tool input fields (e.g., `path` for file tools, `cmd` for bash).

**Acceptance criteria:**
- Permission rules parsed from CLAUDE.md or `.aegis/permissions.toml`
- Allow/deny evaluated before mode-level gate
- At least bash and file tool patterns supported
- Documented with examples

---

### P2.3 — Repo-map / codebase index

**Gap:** For large repos Aegis has no way to give the model a compact structural overview without reading every file. The agent must discover structure through tool calls.

**Present in:** Aider (repo-map from ctags), Cursor (semantic index), Windsurf (Fast Context).

**Recommended approach:** On session start (or on demand via `/index`), build a compact repo map: walk the repo, extract top-level symbols (functions, types, exports) using a lightweight parser or `ctags`, and write a summary file injected into the system prompt. Cap at ~2000 tokens. Refresh on file changes.

**Acceptance criteria:**
- `aegis index` command builds and caches a repo map
- Repo map injected into system prompt when present
- Map size capped and truncated gracefully
- Map invalidated when files change (mtime check)

---

### P2.4 — Dual-layer output validation

**Gap:** Aegis has no mechanism to validate agent outputs before returning them to the user or committing tool effects. A persona can produce malformed or incomplete output with no guard.

**Present in:** CrewAI (function guardrails + LLM guardrail per task), AutoGen (human-in-loop per turn).

**Recommended approach:** Add an optional `output_guard` field to persona config. Two modes: `schema` (validate output matches a JSON schema — useful for structured-output personas like data-analyst) and `llm` (a second, cheap LLM call that checks the output against a rubric and returns pass/fail + reason). Both modes surface the result to the engine, which can retry up to N times before surfacing the raw output with a warning.

**Acceptance criteria:**
- `output_guard` field in persona config (optional)
- Schema validation mode working for structured personas
- LLM validation mode working with a configurable rubric
- Up to 3 automatic retries on guard failure
- Warning surfaced in TUI when guard triggers

---

### P2.5 — Grounded web search built-in

**Gap:** Aegis has no first-class web search tool. Research tasks require manual URL fetching, which is slower and less discoverable than a search primitive.

**Present in:** Gemini CLI (Google Search), Devin (web browsing).

**Recommended approach:** Add a `web_search` built-in tool that accepts a query string and returns the top N results (title, URL, snippet). Backend: Brave Search API or SerpAPI (configurable API key in config). Inject results as a tool result block; model can then fetch specific URLs with the existing fetch tool.

**Acceptance criteria:**
- `web_search` tool available in all personas
- API key configured in `config.toml` under `[tools.web_search]`
- Returns top 5 results by default, configurable
- Tool result includes title, URL, and snippet per result

---

## P3 — Exploratory

Long-horizon or niche capabilities worth tracking. No immediate implementation commitment.

### P3.1 — Tiered long-term memory

**Gap:** Aegis memory is session-scoped (SQLite) and project-scoped (CLAUDE.md files). There is no persistent entity memory or cross-session factual store.

**Present in:** CrewAI (short-term / long-term / entity / contextual tiers), Devin (persistent task memory).

**Notes:** Entity memory (extracted facts about people, codebases, decisions) is the highest-value tier for a security persona that accumulates knowledge about a target system across sessions. Consider a SQLite-backed entity store keyed by `(project, entity_type, entity_name)`.

---

### P3.2 — Async / background task execution

**Gap:** All Aegis sessions are synchronous — the user waits for the agent to finish. Long-running tasks (full repo audit, multi-file refactor) block the TUI.

**Present in:** Cursor (Background Agent in cloud VM), Devin (async SaaS tasks).

**Notes:** The daemon architecture already separates client from server. Extending this to support detached sessions (start a task, detach, reattach later) is architecturally straightforward but requires TUI state management for async notifications.

---

### P3.3 — Component serialization / persona templates

**Gap:** Personas are Go constants. Sharing a custom persona configuration (persona + tools + model + permission rules) requires distributing source code or CLAUDE.md snippets.

**Present in:** AutoGen (component serialization to JSON), CrewAI (YAML crew definitions).

**Notes:** A `persona.toml` format that combines persona system prompt, allowed tools, model override, and permission rules would enable users to share configurations as files without code changes. This builds on the model-router abstraction (P2.1) and text-based permission rules (P2.2).

---

### P3.4 — DeepWiki-style project knowledge base

**Gap:** Devin maintains a queryable knowledge base auto-generated from the repo. Long-running engagement with a codebase benefits from accumulated structural knowledge that survives context window limits.

**Present in:** Devin (DeepWiki).

**Notes:** A lightweight version would be a project-level SQLite FTS index populated from code comments, README files, commit messages, and documentation. The agent queries it via a `project_knowledge` tool rather than re-reading files on every session.

---

### P3.5 — Automatic rollback on tool failure

**Gap:** When a sequence of tool calls partially fails (e.g., three files written, fourth write errors), Aegis leaves the workspace in a partial state. No rollback mechanism exists.

**Present in:** Windsurf (Cascade with automatic rollback).

**Notes:** Git-native rollback is the simplest implementation: checkpoint the workspace with a commit before a multi-step task begins; on unrecoverable failure, offer `git reset --hard` to the checkpoint. Requires explicit task boundaries, which ties to the sub-agent primitive (P1.1).
