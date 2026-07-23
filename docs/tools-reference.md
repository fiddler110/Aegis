# Tools Reference

Aegis has 50+ built-in tools across 14 categories. Tools are exposed to the model as callable functions; the model decides when and how to use them. All tool calls go through the permission gate before execution.

Niche tools (LaTeX, diagram, cron, LSP, long-term memory, agent teams) are registered as **deferred**: the model sees only their names in a compact index and loads full schemas on demand via `tool_search`. This keeps per-turn context lean — especially important for local models.

**Capability tags** control which permission modes allow a tool:

| Capability | Allowed in |
|-----------|-----------|
| `read` | plan, build, auto |
| `write` | build, auto |
| `execute` | build (prompts), auto |
| `network` | plan, build, auto |

---

## File Operations

All file tools are confined to the workspace root. A **file staleness tracker** rejects edits to files modified externally since they were last read, preventing accidental overwrites.

### `read_file`

**Capability:** read

Read a UTF-8 text file. Returns content with 1-based line numbers.

```json
{
  "path": "internal/engine/engine.go",
  "offset": 100,      // optional: start at line 100
  "limit": 50         // optional: read 50 lines
}
```

Binary files return a base64-encoded representation.

---

### `write_file`

**Capability:** write

Create or overwrite a file with content. Creates parent directories if they don't exist.

```json
{
  "path": "internal/config/defaults.go",
  "content": "package config\n\n..."
}
```

---

### `edit_file`

**Capability:** write

Replace an exact string in a file. The string must appear exactly once unless `replace_all` is true. Fails if the string is not found or is ambiguous.

```json
{
  "path": "internal/client/client.go",
  "old_string": "Timeout: 0",
  "new_string": "Timeout: 30 * time.Second",
  "replace_all": false
}
```

Use this tool for targeted edits rather than rewriting whole files with `write_file`.

---

### `multi_edit`

**Capability:** write

Apply multiple string replacements across one or more files in a single call. More efficient than multiple `edit_file` calls when making coordinated changes.

```json
{
  "edits": [
    {
      "path": "internal/server/server.go",
      "old_string": "DefaultAddr = \"localhost:4127\"",
      "new_string": "DefaultAddr = \"127.0.0.1:4127\""
    },
    {
      "path": "internal/client/client.go",
      "old_string": "addr: \"localhost:4127\"",
      "new_string": "addr: \"127.0.0.1:4127\""
    }
  ]
}
```

---

### `glob`

**Capability:** read

Find files matching a glob pattern. Returns a list of matching paths relative to the workspace root.

```json
{
  "pattern": "internal/**/*.go",
  "exclude": ["*_test.go", "vendor/**"]
}
```

---

### `ls`

**Capability:** read

List directory contents as an indented tree. Automatically skips `.git`, `node_modules`, and `vendor`.

```json
{
  "path": "internal/engine"
}
```

---

## Search

### `grep`

**Capability:** read

Search file contents with a regular expression. Returns `path:line:text` matches.

```json
{
  "pattern": "func.*Engine",
  "path": "internal/",        // optional: search scope
  "include": "*.go",          // optional: file pattern filter
  "case_sensitive": false
}
```

---

### `project_knowledge`

**Capability:** read

Full-text search of the project knowledge base (FTS5-indexed README, documentation, and code comments). Faster than `grep` for finding conceptual content. When `embeddings.enabled: true`, results are a reciprocal-rank fusion of BM25 keyword matches and semantic (cosine-similarity) matches — see [Memory & Knowledge → Semantic Recall](memory-and-knowledge.md#semantic-recall-optional).

```json
{
  "query": "permission mode approval gate"
}
```

The knowledge base is populated by `aegis knowledge index` (not `aegis index`, which builds the unrelated repo map). If no index exists, the tool returns nothing.

---

## Git

### `git`

**Capability:** read

Read-only repository inspection. Supports: `status`, `diff`, `log`, `show`, `branch` (listing only), `remote`, `blame`, `ls-files`, `shortlog`, `tag`, `describe`, `rev-parse`, `stash list`.

```json
{
  "args": ["log", "--oneline", "-10"]
}
```

```json
{
  "args": ["diff", "HEAD~1"]
}
```

```json
{
  "args": ["blame", "internal/engine/engine.go", "-L", "100,120"]
}
```

The tool runs `git` with a validated argument vector (never a shell string), confined to the workspace. Flags that could escape the repo (`--git-dir`, `--work-tree`, `--output`, `-c`) and mutating forms of read subcommands (`branch -D`, `tag -d`) are rejected. Available in **plan** mode.

---

### `git_commit`

**Capability:** write

Stage changes and create a commit.

```json
{
  "message": "fix: increase HTTP client timeout to 30s",
  "paths": ["internal/client/client.go"],  // optional: specific files to stage
  "all": false                             // optional: stage all tracked modifications
}
```

By default, stages all tracked modifications. Pass `paths` to stage specific files, or set `all: false` to commit only what is already staged (useful when the agent wants to stage selectively with `shell` first).

Returns the new short commit hash and a diffstat. Reports "nothing to commit" cleanly rather than failing.

**Pre-commit test gate (P46.2).** When `git.pre_commit_test_command` is configured, `git_commit` runs that command in the workspace *before* staging; a non-zero exit aborts the commit and returns the command's output instead, so "tests pass before every commit" is a mechanical gate rather than unenforced prose. Unset (the default) leaves `git_commit` a straight passthrough. Because it executes an arbitrary host command, the setting is frozen from untrusted project config by the workspace-trust gate (see [permissions.md](permissions.md)).

---

### `git_pr`

**Capability:** network

Push the current branch and open a pull request via the `gh` CLI. Falls back to printing a GitHub compare URL if `gh` is not available or not authenticated.

```json
{
  "title": "fix: increase HTTP client timeout to 30s",
  "body": "Increases the default timeout from 0 (no timeout) to 30s to prevent hung connections.",
  "draft": false,        // optional: open as a draft PR
  "base": "main"         // optional: target branch (defaults to repo default)
}
```

Returns the PR URL on success. Used by background sessions to automatically close the loop after autonomous coding work.

---

## Shell

### `shell`

**Capability:** execute

Run a shell command in the workspace directory.

```json
{
  "command": "go test ./internal/engine/...",
  "timeout_ms": 30000,        // optional: override default timeout
  "background": false         // optional: run async, return task ID immediately
}
```

When `background: true`, the tool returns a task ID immediately. Use the task tools to monitor and retrieve output.

Every invocation is gated by the permission mode. In `build` mode, a prompt appears unless `auto_approve_exec: true` or a matching `allow` rule exists.

Commands run with the workspace root as the working directory.

---

## Background Tasks

Tasks let the agent launch long-running operations and check back on them later.

### `task_create`

**Capability:** execute

Launch a long-running command as a background job. Returns a task ID immediately.

```json
{
  "command": "npm run build",
  "title": "Frontend build"   // optional: human-readable label
}
```

---

### `task_list`

**Capability:** read

List all background jobs (newest first).

```json
{}
```

Returns: id, kind, state (running/done/failed), title, created_at.

---

### `task_get`

**Capability:** read

Get the status and tail of a job's output by ID.

```json
{
  "id": "task-abc123"
}
```

---

### `task_output`

**Capability:** read

Get the full accumulated output of a job.

```json
{
  "id": "task-abc123"
}
```

---

### `task_update`

**Capability:** write

Rename a background job (update its title).

```json
{
  "id": "task-abc123",
  "title": "Frontend build (release)"
}
```

---

### `task_stop`

**Capability:** write

Cancel a running job. No-op if the job has already finished.

```json
{
  "id": "task-abc123"
}
```

---

## Scheduling

### `cron_create`

**Capability:** execute

Create a recurring cron job using a standard 5-field cron expression.

```json
{
  "schedule": "0 9 * * 1-5",      // 9am Monday-Friday
  "command": "aegis scan .",
  "title": "Daily security scan"
}
```

No one is present to approve a job when it fires unattended, so at fire time the command is
checked against the full permission gate stack — the same one interactive shell calls get: text
allow/deny rules, then the contextual egress/network policy, then the coarse permission mode
(P27.15/FIND-08). An explicit `deny` rule blocks a job's command regardless of `auto_approve`; an
explicit `allow` rule lets it fire unattended without needing `auto_approve` at all. Absent a
matching rule, a mode-level or contextual-gate approval point (e.g. build-mode execute, or
egress-then-write if enabled) resolves from the job's `auto_approve` flag, since nothing can answer
an interactive prompt here — set it to allow the job to fire even when the daemon's mode would
otherwise require approval.

Run `aegis cron list` from the CLI (not a model-facing tool call) to review persisted jobs as an
operator, including which ones carry `auto_approve`; add `--auto-approve-only` to see just those.

---

### `cron_list`

**Capability:** read

List all cron jobs with ID, schedule, enabled state, and title.

```json
{}
```

---

### `cron_delete`

**Capability:** write

Delete a cron job by ID.

```json
{
  "id": "cron-abc123"
}
```

---

### `cron_toggle`

**Capability:** write

Enable or disable a cron job without deleting it.

```json
{
  "id": "cron-abc123",
  "enabled": false
}
```

---

### `cron_history`

**Capability:** read

List cron job fire-attempt audit history: job id, fired-at time, exit status (`ok`/`error`/
`blocked`), and a truncated snippet of the run's combined output. Most recent first.

```json
{
  "id": "cron-abc123",
  "limit": 20
}
```

`id` is optional (filters to one job); `limit` defaults to 20.

---

## Web

### `web_fetch`

**Capability:** network

Fetch a URL over HTTP/HTTPS. HTML is converted to readable text; other formats (JSON, plain text) are returned as-is.

```json
{
  "url": "https://pkg.go.dev/net/http"
}
```

Private IP addresses are rejected (SSRF protection).

---

### `web_search`

**Capability:** network

Search the web and return titles, URLs, and snippets. The provider is selected by the `search:` config section; DuckDuckGo HTML scraping is the zero-config fallback.

```json
{
  "query": "golang context cancellation best practices"
}
```

Configure a dedicated search API in `config.yaml`:

```yaml
search:
  provider: brave          # brave | tavily | searxng | duckduckgo (default)
  api_key: "$BRAVE_API_KEY"   # expanded from environment / .aegis/.env
  base_url: ""             # required for searxng self-hosted instances
```

---

## Memory & Learning

### `remember`

**Capability:** write

Persist a fact to project or user memory. Loaded into every future session's system prompt.

```json
{
  "text": "The production database uses PostgreSQL 15 with partitioned tables.",
  "scope": "project"   // "project" or "user"
}
```

Project memory goes to `.aegis/memory.md`. User memory goes to the global data directory.

---

### `skill`

**Capability:** read

Load the full body of a skill by name. At session start, only skill names and descriptions are injected as a compact index. Use this tool to fetch the full procedure when you need to follow it.

```json
{
  "name": "deploy-staging"
}
```

Returns the skill's full markdown content. Skills without a `description:` frontmatter field are still eagerly injected for backward compatibility.

---

### `save_skill`

**Capability:** write

Save a reusable procedure as a skill file. Skills are loaded into every future session's system prompt.

```json
{
  "name": "deploy-staging",
  "description": "Deploy to the staging environment",
  "content": "1. Run `go build -o bin/app ./cmd/app`\n2. ...",
  "scope": "project"
}
```

---

### `entity_remember`

**Capability:** write

Persist structured facts about a named entity to the long-term cross-session store.

```json
{
  "project": "Aegis",
  "entity_type": "system",       // system | file | api | person | decision
  "entity_name": "daemon",
  "facts": "The daemon owns sessions and runs on 127.0.0.1:4127. Auth via bearer token in ~/.config/aegis/auth."
}
```

---

### `entity_recall`

**Capability:** read

Search the long-term entity memory for facts matching a query. Ranking is BM25 by default, or hybrid BM25 + semantic when `embeddings.enabled: true` (see [Memory & Knowledge → Semantic Recall](memory-and-knowledge.md#semantic-recall-optional)).

```json
{
  "query": "daemon authentication"
}
```

Returns matching facts across all projects and entity types, ranked by relevance.

---

## Code Intelligence (LSP)

These tools require LSP servers configured in `lsp[]` (see [Configuration](configuration.md)).

### `diagnostics`

**Capability:** read  *(deferred)*

Get LSP diagnostics (errors and warnings) for a file.

```json
{
  "path": "internal/engine/engine.go"
}
```

Returns a list of diagnostics with severity, line/column, message, and source.

---

### `references`

**Capability:** read  *(deferred)*

Find all references to a symbol at a given position using LSP.

```json
{
  "path": "internal/engine/engine.go",
  "line": 42,
  "character": 15
}
```

Returns a list of locations (file, line, character) where the symbol is used.

---

### `definition`

**Capability:** read  *(deferred — load via `tool_search`)*

Jump to the definition of a symbol using LSP.

```json
{
  "path": "internal/engine/engine.go",
  "line": 42,
  "character": 15
}
```

Returns the file path and position of the symbol's declaration.

---

### `hover`

**Capability:** read  *(deferred)*

Get hover documentation for a symbol using LSP.

```json
{
  "path": "internal/engine/engine.go",
  "line": 42,
  "character": 15
}
```

Returns the LSP hover content (type signature, docstring, etc.).

---

### `document_symbols`

**Capability:** read  *(deferred)*

List all symbols (functions, types, variables) in a file.

```json
{
  "path": "internal/engine/engine.go"
}
```

Returns a hierarchical list of symbol names, kinds, and locations.

---

### `workspace_symbols`

**Capability:** read  *(deferred)*

Search for symbols across the entire workspace by name.

```json
{
  "query": "Engine"
}
```

Returns all matching symbols (name, kind, location) across all open files and packages.

---

### `call_hierarchy`

**Capability:** read  *(deferred)*

Get the call hierarchy (callers or callees) for a function using LSP.

```json
{
  "path": "internal/engine/engine.go",
  "line": 42,
  "character": 15,
  "direction": "incoming"   // "incoming" (callers) or "outgoing" (callees)
}
```

---

## Discovery & Meta

### `tool_search`

**Capability:** read

Search for deferred tools by name or keyword and load their schemas into the session registry. Deferred tools are advertised only by name at session start to save context; calling `tool_search` makes them callable.

```json
{
  "query": "latex diagram"    // keyword search across deferred tool names and descriptions
}
```

Returns matching tool schemas and registers them for immediate use. The model should call this when it needs a capability that was mentioned in `<deferred_tools>` but not yet loaded.

---

## Multi-Agent

### `latex_build`

**Capability:** execute

Compile a `.tex` file to PDF. Runs 1–3 passes to resolve cross-references and table of contents.

```json
{
  "path": "report.tex",
  "engine": "xelatex",    // "xelatex" (default), "pdflatex", or "lualatex"
  "runs": 2,              // number of compilation passes (1-3, default 2)
  "check_only": false     // true = syntax validation only, no PDF written
}
```

Returns: errors with context lines, deduplicated warnings, page count, output PDF path.

---

### `latex_new_document`

**Capability:** write

Create a new `.tex` file with a production-quality preamble for enterprise reports, white papers, and technical documents.

```json
{
  "path": "security-report.tex",
  "style": "report",        // "report", "whitepaper", "article", "book"
  "title": "Security Assessment Report",
  "author": "Security Team",
  "sections": ["Executive Summary", "Findings", "Recommendations"]
}
```

The generated preamble includes: professional typography, semantic heading colors, `booktabs` tables, `listings` code blocks, `tcolorbox` callout boxes (`notebox` / `warnbox` / `keybox`), figure captions, `hyperref` PDF metadata, and `%%TODO` markers at each section.

**Typical workflow:**
1. `glob` + `read_file` — collect source material
2. `latex_new_document` — scaffold the template
3. `edit_file` — fill each `%%TODO`
4. `latex_build` — compile to PDF

---

## Diagramming

### `render_diagram`

**Capability:** write

Render a diagram to SVG, PNG, PDF, or draw.io format.

```json
{
  "type": "mermaid",          // mermaid, plantuml, c4plantuml, graphviz, drawio, ...
  "source": "graph TD\n  A --> B",
  "output_path": "docs/architecture.svg",
  "format": "svg"             // svg, png, pdf
}
```

Uses [Kroki](https://kroki.io) by default (configurable via `diagram.kroki_url`). Falls back to local CLI tools (Mermaid CLI, PlantUML, Graphviz dot) if Kroki is unavailable.

---

## Security

### `security_scan`

**Capability:** execute

Run available static/dependency/secrets scanners (opengrep, trivy, gitleaks, kubescape, hadolint, osv-scanner, grype, and opt-in engines) against a path — or a built container image, or generate an SBOM instead — and return a normalized findings report. See [Security Features](security_scan.md) for the full scanner list and config.

```json
{
  "path": "."                 // optional: workspace-relative subdirectory; defaults to the whole workspace
}
```

```json
{
  "image": "alpine:3.20"      // scan a built container image instead of the workspace; mutually exclusive with path
}
```

```json
{
  "sbom": true                // generate a CycloneDX SBOM via syft instead of scanning for findings; mutually exclusive with image
}
```

```json
{
  "scanners": ["trufflehog"]  // run only these scanner names and/or category aliases (e.g. "secrets", "sast"), force-enabled for this run regardless of config; omit to run every enabled scanner plus auto-detected language-specific engines. Mutually exclusive with image/sbom.
}
```

Returns findings with: severity (critical/high/medium/low/info), location (file:line), rule ID, message, and remediation hint; deduped across overlapping tools and tagged with an OWASP ASVS chapter where confidently derivable. Every run also persists its report to `.aegis/security/scan.json` (or `image.json` for an image scan) — same artifact `aegis scan`/`/scan` produce.

---

### `dast_scan` (deferred)

**Capability:** execute

Dynamic Application Security Testing via OWASP ZAP: crawls (and, in `active`/`api` mode, actively attacks) a *running* web application to find real, exploitable vulnerabilities. Container-only (`security.tools.zap.image`, digest-pinned).

```json
{
  "target": "http://localhost:3000",
  "mode": "baseline",          // "baseline" (passive, default), "active", or "api"
  "api_definition": ""         // OpenAPI spec URL/path; required when mode is "api"
}
```

The target must be loopback/private (allowed by default) or explicitly declared in `security.dast.allowed_targets` — checked unconditionally, independent of permission mode. `active`/`api` modes additionally require `security.dast.allow_active: true`. Persists its report to `.aegis/security/dast.json`. See [Security Features](security_scan.md#dynamic-application-security-testing-dast) for the full gating.

---

### `recon_scan` (deferred)

**Capability:** execute

Network/host reconnaissance for attack-surface mapping: nmap discovers live hosts, open ports, and service/version banners across a target host list or CIDR range; nuclei then matches its community template library (CVEs, misconfigurations, exposed panels) against whatever nmap found alive.

```json
{
  "targets": ["192.168.1.0/24", "db.lan"]   // bare hosts, IPs, or CIDR ranges — no http(s):// scheme
}
```

Shares its target-authorization gate with `dast_scan` (loopback/private allowed by default, else must be declared in `security.dast.allowed_targets`), checked individually per target with a 256-target cap per call. `nuclei` additionally requires `security.tools.nuclei.templates_version` (a pinned `nuclei-templates` release tag). Host-binary only — no container fallback. `security.dast.allow_active: true` unlocks nmap's OS-detection/full-port-range/default-script mode and nuclei's full template set. Persists its report to `.aegis/security/network.json`. See [Security Features](security_scan.md#network--host-reconnaissance-nmap--nuclei).

---

### `security_advise` (deferred)

**Capability:** network

Security engagement assistant: a persistent, multi-day engagement notebook plus NVD CVE lookups and guarded, rule-based next-step suggestions. Notebooks are scoped to a named `engagement` string (not the chat session) so notes survive across sessions and daemon restarts.

```json
{
  "action": "note",             // "note", "list" (alias "log"), "cve_lookup", "suggest", or "status"
  "engagement": "acme-2026q3",  // required for note/list/log/suggest/status
  "text": "recon_scan found an admin panel on 10.0.0.5",
  "tags": ["recon"]             // optional, action: note
}
```

```json
{
  "action": "cve_lookup",
  "cve_id": "CVE-2021-44228"     // or "keyword": "log4j" (mutually exclusive), plus optional "limit"
}
```

`note` appends a timestamped, optionally-tagged note to the named engagement's notebook (stored under the daemon's per-user data directory, one JSONL file per engagement). `list`/`log` returns every note, oldest first. `cve_lookup` queries the NVD REST API (`https://services.nvd.nist.gov/rest/json/cves/2.0`); the unauthenticated public API is rate-limited to roughly 5 requests/30s — a 403/429 comes back as a clear error, not a hang or crash — set `NVD_API_KEY` in the environment for a higher limit. `suggest` returns plain-text next-step suggestions derived from simple, explainable rules over the notebook's own content (e.g. no recon logged yet, or findings referenced but never documented) — it never executes another tool or scan itself; a human or the calling model decides whether to act on a suggestion. `status` returns a short digest (note count, date range, and how many notes reference recon/dast/security_scan/findings/cve lookups) — a tool-action fallback for the P13.4 status-digest scope rather than a `/status` (`api.StatusInfo`) field, since that endpoint is daemon-global with no precedent for a per-entity key.

---

### `scope` (deferred)

**Capability:** read

Declare or clear a **per-task file-write scope** (P46.1): an allowlist of workspace-relative path globs that `write_file`/`edit_file`/`multi_edit` calls must stay within until it is cleared. Once set, a write to any file outside the scope is *refused by the permission gate*, not merely discouraged — so a task that should only touch a handful of files can say so and have it enforced.

```json
{
  "action": "set",                       // "set", "clear", or "show"
  "paths": ["internal/auth/**", "cmd/main.go"]  // required for "set"
}
```

Globs use `*`/`?` wildcards where `*` spans path separators (so `src/**` and `src/*` both cover `src/a/b.go`); paths and patterns are normalized (separator/case/`..` cleanup) so a traversal or case trick can't dodge the scope. Reads are never restricted. The scope is per session and persists across turns until you `clear` it (or set a new one). Capability is **read** — setting a scope only tightens what writes are allowed, so it's usable even in plan mode. Used together with `git_commit`'s pre-commit test gate, the built-in `structured-build` skill drives a one-task-one-commit workflow on top of both.

---

## Planning

### `todo_add`

**Capability:** write

Add a task to the in-session todo list.

```json
{
  "text": "Fix the connection timeout handling",
  "priority": "high"    // optional: "high", "medium", "low"
}
```

---

### `todo_list`

**Capability:** read

Show the current todo list with status markers.

```json
{}
```

Returns items with status: `[ ]` pending, `[~]` in_progress, `[x]` done.

---

### `todo_update`

**Capability:** write

Update a todo item's status.

```json
{
  "id": "todo-1",
  "status": "done"    // "pending", "in_progress", "done"
}
```

---

## User Interaction

### `ask_user`

**Capability:** read

Ask the user a question and wait for their answer. Pauses the agent loop until the user responds.

```json
{
  "question": "Which database should we migrate to?",
  "type": "single_choice",       // "free_text", "single_choice", "multi_choice"
  "options": ["PostgreSQL", "MySQL", "SQLite"]
}
```

---

## Discovery

### `list_models`

**Capability:** read

Probe localhost for running local model servers and list available models.

```json
{
  "include_remote": false
}
```

Probes Ollama (`:11434`), LM Studio (`:1234`), and LiteLLM (`:4000`). Returns model names and server types.

---

## Multi-Agent

### `agent`

**Capability:** spawn

Delegate a task to a sub-agent. The sub-agent runs independently with its own engine loop, tool registry, and permission scope (cannot exceed the parent's).

```json
{
  "prompt": "Review all files in internal/engine/ for potential race conditions",
  "subagent_type": "general",    // optional: agent type or custom agent name
  "background": false,           // optional: return task ID without waiting
  "mode": "plan"                 // optional: permission mode for sub-agent
}
```

**Execution modes:**
- `background: false` (default) — wait for the sub-agent to complete, return its answer
- `background: true` — return a task ID immediately; monitor with `task_get`

**Workflow patterns:**
- **Sequential** — spawn agents one at a time, pass output forward
- **Parallel** — spawn multiple agents with `background: true`, collect results with `task_output`
- **Loop** — repeat the same agent until a condition is met

Recursion depth is limited to 3 levels.

---

## Agent Teams

Agent teams use a SQLite-backed shared task list and a file mailbox for peer-to-peer messaging. Multiple agents claim and complete tasks independently without a parent-child hierarchy. All team tools are deferred — load them with `tool_search`.

### `team_task_add`

**Capability:** write  *(deferred)*

Add a task to the shared team task list.

```json
{
  "title": "Review internal/permission/ for correctness",
  "description": "Check all rule evaluation paths",   // optional
  "priority": "high"                                   // optional: "high", "medium", "low"
}
```

Returns the task ID. Any team agent can claim and complete it.

---

### `team_task_list`

**Capability:** read  *(deferred)*

List all team tasks with their current state.

```json
{}
```

Returns: id, title, state (pending/claimed/done), claimed_by, priority.

---

### `team_task_claim`

**Capability:** write  *(deferred)*

Atomically claim a pending task. Fails if another agent already claimed it.

```json
{
  "task_id": "teamtask-abc123"
}
```

Returns the task details on success. Used to prevent two agents working the same task.

---

### `team_task_complete`

**Capability:** write  *(deferred)*

Mark a claimed task as done and record the result.

```json
{
  "task_id": "teamtask-abc123",
  "result": "Found 2 issues in rules.go, see findings above"
}
```

---

### `team_send`

**Capability:** write  *(deferred)*

Send a message to a peer agent via the file mailbox.

```json
{
  "to": "agent-abc123",       // recipient agent session ID
  "subject": "handoff",
  "body": "Finished reviewing auth — starting on session store next"
}
```

---

### `team_inbox`

**Capability:** read  *(deferred)*

Read messages sent to this agent by peers.

```json
{
  "since": "2026-07-02T10:00:00Z"   // optional: only messages after this timestamp
}
```

Returns a list of messages with sender, subject, body, and timestamp.
