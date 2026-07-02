# Session Management

Sessions are the core persistence unit in Aegis. Every conversation is a session: a durable, SQLite-backed record that survives TUI restarts, daemon restarts, and machine reboots.

---

## What a Session Contains

| Field | Description |
|-------|-------------|
| `id` | UUID — used for `--resume`, export, and API calls |
| `title` | Auto-generated from the first user message |
| `persona` | Persona active when the session was created |
| `mode` | Current permission mode |
| `system` | System prompt (set by persona + memory + repo map) |
| `messages` | Full conversation history (user, assistant, tool calls, tool results) |
| `traces` | Per-turn observability records (tokens, cost, tools, timing) |
| `input_tokens` | Cumulative input token count |
| `output_tokens` | Cumulative output token count |
| `cost_usd` | Cumulative estimated USD spend |
| `created_at` | Creation timestamp |
| `updated_at` | Last-updated timestamp |
| `archived_at` | Set when archived; null when active |
| `background` | Whether the session runs detached from the TUI |

Sessions are stored in `~/.local/share/aegis/sessions.db` (SQLite).

---

## Creating and Resuming Sessions

**New session (default):**
```bash
aegis
aegis --persona security --mode build
```

**Resume by ID:**
```bash
aegis --resume abc12345
```

**Switch sessions inside TUI:**
Press `Ctrl+R` to open the interactive session picker. Sessions are listed newest-first with title, mode, persona, and last-updated time. Select one to replay its transcript.

---

## Listing Sessions

```bash
aegis sessions list              # list all active sessions
aegis sessions list --archived   # include archived sessions
```

Inside the TUI:
```
/session       show current session info
/session list  list all sessions
```

---

## Checkpoints and Rewind

Every user turn captures a **checkpoint** before the agent runs. A checkpoint contains:

1. The conversation length (message count) at the start of the turn — so the transcript can be truncated back
2. A copy-on-write snapshot of any file the agent modifies (captured the first time each file is touched during the turn, before the modification)

Files larger than 16 MiB are not snapshotted.

### Listing checkpoints

```
/rewind
```

Shows all checkpoints for the current session, newest first, with:
- Checkpoint number
- Your message (truncated)
- Number of files snapshotted

### Restoring a checkpoint

```
/rewind <n>                  restore checkpoint n (files + conversation)
/rewind <n> code             restore only files
/rewind <n> conversation     restore only transcript
/rewind <n> both             restore both (default)
```

**Scopes:**

| Scope | Effect |
|-------|--------|
| `code` | Revert files the turn modified; delete files the turn created. Transcript untouched. |
| `conversation` | Truncate the transcript back to before the turn. Files untouched. |
| `both` | Do both (default). |

After a `code` or `both` restore, file-staleness tracking is cleared so the agent re-reads any reverted file before touching it again.

### `git reset` variant

```
/rollback [n]
```

Like `/rewind n both`, but also runs `git reset --hard` to the git HEAD from before the turn. Use this when you want to undo both agent file changes and any `git_commit` calls the agent made during the turn.

---

## Archiving Sessions

Archive hides a session from normal listings while keeping all data:

```bash
aegis sessions archive <id>     # archive (soft-delete)
aegis sessions unarchive <id>   # restore
```

Inside TUI:
```
/archive      archive current session
/archive off  restore current session from archive
```

Archived sessions appear with `--archived`:
```bash
aegis sessions list --archived
```

---

## Deleting Sessions

```bash
aegis sessions delete <id>
```

Permanently removes the session, all messages, traces, and checkpoints. Irreversible.

---

## Auto-Pruning

Configure automatic cleanup of old sessions:

```yaml
cleanup:
  session_ttl_days: 30    # delete non-archived sessions older than 30 days
  interval_hours: 24      # check once per day
```

Run manually:
```bash
aegis sessions prune
```

Only non-archived sessions past the TTL are pruned. Archived sessions are always kept until explicitly deleted.

---

## Exporting Sessions

Export a session as a shareable, self-contained file:

```bash
aegis sessions export <id> --format html [--out file]
aegis sessions export <id> --format md
aegis sessions export <id> --format json
```

Inside the TUI:
```
/share         export as HTML
/share html    export as HTML
/share md      export as Markdown
/share json    export as JSON
```

**Formats:**

| Format | Use case |
|--------|---------|
| `html` (default) | Self-contained file with collapsible thinking/tool sections and inline images. Opens in any browser. |
| `md` | Markdown — good for pasting into GitHub issues, PRs, or documents |
| `json` | Raw session data — for tooling, re-import, or programmatic processing |

Tool results are truncated past ~8k characters to keep exported files manageable.

---

## Per-Turn Traces

Each turn records structured observability data:

```bash
aegis sessions trace <id>
```

Prints a per-turn table:

```
Turn  Model              In      Out   Cache    Cost    Tools                     Wall
───────────────────────────────────────────────────────────────────────────────────────
  1   claude-opus-4-8   4821    321    4000   $0.009  glob(2ms) read_file(5ms)   8.2s
  2   claude-opus-4-8   5210    892       0   $0.021  edit_file(1ms)             4.1s
  3   claude-opus-4-8   6104    234    5100   $0.004  (none)                     2.3s
───────────────────────────────────────────────────────────────────────────────────────
Total                  16135   1447    9100   $0.034                            14.6s
```

Useful for auditing run costs, profiling slow turns, and verifying cache behavior.

---

## Background Sessions

A session can be detached from the TUI and continue running in the background:

```
/detach on
```

The agent keeps working even if you close the TUI. Reconnect by resuming the session:
```bash
aegis --resume <id>
```

Check background session progress:
```bash
aegis bg events <id>
```

---

## HTTP API

Sessions are also manageable via the daemon's HTTP API (useful for automation):

```bash
# List sessions
curl -H "Authorization: Bearer <token>" http://127.0.0.1:4127/sessions

# Create session
curl -X POST -H "Authorization: Bearer <token>" \
  -d '{"persona":"security","mode":"plan"}' \
  http://127.0.0.1:4127/sessions

# Send a message (SSE stream)
curl -N -X POST -H "Authorization: Bearer <token>" \
  -d '{"content":"review this codebase for security issues"}' \
  http://127.0.0.1:4127/sessions/<id>/messages

# List checkpoints
curl -H "Authorization: Bearer <token>" \
  http://127.0.0.1:4127/sessions/<id>/checkpoints

# Restore a checkpoint
curl -X POST -H "Authorization: Bearer <token>" \
  -d '{"seq":3,"scope":"both"}' \
  http://127.0.0.1:4127/sessions/<id>/rewind
```

The bearer token is stored at `~/.local/share/aegis/auth`.

---

## Practical Tips

**Name your sessions** — The title is auto-set from the first message. Starting with a clear description (`"Security review of internal/api/ package"`) makes sessions easy to find in the picker.

**Use archive instead of delete** — Archive sessions you might want to revisit; only delete sessions whose data you're certain you don't need.

**Checkpoint before risky runs** — Checkpoints are created automatically, but knowing they exist lets you be bolder. If an agent run makes bad changes, `/rewind 1` undoes the last turn.

**Export for sharing** — `aegis sessions export <id> --format html` creates a self-contained HTML file that colleagues can open without running Aegis.

**Use `sessions trace` for cost auditing** — See exactly which turns were expensive, what models were used, and what the cache hit rate was.
