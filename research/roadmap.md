# Aegis Capability Roadmap
**Date:** 2026-06-29
**Updated:** 2026-07-03 (v12 — P6.4 context editing/tool-result pruning shipped; remaining open work: P6.1/P6.2/P6.3/P6.5)

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

**P3 (all 6 items shipped 2026-07-02):**
- P3.1 Tiered long-term memory — SQLite FTS5 entity store (`internal/longmem`); `entity_remember` / `entity_recall` tools; ADK `BaseMemoryService`-compatible interface
- P3.2 Async/background task execution — `/detach` TUI command; daemon persists session to `bg_events` table; `aegis bg list/events` CLI; detached context survives TUI disconnect
- P3.3 DeepWiki-style project knowledge base — SQLite FTS5 index of docs/comments (`internal/knowledge`); `project_knowledge` tool with BM25 ranking and snippet extraction
- P3.4 Automatic rollback on tool failure — `git_sha` captured per checkpoint; `/rollback` TUI command runs `git reset --hard <sha>`; `GitRollback` flag on `RewindRequest`
- P3.6 Typed tool output schemas — optional `OutputSchemer` interface on `Tool`; `OutputSchema json.RawMessage` on `ToolSchema`; all built-in tools declare output schemas
- P3.7 Animation pause off-screen — spinner tick suppressed when `followBottom` is false; animation resumes automatically on scroll-back

**P4 — Core Harness Parity (all 6 items shipped 2026-07-02):**
- P4.3 Skills progressive disclosure — `internal/skills` now injects a compact `<skills_available>` index (name + frontmatter `description:`); a `skill` builtin tool loads the full body on demand. Description-less skills fall back to eager injection.
- P4.4 User-configurable lifecycle hooks — `hooks:` config maps `pre_tool_use`/`post_tool_use`/`session_start`/`stop`/`subagent_stop` to shell commands (`internal/hooks` `Exec`); JSON event on stdin, exit 2 vetoes with stderr surfaced.
- P4.5 Headless structured output — `aegis chat --output-format text|json|stream-json`.
- P4.6 Deferred tool loading — `tool.Registry` gained `RegisterDeferred`/`Deferred`/`Load`/`SearchDeferred`; niche tools (latex, diagram, cron, lsp, longmem, team) are advertised as a `<deferred_tools>` one-liner and loaded via the `tool_search` meta-tool.
- P4.7 OS-level sandbox — `sandbox.backend: os` confines the local shell via macOS seatbelt / Linux bwrap; reported by `aegis sandbox detect`.
- P4.8 Close the loop — `git_pr` tool pushes the branch and opens a PR via `gh`, with a GitHub compare-URL fallback.

**P5.1–P5.7 (all shipped 2026-07-02):**
- P5.1 Agent teams — SQLite-backed shared task list (`swarm.TaskList`, `team_task_*` tools with atomic claim) + peer messaging (`team_send`/`team_inbox` over the file mailbox).
- P5.2 LSP tools — added `definition`, `hover`, `document_symbols`, `workspace_symbols`, `call_hierarchy` (registered deferred).
- P5.3 Pluggable web search — `search:` config selects brave/tavily/searxng; DuckDuckGo scrape remains the zero-config fallback.
- P5.4 Background notifications — `notify:` config fires desktop (osascript/notify-send/toast) and/or webhook on background-session completion/error.
- P5.5 @file#L10-40 line-range mentions — server expands `@path#L10-40` tokens in user messages to inline file excerpts before the engine call.
- P5.6 Draft stash — unsent textarea content saved to `.aegis/stash.json` on quit; restored on next session start.
- P5.7 Bundle install from git URL — `aegis bundle install/info <git-url>` clones `--depth=1` to temp dir and installs as a normal local bundle.

**P5.8–P5.9 (shipped 2026-07-02):**
- P5.8 Semantic recall layer — `internal/embed` (Ollama `/api/embed` client, cosine similarity, reciprocal-rank fusion); `knowledge.Store` and `longmem.Store` gained an optional `Embedder` and a `docs_vec`/`mem_vec` BLOB vector table; `Search`/`SearchMemory` fuse BM25 + semantic rankings via RRF when `embeddings.enabled: true`, else BM25-only (default). `aegis knowledge index` CLI command added. Along the way, fixed a real gap: `knowledge.Store`/`longmem.Store` were built but never opened by the daemon — `project_knowledge`/`entity_remember`/`entity_recall` were dead tools; now wired into `internal/server`.
- P5.9 Provider failover — `provider.WithFailover` chains a primary adapter with ordered fallback targets, switching only on synchronous Stream failure after each target's own retry budget is exhausted (never mid-stream, so no partial output is replayed). `provider.fallback` config (ordered provider/model/base_url entries) + `provider.allow_cloud_fallback` guard: local→cloud failover is skipped with a warning unless explicitly opted in; cloud→cloud and any→local are never gated. `providerfactory.Build` assembles the chain.

**TUI quality track quick wins (shipped 2026-07-02):**
- TQ2 Real unified diffs — LCS-based Myers diff in `toolview.go`; context lines, `+`/`-` markers, `@@ ... @@` separators between hunks. Replaces delete-all/add-all.
- TQ4a/b Copy affordances — `/copy` copies last assistant message via `pbcopy`/`xclip`/`clip.exe`; `/copy N` copies Nth fenced code block; toast confirmation.
- TQ5 Toggleable sidebar — `sidebarOpen bool` (default off); `ctrl+b` / `/sidebar` toggle; context %, cost, agent count folded into status bar when hidden.
- TQ7 Live todo strip — intercepts `todo_add`/`todo_update`/`todo_list` tool results; renders `▣▶▢` progress strip above input with in-progress task text.

**TQ3/TQ6/TQ8/TQ9/TQ10 (shipped 2026-07-03 — TQ track complete):**
- TQ3 Streaming markdown — the live tail renders through glamour incrementally: `liveBlock.render` takes a markdown-render callback (`model.mdRender`, trailing newlines normalized so settled-prefix + tail concatenation is byte-identical to a whole-source render); the settled prefix up to the last safe boundary is cached, only the growing tail re-renders per token. The end-of-turn restyle "pop" is gone — `flushLiveText` produces the same styled output the user was already watching.
- TQ6 Richer approval flow — y/a/n banner replaced by an option-list dialog (`internal/tui/approval.go`): ↑/↓ + enter or y/a/n/f shortcuts across `Allow once / Allow always for pattern / Deny / Deny with feedback`. Edits/writes/shell show the TQ2 diff or command block as the preview. "Allow always" derives a scoped pattern (`suggestRulePattern`: `npm test -v` → `npm test*`, file paths → directory glob, URLs → host) and sends it as `ApproveRequest.Pattern`; the server installs `allow <tool>(<pattern>)` into the live rule set (`Server.addPermissionRule`, mutex-guarded) and persists it to `.aegis/config.yaml → permission.rules` (`config.AppendProjectPermissionRule`, dedup + key-preserving) so it survives restarts. Pattern-less allow-always keeps the old per-tool session cache. "Deny with feedback" sends the denial then injects the typed reason via the buffered steer channel so the model learns *why*.
- TQ8 Message queueing — `alt+enter` during streaming queues the draft as the next user turn (dimmed `⏳ queued ▸` block below the live tail); queued messages auto-send one per completed run at stream close. Plain enter keeps steer semantics. Explicit cancel (esc-esc / ctrl+c) or a stream error discards the queue rather than auto-sending into a broken run.
- TQ9 Input polish bundle — `shift+enter` newline via the textarea keymap (bubbletea v2 requests Kitty key disambiguation by default; `ctrl+j` fallback kept); pasted image paths (`tea.PasteMsg`, quoted or bare, `.png/.jpg/.jpeg/.gif/.webp/.bmp`) become `@image:` attachment tokens automatically — `extractImageRefs` now regex-based with quoted-path support, and no longer flattens newlines in messages; ↑/↓ move the cursor inside a multiline draft with history navigation only at the first/last line; thinking blocks collapse to `✻ thought for Ns  (ctrl+o to expand)` — ctrl+o swaps every thinking block between collapsed/expanded in place via `transcript.setBlockRaw` (byte-accounting exact).
- TQ10 Theme system — the hardcoded Charmtone palette moved behind `colorScheme` (`internal/tui/colorscheme.go`) with `darkScheme` (identical to the old Pantera look) and `lightScheme` built-ins; `tui.theme` config key ("dark"/"light", default dark) applied by `tui.Run` before styles are built; the matching glamour markdown style ("dark"/"light") follows the scheme, as does the ANSI-16 shell-output remap. Terminal-background auto-detection remains a stretch goal.

**TQ1 (shipped 2026-07-02):**
- TQ1 Block-based transcript model — `internal/tui/transcript.go`: `transcriptBlock` (raw ANSI content + per-block width-keyed wrap cache) replaces the old whole-buffer `cappedBuffer`/`wrapCache`; `liveBlock` keeps the settled-prefix boundary-cache trick (previously scattered `liveWrapCache*` fields on `model`) so a long streaming reply stays O(tail) per token. Trimming now drops whole blocks (with a one-time "[earlier output trimmed]" marker held outside the evictable slice) instead of severing content mid-line. Resize/theme changes self-correct lazily — each block just notices its cached width is stale on next render, no explicit invalidation needed. The timeline picker's scroll-to-turn moved from a byte offset into `cappedBuffer.buf` to a block index (`transcript.renderUpTo`). Verified via `internal/tui/transcript_test.go` (block cache, trim, live-block boundary caching) and `internal/tui/integration_test.go`, which drives a full turn (thinking → streamed text → tool call/result → turn done) plus two resizes through the real `Update`/`applyEvent`/`refresh`/`View` path — interactive PTY tools (tmux, winpty) weren't available in this sandbox, so this scripted drive substitutes for a live terminal session.

---

## 2026-07 Landscape Review

What changed in the top-tier harnesses since the 2026-06-29 competitive analysis, and what it means for Aegis.

**Claude Code** (the closest architectural relative):
- **Agent Teams** (Feb 2026, with Opus 4.6) — peer sessions that message each other directly, claim tasks from a shared task list, and challenge each other's findings. Distinct from subagents (which report up to a parent). Aegis's swarm mailbox is the right substrate but lacks the shared task list and peer messaging semantics.
- **Skills with progressive disclosure** — only skill *name + description* load at session start; the full body loads on invocation. Aegis injects every skill's full markdown into the system prompt on every session (`internal/skills`), which does not scale.
- **Lifecycle hooks as user config** — shell commands / HTTP endpoints / LLM prompts firing on `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `Notification`; exit code 2 vetoes a tool call. Aegis has an internal Go hook interface + audit JSONL, but nothing user-configurable without recompiling.
- **Deferred tools / ToolSearch** — tool schemas are lazy-loaded via a search meta-tool instead of shipping every schema every turn.
- **Dispatch & Channels** — programmatic task submission via API plus event streams for dashboards/alerting.
- **Background agents finish the job** — commit, push, and open a draft PR when code work completes in a worktree.

**opencode** — 75+ providers via Models.dev; TypeScript plugin system with 25+ lifecycle hooks; experimental LSP *tool* (go-to-definition, references, hover, call hierarchy — not just diagnostics); session share links; desktop app + IDE extension.

**Codex CLI** — default-on sandboxing (container, plus OS-level seatbelt/Landlock when no container); headline **token efficiency** (~4× fewer tokens than peers on Terminal-Bench); runs as an MCP *server*; native GitHub Actions + auto-PR; Rust rewrite.

**Gemini CLI** — 1M context standard; Google Search grounding; 90+ extensions; subagents with parallel delegation (Apr 2026); being folded into the Antigravity platform.

**Convergent themes across all four:** (1) token efficiency as a first-class metric, (2) user-configurable lifecycle hooks, (3) lazy/progressive context loading (skills, tools, docs), (4) headless/programmatic operation with structured output, (5) forge integration that completes the loop (PR out the other end), (6) sandboxing that doesn't require Docker.

**Where Aegis is already at or ahead of parity** (no action needed): prompt-cache breakpoints in the Anthropic adapter; per-turn structured traces + cost budget enforcement; checkpoints/rewind + git rollback; output validation guard (LLM rubric + schema modes); cron scheduling; container sandbox matrix (Docker/Podman/WSL/Apple); ACP editor protocol; local-LLM-first provider posture; 17 security personas + contextual security policies (egress-then-write); audit trail.

---

## Gap Analysis

| # | Category | Gap | Present in | Severity | Status |
|---|----------|-----|-----------|----------|--------|
| 1 | Context efficiency | Skills fully injected into system prompt (no progressive disclosure) | Claude Code | **High** | ✅ Done (P4.3) |
| 2 | Extensibility | No user-configurable lifecycle hooks (config-driven shell/HTTP hooks with veto) | Claude Code, opencode | **High** | ✅ Done (P4.4) |
| 3 | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading | Claude Code (ToolSearch) | **High** | ✅ Done (P4.6) |
| 4 | Automation | Headless `aegis chat` emits plain text only — no JSON / stream-JSON output for scripting & CI | Claude Code (`-p --output-format`), Codex | **High** | ✅ Done (P4.5) |
| 5 | Safety | Local sandbox backend = no isolation; OS-level sandboxing (seatbelt/Landlock) requires no Docker | Codex CLI (default-on) | **High** | ✅ Done (P4.7) |
| 6 | Workflow | Git tool stops at commit; no push / PR creation; background sessions don't close the loop | Claude Code, Codex | **High** | ✅ Done (P4.8) |
| 7 | Multi-agent | Subagents report up only; no shared task list, task claiming, or peer messaging | Claude Code Agent Teams | Medium | ✅ Done (P5.1) |
| 8 | Tools | LSP tools = diagnostics + references only; no hover/definition/symbols/call-hierarchy | opencode | Medium | ✅ Done (P5.2) |
| 9 | Tools | Web search scrapes DuckDuckGo HTML — brittle, rate-limited, ungrounded | Gemini (Search grounding), Claude Code | Medium | ✅ Done (P5.3) |
| 10 | Automation | No notification channel (desktop/webhook) when a detached session finishes or needs input | Claude Code (Notification hook, Channels) | Medium | ✅ Done (P5.4) |
| 11 | TUI | No `@file#start-end` line-range syntax in @mentions | opencode | Low | ✅ Done (P5.5) |
| 12 | TUI | No draft stash across sessions | opencode | Low | ✅ Done (P5.6) |
| 13 | Persistence | No mid-turn state persistence on crash | Crush, opencode | Low | ⬜ P6.1 |
| 14 | Interop | No A2A protocol; Aegis also cannot act as an MCP *server* | ADK, Codex (MCP server mode) | Low | ⬜ P6.2/P6.3 |
| 15 | Extensibility | Bundles install from local path only — no git-URL install or shared index | opencode plugin ecosystem | Low | ✅ Done (P5.7) |
| 16 | Memory | Knowledge/longmem retrieval is BM25-only; no semantic (embedding) recall | Cursor, Devin | Low | ✅ Done (P5.8) |
| 17 | Reliability | No provider failover — an outage on the configured provider halts everything | Aider (litellm routing) | Low | ✅ Done (P5.9) |

---

## P5 — Remaining Items

### P5.8 — Semantic recall layer (optional embeddings)
**Gap #16.** Optional embedding index over `internal/knowledge` and `internal/longmem` using a local embedding model via Ollama (`/api/embed`), merged with BM25 via reciprocal-rank fusion. Strictly opt-in — keeps zero-dependency FTS5 as the default.
**Effort:** Medium.

### P5.9 — Provider failover
**Gap #17.** `provider.fallback` config listing ordered (provider, model) pairs; on repeated transport-level failure (exhausted `retry.go` budget) the factory swaps adapters mid-session and logs the switch. Guard: never silently fail over across trust boundaries (cloud → local ok; local → cloud requires explicit opt-in).
**Effort:** Medium.

---

## TQ — TUI Quality Track

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience. The recurring theme: Aegis renders the conversation as **one append-only styled string** (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a **list of typed message blocks** rendered and cached individually. Most of the "less clean" feel traces back to that one structural difference; the rest is diff quality, streaming markdown, and interaction polish.

### Completed

| # | Item | Shipped |
|---|------|---------|
| TQ2 | Real unified diffs (LCS Myers, context lines, hunk headers) | 2026-07-02 |
| TQ4a/b | `/copy` last message + `/copy N` nth code block | 2026-07-02 |
| TQ5 | Toggleable sidebar, default off; `ctrl+b` / `/sidebar`; context bar in status line | 2026-07-02 |
| TQ7 | Live todo strip (`▣▶▢`) above input, intercepts todo tool results | 2026-07-02 |
| TQ1 | Block-based transcript model (`internal/tui/transcript.go`: `transcriptBlock`/`transcript`/`liveBlock`) | 2026-07-02 |
| TQ3 | Streaming markdown — live tail renders through glamour incrementally from the last safe boundary; no end-of-turn restyle pop | 2026-07-03 |
| TQ9 | Input polish: `shift+enter` newline, image-path paste → attachment token, cursor-aware ↑/↓ history, collapsible `✻ thought for Ns` blocks (`ctrl+o`) | 2026-07-03 |
| TQ8 | Message queueing — `alt+enter` queues next turn during streaming; dimmed pending blocks auto-send at stream close | 2026-07-03 |
| TQ6 | Option-list approval dialog with diff preview; "allow always" writes a pattern-scoped `permission.rules` entry persisted to `.aegis/config.yaml`; deny-with-feedback steers the reason back to the model | 2026-07-03 |
| TQ10 | Theme system — `colorScheme` with dark + light built-ins, `tui.theme` config key, glamour style + ANSI-16 remap follow the scheme | 2026-07-03 |

The TQ track is complete. Remaining cosmetic stretch ideas (not scheduled): TQ4c scoped mouse capture, terminal-background auto-detection for theme selection.

---

## P6 — Long-Horizon / Exploratory

### P6.1 — Mid-turn state persistence *(was P4.1)*
Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.

### P6.2 — A2A protocol integration *(was P4.2)*
Agent-to-Agent HTTP+SSE protocol (ADK Go 2.0, GA June 2026): `a2a_agent` client tool for calling remote agents + expose the daemon as an A2A server (`.well-known/agent.json` discovery). No SDK dependency — it's a protocol. Depends on P5.1 being stable.

### P6.3 — MCP server mode
Expose Aegis itself as an MCP server (`aegis mcp-serve`): sessions and selected tools become MCP tools callable from other harnesses (Claude Code, Codex, editors). Complements A2A; the daemon API maps cleanly. Codex already does this and it materially expands where the harness can be embedded.

### P6.4 — Context editing / tool-result pruning ✅ Done 2026-07-03
`compaction.pruneStaleToolResults` (`internal/compaction/prune.go`) runs as a deterministic pre-pass inside `Summarizer.Compact`, before any LLM call: `read_file` results for a path that was read again later are blanked to a one-line marker, and large `grep`/`glob`/`ls` dumps outside the trailing `keepRecent` window are truncated to a short preview. Never touches conversational text, tool errors, or the recent window. If pruning alone brings the estimate back under budget, `Compact` returns immediately — no summarizer call, no LLM cost.

### P6.5 — Desktop / IDE surface beyond ACP
ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Only worth it if user demand materializes — the TUI is the product.

---

## Recommended Next Steps

```
Done 2026-07-02:  P5.8 semantic recall → P5.9 provider failover → TQ1 block transcript
Done 2026-07-03:  TQ3 streaming markdown → TQ9 input polish → TQ8 queueing → TQ6 approvals → TQ10 themes → P6.4 context editing
Long-horizon:     P6.1 mid-turn persistence → P6.2 A2A → P6.3 MCP server → P6.5 desktop/IDE surface
```

With the TQ track and P6.4 complete, the remaining P6 items are all exploratory/low-probability-payoff: P6.1 (crash-during-turn persistence) has no reported pain point yet; P6.2/P6.3 (A2A, MCP server mode) are interop bets with no current consumer; P6.5 (desktop/IDE surface) is speculative pending user demand. None are blocking — revisit if a concrete need surfaces.

---

## Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
