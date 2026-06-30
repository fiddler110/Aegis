# Aegis Capability Roadmap
**Date:** 2026-06-29
**Updated:** 2026-06-30 (v2 — engine + TUI gaps from Crush / OpenCode competitive review)

> **Previously completed items:**
>
> | Item | Status |
> |---|---|
> | P1.1 sub-agent | ✅ done (`internal/tool/builtin/agent.go`) |
> | P1.2 observability | ✅ done (`internal/trace`, `aegis sessions trace`) |
> | P1.3 sandbox | ✅ done (`internal/sandbox`, `aegis sandbox`) |
> | P1.4 `--resume` | ✅ done (`--resume`, TUI session picker) |
> | P2.1 model-router | ✅ done — `openai` provider + `base_url` covers OpenAI-compat endpoints |
> | P2.2 permission rules | ✅ done — `permission.rules` in config, `internal/permission/rules.go` |
> | P2.3 repo-map | ✅ done — `internal/repomap`, `aegis index` |
> | P2.4 output validation | ✅ done — `internal/guard`, engine `OutputGuard` seam, `/guard` toggle |
> | P2.5 web search | ✅ done — `web_search` tool (DuckDuckGo, no API key) |
> | P3.3 persona templates | ✅ done — `.aegis/personas/*.md`, per-persona model/mode/rules/guard |

---

## Gap Summary

| Category | Gap | Missing vs. |
|---|---|---|
| Permission | Interactive approval gate — session-persistent approve/deny that suspends execution | Crush, OpenCode |
| TUI | Tool output collapsing — inline results scroll unbounded with no truncate/expand | Crush, OpenCode |
| TUI | Inline diff view — file edits not shown as diffs in the transcript | Crush, OpenCode |
| TUI | Rich permission approval — Y/N banner has no content preview (command/diff/path) | OpenCode |
| File discovery | No ripgrep integration; no `ls` directory-tree tool for model navigation | Crush, OpenCode |
| TUI | No `!` bang shell mode for raw command execution without the LLM | Crush, OpenCode |
| TUI | Alphabetical @mention with no frecency/fuzzy file ranking | OpenCode |
| TUI | No file-change tracking in sidebar (+/- counts for modified files) | Crush |
| TUI | No `@file#start-end` line-range syntax in @mentions | OpenCode |
| TUI | Subagent status only via `/teammates`; no always-visible footer strip | OpenCode |
| TUI | No conversation timeline/scrubber for navigating long sessions | OpenCode |
| Engine | Max-step hard-abort with no graceful summary turn | OpenCode |
| Engine | Context compaction is periodic (every 5 iters) not proactive (pre-turn headroom check) | OpenCode |
| Memory | No tiered long-term / entity memory | CrewAI, Devin |
| Async | No background / detached task execution | Cursor, Devin |
| Context | No DeepWiki-style project knowledge base | Devin |
| Safety | No automatic rollback on tool failure | Windsurf |

---

## P1 — Critical

High user-impact gaps present in both Crush and OpenCode. These directly affect daily usability.

### P1.1 — Interactive permission gate with session persistence

**Gap:** `Gate.Check()` is synchronous and stateless — it approves or denies immediately with no way to suspend execution and ask the user interactively, and no mechanism to save the user's answer for the rest of the session. Both competitors can pause a tool call, present an approval prompt, and remember the user's answer for subsequent calls.

**Present in:** Crush (`internal/permission/` — four-path resolution: hook pre-approve → allowlist → `AutoApproveSession` → interactive channel wait; `GrantPersistent` stores by `PermissionKey(sessionID, toolName, action, path)`). OpenCode (`permission.ts` — three-state `evaluate()`: `allow`/`deny`/`ask`; `ask` suspends via Effect deferred, publishes `Event.Asked`; `reply("always")` retroactively approves all matching pending requests for the session).

**Recommended approach:** Add a third gate state `pending` alongside `allowed`/`denied`. When the gate returns `pending`, the engine suspends dispatch of that tool call and emits a `KindApprovalRequest` event (already exists and the TUI already renders the Y/N banner). The engine blocks on a response channel until the TUI posts the decision. Add a `SessionPermissionCache` struct keyed by `(toolName, inputPattern)` that the gate checks first — if a prior "allow always" answer matches, the call proceeds without prompting.

The TUI already has `approvalState`, `renderApprovalBanner`, and `sendApprovalCmd` — the missing piece is that the engine gate fires synchronously rather than waiting. Threading a response channel through `Gate.Check` and wiring "allow always" to the cache closes the loop.

**Acceptance criteria:**
- `Gate.Check()` supports a `pending` state that suspends tool dispatch
- Engine blocks on an approval channel and resumes when TUI posts the decision
- "Allow once" (y) runs the tool this time; "Allow always" (a) saves and auto-approves future matching calls
- "Deny" (n) injects an error result and the run continues
- Existing plan/build/auto modes remain as the coarse filter; interactive approval handles per-call grey areas

---

### P1.2 — Collapsible tool output in the transcript

**Gap:** Tool results render inline at full length. Long results (grep output, full file reads, shell stdout) fill the viewport with content the user may not need to read. Both competitors truncate to ~10 lines and offer an expand toggle.

**Present in:** Crush (`internal/ui/chat/tools.go` — 5 status states: awaiting-permission, running/spinner, success, error, canceled; compact mode = single-line header with status icon; expanded = full output, syntax-highlighted, truncated to `responseContextHeight` = 10 lines with "N lines remaining" affordance; `ToggleExpanded()` on keypress). OpenCode (inline compact vs. block rendering; global "Show tool details" toggle hides all completed tool output post-run).

**Recommended approach:** Add an `expanded bool` field to the `toolEntry` struct in the TUI model. On `KindToolResult`, count the lines in the result; if `> 10`, store the full result but render only the first 10 lines followed by a dim `▶ N more lines` footer. A key binding (Tab when cursor is in the transcript, or clicking the footer line) toggles `expanded`. The sidebar tool-status icons (already present as ●/✓/×) remain visible in compact mode so status is never hidden. Add `/tools compact` and `/tools full` slash commands to set the session default.

**Acceptance criteria:**
- Tool results with more than 10 lines are truncated; a `▶ N more lines` affordance is shown
- Tab (or a dedicated key) on the tool line expands/collapses
- Compact header always shows tool name + status icon
- `/tools compact` and `/tools full` control the session default
- Existing transcript rendering for short results is unchanged

---

### P1.3 — Inline diff display for file edits

**Gap:** When `edit_file` or `write_file` runs, the transcript shows only a one-line confirmation ("edited foo.go (1 replacement)"). There is no visual diff. Both competitors render diffs inline in the transcript.

**Present in:** Crush (`unified_diff.go` — split-view on wide terminals, unified on narrow; rendered per-edit tool result). OpenCode (diff viewer in permission approval prompt + revert visualization with redo shortcut after apply).

**Recommended approach:** The checkpoint system already captures pre-edit content (`checkpoint.SnapshotterFrom(ctx).Capture(abs)` in `builtin/file.go`). After a successful `edit_file` or `write_file`, compute a unified diff between the captured before-bytes and the written after-bytes in the server's `runs.go` tool-dispatch path. Return the diff as an additional section in the tool result string. In the TUI, detect tool results that contain a `@@` unified-diff header and render added lines in green, removed lines in red, context lines dim. Diff display obeys the collapsing rule from P1.2 (collapsed to 10 lines by default).

For `write_file` on a new file, show `+N lines (new file)` instead.

**Acceptance criteria:**
- `edit_file` results include a unified diff in the transcript (colored green/red)
- `write_file` on a new file shows `+N lines (new file)` 
- `write_file` overwriting an existing file shows the diff between old and new content
- Diff collapses beyond 10 lines with the P1.2 expand affordance
- Diff rendering does not break for binary files (detect and skip)

---

### P1.4 — Rich permission approval UX

**Gap:** The approval banner shows tool name + reason and asks Y/N. It does not show *what the tool will do* — the command that will run, the file path being written, or the content that will change. OpenCode's approval prompt is significantly more information-dense.

**Present in:** OpenCode (`session/permission.tsx` — `△ Permission required` + specific details: file path / command / URL + diff viewer for edits + bash command preview; three options: Allow once / Allow always / Reject; secondary feedback textarea on reject).

**Recommended approach:** Extend the `KindApprovalRequest` event (already in the API) to carry a structured `preview` string field. Populate it per tool type: bash tools → the full command string; file write/edit → the first 20 lines of content or the diff (once P1.3 is done); network tools → the URL. The TUI `renderApprovalBanner` renders this preview between the header and the Y/N line, scrollable if long. Add 'a' as a third key for "allow always" (see P1.1).

**Acceptance criteria:**
- Approval banner shows a content preview for every tool type (command, file + first N lines, or URL)
- "Allow once" (y), "Allow always" (a), "Deny" (n) as three distinct options
- Preview truncated at 10 lines with "... N more" indicator
- Reject optionally accepts feedback text (type after pressing n)

---

## P2 — Meaningful

Present in one or both competitors; meaningful UX and capability improvements.

### P2.1 — Ripgrep integration + `ls` directory tree tool

**Gap:** `glob` and `grep` tools run `filepath.WalkDir` on every call with a hardcoded skip list. No `.gitignore` awareness, and speed degrades on large repos. There is also no `ls` tree tool — the model cannot browse directory structure without reading files, which wastes tokens.

**Present in:** Crush (`glob.go` — `rg --files` with literal-prefix optimization, fallback to `fsext.GlobGitignoreAwareCtx` + `.crushignore`). OpenCode (`ripgrep.ts` — `rg --files` stdout streaming, `rg --json` for grep; both support `.gitignore`). Both ship a named `ls` tool returning an indented directory tree.

**Recommended approach:**
1. In `builtin/search.go`, detect `rg` at startup (`exec.LookPath("rg")`). If present, implement glob as `rg --files | filter by pattern` and grep as `rg --json`. Fall back to the current WalkDir implementation when `rg` is absent — no regression for environments without ripgrep.
2. Add an `ls` built-in tool (`builtin/ls.go`, `CapRead`) that accepts `path` and `depth` (default 2), returns an indented tree (directories first, entries capped at 200), skipping `.git`/`node_modules`/`vendor`.

**Acceptance criteria:**
- `glob` and `grep` use `rg` when available; `.gitignore` patterns respected automatically
- `ls` tool available to all personas, returns an indented tree up to specified depth
- Fallback to current WalkDir when `rg` is not installed; existing tests still pass
- `ls` validates path stays within workspace root

---

### P2.2 — Bang `!` shell mode in the input

**Gap:** Running a quick shell command requires the LLM's involvement or switching to the Ctrl+X terminal pane. Both Crush and OpenCode support a `!` prefix that executes the line as a raw shell command and streams output directly into the transcript without any LLM round-trip.

**Present in:** Crush (input prefix `!` enters bang mode; Escape or backspace-at-start exits). OpenCode (same `!` prefix pattern).

**Recommended approach:** In `tui.go`'s `Update` function, when Enter is pressed and the input value starts with `!`, strip the prefix and execute the remainder as a shell command in the workspace directory. Stream stdout/stderr into the transcript as a tool-result-style block (same styling as a `bash` tool result but labeled `shell`). No LLM call, no token consumption. The command runs via `os/exec` with the same CWD as the session. Add to input history the same as normal messages.

**Acceptance criteria:**
- `! command args` executes and shows output inline in the transcript
- Exit code and stderr shown on non-zero exit
- No LLM call; no tokens consumed
- Command goes into input history; Up/Down navigates back to it
- No interaction with the approval gate (user typed it explicitly)

---

### P2.3 — Frecency-ranked @mention file autocomplete

**Gap:** Aegis builds a file index on first `@` keypress via a full directory walk and presents results alphabetically. There is no ranking by recency, frequency, or fuzzy path matching.

**Present in:** OpenCode (`frecency.tsx` — frecency score = frequency × decay(recency); `autocomplete.tsx` — fuzzy match across four categories: files (frecency-ranked), agents, MCP resources, slash commands). Crush (four-tier prioritization: exact name match → basename prefix → path-segment → alphabetical fallback).

**Recommended approach:** Add frecency tracking in memory for the current session: each time a file is read or written by a tool call, increment its score and record the timestamp. On `@` keypress, sort completion candidates by `score × recency_decay` (simple exponential decay). Add a `@file#start-end` line-range extension: after selecting a file, if `#` is typed, offer line-number completions (e.g., `#1-50`). The `read_file` tool already supports `offset` and `limit` — the `#start-end` syntax is client-side sugar that translates to those parameters.

**Acceptance criteria:**
- Files read or written by the agent in the current session rank above unaccessed files in @mentions
- Prefix-match and path-segment scoring applied before alphabetical fallback
- `@file#15-20` syntax reads lines 15–20 of the file (translated to `offset=15, limit=5`)
- Frecency state is in-memory per session; no persistent file required

---

### P2.4 — File-change tracking in sidebar

**Gap:** The sidebar shows session metadata, mode, model, tools, context, and cost — but not which files have been modified in the current session. Users need `git diff --stat` or `/checkpoint` to see what changed.

**Present in:** Crush — sidebar section shows modified files with `+N` added / `-M` deleted line counts, derived from the file tracker, updated live.

**Recommended approach:** Aegis already has `internal/filetracker` recording reads and writes per absolute path. Extend `Tracker.RecordWrite` to capture before/after line counts (already available in the `write_file` / `edit_file` tool path since checkpoint captures the before-bytes). Expose a `ChangedFiles() []FileChange` method on the tracker. The daemon API exposes this via a new field on the session response. The TUI renders a "FILES" sidebar section listing modified paths with `+N/-M` next to each, sorted by modification recency. Section is hidden when no files have been modified.

**Acceptance criteria:**
- Sidebar "FILES" section appears when any file is written in the current session
- Each entry shows relative path + `+N` added / `-M` removed lines where available
- Section refreshes after each `KindToolResult` for a write-capability tool
- Hidden when no writes have occurred

---

### P2.5 — Subagent footer strip

**Gap:** Sub-agent status is only visible via the `/teammates` command, which prints a one-time snapshot into the transcript. There is no persistent, always-visible indicator of running or queued sub-agents.

**Present in:** OpenCode (`subagent-footer.tsx` + `dialog-subagent.tsx` — running sub-agents appear in a persistent footer strip above the input area; keybind to relegate to background; parent/child session hierarchy navigation).

**Recommended approach:** Add a slim footer strip (1–2 lines) rendered between the transcript viewport and the input area when any sub-agents are active. Each entry: short agent ID + persona name + status (running/done/failed) + elapsed seconds. Strip hides automatically when all agents complete. The `/teammates` slash command remains for full detail. The daemon already exposes a `Teammates` API call that the TUI uses for the existing `/teammates` render — poll it periodically while any agent is running, or wire a push event for agent state changes.

**Acceptance criteria:**
- Footer strip appears while any sub-agent run is in progress
- Each entry shows: short ID + persona + status + elapsed time
- Strip auto-hides when all agents complete or fail
- `/teammates` command continues to work as before for full detail

---

### P2.6 — Max-step graceful degradation

**Gap:** When `MaxIterations` is reached, the engine emits `KindError` and aborts. The user sees an error but no summary of what was accomplished or what remains. Only OpenCode handles this gracefully.

**Present in:** OpenCode — on the last allowed step, tool definitions are withheld from the request and a prompt is injected instructing the model to summarize progress, constraints met, and remaining work. The model produces a useful partial-progress reply; the run ends with `KindDone` rather than `KindError`.

**Recommended approach:** In `engine.Run`, on `iter == e.maxIterations - 1` (the final iteration), instead of allowing a normal tool-calling turn, set a flag that: (a) passes `nil` for `req.Tools` (no tools offered), and (b) prepends a system injection to the user message: `"[Step limit reached. Summarize what you have accomplished, what constraints were met, and what work remains. Do not call any tools.]"`. The model produces a text-only final turn, emitted as `KindDone`. Reserve `KindError` for genuine failures (budget exceeded, loop detected, context overflow, provider error).

**Acceptance criteria:**
- On the final allowed iteration, tools are withheld and a summary instruction is injected
- The model's summary text is emitted as a normal `KindDone` (not `KindError`)
- `KindError` still fires for: budget exceeded, loop detection, context overflow, provider errors
- `MaxIterations` semantics unchanged from the caller's perspective

---

### P2.7 — Proactive context compaction

**Gap:** Compaction is triggered at session start and every 5 tool-call iterations mid-run. Between checks a turn can hit the provider's context limit, causing an error requiring recovery. OpenCode checks token headroom before every turn.

**Present in:** OpenCode — `estimate({system, messages, tools})` called before each model turn; if `estimate > contextWindow - max(outputTokens, buffer)`, compaction runs proactively before the turn.

**Recommended approach:** Before each call to `e.turn()` in `engine.Run`, compute an estimated token count using the existing `estimateTokens()` helper. If the estimate exceeds a threshold fraction of the model's context window (default 85%), trigger the `Compactor` before the turn rather than waiting for a provider error. Add a `ContextWindowTokens int` field to `Options` (with per-model defaults keyed on model-name substrings, reusing the logic in `contextWindowFor` in the TUI). The existing every-5-iterations periodic check can be removed once this is in place.

**Acceptance criteria:**
- Token headroom estimated before each turn using `estimateTokens()`
- Compaction triggered when estimate exceeds 85% of `ContextWindowTokens`
- `ContextWindowTokens` in `Options` with per-model defaults
- The every-5-iterations mid-run check removed (replaced by per-turn check)
- Existing `Compactor` interface unchanged

---

### P2.8 — Conversation timeline dialog

**Gap:** Long sessions have no navigation mechanism beyond scrolling. Users cannot jump to an earlier user turn without reading through the entire transcript.

**Present in:** OpenCode (`dialog-timeline.tsx` — modal listing all user messages with timestamps; click any message to jump to that position in the conversation).

**Recommended approach:** Add a `/timeline` slash command that opens a full-screen overlay listing all user turns in the session with timestamps and a first-line preview (60 chars). Selecting an entry scrolls the transcript viewport to that turn's position. The data is available from `m.history` (already populated on session load and appended on each user send). The overlay uses the same pattern as the existing command palette and persona/session pickers.

**Acceptance criteria:**
- `/timeline` opens an overlay listing all user turns with timestamps and first-line previews
- Arrow keys + Enter or mouse click navigates to the selected turn
- Transcript viewport scrolls to the selected message
- Esc dismisses the overlay and returns focus to the input

---

## P3 — Exploratory

Long-horizon or niche capabilities worth tracking; no immediate implementation commitment.

### P3.1 — Tiered long-term memory

**Gap:** Aegis memory is session-scoped (SQLite) and project-scoped (CLAUDE.md). No persistent entity memory or cross-session factual store.

**Present in:** CrewAI (short-term / long-term / entity / contextual tiers), Devin (persistent task memory).

**Notes:** Entity memory (extracted facts about target systems, codebases, and decisions) is highest-value for security personas that accumulate knowledge about a target across sessions. A SQLite-backed entity store keyed by `(project, entity_type, entity_name)` would be a natural extension of the existing session store.

---

### P3.2 — Async / background task execution

**Gap:** All sessions are synchronous. Long-running tasks (full-repo audit, multi-file refactor) block the TUI.

**Present in:** Cursor (Background Agent in cloud VM), Devin (async SaaS tasks).

**Notes:** The daemon architecture already separates client from server. Detached sessions (start, detach, reattach later) are architecturally straightforward but require TUI push notifications for async completions and state management for re-attach. Ties to the existing swarm/subprocess infrastructure.

---

### P3.3 — DeepWiki-style project knowledge base

**Gap:** No queryable knowledge base auto-generated from the repo. Re-reading files on every session discards accumulated structural knowledge.

**Present in:** Devin (DeepWiki).

**Notes:** A project-level SQLite FTS index populated from code comments, README files, commit messages, and documentation, queried via a `project_knowledge` tool. The repo-map (already built) provides the structural skeleton; this adds semantic content depth.

---

### P3.4 — Automatic rollback on tool failure

**Gap:** Partial failures leave the workspace in an inconsistent state. No rollback mechanism exists.

**Present in:** Windsurf (Cascade with automatic rollback).

**Notes:** Git-native rollback is the simplest approach: checkpoint the workspace with a commit before a multi-step task begins; on unrecoverable failure, offer `git reset --hard` to the pre-task commit. Requires explicit task boundaries — ties to the sub-agent primitive (already implemented as `run_agent`).

---

### P3.5 — Mid-turn state persistence

**Gap:** If the process crashes mid-turn, the partial turn (accumulated assistant text, tool calls received) is lost. Crush and OpenCode commit incremental state to SQLite during streaming.

**Notes:** The session layer persists completed turns to SQLite. Extending this to write partial state per streaming callback (assistant text accumulated so far, tool calls received but not yet dispatched) would require threading the session store into the engine or reusing the checkpoint infrastructure. High implementation complexity for a relatively low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.

---

### P3.6 — Typed tool output schemas

**Gap:** Aegis tools return raw strings. OpenCode tools declare Effect Schema for both input and output; the harness validates and serializes cleanly.

**Notes:** Adding a typed output schema to the `Tool` interface would enable structured output parsing, tool result validation, and richer client rendering without string parsing. Low immediate user impact; high architectural cleanliness. Would require a new `OutputSchema() json.RawMessage` method on the interface and corresponding changes to all built-in tools.

---

### P3.7 — Animation pause off-screen

**Gap:** The shimmer animation and spinner tick unconditionally, triggering redraws even when the animated content is scrolled off-screen.

**Present in:** Crush (`pausedAnimations` map — animation ticks disabled for items not visible in the viewport, resume on scroll-back).

**Notes:** Minor performance improvement; most relevant for slow terminals or very long sessions. Track whether the "● thinking…" line is within the visible viewport range; suppress `sp.Tick` commands when it is not. Low priority but clean.

---

### P3.8 — Draft stash

**Gap:** Aegis has in-memory input history but no persistent draft save across sessions. Typed but unsent long-form prompts are lost on restart.

**Present in:** OpenCode (`stash.tsx` — saves and restores draft messages across sessions).

**Notes:** A simple `.aegis/stash.json` file storing the last N unsent draft messages per session (keyed by session ID) would suffice. Low priority but useful for long-form prompts interrupted by a restart or daemon cycle.
