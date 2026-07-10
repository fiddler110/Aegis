# STRIDE + Abuse Cases — Threat Analysis

> This analysis uses the standard **STRIDE** methodology (Spoofing, Tampering, Repudiation, Information Disclosure, Denial of Service, Elevation of Privilege) extended with **Abuse Cases** (business logic abuse, workflow manipulation, feature misuse). The "A" column in tables below represents Abuse — a supplementary category covering threats where legitimate features are misused for unintended purposes. This is distinct from Elevation of Privilege (E), which covers authorization bypass.

## Exploitability Tiers

| Tier | Label | Prerequisites | Assignment Rule |
|------|-------|---------------|----------------|
| **Tier 1** | Direct Exposure | `None` | Exploitable by unauthenticated external attacker with NO prior access. The prerequisite field MUST say `None`. |
| **Tier 2** | Conditional Risk | Single prerequisite: `Authenticated User`, `Privileged User`, `Internal Network`, or `Local Process Access` | Requires exactly ONE form of access. The prerequisite field has ONE item. |
| **Tier 3** | Defense-in-Depth | `Host/OS Access`, `Admin Credentials`, `{Component} Compromise`, `Physical Access`, or MULTIPLE prerequisites joined with `+` | Requires significant prior breach, infrastructure access, or multiple combined prerequisites. |

> **Deployment override in effect:** This system is classified `LOCALHOST_SERVICE` (see `0.1-architecture.md`). Tier 1 (`Prerequisites = None`) is forbidden for every component in this analysis — every threat below carries a `Local Process Access` (T2) or `Host/OS Access` (T3) floor, or `Internal Network` (T2) for outbound-only external services.

## Summary

| Component | Link | S | T | R | I | D | E | A | Total | T1 | T2 | T3 | Risk |
|-----------|------|---|---|---|---|---|---|---|-------|----|----|----|------|
| TerminalUI | [Link](#terminalui) | 0 | 1 | 0 | 0 | 1 | 0 | 0 | 2 | 0 | 2 | 0 | Low |
| Client | [Link](#client) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Low |
| Server | [Link](#server) | 1 | 1 | 0 | 1 | 1 | 0 | 0 | 4 | 0 | 3 | 1 | Medium |
| WebUI | [Link](#webui) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | High |
| Engine | [Link](#engine) | 0 | 0 | 0 | 1 | 1 | 0 | 0 | 2 | 0 | 2 | 0 | Medium |
| ToolRegistry | [Link](#toolregistry) | 0 | 1 | 0 | 0 | 0 | 1 | 1 | 3 | 0 | 3 | 0 | High |
| PermissionGate | [Link](#permissiongate) | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Low |
| PersonaLoader | [Link](#personaloader) | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | High |
| SkillRegistry | [Link](#skillregistry) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Medium |
| OutputGuard | [Link](#outputguard) | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 0 | 1 | 0 | Low |
| SecurityScanner | [Link](#securityscanner) | 0 | 1 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 1 | 1 | Low |
| CronScheduler | [Link](#cronscheduler) | 0 | 0 | 1 | 0 | 0 | 1 | 0 | 2 | 0 | 1 | 1 | High |
| SwarmCoordinator | [Link](#swarmcoordinator) | 0 | 0 | 0 | 0 | 1 | 1 | 0 | 2 | 0 | 2 | 0 | Medium |
| MCPClient | [Link](#mcpclient) | 0 | 0 | 0 | 1 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Medium |
| MCPServer | [Link](#mcpserver) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | High |
| ACPAgent | [Link](#acpagent) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Medium |
| ExecutionSandbox | [Link](#executionsandbox) | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | 1 | 0 | Low |
| ConfigLoader | [Link](#configloader) | 0 | 0 | 0 | 2 | 0 | 0 | 0 | 2 | 0 | 1 | 1 | Medium |
| AnthropicAdapter | [Link](#anthropicadapter) | 0 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| OpenAIAdapter | [Link](#openaiadapter) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| SessionStore | [Link](#sessionstore) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Medium |
| CheckpointStore | [Link](#checkpointstore) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Medium |
| MemoryStore | [Link](#memorystore) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Medium |
| Mailbox | [Link](#mailbox) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Low |
| AnthropicAPI | [Link](#anthropicapi) | 0 | 0 | 0 | 1 | 1 | 0 | 0 | 2 | 0 | 2 | 0 | Medium |
| OpenAICompatibleEndpoint | [Link](#openaicompatibleendpoint) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Medium |
| MCPExternalServers | [Link](#mcpexternalservers) | 0 | 1 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Medium |
| GitHubAPI | [Link](#githubapi) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Internet | [Link](#internet) | 0 | 1 | 0 | 0 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Medium |
| ContainerRuntime | [Link](#containerruntime) | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | 1 | 0 | Medium |
| **Totals** | | **4** | **11** | **1** | **14** | **6** | **7** | **4** | **47** | **0** | **38** | **9** | |

---

## TerminalUI

**Trust Boundary:** ClientProc
**Role:** Bubbletea terminal client; renders streamed turns, dialogs, approvals
**Data Flows:** DF01, DF03
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T01.T | Tampering | Streamed model text may contain ANSI/OSC control sequences (cursor moves, hidden text, title-bar/clipboard OSC injection) that manipulate the terminal before rendering; no sanitization was found in the render path | Local Process Access | DF01 | None observed — the renderer is not shown to strip/escape control sequences | Open |
| T02.D | Denial of Service | An extremely large single streamed chunk could stall rendering | Local Process Access | DF01 | Wrap-cache and scroll-follow logic already handle large streamed content (project history: P21.7 scroll-follow fix) | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | TerminalUI presents whatever Client already fetched from an authenticated session; it makes no independent identity decision |
| Repudiation | Turn-level audit is captured in SessionStore/turn traces, not the renderer |
| Information Disclosure | TerminalUI holds no data beyond what is already visible on screen via an authenticated session |
| Elevation of Privilege | No privilege levels exist inside the terminal renderer |
| Abuse | No workflow beyond rendering exists at this layer beyond the tampering threat above |

---

## Client

**Trust Boundary:** ClientProc
**Role:** HTTP client wrapper used by TerminalUI/CLI; holds the bearer auth token
**Data Flows:** DF03, DF04
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T03.I | Information Disclosure | The bearer token resides in Client's process memory for its lifetime; a core dump or memory inspection of the process would disclose it | Host/OS Access | DF04 | None observed beyond OS-level process protection | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Client presents the token; it does not itself authenticate anyone |
| Tampering | Client has no persistent state to tamper with |
| Repudiation | Requests are attributable via the bearer token at the Server |
| Denial of Service | Client has no listener or shared resource to exhaust |
| Elevation of Privilege | Client carries no privilege beyond the single token it holds |
| Abuse | Client implements no workflow logic of its own |

---

## Server

**Trust Boundary:** DaemonProc
**Role:** Daemon HTTP API; wires sessions, tools, permissions, personas, swarm, MCP, cron, checkpoints
**Data Flows:** DF04, DF05, DF06, DF22, DF23, DF24, DF25, DF34
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T04.S | Spoofing | No rate limiting, backoff, or alerting exists on repeated invalid-bearer-token attempts (`authMiddleware`); a 256-bit token makes brute force infeasible, but no audit signal exists if attempts occur | Local Process Access | DF04 | Constant-time comparison only (`subtle.ConstantTimeCompare`) | Open |
| T05.T | Tampering | `server.addr` is operator-configurable to a non-loopback address (e.g. `0.0.0.0:4127`) with no validation or startup warning; combined with T04 this would expose the full API to the network | Privileged User | DF04 | Default remains `127.0.0.1:4127`; no code enforces the default | Open |
| T07.D | Denial of Service | A malicious or runaway client could open unbounded concurrent runs or hold SSE connections open indefinitely | Local Process Access | DF04 | `server.max_concurrent_runs` (429 on overflow), `server.max_run_duration_sec`, bounded per-connection SSE buffer (P21.5) | Mitigated |

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T06.I | Information Disclosure | Client-Server traffic is unencrypted (plain HTTP over loopback); on a shared multi-user host, another local account with packet-capture privilege could observe conversation content and the bearer token | Host/OS Access | DF04 | Loopback-only default limits exposure to the local host | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Repudiation | Session/turn tracing (SessionStore) covers request attribution at this layer |
| Elevation of Privilege | Server has no internal privilege tiers beyond authenticated/not; scope escalation is analyzed under PermissionGate and PersonaLoader |
| Abuse | Workflow-abuse threats are enumerated under the components that actually perform actions (ToolRegistry, CronScheduler) |

---

## WebUI

**Trust Boundary:** DaemonProc
**Role:** Embedded browser UI served at `/ui` (Preact/Vite SPA)
**Data Flows:** DF02, DF06
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T08.S | Spoofing | `GET /ui` and `POST /auth/exchange` are intentionally pre-auth so a browser navigation can request a page token before it has any credential. This means **any local process** that can reach `127.0.0.1:4127` — not only the operator's own browser — can mint a page token and redeem it at `/auth/exchange` for the real daemon bearer token, fully obtaining the credential the token scheme exists to protect | Local Process Access | DF02, DF06 | Page tokens are single-use and expire after 60s, which limits *replay* but does not restrict *who* may request one in the first place | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Tampering | Page content is server-rendered from a fixed embedded bundle; not attacker-modifiable in transit given the loopback-only default |
| Repudiation | Audit for authenticated calls is covered at the Server layer once the real token is obtained |
| Information Disclosure | The token disclosure is exactly the Spoofing threat above, not a separate surface |
| Denial of Service | No additional availability surface beyond Server's existing run/connection ceilings |
| Elevation of Privilege | The Spoofing threat above already grants the top-level credential directly; no further escalation step exists |
| Abuse | No workflow-abuse surface distinct from the credential-acquisition threat above |

---

## Engine

**Trust Boundary:** DaemonProc
**Role:** Core agent loop: model calls, tool dispatch, compaction, output guard, loop detection, budget
**Data Flows:** DF05, DF07, DF08, DF09, DF10, DF11, DF12, DF13, DF14, DF15
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T09.I | Information Disclosure | Full conversation content — including any file contents, secrets, or credentials a tool read into context — is forwarded to whichever provider adapter is configured, with no redaction/DLP step at the Engine layer | Internal Network | DF14, DF15 | Operators may configure a local Ollama backend instead; env-strip only protects the shell tool's own environment, not conversation content | Open |
| T10.D | Denial of Service | No token/dollar budget ceiling is enforced when using a local (free) model (`OPENAI_API_KEY=ollama`), so a runaway tool loop has no cost-based circuit breaker | Local Process Access | DF14, DF15 | `internal/engine/loopdetect.go` catches repeating tool-call loops; a wall-clock run timeout (P21.5) bounds duration | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Engine does not itself authenticate external parties; that is Server's responsibility |
| Tampering | Model responses are consumed as typed data via the `Adapter` interface, not executed |
| Repudiation | SessionStore captures the full turn trace |
| Elevation of Privilege | Privilege decisions are delegated entirely to PermissionGate |
| Abuse | Workflow-abuse threats are enumerated under ToolRegistry, the component that actually executes actions |

---

## ToolRegistry

**Trust Boundary:** DaemonProc
**Role:** Registry of 39+ built-in tools (shell, git/git-pr, web fetch/search, LSP, security scan, memory, diagram, etc.)
**Data Flows:** DF07, DF16, DF17, DF18, DF19, DF20, DF21
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T11.T | Tampering | The `lsp` tool spawns an external LSP server process whose `command`/`args` are sourced from project/user config with no allowlist, checksum, or path restriction; a malicious config (e.g. from a cloned repository) can point at an arbitrary binary | Local Process Access | DF07 | Relies entirely on the pre-existing config trust boundary — whoever controls `.aegis/config.yaml` already controls the workspace | Open |
| T12.A | Abuse | `web_fetch`/`web_search` results are injected into the model's context with **no untrusted-content marker**, unlike MCP tool output (which is always wrapped in `<mcp_untrusted_output>` by `internal/mcp/trust.go`); a malicious or compromised web page is an unmarked indirect-prompt-injection vector | Internal Network | DF21 | SSRF-safe dialer restricts the *destination* (loopback/RFC1918/link-local/cloud-metadata, including on redirect) but does nothing about the *content* of allowed destinations | Open |
| T13.E | Elevation of Privilege | In `auto` permission mode, every execute-capability tool (shell, git, etc.) runs with no per-call confirmation | Local Process Access | DF18 | `build` mode (the default) requires interactive approval; contextual/rule gates still apply in `auto` | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | ToolRegistry does not authenticate callers; PermissionGate is the enforcement point |
| Repudiation | Tool calls are recorded in turn traces |
| Information Disclosure | Beyond T12 above, no additional distinct disclosure surface was identified (`git.go`'s allowlist and env-strip mitigate the git/shell paths) |
| Denial of Service | No additional availability surface beyond Engine's/Server's existing ceilings |

---

## PermissionGate

**Trust Boundary:** DaemonProc
**Role:** Capability gate (read/write/execute/network/spawn), contextual egress gate, text allow/deny rule engine, advisory persona tool gate
**Data Flows:** DF08
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T14.E | Elevation of Privilege | `PersonaToolGate` is advisory-only: a tool call outside a persona's declared `Tools` list is warned and allowed (or interactively confirmed) but never blocked; an operator who assumes a persona's tool list is a hard security boundary is mistaken | Local Process Access | DF08 | Documented as advisory by design; real enforcement is capability mode + contextual gate + rule engine, all of which remain active regardless | Mitigated |
| T15.T | Tampering | A loosely-scoped `allow` rule could historically be widened via shell metacharacters (`&&`, `\|`, backticks) to permit more than intended | Local Process Access | DF08 | `globToRegexpExec` restricts execute-capability allow-rule patterns; regression-tested (`rules_test.go`) | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | No external identity is evaluated at this layer |
| Repudiation | Gate decisions are exposed via SSE approval events and captured in the turn trace |
| Information Disclosure | The gate holds no sensitive data of its own |
| Denial of Service | No additional availability surface beyond the components it gates |
| Abuse | Covered by the Elevation of Privilege threat above |

---

## PersonaLoader

**Trust Boundary:** DaemonProc
**Role:** Loads/hot-reloads built-in and project/user persona `.md` files
**Data Flows:** DF09
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T16.T | Tampering | Persona `.md` frontmatter and system-prompt body are parsed and injected verbatim into the model's system prompt with no sanitization; a malicious `.aegis/personas/*.md` file (e.g. committed to a compromised or malicious cloned repository) is a direct prompt-injection vector against every session using that persona | Local Process Access | DF09 | None at the loader layer; partially offset by the mode/rule escalation guard, which limits *permission* impact but not prompt content | Open |
| T17.E | Elevation of Privilege | A loaded persona could historically raise a session's permission mode or smuggle in extra allow rules via its frontmatter | Local Process Access | DF09 | `resolveSessionMode`/`filterPersonaRules` strip escalation from any `Loaded` persona; regression-tested | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The loader does not authenticate anyone |
| Repudiation | Persona selection is recorded as part of session configuration |
| Information Disclosure | The loader stores no secrets of its own |
| Denial of Service | No availability surface beyond normal file reads |
| Abuse | The workflow-abuse impact of persona content is the prompt-injection threat above |

---

## SkillRegistry

**Trust Boundary:** DaemonProc
**Role:** Progressive-disclosure skill loader: project/user `.md` skills + binary-embedded built-in skills
**Data Flows:** DF10
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T18.T | Tampering | The same unsanitized-content injection pattern as PersonaLoader applies to skill `.md` bodies (project/user skill files), which are wrapped and injected into the system prompt verbatim | Local Process Access | DF10 | None at the loader layer | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The loader does not authenticate anyone |
| Repudiation | Skill activation is recorded as part of the turn/session context |
| Information Disclosure | The loader stores no secrets of its own |
| Denial of Service | No availability surface beyond normal file reads |
| Elevation of Privilege | Skills carry no independent permission logic; the resulting tool calls are gated by PermissionGate |
| Abuse | The workflow-abuse impact of skill content is the prompt-injection threat above |

---

## OutputGuard

**Trust Boundary:** DaemonProc
**Role:** Optional second model pass validating final output against a rubric/JSON schema
**Data Flows:** DF11
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T19.A | Abuse | On a guard-model transport failure, `LLMGuard` fails open by design, but no distinct log/metric differentiates "guard passed" from "guard was skipped due to transport failure" — an operator relying on the guard for compliance has no signal that validation silently did not occur for that turn | Local Process Access | DF11 | Ambiguous/malformed verdicts fail closed by design, which is the harder failure mode to get right | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The guard call is internal to the daemon; no external identity is involved |
| Tampering | The guard consumes typed output, not executable content |
| Repudiation | Guard verdicts are part of the turn trace |
| Information Disclosure | The guard introduces no additional data store or exposure surface |
| Elevation of Privilege | The guard has no privilege/authorization role |

---

## SecurityScanner

**Trust Boundary:** DaemonProc
**Role:** Wraps external SAST/SCA/DAST tools (semgrep, trivy, grype, kubescape, hadolint, ZAP, etc.)
**Data Flows:** DF16
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T21.I | Information Disclosure | DAST/recon scans could be pointed at arbitrary network targets, potentially probing infrastructure outside the intended scope | Internal Network | DF16 | `isHostAllowed` allow-list (loopback/RFC1918 always allowed, else explicit allow-list); hostnames matched as literal strings to avoid TOCTOU identity changes | Mitigated |

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T20.T | Tampering | Installer scripts for scanner tools (`internal/security/install.go`) invoke `exec.CommandContext(ctx, shell, args...)`; the construction of `args` on this specific path was not fully verified during this analysis (all other scanner invocations were confirmed to use argv-style `exec.CommandContext` with no shell interpolation) | Host/OS Access | DF16 | Unconfirmed — flagged for manual verification (see `0-assessment.md` → Needs Verification) | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The scanner is invoked synchronously by an already-authorized tool call; it introduces no independent identity |
| Repudiation | Scan invocations are recorded in the turn trace |
| Denial of Service | Scans run synchronously with caller-controlled timeouts; no distinct availability surface |
| Elevation of Privilege | The scanner runs with the same privilege as the daemon process; no escalation path beyond T20 |
| Abuse | No workflow beyond scan invocation exists at this layer |

---

## CronScheduler

**Trust Boundary:** DaemonProc
**Role:** SQLite-backed scheduler that fires background shell/tool jobs unattended
**Data Flows:** DF22, DF27
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T22.E | Elevation of Privilege | A scheduled job fires its stored shell command through `cronShellRunner`/`ExecStreaming` with **no permission gate, no `Approver`, and no approval prompt of any kind** — unlike the equivalent interactive tool call, which goes through PermissionGate/contextual gate/approval | Local Process Access | DF27 | Only a 10-minute wall-clock timeout bounds the run | Open |

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T23.R | Repudiation | `Job.Command` is stored as a raw shell string in SQLite with no execution audit trail distinct from a normal tool-call trace | Host/OS Access | DF27 | None observed | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Jobs execute under the daemon's own identity; there is no external caller to spoof |
| Tampering | Job storage uses the same SQLite mechanism as SessionStore (see SessionStore for the datastore-level threat) |
| Information Disclosure | No distinct disclosure surface beyond SessionStore's general datastore risk |
| Abuse | The unattended-execution threat above is captured as Elevation of Privilege, not a separate business-logic abuse |

---

## SwarmCoordinator

**Trust Boundary:** DaemonProc
**Role:** Spawns sub-agents as goroutines (in-process) or OS subprocesses; adaptive concurrency limiter
**Data Flows:** DF23, DF26
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T24.D | Denial of Service | Sub-agents spawned by SwarmCoordinator share a single dollar/token budget tracker (P10.3); one runaway or malicious sub-agent can exhaust the budget for every sibling agent in the swarm | Local Process Access | DF23 | `AdaptiveLimiter` bounds concurrency but not aggregate spend | Open |
| T25.E | Elevation of Privilege | Sub-agents previously bypassed the parent's contextual/rule gates via a bare mode gate (P10.1), losing sandbox/env-strip/budget enforcement | Local Process Access | DF23 | `buildGate(cfg.Mode, ...)` now applied uniformly to in-process and subprocess sub-agents; `TestSubAgentRunnerAppliesOperatorDenyRule` regression test | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Sub-agents are spawned by the trusted daemon process itself; no external identity is involved |
| Tampering | Inter-agent message integrity is a Mailbox-level concern, covered under Mailbox |
| Repudiation | Sub-agent actions are recorded via the same turn-trace mechanism as top-level runs |
| Information Disclosure | Disclosure risk from mailbox contents is covered under Mailbox |
| Abuse | The budget-exhaustion threat above is categorized as Denial of Service, not business-logic abuse |

---

## MCPClient

**Trust Boundary:** DaemonProc
**Role:** Aegis acting as an MCP client (stdio + HTTP/SSE) to external MCP servers
**Data Flows:** DF17
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T26.A | Abuse | The opt-in `scan_output` heuristic injection scan is disabled by default, is regex-based, and only appends a warning banner rather than filtering; a reworded, encoded, or otherwise obfuscated prompt-injection payload from a malicious/compromised MCP server passes through undetected even when enabled | Internal Network | DF17 | The always-on `<mcp_untrusted_output>` provenance wrapper still tells the model the content is untrusted data, independent of the scan outcome | Open |
| T27.I | Information Disclosure | Without any marking, a compromised MCP server's output would be indistinguishable from trusted instructions to the model | Internal Network | DF17 | Every `tools/call`/`resources/read`/`prompts/get` result is unconditionally wrapped in a provenance marker (`internal/mcp/trust.go`) | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Server identity is an operator-configuration matter (stdio spawns a specific binary; HTTP/SSE uses per-server bearer auth) |
| Tampering | In-transit tampering is covered by the provenance-wrapping mitigation above |
| Repudiation | MCP calls are recorded in the turn trace |
| Denial of Service | No distinct availability surface beyond normal network calls |

---

## MCPServer

**Trust Boundary:** DaemonProc
**Role:** `aegis mcp-serve` — exposes Aegis sessions as MCP tools to other MCP-speaking harnesses over stdio
**Data Flows:** DF24, DF32
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T28.S | Spoofing | `handleToolsCall` performs **no authentication check** on `aegis_prompt`, `aegis_new_session`, or `aegis_list_sessions`; any local process capable of writing to the subprocess's stdin can drive full agent turns as the legitimate calling harness | Local Process Access | DF32 | New sessions default to `plan` (read-only) mode; destructive actions additionally require `--auto-approve`/`mcp_server.auto_approve` | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Tampering | MCPServer is a thin stdio protocol handler with no independent data to tamper with |
| Repudiation | Session/turn traces cover attribution once a session exists |
| Information Disclosure | Disclosure risk is a direct consequence of the Spoofing threat above, not a separate surface |
| Denial of Service | Governed by the same run/concurrency ceilings as any other session |
| Elevation of Privilege | Downstream actions are gated by the session's own permission mode, same as any other caller |
| Abuse | Covered by the Spoofing threat above |

---

## ACPAgent

**Trust Boundary:** DaemonProc
**Role:** ACP JSON-RPC server (stdio) for editor integrations (Zed, Neovim)
**Data Flows:** DF25, DF33
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T29.S | Spoofing | The ACP `methodAuthenticate` handler is a no-op stub that always acknowledges; any local process able to write to the subprocess's stdin is trusted as the editor without any credential check | Local Process Access | DF33 | Relies entirely on the OS process boundary — the editor is expected to be the only thing that spawned this subprocess | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Tampering | ACPAgent is a thin stdio protocol handler with no independent data to tamper with |
| Repudiation | Session/turn traces cover attribution once a session exists |
| Information Disclosure | Disclosure risk is a direct consequence of the Spoofing threat above |
| Denial of Service | Governed by the same run/concurrency ceilings as any other session |
| Elevation of Privilege | Downstream actions are gated by the session's own permission mode |
| Abuse | Covered by the Spoofing threat above |

---

## ExecutionSandbox

**Trust Boundary:** DaemonProc
**Role:** Pluggable exec backend: local shell, Docker, Podman, WSL, Apple Containers
**Data Flows:** DF18, DF27, DF28
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T30.E | Elevation of Privilege | When the configured container backend fails to initialize and `cfg.Strict` is not set, `SelectSandbox` falls back to local, unsandboxed execution and only logs a warning (surfaced via `/healthz`) rather than blocking; an operator not watching logs/health silently loses the sandbox's defense-in-depth layer | Local Process Access | DF18, DF27 | `cfg.Strict` makes this a hard failure instead of a silent fallback | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | ExecutionSandbox is invoked only by already-authorized internal callers |
| Tampering | No independent data store to tamper with |
| Repudiation | Executions are recorded in the turn trace |
| Information Disclosure | No distinct disclosure surface beyond command output already covered under ToolRegistry |
| Abuse | Container-engine-specific privilege risk is covered under ContainerRuntime |

---

## ConfigLoader

**Trust Boundary:** DaemonProc
**Role:** Layered config (defaults → user config → project config → env vars); `.aegis/.env` secret loading
**Data Flows:** DF34
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T31.I | Information Disclosure | `MCPServerConfig.Auth`/`Search.APIKey` support `$VAR` expansion via `os.ExpandEnv`, but nothing prevents an operator from hardcoding a literal secret directly in `config.yaml` instead of referencing an environment variable | Privileged User | DF34 | `.aegis/config.yaml` is excluded from git by the blanket `/.aegis/*` `.gitignore` rule by default; `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` are excluded from YAML unmarshaling entirely (`koanf:"-"`); CLI debug/dry-run paths redact `Provider.APIKey` | Mitigated |

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T32.I | Information Disclosure | Nothing enforces Windows ACL hardening on `.aegis/.env` the way `restrictToOwner` hardens `daemon.token`; on a shared Windows host, another local account may be able to read secrets from `.env` via the data/project directory's inherited ACL | Host/OS Access | DF34 | None beyond the parent directory's default permissions | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | ConfigLoader is read at process startup; no runtime identity decision is made here |
| Tampering | Config file integrity is a Host/OS-Access-level concern already captured above |
| Repudiation | Config is not a per-request audit surface |
| Denial of Service | No availability surface beyond a one-time startup read |
| Elevation of Privilege | Config values feed PermissionGate/PersonaLoader, which are analyzed independently |
| Abuse | No workflow logic exists at this layer beyond secret handling above |

---

## AnthropicAdapter

**Trust Boundary:** DaemonProc
**Role:** `provider.Adapter` implementation streaming to the Anthropic Messages API
**Data Flows:** DF14, DF29
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T33.D | Denial of Service | A cloud outage or API rate-limit halts sessions pinned to this adapter with no automatic local fallback unless configured | Internal Network | DF29 | `internal/provider/failover.go` and `retry.go` exist specifically to handle transient/outage conditions | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | TLS + system CA trust validate the endpoint identity; analyzed from the external service's perspective under AnthropicAPI |
| Tampering | TLS protects data in transit; content-level trust is analyzed under AnthropicAPI |
| Repudiation | API calls are recorded in the turn trace |
| Information Disclosure | Data-exposure risk is analyzed under AnthropicAPI (the actual trust boundary crossed) |
| Elevation of Privilege | The adapter carries no privilege beyond making an outbound API call |
| Abuse | No workflow-abuse surface distinct from Engine's dispatch logic |

---

## OpenAIAdapter

**Trust Boundary:** DaemonProc
**Role:** `provider.Adapter` implementation streaming to an OpenAI-compatible endpoint (cloud OpenAI or local Ollama)
**Data Flows:** DF15, DF30
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T34.T | Tampering | When configured against a local Ollama endpoint, the connection is typically plain local HTTP (Ollama commonly does not terminate TLS); on a shared host another local account could observe or tamper with model traffic | Local Process Access | DF30 | Default Ollama bind is also loopback-only, matching the daemon's own posture | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Endpoint identity is analyzed from the external service's perspective under OpenAICompatibleEndpoint |
| Repudiation | API calls are recorded in the turn trace |
| Information Disclosure | Data-exposure risk is analyzed under OpenAICompatibleEndpoint |
| Denial of Service | Outage/failover handling mirrors AnthropicAdapter |
| Elevation of Privilege | The adapter carries no privilege beyond making an outbound API call |
| Abuse | No workflow-abuse surface distinct from Engine's dispatch logic |

---

## SessionStore

**Trust Boundary:** DaemonProc
**Role:** SQLite conversations/turn traces/cost store on local disk
**Data Flows:** DF12
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T35.I | Information Disclosure | The SQLite database is plaintext with no encryption at rest (no SQLCipher/`PRAGMA key`) and, unlike `daemon.token`, gets no explicit Windows ACL hardening — it inherits the data directory's default ACL. Full conversation content, including any secrets the model saw, persists indefinitely in a readable file | Host/OS Access | DF12 | `EnsureDataDir` creates the parent directory with `0o700` on POSIX | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | SessionStore is a passive datastore written only by the already-authorized Engine |
| Tampering | No independent integrity-enforcement surface beyond file permissions, captured in the disclosure risk above |
| Repudiation | SessionStore *is* the audit mechanism for other components |
| Denial of Service | No availability surface distinct from normal disk I/O |
| Elevation of Privilege | The store has no privilege model of its own |
| Abuse | No workflow logic exists at the datastore layer |

---

## CheckpointStore

**Trust Boundary:** DaemonProc
**Role:** Per-turn file snapshots enabling `/rewind`
**Data Flows:** DF13
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T36.I | Information Disclosure | Per-turn file snapshots are stored unencrypted on local disk and may contain sensitive file content captured for `/rewind` | Host/OS Access | DF13 | Same data-directory permissions as SessionStore | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Passive datastore written only by the already-authorized Engine |
| Tampering | Restore is pure file-content rollback with no command re-execution (confirmed: `RestoreFiles` only calls `os.WriteFile`/`os.Remove`) |
| Repudiation | Checkpoint creation/restore is recorded in the turn trace |
| Denial of Service | No availability surface distinct from normal disk I/O |
| Elevation of Privilege | The store has no privilege model of its own |
| Abuse | No workflow logic exists at the datastore layer |

---

## MemoryStore

**Trust Boundary:** DaemonProc
**Role:** Project/user persistent memory files with relevance scoring
**Data Flows:** DF19
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T37.T | Tampering | Memory files are plain files under the project/user memory directory; nothing prevents direct editing of a memory file to inject false "learned" content that a future session will trust as its own prior conclusions — a persistent, low-visibility prompt-injection vector | Host/OS Access | DF19 | None beyond file permissions | Open |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Passive datastore written only by the already-authorized ToolRegistry |
| Repudiation | Memory writes occur as part of a traced tool call |
| Information Disclosure | Same Host/OS-Access-level disclosure risk as SessionStore; not separately elevated here |
| Denial of Service | No availability surface distinct from normal disk I/O |
| Elevation of Privilege | The store has no privilege model of its own |
| Abuse | The injection risk above is categorized as Tampering, not business-logic abuse |

---

## Mailbox

**Trust Boundary:** DaemonProc
**Role:** File-based inter-agent mailbox for swarm messaging
**Data Flows:** DF26
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

*No Tier 2 threats identified.*

#### Tier 3 — Defense-in-Depth

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T38.I | Information Disclosure | Inter-agent messages are plaintext files under `dataDir/teams/.../inbox`, readable by any process running as the same OS user, including a compromised sibling tool/plugin | Host/OS Access | DF26 | `0o700` directory / `0o600` file permissions restrict access to the OS user account | Mitigated |

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Only the trusted SwarmCoordinator writes to the mailbox |
| Tampering | Same file-permission mitigation as the disclosure risk above |
| Repudiation | Messages are timestamped and consumed as part of the swarm's own tracing |
| Denial of Service | No availability surface distinct from normal disk I/O |
| Elevation of Privilege | The store has no privilege model of its own |
| Abuse | No workflow logic exists at the datastore layer |

---

## AnthropicAPI

**Trust Boundary:** None (external)
**Role:** Anthropic's cloud Messages API
**Data Flows:** DF29
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T39.I | Information Disclosure | Full conversation content — potentially including secrets, proprietary source code, or PII a tool read from disk — is transmitted to a third-party cloud LLM provider with no local redaction/DLP step | Internal Network | DF29 | Anthropic's own data-handling terms apply; operators may choose a local Ollama backend instead | Open |
| T40.D | Denial of Service | A full Anthropic outage stalls any session pinned to that provider | Internal Network | DF29 | `failover.go`/`retry.go`; operators can switch to an OpenAI-compatible/local model | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | TLS + certificate validation cover endpoint spoofing in transit |
| Tampering | TLS protects data in transit |
| Repudiation | Out of this system's control boundary; Anthropic's own infrastructure |
| Elevation of Privilege | No privilege boundary is crossed by this external call beyond data exposure |
| Abuse | No workflow-abuse surface distinct from the data-exposure risk above |

---

## OpenAICompatibleEndpoint

**Trust Boundary:** None (external)
**Role:** Cloud OpenAI API or local Ollama server, selected by configuration
**Data Flows:** DF30
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T41.I | Information Disclosure | The same cross-cloud data-exposure risk as AnthropicAPI applies when configured against cloud OpenAI; the risk does not apply when configured against local Ollama, since data never leaves the host | Internal Network | DF30 | Local-Ollama configuration eliminates this risk entirely; the risk is purely a function of operator configuration | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | TLS (cloud) or loopback binding (local Ollama) cover endpoint spoofing |
| Tampering | TLS (cloud) or loopback binding (local Ollama) cover in-transit tampering |
| Repudiation | Out of this system's control boundary for the cloud case; local Ollama has no distinct audit requirement |
| Denial of Service | Outage/failover handling mirrors AnthropicAPI |
| Elevation of Privilege | No privilege boundary is crossed by this external call beyond data exposure |
| Abuse | No workflow-abuse surface distinct from the data-exposure risk above |

---

## MCPExternalServers

**Trust Boundary:** None (external)
**Role:** Third-party MCP servers Aegis connects out to as a client
**Data Flows:** DF31
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T42.T | Tampering | A malicious or compromised configured MCP server can return arbitrary content, including embedded instructions attempting to manipulate the agent (indirect prompt injection / confused deputy) via `tools/call`/`resources/read`/`prompts/get` results | Internal Network | DF31 | Always-on provenance wrapping (`internal/mcp/trust.go`) plus opt-in heuristic scan (see MCPClient T26/T27) | Mitigated |
| T43.I | Information Disclosure | Tool-call arguments constructed by the model — potentially containing file contents or secrets the model has read — are sent to whichever MCP server is configured, which receives that data unconditionally at the transport layer | Internal Network | DF31 | None beyond the operator's choice of which MCP servers to configure | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Server identity is an operator-configuration matter (stdio spawns a specific binary; HTTP/SSE uses per-server bearer auth) |
| Repudiation | MCP calls are recorded in the turn trace |
| Denial of Service | No distinct availability surface beyond normal network calls |
| Elevation of Privilege | No privilege boundary is crossed beyond the content-trust threats above |
| Abuse | Captured as Tampering (T42) above, consistent with the STRIDE-A boundary between manipulation and misuse |

---

## GitHubAPI

**Trust Boundary:** None (external)
**Role:** Reached indirectly through the `gh` CLI / git credential helper for PR creation
**Data Flows:** DF20
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T44.I | Information Disclosure | PR titles/bodies generated by the model and pushed via `gh pr create` could inadvertently include secrets or sensitive content the model had in context, publishing them to a (possibly public) GitHub repository | Internal Network | DF20 | `git.go`'s subcommand allowlist and denylisted flags prevent config/exec escapes, but do not inspect PR body content | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | Credentials are never handled in-process; delegated entirely to the `gh` CLI / git credential helper |
| Tampering | No in-process construction of GitHub API requests exists to tamper with |
| Repudiation | GitHub's own audit log covers this boundary |
| Denial of Service | Out of this system's control; GitHub availability is a vendor concern |
| Elevation of Privilege | No privilege boundary is crossed beyond the disclosure risk above |
| Abuse | No workflow-abuse surface distinct from the disclosure risk above |

---

## Internet

**Trust Boundary:** None (external)
**Role:** Arbitrary HTTP(S) targets reached by the web-fetch/web-search tools
**Data Flows:** DF21
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T45.A | Abuse | Arbitrary fetched web content re-enters the model's context with no untrusted-content marker (see ToolRegistry T12), making indirect prompt injection from any reachable page a live risk | Internal Network | DF21 | SSRF-safe dialer restricts the *destination* but not the *content* of allowed destinations | Open |
| T46.T | Tampering | Without dial-time and redirect-time IP validation, `web_fetch`/`web_search` could be used to reach internal-only services (SSRF) | Internal Network | DF21 | DNS-then-IP validation before dial, revalidated on every redirect hop (5-hop cap); covers RFC1918/loopback/link-local/IPv6 equivalents and the `169.254.169.254` cloud-metadata address | Mitigated |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | The Internet is by definition an untrusted, unauthenticated zone; no Aegis-side identity is asserted against it |
| Repudiation | Outbound requests are recorded in the turn trace |
| Denial of Service | No distinct availability surface beyond normal network calls |
| Elevation of Privilege | No privilege boundary is crossed beyond the content-trust threat above |

---

## ContainerRuntime

**Trust Boundary:** None (external)
**Role:** Local Docker/Podman/WSL/Apple Containers engine the sandbox execs into
**Data Flows:** DF28
**Pod Co-location:** N/A

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 threats identified.*

#### Tier 2 — Conditional Risk

| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T47.E | Elevation of Privilege | Granting the daemon access to the Docker/Podman socket is equivalent to local-root/administrator access on the host — a well-known property of these container engines; any component able to reach ExecutionSandbox's container backend inherits this | Local Process Access | DF28 | ExecutionSandbox's auto-detection and strict/fallback warning (see ExecutionSandbox T30) is the only local control; no rootless-mode enforcement was found | Open |

#### Tier 3 — Defense-in-Depth

*No Tier 3 threats identified.*

#### Categories Not Applicable

| Category | Justification |
|----------|---------------|
| Spoofing | ContainerRuntime is a local, already-trusted engine the operator installed |
| Tampering | No in-process construction of container-engine requests beyond the privilege-equivalence risk above |
| Repudiation | Container execution is recorded in the turn trace via ExecutionSandbox |
| Information Disclosure | No distinct disclosure surface beyond command output already covered under ToolRegistry |
| Abuse | No workflow-abuse surface distinct from the privilege-equivalence risk above |
