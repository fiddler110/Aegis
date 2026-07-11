# Extensibility

Aegis can be extended in four main ways: MCP servers (external tools via a standard protocol), custom slash commands (prompt templates), custom agent definitions (reusable personas), and process plugins (external command tools). Bundles package these together for distribution.

---

## Lifecycle Hooks

Hooks let you run shell commands (or any executable) in response to agent lifecycle events — no Go code required.

### Configuration

```yaml
hooks:
  pre_tool_use:   "my-hook pre"    # runs before every tool call
  post_tool_use:  "my-hook post"   # runs after every tool call
  session_start:  ""               # runs when a session starts
  stop:           ""               # runs when the agent finishes
  subagent_stop:  ""               # runs when a sub-agent finishes
```

### Event contract

Aegis writes a JSON event object to the hook command's **stdin**. For `pre_tool_use`:

```json
{
  "event": "pre_tool_use",
  "session_id": "abc12345",
  "tool_name": "shell",
  "tool_input": {"command": "rm -rf build/"},
  "timestamp": "2026-07-02T10:00:00Z"
}
```

**Exit codes:**
- `0` — allow (proceed normally)
- `2` — **veto** the tool call; the hook's stderr is returned to the model as an error message
- Any other non-zero — logged as a warning; execution continues

### Examples

**Lint on file write:**
```bash
#!/bin/sh
# pre-tool-lint — runs before every tool call
input=$(cat)
tool=$(echo "$input" | jq -r '.tool_name')
if [ "$tool" = "write_file" ] || [ "$tool" = "edit_file" ]; then
    path=$(echo "$input" | jq -r '.tool_input.path')
    golangci-lint run "$path" 2>&1 || exit 2
fi
```

**Audit trail to a custom log:**
```bash
#!/bin/sh
cat >> /var/log/aegis-audit.jsonl
```

**Block writes to production config:**
```bash
#!/bin/sh
input=$(cat)
path=$(echo "$input" | jq -r '.tool_input.path // empty')
if echo "$path" | grep -q '^/etc/production'; then
    echo "Writes to production config are not allowed" >&2
    exit 2
fi
```

Hooks complement the `permission.rules` text-rule system. Rules are evaluated first (fast, in-process); hooks run after (shell-out, can do arbitrary logic).

---

## MCP Servers

[Model Context Protocol](https://modelcontextprotocol.io) (MCP) servers expose additional tools to the agent. From the agent's perspective, MCP tools appear alongside built-in tools with no distinction.

MCP servers are third-party code and their tool output is treated as
untrusted external input — see [MCP Trust Boundary](mcp-trust-boundary.md)
for how Aegis marks that output's provenance and the opt-in `scan_output`
heuristic scan, in addition to the capability gating described below. The
same trust boundary applies in the outbound direction: tool-call arguments
are forwarded verbatim to the target server, and the opt-in
`scan_arguments` check (same doc) flags credential-shaped content in them
before they leave the process.

Two transport modes are supported:
- **stdio** — Aegis launches the server as a child process and communicates over stdin/stdout (JSON-RPC)
- **HTTP/SSE** — Aegis connects to a running HTTP server

### Configuration

```yaml
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]

  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_..."

  - name: postgres
    command: npx
    args: ["-y", "@modelcontextprotocol/server-postgres"]
    env:
      DATABASE_URL: "$DATABASE_URL"   # expanded from environment

  # HTTP/SSE transport: empty command = HTTP mode
  - name: my-http-server
    command: ""
    auth: "$MY_MCP_TOKEN"   # $VAR or ${VAR} expanded from environment / .aegis/.env
```

### Keeping secrets out of config

Place sensitive values in `.aegis/.env` (gitignored):

```ini
# .aegis/.env
MY_MCP_TOKEN=secret-bearer-token-here
DATABASE_URL=postgres://user:pass@localhost/db
```

These are available as `$VAR` in supported YAML fields (`mcp[].auth`, `mcp[].env` values).

### Useful MCP servers

| Server | Package | What it adds |
|--------|---------|-------------|
| Filesystem | `@modelcontextprotocol/server-filesystem` | File operations outside workspace |
| GitHub | `@modelcontextprotocol/server-github` | GitHub API (issues, PRs, search) |
| Postgres | `@modelcontextprotocol/server-postgres` | SQL queries, schema inspection |
| Brave Search | `@modelcontextprotocol/server-brave-search` | Web search via Brave |
| Puppeteer | `@modelcontextprotocol/server-puppeteer` | Browser automation |
| Slack | `@modelcontextprotocol/server-slack` | Slack messaging |

---

## Custom Commands

Custom slash commands are markdown files that define prompt templates. They appear as `/command-name` in the TUI alongside built-in commands.

### Locations

| Scope | Directory |
|-------|-----------|
| Project | `.aegis/commands/*.md` |
| User (global) | `~/.local/share/aegis/commands/*.md` |

Project commands override user commands with the same name.

### File Format

```markdown
---
name: review
description: Review code changes for a given file
args: [file]
---
Please review the code in {{file}} for:
- Bugs and logic errors
- Security vulnerabilities (reference OWASP/CWE)
- Performance issues
- Code style and maintainability

Provide specific line-number references for each finding.
```

**Frontmatter fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | The slash command name (used as `/name` in TUI) |
| `description` | string | Shown in the command completion popup |
| `args` | list | Named argument placeholders (used as `{{arg}}` in body) |

**In the TUI:**
```
/review internal/engine/engine.go
```

The argument is substituted for `{{file}}` and the result is sent as a user message.

### Examples

**Quick PR description:**
```markdown
---
name: pr-desc
description: Generate a PR description for the current changes
args: []
---
Generate a pull request description for the current git diff. Include:
1. A concise title (under 70 characters)
2. A "What changed" bullet list
3. A "Why" section explaining the motivation
4. A test plan checklist

Use `git diff HEAD` to see the changes.
```

**Security checklist:**
```markdown
---
name: sec-check
description: Run a security checklist on a file
args: [path]
---
Perform a security review of {{path}}. Check for:

**Injection:** SQL injection, command injection, LDAP injection, XSS
**Authentication:** Broken auth, session fixation, weak credentials
**Authorization:** Missing access controls, privilege escalation
**Data exposure:** Sensitive data in logs, responses, or storage
**Cryptography:** Weak algorithms, hardcoded keys, improper cert validation
**Dependencies:** Known CVEs in imports

For each finding, cite: severity (Critical/High/Medium/Low), file:line, CWE ID, and remediation.
```

**Architecture diagram:**
```markdown
---
name: diagram
description: Generate a Mermaid architecture diagram
args: [scope]
---
Generate a Mermaid architecture diagram for {{scope}}.

1. Use `glob` and `read_file` to understand the structure
2. Create a C4-style context or component diagram
3. Render it with `render_diagram` (type: mermaid, output: docs/architecture.svg)

Focus on key components, their relationships, and data flows.
```

**Listing custom commands:**
```
/commands
```

---

## Custom Agent Definitions

Agent definitions are reusable sub-agent configurations. They let you define specialized agents with specific system prompts and tool focus for use with the `agent` tool or `--persona` flag.

### Locations

| Scope | Directory |
|-------|-----------|
| Project | `.aegis/agents/*.md` |
| User (global) | `~/.local/share/aegis/agents/*.md` |

Project agents override user agents with the same name.

### File Format

```markdown
---
name: reviewer
description: Strict code reviewer focused on correctness and security
mode: plan
tools: [read_file, glob, grep, diagnostics]
---
You are a senior software engineer performing a thorough code review.

Your approach:
1. Read the full file before commenting
2. Focus on correctness first, then security, then style
3. Cite specific line numbers for every finding
4. Suggest concrete fixes, not abstract advice
5. Group findings by severity: Critical, High, Medium, Low

Never suggest changes unrelated to the current review scope.
```

**Frontmatter fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Agent name (used with `--persona` or as `subagent_type` in `agent` tool) |
| `description` | string | Shown in persona picker |
| `mode` | string | Default permission mode: `plan`, `build`, `auto` |
| `tools` | list | Informational tool list shown in UI |

The file body becomes the system prompt for sessions using this agent.

### Using custom agents

**As a persona:**
```bash
aegis --persona reviewer
```

**As a sub-agent:**
```json
{
  "prompt": "Review all files changed in this PR",
  "subagent_type": "reviewer"
}
```

### Examples

**Documentation writer:**
```markdown
---
name: doc-writer
description: Technical documentation writer
mode: plan
---
You are a technical writer specializing in developer documentation.

Your output:
- Clear prose, not bullet point soup
- Code examples for every non-trivial concept
- Prerequisites stated upfront
- Links to related documentation
- Accurate: based on reading the actual source, not assumptions

When documenting a function or package, always read the source first.
```

**Test generator:**
```markdown
---
name: test-gen
description: Go test generator following project conventions
mode: build
---
You write Go tests that follow these conventions:
- Table-driven tests with `t.Run` subtests
- Descriptive test names: `TestFunctionName_Scenario_ExpectedBehavior`
- `require` for fatal assertions, `assert` for non-fatal (testify)
- No mocks unless the interface is clearly external (HTTP, database)
- Integration tests in `*_integration_test.go` with build tag `//go:build integration`

Before writing tests, read the file under test and any existing test files.
```

---

## Process Plugins

Process plugins register external commands as tools. Aegis pipes tool input as JSON to the command's stdin and captures its stdout as the result. From the agent's perspective, these appear as first-class tools.

### Configuration

```yaml
plugins:
  - name: check_types
    description: "Run TypeScript type checking on a file"
    command: npx
    args: ["tsc", "--noEmit", "--pretty"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}'
    capability: read
    timeout_sec: 60

  - name: eslint
    description: "Lint JavaScript/TypeScript files"
    command: npx
    args: ["eslint", "--format", "json"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}'
    capability: read
    timeout_sec: 30

  - name: terraform_validate
    description: "Validate a Terraform configuration directory"
    command: terraform
    args: ["validate", "-json"]
    input_schema: '{"type":"object","properties":{"dir":{"type":"string"}}}'
    capability: read
    timeout_sec: 60
```

**Plugin config fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Tool name as seen by the model |
| `description` | string | Tool description in the model's tool list |
| `command` | string | Executable to run |
| `args` | list | Additional arguments (prepended to the call) |
| `input_schema` | string | JSON Schema for the tool's input (also used by the model to know how to call it) |
| `capability` | string | `read`, `write`, `execute`, or `network` — determines permission gating |
| `timeout_sec` | int | Execution timeout in seconds |

### How it works

When the agent calls a plugin tool:

1. Aegis passes the tool input JSON to the command's **stdin**
2. The command runs and produces output to **stdout**
3. Aegis captures stdout and returns it as the tool result
4. stderr is captured for error reporting but not returned to the model

**Example stdin for `check_types` with input `{"path": "src/index.ts"}`:**
```json
{"path": "src/index.ts"}
```

---

## Plugin Bundles

A bundle is a distribution unit that packages commands, agents, and skills together for easy sharing and installation.

### Bundle structure

```
my-bundle/
  bundle.yaml          manifest
  commands/
    review.md
    sec-check.md
  agents/
    secure-reviewer.md
  skills/
    deploy.md
```

**`bundle.yaml`:**
```yaml
name: security-toolkit
version: "1.0.0"
description: Security review commands and agents for Go microservices
author: security-team
```

### Installing a bundle

```bash
# From a local directory
aegis bundle info ./my-bundle             # preview what would be installed
aegis bundle install ./my-bundle          # install to .aegis/ (project scope)
aegis bundle install ./my-bundle --scope user    # install to user data dir
aegis bundle install ./my-bundle --overwrite     # overwrite existing files

# From a git URL (clones --depth=1 to a temp dir, then installs)
aegis bundle info https://github.com/org/security-bundle
aegis bundle install https://github.com/org/security-bundle
```

Git URL install works with any HTTPS or SSH git URL. The clone is discarded after installation.

### Pinning a bundle's content hash (P7.6)

A bundle installs arbitrary tool/persona/command code with no other provenance check — a git-URL install has no signature verification, so a compromised or force-pushed upstream repo would otherwise install different code than what you reviewed, silently. `bundle info`/`bundle install` print a `sha256:`-prefixed content hash covering the manifest and every artifact file. Pin it once you've reviewed a bundle, and re-verify on every later install (e.g. in CI or a setup script):

```bash
aegis bundle info https://github.com/org/security-bundle
# ... content hash: sha256:9f2b...

aegis bundle install https://github.com/org/security-bundle --expect-sha256 9f2b...
# aborts with an error instead of installing if the repo now serves different content
```

This is trust-on-first-use pinning (like an SSH host key fingerprint), not a signature — it detects change after you've reviewed a bundle once, it doesn't establish who authored it.

**Scope:**
- `project` (default) — installs to `.aegis/commands/`, `.aegis/agents/`, `.aegis/skills/`
- `user` — installs to the user data directory

The installer never edits `config.yaml` — config-level pieces (MCP servers, permission rules) stay under your control.

---

## Discovery: What's Loaded

Use `aegis dry-run` to see everything Aegis has loaded before making any model call:

```bash
aegis dry-run
```

Shows:
- Resolved config
- Active persona
- Loaded memory entries and skills
- Available tools (built-in + MCP + plugins + custom agents)
- Repo map and knowledge base status

Also list tools from inside the TUI:
```
/commands     custom slash commands
/skills       saved skills
/memory       memory entries
/models       current model
```
