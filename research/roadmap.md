# Aegis Capability Roadmap
**Date:** 2026-06-29
**Updated:** 2026-07-02 (v8 — TUI quality track added from code-level review vs. Claude Code / opencode; v7 added P4/P5/P6 harness waves)

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

| # | Category | Gap | Present in | Severity |
|---|----------|-----|-----------|----------|
| 1 | Context efficiency | Skills fully injected into system prompt (no progressive disclosure) | Claude Code | **High** |
| 2 | Extensibility | No user-configurable lifecycle hooks (config-driven shell/HTTP hooks with veto) | Claude Code, opencode | **High** |
| 3 | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading | Claude Code (ToolSearch) | **High** |
| 4 | Automation | Headless `aegis chat` emits plain text only — no JSON / stream-JSON output for scripting & CI | Claude Code (`-p --output-format`), Codex | **High** |
| 5 | Safety | Local sandbox backend = no isolation; OS-level sandboxing (seatbelt/Landlock) requires no Docker | Codex CLI (default-on) | **High** |
| 6 | Workflow | Git tool stops at commit; no push / PR creation; background sessions don't close the loop | Claude Code, Codex | **High** |
| 7 | Multi-agent | Subagents report up only; no shared task list, task claiming, or peer messaging | Claude Code Agent Teams | Medium |
| 8 | Tools | LSP tools = diagnostics + references only; no hover/definition/symbols/call-hierarchy | opencode | Medium |
| 9 | Tools | Web search scrapes DuckDuckGo HTML — brittle, rate-limited, ungrounded | Gemini (Search grounding), Claude Code | Medium |
| 10 | Automation | No notification channel (desktop/webhook) when a detached session finishes or needs input | Claude Code (Notification hook, Channels) | Medium |
| 11 | TUI | No `@file#start-end` line-range syntax in @mentions *(carried from v6)* | opencode | Low |
| 12 | TUI | No draft stash across sessions *(carried, P3.8)* | opencode | Low |
| 13 | Persistence | No mid-turn state persistence on crash *(carried, P4.1)* | Crush, opencode | Low |
| 14 | Interop | No A2A protocol *(carried, P4.2)*; Aegis also cannot act as an MCP *server* | ADK, Codex (MCP server mode) | Low |
| 15 | Extensibility | Bundles install from local path only — no git-URL install or shared index | opencode plugin ecosystem | Low |
| 16 | Memory | Knowledge/longmem retrieval is BM25-only; no semantic (embedding) recall | Cursor, Devin | Low |
| 17 | Reliability | No provider failover — an outage on the configured provider halts everything | Aider (litellm routing) | Low |

---

## P4 — Core Harness Parity (high priority)

The items that most affect day-to-day quality, cost, and trust. Ordered by recommended implementation sequence.

### P4.3 — Skills progressive disclosure
**Gap #1.** Replace eager system-prompt injection with a two-stage model: at session start inject only a compact index (`name — description`, requiring a frontmatter `description:` field in skill files); add a `skill` built-in tool that loads the full body on invocation. Backward compatible — description-less legacy skills can fall back to eager injection.
**Why first:** directly reduces per-turn input tokens for every user with more than one or two skills; token efficiency is the 2026 headline metric. Touches `internal/skills` + one new builtin tool.
**Effort:** Small.

### P4.4 — User-configurable lifecycle hooks
**Gap #2.** Config-driven external hooks: `hooks:` section in config.yaml mapping lifecycle events (`pre_tool_use`, `post_tool_use`, `session_start`, `stop`, `subagent_stop`) to shell commands. Contract mirrors Claude Code: JSON event on stdin; exit 0 = allow, exit 2 = veto with stderr surfaced to the model. The internal seam already exists (`engine.Hooks`, `hooks.Multi`) — this adds an `ExecHook` implementation and config plumbing.
**Why:** the single biggest extensibility multiplier; unlocks lint-on-edit, policy enforcement, and CI-style automation without touching Go code. Also a natural fit for the security-audit posture (hooks + existing JSONL audit trail).
**Effort:** Medium.

### P4.5 — Headless structured output & programmatic API
**Gap #4.** `aegis chat --output-format text|json|stream-json`: `json` emits a final result object (answer, cost, usage, tool-call count, session id); `stream-json` emits one engine event per line (the `engine.Event` → JSON mapping already exists for SSE). Document the daemon HTTP API as a stable programmatic surface (Dispatch-equivalent: `POST` a task to a background session, poll `bg events`).
**Why:** CI pipelines, scripting, and eval harnesses are how agent CLIs get embedded in 2026 workflows; Aegis has all the pieces (SSE stream, bg sessions) but no machine-readable front door.
**Effort:** Small–Medium.

### P4.6 — Deferred tool loading (tool search)
**Gap #3.** Partition the registry into a core set (file ops, shell, search — always exposed) and a deferred set (LaTeX, diagram, cron, entity memory, LSP, MCP-imported tools) exposed only as names in a system-prompt one-liner. A `tool_search` meta-tool loads deferred schemas into the session registry on demand. The register/expose split in `tool.Registry` was built for exactly this.
**Why:** 39 builtins + MCP servers + plugins already produce thousands of schema tokens per turn; this compounds with P4.3 for local models with small context windows — Aegis's primary audience.
**Effort:** Medium.

### P4.7 — OS-level sandbox for the local backend
**Gap #5.** Add lightweight OS sandboxing to the `local` sandbox backend so shell isolation no longer requires a container runtime: `sandbox-exec` (seatbelt) on macOS, Landlock/`bwrap` on Linux — deny writes outside the workspace and (optionally) network egress. Wire into the existing `sandbox.Backend` interface next to `local`/`docker`; `aegis sandbox detect` reports it.
**Why:** Codex made default-on sandboxing table stakes for a security-focused harness; "install Docker first" is a real adoption barrier. Aegis markets itself on security — its own local shell should not be the least-defended path.
**Effort:** Medium–Large (platform-specific).

### P4.8 — Close the loop: push & PR creation
**Gap #6.** Extend the git tool (or add a `git_pr` tool, CapNetwork) with push + PR creation via `gh` CLI when available, API fallback otherwise. Optional per-config behavior for background sessions: on successful completion in a worktree, commit → push → open a draft PR (mirroring Claude Code's background agents).
**Why:** every top-tier harness now ends autonomous work with a reviewable PR rather than dirty local state; combines with existing worktree + bg-session support into a genuinely autonomous pipeline.
**Effort:** Small–Medium.

---

## P5 — Differentiation & Polish (medium priority)

### P5.1 — Agent teams (shared task list + peer messaging)
**Gap #7.** Extend the swarm layer: a durable shared task list (SQLite, like `bg_events`) that teammates claim/complete; extend the file mailbox to allow live teammate-to-teammate messages (not just parent-child); TUI strip showing team task state. Builds directly on `swarm.Registry`, the mailbox, and P2.9 workflow primitives.
**Effort:** Large. Do after P4.4/P4.5 so hooks and structured events can observe team activity.

### P5.2 — LSP code-intelligence tools
**Gap #8.** Add `hover`, `definition`, `document_symbols`, `workspace_symbols`, and `call_hierarchy` tools next to the existing `diagnostics`/`references` in `internal/tool/builtin/lsp.go`. The LSP client/manager already maintains server connections — this is mostly request plumbing. Register as *deferred* tools (P4.6) to avoid schema bloat.
**Effort:** Small–Medium.

### P5.3 — Pluggable web search providers
**Gap #9.** `search:` config section selecting a provider: Brave / Tavily / Kagi / SearxNG (self-hosted fits the local-first posture) with API key from env; DuckDuckGo HTML scrape remains the zero-config fallback. Same `web_search` tool contract.
**Effort:** Small.

### P5.4 — Background session notifications
**Gap #10.** When a detached session completes, errors, or hits an approval gate: fire desktop notification (`osascript` / `notify-send` / PowerShell toast) and/or a configured webhook URL with the event JSON. Pairs with P4.4 (a `notification` hook event) and P4.5 (webhook consumers).
**Effort:** Small.

### P5.5 — `@file#start-end` line-range mentions
**Gap #11, carried from v6.** Parse `#L10-40` (and `#10-40`) suffixes in @mention completion; inject only the referenced range.
**Effort:** Small.

### P5.6 — Draft stash *(was P3.8)*
**Gap #12.** Persist unsent drafts per session in `.aegis/stash.json`; restore on reattach/restart.
**Effort:** Small.

### P5.7 — Bundle install from git URL
**Gap #15.** `aegis bundle install <git-url>` — clone to a cache dir, verify manifest, install. A shared community index can wait; the URL install is the enabler.
**Effort:** Small.

### P5.8 — Semantic recall layer (optional embeddings)
**Gap #16.** Optional embedding index over `internal/knowledge` and `internal/longmem` using a local embedding model via Ollama (`/api/embed`), merged with BM25 via reciprocal-rank fusion. Strictly opt-in — keeps zero-dependency FTS5 as the default.
**Effort:** Medium.

### P5.9 — Provider failover
**Gap #17.** `provider.fallback` config listing ordered (provider, model) pairs; on repeated transport-level failure (exhausted `retry.go` budget) the factory swaps adapters mid-session and logs the switch. Guard: never silently fail over across trust boundaries (cloud → local ok; local → cloud requires explicit opt-in).
**Effort:** Medium.

---

## TQ — TUI Quality Track (2026-07-02 code review)

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience. The recurring theme: Aegis renders the conversation as **one append-only styled string** (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a **list of typed message blocks** rendered and cached individually. Most of the "less clean" feel traces back to that one structural difference; the rest is diff quality, streaming markdown, and interaction polish.

### TUI gap table

| # | Gap | Evidence in code | vs. | Severity |
|---|-----|------------------|-----|----------|
| TQ1 | Transcript is a flat string buffer, not a block/message list | `cappedBuffer` (1 MiB cap, top-trimmed); `wrapCache`/`liveWrapCache` O(n) re-wrap hacks; `/tools full` affects only *future* tool renders; resize re-wraps pre-styled ANSI | Claude Code, opencode, Crush | **High** |
| TQ2 | Diff view is not a real diff — all old lines `-` then all new lines `+` | `diffLines()` in `toolview.go`: no LCS, no context lines, no line numbers, no syntax highlight | Claude Code, opencode | **High** |
| TQ3 | Streaming text is plain-wrapped raw markdown; glamour render only at turn end causes a visible "pop"/reflow | `refresh()` wraps `liveText` raw; `flushLiveText()` restyles at `KindTurnDone` | Claude Code | **High** |
| TQ4 | Native terminal select/copy broken by alt-screen + cell-motion mouse capture; no copy affordances to compensate | `View()` sets `AltScreen` + `MouseModeCellMotion`; no copy-message / copy-code-block anywhere | Claude Code (inline mode), opencode | Medium |
| TQ5 | Always-on 22-col sidebar duplicates status-bar info (mode, model, stats, cwd) and shrinks the conversation | `sidebarInnerW=21`, auto-collapse only below 88 cols; no toggle | Claude Code (no sidebar), opencode (compact footer) | Medium |
| TQ6 | Approval prompt is a minimal 3-line y/a/n banner; "always" = coarse per-tool server cache | `renderApprovalBanner()`; `SendApproval(allowAlways)` keyed by tool only | Claude Code (option list, pattern-level rules, previews) | Medium |
| TQ7 | No live task/plan progress widget — todo tool output is invisible in the UI | no `todo` rendering anywhere in `internal/tui` | Claude Code (task list strip) | Medium |
| TQ8 | Cannot queue the next message while streaming (Enter = steer only) | `Update()`: Enter during streaming → `sendSteerCmd` | Claude Code (message queueing) | Low |
| TQ9 | Input/interaction polish: newline is `ctrl+j` (no shift+enter), image attach requires typed `@image:<path>`, ↑/↓ doubles as history vs. cursor-move in multiline input, thinking text permanently inlined (no collapse) | `keymap.go`; `extractImageRefs()`; `appendThinking()` | Claude Code, opencode | Low |
| TQ10 | Dark-only theme; glamour hardcoded to `"dark"` style | `newGlamourRenderer()`, Charmtone dark-first palette | opencode/Crush (theme system + light) | Low |

### TQ1 — Block-based transcript model
Refactor the transcript from `cappedBuffer` into `[]transcriptBlock` (user text, assistant markdown, thinking, tool call+result, system notice), each block rendering itself at the current width with a per-block cache. The viewport content becomes the join of rendered blocks.
**Why first:** it is the enabling change for almost everything below — retroactive expand/collapse of tool output and thinking blocks, `/tools full` applying to history, correct re-render on resize or theme change, copy-a-message, and removal of the three wrap-cache hacks and the 1 MiB hard truncation (blocks can page from the session store instead of being thrown away).
**Effort:** Large. Pure refactor of `internal/tui`; no daemon changes.

### TQ2 — Real unified diffs
Replace `diffLines`' delete-all/add-all output with a proper line-level LCS diff: context lines, line numbers, hunk headers, and only changed lines marked. Add intra-line change highlighting where cheap. Apply to `edit`/`multiedit`/`write` tool views *and* the approval preview so the user approves what will actually change.
**Why:** the diff is the single most-viewed artifact in a coding agent; delete-everything/re-add-everything rendering makes small edits unreadable and erodes trust in approvals. Independent of TQ1 — can ship immediately.
**Effort:** Small–Medium (a vendored LCS or `diff` package plus renderer rework).

### TQ3 — Streaming markdown rendering
Render the live tail through glamour incrementally instead of raw-wrapping until turn end. Practical approach (used by Crush): re-render the live block through glamour on each refresh tick, but only from the last safe markdown boundary (the `findSafeMarkdownBoundary` helper already exists); with TQ1, the live block is just a block whose source keeps growing.
**Why:** the end-of-turn restyle "pop" — raw `**bold**` and backticks suddenly becoming formatted — is the most visible streaming-quality difference vs. Claude Code.
**Effort:** Medium. Sequence after TQ1 to avoid doing the work twice.

### TQ4 — Copy & selection ergonomics
Three parts, cheapest first: (a) `/copy` command + keybinding that copies the last assistant message to the clipboard (OSC 52 so it works over SSH); (b) a code-block picker — enumerate fenced blocks in the last message, copy by number; (c) evaluate scoping mouse capture (enable cell-motion only while a picker/terminal pane is open) so terminal-native drag-select works in the common case.
**Why:** "I can't copy the answer" is the sharpest day-one friction of alt-screen TUIs; Claude Code sidesteps it by running inline, opencode by providing copy affordances.
**Effort:** Small (a, b); Medium (c).

### TQ5 — Declutter: toggleable sidebar, single status line
Make the sidebar toggleable (`ctrl+b` / `/sidebar`), default *off*, promoting the conversation to full width. Fold its unique, glanceable data (context-bar fill %, cost, running-agent count) into the existing bottom status line; keep the rich breakdown behind `/status` (already exists) and in the sidebar for those who want it on. Remove duplication (mode/model/cwd currently appear in both).
**Why:** both Claude Code and opencode give the conversation the full terminal width and keep chrome to one line; the always-on panel is the largest visual divergence.
**Effort:** Small.

### TQ6 — Richer approval flow
Upgrade the y/a/n banner to an option-list dialog: arrow-key selection with `Allow once / Allow always for this pattern / Deny / Deny with feedback`. "Always" should write a text permission rule (e.g. `allow bash(npm test*)` — the `permission.Rules` syntax already exists) scoped to the *command pattern or path*, not the whole tool, and persist it to project config so it survives restarts. Show the TQ2 diff for pending edits.
**Why:** approval is where trust is built; per-tool "always" is too coarse for a security-branded harness (approving one `shell` call approves every future shell command).
**Effort:** Medium.

### TQ7 — Live task progress strip
When the todo/plan tool updates, render a compact progress strip (`▣▣▢▢ 2/4 refactor session store`) above the input area, expandable via `/tasks`. Data already flows through tool events; this is pure presentation.
**Effort:** Small.

### TQ8 — Message queueing while streaming
Let Enter during streaming offer *queue* as well as *steer*: keep the current steer semantics on plain Enter (it is genuinely good), and add `alt+enter` (or a prefix) to queue the text as the next user turn, shown as a dimmed pending block that auto-sends at `KindDone`.
**Effort:** Small (after TQ1 makes pending blocks trivial to render).

### TQ9 — Input polish bundle
- `shift+enter` for newline via the Kitty keyboard protocol (bubbletea v2 supports it; keep `ctrl+j` fallback)
- Paste-detection for image paths: a pasted path ending in `.png/.jpg/...` becomes an attachment chip instead of requiring the typed `@image:` incantation
- ↑/↓ moves the cursor within a multiline draft; history navigation only when the cursor is already at the first/last line (the standard Claude Code/opencode behavior)
- Collapse thinking blocks to a one-line `✻ thought for 12s` header, expandable (needs TQ1)
**Effort:** Small each; bundle into one pass.

### TQ10 — Theme system & light mode
Move the hardcoded Charmtone palette behind a small theme interface with dark + light built-ins and a `tui.theme` config key; pass the matching glamour style through. Terminal-background auto-detection is a stretch goal.
**Effort:** Medium. Lowest priority in this track — cosmetic, and the dark default is fine for the core audience.

### Recommended TQ sequence

```
Quick wins first:   TQ2 real diffs → TQ5 declutter → TQ4a/b copy affordances → TQ7 task strip
Foundation:         TQ1 block transcript
On the foundation:  TQ3 streaming markdown → TQ9 input polish → TQ8 queueing → TQ6 approvals
Cosmetic tail:      TQ10 themes, TQ4c scoped mouse capture
```

TQ2 + TQ5 + TQ4a deliver the most "feels like Claude Code" per line of code changed and need no refactor. TQ1 is the investment that makes the rest cheap and removes the wrap-cache/truncation debt permanently.

---

## P6 — Long-Horizon / Exploratory

### P6.1 — Mid-turn state persistence *(was P4.1)*
Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.

### P6.2 — A2A protocol integration *(was P4.2)*
Agent-to-Agent HTTP+SSE protocol (ADK Go 2.0, GA June 2026): `a2a_agent` client tool for calling remote agents + expose the daemon as an A2A server (`.well-known/agent.json` discovery). No SDK dependency — it's a protocol. Depends on P5.1 being stable.

### P6.3 — MCP server mode
Expose Aegis itself as an MCP server (`aegis mcp-serve`): sessions and selected tools become MCP tools callable from other harnesses (Claude Code, Codex, editors). Complements A2A; the daemon API maps cleanly. Codex already does this and it materially expands where the harness can be embedded.

### P6.4 — Context editing / tool-result pruning
Before invoking LLM summarization (compaction), deterministically drop or truncate *stale tool results* (old file reads superseded by later reads, large search dumps already acted upon). Cheaper than a model call, preserves conversational text verbatim, and mirrors Anthropic's context-editing direction. Fits in `internal/compaction` as a pre-pass.

### P6.5 — Desktop / IDE surface beyond ACP
ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Only worth it if user demand materializes — the TUI is the product.

---

## Recommended Sequence

```
Wave 1 (context & control):   P4.3 skills disclosure → P4.6 deferred tools → P4.4 hooks
Wave 2 (automation loop):     P4.5 headless JSON → P4.8 push/PR → P5.4 notifications
Wave 3 (safety & tools):      P4.7 OS sandbox → P5.2 LSP tools → P5.3 search providers
Wave 4 (scale-out):           P5.1 agent teams → P5.9 failover → P5.8 semantic recall
TUI track (parallel):         TQ2 diffs → TQ5 declutter → TQ4a/b copy → TQ1 block model → TQ3/TQ6–TQ9
Quick wins (any time):        P5.5 line ranges, P5.6 draft stash, P5.7 bundle git install, TQ7 task strip
```

Waves 1–2 deliver the largest share of "top-tier feel": lower token burn per turn, extensibility without recompiling, and autonomous work that ends in a reviewable PR. Wave 3 hardens the security story that differentiates Aegis. Wave 4 is scale-out once the fundamentals are competitive. The TUI track runs in parallel — it touches only `internal/tui`, so it never conflicts with daemon-side waves.

---

## Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
