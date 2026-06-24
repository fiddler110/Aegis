# Harness Enhancement — Handoff / Resume Notes

_Last updated: 2026-06-24_

This document captures the roadmap and current state so work can resume in a new
session. The full approved roadmap lives at:
`C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`

## TL;DR

Implementing a phased enhancement roadmap (Phases 9–15) for the Go agent harness.

- **Phases 9–14: ✅ ALL COMPLETE**
- **Phase 15 (Extensibility): ⬜ not started**

## Git state

- **No git remote.** Everything is local.
- Branches: `phase-9-resilience-cost` → `phase-10-multi-agent` → `phase-11-tasks` → `phase-12-sandbox` → `phase-13-coding` → `phase-14-memory`
- **Currently checked out: `phase-14-memory`**.

To verify: `go build ./... && go test ./...` (all green).

## What's done (Phases 9–14)

### Phase 9 — Resilience & Cost
Retry/backoff, prompt caching, cost tracking, parallel tools, loop detection

### Phase 10 — Multi-agent orchestration
Swarm core, agent definitions, agent tool (sync+async), worker CLI, audit/TUI

### Phase 11 — Background tasks & scheduling
Task manager + SQLite, 6 task tools, background shell/agent, cron scheduler

### Phase 12 — Sandboxing & contextual security
Sandbox backend (local/Docker/Podman/Apple Containers), path validator, contextual gate, audit

### Phase 13 — Coding intelligence
File staleness tracker, multi_edit, LSP client + tools, MCP HTTP/SSE, local model discovery

### Phase 14 — Memory, planning & UX

- **14.1** — Relevance-scored memory retrieval (TF-IDF keyword scoring, top-K + token budget)
- **14.2** — TODO/planning tools (`todo_add`, `todo_update`, `todo_list`) + `ask_user` structured question tool
- **14.3** — Context discovery (`AGENTS.md`, `CLAUDE.md`, `.agentharnessignore`) + `harness dry-run` CLI

## NEXT: Phase 15 — Extensibility

Goal: custom slash commands from markdown, user-defined agent types, plugin loader for external process tools.

## Working conventions
- From-scratch, no heavy deps, stdlib preferred
- `modernc.org/sqlite` (pure Go, driver `"sqlite"`)
- Don't mass-reformat pre-existing files

## How to resume
1. `git checkout phase-14-memory` (or create `phase-15-plugins` off it).
2. Re-read the roadmap: `C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`.
3. `go build ./... && go test ./...` to confirm green baseline.
