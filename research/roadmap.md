# Aegis Capability Roadmap
**Date:** 2026-06-29
**Updated:** 2026-07-02 (v6 — P3.1–P3.4, P3.6, P3.7 shipped)

---

## Completed

**P2 (all 9 items shipped 2026-07-01):**
- P2.1 Ripgrep + `ls` directory tree tool
- P2.2 Bang `!` shell mode in TUI
- P2.3 Frecency-ranked @mention file autocomplete
- P2.4 File-change tracking in sidebar
- P2.5 Subagent footer strip
- P2.6 Max-step graceful degradation
- P2.7 Proactive context compaction (85% headroom check)
- P2.8 Conversation timeline dialog (`/timeline`)
- P2.9 Workflow agent primitives (sequential / parallel / loop)

**P3 (shipped 2026-07-02):**
- P3.1 Tiered long-term memory — SQLite FTS5 entity store (`internal/longmem`); `entity_remember` / `entity_recall` tools; ADK `BaseMemoryService`-compatible interface
- P3.2 Async/background task execution — `/detach` TUI command; daemon persists session to `bg_events` table; `aegis bg list/events` CLI; detached context survives TUI disconnect
- P3.3 DeepWiki-style project knowledge base — SQLite FTS5 index of docs/comments (`internal/knowledge`); `project_knowledge` tool with BM25 ranking and snippet extraction
- P3.4 Automatic rollback on tool failure — `git_sha` captured per checkpoint; `/rollback` TUI command runs `git reset --hard <sha>`; `GitRollback` flag on `RewindRequest`
- P3.6 Typed tool output schemas — optional `OutputSchemer` interface on `Tool`; `OutputSchema json.RawMessage` on `ToolSchema`; all built-in tools declare output schemas
- P3.7 Animation pause off-screen — spinner tick suppressed when `followBottom` is false; animation resumes automatically on scroll-back

---

## Remaining Gaps

| Category    | Gap                                                             | Missing vs.        |
| ----------- | --------------------------------------------------------------- | ------------------ |
| TUI         | No `@file#start-end` line-range syntax in @mentions             | OpenCode           |
| Persistence | No mid-turn state persistence on crash                          | Crush, OpenCode    |
| TUI         | No draft stash across sessions                                  | OpenCode           |
| Interop     | No A2A protocol support for cross-framework agent communication | ADK Go 2.0         |

---

## P3 — Exploratory

Long-horizon or niche capabilities worth tracking; no immediate implementation commitment.

### ✓ P3.1 — Tiered long-term memory (shipped 2026-07-02)

**Gap:** Aegis memory is session-scoped (SQLite) and project-scoped (CLAUDE.md). No persistent entity memory or cross-session factual store.

**Present in:** CrewAI (short-term / long-term / entity / contextual tiers), Devin (persistent task memory). ADK Go 2.0 (`BaseMemoryService` — `AddSession(ctx, session)` + `SearchMemory(ctx, query)` interface, backed by in-memory or vector store implementations).

**Notes:** Entity memory (extracted facts about target systems, codebases, and decisions) is highest-value for security personas that accumulate knowledge about a target across sessions. A SQLite-backed entity store keyed by `(project, entity_type, entity_name)` would be a natural extension of the existing session store. **Model the Go interface on ADK's `BaseMemoryService`** — two methods (`AddSession`, `SearchMemory`) — so that the implementation is compatible with future A2A interop (see P3.9) and familiar to anyone who has used ADK. The SQLite FTS5 extension covers the `SearchMemory` side without adding a vector database dependency.

---

### ✓ P3.2 — Async / background task execution (shipped 2026-07-02)

**Gap:** All sessions are synchronous. Long-running tasks (full-repo audit, multi-file refactor) block the TUI.

**Present in:** Cursor (Background Agent in cloud VM), Devin (async SaaS tasks).

**Notes:** The daemon architecture already separates client from server. Detached sessions (start, detach, reattach later) are architecturally straightforward but require TUI push notifications for async completions and state management for re-attach. Ties to the existing swarm/subprocess infrastructure.

---

### ✓ P3.3 — DeepWiki-style project knowledge base (shipped 2026-07-02)

**Gap:** No queryable knowledge base auto-generated from the repo. Re-reading files on every session discards accumulated structural knowledge.

**Present in:** Devin (DeepWiki).

**Notes:** A project-level SQLite FTS index populated from code comments, README files, commit messages, and documentation, queried via a `project_knowledge` tool. The repo-map (already built) provides the structural skeleton; this adds semantic content depth.

---

### ✓ P3.4 — Automatic rollback on tool failure (shipped 2026-07-02)

**Gap:** Partial failures leave the workspace in an inconsistent state. No rollback mechanism exists.

**Present in:** Windsurf (Cascade with automatic rollback).

**Notes:** Git-native rollback is the simplest approach: checkpoint the workspace with a commit before a multi-step task begins; on unrecoverable failure, offer `git reset --hard` to the pre-task commit. Requires explicit task boundaries — ties to the sub-agent primitive (P2.9, shipped).


---

### ✓ P3.6 — Typed tool output schemas (shipped 2026-07-02)

**Gap:** Aegis tools return raw strings. OpenCode tools declare Effect Schema for both input and output; the harness validates and serializes cleanly.

**Notes:** Adding a typed output schema to the `Tool` interface would enable structured output parsing, tool result validation, and richer client rendering without string parsing. Low immediate user impact; high architectural cleanliness. Would require a new `OutputSchema() json.RawMessage` method on the interface and corresponding changes to all built-in tools.

---

### ✓ P3.7 — Animation pause off-screen (shipped 2026-07-02)

**Gap:** The shimmer animation and spinner tick unconditionally, triggering redraws even when the animated content is scrolled off-screen.

**Present in:** Crush (`pausedAnimations` map — animation ticks disabled for items not visible in the viewport, resume on scroll-back).

**Notes:** Minor performance improvement; most relevant for slow terminals or very long sessions. Track whether the "● thinking…" line is within the visible viewport range; suppress `sp.Tick` commands when it is not. Low priority but clean.

---

### P3.8 — Draft stash

**Gap:** Aegis has in-memory input history but no persistent draft save across sessions. Typed but unsent long-form prompts are lost on restart.

**Present in:** OpenCode (`stash.tsx` — saves and restores draft messages across sessions).

**Notes:** A simple `.aegis/stash.json` file storing the last N unsent draft messages per session (keyed by session ID) would suffice. Low priority but useful for long-form prompts interrupted by a restart or daemon cycle.

---

### P4.1 — Mid-turn state persistence

**Gap:** If the process crashes mid-turn, the partial turn (accumulated assistant text, tool calls received) is lost. Crush and OpenCode commit incremental state to SQLite during streaming.

**Notes:** The session layer persists completed turns to SQLite. Extending this to write partial state per streaming callback (assistant text accumulated so far, tool calls received but not yet dispatched) would require threading the session store into the engine or reusing the checkpoint infrastructure. High implementation complexity for a relatively low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.


---

### P4.2 — ADK Agent-to-Agent (A2A) protocol integration

**Source:** ADK Go 2.0 A2A protocol (GA June 2026).

**Gap:** Aegis agents communicate only within the process boundary (goroutines) or via subprocess IPC. There is no standardized way for Aegis agents to interoperate with agents built on other frameworks (ADK, LangGraph, CrewAI).

**What ADK provides:** A2A is a lightweight HTTP+SSE protocol for agent discovery and inter-agent task delegation. An A2A-compatible agent exposes a discovery endpoint (`.well-known/agent.json` — its "agent card") and a task submission endpoint. Any agent on the network can call it by posting a task and streaming the response, exactly as a tool call. This makes cross-framework agent composition possible without a shared SDK.

**Notes:** Implementing A2A would allow Aegis to operate as both:
- **A2A client** — a new `a2a_agent` built-in tool that calls remote ADK (or any A2A-compatible) agents as if they were local sub-agents; results flow back into the transcript like any tool result
- **A2A server** — expose the existing HTTP/SSE API as an A2A-compliant endpoint so Aegis sessions are callable by external ADK orchestrators

The existing daemon HTTP server maps cleanly to A2A's server-side shape. On the client side, `a2a_agent` takes a `url` (the remote agent's base URL) and `task` string, making it composable with the P2.9 workflow modes. No Go ADK SDK dependency required — A2A is a protocol, not a library. Long-horizon; depends on P2.9 (workflow primitives, shipped) and P3.1 (shared memory interface) being stable first.
