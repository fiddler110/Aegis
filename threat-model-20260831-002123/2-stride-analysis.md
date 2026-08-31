# STRIDE + Abuse Cases — Threat Analysis

> This analysis uses the standard **STRIDE** methodology (Spoofing, Tampering, Repudiation, Information Disclosure, Denial of Service, Elevation of Privilege) extended with **Abuse Cases** (business logic abuse, workflow manipulation, feature misuse). The "A" column in tables below represents Abuse — a supplementary category covering threats where legitimate features are misused for unintended purposes. This is distinct from Elevation of Privilege (E), which covers authorization bypass.

## Exploitability Tiers

Threats are classified into three exploitability tiers based on the prerequisites an attacker needs:

| Tier | Label | Prerequisites | Assignment Rule |
|------|-------|---------------|----------------|
| **Tier 1** | Direct Exposure | `None` | Exploitable by unauthenticated external attacker with NO prior access. The prerequisite field MUST say `None`. |
| **Tier 2** | Conditional Risk | Single prerequisite: `Authenticated User`, `Privileged User`, `Internal Network`, or single `{Boundary} Access` | Requires exactly ONE form of access. The prerequisite field has ONE item. |
| **Tier 3** | Defense-in-Depth | `Host/OS Access`, `Admin Credentials`, `{Component} Compromise`, `Physical Access`, or MULTIPLE prerequisites joined with `+` | Requires significant prior breach, infrastructure access, or multiple combined prerequisites. |

## Summary

| Component | Link | S | T | R | I | D | E | A | Total | T1 | T2 | T3 | Risk |
|-----------|------|---|---|---|---|---|---|---|-------|----|----|----|------|
| TUI | [Link](#tui) | 0 | 1 | 1 | 2 | 1 | 0 | 1 | 6 | 0 | 0 | 6 | Medium |
| Client | [Link](#client) | 1 | 1 | 0 | 2 | 0 | 0 | 1 | 5 | 0 | 0 | 5 | Medium |
| WebUI | [Link](#webui) | 1 | 1 | 0 | 2 | 1 | 1 | 1 | 7 | 0 | 7 | 0 | High |
| ACPAgent | [Link](#acpagent) | 1 | 0 | 1 | 1 | 0 | 1 | 1 | 5 | 0 | 0 | 5 | Medium |
| MCPServer | [Link](#mcpserver) | 1 | 0 | 1 | 1 | 1 | 2 | 1 | 7 | 0 | 0 | 7 | High |
| Server | [Link](#server) | 1 | 2 | 2 | 2 | 2 | 1 | 1 | 11 | 0 | 11 | 0 | High |
| Engine | [Link](#engine) | 1 | 2 | 1 | 2 | 2 | 2 | 2 | 12 | 0 | 0 | 12 | High |
| PermissionGate | [Link](#permissiongate) | 0 | 1 | 1 | 0 | 0 | 3 | 1 | 6 | 0 | 0 | 6 | High |
| MCPClient | [Link](#mcpclient) | 1 | 1 | 0 | 2 | 1 | 1 | 1 | 7 | 0 | 0 | 7 | Medium |
| CronScheduler | [Link](#cronscheduler) | 0 | 1 | 1 | 1 | 1 | 2 | 1 | 7 | 0 | 0 | 7 | High |
| SandboxBackend | [Link](#sandboxbackend) | 0 | 2 | 1 | 2 | 2 | 3 | 1 | 11 | 0 | 0 | 11 | High |
| MultiScanner | [Link](#multiscanner) | 1 | 1 | 0 | 1 | 1 | 1 | 1 | 6 | 0 | 0 | 6 | Medium |
| SessionStore | [Link](#sessionstore) | 0 | 1 | 1 | 2 | 1 | 0 | 0 | 5 | 0 | 0 | 5 | Medium |
| CheckpointStore | [Link](#checkpointstore) | 0 | 1 | 0 | 1 | 1 | 0 | 1 | 4 | 0 | 0 | 4 | Medium |
| WorkspaceTrustStore | [Link](#workspacetruststore) | 0 | 1 | 1 | 0 | 0 | 2 | 0 | 4 | 0 | 0 | 4 | Medium |
| DaemonTokenFile | [Link](#daemontokenfile) | 1 | 1 | 0 | 2 | 1 | 0 | 0 | 5 | 0 | 0 | 5 | Medium |
| AnthropicAPI | [Link](#anthropicapi) | 1 | 1 | 1 | 2 | 1 | 0 | 1 | 7 | 0 | 7 | 0 | High |
| OllamaServer | [Link](#ollamaserver) | 1 | 1 | 0 | 1 | 1 | 1 | 0 | 5 | 0 | 5 | 0 | Medium |
| ExternalMCPServer | [Link](#externalmcpserver) | 1 | 2 | 0 | 1 | 1 | 1 | 1 | 7 | 0 | 7 | 0 | High |
| ExternalWebService | [Link](#externalwebservice) | 1 | 1 | 0 | 1 | 1 | 1 | 1 | 6 | 0 | 6 | 0 | High |
| ContainerRuntime | [Link](#containerruntime) | 0 | 1 | 0 | 1 | 1 | 2 | 0 | 5 | 0 | 5 | 0 | Medium |
| **Totals** | | **13** | **23** | **12** | **29** | **20** | **24** | **17** | **138** | **0** | **48** | **90** | |

---

## TUI

**Trust Boundary:** Client
**Role:** Bubbletea terminal UI; the default front-end and the surface on which tool-call approvals are granted.
**Data Flows:** DF01, DF03
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T01.T | Tampering | Repository or model content rendered in the terminal carries ANSI/OSC escape sequences that rewrite the terminal title, inject keystrokes, or hide text from the operator reviewing an approval. | Host/OS Access | DF01 | `internal/termsafe` (`StripControlSeqs`, `StripDangerousSeqs`) strips control sequences before rendering. | Mitigated |
| T01.R | Repudiation | An approval decision shown and accepted in the TUI leaves no independent record on the client side; the only trace is whatever the daemon persists. | Host/OS Access | DF01, DF03 | Enable the `internal/hooks` audit hook so `PolicyDecision` records are written for every gate outcome. | Open |
| T01.I1 | Information Disclosure | Model output rendered to the terminal includes file contents the agent read, which may contain credentials; no redaction is applied on the display path. | Host/OS Access | DF01 | Apply `internal/redact` to rendered tool results, not only to shared transcripts. | Open |
| T01.I2 | Information Disclosure | Terminal scrollback and any terminal-emulator logging retain secrets that appeared in agent output after the session ends. | Host/OS Access | DF01 | Document the scrollback exposure; offer a "no secrets to screen" posture that redacts by default. | Open |
| T01.D | Denial of Service | A pathological model response (a single multi-megabyte line, or dense wide-glyph content) stalls the render loop and makes the UI unresponsive. | Host/OS Access | DF01 | Bound per-render line length and total buffered output in the TUI view layer. | Open |
| T01.A | Abuse | Long parallel tool rounds produce repeated approval prompts, training the operator to approve without reading — approval fatigue that converts a deliberate gate into a rubber stamp. | Host/OS Access | DF01 | Batch related approvals with a single reviewable summary; surface the resolved argv rather than the raw command string. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The TUI has no identity of its own and authenticates nothing; it is a rendering surface over the `Client` library. |
| Elevation of Privilege | The TUI holds no privilege the invoking user does not already have; every privileged action is decided in the daemon. |

---

## Client

**Trust Boundary:** Client
**Role:** HTTP client library that loads `daemon.token`, pins the daemon's self-signed certificate, and carries every API call.
**Data Flows:** DF03, DF04, DF06, DF07, DF25
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T02.S | Spoofing | The client pins whatever certificate is present at `<DataDir>/daemon.crt`; a same-user process that writes that file before the client first reads it makes the client trust an impersonating listener. | Host/OS Access | DF04, DF25 | Apply `fsguard.RestrictToOwner` to `daemon.crt` as well as `daemon.token`, and record a pin fingerprint the client can compare. | Open |
| T02.T | Tampering | Deleting `daemon.crt`/`daemon.key` causes silent regeneration on next daemon start, changing the pin with no operator-visible signal. | Host/OS Access | DF25 | Log loudly on regeneration and require an explicit acknowledgement when a previously-seen pin changes. | Open |
| T02.I1 | Information Disclosure | The bearer token is held in process memory and attached to every request; a crash dump or debugger attach on the client process exposes it. | Host/OS Access | DF04 | Zero the token buffer after use where practical; document that a same-user debugger defeats this. | Open |
| T02.I2 | Information Disclosure | Client-side request/response logging would persist full conversation content including any secrets present in tool results. | Host/OS Access | DF04 | `internal/redact` is applied to persisted/shared output (`security.redact_secrets: true` by default). | Mitigated |
| T02.A | Abuse | Any local process running as the operator can read `daemon.token` and use this same client library to drive full agent turns — the client is not a privilege boundary. | Host/OS Access | DF25, DF04 | Document explicitly that same-user local processes are inside the trust boundary; consider per-client tokens with scopes. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | The client performs no independently-attributable action; every request it makes is logged and attributed at the daemon. |
| Denial of Service | The client is a library in the caller's process — exhausting it denies service only to its own caller. |
| Elevation of Privilege | The client runs with exactly the invoking user's privileges and grants none. |

---

## WebUI

**Trust Boundary:** Client
**Role:** Browser single-page app served from the daemon's embedded `dist/`, obtaining the daemon token through the page-token exchange.
**Data Flows:** DF02, DF05
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T03.S | Spoofing | Any local process that can reach the loopback port calls `GET /ui`, reads the page token out of the response body and the CSRF nonce out of the `Set-Cookie` header, and redeems both at `/auth/exchange` for the real daemon token — the double-submit binding stops hostile *web pages*, not raw HTTP clients. | Local Process Access | DF05 | Documented as accepted residual risk in the code (`uiCSRFCookieName` doc comment). Bind the exchange to an operator-visible confirmation, or require the real token for the first `/ui` load when one is available. | Open |
| T03.T | Tampering | The SPA is served from a committed `dist/` embedded at build time; drift between `frontend/src` and `dist/` is caught only by the web-UI drift check in `ci.yml`, whose push/PR triggers are commented out. | Local Process Access | DF05 | Re-enable the CI triggers, or add the drift check to a workflow that still runs on push. | Open |
| T03.I1 | Information Disclosure | The CSRF cookie is minted without `Secure` when the request did not arrive over TLS, so on a plaintext deployment it can travel over a non-TLS hop. | Local Process Access | DF05 | `server.tls.enabled` defaults to `true`, so `r.TLS != nil` and `Secure` is set on the default deployment; `trust_proxy_headers` covers the reverse-proxy case. | Mitigated |
| T03.I2 | Information Disclosure | The real daemon token is delivered into browser JavaScript memory, where any script-injection defect in the SPA (or a malicious browser extension with host access) can read and exfiltrate it. | Local Process Access | DF02, DF05 | Keep the daemon token out of JS entirely by proxying calls through a cookie-authenticated session; the CSP (`default-src 'self'`) reduces but does not remove the exposure. | Open |
| T03.D | Denial of Service | `GET /ui` is exempt from `authMiddleware` and mints a page token per load; once 1024 unexpired tokens are outstanding, `mintPageToken` refuses and the operator's own UI will not load. | Local Process Access | DF05 | The cap is a deliberate memory bound (refuse rather than evict). Add per-source-address minting throttling so a flood cannot deny the operator's own load. | Open |
| T03.E | Elevation of Privilege | A hostile page frames or embeds `/ui` to drive privileged actions with the operator's session (clickjacking / cross-origin reads). | Local Process Access | DF02 | `X-Frame-Options: DENY`, CSP `frame-ancestors 'none'`, and `originMiddleware` rejecting non-loopback `Origin` headers. | Mitigated |
| T03.A | Abuse | An operator exposing the UI through an SSH tunnel or reverse proxy meets a self-signed-certificate warning and is conditioned to click through it, defeating the pin. | Local Process Access | DF02 | The CLI calls the warning out explicitly when TLS is on; document a supported operator-supplied `cert_file`/`key_file` path for tunnelled access. | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | The SPA takes no action of its own; every state change it triggers is an authenticated daemon API call recorded at the daemon. |

---

## ACPAgent

**Trust Boundary:** Client
**Role:** ACP JSON-RPC agent driven by an editor (Zed, Neovim) over stdio.
**Data Flows:** DF06
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T04.S | Spoofing | The peer on the other end of stdio is whatever process the editor launched; `handleAuthenticate` verifies a shared token but cannot distinguish the editor from anything else that inherited the pipe. | Host/OS Access | DF06 | Document the stdio trust assumption; the token check bounds accidental, not deliberate, misuse by a same-user process. | Open |
| T04.R | Repudiation | Permission responses arrive as `session/request_permission` replies from the editor with no evidence a human saw the prompt; an editor plugin can auto-answer. | Host/OS Access | DF06 | Record the approval channel (`acp`) alongside the decision so a transcript distinguishes editor-answered from human-answered approvals. | Open |
| T04.I | Information Disclosure | Prompt content, file contents and tool results traverse stdio in cleartext to the editor process. | Host/OS Access | DF06 | Same-host, same-user pipe; no network exposure and no additional confidentiality boundary to defend. | Mitigated |
| T04.E | Elevation of Privilege | The permission mode for an ACP session is chosen by the client at `session/new`, so an editor plugin can request a more permissive posture than the operator's interactive default. | Host/OS Access | DF06 | Clamp client-requested modes to a configured ceiling, as `mcp_server.default_mode` now does for the MCP path. | Open |
| T04.A | Abuse | An editor plugin can drive continuous unattended turns — legitimate functionality used to run the agent without the operator's attention. | Host/OS Access | DF06 | Surface an editor-driven-run indicator and apply the same run budgets used for interactive sessions. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Tampering | The agent stores no state of its own; every mutation it forwards is validated and persisted by the daemon. |
| Denial of Service | It serves exactly one stdio peer, which is the process that launched it; denying it denies only its own launcher. |

---

## MCPServer

**Trust Boundary:** Client
**Role:** `aegis mcp-serve` — exposes Aegis itself as an MCP server over stdio so another agent or editor can drive it.
**Data Flows:** DF07
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T05.S | Spoofing | Anything able to write to this subprocess's stdin drives full agent turns; the `authenticate` method is the only barrier and the pipe is inherited from whoever launched the process. | Host/OS Access | DF07 | Documented in the package (`internal/mcpserver/server.go:84`). Treat launch of `mcp-serve` as equivalent to granting agent access. | Open |
| T05.R | Repudiation | A turn injected into a session the MCP client did not create is recorded indistinguishably from a turn the operator typed in the TUI. | Host/OS Access | DF07 | Record message origin (`mcp`, `acp`, `tui`, `cron`) on every persisted turn. | Open |
| T05.I | Information Disclosure | `aegis_list_sessions` proxies `Backend.ListSessions`, which is the daemon's `store.List` — every session on the daemon, including titles and working directories of sessions created interactively. | Host/OS Access | DF07 | Filter the listing to sessions this server instance created, or to a recorded session origin. | Open |
| T05.D | Denial of Service | An MCP client can start many concurrent runs and consume the daemon's global run slots, starving the operator's interactive session. | Host/OS Access | DF07 | `server.max_concurrent_runs` defaults to 10 and `server.max_run_duration_sec` to 1800, both explicitly sized to bound a lower-trust caller like `mcp-serve`. | Mitigated |
| T05.E1 | Elevation of Privilege | `callPrompt` accepts any `session_id` verbatim, so an authenticated MCP client can post a turn into an interactive `auto`-mode session and inherit that session's mode, persona, workdir and `additional_roots` — a workspace the MCP path could never have created for itself. | Host/OS Access | DF07 | Filed as **P80.1**; needs a recorded session origin filtered server-side. | Open |
| T05.E2 | Elevation of Privilege | The `mcp_server.default_mode` clamp binds only sessions this server creates; a borrowed session carries whatever mode it already had, so the clamp is bypassed by reuse. | Host/OS Access | DF07 | Same fix as T05.E1 — the clamp is only meaningful once session ownership is enforced. | Open |
| T05.A | Abuse | `mcp_server.auto_approve_tools` can be widened to auto-approve execute-capable tools, turning the MCP surface into unattended command execution. | Host/OS Access | DF07 | `mcp_server.auto_approve` defaults to `false`, and auto-approval is now per-tool rather than one undiscriminated yes. | Mitigated |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Tampering | The server holds no persistent state; it proxies to daemon endpoints that validate and persist. |

---

## Server

**Trust Boundary:** Daemon
**Role:** The daemon's HTTP API — authentication, session lifecycle, configuration endpoints, engine construction and SSE event streaming.
**Data Flows:** DF04, DF05, DF08, DF15, DF16, DF17, DF18, DF19, DF23
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T06.S | Spoofing | Authentication is possession of a single long-lived bearer token on disk; every local process running as the operator is indistinguishable from the operator's own client. | Local Process Access | DF04 | Constant-time compare and lockout are in place; add token rotation and per-client scoped credentials. | Open |
| T06.T1 | Tampering | `PATCH /config/sandbox`, `/config/security`, `/config/skills` and `/config/cost` mutate security-relevant runtime settings over the API with nothing beyond the bearer token — including the sandbox backend that confines command execution. | Local Process Access | DF04 | Require an explicit interactive confirmation (or a separate privileged token) for endpoints that weaken isolation. | Open |
| T06.T2 | Tampering | `server.session_workdir_allowlist` is documented as ignored on the default loopback bind, so any token holder can create a session rooted at any directory the daemon can read. | Local Process Access | DF04 | Apply the allowlist unconditionally, or gate non-default workdirs behind the workspace-trust prompt. | Open |
| T06.R1 | Repudiation | Successful privileged operations — config PATCHes, session deletion, prune, harden — produce no dedicated audit record; only failed authentication is logged, and only one attempt in five. | Local Process Access | DF04 | Emit a structured audit event for every state-changing endpoint. | Open |
| T06.R2 | Repudiation | The `internal/hooks` audit sink is opt-in, so a default deployment retains no tamper-evident record of tool executions or policy decisions. | Local Process Access | DF04, DF08 | Enable a default-on audit log under the data directory with owner-only permissions. | Open |
| T06.I1 | Information Disclosure | `/healthz` is exempt from authentication, letting any local process confirm the daemon's presence and readiness without credentials. | Local Process Access | DF04 | Minimal by design; keep the response free of version, workspace or session detail. | Open |
| T06.I2 | Information Disclosure | SSE event streams carry full conversation content, tool inputs and tool results to whichever client holds the token. | Local Process Access | DF05 | `server.tls.enabled: true` by default encrypts the loopback hop against a local packet-capture adversary. | Mitigated |
| T06.D1 | Denial of Service | The unauthenticated `/ui` route mints page-token entries; at `maxPageTokens` the daemon refuses to mint and the web UI stops loading for everyone. | Local Process Access | DF05 | Refusing rather than evicting is deliberate; add per-address throttling ahead of the cap. | Open |
| T06.D2 | Denial of Service | Repeated invalid authentication engages a process-wide lockout window that could wedge the operator's own client out of its own daemon. | Local Process Access | DF04 | The token is checked *before* the lockout window and a valid token is served throughout it, so a local guesser cannot lock out the legitimate holder. | Mitigated |
| T06.E | Elevation of Privilege | Setting `server.allow_remote: true` converts a loopback service into a network service whose entire access control is one static bearer token with no TLS client authentication and no per-user identity. | Local Process Access | DF04 | The bind is refused without the flag and a startup WARN is emitted; document that remote exposure requires an external authenticating proxy. | Open |
| T06.A | Abuse | Any token holder can create a session in `auto` mode with `auto_approve_exec`, then drive unattended host command execution through a legitimate API. | Local Process Access | DF04 | Clamp API-requested modes to a configured ceiling the same way the MCP path now does. | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

*All seven STRIDE-A categories produced at least one concrete threat for this component.*

---

## Engine

**Trust Boundary:** Daemon
**Role:** The agent loop — model turns, parallel tool rounds, compaction, budgets, loop detection.
**Data Flows:** DF08, DF09, DF10, DF11, DF12, DF13, DF14, DF20
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T07.S | Spoofing | The prose tool-call shim (`internal/provider/prosetoolcall.go`, `internal/toolshim`) parses tool calls out of free-form model text, so content the model merely *quotes* — from a fetched page or an MCP result — can be promoted into a real tool call. | ExternalWebService Compromise | DF08, DF14 | Keep the shim off by default (`provider.tool_call_shim: off`) and never parse tool calls out of content that arrived inside an untrusted-content wrapper. | Open |
| T07.T1 | Tampering | Indirect prompt injection: instructions embedded in fetched web content, MCP results or repository files steer the model into issuing tool calls the operator never asked for. | ExternalWebService Compromise | DF11, DF14 | `internal/trust` wraps such content and scans it heuristically, but the wrapper is advisory to the model — it constrains nothing mechanically. | Open |
| T07.T2 | Tampering | In a parallel tool round, read/network calls take no lock and are deliberately not held off by a concurrent write; only same-`path` calls are ordered, and the dependency graph keys on the literal `"path"` input field, so a `shell` call and a `read_file` are never ordered against each other. | Host/OS Access | DF08, DF10 | Documented design (P8.6). Extend the dependency graph to cover shell commands whose argv contains a path also used by a concurrent write. | Open |
| T07.R | Repudiation | A tool call interrupted mid-flight could be reported to the model as "not run" when it had in fact started, losing the record of a side effect. | Host/OS Access | DF08 | `repairOrphanedToolUses` reports a started call as *possibly* completed, never as not run (P65.1); `Engine.startedTools` tracks the set. | Mitigated |
| T07.I1 | Information Disclosure | Every file the agent reads, plus the repo map and tool results, is placed in a provider request; the redaction pass protects shared and persisted transcripts, not the outbound provider payload. | Host/OS Access | DF12, DF13 | Apply `internal/redact` (or an explicit allowlist prompt) to provider requests when the provider is a non-loopback cloud endpoint. | Open |
| T07.I2 | Information Disclosure | Truncated tool-result remainders spill to `<workspace>/.aegis/spill/` as plaintext files that outlive the turn and are reachable by `read_file`. | Host/OS Access | DF08 | Write spill files with owner-only permissions and reap them at session end. | Open |
| T07.D1 | Denial of Service | The primary spend and length bounds — `cost.budget_usd`, `cost.max_tokens_per_run`, `cost.session_cap_usd`, `cost.daily_cap_usd` — all default to `0`, meaning unlimited. | Host/OS Access | DF12 | Ship non-zero defaults, or refuse to start a cloud-provider session with no bound set. | Open |
| T07.D2 | Denial of Service | A long-running tool call stalls a turn indefinitely, holding a run slot and the model server. | Host/OS Access | DF08, DF10 | `cost.max_turn_stall` defaults to 900s and is the one bound that covers tool execution; `TestToolTimeoutsStayUnderTheStallBound` enumerates per-call timeouts. | Mitigated |
| T07.E1 | Elevation of Privilege | A per-call capability computed against the wrong root would classify a call as a read when it executes elsewhere, widening the effective capability across the approval round-trip. | Host/OS Access | DF09 | `tool.CapabilityOverrider` takes a context and classifies against `effectiveRoot`; `tool.WithCapabilityMemo` makes the verdict single-valued for the gate, scheduler and bookkeeping. | Mitigated |
| T07.E2 | Elevation of Privilege | Handing a sub-agent, debate role or session the parent `Registry` rather than a clone would let session-scoped tools escape their session. | Host/OS Access | DF08 | `Registry.Clone()` overlays exposure decisions and routes clone-local registration to an overlay; both directions are pinned by tests. | Mitigated |
| T07.A1 | Abuse | Loop-detection exemptions (`PollExempter`, `SignatureTransparent`) hide calls or arguments from the repetition breaker, which a model could exploit to loop indefinitely. | Host/OS Access | DF08 | Tests keep both exemption sets narrow and disjoint, and `SignatureTransparent` is barred from covering model-chosen search queries. | Mitigated |
| T07.A2 | Abuse | An injected instruction or a looping model drives sustained metered spend against a cloud provider using entirely legitimate functionality. | Host/OS Access | DF12 | Depends on T07.D1 — without a default budget there is no ceiling on this abuse. | Open |

#### Categories Not Applicable

*All seven STRIDE-A categories produced at least one concrete threat for this component.*

---

## PermissionGate

**Trust Boundary:** Daemon
**Role:** Capability classification plus mode, rule, contextual, persona-tool and scope layers consulted before every tool call.
**Data Flows:** DF09
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T08.T | Tampering | `classifyShellCommand` is roughly 1,129 lines of hand-written argument parsing across 40+ commands and three shell dialects; any parsing defect makes a mutating or escaping command classify as a read. Two such defects (an unexpanded `~`, an unconfined `argv[0]`) have already shipped and been fixed. | Host/OS Access | DF09 | `FuzzClassifyShellCommand` states the invariant (a `CapRead` verdict implies nothing outside root is touched and no binary from inside root is executed); keep the fuzz corpus growing with every fixed case. | Open |
| T08.R | Repudiation | Gate decisions are surfaced through `Audit.PolicyDecision`, which only records anything when the audit hook is configured — off in a default deployment. | Host/OS Access | DF09 | Make the policy-decision log default-on. | Open |
| T08.E1 | Elevation of Privilege | `Gate.Check` consults `tool.EffectiveCapability` *before* `Policy.Decide`, so a call downgraded by the classifier is allowed silently in every mode — including plan mode, whose documented guarantee is that no commands run at all. | Host/OS Access | DF09 | `permission.plan_mode_shell_reads: false` makes the guarantee unconditional; it is not the default. | Open |
| T08.E2 | Elevation of Privilege | A bare `permission.New` at an `engine.New` call site yields the mode gate alone — no rules, contextual policy, persona-tool or scope layer (the P66.13 bypass class). | Host/OS Access | DF09 | `internal/enginecfg.BuildGate` is the single constructor and `TestEveryEngineCallSiteDecidesItsGate` fails any new `engine.New` that neither uses it nor justifies its absence. | Mitigated |
| T08.E3 | Elevation of Privilege | A persona's `tools:` list is advisory — it prompts and warns but never enforces — so a persona documented as read-only can still issue write and execute calls. | Host/OS Access | DF09 | Documented in `docs/personas.md`. Offer an enforcing mode for personas used as a containment boundary. | Open |
| T08.A | Abuse | `permission.plan_mode_shell_reads` defaults to `true`, trading plan mode's hard guarantee for the ergonomics of `shell("git log")` — precisely the posture an operator selects when reviewing an untrusted repository. | Host/OS Access | DF09 | Default the flag to `false` for workspaces that have not been granted trust. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The gate has no identity and authenticates nothing; it evaluates a call already attributed by the engine. |
| Information Disclosure | The gate reads tool inputs it is asked to classify and emits only allow/ask/deny plus a reason string; it stores and transmits nothing. |
| Denial of Service | A gate failure denies a tool call, which is the fail-closed outcome, not a loss of service. |

---

## MCPClient

**Trust Boundary:** Daemon
**Role:** Outbound MCP client for stdio subprocess and HTTP/SSE servers, exposing their tools into the session registry.
**Data Flows:** DF11, DF21
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T09.S | Spoofing | A stdio MCP server's identity is the configured command path; a replaced or shimmed binary on `PATH` is indistinguishable from the intended server. | Host/OS Access | DF21 | Resolve configured server commands to absolute paths and record a binary digest at first use. | Open |
| T09.T | Tampering | A server can replace a tool's schema mid-session via `tools/list_changed`, changing what an already-approved tool name does. | Host/OS Access | DF21 | Registry clones share one `toolTable` so a parent re-registration reaches existing clones, and stale schema caches are discarded when the parent version moves. | Mitigated |
| T09.I1 | Information Disclosure | Tool arguments constructed by the model can carry workspace file contents or credentials to a third-party MCP server. | Host/OS Access | DF21 | `warnOutboundSecrets` logs a warning but does not block; add a redaction or refusal path for arguments matching `internal/redact` classes. | Open |
| T09.I2 | Information Disclosure | MCP server credentials live in `.aegis/.env`, which is a project-controlled file. | Host/OS Access | DF21 | `.aegis/.env` is read only in a trusted workspace, may not set `AEGIS_*` keys, and is ACL-restricted on Windows via `fsguard`. | Mitigated |
| T09.D | Denial of Service | A hanging or slow MCP server holds a tool-call slot and stalls the turn. | Host/OS Access | DF21 | Per-call timeouts are enumerated by `TestToolTimeoutsStayUnderTheStallBound` and kept under the 900s turn-stall bound. | Mitigated |
| T09.E | Elevation of Privilege | A tool newly advertised by a server after the session started becomes callable without any fresh operator approval of that server's expanded surface. | Host/OS Access | DF21 | Require re-approval when a server's advertised tool set grows, not just when its schemas change. | Open |
| T09.A | Abuse | A server controls its own tool descriptions, which are injected into the model's context and can be written to induce the model to call that tool with sensitive arguments. | Host/OS Access | DF21 | Wrap MCP-supplied tool descriptions in the same untrusted-content marker already applied to MCP results. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | Every MCP tool call is a tool call in the engine's own record; the client adds no separately-attributable action. |

---

## CronScheduler

**Trust Boundary:** Daemon
**Role:** Background job scheduler that starts unattended agent runs on a schedule.
**Data Flows:** DF19, DF20
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T10.T | Tampering | A stored job's prompt and configuration are executed later with no operator present; a job row modified in the cron SQLite store changes what runs. | Host/OS Access | DF19, DF20 | Restrict the cron database to owner-only, and re-display a job's full definition before each first run after modification. | Open |
| T10.R | Repudiation | Output and side effects of an unattended run are attributed to no human, and are recorded the same way an interactive turn is. | Host/OS Access | DF20 | Stamp a `cron` origin on every persisted turn and every audit record produced by a scheduled run. | Open |
| T10.I | Information Disclosure | A scheduled run reads workspace files and sends them to the configured provider with nobody watching, so an exfiltration path opened by injection runs unobserved. | Host/OS Access | DF20 | Require an explicit per-job acknowledgement when the job's provider is a non-loopback cloud endpoint. | Open |
| T10.D | Denial of Service | Overlapping schedules start concurrent runs that consume the daemon's global run slots. | Host/OS Access | DF19 | `server.max_concurrent_runs` (default 10) and `server.max_run_duration_sec` (default 1800) bound both the count and the duration. | Mitigated |
| T10.E1 | Elevation of Privilege | A job created in `auto` mode auto-approves execute-capable tool calls on every future firing, indefinitely, with no operator in the loop. | Host/OS Access | DF20 | Refuse `auto` mode for scheduled jobs unless the sandbox backend is a real isolation backend, mirroring `allow_unsandboxed_auto_exec`. | Open |
| T10.E2 | Elevation of Privilege | A job whose working directory differs from the daemon's workspace would be classified against the wrong root, mis-scoping every capability decision. | Host/OS Access | DF20 | `tool.CapabilityOverrider` classifies against `effectiveRoot`; cron jobs were the case that motivated it. | Mitigated |
| T10.A | Abuse | Cron is a persistence mechanism: an attacker who obtains the bearer token once can register a recurring job that keeps running after the token is rotated. | Host/OS Access | DF19 | Show registered jobs prominently at daemon start and require re-confirmation of jobs created outside an interactive session. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The scheduler is an in-process goroutine with no identity to assume and no peer to authenticate. |

---

## SandboxBackend

**Trust Boundary:** Sandbox
**Role:** Executes model-requested shell commands under a container, an OS sandbox profile, WSL, or — as the documented fallback — directly on the host.
**Data Flows:** DF10, DF22
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T11.T1 | Tampering | The `local` backend runs the model's command directly on the host with the operator's full privileges and no filesystem confinement beyond the tool-level path checks. | Host/OS Access | DF10 | Reached only when neither a container runtime nor an OS sandbox is available; make that state a hard failure under a `sandbox.strict` default rather than a WARN. | Open |
| T11.T2 | Tampering | The container backend bind-mounts the workspace read-write, so a command inside the sandbox can rewrite any workspace file including `.aegis/config.yaml` and project skills. | Host/OS Access | DF10, DF22 | Mount read-only for read-classified commands, and route writes through the checkpointed tool path. | Open |
| T11.R | Repudiation | Executed commands are recorded only through the opt-in audit hook, so a default deployment has no durable record of what the agent ran. | Host/OS Access | DF10 | Default-on command audit log under the data directory. | Open |
| T11.I1 | Information Disclosure | The spawned process inherits the daemon's environment minus a denylist (`DefaultStripEnv` plus configured names), so any secret-bearing variable not on the list is visible to the command. | Host/OS Access | DF10 | Invert to an allowlist: pass only the variables a command legitimately needs. | Open |
| T11.I2 | Information Disclosure | The workspace mount exposes everything under the root to the command, including `.aegis/.env` and any credential files the developer keeps in-tree. | Host/OS Access | DF22 | Exclude `.aegis/.env` and configured secret paths from the mount. | Open |
| T11.D1 | Denial of Service | A runaway command exhausts host memory, CPU or process table, starving the model server the agent depends on. | Host/OS Access | DF22 | `sandbox.limits` defaults to 4G memory, 2 CPUs and 1024 PIDs per container. | Mitigated |
| T11.D2 | Denial of Service | The `local` backend applies none of those limits — a fork bomb or memory hog runs unbounded on the host. | Host/OS Access | DF10 | Apply OS-level resource limits (job objects on Windows, rlimits/cgroups on POSIX) to locally-executed commands. | Open |
| T11.E1 | Elevation of Privilege | `SelectSandbox` cascades container → OS → `local` and emits only a startup WARN, so a Windows host with no container runtime silently executes every agent command unconfined. | Host/OS Access | DF10 | `sandbox.strict` makes the fallback fatal but is not the default; make strict the default and require an explicit opt-out. | Open |
| T11.E2 | Elevation of Privilege | `auto_approve_exec` combined with the unsandboxed local backend is unattended remote code execution on the host. | Host/OS Access | DF10 | The daemon refuses to start on that combination unless `permission.allow_unsandboxed_auto_exec` is set explicitly. | Mitigated |
| T11.E3 | Elevation of Privilege | The persistent per-workspace container keeps state between commands for the session TTL, so a command that plants a shim or modifies `PATH` inside the container affects every later command in the session. | Host/OS Access | DF22 | Persistence is on by default (`sandbox.persistent: true`); document the retained-state model and offer a per-command reset. | Open |
| T11.A | Abuse | With `sandbox.network` enabled, the shell tool becomes a general-purpose network client that bypasses the SSRF blocklist enforced on `web_fetch`. | Host/OS Access | DF22 | `sandbox.network` defaults to `false`; when enabled, apply an egress policy to the container rather than leaving it unrestricted. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The backend authenticates nothing; it executes a command already authorized by the gate. |

---

## MultiScanner

**Trust Boundary:** Sandbox
**Role:** Runs containerized security scanner images against the workspace and aggregates their findings.
**Data Flows:** DF23, DF24
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T12.S | Spoofing | Scanner image identity is asserted by a mutable tag rather than a digest, so a compromised registry or a local image rebuild substitutes a different scanner. | Host/OS Access | DF24 | `aegis security verify-image` exists; make verification a precondition of a scan run rather than a separate command. | Open |
| T12.T | Tampering | A scanner that fails partway reports a confident but wrong finding count, which an operator would read as "clean". | Host/OS Access | DF23 | gosec is the one two-phase tool and a failed warm phase aborts the scan rather than reporting a count. | Mitigated |
| T12.I | Information Disclosure | Scan reports embed source excerpts and dependency inventories and are written into the workspace where they may be committed. | Host/OS Access | DF23 | `internal/security/redact.go` scrubs findings; add the report path to a generated `.gitignore` entry. | Open |
| T12.D | Denial of Service | A rogue or looping scanner floods stdout and exhausts daemon heap. | Host/OS Access | DF23 | `execbound.go` caps retained scanner output at 64 MiB while still draining the pipe to completion. | Mitigated |
| T12.E | Elevation of Privilege | A scanner container with both the workspace mounted and network access could exfiltrate source. | Host/OS Access | DF24 | The multiscanner runs `--network none` with the workspace mounted; `aegis-netscanner` runs with network and never a workspace mount; `update-db` is the only networked run of the former. | Mitigated |
| T12.A | Abuse | `recon_scan` auto-authorizes every loopback and RFC1918 target with no allowlist entry, so a model-issued call can port-scan and template-scan the operator's home or corporate LAN. | Host/OS Access | DF24 | `security.dast.allow_active` defaults to `false` for aggressive checks, but passive nmap/nuclei sweeps of private space need no configuration at all. | Open |

#### Categories Not Applicable

*All seven STRIDE-A categories produced at least one concrete threat for this component.*

---

## SessionStore

**Trust Boundary:** Daemon
**Role:** SQLite database holding conversations, traces and cost records for every session.
**Data Flows:** DF15
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T13.T | Tampering | Any process running as the operator can edit the SQLite file directly, rewriting conversation history and trace records with no integrity check on read. | Host/OS Access | DF15 | Add a per-record MAC or an append-only trace log so history tampering is detectable. | Open |
| T13.R | Repudiation | Trace records are the closest thing to an audit trail and they are freely mutable, so they cannot support a repudiation claim. | Host/OS Access | DF15 | Keep security-relevant events in a separate append-only log rather than the same mutable database. | Open |
| T13.I1 | Information Disclosure | Full conversation content — including file contents and any secrets the agent read — is persisted unencrypted and retained indefinitely. | Host/OS Access | DF15 | Offer at-rest encryption keyed to the OS credential store, and a retention policy that prunes old sessions by default. | Open |
| T13.I2 | Information Disclosure | `sqlitestore.Open` creates the parent directory `0o700` but the database file itself is created by the driver with default permissions and no `fsguard.RestrictToOwner` call, so on Windows it inherits the parent ACL rather than an owner-only one. | Host/OS Access | DF15 | Apply `fsguard.RestrictToOwner` to the database file (and its `-wal`/`-shm` companions) as `daemon.token` already does. | Open |
| T13.D | Denial of Service | Concurrent runs contend on a single SQLite file and can pile up on lock waits. | Host/OS Access | DF15 | `journal_mode=WAL` plus a 5s `busy_timeout` applied per connection through the DSN. | Mitigated |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | A local file has no identity to assume; access control is the filesystem's. |
| Elevation of Privilege | The store grants no capability — reading it yields data, not authority. |
| Abuse | The store has no feature surface to misuse; it is written and read only by the daemon. |

---

## CheckpointStore

**Trust Boundary:** Daemon
**Role:** Per-turn file snapshots supporting `/rewind`.
**Data Flows:** DF16
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T14.T | Tampering | A modified snapshot is restored over live workspace files on rewind, so tampering with the store becomes tampering with the working tree. | Host/OS Access | DF16 | Record a content digest per snapshot and verify it before restore. | Open |
| T14.I | Information Disclosure | Snapshots contain complete copies of workspace files, including any secrets present at snapshot time, and persist after the session that created them. | Host/OS Access | DF16 | Reap snapshots with the session, and apply owner-only permissions to the snapshot directory. | Open |
| T14.D | Denial of Service | Per-turn snapshots of a large workspace grow without a documented bound and can fill the disk. | Host/OS Access | DF16 | Cap total snapshot bytes per session and evict oldest first. | Open |
| T14.A | Abuse | `/rewind` is a legitimate feature that silently reverts files, so an injected instruction can use it to undo a reviewer's edits or erase evidence of a prior tool call. | Host/OS Access | DF16 | Require explicit confirmation for a rewind that would discard changes made outside the agent's own tool calls. | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Snapshots carry no identity claim; they are content addressed by session and turn. |
| Repudiation | Snapshot creation is a side effect of turns already recorded in the session store. |
| Elevation of Privilege | Restoring a snapshot confers no capability beyond writing files the daemon could already write. |

---

## WorkspaceTrustStore

**Trust Boundary:** Daemon
**Role:** JSON store of per-directory trust grants, each pinned to the security-relevant fingerprint of that directory's project config.
**Data Flows:** DF17
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T15.T | Tampering | The grant file is written `0o600` inside a `0o700` directory but carries no integrity protection, so a same-user process can insert a grant for any directory and suppress the trust prompt. | Host/OS Access | DF17 | Apply `fsguard.RestrictToOwner` to the store file and sign the entry set with a key held in the OS credential store. | Open |
| T15.R | Repudiation | An entry records only `TrustedAt` and the fingerprint — not who granted it or through which interface — so an inserted grant is indistinguishable from an operator decision. | Host/OS Access | DF17 | Record the granting interface and process identity alongside each entry. | Open |
| T15.E1 | Elevation of Privilege | The fingerprint deliberately excludes `.aegis/.env`, so a project can change the secrets loaded into the daemon's environment without invalidating an existing trust grant. | Host/OS Access | DF17 | Documented as a deliberate hole in `internal/config/fingerprint.go`; the load-order constraint that motivates it should be revisited so the file can be covered. | Open |
| T15.E2 | Elevation of Privilege | A grant written before fingerprints existed has an empty `Fingerprint` and would otherwise match any configuration. | Host/OS Access | DF17 | `Store.Check` treats an empty fingerprint as a pre-P66.25 grant and re-prompts once. | Mitigated |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The store has no identity to assume; it is a local file consulted by one process. |
| Information Disclosure | Its contents are a list of directories the operator already knows they work in; it holds no secret. |
| Denial of Service | A corrupt or unreadable store fails closed — the trust prompt reappears — which is the safe outcome. |
| Abuse | The store exposes no feature to misuse; it is queried through `config.WorkspaceTrusted` and nothing else. |

---

## DaemonTokenFile

**Trust Boundary:** Daemon
**Role:** `daemon.token`, `daemon.crt` and `daemon.key` on the local filesystem — the whole of the daemon's credential material.
**Data Flows:** DF18, DF25
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T16.S | Spoofing | Possession of `daemon.token` is the entire identity model — there is no binding to a process, user session or client certificate. | Host/OS Access | DF25 | Add per-client tokens issued on an operator-confirmed handshake so a stolen credential is scoped and revocable. | Open |
| T16.T | Tampering | Deleting `daemon.crt`/`daemon.key` makes the daemon regenerate them on next start, silently changing the certificate every pinned client trusts. | Host/OS Access | DF18 | Treat regeneration of an existing pin as an event requiring explicit operator acknowledgement. | Open |
| T16.I1 | Information Disclosure | A world-readable token file would expose the daemon credential to every account on a shared host. | Host/OS Access | DF18 | Written `0o600` and then hardened by `fsguard.RestrictToOwner`, which applies a real non-inherited owner-only ACL on Windows where the mode bit is cosmetic. | Mitigated |
| T16.I2 | Information Disclosure | The token never rotates for the life of the file, so a credential captured once (backup, snapshot, screen share) stays valid indefinitely. | Host/OS Access | DF18 | Rotate on daemon restart, or on an interval, with clients re-reading the file. | Open |
| T16.D | Denial of Service | An unwritable or full data directory prevents token generation and blocks daemon start. | Host/OS Access | DF18 | `generateAndWriteToken` returns the error and `ListenAndServe` refuses to start with an empty token, failing closed. | Mitigated |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | The file records no actions; attribution lives in the daemon's logs and session store. |
| Elevation of Privilege | Reading the file yields the existing daemon credential — it confers no privilege above what that credential already carries. |
| Abuse | A credential file has no feature surface to misuse. |

---

## AnthropicAPI

**Trust Boundary:** External
**Role:** Cloud LLM API reached over HTTPS with an `x-api-key` header; the destination for prompts, file contents and tool results.
**Data Flows:** DF12
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T17.S | Spoofing | A `provider.base_url` override pointed at an attacker-controlled HTTPS host receives every request including the API key; the override is warned about, not refused. | Authenticated User | DF12 | `validateBaseURL` warns when a cloud provider's default host is overridden. Escalate to a confirmation prompt, or treat the override as a security-relevant config key frozen under workspace trust. | Open |
| T17.T | Tampering | The model's response is trusted to shape tool calls; a compromised or substituted endpoint controls what the agent does next. | Authenticated User | DF12 | Keep destructive tool calls behind operator approval regardless of provider, which the default `build` mode does for execute-capable tools. | Open |
| T17.R | Repudiation | There is no local, tamper-evident record of exactly what payload left the host for the vendor, so a later data-exposure question cannot be answered from the machine. | Authenticated User | DF12 | Log a hash and byte count of each outbound provider payload to the audit sink. | Open |
| T17.I1 | Information Disclosure | Source code, configuration and any secrets present in read files leave the operator's machine for a third party under that vendor's retention terms. | Authenticated User | DF12 | Apply `internal/redact` to outbound provider payloads when the endpoint is not loopback, and surface a per-session indicator of what has been sent. | Open |
| T17.I2 | Information Disclosure | A `base_url` on plaintext HTTP would send the API key in the clear. | Authenticated User | DF12 | `validateBaseURL` refuses outright when the scheme is `http`, the host is non-loopback and a real API key would be attached. | Mitigated |
| T17.D | Denial of Service | Vendor rate limiting, quota exhaustion or an outage stalls every run. | Authenticated User | DF12 | Retry, failover and admission-control decorators wrap the adapter (`internal/provider/retry.go`, `failover.go`, `admission.go`). | Mitigated |
| T17.A | Abuse | Legitimate agent behaviour drives unbounded metered spend, since every `cost.*` cap defaults to `0`. | Authenticated User | DF12 | Ship non-zero `cost.session_cap_usd`/`cost.daily_cap_usd` defaults for cloud providers. | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Elevation of Privilege | The vendor endpoint holds no privilege in this system; its influence is covered under Tampering. |

---

## OllamaServer

**Trust Boundary:** External
**Role:** Operator-run local model server, typically `http://localhost:11434`, serving completions to the engine.
**Data Flows:** DF13
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T18.S | Spoofing | Ollama listens on loopback with no authentication, so any local process that binds the port first — or wins a restart race — answers as the model and dictates the agent's tool calls. | Local Process Access | DF13 | Support a shared secret or Unix-socket path for the local provider, and record the endpoint's identity across restarts. | Open |
| T18.T | Tampering | Completions traverse plaintext loopback HTTP and can be observed or modified by a local process with packet-capture privilege. | Local Process Access | DF13 | Allow TLS to a local provider endpoint, and prefer a filesystem socket where the platform supports it. | Open |
| T18.I | Information Disclosure | Prompts containing workspace file contents traverse that same plaintext loopback hop. | Local Process Access | DF13 | Same remediation as T18.T; the daemon's own listener already solved this with pinned TLS. | Open |
| T18.D | Denial of Service | Model load, eviction, or a `num_ctx` mismatch between the Modelfile and the daemon's plan stalls or degrades every run. | Local Process Access | DF13 | The `num_ctx` decorator and the persisted per-model capability cache (`internal/modelcaps`) detect and adapt; the turn-stall bound reclaims a wedged run. | Mitigated |
| T18.E | Elevation of Privilege | A pulled model file carries no signature, so a poisoned or backdoored model steers tool calls while appearing to be the model the operator selected. | Local Process Access | DF13 | Model provenance is the model host's and Ollama's responsibility; Aegis can only record the digest it was served. | Platform |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | The local model server performs no attributable action in this system; its influence is recorded as the turn content the engine persists. |
| Abuse | Aegis exposes no feature of the local model server to a caller; abuse of the model's behaviour is covered under Engine's Abuse threats. |

---

## ExternalMCPServer

**Trust Boundary:** External
**Role:** Third-party MCP servers supplying tools and returning content into the model's context.
**Data Flows:** DF21
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T19.S | Spoofing | A stdio server is authenticated only by the fact that the configured command started; an HTTP server is authenticated only by TLS to a configured URL. | Local Process Access | DF21 | Pin an absolute command path and a binary digest for stdio servers; require a pinned certificate or token for HTTP servers. | Open |
| T19.T1 | Tampering | Tool results are attacker-controlled text placed directly into the model's context — the primary indirect prompt-injection vector in the system. | Local Process Access | DF21 | `wrapUntrusted` always applies a provenance marker and `trust.ScanForInjection` annotates hits, but neither constrains the model mechanically. | Open |
| T19.T2 | Tampering | `tools/list_changed` lets a server swap a tool's schema and semantics after the operator has already approved calls to that name. | Local Process Access | DF21 | Re-prompt on a semantic change to an approved tool, not only on the first approval. | Open |
| T19.I | Information Disclosure | The model chooses tool arguments, so an injected instruction can move workspace file contents into an argument sent to the server. | Local Process Access | DF21 | `warnOutboundSecrets` warns only; add refusal for arguments matching known secret classes. | Open |
| T19.D | Denial of Service | A hanging server stalls the tool round it participates in. | Local Process Access | DF21 | Per-call timeouts are enumerated and kept below the turn-stall bound. | Mitigated |
| T19.E | Elevation of Privilege | A server can advertise a tool whose name shadows or resembles a built-in, so the model calls the external one believing it is calling the harness. | Local Process Access | DF21 | Namespace MCP tool names on exposure and reject collisions with built-in names. | Open |
| T19.A | Abuse | A server writes its tool descriptions specifically to induce the model to call it with sensitive arguments, using a legitimate protocol feature as a social-engineering channel against the model. | Local Process Access | DF21 | Wrap server-supplied descriptions in the untrusted-content marker already applied to their results. | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | The server's actions surface as tool calls the engine already records; it has no separately attributable action in this system. |

---

## ExternalWebService

**Trust Boundary:** External
**Role:** Arbitrary HTTP(S) endpoints and search backends reached by `web_fetch` and `web_search`.
**Data Flows:** DF14
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T20.S | Spoofing | Search results are scraped from DuckDuckGo/Marginalia HTML and carry no origin authentication beyond transport TLS; a challenge page or a poisoned result set is parsed as authoritative. | Authenticated User | DF14 | `looksLikeDDGChallenge`/`looksLikeMarginaliaChallenge` detect interstitials; surface parse-confidence to the model rather than presenting scraped results as facts. | Open |
| T20.T | Tampering | Fetched HTML is converted to text and enters model context, making any page the model visits an injection vector. | Authenticated User | DF14 | `trust.Wrap` marks the content and `ScanForInjection` annotates hits, including zero-width-obfuscated payloads; the marker is advisory. | Open |
| T20.I | Information Disclosure | The model chooses the URL, so workspace-derived data can be encoded into the request path or query string and observed by the destination. | Authenticated User | DF14 | Apply `internal/redact` classes to outbound URLs and refuse fetches whose URL contains material matching a secret pattern. | Open |
| T20.D | Denial of Service | A slow or very large response ties up the fetch path. | Authenticated User | DF14 | Bounded retries with `webBackoff`, a response-size limit via `defaultFetchLimit`, and per-call timeouts under the stall bound. | Mitigated |
| T20.E | Elevation of Privilege | SSRF: a fetch to a loopback or private address reaches the operator's Ollama, the Aegis daemon itself, or a cloud metadata endpoint. | Authenticated User | DF14 | `netblock.SafeDialer` blocks `0.0.0.0/8`, RFC1918, CGNAT, link-local, multicast, reserved space and the NAT64 prefix, and `CheckRedirect` re-validates every redirect hop. | Mitigated |
| T20.A | Abuse | `web_fetch` is a legitimate feature that doubles as an exfiltration channel: an injected instruction encodes file content into a URL the agent then fetches. | Authenticated User | DF14 | Combine the T20.I redaction with an operator-visible egress log of every fetched URL. | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | The remote service takes no action inside this system; fetches are recorded as tool calls by the engine. |

---

## ContainerRuntime

**Trust Boundary:** External
**Role:** Docker or Podman on the host, driven through its CLI to run sandbox and scanner containers.
**Data Flows:** DF22, DF24
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T21.T | Tampering | `sandbox.image` defaults to the mutable tag `ubuntu:22.04`; the image behind that tag can change between runs, and scanner images are likewise referenced by tag. | Local Process Access | DF22, DF24 | Pin images by digest and record the resolved digest in the scan/session record. | Open |
| T21.I | Information Disclosure | The workspace bind mount makes every file under the root readable inside the container, including files the specific command had no need to see. | Local Process Access | DF22 | Mount only the paths a command's classified argv requires. | Open |
| T21.D | Denial of Service | If the runtime is unavailable or a container fails to start, `SelectSandbox` degrades to a weaker backend rather than failing the command, so a runtime outage becomes a silent loss of isolation. | Local Process Access | DF22 | Make the degradation fatal by default (`sandbox.strict`) so an outage is a visible failure. | Open |
| T21.E1 | Elevation of Privilege | On Linux, membership of the `docker` group is equivalent to host root, so any process able to drive the runtime — including the sandboxed command if the socket is reachable — can escape to the host. | Local Process Access | DF22 | Docker's group-equals-root model is the runtime's, not Aegis's; prefer rootless Podman and never mount the socket into a container. | Platform |
| T21.E2 | Elevation of Privilege | A container running with default capabilities and privilege escalation enabled weakens the isolation the sandbox exists to provide. | Local Process Access | DF22 | `OCIHardeningFlags` applies `--cap-drop=ALL` and `--security-opt=no-new-privileges` to every run, alongside memory, CPU and PID limits. | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The runtime is located by binary name on `PATH` and authenticates nothing; binary substitution is covered under SandboxBackend. |
| Repudiation | Container runs are recorded by the runtime's own daemon and by the engine's tool record; the runtime adds no separately attributable action here. |
| Abuse | Aegis exposes no runtime feature to a caller beyond running the sandbox image; misuse of that path is covered under SandboxBackend's Abuse threat. |
