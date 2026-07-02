# Tools Reference

Aegis has 39 built-in tools across 13 categories. Tools are exposed to the model as callable functions; the model decides when and how to use them. All tool calls go through the permission gate before execution.

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

Full-text search of the project knowledge base (FTS5-indexed README, documentation, and code comments). Faster than `grep` for finding conceptual content.

```json
{
  "query": "permission mode approval gate"
}
```

The knowledge base is populated by `aegis index`. If no index exists, the tool returns nothing.

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

Search the web via DuckDuckGo. Returns titles, URLs, and snippets.

```json
{
  "query": "golang context cancellation best practices"
}
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
  "facts": "The daemon owns sessions and runs on 127.0.0.1:4127. Auth via bearer token in ~/.local/share/aegis/auth."
}
```

---

### `entity_recall`

**Capability:** read

Search the long-term entity memory for facts matching a query.

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

**Capability:** read

Get LSP diagnostics (errors and warnings) for a file.

```json
{
  "path": "internal/engine/engine.go"
}
```

Returns a list of diagnostics with severity, line/column, message, and source.

---

### `references`

**Capability:** read

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

## Documents & Reports

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

**Capability:** read

Run security scanners against a path and return a normalized findings report.

```json
{
  "path": ".",               // path to scan
  "tools": ["semgrep", "trivy", "gitleaks"]  // optional: subset of tools
}
```

Runs whichever of semgrep, trivy, and gitleaks are installed. Returns findings with: severity (critical/high/medium/low/info), location (file:line), rule ID, message, and remediation hint.

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
