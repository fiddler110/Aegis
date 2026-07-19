# STRIDE + Abuse Cases — Threat Analysis

> This analysis uses the standard **STRIDE** methodology (Spoofing, Tampering, Repudiation, Information Disclosure, Denial of Service, Elevation of Privilege) extended with **Abuse Cases** (business logic abuse, workflow manipulation, feature misuse). The "A" column in tables below represents Abuse — a supplementary category covering threats where legitimate features are misused for unintended purposes. This is distinct from Elevation of Privilege (E), which covers authorization bypass.

## Exploitability Tiers

| Tier | Label | Prerequisites | Assignment Rule |
|------|-------|---------------|----------------|
| **Tier 1** | Direct Exposure | `None` | Exploitable by unauthenticated external attacker with NO prior access. The prerequisite field MUST say `None`. |
| **Tier 2** | Conditional Risk | Single prerequisite: `Authenticated User`, `Privileged User`, `Internal Network`, or single `{Boundary} Access` | Requires exactly ONE form of access. The prerequisite field has ONE item. |
| **Tier 3** | Defense-in-Depth | `Host/OS Access`, `Admin Credentials`, `{Component} Compromise`, `Physical Access`, or MULTIPLE prerequisites joined with `+` | Requires significant prior breach, infrastructure access, or multiple combined prerequisites. |

Aegis remains classified `LOCALHOST_SERVICE` (the daemon binds loopback-only by default), so there are **no Tier 1 threats**: every threat requires at least local process access to the host. Since the 2026-07-12 baseline, a new workspace-trust gate (`internal/workspacetrust`) has closed or substantially narrowed several of the previously dominant "hostile cloned repository" threats — see the `Change` column below and the Executive Summary in `0-assessment.md`.

## Summary

| Component | Link | S | T | R | I | D | E | A | Total | T1 | T2 | T3 | Risk |
|-----------|------|---|---|---|---|---|---|---|-------|----|----|----|------|
| TerminalUI | [link](#terminalui) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Client | [link](#client) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Server | [link](#server) | 0 | 1 | 0 | 1 | 1 | 1 | 1 | 5 | 0 | 5 | 0 | Low |
| WebUI | [link](#webui) | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Low |
| Engine | [link](#engine) | 0 | 1 | 0 | 1 | 1 | 0 | 1 | 4 | 0 | 4 | 0 | Moderate |
| AnthropicAdapter | [link](#anthropicadapter) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Moderate |
| OpenAIAdapter | [link](#openaiadapter) | 1 | 0 | 0 | 1 | 1 | 0 | 0 | 3 | 0 | 3 | 0 | Moderate |
| OllamaAdapter | [link](#ollamaadapter) | 1 | 0 | 0 | 1 | 1 | 0 | 0 | 3 | 0 | 3 | 0 | Moderate |
| ToolRegistry | [link](#toolregistry) | 0 | 1 | 0 | 2 | 0 | 1 | 1 | 5 | 0 | 5 | 0 | Elevated |
| PermissionGate | [link](#permissiongate) | 0 | 1 | 0 | 0 | 0 | 2 | 1 | 4 | 0 | 4 | 0 | Low |
| OutputGuard | [link](#outputguard) | 0 | 1 | 0 | 1 | 0 | 0 | 1 | 3 | 0 | 3 | 0 | Low |
| ExecutionSandbox | [link](#executionsandbox) | 0 | 1 | 0 | 1 | 0 | 2 | 0 | 4 | 0 | 2 | 2 | Moderate |
| SwarmCoordinator | [link](#swarmcoordinator) | 0 | 0 | 0 | 0 | 1 | 1 | 1 | 3 | 0 | 3 | 0 | Low |
| MCPClient | [link](#mcpclient) | 0 | 0 | 0 | 2 | 0 | 1 | 1 | 4 | 0 | 4 | 0 | Low |
| MCPServer | [link](#mcpserver) | 1 | 0 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Low |
| ACPAgent | [link](#acpagent) | 1 | 0 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Low |
| CronScheduler | [link](#cronscheduler) | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 2 | 0 | 2 | 0 | Elevated |
| HooksRunner | [link](#hooksrunner) | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 2 | 0 | 2 | 0 | Low |
| ConfigLoader | [link](#configloader) | 0 | 2 | 0 | 1 | 0 | 1 | 1 | 5 | 0 | 5 | 0 | Moderate |
| WorkspaceTrust | [link](#workspacetrust) | 0 | 1 | 0 | 0 | 0 | 1 | 1 | 3 | 0 | 3 | 0 | Moderate |
| PersonaLoader | [link](#personaloader) | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Low |
| SkillRegistry | [link](#skillregistry) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| SecurityScanner | [link](#securityscanner) | 0 | 1 | 0 | 0 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Low |
| ToolCallProbe | [link](#toolcallprobe) | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 0 | 1 | 0 | Low |
| SessionStore | [link](#sessionstore) | 0 | 1 | 1 | 1 | 0 | 0 | 0 | 3 | 0 | 2 | 1 | Moderate |
| CheckpointStore | [link](#checkpointstore) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Low |
| MemoryStore | [link](#memorystore) | 0 | 1 | 0 | 1 | 0 | 0 | 1 | 3 | 0 | 2 | 1 | Low |
| KnowledgeIndex | [link](#knowledgeindex) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Low |
| Mailbox | [link](#mailbox) | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 2 | 0 | 0 | 2 | Low |
| AnthropicAPI | [link](#anthropicapi) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Moderate |
| OpenAICompatibleEndpoint | [link](#openaicompatibleendpoint) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Moderate |
| OllamaNativeEndpoint | [link](#ollamanativeendpoint) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Low |
| GitHubAPI | [link](#githubapi) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Internet | [link](#internet) | 0 | 1 | 0 | 1 | 0 | 0 | 1 | 3 | 0 | 3 | 0 | Low |
| MCPExternalServers | [link](#mcpexternalservers) | 0 | 1 | 0 | 0 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Low |
| ContainerRuntime | [link](#containerruntime) | 0 | 0 | 0 | 1 | 0 | 1 | 0 | 2 | 0 | 1 | 1 | Low |
| **Totals** | | **10** | **18** | **1** | **23** | **5** | **16** | **17** | **90** | **0** | **81** | **9** | |

> **Note on threat counts:** 90 threats across 36 analyzed components (up from 76 across 32 at the 2026-07-12 baseline) — 3 new components (`OllamaAdapter`, `WorkspaceTrust`, `ToolCallProbe`) and 1 new external entity (`OllamaNativeEndpoint`) fully contribute new threats, and several components gained one additional threat from newly-discovered or newly-introduced attack surface during re-analysis. The increase reflects broader coverage, not new insecurity — a large majority of previously-Open Tier 2 threats are now `Fixed` (see the `Change` column and `3-findings.md`).

---

## TerminalUI

**Trust Boundary:** ClientProcess
**Role:** Bubbletea terminal UI that renders the timeline, streaming output, dialogs, and slash commands
**Data Flows:** DF01, DF05

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T31.T | Tampering | Untrusted model/tool output rendered in the terminal could carry escape sequences that spoof the UI or manipulate the operator's terminal | Local Process Access | DF01 | `stripDangerousSeqs()` now actively strips OSC/DCS/APC/PM/SOS and non-SGR CSI sequences from untrusted raw tool output before rendering (`internal/tui/sanitize.go:72-129`), replacing the prior passive Bubbletea/lipgloss-only framing | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | TerminalUI has no identity to spoof; it renders local output only |
| Repudiation | No audit-relevant actions originate from this component |
| Information Disclosure | Renders only; does not independently transmit data |
| Denial of Service | Local rendering only, bounded by terminal size |
| Elevation of Privilege | No authorization decisions made here |
| Abuse | Covered by the Tampering threat above; no distinct feature-misuse vector |

---

## Client

**Trust Boundary:** ClientProcess
**Role:** HTTP+SSE client that connects the UI to the daemon and carries the bearer token
**Data Flows:** DF05, DF06

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T32.I | Information Disclosure | The client holds the daemon bearer token in memory and transmits it over loopback HTTP, observable by another local packet-capturing account | Local Process Access | DF06 | `server.tls.enabled` now **defaults true** (`internal/config/config.go`, P27.5) — pinned self-signed loopback TLS is on unless explicitly disabled; `NewFromConfig` now returns an error on a TLS-pin mismatch instead of silently connecting | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | Client authenticates the daemon via TLS cert pinning; no separate spoofing surface |
| Tampering | No local persistent state to tamper with |
| Repudiation | Not applicable to a stateless client |
| Denial of Service | Bounded by daemon-side controls |
| Elevation of Privilege | Client has no independent authorization role |
| Abuse | No distinct feature-misuse vector beyond the Information Disclosure threat |

---

## Server

**Trust Boundary:** DaemonProcess
**Role:** Daemon HTTP API; owns sessions, config-admin endpoints, web UI, and auth middleware
**Data Flows:** DF02, DF06, DF07, DF08, DF09, DF10, DF42

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified — loopback-only bind, bearer-token auth.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T01.T | Tampering | Config-mutating endpoints (`PATCH /config/*`, `POST /config/harden`) let a bearer-token holder alter sandbox/security posture at runtime | Local Process Access | DF06 | Requires the daemon bearer token; harden/install require an explicit `confirm` flag | Mitigated | Existing |
| T01.I | Information Disclosure | Client↔daemon traffic could be observed by another local account with packet-capture privilege | Local Process Access | DF06 | `server.tls.enabled` now defaults true (P27.5); pinned self-signed cert on loopback | Mitigated | Fixed |
| T01.D | Denial of Service | The API had no per-IP/connection rate limiting and unlimited default concurrency | Local Process Access | DF06 | `server.max_concurrent_runs` now defaults to 10, `server.max_run_duration_sec` to 1800 (`internal/config`, P27.12) | Mitigated | Fixed |
| T01.E | Elevation of Privilege | Binding a non-loopback address exposes the token-protected API to the network | Internal Network | DF06 | `validateListenAddr` refuses non-loopback bind unless `server.allow_remote` is explicitly set | Mitigated | Existing |
| T01.A | Abuse | A hostile local process or webpage drives the `/ui` auth flow to obtain the real daemon token | Local Process Access | DF02 | Bearer-token auth + origin middleware (anti-DNS-rebind) + single-use page token with double-submit CSRF, now scheme/proxy-aware `Secure` flag (P31.3/P32.10) | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | Covered by Abuse (T01.A) — the auth-flow-spoofing vector |

---

## WebUI

**Trust Boundary:** DaemonProcess
**Role:** Embedded Preact single-page app served at `/ui` on the daemon's loopback port
**Data Flows:** DF02

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T02.S | Spoofing | A cross-origin page tries to impersonate the operator's browser and steal the daemon token | Local Process Access | DF02 | Single-use page token, HttpOnly SameSite=Strict CSRF cookie (now scheme/proxy-aware `Secure`), no `Access-Control-Allow-Origin`, `X-Frame-Options: DENY` | Mitigated | Existing |
| T02.A | Abuse | DNS-rebinding a malicious site onto the loopback origin to call the API | Local Process Access | DF02 | `originMiddleware` rejects non-loopback `Origin`; strict CSP `connect-src 'self'` | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | No independent write surface beyond the daemon API (see Server) |
| Repudiation | No audit-relevant state owned by WebUI itself |
| Information Disclosure | Covered by Server's plaintext-transport threat (T01.I) |
| Denial of Service | Bounded by Server's controls |
| Elevation of Privilege | No independent authorization role |

---

## Engine

**Trust Boundary:** DaemonProcess
**Role:** Core agent loop — model calls, tool dispatch, budget/loop enforcement, guard integration, now with output-guard rollback
**Data Flows:** DF07, DF13, DF14, DF15-DF22, DF40

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T03.T | Tampering | Project context files (`AGENTS.md`, `CLAUDE.md`, `.aegis/context.md`) are concatenated into the system prompt with no untrusted-provenance marker, letting a cloned repo inject instructions | Local Process Access | DF18 | Personas/skills/memory are now trust-wrapped; context files specifically were not confirmed to be wrapped in this pass (see Needs Verification) | Open | Existing |
| T03.I | Information Disclosure | File/conversation content read into context is forwarded to the configured cloud model provider | Local Process Access | DF13, DF14, DF40 | `security.redact_secrets` now **defaults true** (gitleaks masking before model send, P27.3); local Ollama avoids egress | Mitigated | Fixed |
| T03.D | Denial of Service | A runaway tool-call loop or unbounded token spend exhausts cost/resources | Local Process Access | DF07 | Budget (`budget_usd`/`max_tokens_per_run`) and loop-detector abort the run | Mitigated | Existing |
| T03.A | Abuse | Prompt-injection embedded in tool output coerces the model into unintended tool calls | Local Process Access | DF17 | Untrusted-content wrapping + injection scan (now default-on for search/MCP output, P27.13); execute tools still gated | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | Engine has no external identity to spoof |
| Repudiation | Covered by SessionStore's per-turn trace persistence |
| Elevation of Privilege | Authorization decisions belong to PermissionGate |

---

## AnthropicAdapter

**Trust Boundary:** DaemonProcess
**Role:** Normalizes the Anthropic Messages API to the provider seam; refactored onto the shared SSE client helper
**Data Flows:** DF13, DF31

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T04.S | Spoofing | A project config `provider.base_url` override redirects requests (bearing `ANTHROPIC_API_KEY`) to an attacker-controlled host | Local Process Access | DF31 | `validateBaseURL` (`internal/providerfactory/factory.go:108-154`, P27.2) blocks plaintext-HTTP to a non-loopback host with a real key attached; an HTTPS override still only **warns** and is not gated by the workspace-trust store | Open | Existing |
| T04.I | Information Disclosure | The API key is transmitted on every request | Local Process Access | DF31 | Key read only from env; never logged/stored; stripped from child/shell env; HTTPS default | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | No independent write surface |
| Repudiation | Not independently auditable beyond SessionStore |
| Denial of Service | Covered generically by Engine's budget controls |
| Elevation of Privilege | No independent authorization role |
| Abuse | No distinct feature-misuse vector beyond credential handling above |

---

## OpenAIAdapter

**Trust Boundary:** DaemonProcess
**Role:** Normalizes OpenAI/Azure/Ollama-compat chat-completions to the provider seam; refactored onto the shared SSE client, gained response-header-timeout config
**Data Flows:** DF14, DF32

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T05.S | Spoofing | A project config `provider.base_url` override redirects requests (bearing `OPENAI_API_KEY`) to an attacker host; the default Ollama endpoint is plain HTTP loopback | Local Process Access | DF32 | Same `validateBaseURL` partial mitigation as T04.S | Open | Existing |
| T05.I | Information Disclosure | The API key is transmitted to the configured endpoint | Local Process Access | DF32 | Key from env only; never logged/stored; HTTPS for cloud endpoints | Mitigated | Existing |
| T40.D | Denial of Service | A slow or hung provider response (no response-header timeout) could stall an agent run indefinitely, previously with no bound | Local Process Access | DF32 | `WithResponseHeaderTimeout` on the shared SSE client (`internal/provider/sse`, P35.5-P35.7) now bounds the wait for response headers before treating the request as failed | Mitigated | New (previously unidentified — confirmed absent in the P22.5/baseline-era adapter code; fixed by the same commit range documented in the roadmap as "native-Ollama response-header-timeout cluster") |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | No independent write surface |
| Repudiation | Not independently auditable beyond SessionStore |
| Elevation of Privilege | No independent authorization role |
| Abuse | No distinct feature-misuse vector beyond credential handling above |

---

## OllamaAdapter

**Trust Boundary:** DaemonProcess
**Role:** **[NEW]** Native Ollama `/api/chat` adapter (`internal/provider/ollama`), distinct from the OpenAI-compat path; supports bounded `keep_alive` and response-header-timeout tuning
**Data Flows:** DF40, DF41

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T33.S | Spoofing | A project config `provider.base_url` override redirects native-Ollama requests to an attacker host | Local Process Access | DF41 | Routed through the same `providerfactory.buildOne`/`validateBaseURL` as every other provider (P27.2) — same partial (HTTP-only) mitigation as T04.S/T05.S | Open | New |
| T33.I | Information Disclosure | The default native-Ollama endpoint uses plain HTTP on loopback | Local Process Access | DF41 | Local-only by default; content stays on-host unless `base_url` is redirected off-host | Mitigated | New |
| T33.D | Denial of Service | An unbounded `keep_alive` could pin a large model resident indefinitely, or a hung response could stall a run | Local Process Access | DF41 | `provider.keep_alive` now defaults to a bounded resident window (`providerfactory.defaultOllamaKeepAlive`, P35.4); `WithResponseHeaderTimeout` bounds header waits | Mitigated | New |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | No independent write surface |
| Repudiation | Not independently auditable beyond SessionStore |
| Elevation of Privilege | No independent authorization role |
| Abuse | No distinct feature-misuse vector identified beyond the threats above |

---

## ToolRegistry

**Trust Boundary:** DaemonProcess
**Role:** Tool dispatch with capability-based gating; read-only-downgrade allowlist recently expanded
**Data Flows:** DF17, DF23-DF27, DF33, DF34

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T06.T | Tampering | File-mutating tools could write outside the workspace via path traversal | Local Process Access | DF23 | `ValidatePath` with `Rel`-based escape check, `..` rejection, and symlink resolution incl. new files | Mitigated | Existing |
| T06.I | Information Disclosure | Read-capability tool output may contain secrets that are then forwarded to the model provider | Local Process Access | DF17 | `security.redact_secrets` now default-on (P27.3) masks detected secrets before model send | Mitigated | Existing |
| T36.I | Information Disclosure | The read-only-downgrade allowlist was expanded to include `env`/`printenv`/`ps`/`whoami`/`hostname` etc. (`internal/tool/builtin/shell_readonly.go:16-27`); these now run under `CapRead` (auto-approved even in `plan` mode) instead of `CapExecute`, so `env`/`printenv` can dump the full process environment (potentially including provider API keys) to the model with no approval prompt, and `ps` discloses other local processes' argv | Local Process Access | DF23 | Shell-metachar/argument restrictions still apply to the command itself, but do not limit what `env`'s own output reveals | Open | New (previously unidentified — the downgrade allowlist did not include `env`/`printenv` at baseline; introduced by commit `6bcce63`) |
| T06.E | Elevation of Privilege | A tool call could run at a higher capability than intended | Local Process Access | DF17 | Every tool declares a capability consulted by the permission gate before execution; the gate now also consults each call's *effective* capability (see PermissionGate T37.E) | Mitigated | Existing |
| T06.A | Abuse | Untrusted MCP/web tool output is treated as trusted instructions | Local Process Access | DF24, DF34 | All external tool output wrapped in untrusted-provenance markers | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | ToolRegistry has no external identity to spoof |
| Repudiation | Tool calls are persisted as per-turn traces in SessionStore |
| Denial of Service | Covered by Engine's budget/loop controls |

---

## PermissionGate

**Trust Boundary:** DaemonProcess
**Role:** Mode + rule + contextual authorization of every tool call; contextual gate now keyed on each call's effective capability
**Data Flows:** DF15

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T07.T | Tampering | A scoped allow rule is widened via shell chaining or a path-traversal variant | Local Process Access | DF15 | Execute allow rules use metachar-restricted globs; file rules normalize path + case | Mitigated | Existing |
| T07.E | Elevation of Privilege | A tool call escalates past the intended mode | Local Process Access | DF15 | plan denies write/execute and gates network; build gates execute; deny > allow > mode precedence | Mitigated | Existing |
| T37.E | Elevation of Privilege | The contextual gate previously consulted only a tool's *static* declared capability (`t.Capability()`); a tool statically classified `CapExecute` but dynamically reclassified `CapNetwork` for a specific call could skip the network-specific allowlist entirely | Local Process Access | DF15 | `ContextualGate.Check` now calls `tool.EffectiveCapability(t, input)` for the specific call (`internal/permission/contextual.go:105`, P32.2) | Mitigated | New (previously unidentified — the static-capability check predates this fix; closed by the cited commit) |
| T07.A | Abuse | A scoped rule on a tool lacking a recognized subject field silently never matches, giving false confidence | Local Process Access | DF15 | `WarnUnmatchableRules` logs such no-op rules at startup | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | PermissionGate has no external identity to spoof |
| Information Disclosure | Gate decisions do not themselves handle sensitive data |
| Denial of Service | Not a resource-bound surface |

---

## OutputGuard

**Trust Boundary:** DaemonProcess
**Role:** Second-model validation of final answers and written files; consumption pattern now includes a rollback path
**Data Flows:** DF16

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T08.T | Tampering | The guard runs after files are already written to disk, so a FAIL verdict previously could not un-write a malicious deliverable | Local Process Access | DF16 | The Engine now calls `checkpoint.Snapshotter.RestoreFiles` to roll back file writes when the guard's corrective retries are exhausted (`internal/checkpoint/checkpoint.go:266-278`, P27.16) | Mitigated | Fixed |
| T08.I | Information Disclosure | Untrusted content re-entering the guard's judge model could inject a forged verdict | Local Process Access | DF16 | Content tag-escaped and framed as data in the judge prompt | Mitigated | Existing |
| T08.A | Abuse | A successful-but-ambiguous guard response is treated as a pass | Local Process Access | DF16 | `parseVerdict` fails closed on ambiguous output, fails open only on transport error | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Verdicts are logged per-turn |
| Denial of Service | Bounded by Engine's budget controls |
| Elevation of Privilege | No independent authorization role (guard is disable-able only via trust-gated persona control fields, see PersonaLoader) |

---

## ExecutionSandbox

**Trust Boundary:** DaemonProcess
**Role:** Pluggable command execution isolation (local/OS/container); the OS backend now confines reads, not just writes/network
**Data Flows:** DF23, DF29, DF30, DF36

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T09.T | Tampering | The default `local` backend runs shell commands directly on the host with no filesystem/network/process isolation | Local Process Access | DF23 | Approval required in build mode; env-key stripping; startup refusal of auto-approve + local | Open | Existing |
| T09.E | Elevation of Privilege | The API key is never passed into the container and is stripped from the shell environment | Local Process Access | DF23 | `DefaultStripEnv` + no `-e` key injection into containers | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T09.I | Information Disclosure | The OS sandbox (seatbelt/bwrap) previously left the entire host filesystem readable, enabling read-then-exfiltrate of SSH keys/credentials | Host/OS Access | DF23 | Reads are now confined to `workspace ∪ readPaths ∪ toolchain defaults` (`defaultOSReadPaths()`, explicitly excludes `~/.ssh`, `~/.aws`, `~/.config`); `sandbox.os_extra_read_paths` allows operator-controlled widening (`internal/sandbox/os_sandbox.go`, P27.18) | Mitigated | Fixed |
| T09.E1 | Elevation of Privilege | When the container backend uses the Docker/Podman socket, socket access is root-equivalent on the host | Host/OS Access | DF36 | `--cap-drop=ALL`, `--security-opt=no-new-privileges`; documented socket-privilege notice | Open | Existing |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Command execution is logged per-turn |
| Denial of Service | Bounded by Engine's budget/duration controls |
| Abuse | Covered by Tampering (T09.T) — no distinct feature-misuse vector |

---

## SwarmCoordinator

**Trust Boundary:** DaemonProcess
**Role:** Spawns sub-agents as goroutines/subprocesses with mode clamping; detached spawns now guaranteed a budget share
**Data Flows:** DF10, DF28

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T10.D | Denial of Service | A detached/background sub-agent spawn previously lost the shared cost tracker and fell back to a fresh full budget, escaping the fan-out tree's ceiling | Local Process Access | DF10 | Detached spawns now inherit a guaranteed fair share of the parent's remaining budget (`internal/swarm`, commit `9dec380`, FIND-14) | Mitigated | Fixed |
| T10.E | Elevation of Privilege | A sub-agent requests a more permissive mode than its parent | Local Process Access | DF10 | `clampMode` allows only restriction; sub-agents inherit the full gate stack | Mitigated | Existing |
| T10.A | Abuse | Uncontrolled recursive fan-out exhausts host resources | Local Process Access | DF10 | `maxSpawnDepth=3`, `MaxParallelAgents=8`, adaptive limiter, per-agent duration cap | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Tampering | Covered by Mailbox's own STRIDE section for inter-agent messages |
| Repudiation | Spawns are logged per-turn |
| Information Disclosure | Covered by Mailbox's own STRIDE section |

---

## MCPClient

**Trust Boundary:** DaemonProcess
**Role:** Outbound MCP client (stdio + HTTP/SSE) to external tool servers; HTTP/SSE transport now behind its own SSRF-safe dialer
**Data Flows:** DF24, DF35

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T11.I | Information Disclosure | The HTTP MCP client previously used a plain `http.Client` with no SSRF dialer, so a configured `http://` endpoint could target internal/loopback services | Local Process Access | DF35 | New `mcpSSRFSafeDialer`/`mcpValidateNotPrivate`/`mcpPrivateRanges` (`internal/mcp/http.go:18-95`, P27.8) blocks RFC1918/loopback/link-local ranges on connect and every redirect | Mitigated | Fixed |
| T11.I2 | Information Disclosure | Sensitive data could be exfiltrated inside outbound tool-call arguments to an untrusted server | Local Process Access | DF35 | Opt-in `scan_arguments` flags credential-shaped arguments (log-only) | Mitigated | Existing |
| T11.E | Elevation of Privilege | An unlabeled MCP server's tools default to an over-permissive capability | Local Process Access | DF24 | Unknown/empty capability defaults to the most-restrictive `execute` (Ask in build, Deny in plan) | Mitigated | Existing |
| T11.A | Abuse | External MCP server output is treated as trusted instructions | Local Process Access | DF24 | All MCP output always wrapped untrusted; `mcp.servers[].scan_output` now defaults true (P27.13) | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | MCP server identity is operator-configured, not independently authenticated |
| Tampering | No independent write surface |
| Repudiation | Not independently auditable beyond SessionStore |
| Denial of Service | Bounded by Engine's budget controls |

---

## MCPServer

**Trust Boundary:** DaemonProcess
**Role:** `aegis mcp-serve` — exposes Aegis sessions as MCP tools over stdio; CLI entrypoint now always resolves an auth token
**Data Flows:** DF03, DF11

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T12.S | Spoofing | With `AEGIS_MCP_TOKEN` previously unset by default, any local process controlling the server's stdin could drive full agent turns | Local Process Access | DF03 | The `aegis mcp-serve` CLI entrypoint now always resolves a non-empty token (env var or auto-generated owner-only file) before constructing the server (`internal/cli/stdiotoken.go`, P27.4) — the underlying `mcpserver.Options.AuthToken` field itself still defaults empty when the package is embedded directly rather than via the CLI | Mitigated | Fixed |
| T12.E | Elevation of Privilege | A driving harness escalates a session's execution posture | Local Process Access | DF11 | New sessions default to plan mode; approvals auto-denied unless `AutoApprove` | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | No independent write surface beyond session ops |
| Repudiation | Session ops are logged per-turn |
| Information Disclosure | Covered by Engine's egress controls |
| Denial of Service | Bounded by Engine's budget controls |
| Abuse | Covered by the Spoofing/Elevation threats above |

---

## ACPAgent

**Trust Boundary:** DaemonProcess
**Role:** ACP JSON-RPC server over stdio for editor integrations; CLI entrypoint now always resolves an auth token
**Data Flows:** DF04, DF12

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T13.S | Spoofing | With `AEGIS_ACP_TOKEN` previously unset by default, any local process controlling stdin could invoke session/prompt operations | Local Process Access | DF04 | The `aegis acp` CLI entrypoint now always resolves `AEGIS_ACP_TOKEN` or an auto-generated `acp.token` file before calling `NewAgent` (`internal/cli/stdiotoken.go`, P27.4) | Mitigated | Fixed |
| T13.E | Elevation of Privilege | An editor integration escalates the session's execution posture | Local Process Access | DF12 | Session posture governed by the same permission gate stack | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | No independent write surface beyond session ops |
| Repudiation | Session ops are logged per-turn |
| Information Disclosure | Covered by Engine's egress controls |
| Denial of Service | Bounded by Engine's budget controls |
| Abuse | Covered by the Spoofing/Elevation threats above |

---

## CronScheduler

**Trust Boundary:** DaemonProcess
**Role:** Persistent scheduler for background shell/agent tasks — unchanged since baseline
**Data Flows:** DF09, DF29

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T14.E | Elevation of Privilege | Cron fire-time gating consults only the coarse mode policy — the text allow/deny rules and contextual egress gate are bypassed for scheduled commands | Local Process Access | DF29 | Fire-time gate re-reads the daemon's current mode | Open | Existing |
| T14.A | Abuse | An auto-mode session can create a persistent `auto_approve` cron job that keeps executing shell commands unattended in auto and build mode across restarts | Local Process Access | DF09 | Only plan mode blocks fire; jobs persisted in SQLite | Open | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Tampering | Covered by Elevation threat above |
| Repudiation | Jobs and fires are persisted in SQLite |
| Information Disclosure | Covered by Engine's egress controls |

---

## HooksRunner

**Trust Boundary:** DaemonProcess
**Role:** Runs project-configured lifecycle hooks on tool/session events; hook *definitions* from untrusted projects are now frozen by the workspace-trust gate
**Data Flows:** DF22, DF30

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T15.E | Elevation of Privilege | Hooks configured in a project `.aegis/config.yaml` previously ran arbitrary shell commands on `session_start`/tool events with no confirmation, giving a cloned malicious repo host code execution before the model even acted | Local Process Access | DF30 | `cfg.Hooks` is one of the five keys frozen by `applyWorkspaceTrust` to the user/global baseline unless the directory has been explicitly trusted via `aegis trust` (`internal/config/config.go:1219-1258`, P27.1) | Mitigated | Fixed |
| T15.A | Abuse | A `pre_tool_use` hook can observe/veto every tool call and its arguments, exposing data or manipulating the workflow | Local Process Access | DF22 | Same workspace-trust freeze as T15.E — a project-sourced `pre_tool_use` hook does not run at all until the directory is trusted | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Tampering | Covered by Elevation threat above |
| Repudiation | Hook dispatch is logged per-turn |
| Information Disclosure | Covered by the Abuse threat above |
| Denial of Service | Bounded by Engine's budget controls |

---

## ConfigLoader

**Trust Boundary:** DaemonProcess
**Role:** Layered config loader including project `.aegis/config.yaml`/`.env`; now applies the workspace-trust gate and flips several security defaults on
**Data Flows:** DF08, DF37

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T16.T | Tampering | The project `.aegis/config.yaml` previously overrode sandbox backend, permission rules, personas, and more with no workspace-trust gate | Local Process Access | DF08, DF37 | `applyWorkspaceTrust` now freezes `permission.*`, `sandbox.*`, `mcp.servers`, `notify.webhook`, and `hooks` to the user/global baseline unless the directory is trusted (`internal/config/config.go:1219-1258`, P27.1) | Mitigated | Fixed |
| T38.T | Tampering | `provider.base_url` is deliberately **not** one of the five trust-gated key groups — an untrusted project's `.aegis/config.yaml` can still set `provider.base_url` to an attacker-controlled **HTTPS** host, and `validateBaseURL` only warns (does not block) for a non-default HTTPS host, so the API key is still attached and sent | Local Process Access | DF08 | `validateBaseURL` blocks only the plaintext-HTTP-to-non-loopback sub-case; the HTTPS sub-case is unmitigated by either the trust gate or the base_url check | Open | New (previously unidentified as a residual gap — the original FIND-03 covered this broadly; P27.2 closed the HTTP sub-case but the design note in `factory.go:108-114` explicitly narrows scope away from the workspace-trust gate, leaving the HTTPS sub-case open) |
| T16.I | Information Disclosure | Project config could add attacker-controlled MCP servers or a `notify.webhook`, creating exfiltration channels | Local Process Access | DF08 | `mcp.servers` and `notify.webhook` are both trust-gated keys — frozen to baseline unless trusted (P27.1) | Mitigated | Fixed |
| T16.E | Elevation of Privilege | Project config could set `permission.mode`, `auto_approve_exec`, or `allow_unsandboxed_auto_exec`, lowering the approval posture | Local Process Access | DF08 | `permission.*` and `sandbox.*` are trust-gated keys — frozen to baseline unless trusted (P27.1); unsandboxed auto-exec still additionally requires the explicit opt-in flag | Mitigated | Fixed |
| T16.A | Abuse | `.aegis/.env` secrets are loaded into the process environment | Local Process Access | DF08 | `.env` hardened with owner-only ACL on read (best-effort); not itself trust-gated but always ACL-hardened | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Config load decisions are not independently audited beyond the workspace-trust diff log |
| Denial of Service | Not a resource-bound surface |

---

## WorkspaceTrust

**Trust Boundary:** DaemonProcess
**Role:** **[NEW]** Per-directory trust decision store (`internal/workspacetrust`) gating whether a project's security-relevant config/persona overrides are honored; driven by `aegis trust`
**Data Flows:** DF37, DF38, DF39

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T34.T | Tampering | A local process running as the same user could directly edit `<data_dir>/workspace_trust.json` to mark a hostile directory as trusted, bypassing the intended `aegis trust` confirmation flow | Local Process Access | DF37, DF38 | The file is `fsguard`-hardened (owner-only ACL), which stops other accounts but not the same user/process that already has local process access | Open | New |
| T34.E | Elevation of Privilege | Trusting a directory unfreezes `permission.*`, `sandbox.*`, `mcp.servers`, `notify.webhook`, and `hooks` for that path indefinitely; a single careless `aegis trust` grants a hostile repo durable full config control until explicitly revoked | Local Process Access | DF37, DF38 | `aegis trust --revoke` exists to undo a grant; no automatic expiry or narrower per-key trust grants are offered | Open | New |
| T34.A | Abuse | Directory-path normalization/case-folding (especially on Windows) could in principle be exploited via path aliasing (8.3 short names, symlinks, junctions) so an untrusted directory resolves to the same canonical form as a trusted one | Local Process Access | DF37, DF38 | Paths are normalized/case-folded before lookup, but alias-resistance against symlinks/junctions/8.3 names was not independently verified in this pass | Open | New |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof; trust decisions are local-operator-driven |
| Repudiation | Single-operator local tool; no meaningful non-repudiation requirement for trust grants |
| Information Disclosure | The trust store contains only directory paths and boolean decisions, not secrets |
| Denial of Service | Not a resource-bound surface |

---

## PersonaLoader

**Trust Boundary:** DaemonProcess
**Role:** Loads and hot-reloads persona system prompts from project/user directories; project persona control fields now honored only for trusted workspaces
**Data Flows:** DF18, DF38

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T17.T | Tampering | A project persona's frontmatter could previously set `output_guard: none`, `mode`, `tools`, and `rules` unconditionally, weakening the guard/permission posture when that persona is selected | Local Process Access | DF18, DF38 | `LoadFromDirs`/`Refresh` now take a `projectTrusted` flag (`internal/persona/load.go`, P27.7) — project persona control fields are honored only if the workspace is trusted; the persona body still loads (trust-wrapped) regardless | Mitigated | Fixed |
| T17.E | Elevation of Privilege | A selected malicious persona lowers the effective permission mode for the session | Local Process Access | DF18, DF38 | Same trust-gate as T17.T — an untrusted project persona's `mode` override is dropped, falling back to defaults | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Persona selection is logged per-turn |
| Information Disclosure | Persona body content handling covered under Engine |
| Denial of Service | Not a resource-bound surface |
| Abuse | Covered by the Tampering/Elevation threats above |

---

## SkillRegistry

**Trust Boundary:** DaemonProcess
**Role:** Progressive-disclosure skills from project/user/embedded sources; gained the embedded `threat-modeling` skill and a YAML-frontmatter parsing fix
**Data Flows:** DF19

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T18.T | Tampering | A project skill body could inject instructions into the model context | Local Process Access | DF19 | Project/user skill bodies wrapped in untrusted-provenance markers; only embedded builtins are trusted | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Skill loading is logged at startup |
| Information Disclosure | No sensitive data handled independently |
| Denial of Service | Not a resource-bound surface |
| Elevation of Privilege | Skills are advisory content, not control-field carriers |
| Abuse | Covered by the Tampering threat above |

---

## SecurityScanner

**Trust Boundary:** DaemonProcess
**Role:** Runs SAST/secret/DAST/recon scanners host or container; `nuclei` argument validation hardened, scanners consolidated into one image
**Data Flows:** DF25

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T39.T | Tampering | The `nuclei` `templates_version` value was previously unvalidated, reaching both a `filepath.Join` and a `git clone --branch` argument — a crafted value could achieve path traversal or git-argument injection | Local Process Access | DF25 | `templates_version` is now validated against `^[A-Za-z0-9._-]+$` and rejected if it starts with `-` or is pure-dot (`internal/security/recon.go`, P31.1) | Mitigated | New (previously unidentified — confirmed present in the baseline-era `recon.go`; fixed by the cited commit, closing a CodeQL-flagged alert) |
| T19.A | Abuse | The `recon_scan`/`dast_scan` tools make real outbound scans whose only real guard is the target-authorization allowlist | Local Process Access | DF25 | `security.dast.allowed_targets` is now explicitly sourced from user/global config **only**, never the project layer, even when the workspace is trusted (P27.9) | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Scans are logged per invocation |
| Information Disclosure | Scanner output handling covered under ToolRegistry |
| Denial of Service | Bounded by Engine's budget controls |
| Elevation of Privilege | No independent authorization role beyond the allowlist above |

---

## ToolCallProbe

**Trust Boundary:** DaemonProcess
**Role:** **[NEW]** Startup/`doctor` capability probe (`internal/toolcallprobe`) verifying a configured model can actually call tools
**Data Flows:** DF42

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T35.A | Abuse | A false-positive or false-negative probe verdict could misrepresent a model's real tool-calling capability, misleading the operator into trusting (or distrusting) a model incorrectly — the original motivation for this component was exactly such a false positive (a reasoning model thinking past the token cap, misreported as "can't call tools") | Local Process Access | DF42 | Verdict is cached per-model and re-derived at startup/`doctor`; a dedicated live-probe test tier (`toolcallprobe.TestLiveProbeReachesAVerdict`) exists to catch verdict-rule regressions, run on-demand rather than in default CI | Open | New |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Tampering | No persistent write surface beyond an in-memory/cached verdict |
| Repudiation | Diagnostic-only; not a security control requiring audit |
| Information Disclosure | The probe call itself is a minimal synthetic prompt, not user data |
| Denial of Service | Bounded, single small inference call at startup/on-demand |
| Elevation of Privilege | No authorization role |

---

## SessionStore

**Trust Boundary:** DaemonProcess
**Role:** SQLite store of full conversations, traces, cost, and cron jobs; pruning now cascades to checkpoint cleanup
**Data Flows:** DF20

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T20.T | Tampering | Another local account tampers with the session database files | Local Process Access | DF20 | `0o700` data dir + `fsguard` owner-only ACL on the DB and WAL/SHM sidecars | Mitigated | Existing |
| T20.R | Repudiation | Agent actions cannot be attributed after the fact | Local Process Access | DF20 | Per-turn traces (tokens, cost, tool names/durations) persisted for attribution | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T20.I | Information Disclosure | The session DB stores full conversations, tool outputs, and system prompts in plaintext with no encryption at rest | Host/OS Access | DF20 | Owner-only filesystem ACL is the sole confidentiality control; pruning now removes orphaned data sooner (`session.CheckpointCleaner`, P32.3) but does not add encryption | Open | Existing |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Denial of Service | Not a network-reachable surface |
| Elevation of Privilege | No independent authorization role |
| Abuse | Covered by the Tampering/Repudiation threats above |

---

## CheckpointStore

**Trust Boundary:** DaemonProcess
**Role:** Per-turn file-content snapshots for `/rewind`; now also the restore source for output-guard rollback
**Data Flows:** DF21

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 2 threats identified for this component.* | — | — | — | — | — |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T21.I | Information Disclosure | Pre-modification snapshots of arbitrary workspace file contents are stored plaintext in the session DB | Host/OS Access | DF21 | Shares the session DB's owner-only ACL; no encryption at rest | Open | Existing |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Tampering | Shares SessionStore's ACL hardening |
| Repudiation | Restore actions are logged per-turn |
| Denial of Service | Not a network-reachable surface |
| Elevation of Privilege | No independent authorization role |
| Abuse | No distinct feature-misuse vector beyond the Information Disclosure threat |

---

## MemoryStore

**Trust Boundary:** DaemonProcess
**Role:** Project/user memory files plus a long-term memory database; `longmem.db` now `fsguard`-hardened, memory content now trust-wrapped, retention cap added
**Data Flows:** DF26

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T22.T | Tampering | A memory file is hand-edited outside Aegis to plant false "learned" context | Local Process Access | DF26 | sha256 integrity sidecar prepends a tamper warning on out-of-band edits | Mitigated | Existing |
| T22.A | Abuse | A project `.aegis/memory.md` planted by a cloned repo was previously injected into every session's system prompt with only a tamper warning, not a block | Local Process Access | DF26 | Memory file content is now passed through `wrapMemoryFile` — the same untrusted-content provenance wrap applied to skills/personas/web output (`internal/memory/context.go`) | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T22.I | Information Disclosure | The long-term memory DB (`longmem.db`) previously relied on the `0o700` data dir only, so on a shared Windows host it could inherit a looser parent ACL | Host/OS Access | DF26 | `hardenDBPermissions()` now applies `fsguard.RestrictToOwner` to `longmem.db` and sidecars on open (`internal/memory/memory.go`, P27.10) | Mitigated | Fixed |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Repudiation | Memory writes are logged with the integrity sidecar |
| Denial of Service | Bounded by the new retention cap (`pruneToCap`) |
| Elevation of Privilege | No independent authorization role |

---

## KnowledgeIndex

**Trust Boundary:** DaemonProcess
**Role:** Per-project FTS index of file contents at `.aegis/knowledge.db`; now `fsguard`-hardened
**Data Flows:** DF27

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 2 threats identified for this component.* | — | — | — | — | — |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T23.I | Information Disclosure | The knowledge index DB previously relied on the `0o700` directory only, inheriting no independent ACL hardening | Host/OS Access | DF27 | Same `fsguard.RestrictToOwner` hardening pattern now applied to `knowledge.db` and sidecars (`internal/knowledge/knowledge.go`, P27.10) | Mitigated | Fixed |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | No external identity to spoof |
| Tampering | Shares the same ACL hardening as the Information Disclosure control |
| Repudiation | Index rebuilds are deterministic from workspace content |
| Denial of Service | Not a network-reachable surface |
| Elevation of Privilege | No independent authorization role |
| Abuse | No distinct feature-misuse vector beyond the Information Disclosure threat |

---

## Mailbox

**Trust Boundary:** DaemonProcess
**Role:** File-based inter-agent message queue for swarms; root now `fsguard`-hardened, processed messages now `0o600`
**Data Flows:** DF28

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 2 threats identified for this component.* | — | — | — | — | — |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T24.S | Spoofing | Mailbox messages carry no authentication/signature, so any local account able to write an agent's inbox directory can inject peer/steering/shutdown messages | Host/OS Access | DF28 | `0o700` `teams/` tree plus the new `fsguard.RestrictToOwner` root hardening (`internal/swarm/mailbox.go:73-95`, P27.11) narrows this to same-user access; no message-level auth/signature added | Open | Existing |
| T24.T | Tampering | Processed message files were previously written world-readable (`0o644`) and the mailbox tree was not `fsguard`-hardened | Host/OS Access | DF28 | `hardenMailboxRoot()` applies `fsguard.RestrictToOwner` on every `OpenMailbox` call, and processed messages are now written `0o600` instead of `0o644` (P27.11, FIND-20) | Mitigated | Fixed |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Repudiation | Messages are file-timestamped |
| Information Disclosure | Covered by the Tampering threat above (world-readable fix) |
| Denial of Service | Bounded by SwarmCoordinator's fan-out limits |
| Elevation of Privilege | No independent authorization role |
| Abuse | Covered by the Spoofing threat above |

---

## AnthropicAPI

**Trust Boundary:** External
**Role:** Anthropic Messages API endpoint
**Data Flows:** DF31

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T25.S | Spoofing | A man-in-the-middle impersonates the provider endpoint | Internal Network | DF31 | HTTPS with default certificate verification; `InsecureSkipVerify` never used | Mitigated | Existing |
| T25.I | Information Disclosure | File/conversation content is disclosed to the external provider | Internal Network | DF31 | Inherent to cloud model use; `security.redact_secrets` now default-on; local Ollama avoids egress | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | External service, no independent write surface from Aegis |
| Repudiation | Not applicable to a third-party API |
| Denial of Service | Not Aegis's control surface |
| Elevation of Privilege | Not Aegis's control surface |
| Abuse | Covered by the Information Disclosure threat above |

---

## OpenAICompatibleEndpoint

**Trust Boundary:** External
**Role:** OpenAI/Azure/Ollama (OpenAI-compat mode) chat-completions endpoint
**Data Flows:** DF32

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T26.S | Spoofing | A misconfigured or attacker-set `base_url` points at a rogue endpoint that harvests the key | Local Process Access | DF32 | Same partial `validateBaseURL` mitigation (HTTP-only) as T04.S/T05.S | Open | Existing |
| T26.I | Information Disclosure | Content is disclosed to the endpoint; the default Ollama URL uses plain HTTP on loopback | Local Process Access | DF32 | Local endpoints keep content on-host; cloud endpoints use HTTPS | Open | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | External service, no independent write surface from Aegis |
| Repudiation | Not applicable to a third-party endpoint |
| Denial of Service | Not Aegis's control surface |
| Elevation of Privilege | Not Aegis's control surface |
| Abuse | Covered by the Spoofing/Information Disclosure threats above |

---

## OllamaNativeEndpoint

**Trust Boundary:** External
**Role:** **[NEW]** Local Ollama server's native `/api/chat` endpoint (loopback, plain HTTP by default)
**Data Flows:** DF41

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T41.S | Spoofing | A misconfigured or attacker-set `base_url` points the native-Ollama adapter at a rogue endpoint | Local Process Access | DF41 | Same partial `validateBaseURL` mitigation (HTTP-only) as T04.S/T05.S/T33.S | Open | New |
| T41.I | Information Disclosure | The native endpoint uses plain HTTP by default | Local Process Access | DF41 | Local-only by default; content stays on-host absent a redirect | Mitigated | New |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Tampering | External service, no independent write surface from Aegis |
| Repudiation | Not applicable to a local model server |
| Denial of Service | Covered by OllamaAdapter's keep_alive/timeout controls |
| Elevation of Privilege | Not Aegis's control surface |
| Abuse | Covered by the Spoofing/Information Disclosure threats above |

---

## GitHubAPI

**Trust Boundary:** External
**Role:** GitHub API for PR/repository operations
**Data Flows:** DF33

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T27.I | Information Disclosure | Repository/PR content is disclosed to GitHub | Internal Network | DF33 | HTTPS transport; token-scoped access under the operator's own account | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | HTTPS + operator-scoped token |
| Tampering | External service, no independent write surface from Aegis |
| Repudiation | GitHub's own audit log covers this |
| Denial of Service | Not Aegis's control surface |
| Elevation of Privilege | Token-scoped to the operator's own permissions |
| Abuse | Covered by the Information Disclosure threat above |

---

## Internet

**Trust Boundary:** External
**Role:** Arbitrary web targets for `web_fetch`/`web_search`
**Data Flows:** DF34

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T28.T | Tampering | Fetched web content injects instructions into the model context | Local Process Access | DF34 | Content wrapped in untrusted-provenance markers | Mitigated | Existing |
| T28.I | Information Disclosure | A model-supplied fetch URL targets internal/loopback services (SSRF) | Local Process Access | DF34 | `ssrfSafeDialer` blocks private/loopback/link-local IPs with redirect validation | Mitigated | Existing |
| T28.A | Abuse | Indirect prompt injection in fetched content evades detection | Local Process Access | DF34 | `search.scan_output` now defaults true (P27.13); the `web_fetch` tool's own scan-on-fetch default was not independently re-confirmed in this pass (see Needs Verification) | Open | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | Web targets are not independently authenticated by Aegis |
| Repudiation | Fetches are logged per-turn |
| Denial of Service | Bounded by Engine's budget controls |
| Elevation of Privilege | No independent authorization role |

---

## MCPExternalServers

**Trust Boundary:** External
**Role:** Operator-configured external MCP tool servers
**Data Flows:** DF35

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T29.T | Tampering | A compromised external MCP server returns content that injects instructions | Local Process Access | DF35 | All MCP output always wrapped untrusted | Mitigated | Existing |
| T29.A | Abuse | A malicious MCP server's output previously evaded the opt-in injection scan | Local Process Access | DF35 | `mcp.servers[].scan_output` now defaults true (P27.13); output always marked untrusted regardless | Mitigated | Fixed |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — | — |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | Server identity is operator-configured, not independently authenticated |
| Repudiation | MCP calls are logged per-turn |
| Information Disclosure | Covered by MCPClient's own STRIDE section |
| Denial of Service | Bounded by Engine's budget controls |
| Elevation of Privilege | Covered by MCPClient's capability-default control |

---

## ContainerRuntime

**Trust Boundary:** External
**Role:** Docker/Podman/WSL/Apple container runtime backing the sandbox
**Data Flows:** DF36

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T30.I | Information Disclosure | A sandboxed command reaches the network to exfiltrate workspace data | Local Process Access | DF36 | Container runs `--network none` by default unless `sandbox.network` is enabled | Mitigated | Existing |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status | Change |
|----|----------|--------|---------------|---------------|------------|--------|--------|
| T30.E | Elevation of Privilege | Access to the Docker/Podman socket used by the container backend is root-equivalent on the host | Host/OS Access | DF36 | `--cap-drop=ALL`, `--security-opt=no-new-privileges`; documented socket-privilege notice | Open | Existing |

#### Categories Not Applicable
| Category | Justification |
|----------|---------------|
| Spoofing | Not Aegis's control surface |
| Tampering | Covered by the Information Disclosure threat above |
| Repudiation | Not Aegis's control surface |
| Denial of Service | Bounded by the multiscanner/container consolidation |
| Abuse | Covered by the Elevation of Privilege threat above |

---

## Arithmetic Verification

- Per-component totals sum to **90** concrete threats (S=10, T=18, R=1, I=23, D=5, E=16, A=17).
- Tier distribution: T1=0, T2=81, T3=9 (81 + 9 = 90).
- Every component in `0.1-architecture.md` (excluding the external actors Operator and ExternalHarness) has a STRIDE section here (36 sections).
- Of the 76 baseline threats, 0 were removed with a component; the remainder are either `Existing` (still present, unchanged assessment), `Fixed` (remediated by a specific cited code change), or carry an updated `Mitigated` rationale where the underlying control was strengthened. 14 threats are `New` in this report (3 new components fully contribute new threats, plus newly-discovered gaps in modified components).
