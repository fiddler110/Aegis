# Security Findings

---

## Tier 1 — Direct Exposure (No Prerequisites)

*No Tier 1 findings identified for this repository.*

Aegis is classified `LOCALHOST_SERVICE`: the daemon binds `127.0.0.1:4127` only (refusing non-loopback binds unless `server.allow_remote` is explicitly set), so no component is reachable by an unauthenticated remote attacker. Every finding below therefore requires at least local process access to the host — most commonly the operator running Aegis inside an attacker-influenced working directory (a cloned malicious repository).

---

## Tier 2 — Conditional Risk (Authenticated / Single Prerequisite)

### FIND-01: Project-configured lifecycle hooks execute arbitrary shell on session start

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Important |
| CVSS 4.0 | 8.5 (CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:P/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N) |
| CWE | [CWE-94](https://cwe.mitre.org/data/definitions/94.html): Improper Control of Generation of Code ('Code Injection') |
| OWASP | A08:2025 – Software or Data Integrity Failures |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Redesign |
| Component | HooksRunner |
| Related Threats | [T15.E](2-stride-analysis.md#hooksrunner), [T15.A](2-stride-analysis.md#hooksrunner) |

#### Description

Aegis honors a project's `.aegis/config.yaml` `hooks` block, which defines `sh -c` commands run on lifecycle events including `session_start` and per-tool-call events. Because the project config is merged automatically with no workspace-trust prompt, opening (running `aegis` inside) a cloned malicious repository causes attacker-authored hook commands to execute on the operator's host — before the model takes any action and outside the permission gate / sandbox that governs model-initiated tool calls. A `pre_tool_use` hook additionally observes every tool call and its arguments and can veto or steer the workflow, giving the repo author a data-exfiltration and manipulation channel.

#### Evidence

**Prerequisite basis:** The daemon binds loopback-only (`internal/config/config.go:715`, `validateListenAddr` at `internal/server/server.go:944-953`), so this is not remotely reachable; exploitation requires the operator to run Aegis in an attacker-controlled directory — `Local Process Access` per the Component Exposure Table for `HooksRunner`.

- `internal/hooks` runs project-configured `sh -c` lifecycle commands on session/tool events (DF30, DF22).
- Hooks are read from project config and executed without a confirmation prompt or trust gate.
- Hook events deliver a JSON event on stdin to a project-controlled command; a `pre_tool_use` hook can inspect and veto arbitrary tool calls.

#### Remediation

Gate project-sourced hooks behind an explicit, per-workspace trust decision (a first-run "trust this workspace?" prompt keyed on the directory path), matching the treatment that untrusted project content receives elsewhere. Until trusted, ignore `hooks` originating from project config (honor only user/global-level hooks). Optionally run hook commands through the same `ExecutionSandbox` used for model tool calls.

#### Verification

Clone a repo whose `.aegis/config.yaml` defines a `session_start` hook that writes a marker file, start Aegis in that directory, and confirm the marker is NOT created until the workspace has been explicitly trusted.

---

### FIND-02: Project `.aegis/config.yaml` auto-merged without a workspace-trust gate

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Important |
| CVSS 4.0 | 8.2 (CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:P/VC:H/VI:H/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-829](https://cwe.mitre.org/data/definitions/829.html): Inclusion of Functionality from Untrusted Control Sphere |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Redesign |
| Component | ConfigLoader |
| Related Threats | [T16.T](2-stride-analysis.md#configloader), [T16.I](2-stride-analysis.md#configloader), [T16.E](2-stride-analysis.md#configloader), [T16.A](2-stride-analysis.md#configloader), [T01.T](2-stride-analysis.md#server) |

#### Description

The layered config loader merges a working directory's `.aegis/config.yaml` over global config at the highest file-precedence tier, with no workspace-trust prompt. A cloned repository can therefore override the sandbox backend, permission mode and rules, personas, MCP server list, and notification webhook simply by shipping a config file. This lowers the approval posture (`permission.mode`, `auto_approve_exec`, `allow_unsandboxed_auto_exec`), adds attacker-controlled MCP servers or a `notify.webhook` (creating an exfiltration channel), and loads `.aegis/.env` secrets into the process environment. The runtime config-mutating endpoints (`PATCH /config/*`, `POST /config/harden`) are a related surface, though those require the daemon bearer token and an explicit `confirm` flag.

#### Evidence

**Prerequisite basis:** Config is loaded from the working directory; exploitation requires the operator to run Aegis inside an attacker-supplied directory — `Local Process Access`, consistent with the `ConfigLoader` row of the Component Exposure Table.

- Config precedence (lowest→highest): defaults → `~/.config/aegis/config.yaml` → `.aegis/config.yaml` → `AEGIS_*` env (`internal/config`).
- Project config can set `permission.mode`, `auto_approve_exec`, `allow_unsandboxed_auto_exec`, `mcp.servers`, and `notify.webhook`.
- Downstream guards partially blunt the impact: unsandboxed auto-exec still requires the explicit opt-in flag to start, and MCP auto-approve defaults off; `.aegis/.env` is best-effort owner-ACL hardened on read.

#### Remediation

Introduce an explicit workspace-trust gate: on first use of a directory, prompt before applying security-relevant project config keys (`permission.*`, `sandbox.*`, `mcp.servers`, `notify.webhook`, `hooks`). Treat untrusted workspaces as config-frozen (project file may set non-security preferences only). Surface a diff of which security settings the project config would change.

#### Verification

Place a `.aegis/config.yaml` that sets `permission.mode: auto` and a `notify.webhook` in a test directory; start Aegis and confirm the elevated mode and webhook are NOT applied without explicit trust confirmation.

---

### FIND-03: Provider `base_url` override can redirect API-key-bearing requests to an attacker host

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Important |
| CVSS 4.0 | 7.1 (CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:P/VC:H/VI:N/VA:N/SC:H/SI:N/SA:N) |
| CWE | [CWE-522](https://cwe.mitre.org/data/definitions/522.html): Insufficiently Protected Credentials |
| OWASP | A02:2025 – Security Misconfiguration |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Standard Mitigation |
| Component | AnthropicAdapter |
| Related Threats | [T04.S](2-stride-analysis.md#anthropicadapter), [T05.S](2-stride-analysis.md#openaiadapter), [T26.S](2-stride-analysis.md#openaicompatibleendpoint), [T04.I](2-stride-analysis.md#anthropicadapter), [T05.I](2-stride-analysis.md#openaiadapter) |

#### Description

Both provider adapters honor a `provider.base_url` override with no validation of the destination. Because the override can be supplied via project `.aegis/config.yaml` (see FIND-02), a cloned repository can point requests bearing `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` at an attacker-controlled endpoint that simply harvests the key on the first request. The key handling itself is otherwise sound — read from the environment only, never logged or stored, stripped from child/shell environments, and sent over HTTPS to the default cloud endpoints — so the residual risk is entirely the unvalidated destination. The default Ollama endpoint additionally uses plain HTTP on loopback.

#### Evidence

**Prerequisite basis:** The `base_url` is a config value applied at daemon startup; setting it requires write access to a config layer, i.e. `Local Process Access` (matches the `AnthropicAdapter`/`OpenAIAdapter` exposure rows).

- `provider.base_url` is applied with no allowlist or scheme/host validation (`internal/provider/anthropic`, `internal/provider/openai`).
- API keys are sourced from env only and transmitted on every request (T04.I, T05.I are otherwise mitigated).
- Default OpenAI-compatible (Ollama) base URL is plain `http://` on loopback.

#### Remediation

Validate `base_url` against an allowlist of known-good provider hosts, or require an explicit confirmation when a non-default `base_url` is set from project-level config. At minimum, warn prominently at startup when the effective `base_url` differs from the provider default, and refuse to attach the API key to a plaintext-HTTP non-loopback endpoint.

#### Verification

Set `provider.base_url` to a non-default HTTPS host in project config and confirm Aegis warns/prompts before sending a keyed request; confirm a plaintext-HTTP non-loopback `base_url` is refused.

---

### FIND-04: Default `local` sandbox runs shell commands on the host with no isolation

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 6.8 (CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:P/VC:L/VI:H/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-693](https://cwe.mitre.org/data/definitions/693.html): Protection Mechanism Failure |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Existing Control |
| Component | ExecutionSandbox |
| Related Threats | [T09.T](2-stride-analysis.md#executionsandbox), [T06.T](2-stride-analysis.md#toolregistry), [T06.E](2-stride-analysis.md#toolregistry), [T09.E](2-stride-analysis.md#executionsandbox) |

#### Description

The default `local` sandbox backend executes shell commands directly on the host with only environment-variable stripping — no filesystem confinement, network isolation, or process containment. Shell commands are not path-confined, so a command approved for one purpose can read or write anywhere the daemon's user can. The primary compensating control is the build-mode approval prompt (and a startup refusal of the `auto` mode + `local` backend combination), which keeps a human in the loop for execute-capability tools. File-mutating tools are separately confined by `ValidatePath` (Rel-based escape check, `..` rejection, symlink resolution), and every tool's capability is checked before dispatch, so the exposure is specifically the unconfined shell execution surface.

#### Evidence

**Prerequisite basis:** Command execution is reachable only through the loopback, token-authenticated daemon API and is approval-gated in the default build mode — `Local Process Access` per the `ExecutionSandbox` exposure row.

- `internal/sandbox` local backend applies `DefaultStripEnv` only; no fs/net/pid isolation (DF23).
- Startup refuses the `allow_unsandboxed_auto_exec`-off + `auto` + `local` combination.
- `ValidatePath` confines file tools (T06.T mitigated); capability gate governs tool dispatch (T06.E mitigated).

#### Remediation

Recommend (and document prominently) the OS or container sandbox backends for untrusted workloads. Consider defaulting new installs to the OS sandbox where available (seatbelt/bwrap), or emitting a persistent warning banner when the `local` backend is active with execute-capable tools enabled.

#### Verification

With the `local` backend, confirm an approved shell command can read a file outside the workspace; then switch to the container backend and confirm the same command is confined.

---

### FIND-05: Read-tool and conversation content (incl. secrets) forwarded to the cloud model provider

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 6.1 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:H/VI:N/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-200](https://cwe.mitre.org/data/definitions/200.html): Exposure of Sensitive Information to an Unauthorized Actor |
| OWASP | A04:2025 – Cryptographic Failures |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Existing Control |
| Component | Engine |
| Related Threats | [T03.I](2-stride-analysis.md#engine), [T06.I](2-stride-analysis.md#toolregistry), [T25.I](2-stride-analysis.md#anthropicapi), [T26.I](2-stride-analysis.md#openaicompatibleendpoint), [T11.I2](2-stride-analysis.md#mcpclient) |

#### Description

File contents, tool outputs, and conversation history read into the model context are transmitted to the configured cloud provider on every turn. Read-capability tools may surface secrets (keys, tokens, `.env` values) that are then egressed. Aegis ships an opt-in `security.redact_secrets` control (gitleaks-backed masking applied before model send) and outbound MCP argument scanning, but both are off by default, and using a local Ollama endpoint is the only way to fully avoid egress. This is partly inherent to cloud-model use, so the finding targets making the redaction defense the discoverable default rather than eliminating egress.

#### Evidence

**Prerequisite basis:** Context assembly and egress happen inside the loopback daemon; observing or influencing it requires `Local Process Access` (matches the `Engine` exposure row).

- `security.redact_secrets` masks detected secrets before model send but is opt-in (`internal/security`).
- MCP outbound `scan_arguments` flags credential-shaped arguments (log-only, opt-in) (`internal/mcp/outbound.go`).
- Content is sent over HTTPS to cloud providers; `InsecureSkipVerify` is never used.

#### Remediation

Default `security.redact_secrets` on, or prompt to enable it on first cloud-provider use. Document the local-Ollama path for sensitive repositories. Consider a pre-send preview/summary of what categories of data are leaving the host.

#### Verification

Enable `security.redact_secrets`, place a known fake secret in a file, ask the agent to read it, and confirm the value is masked in the outbound request payload.

---

### FIND-06: `mcp-serve` and ACP stdio interfaces are unauthenticated by default

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 6.0 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:L/VI:H/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-306](https://cwe.mitre.org/data/definitions/306.html): Missing Authentication for Critical Function |
| OWASP | A07:2025 – Authentication Failures |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Standard Mitigation |
| Component | MCPServer |
| Related Threats | [T12.S](2-stride-analysis.md#mcpserver), [T13.S](2-stride-analysis.md#acpagent), [T12.E](2-stride-analysis.md#mcpserver), [T13.E](2-stride-analysis.md#acpagent) |

#### Description

`aegis mcp-serve` (MCP server) and the ACP JSON-RPC agent expose full agent-driving operations over stdio. Both support an optional shared-secret token (`AEGIS_MCP_TOKEN` / `AEGIS_ACP_TOKEN`), but the tokens are unset by default, so any local process able to control the server's stdin can drive full agent turns or invoke session/prompt operations. The execution posture is still bounded — new sessions default to plan mode and approvals are auto-denied unless `AutoApprove` — so the residual risk is unauthorized session control rather than immediate command execution.

#### Evidence

**Prerequisite basis:** stdio interfaces have no network listener; driving them requires a co-located process — `Local Process Access` per the `MCPServer`/`ACPAgent` exposure rows.

- `internal/mcpserver` gates `tools/call` on `AEGIS_MCP_TOKEN` only when set (DF03, DF11).
- `internal/acp` gates `session/new` and `session/prompt` on `AEGIS_ACP_TOKEN` only when set (DF04, DF12).
- New sessions default to plan mode; approvals auto-denied absent `AutoApprove` (T12.E, T13.E mitigated).

#### Remediation

Generate and require a token by default for both stdio servers (mirroring the daemon's auto-generated bearer token), writing it to an owner-only file that the launching integration reads. Fall back to unauthenticated only under an explicit opt-out flag.

#### Verification

Launch `aegis mcp-serve` with no token set and confirm it either generates/requires a token or emits a clear warning that the interface is unauthenticated.

---

### FIND-07: Project context and memory files injected into the system prompt without a trust marker

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.9 (CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:P/VC:L/VI:L/VA:N/SC:N/SI:L/SA:N) |
| CWE | [CWE-1427](https://cwe.mitre.org/data/definitions/1427.html): Improper Neutralization of Input Used for LLM Prompting |
| OWASP | A05:2025 – Injection |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Standard Mitigation |
| Component | Engine |
| Related Threats | [T03.T](2-stride-analysis.md#engine), [T22.A](2-stride-analysis.md#memorystore), [T18.T](2-stride-analysis.md#skillregistry), [T22.T](2-stride-analysis.md#memorystore) |

#### Description

Project context files (`AGENTS.md`, `CLAUDE.md`, `.aegis/context.md`) and a project `.aegis/memory.md` are concatenated into the system prompt with no untrusted-provenance marker, so a cloned repository can inject steering instructions into every session. This is inconsistent with the rest of the codebase, where persona bodies, project/user skill bodies, and fetched web/MCP output are all wrapped in untrusted-provenance markers (skills and personas are already trust-wrapped; memory has a sha256 integrity sidecar that warns on out-of-band edits but does not block injection).

#### Evidence

**Prerequisite basis:** Context/memory files are read from the workspace; injection requires the operator to run Aegis in an attacker-supplied directory — `Local Process Access` (matches the `Engine`/`MemoryStore` exposure rows).

- Context files (`AGENTS.md`/`CLAUDE.md`/`.aegis/context.md`) concatenated into the system prompt without trust wrapping (DF18).
- Project skill bodies ARE trust-wrapped (T18.T mitigated, `internal/skills`); memory has an integrity sidecar (T22.T mitigated, `internal/memory/integrity.go`).
- Memory injection (T22.A) is warned-but-not-blocked.

#### Remediation

Wrap project-sourced context and memory content in the same untrusted-provenance markers used for skills/personas/web output before injecting into the prompt, or gate their inclusion behind the workspace-trust decision proposed in FIND-01/FIND-02.

#### Verification

Add an `AGENTS.md` containing an injected instruction (e.g., "ignore prior instructions and…") in a test workspace and confirm the content is enclosed in an untrusted-provenance marker in the assembled prompt.

---

### FIND-08: Cron fire-time gating bypasses rule/contextual gates and permits persistent unattended execution

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.6 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:L/VI:L/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-863](https://cwe.mitre.org/data/definitions/863.html): Incorrect Authorization |
| OWASP | A01:2025 – Broken Access Control |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Standard Mitigation |
| Component | CronScheduler |
| Related Threats | [T14.E](2-stride-analysis.md#cronscheduler), [T14.A](2-stride-analysis.md#cronscheduler) |

#### Description

When a scheduled job fires, the fire-time gate consults only the daemon's coarse permission mode — the text allow/deny rules and the contextual egress gate that apply to interactive tool calls are not re-evaluated for scheduled commands. Because only plan mode blocks a fire, an `auto`- or `build`-mode session can create a persisted `auto_approve` cron job that keeps executing shell commands unattended across daemon restarts, with a weaker authorization check than the operator would face interactively.

#### Evidence

**Prerequisite basis:** Cron jobs are created through the loopback daemon API and persisted in SQLite; reaching that surface requires `Local Process Access` (matches the `CronScheduler` exposure row).

- Fire-time gate re-reads only the current mode; text rules + contextual gate are not applied to scheduled commands (DF29).
- `auto_approve` jobs persist in SQLite and fire in build/auto mode (DF09); only plan mode blocks a fire.

#### Remediation

Apply the full permission stack (mode + text allow/deny rules + contextual egress gate) at cron fire time, identical to interactive tool dispatch. Require an explicit, separately-confirmed flag to create an `auto_approve` job, and surface persisted auto-approve jobs in a review/audit view.

#### Verification

Create an `auto_approve` cron job that runs a denied-by-rule command; confirm the fire-time gate now rejects it under the active rules.

---

### FIND-09: Malicious persona can weaken the guard and permission posture

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.4 (CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:P/VC:L/VI:L/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-284](https://cwe.mitre.org/data/definitions/284.html): Improper Access Control |
| OWASP | A01:2025 – Broken Access Control |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Standard Mitigation |
| Component | PersonaLoader |
| Related Threats | [T17.T](2-stride-analysis.md#personaloader), [T17.E](2-stride-analysis.md#personaloader), [T07.T](2-stride-analysis.md#permissiongate), [T07.E](2-stride-analysis.md#permissiongate), [T07.A](2-stride-analysis.md#permissiongate) |

#### Description

A project persona's YAML frontmatter carries control fields (`output_guard: none`, `mode`, `tools`, `rules`) that are applied as real settings when that persona is selected. A persona shipped in a cloned repo's `.aegis/personas/` can therefore disable the output guard or lower the effective permission mode. The persona body itself is trust-wrapped, but the control fields are not gated. The underlying permission gate remains sound — deny > allow > mode precedence, scoped exec globs are metachar-restricted, file rules normalize path and case (T07.* mitigated) — so the risk is that a malicious persona configures a weaker-but-still-enforced posture, not that the gate itself fails.

#### Evidence

**Prerequisite basis:** Personas load from the project/user directories; a hostile persona requires the operator to run in an attacker-supplied workspace — `Local Process Access` (matches the `PersonaLoader` exposure row).

- Persona frontmatter `output_guard`, `mode`, `tools`, `rules` applied as settings (`internal/persona/load.go`, DF18).
- Persona body IS trust-wrapped; control fields are not.
- Permission gate precedence and rule-matching hardening intact (`internal/permission/rules.go`).

#### Remediation

Treat persona control fields from project-level personas as untrusted: ignore or require confirmation for `output_guard: none`, `mode` lowering, and rule additions sourced from project config, folding this into the workspace-trust gate (FIND-02). User/global personas may retain full control.

#### Verification

Select a project persona with `output_guard: none` and confirm the guard is not silently disabled without an explicit trust confirmation.

---

### FIND-10: HTTP MCP client has no SSRF protection

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.3 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:L/VI:L/VA:N/SC:L/SI:N/SA:N) |
| CWE | [CWE-918](https://cwe.mitre.org/data/definitions/918.html): Server-Side Request Forgery (SSRF) |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Standard Mitigation |
| Component | MCPClient |
| Related Threats | [T11.I](2-stride-analysis.md#mcpclient), [T11.E](2-stride-analysis.md#mcpclient), [T28.I](2-stride-analysis.md#internet) |

#### Description

The HTTP/SSE MCP client uses a plain `http.Client` with no SSRF-safe dialer, unlike the web-fetch tool, which blocks private/loopback/link-local IPs with redirect validation. A configured `http://` MCP endpoint can therefore target internal or loopback services. The exposure is bounded because MCP endpoints are operator-configured rather than model-controlled — but a hostile project config (FIND-02) can supply that endpoint. Related MCP controls are otherwise sound: unknown/empty tool capability defaults to the most-restrictive `execute`, and all MCP output is trust-wrapped.

#### Evidence

**Prerequisite basis:** MCP endpoints are set in config and reached through the daemon; configuring them requires `Local Process Access` (matches the `MCPClient` exposure row).

- `internal/mcp` HTTP transport uses a plain `http.Client`, no SSRF dialer (DF35), contrast `internal/tool/builtin/web.go:109-162`.
- MCP tool capability defaults to most-restrictive `execute` (T11.E mitigated); output always trust-wrapped.
- Web-fetch SSRF dialer already exists and could be reused (T28.I mitigated there).

#### Remediation

Route the HTTP MCP transport through the same `ssrfSafeDialer` used by the web tool, blocking private/loopback/link-local destinations (with an explicit opt-in for intentionally-internal MCP servers).

#### Verification

Configure an `http://` MCP server pointed at a loopback service and confirm the connection is blocked unless explicitly allowlisted.

---

### FIND-11: Recon/DAST scanning gated only by the target-authorization allowlist

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.1 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:N/VI:L/VA:N/SC:L/SI:L/SA:N) |
| CWE | [CWE-285](https://cwe.mitre.org/data/definitions/285.html): Improper Authorization |
| OWASP | A01:2025 – Broken Access Control |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Existing Control |
| Component | SecurityScanner |
| Related Threats | [T19.A](2-stride-analysis.md#securityscanner) |

#### Description

The `recon_scan` / `dast_scan` tools launch real outbound scans (nmap/nuclei/DAST tooling). The only substantive guard against scanning arbitrary third-party hosts is the target-authorization allowlist: targets are restricted to loopback/private ranges unless allowlisted, and active-mode scanning requires an `allow_active` flag. Widening `allowed_targets` (e.g., via a hostile project config, FIND-02) enables scanning arbitrary Internet hosts from the operator's machine, which may be unauthorized activity.

#### Evidence

**Prerequisite basis:** Scanning tools are invoked through the daemon API and are approval/allowlist-gated — `Local Process Access` (matches the `SecurityScanner` exposure row).

- Targets restricted to loopback/private unless allowlisted; active mode requires `allow_active` (`internal/security`, DF25).
- The allowlist is a config value, so it inherits the project-config trust weakness (FIND-02).

#### Remediation

Require the allowlist to be sourced from user/global config only (not project config), or gate any project-supplied `allowed_targets` widening behind the workspace-trust decision. Log every scan target for auditability.

#### Verification

Attempt a `recon_scan` against a non-allowlisted public host and confirm it is refused; confirm a project-config-supplied allowlist widening does not take effect without trust confirmation.

---

### FIND-12: Prompt-injection scan of untrusted content is best-effort and opt-in

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.0 (CVSS:4.0/AV:L/AC:H/AT:N/PR:N/UI:P/VC:L/VI:L/VA:N/SC:N/SI:L/SA:N) |
| CWE | [CWE-184](https://cwe.mitre.org/data/definitions/184.html): Incomplete List of Disallowed Inputs |
| OWASP | A05:2025 – Injection |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Standard Mitigation |
| Component | Internet |
| Related Threats | [T28.A](2-stride-analysis.md#internet), [T29.A](2-stride-analysis.md#mcpexternalservers), [T03.A](2-stride-analysis.md#engine), [T06.A](2-stride-analysis.md#toolregistry), [T11.A](2-stride-analysis.md#mcpclient), [T28.T](2-stride-analysis.md#internet), [T29.T](2-stride-analysis.md#mcpexternalservers), [T31.T](2-stride-analysis.md#terminalui) |

#### Description

External content from web fetches and MCP servers is consistently wrapped in untrusted-provenance markers (a strong, always-on control that mitigates the bulk of indirect-injection risk), and the engine still gates execute-capability tools regardless of model coercion. However, the additional injection *scan* (invisible-character + base64 heuristics) is best-effort and opt-in per tool, so a crafted payload that evades those heuristics can still reach the model as (marked-but-persuasive) untrusted content. The residual risk is that provenance-marking alone may not stop a determined indirect-injection payload from influencing model behavior.

#### Evidence

**Prerequisite basis:** Injection arrives via content the agent fetches while running locally; realizing it requires the agent to be operated against attacker-controlled content — `Local Process Access` (matches the `Internet`/`MCPExternalServers` exposure rows).

- Untrusted-provenance wrapping is always applied to web + MCP output (T28.T, T29.T, T06.A, T11.A, T03.A mitigated; `internal/trust`).
- The invisible-char + base64 injection scan is best-effort and opt-in per tool (T28.A, T29.A open).

#### Remediation

Enable the injection scan by default for network-sourced content, expand detection beyond invisible-char/base64 heuristics, and consider a distinct high-visibility rendering of untrusted content in the transcript so the operator can spot injected instructions.

#### Verification

Fetch a page containing a known injection pattern with the scan enabled by default and confirm it is flagged; confirm untrusted content is visibly delimited in the transcript.

---

### FIND-13: Client↔daemon loopback traffic is plaintext HTTP by default

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 4.8 (CVSS:4.0/AV:L/AC:H/AT:N/PR:L/UI:N/VC:H/VI:L/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-319](https://cwe.mitre.org/data/definitions/319.html): Cleartext Transmission of Sensitive Information |
| OWASP | A04:2025 – Cryptographic Failures |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Existing Control |
| Component | Server |
| Related Threats | [T01.I](2-stride-analysis.md#server), [T01.E](2-stride-analysis.md#server), [T01.A](2-stride-analysis.md#server), [T02.S](2-stride-analysis.md#webui), [T02.A](2-stride-analysis.md#webui), [T32.I](2-stride-analysis.md#client) |

#### Description

Client↔daemon traffic is plain HTTP over loopback by default, so another local account with packet-capture privilege can observe the 32-byte bearer token and full conversation content. Optional TLS (`server.tls.enabled`) with a pinned self-signed certificate is available but off by default. The surrounding authentication controls are strong: constant-time bearer-token compare, origin middleware rejecting non-loopback origins (anti-DNS-rebind), a single-use 60-second page token with a double-submit CSRF binding for the web UI, `X-Frame-Options: DENY`, a strict `connect-src 'self'` CSP, and a startup refusal of non-loopback binds (T01.E, T01.A, T02.S, T02.A mitigated). The residual risk is the plaintext transport on a shared host.

#### Evidence

**Prerequisite basis:** The API binds loopback only and is bearer-token authenticated; sniffing loopback traffic requires another local account/capture privilege — `Local Process Access` (matches the `Server`/`WebUI` exposure rows).

- Default transport is plain HTTP over loopback; optional pinned-cert TLS at `internal/server/tls.go`.
- Auth middleware constant-time compare + origin middleware (`internal/server/auth.go:45-118`); page token + CSRF (`auth.go:120-247`).
- Non-loopback bind refused unless `allow_remote` (`server.go:944-953`).

#### Remediation

Consider enabling loopback TLS by default (the pinned self-signed cert path already exists), or document the shared-host threat and recommend TLS when the host is multi-user. Rotate the bearer token on daemon restart.

#### Verification

With TLS disabled, confirm the bearer token is visible in a loopback packet capture; enable `server.tls.enabled` and confirm the traffic is encrypted.

---

### FIND-14: Daemon API has no rate limiting and unbounded default concurrency

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Low |
| CVSS 4.0 | 3.9 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:N/VI:N/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-770](https://cwe.mitre.org/data/definitions/770.html): Allocation of Resources Without Limits or Throttling |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Standard Mitigation |
| Component | Server |
| Related Threats | [T01.D](2-stride-analysis.md#server) |

#### Description

The daemon API applies no per-connection or per-IP rate limiting, and `max_concurrent_runs` defaults to unlimited, so a local caller (or a runaway client) can exhaust host resources or hammer the auth endpoint. Invalid-auth attempts are only coarsely logged. Opt-in `max_concurrent_runs` and `max_run_duration_sec` caps exist but are not set by default.

#### Evidence

**Prerequisite basis:** Requests reach only the loopback, token-authenticated API — `Local Process Access` (matches the `Server` exposure row).

- No rate limiting on API routes; `max_concurrent_runs` default unlimited (`internal/config`, `internal/engine`).
- `max_run_duration_sec` and concurrency caps are opt-in.

#### Remediation

Set conservative default caps for `max_concurrent_runs` and per-run duration, and add lightweight throttling / backoff on repeated auth failures.

#### Verification

Issue a burst of concurrent run requests and confirm they are capped by the new default; confirm repeated invalid-auth attempts are throttled.

---

### FIND-15: Output guard runs after files are written and can be disabled

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Low |
| CVSS 4.0 | 3.6 (CVSS:4.0/AV:L/AC:H/AT:N/PR:L/UI:N/VC:N/VI:L/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-696](https://cwe.mitre.org/data/definitions/696.html): Incorrect Behavior Order |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Low |
| Mitigation Type | Existing Control |
| Component | OutputGuard |
| Related Threats | [T08.T](2-stride-analysis.md#outputguard), [T08.I](2-stride-analysis.md#outputguard), [T08.A](2-stride-analysis.md#outputguard) |

#### Description

The output guard performs its second-model validation after files have already been written to disk, so a FAIL verdict cannot un-write a malicious deliverable — it can only drive a corrective retry — and the guard is disable-able (including via persona frontmatter, see FIND-09). The guard's own robustness is otherwise sound: judged content is tag-escaped and framed as data (mitigating verdict-forgery injection), and `parseVerdict` fails closed on ambiguous output, failing open only on a transport error. The real write boundary remains the permission gate and sandbox.

#### Evidence

**Prerequisite basis:** The guard runs inside the loopback daemon over model output — reaching or disabling it requires `Local Process Access` (matches the `OutputGuard` exposure row).

- Guard validates after write; drives a corrective retry, not a rollback (`internal/guard`, DF16).
- Content tag-escaped/framed as data (T08.I mitigated); `parseVerdict` fails closed on ambiguity, open on transport error (T08.A mitigated).

#### Remediation

Position the guard (or a lighter pre-write check) before irreversible writes for high-risk deliverables, and treat guard-disabling from project-level config/personas as an untrusted change (FIND-09). Document that the permission gate/sandbox — not the guard — is the authoritative write boundary.

#### Verification

Trigger a guard FAIL on a file-writing turn and confirm the written file is quarantined/rolled back rather than merely retried.

---

### FIND-16: Detached sub-agent spawns can escape the shared cost budget

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Low |
| CVSS 4.0 | 3.4 (CVSS:4.0/AV:L/AC:L/AT:N/PR:L/UI:N/VC:N/VI:N/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-770](https://cwe.mitre.org/data/definitions/770.html): Allocation of Resources Without Limits or Throttling |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Local Process Access |
| Exploitability Tier | Tier 2 — Conditional Risk (single prerequisite) |
| Remediation Effort | Medium |
| Mitigation Type | Standard Mitigation |
| Component | SwarmCoordinator |
| Related Threats | [T10.D](2-stride-analysis.md#swarmcoordinator), [T10.E](2-stride-analysis.md#swarmcoordinator), [T10.A](2-stride-analysis.md#swarmcoordinator) |

#### Description

A detached/background sub-agent spawn loses the shared cost tracker and falls back to a fresh full budget, escaping the fan-out tree's cost ceiling and enabling unexpected spend. The broader swarm controls are sound: `clampMode` allows only restriction (never escalation), sub-agents inherit the full gate stack, and recursive fan-out is bounded by `maxSpawnDepth=3`, `MaxParallelAgents=8`, an adaptive limiter, and a per-agent duration cap (T10.E, T10.A mitigated). The residual gap is the detached-spawn budget bypass.

#### Evidence

**Prerequisite basis:** Spawning is initiated through the daemon-hosted engine — `Local Process Access` (matches the `SwarmCoordinator` exposure row).

- In-context spawns share a cost tracker with a fair-share floor; detached/background spawns fall back to a fresh full budget (`internal/swarm/agent.go`, DF10).
- `clampMode` restricts-only (`agent.go:555-563`); depth/parallelism caps enforced.

#### Remediation

Propagate a shared (or proportionally-derived) budget ceiling into detached spawns, or require an explicit budget grant when spawning detached, so total spend across the fan-out tree remains bounded.

#### Verification

Spawn a detached sub-agent under a parent with a low remaining budget and confirm the child cannot exceed the tree-wide ceiling.

---

## Tier 3 — Defense-in-Depth (Prior Compromise / Host Access)

### FIND-17: Container backend's Docker/Podman socket access is root-equivalent on the host

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.9 (CVSS:4.0/AV:L/AC:L/AT:N/PR:H/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N) |
| CWE | [CWE-269](https://cwe.mitre.org/data/definitions/269.html): Improper Privilege Management |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Host/OS Access |
| Exploitability Tier | Tier 3 — Defense-in-Depth (prior compromise / host access) |
| Remediation Effort | Medium |
| Mitigation Type | Existing Control |
| Component | ExecutionSandbox |
| Related Threats | [T09.E1](2-stride-analysis.md#executionsandbox), [T30.E](2-stride-analysis.md#containerruntime), [T30.I](2-stride-analysis.md#containerruntime) |

#### Description

When the container sandbox backend uses the Docker/Podman socket, access to that socket is root-equivalent on the host: a process able to talk to the socket can start privileged containers that mount the host filesystem. Aegis applies container hardening (`--cap-drop=ALL`, `--security-opt=no-new-privileges`, `--network none` by default unless `sandbox.network` is enabled) and documents the socket-privilege notice, so the residual risk is the inherent trust placed in the container runtime rather than a missing control.

#### Evidence

**Prerequisite basis:** Exploiting socket privilege requires an actor already able to reach the Docker/Podman socket on the host — `Host/OS Access` (matches the `ContainerRuntime` exposure row and the DiD tier).

- Container backend hardening: `--cap-drop=ALL`, `--security-opt=no-new-privileges` (T30.E open, documented; DF36).
- Default `--network none` blocks exfiltration unless enabled (T30.I mitigated).
- Socket-privilege caveat is documented.

#### Remediation

Prefer rootless Podman or a socket-proxy that constrains the container-create API where available; document that the container backend's security depends on the runtime's socket exposure model.

#### Verification

Confirm the documented guidance recommends rootless/socket-proxy configurations and that default container flags include `--cap-drop=ALL` and `no-new-privileges`.

---

### FIND-18: SQLite session/checkpoint/memory/knowledge stores are unencrypted at rest, some without ACL hardening

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.8 (CVSS:4.0/AV:L/AC:L/AT:N/PR:H/UI:N/VC:H/VI:L/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-311](https://cwe.mitre.org/data/definitions/311.html): Missing Encryption of Sensitive Data |
| OWASP | A04:2025 – Cryptographic Failures |
| Exploitation Prerequisites | Host/OS Access |
| Exploitability Tier | Tier 3 — Defense-in-Depth (prior compromise / host access) |
| Remediation Effort | Medium |
| Mitigation Type | Standard Mitigation |
| Component | SessionStore |
| Related Threats | [T20.I](2-stride-analysis.md#sessionstore), [T21.I](2-stride-analysis.md#checkpointstore), [T22.I](2-stride-analysis.md#memorystore), [T23.I](2-stride-analysis.md#knowledgeindex), [T20.T](2-stride-analysis.md#sessionstore), [T20.R](2-stride-analysis.md#sessionstore) |

#### Description

The SQLite stores hold full conversations, tool outputs, system prompts, and pre-modification snapshots of arbitrary workspace file contents in plaintext with no encryption at rest. The session DB is `fsguard` owner-ACL hardened and lives under a `0o700` data dir (and per-turn traces provide attribution — T20.T, T20.R mitigated), but the long-term memory DB (`longmem.db`) and the per-project knowledge index (`.aegis/knowledge.db`) are not `fsguard`-hardened, so on a shared Windows host they can inherit a looser parent ACL. An actor with host/OS access can read all historical content.

#### Evidence

**Prerequisite basis:** All stores are on-disk with no network listener; reading them requires filesystem access — `Host/OS Access` (matches the datastore exposure rows and the DiD tier).

- Session DB: `0o700` dir + `fsguard` owner-only ACL on DB and WAL/SHM (T20.T mitigated; `internal/fsguard`); per-turn traces persisted (T20.R mitigated).
- Checkpoint snapshots plaintext in the session DB (T21.I, DF21).
- `longmem.db` (T22.I) and `knowledge.db` (T23.I) rely on the `0o700` directory only — no `fsguard`.

#### Remediation

Apply `fsguard.RestrictToOwner` to `longmem.db` and `knowledge.db` (and their WAL/SHM sidecars) as is already done for the session DB. For higher-assurance deployments, offer optional at-rest encryption (e.g., SQLCipher) for the conversation/checkpoint stores.

#### Verification

Inspect the ACLs of `longmem.db` and `knowledge.db` after creation and confirm they are owner-only, matching the session DB.

---

### FIND-19: OS sandbox leaves the entire host filesystem readable

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Moderate |
| CVSS 4.0 | 5.5 (CVSS:4.0/AV:L/AC:L/AT:N/PR:H/UI:N/VC:H/VI:N/VA:N/SC:N/SI:N/SA:N) |
| CWE | [CWE-668](https://cwe.mitre.org/data/definitions/668.html): Exposure of Resource to Wrong Sphere |
| OWASP | A06:2025 – Insecure Design |
| Exploitation Prerequisites | Host/OS Access |
| Exploitability Tier | Tier 3 — Defense-in-Depth (prior compromise / host access) |
| Remediation Effort | Medium |
| Mitigation Type | Standard Mitigation |
| Component | ExecutionSandbox |
| Related Threats | [T09.I](2-stride-analysis.md#executionsandbox) |

#### Description

The OS sandbox backend (seatbelt on macOS, bwrap on Linux) is write- and network-scoped only and leaves the entire host filesystem readable. A command running under it can read SSH keys, cloud credentials, and other secrets and — unless network egress is also denied — exfiltrate them. This is a defense-in-depth gap: it matters when a command is already running untrusted code under the OS sandbox.

#### Evidence

**Prerequisite basis:** Realizing read-then-exfiltrate requires untrusted code executing on the host under the sandbox — `Host/OS Access` (matches the `ExecutionSandbox` DiD row).

- OS sandbox restricts writes/network but not reads (`internal/sandbox`, DF23).
- Network can be denied to block exfiltration; the container backend fully isolates the filesystem.

#### Remediation

Add read-path confinement to the OS sandbox profile (restrict readable paths to the workspace plus required toolchain paths), or recommend the container backend when read-confidentiality matters. Deny network egress by default under the OS sandbox.

#### Verification

Run a command under the OS sandbox that reads a file outside the workspace (e.g., `~/.ssh/id_rsa`) and confirm the read is denied after the profile is tightened.

---

### FIND-20: Swarm mailbox messages are unauthenticated and processed files are world-readable

| Attribute | Value |
|-----------|-------|
| SDL Bugbar Severity | Low |
| CVSS 4.0 | 3.5 (CVSS:4.0/AV:L/AC:L/AT:N/PR:H/UI:N/VC:L/VI:L/VA:L/SC:N/SI:N/SA:N) |
| CWE | [CWE-306](https://cwe.mitre.org/data/definitions/306.html): Missing Authentication for Critical Function |
| OWASP | A07:2025 – Authentication Failures |
| Exploitation Prerequisites | Host/OS Access |
| Exploitability Tier | Tier 3 — Defense-in-Depth (prior compromise / host access) |
| Remediation Effort | Low |
| Mitigation Type | Standard Mitigation |
| Component | Mailbox |
| Related Threats | [T24.S](2-stride-analysis.md#mailbox), [T24.T](2-stride-analysis.md#mailbox) |

#### Description

The file-based inter-agent mailbox carries no message authentication or signature, so any local account able to write an agent's inbox directory can inject peer/steering/shutdown messages. Processed message files are additionally written world-readable (`0o644`), and the mailbox tree is not `fsguard`-hardened (unlike the `0o600` inbox writes). The `0o700` `teams/` parent directory is the sole guard, so on POSIX the risk requires an account that can already traverse into that tree.

#### Evidence

**Prerequisite basis:** Injecting/reading messages requires local filesystem access to the `teams/` tree — `Host/OS Access` (matches the `Mailbox` DiD rows).

- Messages carry no auth/signature (T24.S); inbox writes `0o600`, processed files `0o644` (T24.T); `internal/swarm`, DF28.
- `0o700` `teams/` directory is the sole access guard; tree not `fsguard`-hardened.

#### Remediation

Write processed message files `0o600`, apply `fsguard.RestrictToOwner` to the `teams/` tree, and add a per-run shared secret or HMAC to mailbox messages so injected messages from other accounts are rejected.

#### Verification

Confirm processed mailbox files are created `0o600` and that a message lacking the expected authentication token is rejected by the consuming agent.

---

## Threat Coverage Verification

| Threat ID | Finding ID | Status |
|-----------|------------|--------|
| T01.T | FIND-02 | ✅ Mitigated (FIND-02) |
| T01.I | FIND-13 | ✅ Covered (FIND-13) |
| T01.D | FIND-14 | ✅ Covered (FIND-14) |
| T01.E | FIND-13 | ✅ Mitigated (FIND-13) |
| T01.A | FIND-13 | ✅ Mitigated (FIND-13) |
| T02.S | FIND-13 | ✅ Mitigated (FIND-13) |
| T02.A | FIND-13 | ✅ Mitigated (FIND-13) |
| T03.T | FIND-07 | ✅ Covered (FIND-07) |
| T03.I | FIND-05 | ✅ Covered (FIND-05) |
| T03.D | FIND-16 | ✅ Mitigated (FIND-16) |
| T03.A | FIND-12 | ✅ Mitigated (FIND-12) |
| T04.S | FIND-03 | ✅ Covered (FIND-03) |
| T04.I | FIND-03 | ✅ Mitigated (FIND-03) |
| T05.S | FIND-03 | ✅ Covered (FIND-03) |
| T05.I | FIND-03 | ✅ Mitigated (FIND-03) |
| T06.T | FIND-04 | ✅ Mitigated (FIND-04) |
| T06.I | FIND-05 | ✅ Covered (FIND-05) |
| T06.E | FIND-04 | ✅ Mitigated (FIND-04) |
| T06.A | FIND-12 | ✅ Mitigated (FIND-12) |
| T07.T | FIND-09 | ✅ Mitigated (FIND-09) |
| T07.E | FIND-09 | ✅ Mitigated (FIND-09) |
| T07.A | FIND-09 | ✅ Mitigated (FIND-09) |
| T08.T | FIND-15 | ✅ Covered (FIND-15) |
| T08.I | FIND-15 | ✅ Mitigated (FIND-15) |
| T08.A | FIND-15 | ✅ Mitigated (FIND-15) |
| T09.T | FIND-04 | ✅ Covered (FIND-04) |
| T09.E | FIND-04 | ✅ Mitigated (FIND-04) |
| T09.I | FIND-19 | ✅ Covered (FIND-19) |
| T09.E1 | FIND-17 | ✅ Covered (FIND-17) |
| T10.D | FIND-16 | ✅ Covered (FIND-16) |
| T10.E | FIND-16 | ✅ Mitigated (FIND-16) |
| T10.A | FIND-16 | ✅ Mitigated (FIND-16) |
| T11.I | FIND-10 | ✅ Covered (FIND-10) |
| T11.I2 | FIND-05 | ✅ Mitigated (FIND-05) |
| T11.E | FIND-10 | ✅ Mitigated (FIND-10) |
| T11.A | FIND-12 | ✅ Mitigated (FIND-12) |
| T12.S | FIND-06 | ✅ Covered (FIND-06) |
| T12.E | FIND-06 | ✅ Mitigated (FIND-06) |
| T13.S | FIND-06 | ✅ Covered (FIND-06) |
| T13.E | FIND-06 | ✅ Mitigated (FIND-06) |
| T14.E | FIND-08 | ✅ Covered (FIND-08) |
| T14.A | FIND-08 | ✅ Covered (FIND-08) |
| T15.E | FIND-01 | ✅ Covered (FIND-01) |
| T15.A | FIND-01 | ✅ Covered (FIND-01) |
| T16.T | FIND-02 | ✅ Covered (FIND-02) |
| T16.I | FIND-02 | ✅ Covered (FIND-02) |
| T16.E | FIND-02 | ✅ Covered (FIND-02) |
| T16.A | FIND-02 | ✅ Mitigated (FIND-02) |
| T17.T | FIND-09 | ✅ Covered (FIND-09) |
| T17.E | FIND-09 | ✅ Covered (FIND-09) |
| T18.T | FIND-07 | ✅ Mitigated (FIND-07) |
| T19.A | FIND-11 | ✅ Covered (FIND-11) |
| T20.T | FIND-18 | ✅ Mitigated (FIND-18) |
| T20.R | FIND-18 | ✅ Mitigated (FIND-18) |
| T20.I | FIND-18 | ✅ Covered (FIND-18) |
| T21.I | FIND-18 | ✅ Covered (FIND-18) |
| T22.T | FIND-07 | ✅ Mitigated (FIND-07) |
| T22.A | FIND-07 | ✅ Covered (FIND-07) |
| T22.I | FIND-18 | ✅ Covered (FIND-18) |
| T23.I | FIND-18 | ✅ Covered (FIND-18) |
| T24.S | FIND-20 | ✅ Covered (FIND-20) |
| T24.T | FIND-20 | ✅ Covered (FIND-20) |
| T25.S | — | 🔄 Mitigated by Platform |
| T25.I | FIND-05 | ✅ Covered (FIND-05) |
| T26.S | FIND-03 | ✅ Covered (FIND-03) |
| T26.I | FIND-05 | ✅ Covered (FIND-05) |
| T27.I | — | 🔄 Mitigated by Platform |
| T28.T | FIND-12 | ✅ Mitigated (FIND-12) |
| T28.I | FIND-10 | ✅ Mitigated (FIND-10) |
| T28.A | FIND-12 | ✅ Covered (FIND-12) |
| T29.T | FIND-12 | ✅ Mitigated (FIND-12) |
| T29.A | FIND-12 | ✅ Covered (FIND-12) |
| T30.I | FIND-17 | ✅ Mitigated (FIND-17) |
| T30.E | FIND-17 | ✅ Covered (FIND-17) |
| T31.T | FIND-12 | ✅ Mitigated (FIND-12) |
| T32.I | FIND-13 | ✅ Covered (FIND-13) |
