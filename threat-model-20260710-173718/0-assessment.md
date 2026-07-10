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

Aegis is a single Go binary that runs as either a local daemon (`aegis serve`, or embedded in-process in the TUI) exposing a bearer-token-protected HTTP API, or a client (Bubbletea TUI / embedded browser UI) connecting to it. It gives an LLM agent a 39+ tool registry — shell execution, git/GitHub, web fetch/search, LSP, security scanning, memory, sub-agent swarms — gated by a capability-based permission model, and integrates with external LLM providers (Anthropic, OpenAI-compatible/Ollama), external MCP servers (both as client and as server via `aegis mcp-serve`), and editors (via ACP). The system runs entirely on a single developer workstation with no Kubernetes/cloud deployment manifests in the repository.

Security engineering in this codebase is mature and iterative: the analysis found extensive, already-built, regression-tested mitigations — a hardened bearer-token/DACL scheme, an SSRF-safe outbound dialer, an always-on MCP untrusted-output provenance wrapper, a rule engine with anti-bypass protections, sub-agent gate parity fixes, and more (documented as 17 "existing control" findings below, none of which required further action). Against that backdrop, this analysis also identified a small number of genuinely significant gaps — most notably that the local daemon's page-token web UI flow can be used by *any* local process, not just the operator's own browser, to obtain full API access, and that two local integration surfaces (`aegis mcp-serve`, ACP) and the cron scheduler currently perform no authentication/authorization check of their own, relying entirely on OS process boundaries.

The analysis covers 32 system elements across 2 trust boundaries (Client Process, Daemon Process), plus 6 external services and 2 external actors outside those boundaries. Per the STRIDE Scope Rule (`analysis-principles.md`), external services get STRIDE sections as attack surfaces in their own right, while the 2 external actors (Operator, ExternalHarness) do not, since they are threat sources rather than targets.

### Risk Rating: Elevated

Aegis's `LOCALHOST_SERVICE` deployment posture (loopback-only default bind, bearer-token auth, no Tier 1/unauthenticated-remote findings) meaningfully bounds the blast radius of every finding in this report to an attacker who already has some form of local process access. Within that boundary, however, one Critical finding (FIND-01: any local process can silently obtain the daemon's full-privilege bearer token via the pre-auth web UI flow) and four Important findings (unauthenticated local integration surfaces, unattended cron execution, and two related-but-distinct prompt-injection gaps) represent real, concretely exploitable risk for the common case of a shared or multi-tenant workstation, a compromised sibling process, or a supply-chain-planted project file. The overall rating is "Elevated" rather than "Critical" because no finding is reachable without local process access, and rather than "Moderate" because FIND-01 and FIND-02 collapse the daemon's entire authentication model for any local attacker who reaches them.

> **Note on threat counts:** This analysis identified 47 threats across 30 STRIDE-analyzed components (28 internal components plus 6 external services get STRIDE sections; the 2 external actors, Operator and ExternalHarness, do not, per the STRIDE Scope Rule in `analysis-principles.md`). This count reflects comprehensive STRIDE-A coverage, not systemic insecurity. Of these, **0 are directly exploitable** without prerequisites (Tier 1) — the `LOCALHOST_SERVICE` deployment classification forbids Tier 1 entirely. The remaining 47 (38 Tier 2, 9 Tier 3) represent conditional risks and defense-in-depth considerations, of which 17 were confirmed already mitigated by existing, regression-tested controls.

---

## Action Summary

| Tier | Description | Threats | Findings | Priority |
|------|-------------|---------|----------|----------|
| [Tier 1](3-findings.md#tier-1--direct-exposure-no-prerequisites) | Directly exploitable | 0 | 0 | 🔴 Critical Risk |
| [Tier 2](3-findings.md#tier-2--conditional-risk-authenticated--single-prerequisite) | Requires authenticated access | 38 | 28 | 🟠 Elevated Risk |
| [Tier 3](3-findings.md#tier-3--defense-in-depth-prior-compromise--host-access) | Requires prior compromise | 9 | 7 | 🟡 Moderate Risk |
| **Total** | | **47** | **35** | |

### Priority by Tier and CVSS Score (Top 10)

| Finding | Tier | CVSS Score | SDL Severity | Title |
|---------|------|------------|-------------|-------|
| [FIND-01](3-findings.md#find-01-local-process-can-obtain-the-daemons-real-bearer-token-via-the-pre-auth-ui-page-token-flow) | T2 | 8.2 | Critical | Local process can obtain the daemon's real bearer token via the pre-auth `/ui` page-token flow |
| [FIND-02](3-findings.md#find-02-aegis-mcp-serve-and-the-acp-server-accept-commands-from-any-local-process-with-no-authentication) | T2 | 7.8 | Important | `aegis mcp-serve` and the ACP server accept commands from any local process with no authentication |
| [FIND-03](3-findings.md#find-03-scheduled-cron-jobs-execute-unattended-shell-commands-with-no-permission-gate-or-approval) | T2 | 7.1 | Important | Scheduled cron jobs execute unattended shell commands with no permission gate or approval |
| [FIND-04](3-findings.md#find-04-web_fetchweb_search-content-re-enters-the-models-context-with-no-untrusted-content-marker) | T2 | 6.9 | Important | `web_fetch`/`web_search` content re-enters the model's context with no untrusted-content marker |
| [FIND-05](3-findings.md#find-05-persona-and-skill-md-files-are-injected-into-the-system-prompt-with-no-sanitization) | T2 | 6.9 | Important | Persona and skill `.md` files are injected into the system prompt with no sanitization |
| [FIND-06](3-findings.md#find-06-dockerpodman-socket-access-grants-the-daemon-local-root-equivalent-privilege) | T2 | 6.4 | Moderate | Docker/Podman socket access grants the daemon local-root-equivalent privilege |
| [FIND-07](3-findings.md#find-07-lsp-tool-spawns-a-config-specified-binary-with-no-allowlist-or-verification) | T2 | 6.0 | Moderate | `lsp` tool spawns a config-specified binary with no allowlist or verification |
| [FIND-08](3-findings.md#find-08-serveraddr-can-be-configured-to-a-non-loopback-address-with-no-validation-or-warning) | T2 | 5.8 | Moderate | `server.addr` can be configured to a non-loopback address with no validation or warning |
| [FIND-09](3-findings.md#find-09-conversation-content-is-sent-to-cloud-llm-providers-with-no-redaction-or-dlp-step) | T2 | 5.3 | Moderate | Conversation content is sent to cloud LLM providers with no redaction or DLP step |
| [FIND-10](3-findings.md#find-10-opt-in-mcp-output-injection-scan-is-regex-based-and-easily-bypassed) | T2 | 5.1 | Moderate | Opt-in MCP output injection scan is regex-based and easily bypassed |

### Quick Wins

> No Tier 1 findings exist for this repository (the `LOCALHOST_SERVICE` classification forbids Tier 1). The quick wins below are the highest-value, `Low`-effort remediations across Tier 2/3 — prioritize these before the `Medium`-effort items above.

| Finding | Title | Why Quick |
|---------|-------|-----------|
| [FIND-04](3-findings.md#find-04-web_fetchweb_search-content-re-enters-the-models-context-with-no-untrusted-content-marker) | `web_fetch`/`web_search` content has no untrusted-content marker | Reuse the existing `wrapUntrusted` pattern from `internal/mcp/trust.go` — no new mechanism needed |
| [FIND-08](3-findings.md#find-08-serveraddr-can-be-configured-to-a-non-loopback-address-with-no-validation-or-warning) | `server.addr` misconfiguration has no warning | A single startup validation check against loopback addresses |
| [FIND-11](3-findings.md#find-11-no-rate-limiting-or-alerting-on-repeated-invalid-bearer-token-attempts) | No rate limiting/alerting on invalid tokens | A counter and a log line in the existing `authMiddleware` |
| [FIND-13](3-findings.md#find-13-github-pr-titlesbodies-are-not-inspected-for-secrets-before-publishing) | GitHub PR content not scanned for secrets | Reuse existing secret-pattern checks already present in `internal/security` |
| [FIND-16](3-findings.md#find-16-outputguards-fail-open-behavior-produces-no-distinct-audit-signal-when-validation-is-skipped) | OutputGuard fail-open has no distinct signal | A single additional log field on the existing fail-open path |

---

## Analysis Context & Assumptions

### Analysis Scope
| Constraint | Description |
|------------|-------------|
| Scope | Entire repository at commit `34aa687` on branch `main`; single Go module spanning the daemon, TUI/CLI client, and all `internal/*` packages |
| Excluded | `node_modules`, `.git`, `dist`, `build`, `vendor`, prior `threat-model-*` report folders; third-party dependency source code itself (only how Aegis's own code calls it was analyzed) |
| Focus Areas | Client-daemon authentication boundary, tool-execution capability model, prompt-injection surfaces (persona/skill/MCP/web content), local secrets handling, unattended-execution paths (cron, swarm), and local integration surfaces (`aegis mcp-serve`, ACP) |

### Infrastructure Context
| Category | Discovered from Codebase | Findings Affected |
|----------|--------------------------|-------------------|
| Deployment topology | No Kubernetes/Docker Compose deployment manifests found anywhere in the repository; the daemon is a standalone Go binary binding `127.0.0.1:4127` by default ([internal/config/config.go](../internal/config/config.go), [internal/server/server.go](../internal/server/server.go)) | Drives the `LOCALHOST_SERVICE` deployment classification and every Tier assignment in this report |
| Execution sandbox | Docker/Podman/WSL/Apple Containers are used only as an *outbound* execution backend for tool calls, not as Aegis's own deployment target ([internal/sandbox](../internal/sandbox)) | FIND-06, FIND-21 |
| Data storage | Embedded SQLite (`modernc.org/sqlite`) for sessions/cron jobs; flat files for checkpoints, memory, and mailboxes — no external database server | FIND-29, FIND-30, FIND-35 |
| Platform pattern | Standalone Application (not a Kubernetes operator — no `controller-runtime`/`kubebuilder` in `go.mod`, no `Reconcile()` functions) | Governs the ≤20% Platform-ratio limit; this analysis produced 0% (no threat was classified `Platform`, since Aegis has no external platform layer to delegate to) |

### Needs Verification
| Item | Question | What to Check | Why Uncertain |
|------|----------|---------------|---------------|
| WebUI frontend render paths | Does any current or future component under `internal/server/webui/frontend/src` use `dangerouslySetInnerHTML` or otherwise inject raw HTML for tool output or markdown rendering? | Search the frontend source for raw-HTML injection sinks as new render paths are added | No such sink was found for the current frontend, but this was not exhaustively re-verified against every future rendering path |
| OpenAIAdapter TLS enforcement for remote self-hosted endpoints | Is TLS certificate validation enforced when `OPENAI_API_KEY`/base URL point at an arbitrary remote (non-Ollama, non-cloud-OpenAI) OpenAI-compatible server? | `internal/provider/openai/openai.go` HTTP client/TLS configuration | Only the local-Ollama plaintext case (FIND-15) was directly evidenced; the remote self-hosted case was not separately audited |

> Note: the `internal/security/install.go` argument-construction verification gap is tracked as FIND-31 in `3-findings.md` (not duplicated here) since it already has a documented remediation/verification path.

### Finding Overrides
| Finding ID | Original Severity | Override | Justification | New Status |
|------------|-------------------|----------|---------------|------------|
| — | — | — | No overrides applied. Update this section after review. | — |

### Additional Notes

This is a Standalone Application (not a Kubernetes operator), so the Platform-ratio guardrail is ≤20% per `analysis-principles.md`; this analysis produced a 0% Platform ratio, since Aegis has no external platform layer (cloud provider, cluster control plane, managed database) to which any threat could be legitimately delegated — every mitigation identified is the project's own code, and every unmitigated threat became a finding rather than a deferred "Needs Review" or "Accepted Risk" entry, consistent with the mandatory coverage rules.

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

### Component Documentation
| Component | Documentation URL | Relevant Section |
|-----------|------------------|------------------|
| Anthropic Messages API | https://docs.anthropic.com/en/api/messages | Streaming request/response format used by AnthropicAdapter |
| Model Context Protocol | https://modelcontextprotocol.io/ | Client/server protocol implemented by MCPClient and MCPServer |
| Agent Client Protocol (ACP) | https://agentclientprotocol.com/ | JSON-RPC protocol implemented by ACPAgent |
| Docker Engine API | https://docs.docker.com/engine/api/ | Socket-based container control used by ExecutionSandbox |
| GitHub CLI (`gh`) | https://cli.github.com/manual/ | PR creation flow used by the `git_pr` tool |
| Ollama API | https://github.com/ollama/ollama/blob/main/docs/api.md | Local OpenAI-compatible endpoint reached by OpenAIAdapter |

---

## Report Metadata

| Field | Value |
|-------|-------|
| Source Location | `D:\Development\Aegis` |
| Git Repository | `https://github.com/fiddler110/Aegis.git` |
| Git Branch | `main` |
| Git Commit | `34aa687` (`2026-07-10 11:52:48 -0400`) |
| Model | `claude-sonnet-5` |
| Machine Name | `Scott-Desktop` |
| Analysis Started | `2026-07-10 17:37:18 UTC` |
| Analysis Completed | `2026-07-10 18:12:05 UTC` |
| Duration | `35 minutes` |
| Output Folder | `threat-model-20260710-173718` |
| Prompt | `/threat-model-analyst` |

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
