# Security Assessment

---

## Report Files

| File | Description |
|------|-------------|
| [0-assessment.md](0-assessment.md) | This document — executive summary, risk rating, action plan, metadata |
| [0.1-architecture.md](0.1-architecture.md) | Architecture overview, components, scenarios, tech stack |
| [1-threatmodel.md](1-threatmodel.md) | Threat model DFD diagram with element, flow, and boundary tables |
| [1.1-threatmodel.mmd](1.1-threatmodel.mmd) | Pure Mermaid DFD source file |
| [1.2-threatmodel-summary.mmd](1.2-threatmodel-summary.mmd) | Summary DFD for large systems |
| [2-stride-analysis.md](2-stride-analysis.md) | Full STRIDE-A analysis for all components |
| [3-findings.md](3-findings.md) | Prioritized security findings with remediation |

---

## Executive Summary

Aegis is a local-first AI coding agent shipped as a single Go binary that runs as a loopback-bound daemon (`aegis serve`) with TUI/CLI/web clients, an OpenAI/Anthropic provider seam, 50+ capability-gated built-in tools, a pluggable execution sandbox, and multi-agent swarm coordination. It is designed for a single developer running it on their own workstation against cloud or local models. The security posture is unusually mature for its class: the daemon refuses non-loopback binds unless explicitly opted in, authenticates its API with a constant-time 32-byte bearer token, defends the web UI with an origin gate plus single-use page tokens and double-submit CSRF, wraps untrusted web/MCP content in provenance markers, ships an SSRF-safe web dialer, enforces a permission-mode + rule + capability model on every tool call, and hardens on-disk secrets with owner-only ACLs.

The dominant threat surface is therefore not remote attack but **untrusted local input**: because Aegis is a coding agent, the expected workflow is to run it inside a cloned repository, and several project-scoped inputs (`.aegis/config.yaml`, lifecycle `hooks`, personas, context/memory files) are applied automatically with no workspace-trust boundary. A hostile repository can consequently execute code via hooks, weaken the sandbox/permission posture via config, redirect API-key-bearing requests via a `base_url` override, or inject instructions into the model context. The secondary surface is confidentiality on a shared or multi-user host: loopback traffic is plaintext by default and several SQLite stores are unencrypted at rest (two without ACL hardening).

The analysis covers 34 system elements across 3 trust boundaries.

### Risk Rating: Elevated

The rating is Elevated rather than Critical because the deployment classification is `LOCALHOST_SERVICE`: no component is reachable by an unauthenticated remote attacker, so there are zero directly-exploitable (Tier 1) threats and every finding requires at least local process access to the host. The Elevated level is driven by the missing workspace-trust boundary — a cluster of Important-severity findings (hook-based code execution, unconditional project-config merge, `base_url` credential redirection) that turn the routine act of opening an untrusted repository into a code-execution / credential-theft vector. These are conditional on the operator running Aegis in an attacker-influenced directory, which is nonetheless the tool's normal mode of use.

> **Note on threat counts:** This analysis identified 76 threats across 32 analyzed components. This count reflects comprehensive STRIDE-A coverage, not systemic insecurity. Of these, **0 are directly exploitable** without prerequisites (Tier 1). The remaining 76 represent conditional risks and defense-in-depth considerations.

---

## Action Summary

| Tier | Description | Threats | Findings | Priority |
|------|-------------|---------|----------|----------|
| [Tier 1](3-findings.md#tier-1--direct-exposure-no-prerequisites) | Directly exploitable | 0 | 0 | 🔴 Critical Risk |
| [Tier 2](3-findings.md#tier-2--conditional-risk-authenticated--single-prerequisite) | Requires authenticated access | 67 | 16 | 🟠 Elevated Risk |
| [Tier 3](3-findings.md#tier-3--defense-in-depth-prior-compromise--host-access) | Requires prior compromise | 9 | 4 | 🟡 Moderate Risk |
| **Total** | | **76** | **20** | |

### Priority by Tier and CVSS Score (Top 10)

| Finding | Tier | CVSS Score | SDL Severity | Title |
|---------|------|------------|-------------|-------|
| [FIND-01](3-findings.md#find-01-project-configured-lifecycle-hooks-execute-arbitrary-shell-on-session-start) | T2 | 8.5 | Important | Project-configured lifecycle hooks execute arbitrary shell on session start |
| [FIND-02](3-findings.md#find-02-project-aegisconfigyaml-auto-merged-without-a-workspace-trust-gate) | T2 | 8.2 | Important | Project `.aegis/config.yaml` auto-merged without a workspace-trust gate |
| [FIND-03](3-findings.md#find-03-provider-base_url-override-can-redirect-api-key-bearing-requests-to-an-attacker-host) | T2 | 7.1 | Important | Provider `base_url` override can redirect API-key-bearing requests to an attacker host |
| [FIND-04](3-findings.md#find-04-default-local-sandbox-runs-shell-commands-on-the-host-with-no-isolation) | T2 | 6.8 | Moderate | Default `local` sandbox runs shell commands on the host with no isolation |
| [FIND-05](3-findings.md#find-05-read-tool-and-conversation-content-incl-secrets-forwarded-to-the-cloud-model-provider) | T2 | 6.1 | Moderate | Read-tool and conversation content forwarded to the cloud model provider |
| [FIND-06](3-findings.md#find-06-mcp-serve-and-acp-stdio-interfaces-are-unauthenticated-by-default) | T2 | 6.0 | Moderate | `mcp-serve` and ACP stdio interfaces are unauthenticated by default |
| [FIND-07](3-findings.md#find-07-project-context-and-memory-files-injected-into-the-system-prompt-without-a-trust-marker) | T2 | 5.9 | Moderate | Project context and memory files injected into the system prompt without a trust marker |
| [FIND-08](3-findings.md#find-08-cron-fire-time-gating-bypasses-rulecontextual-gates-and-permits-persistent-unattended-execution) | T2 | 5.6 | Moderate | Cron fire-time gating bypasses rule/contextual gates and permits persistent unattended execution |
| [FIND-09](3-findings.md#find-09-malicious-persona-can-weaken-the-guard-and-permission-posture) | T2 | 5.4 | Moderate | Malicious persona can weaken the guard and permission posture |
| [FIND-10](3-findings.md#find-10-http-mcp-client-has-no-ssrf-protection) | T2 | 5.3 | Moderate | HTTP MCP client has no SSRF protection |

### Quick Wins

*No Tier 1 findings were identified, so there are no zero-prerequisite quick wins. The following Tier 2 findings are `Low`-remediation-effort items that materially reduce the untrusted-repository and shared-host surface.*

| Finding | Title | Why Quick |
|---------|-------|-----------|
| [FIND-03](3-findings.md#find-03-provider-base_url-override-can-redirect-api-key-bearing-requests-to-an-attacker-host) | Provider `base_url` override can redirect API-key-bearing requests | Add an allowlist/warn on non-default `base_url`; small, localized change in the provider adapters |
| [FIND-06](3-findings.md#find-06-mcp-serve-and-acp-stdio-interfaces-are-unauthenticated-by-default) | `mcp-serve` and ACP stdio interfaces unauthenticated by default | Generate and require a token by default, reusing the existing bearer-token pattern |
| [FIND-05](3-findings.md#find-05-read-tool-and-conversation-content-incl-secrets-forwarded-to-the-cloud-model-provider) | Read-tool/conversation content forwarded to the cloud provider | Flip `security.redact_secrets` on by default or prompt on first cloud use; the control already exists |
| [FIND-13](3-findings.md#find-13-clientdaemon-loopback-traffic-is-plaintext-http-by-default) | Client-daemon loopback traffic is plaintext HTTP by default | Enable the already-implemented pinned-cert loopback TLS by default |

---

## Analysis Context & Assumptions

### Analysis Scope
| Constraint | Description |
|------------|-------------|
| Scope | Full STRIDE-A threat model of the Aegis Go daemon/client, its provider adapters, tool/permission/sandbox stack, swarm/MCP/ACP integrations, and on-disk stores, at commit `7230aaf` |
| Excluded | Third-party model providers' internal security, container-runtime internals, the Go standard library and vendored dependencies' own vulnerabilities, and the embedded web UI's transitive npm supply chain |
| Focus Areas | Untrusted-workspace trust boundary (config/hooks/personas/context), credential handling, command-execution isolation, prompt-injection defenses, and at-rest confidentiality |

### Infrastructure Context
| Category | Discovered from Codebase | Findings Affected |
|----------|--------------------------|-------------------|
| Network exposure | Loopback-only bind with opt-in `allow_remote` ([internal/server/server.go](../internal/server/server.go)) | FIND-13, FIND-14 |
| Config layering | Project `.aegis/config.yaml`/`.env` merged at highest file precedence ([internal/config](../internal/config)) | FIND-02, FIND-03, FIND-09, FIND-11 |
| Execution isolation | Pluggable local/OS/container sandbox backends ([internal/sandbox](../internal/sandbox)) | FIND-04, FIND-17, FIND-19 |
| Untrusted-content handling | Provenance wrapping + opt-in injection scan ([internal/trust](../internal/trust)) | FIND-07, FIND-12 |
| At-rest storage | SQLite session/checkpoint/memory/knowledge stores, partial `fsguard` ACLs ([internal/session](../internal/session), [internal/fsguard](../internal/fsguard)) | FIND-18 |

### Needs Verification
| Item | Question | What to Check | Why Uncertain |
|------|----------|---------------|---------------|
| Hook execution timing | Do project-config hooks run before or after any trust prompt exists today? | Trace `session_start` hook dispatch in `internal/hooks` and its callers | Static review suggests no trust gate, but runtime ordering was not executed |
| Terminal rendering | Does the TUI fully neutralize terminal escape sequences in untrusted tool output? | Inspect Bubbletea/lipgloss rendering path for control-char handling | Framework typically escapes, but Aegis-specific rendering not verified end-to-end |
| longmem.db / knowledge.db ACLs | Are these DBs ever `fsguard`-hardened on any platform? | Grep for `RestrictToOwner` calls covering these paths | Session DB is hardened; the two others appeared to rely on directory perms only |
| Cron fire-time gating | Are text rules truly skipped at fire time, or only the interactive contextual gate? | Read the cron fire path's permission checks in `internal/cron` | Behavior inferred from the mode-only re-read; rule application at fire time not confirmed |

### Finding Overrides
| Finding ID | Original Severity | Override | Justification | New Status |
|------------|-------------------|----------|---------------|------------|
| — | — | — | No overrides applied. Update this section after review. | — |

### Additional Notes

Two client-process components (`TerminalUI`, `Client`) were added to the STRIDE analysis during this pass for completeness; their threats are low-risk and map to existing findings (FIND-12, FIND-13). The 76-threat total includes these two additions. The `A` in STRIDE-A denotes Abuse (feature misuse), consistent throughout.

---

## References Consulted

### Security Standards
| Standard | URL | How Used |
|----------|-----|----------|
| Microsoft SDL Bug Bar | https://www.microsoft.com/en-us/msrc/sdlbugbar | Severity classification |
| OWASP Top 10:2025 | https://owasp.org/Top10/2025/ | Threat categorization |
| CVSS 4.0 | https://www.first.org/cvss/v4.0/specification-document | Risk scoring |
| CWE | https://cwe.mitre.org/ | Weakness classification |
| STRIDE | https://learn.microsoft.com/en-us/azure/security/develop/threat-modeling-tool-threats | Threat enumeration |
| OWASP LLM Top 10 | https://genai.owasp.org/llm-top-10/ | Prompt-injection / untrusted-content framing |

### Component Documentation
| Component | Documentation URL | Relevant Section |
|-----------|------------------|------------------|
| Model Context Protocol | https://modelcontextprotocol.io/docs | MCP client/server transport and capability model |
| Agent Client Protocol | https://agentclientprotocol.com | ACP JSON-RPC editor integration surface |
| Ollama | https://github.com/ollama/ollama/blob/main/docs/api.md | Local OpenAI-compatible endpoint (plain HTTP loopback) |
| Bubble Tea | https://github.com/charmbracelet/bubbletea | Terminal UI rendering framework |

---

## Report Metadata

| Field | Value |
|-------|-------|
| Source Location | `D:\Development\Aegis` |
| Git Repository | `https://github.com/fiddler110/Aegis.git` |
| Git Branch | `main` |
| Git Commit | `7230aaf` (`2026-07-12 18:12:38 -0400`) |
| Model | `claude-fable-5` |
| Machine Name | `Scott-Desktop` |
| Analysis Started | `2026-07-12 20:03:18` |
| Analysis Completed | `2026-07-13 12:31:49 UTC` |
| Duration | `~40 min active (spanned a resume across sessions)` |
| Output Folder | `threat-model-20260712-200318` |
| Prompt | `continue the threat model you were working on at threat-model-20260712-200318/` |

---

## Classification Reference

| Classification | Values |
|---------------|--------|
| **Exploitability Tiers** | **T1** Direct Exposure (no prerequisites) · **T2** Conditional Risk (single prerequisite) · **T3** Defense-in-Depth (multiple prerequisites or infrastructure access) |
| **STRIDE + Abuse** | **S** Spoofing · **T** Tampering · **R** Repudiation · **I** Information Disclosure · **D** Denial of Service · **E** Elevation of Privilege · **A** Abuse (feature misuse) |
| **SDL Severity** | `Critical` · `Important` · `Moderate` · `Low` |
| **Remediation Effort** | `Low` · `Medium` · `High` |
| **Mitigation Type** | `Redesign` · `Standard Mitigation` · `Custom Mitigation` · `Existing Control` · `Accept Risk` · `Transfer Risk` |
| **Threat Status** | `Open` · `Mitigated` · `Platform` |
| **Incremental Tags** | `[Existing]` · `[Fixed]` · `[Partial]` · `[New]` · `[Removed]` (incremental reports only) |
| **CVSS** | CVSS 4.0 vector with `CVSS:4.0/` prefix |
| **CWE** | Hyperlinked CWE ID (e.g., [CWE-306](https://cwe.mitre.org/data/definitions/306.html)) |
| **OWASP** | OWASP Top 10:2025 mapping (e.g., A01:2025 – Broken Access Control) |
