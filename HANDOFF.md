# Harness Enhancement — Handoff / Resume Notes

_Last updated: 2026-06-24_

This document captures the roadmap and current state so work can resume in a new
session. The full approved roadmap lives at:
`C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`

## TL;DR

Implementing a phased enhancement roadmap (Phases 9–15) for the Go agent harness,
porting best-of-breed patterns from **OpenHarness**, **Crush** (charmbracelet),
and **opencode** (anomalyco), plus LangGraph/Omnigent/CrewAI/AG2 ideas.

- **Phase 9 (Resilience & Cost): ✅ COMPLETE**
- **Phase 10 (Multi-agent orchestration): ✅ COMPLETE**
- **Phase 11 (Background tasks & scheduling): ✅ COMPLETE**
- **Phase 12 (Sandboxing & contextual security): ✅ COMPLETE**
- **Phase 13 (Coding intelligence): ✅ COMPLETE**
- Phases 14–15: ⬜ not started

## Git state (IMPORTANT)

- **No git remote is configured.** Everything is local.
- Branches (each stacks on the previous):
  - `phase-9-resilience-cost` → `phase-10-multi-agent` → `phase-11-tasks` → `phase-12-sandbox` → `phase-13-coding`
- **Currently checked out: `phase-13-coding`**.
- Working tree is clean except this `HANDOFF.md` (untracked).

To verify: `go build ./... && go test ./...` (all green).

## What's done

### Phase 9 — Resilience & Cost
- Retry/backoff, prompt caching, cost tracking, parallel tools, loop detection

### Phase 10 — Multi-agent orchestration
- Swarm core, agent definitions, agent tool, worker CLI, audit/TUI

### Phase 11 — Background tasks & scheduling
- Task manager + SQLite, 6 task tools, background shell/agent, cron scheduler

### Phase 12 — Sandboxing & contextual security
- Sandbox backend (local/Docker/Podman/Apple Containers auto-detect)
- Hardened path validator (symlink resolution)
- Contextual gate (egress-then-write, network allowlist)
- Audit integration for policy decisions

### Phase 13 — Coding intelligence

#### 13.1 — File staleness tracking + multi_edit tool
- **`internal/filetracker/tracker.go`** — records read mtimes, detects external
  modifications between read and write. `CheckWrite` rejects stale edits,
  `RecordWrite` updates tracked mtime after agent writes.
- **`internal/tool/builtin/multiedit.go`** — `multi_edit` tool: multiple string
  replacements across multiple files in one call. Validates atomically before
  writing.
- File tools (`read_file`, `write_file`, `edit_file`, `multi_edit`) all wired to
  the tracker via `builtin.Options.FileTracker`.

#### 13.2 — LSP client, diagnostics + references tools
- **`internal/lsp/client.go`** — minimal LSP client over JSON-RPC 2.0 with
  Content-Length framing. Supports initialize, didOpen, didChange,
  textDocument/diagnostic (pull), textDocument/references.
- **`internal/lsp/manager.go`** — manages multiple language servers, routes
  requests by file extension. `Start`, `ClientForFile`, `Close`.
- **`internal/tool/builtin/lsp.go`** — two tools:
  - `diagnostics`: opens file in LSP, returns errors/warnings
  - `references`: finds all references to symbol at position
- Config: `lsp[]` array with name, command, args, extensions per server.
- Server wiring: LSP servers start on daemon init, shut down on exit.

#### 13.3 — MCP HTTP/SSE transport
- **`internal/mcp/http.go`** — HTTP+SSE transport for MCP servers. Requests
  POSTed to /message as JSON-RPC; responses via direct JSON or SSE /sse stream.
- `NewHTTPOrStdio` auto-detects: URL commands use HTTP, others use stdio.
- `RegisterServers` updated to auto-detect transport.

#### 13.4 — Local model discovery
- **`internal/discover/discover.go`** — probes Ollama (:11434 /api/tags),
  LM Studio (:1234 /v1/models), LiteLLM (:4000 /v1/models). Unreachable
  servers silently skipped (2s timeout).
- **`internal/tool/builtin/models.go`** — `list_models` tool exposes discovery
  to the model.

### Config knobs summary
- `provider.max_retries`, `cost.budget_usd`, `swarm.backend`
- `sandbox.backend/runtime/image/network`
- `security.egress_then_write`, `security.network_allowlist`
- `lsp[]` (name, command, args, extensions)
- `mcp[]` (name, command/URL, args, env)

## NEXT: Phase 14 — Richer memory, planning & UX polish

Goal: relevance-scored memory retrieval, TODO/planning tool, ask_user_question,
context discovery, --dry-run + config schema. Full detail in roadmap.

After: Phase 15 (custom tools/agents/commands + plugin loader).

## Working conventions
- From-scratch, no heavy deps, stdlib preferred
- Each phase = commits in `Phase N:` style with `Co-Authored-By:` tag
- Tests accompany each change; `modernc.org/sqlite` (pure Go, driver `"sqlite"`)
- Don't mass-reformat pre-existing files

## How to resume
1. `git checkout phase-13-coding` (or create `phase-14-memory` off it).
2. Re-read the roadmap: `C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`.
3. `go build ./... && go test ./...` to confirm green baseline.
4. Start Phase 14 per the roadmap.
