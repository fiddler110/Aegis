# STRIDE + Abuse Cases — Threat Analysis

## Exploitability Tiers

Threats are classified into three exploitability tiers based on the prerequisites an attacker needs:

| Tier | Label | Prerequisites | Assignment Rule |
|------|-------|---------------|----------------|
| **Tier 1** | Direct Exposure | `None` | Exploitable by unauthenticated external attacker with NO prior access. The prerequisite field MUST say `None`. |
| **Tier 2** | Conditional Risk | Single prerequisite: `Authenticated User`, `Privileged User`, `Internal Network`, or single `{Boundary} Access` | Requires exactly ONE form of access. The prerequisite field has ONE item. |
| **Tier 3** | Defense-in-Depth | `Host/OS Access`, `Admin Credentials`, `{Component} Compromise`, `Physical Access`, or MULTIPLE prerequisites joined with `+` | Requires significant prior breach, infrastructure access, or multiple combined prerequisites. |

Aegis is classified `LOCALHOST_SERVICE` (the daemon binds loopback-only by default), so there are **no Tier 1 threats**: every threat requires at least local process access to the host, and the dominant precondition is that the operator runs Aegis inside an attacker-influenced working directory (a cloned malicious repository) or that another local account/process is present on the host.

## Summary

| Component | Link | S | T | R | I | D | E | A | Total | T1 | T2 | T3 | Risk |
|-----------|------|---|---|---|---|---|---|---|-------|----|----|----|------|
| TerminalUI | [link](#terminalui) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Client | [link](#client) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Server | [link](#server) | 0 | 1 | 0 | 1 | 1 | 1 | 1 | 5 | 0 | 5 | 0 | Elevated |
| WebUI | [link](#webui) | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Low |
| Engine | [link](#engine) | 0 | 1 | 0 | 1 | 1 | 0 | 1 | 4 | 0 | 4 | 0 | Elevated |
| AnthropicAdapter | [link](#anthropicadapter) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Elevated |
| OpenAIAdapter | [link](#openaiadapter) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Elevated |
| ToolRegistry | [link](#toolregistry) | 0 | 1 | 0 | 1 | 0 | 1 | 1 | 4 | 0 | 4 | 0 | Elevated |
| PermissionGate | [link](#permissiongate) | 0 | 1 | 0 | 0 | 0 | 1 | 1 | 3 | 0 | 3 | 0 | Moderate |
| OutputGuard | [link](#outputguard) | 0 | 1 | 0 | 1 | 0 | 0 | 1 | 3 | 0 | 3 | 0 | Moderate |
| ExecutionSandbox | [link](#executionsandbox) | 0 | 1 | 0 | 1 | 0 | 2 | 0 | 4 | 0 | 2 | 2 | Elevated |
| SwarmCoordinator | [link](#swarmcoordinator) | 0 | 0 | 0 | 0 | 1 | 1 | 1 | 3 | 0 | 3 | 0 | Moderate |
| MCPClient | [link](#mcpclient) | 0 | 0 | 0 | 2 | 0 | 1 | 1 | 4 | 0 | 4 | 0 | Moderate |
| MCPServer | [link](#mcpserver) | 1 | 0 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Low |
| ACPAgent | [link](#acpagent) | 1 | 0 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Low |
| CronScheduler | [link](#cronscheduler) | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 2 | 0 | 2 | 0 | Elevated |
| HooksRunner | [link](#hooksrunner) | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 2 | 0 | 2 | 0 | Elevated |
| ConfigLoader | [link](#configloader) | 0 | 1 | 0 | 1 | 0 | 1 | 1 | 4 | 0 | 4 | 0 | Elevated |
| PersonaLoader | [link](#personaloader) | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 2 | 0 | 2 | 0 | Moderate |
| SkillRegistry | [link](#skillregistry) | 0 | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| SecurityScanner | [link](#securityscanner) | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 1 | 0 | 1 | 0 | Moderate |
| SessionStore | [link](#sessionstore) | 0 | 1 | 1 | 1 | 0 | 0 | 0 | 3 | 0 | 2 | 1 | Moderate |
| CheckpointStore | [link](#checkpointstore) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Low |
| MemoryStore | [link](#memorystore) | 0 | 1 | 0 | 1 | 0 | 0 | 1 | 3 | 0 | 2 | 1 | Moderate |
| KnowledgeIndex | [link](#knowledgeindex) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 0 | 1 | Low |
| Mailbox | [link](#mailbox) | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 2 | 0 | 0 | 2 | Low |
| AnthropicAPI | [link](#anthropicapi) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Moderate |
| OpenAICompatibleEndpoint | [link](#openaicompatibleendpoint) | 1 | 0 | 0 | 1 | 0 | 0 | 0 | 2 | 0 | 2 | 0 | Moderate |
| GitHubAPI | [link](#githubapi) | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 1 | 0 | 1 | 0 | Low |
| Internet | [link](#internet) | 0 | 1 | 0 | 1 | 0 | 0 | 1 | 3 | 0 | 3 | 0 | Moderate |
| MCPExternalServers | [link](#mcpexternalservers) | 0 | 1 | 0 | 0 | 0 | 0 | 1 | 2 | 0 | 2 | 0 | Moderate |
| ContainerRuntime | [link](#containerruntime) | 0 | 0 | 0 | 1 | 0 | 1 | 0 | 2 | 0 | 1 | 1 | Low |
| **Total** | | **8** | **15** | **1** | **20** | **3** | **14** | **15** | **76** | **0** | **67** | **9** | |

---

## TerminalUI

**Trust Boundary:** ClientProcess
**Role:** Bubbletea terminal UI that renders the timeline, streaming output, dialogs, and slash commands
**Data Flows:** DF01, DF05

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T31.T | Tampering | Untrusted model/tool output rendered in the terminal could carry escape sequences that spoof the UI or manipulate the operator's terminal | Local Process Access | DF01 | Output rendered through Bubbletea/lipgloss; external tool output is provenance-marked before display | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## Client

**Trust Boundary:** ClientProcess
**Role:** HTTP+SSE client that connects the UI to the daemon and carries the bearer token
**Data Flows:** DF05, DF06

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T32.I | Information Disclosure | The client holds the daemon bearer token in memory and transmits it over plain loopback HTTP by default, observable by another local packet-capturing account | Local Process Access | DF06 | Token file owner-ACL hardened; optional loopback TLS (`server.tls.enabled`) | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## Server

**Trust Boundary:** DaemonProcess
**Role:** Daemon HTTP API; owns sessions, config-admin endpoints, web UI, and auth middleware
**Data Flows:** DF02, DF06, DF07, DF08, DF09, DF10

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified — loopback-only bind, bearer-token auth.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T01.T | Tampering | Config-mutating endpoints (`PATCH /config/*`, `POST /config/harden`) let a bearer-token holder alter sandbox/security posture at runtime | Local Process Access | DF06 | Requires the daemon bearer token; harden/install require an explicit `confirm` flag | Mitigated |
| T01.I | Information Disclosure | Client↔daemon traffic is plain HTTP over loopback by default, so another local account with packet-capture privilege can observe the bearer token and full conversation content | Local Process Access | DF06 | Optional TLS (`server.tls.enabled`) with a pinned self-signed cert | Open |
| T01.D | Denial of Service | The API has no per-IP/connection rate limiting and `max_concurrent_runs` defaults to unlimited, so a local caller can exhaust host resources or hammer auth | Local Process Access | DF06 | Coarse invalid-auth logging; `max_concurrent_runs`/`max_run_duration_sec` opt-in | Open |
| T01.E | Elevation of Privilege | Binding a non-loopback address exposes the token-protected API to the network | Internal Network | DF06 | `validateListenAddr` refuses non-loopback bind unless `server.allow_remote` is explicitly set | Mitigated |
| T01.A | Abuse | A hostile local process or webpage drives the `/ui` auth flow to obtain the real daemon token | Local Process Access | DF02 | Bearer-token auth + origin middleware (anti-DNS-rebind) + single-use page token with double-submit CSRF | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## WebUI

**Trust Boundary:** DaemonProcess
**Role:** Embedded Preact single-page app served at `/ui` on the daemon's loopback port
**Data Flows:** DF02

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T02.S | Spoofing | A cross-origin page tries to impersonate the operator's browser and steal the daemon token | Local Process Access | DF02 | Single-use page token, HttpOnly SameSite=Strict CSRF cookie, no `Access-Control-Allow-Origin`, `X-Frame-Options: DENY` | Mitigated |
| T02.A | Abuse | DNS-rebinding a malicious site onto the loopback origin to call the API | Local Process Access | DF02 | `originMiddleware` rejects non-loopback `Origin`; strict CSP `connect-src 'self'` | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## Engine

**Trust Boundary:** DaemonProcess
**Role:** Core agent loop — model calls, tool dispatch, budget/loop enforcement, guard integration
**Data Flows:** DF07, DF13–DF22

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T03.T | Tampering | Project context files (`AGENTS.md`, `CLAUDE.md`, `.aegis/context.md`) are concatenated into the system prompt with no untrusted-provenance marker, letting a cloned repo inject instructions | Local Process Access | DF18 | Personas/skills are trust-wrapped; context files are not | Open |
| T03.I | Information Disclosure | File/conversation content read into context is forwarded to the configured cloud model provider | Local Process Access | DF13, DF14 | Opt-in `security.redact_secrets` (gitleaks); local Ollama avoids egress | Open |
| T03.D | Denial of Service | A runaway tool-call loop or unbounded token spend exhausts cost/resources | Local Process Access | DF07 | Budget (`budget_usd`/`max_tokens_per_run`) and loop-detector abort the run | Mitigated |
| T03.A | Abuse | Prompt-injection embedded in tool output coerces the model into unintended tool calls | Local Process Access | DF17 | Untrusted-content wrapping + injection scan; execute tools still gated | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## AnthropicAdapter

**Trust Boundary:** DaemonProcess
**Role:** Normalizes the Anthropic Messages API to the provider seam
**Data Flows:** DF13, DF31

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T04.S | Spoofing | A project config `provider.base_url` override redirects requests (bearing `ANTHROPIC_API_KEY`) to an attacker-controlled host | Local Process Access | DF31 | None on the base_url value itself | Open |
| T04.I | Information Disclosure | The API key is transmitted on every request | Local Process Access | DF31 | Key read only from env; never logged/stored; stripped from child/shell env; HTTPS default | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## OpenAIAdapter

**Trust Boundary:** DaemonProcess
**Role:** Normalizes OpenAI/Azure/Ollama chat-completions to the provider seam
**Data Flows:** DF14, DF32

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T05.S | Spoofing | A project config `provider.base_url` override redirects requests (bearing `OPENAI_API_KEY`) to an attacker host; the default Ollama endpoint is plain HTTP loopback | Local Process Access | DF32 | None on the base_url value itself | Open |
| T05.I | Information Disclosure | The API key is transmitted to the configured endpoint | Local Process Access | DF32 | Key from env only; never logged/stored; HTTPS for cloud endpoints | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## ToolRegistry

**Trust Boundary:** DaemonProcess
**Role:** Tool dispatch with capability-based gating
**Data Flows:** DF17, DF23–DF27, DF33, DF34

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T06.T | Tampering | File-mutating tools could write outside the workspace via path traversal | Local Process Access | DF23 | `ValidatePath` with `Rel`-based escape check, `..` rejection, and symlink resolution incl. new files | Mitigated |
| T06.I | Information Disclosure | Read-capability tool output may contain secrets that are then forwarded to the model provider | Local Process Access | DF17 | Opt-in `security.redact_secrets` masks detected secrets before model send | Open |
| T06.E | Elevation of Privilege | A tool call could run at a higher capability than intended | Local Process Access | DF17 | Every tool declares a capability consulted by the permission gate before execution | Mitigated |
| T06.A | Abuse | Untrusted MCP/web tool output is treated as trusted instructions | Local Process Access | DF24, DF34 | All external tool output wrapped in untrusted-provenance markers | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## PermissionGate

**Trust Boundary:** DaemonProcess
**Role:** Mode + rule + contextual authorization of every tool call
**Data Flows:** DF15

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T07.T | Tampering | A scoped allow rule is widened via shell chaining or a path-traversal variant | Local Process Access | DF15 | Execute allow rules use metachar-restricted globs; file rules normalize path + case | Mitigated |
| T07.E | Elevation of Privilege | A tool call escalates past the intended mode | Local Process Access | DF15 | plan denies write/execute and gates network; build gates execute; deny > allow > mode precedence | Mitigated |
| T07.A | Abuse | A scoped rule on a tool lacking a recognized subject field silently never matches, giving false confidence | Local Process Access | DF15 | `WarnUnmatchableRules` logs such no-op rules at startup | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## OutputGuard

**Trust Boundary:** DaemonProcess
**Role:** Second-model validation of final answers and written files
**Data Flows:** DF16

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T08.T | Tampering | The guard runs after files are already written to disk, so a FAIL verdict cannot un-write a malicious deliverable; the guard is also disable-able | Local Process Access | DF16 | Guard drives a corrective retry; permission gate/sandbox remain the write boundary | Open |
| T08.I | Information Disclosure | Untrusted content re-entering the guard's judge model could inject a forged verdict | Local Process Access | DF16 | Content tag-escaped and framed as data in the judge prompt | Mitigated |
| T08.A | Abuse | A successful-but-ambiguous guard response is treated as a pass | Local Process Access | DF16 | `parseVerdict` fails closed on ambiguous output; fails open only on transport error | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## ExecutionSandbox

**Trust Boundary:** DaemonProcess
**Role:** Pluggable command execution isolation (local/OS/container)
**Data Flows:** DF23, DF29, DF30, DF36

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T09.T | Tampering | The default `local` backend runs shell commands directly on the host with no filesystem/network/process isolation; shell commands are not path-confined | Local Process Access | DF23 | Approval required in build mode; env-key stripping; startup refusal of auto-approve + local | Open |
| T09.E | Elevation of Privilege | The API key is never passed into the container and is stripped from the shell environment | Local Process Access | DF23 | `DefaultStripEnv` + no `-e` key injection into containers | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T09.I | Information Disclosure | The OS sandbox (seatbelt/bwrap) is write/network-only and leaves the entire host filesystem readable, enabling read-then-exfiltrate of SSH keys/credentials unless network is also denied | Host/OS Access | DF23 | Network can be denied to block exfiltration; container backend fully isolates | Open |
| T09.E1 | Elevation of Privilege | When the container backend uses the Docker/Podman socket, socket access is root-equivalent on the host | Host/OS Access | DF36 | `--cap-drop=ALL`, `--security-opt=no-new-privileges`; documented socket-privilege notice | Open |

---

## SwarmCoordinator

**Trust Boundary:** DaemonProcess
**Role:** Spawns sub-agents as goroutines/subprocesses with mode clamping
**Data Flows:** DF10, DF28

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T10.D | Denial of Service | A detached/background sub-agent spawn loses the shared cost tracker and falls back to a fresh full budget, escaping the fan-out tree's ceiling | Local Process Access | DF10 | Shared tracker for in-context spawns with a fair-share floor | Open |
| T10.E | Elevation of Privilege | A sub-agent requests a more permissive mode than its parent | Local Process Access | DF10 | `clampMode` allows only restriction; sub-agents inherit the full gate stack | Mitigated |
| T10.A | Abuse | Uncontrolled recursive fan-out exhausts host resources | Local Process Access | DF10 | `maxSpawnDepth=3`, `MaxParallelAgents=8`, adaptive limiter, per-agent duration cap | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## MCPClient

**Trust Boundary:** DaemonProcess
**Role:** Outbound MCP client (stdio + HTTP/SSE) to external tool servers
**Data Flows:** DF24, DF35

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T11.I | Information Disclosure | The HTTP MCP client uses a plain `http.Client` with no SSRF dialer, so a configured `http://` endpoint can target internal/loopback services | Local Process Access | DF35 | Endpoints are operator-configured, not model-controlled | Open |
| T11.I2 | Information Disclosure | Sensitive data could be exfiltrated inside outbound tool-call arguments to an untrusted server | Local Process Access | DF35 | Opt-in `scan_arguments` flags credential-shaped arguments (log-only) | Mitigated |
| T11.E | Elevation of Privilege | An unlabeled MCP server's tools default to an over-permissive capability | Local Process Access | DF24 | Unknown/empty capability defaults to the most-restrictive `execute` (Ask in build, Deny in plan) | Mitigated |
| T11.A | Abuse | External MCP server output is treated as trusted instructions | Local Process Access | DF24 | All MCP output always wrapped untrusted + optional injection scan | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## MCPServer

**Trust Boundary:** DaemonProcess
**Role:** `aegis mcp-serve` — exposes Aegis sessions as MCP tools over stdio
**Data Flows:** DF03, DF11

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T12.S | Spoofing | With `AEGIS_MCP_TOKEN` unset (default), any local process controlling the server's stdin can drive full agent turns | Local Process Access | DF03 | Optional shared-secret token gates `tools/call` | Open |
| T12.E | Elevation of Privilege | A driving harness escalates a session's execution posture | Local Process Access | DF11 | New sessions default to plan mode; approvals auto-denied unless `AutoApprove` | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## ACPAgent

**Trust Boundary:** DaemonProcess
**Role:** ACP JSON-RPC server over stdio for editor integrations
**Data Flows:** DF04, DF12

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T13.S | Spoofing | With `AEGIS_ACP_TOKEN` unset (default), any local process controlling stdin can invoke session/prompt operations | Local Process Access | DF04 | Optional shared-secret token gates `session/new` and `session/prompt` | Open |
| T13.E | Elevation of Privilege | An editor integration escalates the session's execution posture | Local Process Access | DF12 | Session posture governed by the same permission gate stack | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## CronScheduler

**Trust Boundary:** DaemonProcess
**Role:** Persistent scheduler for background shell/agent tasks
**Data Flows:** DF09, DF29

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T14.E | Elevation of Privilege | Cron fire-time gating consults only the coarse mode policy — the text allow/deny rules and contextual egress gate are bypassed for scheduled commands | Local Process Access | DF29 | Fire-time gate re-reads the daemon's current mode | Open |
| T14.A | Abuse | An auto-mode session can create a persistent `auto_approve` cron job that keeps executing shell commands unattended in auto and build mode across restarts | Local Process Access | DF09 | Only plan mode blocks fire; jobs persisted in SQLite | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## HooksRunner

**Trust Boundary:** DaemonProcess
**Role:** Runs project-configured `sh -c` lifecycle hooks on tool/session events
**Data Flows:** DF22, DF30

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T15.E | Elevation of Privilege | Hooks configured in a project `.aegis/config.yaml` run arbitrary `sh -c` commands on `session_start` and tool events, giving a cloned malicious repo host code execution before the model even acts | Local Process Access | DF30 | None specific to hooks; hooks are honored from project config without confirmation | Open |
| T15.A | Abuse | A `pre_tool_use` hook can observe/veto every tool call and its arguments, exposing data or manipulating the workflow | Local Process Access | DF22 | Hook JSON event on stdin; project-controlled | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## ConfigLoader

**Trust Boundary:** DaemonProcess
**Role:** Layered config loader including project `.aegis/config.yaml` and `.env`
**Data Flows:** DF08

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T16.T | Tampering | The project `.aegis/config.yaml` in the working directory is auto-merged over global config with no workspace-trust gate, overriding sandbox backend, permission rules, personas, and more | Local Process Access | DF08 | Some downstream guards (unsandboxed-auto-exec refusal, MCP auto-approve default off) | Open |
| T16.I | Information Disclosure | Project config can add attacker-controlled MCP servers or a `notify.webhook`, creating exfiltration channels | Local Process Access | DF08 | None at load time | Open |
| T16.E | Elevation of Privilege | Project config can set `permission.mode`, `auto_approve_exec`, or `allow_unsandboxed_auto_exec`, lowering the approval posture | Local Process Access | DF08 | Unsandboxed auto-exec still requires the explicit opt-in flag to start | Open |
| T16.A | Abuse | `.aegis/.env` secrets are loaded into the process environment | Local Process Access | DF08 | `.env` hardened with owner-only ACL on read (best-effort) | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## PersonaLoader

**Trust Boundary:** DaemonProcess
**Role:** Loads and hot-reloads persona system prompts from project/user directories
**Data Flows:** DF18

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T17.T | Tampering | A project persona's frontmatter can set `output_guard: none`, `mode`, `tools`, and `rules`, weakening the guard/permission posture when that persona is selected | Local Process Access | DF18 | Persona body is trust-wrapped, but control fields are applied as real settings | Open |
| T17.E | Elevation of Privilege | A selected malicious persona lowers the effective permission mode for the session | Local Process Access | DF18 | Same permission gate still enforces the (possibly lowered) mode | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## SkillRegistry

**Trust Boundary:** DaemonProcess
**Role:** Progressive-disclosure skills from project/user/embedded sources
**Data Flows:** DF19

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T18.T | Tampering | A project skill body could inject instructions into the model context | Local Process Access | DF19 | Project/user skill bodies wrapped in untrusted-provenance markers; only embedded builtins are trusted | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## SecurityScanner

**Trust Boundary:** DaemonProcess
**Role:** Runs SAST/secret/DAST/recon scanners host or container
**Data Flows:** DF25

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T19.A | Abuse | The `recon_scan`/`dast_scan` tools make real outbound scans (nmap/nuclei/DAST) whose only real guard is the target-authorization allowlist, so widening `allowed_targets` enables scanning arbitrary hosts | Local Process Access | DF25 | Targets restricted to loopback/private unless allowlisted; active mode requires `allow_active` | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## SessionStore

**Trust Boundary:** DaemonProcess
**Role:** SQLite store of full conversations, traces, cost, and cron jobs
**Data Flows:** DF20

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T20.T | Tampering | Another local account tampers with the session database files | Local Process Access | DF20 | `0o700` data dir + `fsguard` owner-only ACL on the DB and WAL/SHM sidecars | Mitigated |
| T20.R | Repudiation | Agent actions cannot be attributed after the fact | Local Process Access | DF20 | Per-turn traces (tokens, cost, tool names/durations) persisted for attribution | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T20.I | Information Disclosure | The session DB stores full conversations, tool outputs, and system prompts in plaintext with no encryption at rest | Host/OS Access | DF20 | Owner-only filesystem ACL is the sole confidentiality control | Open |

---

## CheckpointStore

**Trust Boundary:** DaemonProcess
**Role:** Per-turn file-content snapshots for `/rewind`, stored in the session DB
**Data Flows:** DF21

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 2 threats identified for this component.* | — | — | — | — |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T21.I | Information Disclosure | Pre-modification snapshots of arbitrary workspace file contents are stored plaintext in the session DB | Host/OS Access | DF21 | Shares the session DB's owner-only ACL; no encryption at rest | Open |

---

## MemoryStore

**Trust Boundary:** DaemonProcess
**Role:** Project/user memory files plus a long-term memory database
**Data Flows:** DF26

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T22.T | Tampering | A memory file is hand-edited outside Aegis to plant false "learned" context | Local Process Access | DF26 | sha256 integrity sidecar prepends a tamper warning on out-of-band edits | Mitigated |
| T22.A | Abuse | A project `.aegis/memory.md` planted by a cloned repo is injected into every session's system prompt | Local Process Access | DF26 | Integrity sidecar warns on tamper but does not block injection | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T22.I | Information Disclosure | The long-term memory DB (`longmem.db`) holding conversation-derived content is not owner-ACL-hardened, so on a shared Windows host it can inherit a looser parent ACL | Host/OS Access | DF26 | `0o700` data dir only; no `fsguard` on this DB | Open |

---

## KnowledgeIndex

**Trust Boundary:** DaemonProcess
**Role:** Per-project FTS index of file contents at `.aegis/knowledge.db`
**Data Flows:** DF27

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 2 threats identified for this component.* | — | — | — | — |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T23.I | Information Disclosure | The knowledge index DB (project file contents) is not owner-ACL-hardened, inheriting only the `0o700` directory on Windows | Host/OS Access | DF27 | Directory permissions only; no `fsguard` on this DB | Open |

---

## Mailbox

**Trust Boundary:** DaemonProcess
**Role:** File-based inter-agent message queue for swarms
**Data Flows:** DF28

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 2 threats identified for this component.* | — | — | — | — |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T24.S | Spoofing | Mailbox messages carry no authentication/signature, so any local account able to write an agent's inbox directory can inject peer/steering/shutdown messages | Host/OS Access | DF28 | `0o700` `teams/` tree is the sole guard | Open |
| T24.T | Tampering | Processed message files are written world-readable (`0o644`) and the mailbox tree is not `fsguard`-hardened, unlike the inbox `0o600` writes | Host/OS Access | DF28 | `0o700` parent directory still gates POSIX access | Open |

---

## AnthropicAPI

**Trust Boundary:** External
**Role:** Anthropic Messages API endpoint
**Data Flows:** DF31

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T25.S | Spoofing | A man-in-the-middle impersonates the provider endpoint | Internal Network | DF31 | HTTPS with default certificate verification; `InsecureSkipVerify` never used | Mitigated |
| T25.I | Information Disclosure | File/conversation content is disclosed to the external provider | Internal Network | DF31 | Inherent to cloud model use; opt-in redaction; local Ollama avoids egress | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## OpenAICompatibleEndpoint

**Trust Boundary:** External
**Role:** OpenAI/Azure/Ollama chat-completions endpoint
**Data Flows:** DF32

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T26.S | Spoofing | A misconfigured or attacker-set `base_url` points at a rogue endpoint that harvests the key | Local Process Access | DF32 | None on the base_url value itself | Open |
| T26.I | Information Disclosure | Content is disclosed to the endpoint; the default Ollama URL uses plain HTTP on loopback | Local Process Access | DF32 | Local endpoints keep content on-host; cloud endpoints use HTTPS | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## GitHubAPI

**Trust Boundary:** External
**Role:** GitHub API for PR/repository operations
**Data Flows:** DF33

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T27.I | Information Disclosure | Repository/PR content is disclosed to GitHub | Internal Network | DF33 | HTTPS transport; token-scoped access under the operator's own account | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## Internet

**Trust Boundary:** External
**Role:** Arbitrary web targets for `web_fetch`/`web_search`
**Data Flows:** DF34

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T28.T | Tampering | Fetched web content injects instructions into the model context | Local Process Access | DF34 | Content wrapped in untrusted-provenance markers | Mitigated |
| T28.I | Information Disclosure | A model-supplied fetch URL targets internal/loopback services (SSRF) | Local Process Access | DF34 | `ssrfSafeDialer` blocks private/loopback/link-local IPs with redirect validation | Mitigated |
| T28.A | Abuse | Indirect prompt injection in fetched content evades detection | Local Process Access | DF34 | Injection scan (invisible-char + base64) is best-effort and opt-in per tool | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## MCPExternalServers

**Trust Boundary:** External
**Role:** Operator-configured external MCP tool servers
**Data Flows:** DF35

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T29.T | Tampering | A compromised external MCP server returns content that injects instructions | Local Process Access | DF35 | All MCP output always wrapped untrusted | Mitigated |
| T29.A | Abuse | A malicious MCP server evades the opt-in injection scan, and its operator-declared capability label is trusted | Local Process Access | DF35 | Output always marked untrusted; capability defaults to most-restrictive | Open |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 3 threats identified for this component.* | — | — | — | — |

---

## ContainerRuntime

**Trust Boundary:** External
**Role:** Docker/Podman/WSL/Apple container runtime backing the sandbox
**Data Flows:** DF36

### STRIDE-A Analysis

#### Tier 1 — Direct Exposure (No Prerequisites)
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| — | — | *No Tier 1 threats identified for this component.* | — | — | — | — |

#### Tier 2 — Conditional Risk
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T30.I | Information Disclosure | A sandboxed command reaches the network to exfiltrate workspace data | Local Process Access | DF36 | Container runs `--network none` by default unless `sandbox.network` is enabled | Mitigated |

#### Tier 3 — Defense-in-Depth
| ID | Category | Threat | Prerequisites | Affected Flow | Mitigation | Status |
|----|----------|--------|---------------|---------------|------------|--------|
| T30.E | Elevation of Privilege | Access to the Docker/Podman socket used by the container backend is root-equivalent on the host | Host/OS Access | DF36 | `--cap-drop=ALL`, `--security-opt=no-new-privileges`; documented socket-privilege notice | Open |

---

## Arithmetic Verification

- Per-component totals sum to **76** concrete threats (S=8, T=15, R=1, I=20, D=3, E=14, A=15).
- Tier distribution: T1=0, T2=67, T3=9 (67 + 9 = 76).
- Every component in `0.1-architecture.md` (excluding the external actors Operator and ExternalHarness) has a STRIDE section here (32 sections).
