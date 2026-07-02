# Memory & Knowledge

Aegis has several systems for persisting information across sessions. Together they let the agent accumulate knowledge about your project and working preferences without having to be told the same things repeatedly.

---

## Overview

| System | Scope | Storage | Loaded into | Purpose |
|--------|-------|---------|-------------|---------|
| Project memory | Project | `.aegis/memory.md` | System prompt | Facts about this project |
| User memory | Global | `~/.local/share/aegis/memory.md` | System prompt | Facts that apply everywhere |
| Skills | Project or global | `*.md` files | System prompt | Reusable procedures |
| Project knowledge base | Project | `.aegis/knowledge.db` | System prompt + `project_knowledge` tool | FTS5-indexed docs and code comments |
| Long-term entity store | Global | `~/.local/share/aegis/longmem.db` | `entity_recall` tool | Cross-session structured facts |

---

## Project Memory

**File:** `.aegis/memory.md`

Project memory stores facts that should always be in context for this project. It is loaded into the system prompt on every turn.

**Adding from the TUI:**
```
/remember The production database is PostgreSQL 15 with row-level security enabled.
```

When prompted for scope, choose **project**.

**Adding with a tool:**

The agent can call `remember` to persist facts:
```json
{
  "text": "The API rate limit is 100 requests per minute per IP.",
  "scope": "project"
}
```

**Viewing:**
```
/memory
```

**Format:** Freeform markdown. You can also edit `.aegis/memory.md` directly in your editor — changes are picked up on the next session start (cached for 5 seconds).

**Example `.aegis/memory.md`:**
```markdown
## Project: Aegis

- Go 1.25+, built with Cobra (CLI) and Bubble Tea (TUI)
- Daemon runs on 127.0.0.1:4127; auth token at ~/.local/share/aegis/auth
- SQLite for sessions, tasks, and knowledge base (modernc.org/sqlite driver)
- Provider adapters: internal/provider/anthropic and internal/provider/openai
- Permission gate in internal/permission/; audit trail as JSONL files
- Do not add unnecessary comments; follow existing code style
- Tests use table-driven patterns; integration tests require a running daemon
```

---

## User Memory

**File:** `~/.local/share/aegis/memory.md` (Linux/macOS) or `%LocalAppData%\aegis\memory.md` (Windows)

User memory stores facts that apply across all projects — working preferences, personal conventions, and context about you.

**Adding from the TUI:**
```
/remember I prefer concise responses without introductory sentences.
```

When prompted for scope, choose **user**.

**Example user memory:**
```markdown
## Working Style

- Prefer concise, direct answers without preamble
- I'm comfortable with Go, Python, and TypeScript; less experienced with Rust
- Always explain the "why" of non-obvious decisions
- I use zsh on macOS and PowerShell on Windows
```

---

## Skills

Skills are reusable procedures saved as markdown files. They are loaded into the system prompt alongside memory, giving the agent step-by-step instructions for repeatable workflows.

**Locations:**
| Scope | Directory |
|-------|-----------|
| Project | `.aegis/skills/*.md` |
| User (global) | `~/.local/share/aegis/skills/*.md` |

**Creating a skill from the TUI:**
```
/remember      # then describe a skill, or use the agent tool:
```

The agent can save skills with `save_skill`:
```json
{
  "name": "deploy-staging",
  "description": "Deploy the application to the staging environment",
  "content": "1. Run `go build -o bin/app ./cmd/app`\n2. Copy binary to staging server: `scp bin/app deploy@staging:/opt/app/`\n3. Restart the service: `ssh deploy@staging 'sudo systemctl restart app'`\n4. Check logs: `ssh deploy@staging 'journalctl -u app -f --lines=50'`",
  "scope": "project"
}
```

**Viewing skills:**
```
/skills
```

**Example skill file** (`.aegis/skills/security-review.md`):
```markdown
# Security Review Procedure

When asked to do a security review:

1. Start with `aegis scan .` to run automated scanners
2. Use `glob` to find all handler/controller files
3. Check each handler for: input validation, authentication, authorization
4. Use `grep` to find all SQL queries and check for injection risks
5. Check for hardcoded secrets: `grep -r "password\|secret\|key\|token" --include="*.go"`
6. Review error handling: errors should not leak sensitive information
7. Check HTTP headers: CORS, CSP, HSTS
8. Summarize findings by severity with file:line citations
```

---

## Project Knowledge Base

**Database:** `.aegis/knowledge.db`

The knowledge base is a SQLite FTS5 index of your project's documentation and code comments. It provides fast, semantic search without reading individual files.

**Building the index:**
```bash
aegis index
```

The index covers:
- `README.md` and all `.md` files in `docs/`
- Exported Go doc comments (`// Package ...`, `// FunctionName ...`)
- Comparable comments in other languages

The cache stores a content fingerprint (path + size + mtime) and is automatically rebuilt when files change.

**Using the knowledge base:**

The agent can search it with the `project_knowledge` tool:
```json
{
  "query": "permission gate approval flow"
}
```

Part of the index is also injected into the system prompt (token budget ~2000 tokens, truncated at file boundaries) so the agent has high-level project context without tool calls.

**Inspecting the repo map:**
```bash
aegis index --print   # show the symbol map
```

The repo map (a separate, lighter index of top-level symbols per file) is injected automatically if `.aegis/repomap.json` exists.

---

## Long-Term Entity Store

**Database:** `~/.local/share/aegis/longmem.db`

The entity store holds cross-session structured facts about named entities — systems, files, APIs, people, or decisions. Unlike project memory (which is a flat text file), entities are keyed and searchable.

**Entity types:**
- `system` — systems, services, infrastructure
- `file` — source files and their purpose
- `api` — APIs, endpoints, contracts
- `person` — people and their roles
- `decision` — architectural or design decisions

**Storing facts** (agent calls `entity_remember`):
```json
{
  "project": "Aegis",
  "entity_type": "system",
  "entity_name": "daemon",
  "facts": "HTTP server on 127.0.0.1:4127. Bearer token auth. Loopback-only. Manages sessions, engine, tool registry. Starts embedded in TUI or runs standalone with `aegis serve`."
}
```

**Recalling facts** (agent calls `entity_recall`):
```json
{
  "query": "daemon authentication bearer token"
}
```

Returns matching facts from all projects, ranked by FTS5 relevance.

The entity store persists across projects. It is not loaded into the system prompt automatically — it is queried on demand by the agent.

---

## Memory Load Order and Caching

At session start, Aegis loads memory in this order:

1. User memory (`~/.local/share/aegis/memory.md`)
2. Project memory (`.aegis/memory.md`)
3. User skills (`~/.local/share/aegis/skills/*.md`)
4. Project skills (`.aegis/skills/*.md`)
5. Repo map (`.aegis/repomap.json` → injected into system prompt)
6. Knowledge base snippet (`.aegis/knowledge.db` → top ~2000 tokens)

Memory files are cached for 5 seconds — file edits are picked up within a few seconds without restarting Aegis.

---

## Relevance Scoring

When multiple memory entries exist, Aegis ranks them by query similarity so the most relevant context surfaces first. This is a lightweight scoring (not full vector search) — exact word matches and proximity to the current query are used.

---

## Practical Patterns

### Remember the right things

Memory is injected into **every** session's system prompt. Keep entries focused and factual — avoid lengthy narratives. Prefer:

```markdown
- API gateway is at https://gateway.internal:8443 (requires mTLS)
```

Over:

```markdown
The API gateway was set up in Q3 by the platform team. It sits in front of all microservices and handles authentication. It requires mutual TLS. The address is https://gateway.internal:8443.
```

### Skills for workflows, memory for facts

Use **memory** for static facts (endpoints, conventions, constraints). Use **skills** for procedures that involve multiple steps (deploy flow, review checklist, test strategy).

### Rebuild the index after major changes

```bash
aegis index
```

Run this after adding significant documentation or refactoring large parts of the codebase. The daemon detects file changes and rebuilds automatically, but triggering it manually ensures the index is fresh before a large session.

### Entity store for cross-project knowledge

If you work on multiple related projects, use the entity store to persist architecture decisions, API contracts, and key people:

```
entity_remember project=Platform entity_type=decision entity_name=auth-strategy
  facts="All services use JWT with RS256. Public key at https://auth.internal/.well-known/jwks.json. Tokens expire after 1 hour."
```

Then any session in any project can recall this with `entity_recall "auth JWT RS256"`.
