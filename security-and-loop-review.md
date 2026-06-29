# Aegis Security & Agent Loop Review

_Date: 2026-06-29_

---

## 1. Security Review

### 1.1 Critical / High

#### C1 — Silent auth bypass when `authToken` is empty (`server.go:1416`)

```go
if s.authToken == "" {
    next.ServeHTTP(w, r)
    return
}
```

`ListenAndServe` already refuses to start when the token is empty, but the middleware still contains the bypass. If the token were cleared after startup (zero-value struct copy, test helper, etc.) every request would be unauthenticated with no indication. The bypass is dead code in production but is a latent defense-in-depth hole.

**Fix:** remove the bypass branch; the middleware should always enforce the token.

---

#### C2 — Web UI served without security headers (`server/webui.go:16`)

`handleWebUI` sets `Content-Type` and `Cache-Control: no-store` only. The auth token is injected inline into the returned HTML (`__AEGIS_TOKEN__` substitution). Missing headers:

| Header | Risk without it |
|---|---|
| `Content-Security-Policy` | XSS in the UI can read the embedded token |
| `X-Frame-Options: DENY` | Clickjacking (even on loopback) |
| `X-Content-Type-Options: nosniff` | MIME-type sniffing |
| `Referrer-Policy: no-referrer` | Token leakage via `Referer` |

Because the daemon binds to loopback only and the origin middleware blocks DNS-rebinding, the practical risk is lower than for a public server — but a browser extension or compromised local process can still reach the daemon, and the UI page hands them the token for free.

**Fix:** add the four headers to `handleWebUI`.

---

#### C3 — API responses lack `X-Content-Type-Options` (`server.go:1313`)

`writeJSON` and `writeError` set `Content-Type: application/json` but not `X-Content-Type-Options: nosniff`. While this is lower risk for a local API, browsers will still MIME-sniff the response if the header is missing.

**Fix:** set `X-Content-Type-Options: nosniff` in `writeJSON`.

---

### 1.2 Medium

#### M1 — `egress_then_write` / `network_allowlist` bypass via `shell` tool (server.go:245)

The server already logs a `Warn` about this at startup. However, the warning text doesn't convey the severity clearly: the shell tool (PowerShell on Windows) can make arbitrary network calls (`Invoke-WebRequest`, `curl.exe`, `nc`, etc.) that bypass both the SSRF protections on `web_fetch` _and_ any `network_allowlist` you configure.

**Status:** warning exists; no code fix possible without blocking the shell tool outright. Recommendation: document that `network_allowlist` only applies to `web_fetch` / `web_search`, and rename or re-document the config field to clarify scope.

---

#### M2 — MCP `Auth` tokens in config files (`config.go:83`)

`MCPServerConfig.Auth` is a plaintext Bearer token stored in the project config file (`.aegis/config.yaml`). Config files are typically version-controlled.

**Recommendation:** accept the value from an environment variable instead, e.g. `AEGIS_MCP_<NAME>_AUTH`, with the config field as a fallback only.

---

#### M3 — Default permission mode is `build`, not `plan` (`config.go:147`)

In `build` mode, `write_file` / `edit_file` / `multi_edit_file` all execute without prompting. Only `shell` (CapExecute) requires approval. New users may not realise the agent can freely rewrite files in their workspace by default.

**Status:** intentional design; worth surfacing prominently in the README/docs.

---

### 1.3 Low / Informational

| ID | Location | Note |
|---|---|---|
| L1 | `web.go:191` | DuckDuckGo HTML scraping; will silently break if DDG changes their DOM. Recommend monitoring or adding a fallback. |
| L2 | `server.go:1412` | `/healthz` and `/ui/*` are unauthenticated. `/healthz` leaks the configured model name in its JSON response. Low risk on loopback. |
| L3 | `permission.go:111` | `Gate.Check` ignores the `input json.RawMessage` parameter (named `_`). Future contextual policies (e.g. blocking writes to sensitive paths) cannot be implemented without changing the interface. Consider passing input to Approver as well. |

---

### 1.4 What's Good

- **SSRF protection** (`web.go:102`): `ssrfSafeDialer` resolves DNS and rejects private IPs before connecting; redirect chains are also validated. Solid implementation.
- **Path traversal** (`sandbox/pathvalidator.go`): `ValidatePath` resolves symlinks to their real paths and checks against the workspace root. Handles both existing and not-yet-existing paths correctly.
- **Token generation** (`server.go:1393`): 32 bytes from `crypto/rand`, written with `0o600` permissions. Constant-time compare on every request.
- **DNS-rebinding protection** (`server.go:1437`): origin middleware rejects any `Origin` header that isn't localhost/loopback.
- **Session serialisation** (`server.go:1308`): per-session semaphore prevents concurrent mutations of the same conversation.
- **Orphaned tool-use repair** (`engine.go:180`): interrupted runs are healed on the next turn rather than permanently corrupting the conversation.
- **Loop detection** (`engine.go:254`): identical tool-call sequences abort after a configurable threshold.

---

## 2. Agent Loop Review

### 2.1 Loop Mechanics — Correct

The `engine.Run` loop (`engine.go:201`) correctly:

1. Calls the model.
2. If the model requests tool calls → executes them (with gate + hooks), appends results, and loops.
3. If no tool calls and `StopReason == StopMaxTokens` → injects a continuation prompt and loops.
4. If no tool calls and stop reason is normal → emits `KindDone` and returns.

Parallel tool execution is properly serialized for write/execute tools, with a semaphore capping concurrency at 8. Event emission is mutex-protected so streamed output is never interleaved.

---

### 2.2 Gap — Document/Output Tasks Rely on Explicit File Paths

**Problem:** `completingTasksBlock` in `persona.go:461` reads:

> "When the user asks you to write output to a specific file or path, call write_file with that path."

This only triggers when the user explicitly mentions a path (e.g., "save to `report.md`"). If the user says "review this codebase and produce a markdown document," the agent may return the review as a chat response and never call `write_file` — because no path was specified and the rule doesn't cover the implicit case.

**Fix:** extend the rule to say: _if the task is to produce a document, report, or structured output and no path is given, default to a sensible filename (e.g., `report.md`, `review.md`, `<topic>-<date>.md`) and call `write_file`._

---

### 2.3 Sub-agent Output (Informational)

Sub-agents launched via the `agent` tool (`server.go:395`) only propagate `KindText` events back to the parent. Tool results (including `write_file` confirmations) are discarded at the sub-agent boundary. Files _are_ written to disk, but the parent only sees the sub-agent's final text. This is acceptable behaviour; the parent can verify by calling `read_file` or `glob`.

---

## 3. Recommended Fixes (Priority Order)

| # | File | Change |
|---|---|---|
| 1 | `internal/server/webui.go` | Add CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy headers |
| 2 | `internal/server/server.go` | Remove empty-token bypass from `authMiddleware` |
| 3 | `internal/server/server.go` | Add `X-Content-Type-Options: nosniff` in `writeJSON` |
| 4 | `internal/persona/persona.go` | Extend `completingTasksBlock` to cover implicit document creation |
