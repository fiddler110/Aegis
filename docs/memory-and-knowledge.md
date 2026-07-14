# Memory & Knowledge

Aegis has several systems for persisting information across sessions. Together they let the agent accumulate knowledge about your project and working preferences without having to be told the same things repeatedly.

---

## Overview

| System | Scope | Storage | Loaded into | Purpose |
|--------|-------|---------|-------------|---------|
| Project memory | Project | `.aegis/memory.md` | System prompt | Facts about this project |
| User memory | Global | `~/.config/aegis/memory.md` | System prompt | Facts that apply everywhere |
| Skills | Project or global | `*.md` files | System prompt | Reusable procedures |
| Project knowledge base | Project | `.aegis/knowledge.db` | `project_knowledge` tool | FTS5-indexed docs and code comments, optionally hybrid BM25+semantic |
| Long-term entity store | Global | `~/.config/aegis/longmem.db` | `entity_recall` tool | Cross-session structured facts, optionally hybrid BM25+semantic |

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

**Integrity check:** Aegis records a sha256 hash of each memory file's content in a sidecar file next to it (`.aegis/memory.md.integrity`) every time it writes via `remember`/`/remember`. If the file's content on disk no longer matches that recorded hash the next time it's loaded — i.e. it was edited by something other than Aegis's own write path — a visible `⚠️ integrity check failed` warning is prepended to that memory's injected text, and a warning is logged. The content itself is never dropped (a mismatch doesn't necessarily mean malicious tampering — you may have hand-edited the file on purpose), so intentional manual edits still load; they just also carry the warning banner until the next `remember` refreshes the sidecar's baseline. A file with no sidecar at all (a pre-existing `memory.md` from before this check existed, or its very first write) loads silently with no warning — the sidecar is simply (re)established as the new trust baseline for future loads. This is a tamper-detection heuristic, not cryptographic authentication: anyone with enough host/OS access to edit `memory.md` can also edit or delete its sidecar, so it does not stop a determined attacker — see [FIND-30](../threat-model-20260710-173718/3-findings.md) for the full threat-model writeup.
```markdown
## Project: Aegis

- Go 1.25+, built with Cobra (CLI) and Bubble Tea (TUI)
- Daemon runs on 127.0.0.1:4127; auth token at ~/.config/aegis/auth
- SQLite for sessions, tasks, and knowledge base (modernc.org/sqlite driver)
- Provider adapters: internal/provider/anthropic and internal/provider/openai
- Permission gate in internal/permission/; audit trail as JSONL files
- Do not add unnecessary comments; follow existing code style
- Tests use table-driven patterns; integration tests require a running daemon
```

---

## User Memory

**File:** `~/.config/aegis/memory.md` (Linux/macOS) or `%AppData%\aegis\memory.md` (Windows)

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

Skills are reusable procedures saved as markdown files, loaded on demand (progressive disclosure) rather than eagerly into every system prompt. See [Skills](skills.md) for the full authoring guide (frontmatter, bundled directory skills with companion assets, precedence, built-ins). Quick reference:

**Locations:**
| Scope | Directory |
|-------|-----------|
| Project | `.aegis/skills/*.md` |
| User (global) | `~/.aegis/skills/*.md` |

**Creating a skill from the TUI:**
```
/remember      # then describe a skill, or use the agent tool:
```

The agent can save skills with `save_skill` — this always writes to the *project* skills directory with exactly the content given (no `description`/`scope` params, so add frontmatter yourself afterward if you want progressive disclosure):
```json
{
  "name": "deploy-staging",
  "content": "1. Run `go build -o bin/app ./cmd/app`\n2. Copy binary to staging server: `scp bin/app deploy@staging:/opt/app/`\n3. Restart the service: `ssh deploy@staging 'sudo systemctl restart app'`\n4. Check logs: `ssh deploy@staging 'journalctl -u app -f --lines=50'`"
}
```

**Viewing skills:**
```
/skills
```

### Built-in skills

Aegis also ships several skills embedded in the binary — `content-review`, `html-report`, `security-audit`, `architecture-diagram`, `debug-investigation`, `redteam-engagement`, `threat-modeling`, `latex-report`, `deep-research` — covering common workflows out of the box. They stay **dormant by default** (no system-prompt cost) until enabled, since unlike a project/user skill file a user didn't choose to author them:

```bash
aegis skills list                          # see all built-ins and their on/off status
aegis skills enable security-audit          # project config (.aegis/config.yaml)
aegis skills enable security-audit --global # user config instead
aegis skills disable security-audit
```

Or from the TUI: `/skills` (list), `/skills enable <name> [global]`, `/skills disable <name> [global]`. Changes take effect on the next restart. A project or user skill file with the same name always takes precedence over a built-in.

`/threat-model`, `/report`, `/research`, and `/review` don't need any of this — they activate their skill for the current session on demand, the moment you run the command, with no config edit or restart (see [docs/skills.md](skills.md#on-demand-activation)).

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

**Database:** `.aegis/knowledge.db` (project-scoped — each project gets its own index)

The knowledge base is a SQLite FTS5 index of your project's documentation and code comments, searched with BM25 keyword ranking by default (or hybrid BM25 + semantic ranking when [embeddings are enabled](#semantic-recall-optional)). It provides fast search without reading individual files.

**Building the index:**
```bash
aegis knowledge index
```

The index covers:
- `README.md` and all `.md`/`.txt`/`.rst` files in the project
- Go doc comments (`// Package ...`, `// FunctionName ...`)
- Comparable comments in other languages

Rerun this after adding significant documentation or refactoring large parts of the codebase — it's a full reindex, not incremental.

> Don't confuse this with `aegis index`, which builds a *different*, lighter artifact: the repo map (`.aegis/repomap.json`), a structural symbol listing (top-level functions/types per file) injected into the system prompt automatically when present. `aegis knowledge index` builds the searchable prose/comment index behind the `project_knowledge` tool. Both are useful together; neither substitutes for the other.

**Using the knowledge base:**

The agent can search it with the `project_knowledge` tool:
```json
{
  "query": "permission gate approval flow"
}
```

**Inspecting the repo map:**
```bash
aegis index --print   # show the symbol map
```

---

## Semantic Recall (optional)

**Config:** `embeddings.*` — see [Configuration Reference](configuration.md#full-config-reference)

By default, the project knowledge base and the long-term entity store are BM25-only (FTS5) — no extra service required. Setting `embeddings.enabled: true` adds a semantic layer: a local Ollama embedding model (default `nomic-embed-text`) embeds every indexed document/fact, and searches become the [reciprocal-rank fusion](https://en.wikipedia.org/wiki/Learning_to_rank#Reciprocal_rank_fusion) of the BM25 ranking and a cosine-similarity ranking over those embeddings. This surfaces matches that share no keywords with the query but are topically related.

```yaml
embeddings:
  enabled: true
  provider: ollama
  model: "nomic-embed-text"
  base_url: "http://localhost:11434"
```

```bash
ollama pull nomic-embed-text
aegis knowledge index   # re-embeds existing docs once enabled
```

If the embedder is unreachable or misconfigured at search time, both stores silently fall back to BM25-only results for that query — semantic recall is strictly additive, never a hard dependency.

---

## Long-Term Entity Store

**Database:** `~/.config/aegis/longmem.db`

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

1. User memory (`~/.config/aegis/memory.md`)
2. Project memory (`.aegis/memory.md`)
3. Project skills (`.aegis/skills/*.md`) — checked first, so a project skill shadows a same-named user skill
4. User skills (`~/.aegis/skills/*.md`)
5. Repo map (`.aegis/repomap.json` → injected into system prompt)

Memory files are cached for 5 seconds — file edits are picked up within a few seconds without restarting Aegis. Each memory file's integrity is re-checked against its `.integrity` sidecar on every (post-cache) load — see [Project Memory](#project-memory) above.

The project knowledge base and long-term entity store are *not* injected into the system prompt — they're queried on demand via the `project_knowledge` and `entity_recall` tools, keeping their (potentially large) content out of every turn's token budget.

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
aegis knowledge index
```

Run this after adding significant documentation. Unlike the repo map (which the daemon rebuilds automatically when it detects file changes), the knowledge base is a full reindex you trigger manually — there's no incremental staleness detection, so run it before a large session if docs have moved since the last index.

### Entity store for cross-project knowledge

If you work on multiple related projects, use the entity store to persist architecture decisions, API contracts, and key people:

```
entity_remember project=Platform entity_type=decision entity_name=auth-strategy
  facts="All services use JWT with RS256. Public key at https://auth.internal/.well-known/jwks.json. Tokens expire after 1 hour."
```

Then any session in any project can recall this with `entity_recall "auth JWT RS256"`.
