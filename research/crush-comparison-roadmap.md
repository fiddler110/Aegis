# Aegis vs Crush — Comparative Roadmap

**Date:** 2026-06-27  
**Scope:** Chat interface, agent harness, and local model handling  
**Reference:** `D:\Development\tools\Crush` (commit-era: June 2026)

---

## Executive Summary

Crush is a mature, production-quality agent harness with a fully decomposed UI layer, a
robust multi-layer agent architecture, and deep per-provider local-model support. Aegis
has a solid provider adapter, working TUI, and compaction — but renders tools generically,
runs a single-layer engine, and has no model capability database or auto-discovery.

The gaps below are ordered by impact on the local-model use case.

---

## 1. Chat Interface

### 1.1 Streaming Markdown Stable-Prefix Cache

**Crush:** `internal/ui/chat/streaming_markdown.go`  
Every glamour render of the assistant text during streaming finds the last "safe" markdown
boundary (blank line where no fenced block, list, table, or setext header is open), caches
the rendered prefix, and only re-renders the trailing chunk. This reduces streaming render
cost from O(n²) to O(tail) per token.

**Aegis:** Full glamour re-render + wrap on every `EventTextDelta`, capped only by
`wrapCache` which still re-wraps everything when content length changes.

**Objective:** Port `streamingMarkdown` + `findSafeMarkdownBoundary` to Aegis's TUI so
long responses don't stall on re-render. The boundary check (~200 lines) is self-contained
and has no external dependencies beyond `strings`.

---

### 1.2 Per-Tool Rich Rendering

**Crush:** `internal/ui/chat/` has 20+ tool-specific renderers, each implementing
`ToolRenderer.RenderTool(sty, width, opts)`:
- **bash** — command header + ANSI-remapped output block with line count
- **edit / multiedit** — unified diff (side-by-side on wide terminals) with +/- counts
- **view** — syntax-highlighted code with line numbers and file path
- **write** — diff from empty to new content
- **grep / glob / ls** — formatted match lists
- **fetch / web_fetch / web_search** — URL + markdown result
- **agent** — nested task with its own spinner
- **MCP** — generic fallback with MCP server name prefix
- All tools: expandable (truncated by default, press `e` to expand), copy-to-clipboard
  (`y`/`c`), animated gradient spinner while running

**Aegis:** `toolview.go` has `renderToolCall` and `renderToolResult` which dispatch on
tool name for edit/write/shell, with a generic gutter-block fallback for everything else.
No per-tool spinners, no expand/collapse, no clipboard copy.

**Objective:** Migrate to a `ToolRenderer` interface per tool name. Priority order:
1. `edit_file` / `multi_edit` — unified diff (already have `diffLines` logic, needs diff library)
2. `shell` — ANSI-remapped output with scrollable block
3. `read_file` / `view` — syntax-highlighted code + line numbers
4. `web_search` — formatted search results
5. Generic MCP tool fallback

Each renderer should support compact (single-line) and expanded (full output) modes.

---

### 1.3 Versioned Virtual List

**Crush:** `internal/ui/list/` implements a virtual list where each item is a
`list.Versioned`. Items expose a version counter that bumps when their internal state
changes (tool call arrives, result arrives, spinner ticks). The list only re-renders items
whose version is newer than the last drawn version. A frozen item (`Finished() == true`)
never re-renders again.

**Aegis:** Single `viewport.Model` displaying a `cappedBuffer` string — any change
causes the entire transcript to be passed through `wrapCache` and set on the viewport.

**Objective:** Introduce a message list model where each turn (user + assistant + tools)
is an item. Items re-render only when their state changes. This is the most impactful
structural change but can be done incrementally — start with assistant messages (which
change on every streaming token) and tool items (which change twice: on call and result).

---

### 1.4 Tool Status State Machine

**Crush:** Tools move through: `AwaitingPermission → Running → Success / Error / Canceled`.
Each state has a distinct icon (●, ✓, ×, ⊘) and style. The animated gradient spinner runs
only while `Running`. A `SpinningFunc` override lets specific tools customize when they animate.

**Aegis:** Status is represented by the `toolEntry.status` string field (`"pending"` / `"ok"` /
`"err"`), rendered in the sidebar activity panel — not inline in the transcript.

**Objective:** Move tool status inline with the tool's transcript entry. Add a per-tool
animation (`spinner.New()` or equivalent) keyed by tool ID. Drive the spinner from a
`tea.Tick` scoped to each active tool rather than a global spinner.

---

## 2. Agent Harness Architecture

### 2.1 Large/Small Model Pair

**Crush:** `coordinator.buildAgentModels()` instantiates two distinct models:
- **Large** — main agent loop, summarization
- **Small** — title generation only (falls back to large if small fails)

Title generation uses a separate `fantasy.Agent` with a `maxOutputTokens` of 40 and
`/no_think` appended to the system prompt to prevent reasoning preamble.

**Aegis:** Single model for everything. Title is never generated.

**Objective:** Add optional `SmallModel` field to engine config. When set, use it for:
- Session title generation (async, after first user turn)
- Compaction summary requests (uses fewer tokens than the primary model is worth burning)

For Ollama, the small model can be a fast local model like `qwen2.5:3b` while the large
model is `qwen3.5:latest`.

---

### 2.2 Context-Window-Aware Auto-Compaction

**Crush:** `internal/agent/agent.go` — `StopWhen` condition:
```go
const (
    largeContextWindowThreshold = 200_000
    largeContextWindowBuffer    = 20_000
    smallContextWindowRatio     = 0.2
)
// If window > 200k: trigger when remaining < 20k tokens
// If window < 200k: trigger when remaining < 20% of window
// If window == 0 (unknown / local model): skip auto-summarize entirely
```

**Aegis:** `compaction/compaction.go` — fixed `MaxBudget` (default 120k tokens), fires
regardless of actual model context window.

**Objective:** 
1. Add `ContextWindow int` to the model config path.
2. Pass it to the engine as a stop condition rather than a fixed budget.
3. When `ContextWindow == 0` (Ollama models without reported context window), skip
   auto-compaction unless the user explicitly configures a budget — avoids immediately
   truncating cheap local sessions.

---

### 2.3 Queued Message Dispatch

**Crush:** `sessionAgent.Run()` — if the session is busy when a new message arrives, it
is enqueued (`messageQueue`). After each streaming step, `drainQueueForStep()` folds
queued messages into the current turn (messages without RunID) or leaves them for their
own turn (messages with RunID). A cancel mark mechanism ensures messages queued before a
cancel are dropped.

**Aegis:** No queue — if a session is busy, messages are silently dropped or the TUI
disables input.

**Objective:** Add a per-session message queue to the engine. At minimum: accept the next
message while a turn is in progress, and process it immediately when the turn finishes.
The fold-into-current-step optimization (Crush's follow-up collapsing) can come later.

---

### 2.4 PrepareStep Hook + Dynamic Tool Refresh

**Crush:** `fantasy.AgentStreamCall.PrepareStep` fires before each streaming step and
can:
- Swap the tool list (picks up newly connected MCP servers)
- Fold queued follow-up messages
- Apply per-step cache-control headers

**Aegis:** Tools are fixed at agent construction time. MCP tool changes require a restart.

**Objective:** Add a `PrepareStep func(messages) messages` callback to `engine.Options`.
Use it to refresh MCP tools on each turn without restarting the engine. This is the entry
point for the MCP hot-reload improvement.

---

## 3. Local Model Support

### 3.1 Auto-Discover Models from Local Endpoints

**Crush:** `internal/discover/` — polls `GET /v1/models` (LM Studio, generic) or
`GET /api/tags` (Ollama) at startup; merges discovered models into the provider's model
list, skipping any already present in config. Uses a shared `http.Client{Timeout: 10s}`.

**Aegis:** No discovery — all models must be named explicitly in config or env vars.

**Objective:** On startup, if `baseURL` matches a known local provider pattern
(`localhost`, `127.0.0.1`, `ollama`), poll the models endpoint and present discovered
models in the TUI model picker. Cache the list for the session; re-poll on demand.

---

### 3.2 Model Capability Database

**Crush:** Uses the `catwalk` library (`charm.land/catwalk`). Each model entry carries:
- `ContextWindow int`
- `DefaultMaxTokens int64`
- `CanReason bool`
- `ReasoningLevels []string`
- `SupportsImages bool`
- `CostPer1MIn / CostPer1MOut / CostPer1MInCached / CostPer1MOutCached float64`
- `Options` (temperature, top_p, etc. defaults)

This drives: auto-summarize thresholds, image filtering, title generation token limits,
reasoning effort injection, cost calculation.

**Aegis:** No capability database. `maxTokens` is a flat config field. Context window is
not tracked.

**Objective:** Introduce a `ModelCapabilities` struct. For well-known Ollama models
(qwen3.5, qwen2.5, deepseek-r1, llama3, etc.), ship a small built-in map. For unknown
models, populate from the Ollama `/api/show` response (`context_length`,
`parameter_size`, `capabilities`). Use capabilities to:
- Skip auto-compaction when context window is unknown
- Filter image attachments when model doesn't support vision
- Choose `DefaultMaxTokens` for the `max_tokens` request field

---

### 3.3 Per-Provider Reasoning Control

**Crush:** `coordinator.getProviderOptions()` has explicit branches for every provider's
reasoning API:
- Anthropic/Bedrock: `thinking.budget_tokens` or `effort`
- OpenAI/Azure: `reasoning_effort`
- Google: `thinking_config.thinking_budget`
- OpenRouter: `reasoning.enabled + effort`
- Ollama/openaicompat: `extra_body.thinking` (enabled/disabled) or `reasoning_effort`
- DeepSeek/ZAI: `thinking.type = "enabled" | "disabled"`
- Fireworks: `thinking.type` (mutually exclusive with `reasoning_effort`)
- io.net: `reasoning.effort`
- Alibaba Singapore: `enable_thinking`

**Aegis:** Single `WithThink(*bool)` option on the OpenAI adapter, which maps to the
`think` field in the request body — correct for Ollama's own extension but not for all
providers.

**Objective:** Add a `ReasoningMode` enum to the adapter config (`none`, `ollamaThink`,
`openaiEffort`, `deepseekThinking`, `anthropicBudget`). Select based on provider ID or
auto-detect from the model name prefix. This prevents sending `think: false` to providers
that don't understand it.

---

### 3.4 Usage Fallback Estimation

**Crush:** `internal/agent/usage_fallback.go` — when a provider returns zero usage
(common with local models that don't report token counts), estimates `PromptTokens` by
counting characters in serialized messages ÷ 4, and `CompletionTokens` from output
length. The estimate is flagged so cost calculations are skipped.

**Aegis:** No fallback — `Usage` stays zero for local models. Context-budget-based
compaction becomes unreliable because `InputTokens + OutputTokens == 0` always.

**Objective:** Port the character-count heuristic. When `Usage.InputTokens == 0` after a
successful stream, estimate from message content and flag as estimated. Use the estimated
count for compaction threshold checks but not for cost display.

---

## 4. Context & Memory

### 4.1 Skill File System

**Crush:** `internal/skills/` — discovers markdown files from configured paths
(`~/.crush/skills/`, project `.crush/skills/`, etc.), deduplicates by slug, and injects
the active set as XML into the system prompt at agent build time. Skills can be toggled
on/off per session and are tracked (loaded/not-loaded) per turn for diagnostics.

**Aegis:** Personas (`internal/persona/`) provide named system prompts selected at
session start. No per-session skill injection, no file-based skill discovery.

**Objective:** Extend personas to support skill files. Discover `.aegis/skills/*.md` in
the working directory and inject them similarly to Crush's skill XML block. This lets
users drop project-specific instructions without changing the Aegis source.

---

### 4.2 Orphaned Tool Call Recovery

**Crush:** `filterOrphanedToolResults` and `syntheticToolResultsForOrphanedCalls` — on
every turn, scans the message history for tool_use blocks with no matching tool_result
(e.g. from a canceled turn) and injects synthetic error results. Without this, providers
reject the conversation with a validation error, permanently locking the session.

**Aegis:** No recovery. A canceled turn that left an open tool_use would permanently
break that session's conversation with most providers.

**Objective:** Add orphan detection to `engine.buildHistory()`. Before sending messages
to the provider, walk assistant messages and inject synthetic error `ToolResultBlock`
entries for any tool_use ID that has no matching result.

---

## 5. Session & Observability

### 5.1 Session Title Generation

**Crush:** `GenerateTitle()` fires as a goroutine on the first user turn (before the
main stream). Uses the small model with `maxOutputTokens=40`, strips `<think>…</think>`
tags from the output, and saves the title atomically via `UpdateTitleAndUsage`.

**Aegis:** No title generation. Sessions get a UUID as their name.

**Objective:** After the first user turn, spawn a goroutine that calls the model with a
short title-generation prompt (40 tokens, no tools). Save the result to the session store.
Display it in the sidebar and session picker.

---

### 5.2 Token & Cost Tracking Per Session

**Crush:** `session.Session` accumulates `PromptTokens`, `CompletionTokens`,
`CostUSD` across all turns. Per-turn usage updates are applied in `OnStepFinish`.
Cost is calculated from `catwalk` pricing fields and displayed in the session list.

**Aegis:** `model.inputTokens` / `outputTokens` / `costUSD` are per-turn fields reset
on each stream — not persisted.

**Objective:** Persist running token totals in the `Session` row. Display cumulative cost
in the context bar. For local models, show estimated tokens only (with a `~` prefix).

---

## 6. Priority Matrix

| # | Objective | Impact | Effort | Priority |
|---|-----------|--------|--------|----------|
| 1 | Orphaned tool call recovery | High (session stability) | Low | **P0** |
| 2 | Context-window-aware compaction | High (local model UX) | Low | **P0** |
| 3 | Usage fallback estimation | High (compaction correctness) | Low | **P0** |
| 4 | Streaming markdown prefix cache | High (rendering perf) | Medium | **P1** |
| 5 | Per-tool rich rendering (edit diff first) | High (UX) | Medium | **P1** |
| 6 | Tool status state machine + per-tool spinner | Medium (UX) | Medium | **P1** |
| 7 | Per-provider reasoning control | Medium (compat) | Medium | **P1** |
| 8 | Auto-discover local models | High (local model UX) | Medium | **P2** |
| 9 | Model capability database | Medium (correctness) | Medium | **P2** |
| 10 | Large/small model pair | Medium (quality) | Medium | **P2** |
| 11 | Session title generation | Low (polish) | Low | **P2** |
| 12 | Token & cost tracking | Medium (observability) | Low | **P2** |
| 13 | Queued message dispatch | Medium (reliability) | Medium | **P3** |
| 14 | PrepareStep hook / dynamic tools | Medium (MCP) | Medium | **P3** |
| 15 | Skill file system | Medium (context) | Medium | **P3** |
| 16 | Versioned virtual list | Medium (perf) | High | **P3** |

---

## 7. What NOT to Port

- **OAuth / Hyper / Copilot / Bedrock providers** — Aegis targets local models first.
- **Sub-agent (`agent` tool) architecture** — Crush's coordinator/sub-session design is
  tightly coupled to its database model; not worth porting until Aegis has persistent
  message storage split from session storage.
- **Sourcegraph / web_search tool** — out of scope for local-first use.
- **catwalk library** — an internal Charm library with its own release cadence; build a
  lighter local equivalent (`ModelCapabilities` struct) rather than taking a direct dep.
- **fantasy provider library** — Aegis's custom adapter is simpler and more auditable for
  a local-first use case; keep it.
