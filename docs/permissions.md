# Permission System

Every tool call goes through the permission gate before executing. The gate determines whether to allow, deny, or ask about a call, and records every decision to an audit trail.

---

## Permission Modes

Three modes control the default posture:

| Mode | File Read | File Write | Shell/Execute | Network | When to use |
|------|-----------|------------|---------------|---------|-------------|
| **plan** | Allow | Deny | Deny | Allow | Safe exploration, read-only analysis |
| **build** | Allow | Allow | Ask | Allow | Default — file edits free, shell prompts |
| **auto** | Allow | Allow | Allow | Allow | Trusted sandboxes, CI/CD, unattended runs |

**Set at launch:**
```bash
aegis --mode plan
aegis --mode build   # default
aegis --mode auto
```

**Switch during a session:**
```
/mode plan
/mode build
/mode auto
```

Or press `Shift+Tab` in the TUI to cycle through modes.

---

## Text-Based Rules

Beyond the coarse mode, you can write fine-grained allow/deny rules in `config.yaml`. Rules are evaluated **before** the mode gate and can override it in both directions.

```yaml
permission:
  mode: build
  rules:
    - "allow bash(npm test*)"      # auto-approve without prompting
    - "allow bash(git status)"
    - "deny write(/etc/*)"         # block even in auto mode
    - "deny shell(rm -rf /*)"
```

### Rule Syntax

```
allow <tool>(<pattern>)
deny  <tool>(<pattern>)
```

**`<tool>`** — one of:
- A specific tool name: `shell`, `write_file`, `web_fetch`, `git_commit`, …
- A capability alias:
  - `bash` / `execute` — matches all execute-capability tools (shell, task_create, cron_create, latex_build)
  - `write` — matches all write-capability tools (write_file, edit_file, multi_edit, git_commit, remember, …)
  - `read` — matches all read-capability tools
  - `network` — matches all network-capability tools (web_fetch, web_search)
- `*` — matches any tool

**`<pattern>`** — a glob matched against the call's primary input:
- For shell/execute tools: the command string
- For file tools: the file path
- For network tools: the URL

`*` in a pattern spans path separators, so `/etc/*` matches everything under `/etc`.

### Precedence

1. If any `deny` rule matches → **block** (even in auto mode)
2. If any `allow` rule matches → **allow** (no prompt, even for shell in build mode)
3. Otherwise → mode gate applies normally

Malformed rules are logged and skipped at startup without aborting the daemon.

### Rule Examples

```yaml
rules:
  # Auto-approve specific safe commands
  - "allow bash(npm test*)"
  - "allow bash(npm run lint)"
  - "allow bash(go test ./...)"
  - "allow bash(go build ./...)"
  - "allow bash(git status)"
  - "allow bash(git diff*)"
  - "allow bash(git log*)"

  # Block dangerous commands
  - "deny shell(rm -rf *)"
  - "deny shell(*curl*)"               # block curl in shell (use web_fetch instead)
  - "deny shell(*wget*)"

  # Restrict writes to project directory
  - "deny write(/etc/*)"
  - "deny write(/usr/*)"
  - "deny write(~/.ssh/*)"

  # Block specific URLs
  - "deny network(*malicious.example.com*)"

  # Allow everything in a test directory, deny everywhere else
  - "allow write(tests/*)"
  - "deny write(*)"
```

---

## Approval Dialogs

In `build` mode, the agent pauses before shell/execute calls and shows an approval dialog:

```
┌─────────────────────────────────────────────────┐
│  ⚙ shell                                        │
│                                                 │
│  rm -rf dist/                                   │
│                                                 │
│  [y] approve  [n] deny  [a] approve all         │
└─────────────────────────────────────────────────┘
```

- **`[y]`** — approve this call
- **`[n]`** — deny this call (returns an error to the agent)
- **`[a]`** — approve all remaining calls in this run (equivalent to `auto` for the rest of the run)

The `aegis chat --yes` flag auto-approves all calls for non-interactive use.

---

## `auto_approve_exec`

Set `auto_approve_exec: true` in config to skip approval prompts for shell/execute calls in `build` mode, without switching to full `auto` mode (file writes still happen silently; shells don't prompt):

```yaml
permission:
  mode: build
  auto_approve_exec: true
```

---

## Audit Trail

Every tool call and permission decision is appended to an audit trail file:

**Location:** `~/.local/share/aegis/audit/<session-id>.jsonl`

Each line is a JSON object:
```json
{
  "ts": "2026-07-02T10:00:00Z",
  "tool": "shell",
  "capability": "execute",
  "input": {"command": "rm -rf dist/"},
  "decision": "allow",
  "rule": "allow bash(rm -rf dist/)",
  "session_id": "abc12345"
}
```

**Decision values:** `allow`, `deny`, `ask_approved`, `ask_denied`.

The audit trail is append-only and never rewritten. It persists even when sessions are deleted.

---

## Contextual Security Policies

Two additional security controls operate at the tool-capability layer:

### `egress_then_write`

When enabled, any write-capability tool call that occurs **after** a network-capability call in the same session requires explicit approval, regardless of mode.

This prevents the agent from fetching external content and then writing it to local files without your knowledge (a common pattern in prompt injection attacks).

```yaml
security:
  egress_then_write: true
```

**Example scenario:** The agent uses `web_fetch` to get an API response, then wants to use `write_file`. With this policy enabled, the write triggers an approval prompt even in `auto` mode.

### `network_allowlist`

Restrict outbound network-capability tool calls to specific domains:

```yaml
security:
  network_allowlist:
    - "api.github.com"
    - "registry.npmjs.org"
    - "pkg.go.dev"
```

An empty list means unrestricted (the default).

**Important scope note:** These policies apply to tool-capability network calls (`web_fetch`, `web_search`, MCP server connections). They do **not** restrict what the `shell` tool can do via `curl`, `wget`, etc. For enforced egress isolation, use the [container sandbox](security.md#sandboxed-execution) with `network: false`.

---

## Combining Policies

A real-world project config combining multiple layers:

```yaml
permission:
  mode: build
  auto_approve_exec: false
  rules:
    # Safe read-only commands
    - "allow bash(go test ./...)"
    - "allow bash(go build ./...)"
    - "allow bash(git status)"
    - "allow bash(git diff*)"
    - "allow bash(git log*)"
    - "allow bash(npm test)"
    - "allow bash(npm run lint)"
    
    # Never touch system directories
    - "deny write(/etc/*)"
    - "deny write(/usr/*)"
    - "deny write(/var/*)"
    - "deny write(~/.ssh/*)"
    - "deny write(~/.aws/*)"
    
    # Block destructive commands
    - "deny shell(rm -rf *)"
    - "deny shell(*--force*)"

security:
  egress_then_write: true     # require approval for writes after network calls
  network_allowlist:           # only allow API calls to known services
    - "api.github.com"
    - "pkg.go.dev"

cost:
  budget_usd: 5.0             # abort if spend exceeds $5

sandbox:
  backend: auto               # use a container if one is available
  network: false              # no network inside containers
```
