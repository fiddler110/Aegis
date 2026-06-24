# Harness Enhancement — Handoff / Resume Notes

_Last updated: 2026-06-24_

This document captures the roadmap and current state so work can resume in a new
session. The full approved roadmap lives at:
`C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`

## TL;DR

**All 7 phases (9–15) of the enhancement roadmap are COMPLETE.**

The harness now has: provider resilience + cost tracking (Phase 9), multi-agent
orchestration with subprocess isolation (Phase 10), background tasks + cron
scheduling (Phase 11), pluggable sandbox backends + contextual security policies
(Phase 12), LSP integration + file staleness + MCP HTTP/SSE + local model
discovery (Phase 13), relevance-scored memory + planning tools + context
discovery (Phase 14), and custom commands/agents/plugins extensibility (Phase 15).

## Git state

- **No git remote.** Everything is local.
- Branches (each stacks on the previous):
  `phase-9-resilience-cost` → `phase-10-multi-agent` → `phase-11-tasks` →
  `phase-12-sandbox` → `phase-13-coding` → `phase-14-memory` → `phase-15-plugins`
- **Currently checked out: `phase-15-plugins`**.

To verify: `go build ./... && go test ./...` (all green).

## Phase summary

| Phase | Branch | What |
|-------|--------|------|
| 9 | `phase-9-resilience-cost` | Retry/backoff, prompt caching, cost tracking, parallel tools, loop detection |
| 10 | `phase-10-multi-agent` | Swarm core, agent definitions, agent tool, worker CLI, audit/TUI |
| 11 | `phase-11-tasks` | Task manager + SQLite, 6 task tools, background shell/agent, cron scheduler |
| 12 | `phase-12-sandbox` | Sandbox backend (Docker/Podman/Apple Containers), path validator, contextual gate |
| 13 | `phase-13-coding` | File staleness, multi_edit, LSP client + tools, MCP HTTP/SSE, model discovery |
| 14 | `phase-14-memory` | Relevance-scored memory, TODO tools, ask_user, context discovery, dry-run CLI |
| 15 | `phase-15-plugins` | Custom commands, custom agents, process tool plugins |

## Phase 15 detail

### 15.1 — Custom slash commands
- **`internal/commands/`** — markdown files with YAML frontmatter (name,
  description, args) + body template. `{{arg}}` placeholders expanded at
  invocation. Discovered from `$DataDir/commands/*.md` and
  `.agentharness/commands/*.md`. Project overrides global.

### 15.2 — Custom agent definitions
- **`internal/agentdef/`** extended — `Register`, `LoadFromDirs`, `ClearCustom`.
  Users drop `agents/*.md` files (YAML frontmatter: name, description, mode,
  tools; body = system prompt). Custom definitions override builtins. Loaded on
  daemon startup from `$DataDir/agents/` and `.agentharness/agents/`.

### 15.3 — Plugin loader (process tools)
- **`internal/plugins/`** — external commands declared in config with JSON Schema.
  Invoked by piping tool input as JSON to stdin, capturing stdout as result.
  Config: `plugins[]` array (name, description, command, args, input_schema,
  capability, timeout_sec). Registered alongside builtins and MCP tools.

## All config knobs

| Key | Default | Description |
|-----|---------|-------------|
| `provider.max_retries` | 4 | Transient failure retries |
| `cost.budget_usd` | 0 | Run cost limit (0 = unlimited) |
| `swarm.backend` | in_process | Sub-agent backend |
| `sandbox.backend` | local | Command sandbox (local/container) |
| `sandbox.runtime` | (auto) | Container runtime preference |
| `sandbox.image` | ubuntu:22.04 | Container image |
| `sandbox.network` | false | Container network access |
| `security.egress_then_write` | false | Require approval for writes after network |
| `security.network_allowlist` | [] | Restrict network to listed domains |
| `lsp[]` | [] | LSP servers (name, command, args, extensions) |
| `plugins[]` | [] | Process tool plugins |
| `mcp[]` | [] | MCP servers (stdio or HTTP) |

## Working conventions
- From-scratch, no heavy deps, stdlib preferred
- `modernc.org/sqlite` (pure Go, driver `"sqlite"`)
- Don't mass-reformat pre-existing files
