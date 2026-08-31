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

Aegis is a local AI coding agent shipped as a single Go binary that runs a daemon plus several client front-ends. Its defining security property is that a probabilistic model — steered in part by content the operator did not author — is given tools that read and write the operator's source tree and execute shell commands on the host. Almost every control in the system exists to bound that one fact, and most of them are unusually well built for a project of this size.

The defensive work here is genuinely strong and should be stated plainly before the gaps. Loopback binding is enforced, not merely defaulted (`validateListenAddr` refuses a non-loopback address without an explicit acknowledgement). TLS is on by default with a pinned self-signed certificate that every in-repo client wires up automatically. The bearer token is compared in constant time behind an exponential lockout that deliberately checks the token *before* the lockout window so a local guesser cannot wedge the operator out of their own daemon. The unauthenticated `/ui` route is bound by a single-use page token with a double-submit CSRF nonce and a mint cap. SSRF is handled by one shared blocklist covering `0.0.0.0/8`, CGNAT, the NAT64 prefix and reserved space, re-validated on every redirect. Container execution drops all capabilities, sets `no-new-privileges` and applies memory, CPU and PID limits. Workspace trust grants are pinned to a fingerprint of the security-relevant project config. Several of these controls carry a documented history of the exact defect they were built to close.

What the analysis found is a consistent shape rather than a scatter of unrelated defects. The controls that constrain the *machine* are strong; the controls that constrain the *model* are advisory. Untrusted content is marked, not contained (FIND-01). Persona tool lists prompt, they do not enforce. `warnOutboundSecrets` warns, it does not block. Plan mode's read-only guarantee is delivered by a 1,129-line hand-written shell classifier whose verdict is consulted *before* the policy layer, so a parsing defect is silently permissive in every mode (FIND-20) — a category with three shipped instances already. Sandboxing degrades to unconfined host execution on a warning line rather than a failure (FIND-22). And the repository's own merge gate — build, race tests, `govulncheck` and `staticcheck`, all deliberately made blocking — currently runs only on manual dispatch (FIND-11).

The analysis covers 22 system elements across 4 trust boundaries.

### Risk Rating: Elevated

The deployment classification is `LOCALHOST_SERVICE`: the daemon binds `127.0.0.1:4127` and refuses anything else without an explicit flag, so nothing here is reachable by an unauthenticated remote attacker and there are no Tier 1 findings. That bounds the blast radius considerably and is why the rating is Elevated rather than Critical. The rating is not lower because the prerequisite that does apply — a process running as the operator, or content the agent is asked to read — is a low bar for this class of tool, and the consequence on the other side of it is host command execution. The single Critical finding (FIND-01) is the one that carries that combination: attacker-controlled content reaching a model with file and network tools, contained only by a marker the model may ignore.

> **Note on threat counts:** This analysis identified 138 threats across 22 components. This count reflects comprehensive STRIDE-A coverage, not systemic insecurity. Of these, **0 are directly exploitable** without prerequisites (Tier 1). The remaining 138 represent conditional risks and defense-in-depth considerations.

---

## Action Summary

| Tier | Description | Threats | Findings | Priority |
|------|-------------|---------|----------|----------|
| [Tier 1](3-findings.md#tier-1--direct-exposure-no-prerequisites) | Directly exploitable | 0 | 0 | 🔴 Critical Risk |
| [Tier 2](3-findings.md#tier-2--conditional-risk-authenticated--single-prerequisite) | Requires authenticated access | 48 | 19 | 🟠 Elevated Risk |
| [Tier 3](3-findings.md#tier-3--defense-in-depth-prior-compromise--host-access) | Requires prior compromise | 90 | 14 | 🟡 Moderate Risk |
| **Total** | | **138** | **33** | |

### Priority by Tier and CVSS Score (Top 10)

| Finding | Tier | CVSS Score | SDL Severity | Title |
|---------|------|------------|-------------|-------|
| [FIND-01](3-findings.md#find-01-indirect-prompt-injection-is-marked-but-not-constrained) | T2 | 8.7 | Critical | Indirect prompt injection is marked but not constrained |
| [FIND-02](3-findings.md#find-02-external-mcp-servers-are-trusted-on-configuration-alone) | T2 | 8.2 | Important | External MCP servers are trusted on configuration alone |
| [FIND-03](3-findings.md#find-03-configuration-endpoints-can-disable-command-isolation-with-only-the-bearer-token) | T2 | 8.1 | Important | Configuration endpoints can disable command isolation with only the bearer token |
| [FIND-04](3-findings.md#find-04-the-web-ui-hands-the-real-daemon-token-to-browser-javascript) | T2 | 7.6 | Important | The web UI hands the real daemon token to browser JavaScript |
| [FIND-05](3-findings.md#find-05-outbound-provider-payloads-and-tool-arguments-are-never-redacted) | T2 | 7.5 | Important | Outbound provider payloads and tool arguments are never redacted |
| [FIND-06](3-findings.md#find-06-providerbase_url-override-redirects-credentials-and-model-control-on-a-warning-only) | T2 | 7.3 | Important | `provider.base_url` override redirects credentials and model control on a warning only |
| [FIND-07](3-findings.md#find-07-the-local-model-endpoint-is-unauthenticated-plaintext-http-on-loopback) | T2 | 7.1 | Important | The local model endpoint is unauthenticated plaintext HTTP on loopback |
| [FIND-08](3-findings.md#find-08-web_fetch-is-an-unreviewed-egress-channel) | T2 | 6.9 | Important | `web_fetch` is an unreviewed egress channel |
| [FIND-09](3-findings.md#find-09-session-working-directory-allowlist-is-not-enforced-on-the-default-bind) | T2 | 6.8 | Important | Session working-directory allowlist is not enforced on the default bind |
| [FIND-10](3-findings.md#find-10-the-container-workspace-mount-exposes-the-whole-workspace-to-every-command) | T2 | 6.5 | Important | The container workspace mount exposes the whole workspace to every command |

### Quick Wins

There are no Tier 1 findings, so this table lists the low-effort findings from the highest tier present (Tier 2). Each is a configuration change, a workflow edit or a single-file fix.

| Finding | Title | Why Quick |
|---------|-------|-----------|
| [FIND-11](3-findings.md#find-11-build-test-vulnerability-and-lint-gates-no-longer-run-on-push-or-pull-request) | Build, test, vulnerability and lint gates no longer run on push or pull request | Uncomment two trigger lines in `.github/workflows/ci.yml`; the whole pipeline already exists and passes. |
| [FIND-06](3-findings.md#find-06-providerbase_url-override-redirects-credentials-and-model-control-on-a-warning-only) | `provider.base_url` override redirects credentials and model control on a warning only | Move one config key into the security-relevant set in `internal/config/fingerprint.go`'s policy table. |
| [FIND-09](3-findings.md#find-09-session-working-directory-allowlist-is-not-enforced-on-the-default-bind) | Session working-directory allowlist is not enforced on the default bind | Remove the loopback exemption; the allowlist logic is already written. |
| [FIND-15](3-findings.md#find-15-all-cost-and-token-budgets-default-to-unlimited) | All cost and token budgets default to unlimited | Change default values in `internal/config/config.go`; `internal/cost` already enforces them. |
| [FIND-16](3-findings.md#find-16-the-unauthenticated-ui-mint-can-be-flooded-to-deny-the-operators-own-ui) | The unauthenticated `/ui` mint can be flooded to deny the operator's own UI | Add a per-address rate limit ahead of the existing cap in `mintPageToken`. |
| [FIND-13](3-findings.md#find-13-container-and-scanner-images-are-referenced-by-mutable-tag) | Container and scanner images are referenced by mutable tag | Resolve and pin digests at first use; `verify-image` already exists. |
| [FIND-17](3-findings.md#find-17-web-ui-assets-are-served-from-a-committed-dist-whose-drift-check-no-longer-runs) | Web UI assets are served from a committed `dist/` whose drift check no longer runs | Largely subsumed by FIND-11 — the drift job is in the same workflow. |
| [FIND-19](3-findings.md#find-19-healthz-discloses-daemon-presence-without-authentication) | `/healthz` discloses daemon presence without authentication | Add a test asserting the response stays minimal; no behaviour change needed today. |

---

## Analysis Context & Assumptions

### Analysis Scope

| Constraint | Description |
|------------|-------------|
| Scope | The full `Aegis` repository working tree at commit `88cea69` on branch `main`, including 896 Go source files across 70+ `internal/` packages, the embedded web UI, the four GitHub Actions workflows, and the container scanner definitions. |
| Excluded | Test files were read for evidence but not threat-modelled as components. `research/` and `testrun/` are development artefacts and were consulted as evidence rather than analysed. Third-party dependency source (`charm.land/*`, `modernc.org/sqlite`, `koanf`) was not audited; `govulncheck` is the project's own control for that surface. Vendored/generated web UI `dist/` bundle contents were not decompiled. |
| Focus Areas | Agent-loop containment (permission gate, capability classification, sandboxing), the daemon's authentication and authorization surface, untrusted-content handling and prompt-injection paths, credential and conversation data at rest, multi-client authorization (MCP, ACP, cron), and CI/release supply chain. |

### Infrastructure Context

| Category | Discovered from Codebase | Findings Affected |
|----------|--------------------------|-------------------|
| Deployment topology | Loopback-bound daemon at `127.0.0.1:4127`, non-loopback refused without `server.allow_remote` ([internal/config/config.go](../internal/config/config.go), [internal/server/lifecycle.go](../internal/server/lifecycle.go)) | Sets the `LOCALHOST_SERVICE` classification, which forbids Tier 1 across all 33 findings |
| Transport security | TLS on by default with an auto-generated pinned self-signed certificate ([internal/config/config_server.go](../internal/config/config_server.go)) | FIND-04, FIND-18, FIND-25 |
| Authentication | Single 32-byte bearer token, constant-time compare, exponential lockout ([internal/server/auth.go](../internal/server/auth.go)) | FIND-03, FIND-04, FIND-25 |
| Execution isolation | `sandbox.backend: container` default cascading to OS then unsandboxed local ([internal/sandbox/](../internal/sandbox/), [internal/config/config.go](../internal/config/config.go)) | FIND-10, FIND-13, FIND-22, FIND-26 |
| Authorization model | Mode/rule/contextual/persona/scope gate stack built once in `enginecfg` ([internal/permission/](../internal/permission/), [internal/enginecfg/](../internal/enginecfg/)) | FIND-20, FIND-21, FIND-23 |
| Untrusted-content handling | Provenance wrapper plus heuristic injection scan on MCP and web content ([internal/trust/](../internal/trust/), [internal/mcp/trust.go](../internal/mcp/trust.go)) | FIND-01, FIND-08, FIND-28 |
| Egress controls | Shared SSRF blocklist and dialer with redirect re-validation ([internal/netblock/netblock.go](../internal/netblock/netblock.go)) | FIND-08 |
| Data at rest | SQLite session and cron stores, file checkpoints, JSON trust store, spill files ([internal/sqlitestore/](../internal/sqlitestore/), [internal/checkpoint/](../internal/checkpoint/), [internal/workspacetrust/](../internal/workspacetrust/)) | FIND-24, FIND-27, FIND-31 |
| Supply chain | CodeQL live on push/PR; CI and release workflows on `workflow_dispatch` only ([.github/workflows/](../.github/workflows/)) | FIND-11, FIND-12, FIND-17 |
| Deployment pattern detection | Standalone application (no `controller-runtime`, `kubebuilder` or `operator-sdk` in `go.mod`), so the Platform-classification limit applied is ≤20%. Observed ratio: 2 of 138 threats (1.4%). | All |

### Needs Verification

| Item | Question | What to Check | Why Uncertain |
|------|----------|---------------|---------------|
| Working-tree state | Does the committed state at `88cea69` match what was analysed? | `git status` reported 104 modified/untracked paths at analysis time, including `shell_readonly.go`, `argv_confine.go`, `pathvalidator.go` and a new `filetrust.go`. The analysis covers the working tree, not the commit alone. | Uncommitted work in a security-relevant area can change conclusions between this report and the next commit. |
| P79.1 regression | Is the Windows read-only-shell confinement regression closed, or only masked in the working tree? | The four named tests pass in the working tree (verified 2026-08-31). Re-run them against `88cea69` with changes stashed, and confirm exploitability through `shellTool.CapabilityFor` end to end rather than the classifier alone. | The roadmap entry states the reachability through the real tool call "has not been checked". |
| Audit hook default | Is any audit sink wired by default in a shipped configuration? | Trace whether `enginecfg`'s hook chain constructs `hooks.NewAudit` when no hook config is present. | FIND-14 assumes it is opt-in based on the absence of a default construction site; a wiring path elsewhere would downgrade the finding. |
| Provider payload redaction | Is `redact.Text` applied anywhere on the outbound provider path? | Grep the adapter request-construction paths for a redaction call before serialization. | Absence of evidence in the read files, not proof of absence across all 896 files. |
| Session DB file ACLs on Windows | Do the SQLite `-wal`/`-shm` files inherit a permissive ACL in practice? | Create a session on Windows and inspect the effective ACL on the database and companion files with `icacls`. | `sqlitestore.Open` restricts the directory but not the file; the driver's file-creation mode was not traced. |
| CVSS scores | Are the CVSS 4.0 base scores accurate to the calculator? | Recompute each vector in the FIRST CVSS 4.0 calculator. | Scores are analyst estimates derived from the stated vectors; vectors are the authoritative part of each finding. |
| CI disablement intent | Was disabling `ci.yml` and `release.yml` deliberate and permanent? | Ask the maintainer; the comment says "Temporarily disabled (non-security pipeline)". | If the disablement is a known temporary state with a planned restore, FIND-11's priority changes but not its validity. |

### Finding Overrides

| Finding ID | Original Severity | Override | Justification | New Status |
|------------|-------------------|----------|---------------|------------|
| — | — | — | No overrides applied. Update this section after review. | — |

### Additional Notes

Two threats are classified `Platform` rather than `Open`, and both are genuinely outside this repository's control: Docker's group-membership-equals-host-root model (T21.E1) and the absence of signatures on Ollama model files (T18.E). Neither is something Aegis code can fix; both are worth an operator-facing note in `docs/installation.md`.

This codebase carries an unusually detailed record of its own security history — `research/roadmap.md` and `research/releases.md` name specific defects, the reasoning that produced them and the tests that now pin them. Two open items in that record were confirmed as live findings here (P80.1 → FIND-21; the plan-mode classifier exposure → FIND-20), and one (P79.1) was verified as no longer reproducing in the working tree and is therefore reported as structural exposure rather than an active defect. Report readers should treat that roadmap as a primary source alongside this document.

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
| OWASP Top 10 for LLM Applications | https://owasp.org/www-project-top-10-for-large-language-model-applications/ | Prompt-injection and excessive-agency framing for FIND-01, FIND-08, FIND-28 |
| SLSA Supply-chain Levels for Software Artifacts | https://slsa.dev/spec/v1.0/levels | Release provenance expectations for FIND-12 |

### Component Documentation

| Component | Documentation URL | Relevant Section |
|-----------|------------------|------------------|
| Docker Engine | https://docs.docker.com/engine/security/ | Docker daemon attack surface and the docker-group-equals-root model (T21.E1) |
| Podman | https://docs.podman.io/en/latest/markdown/podman-run.1.html | Rootless containers, `--cap-drop`, `--security-opt=no-new-privileges` |
| Ollama | https://github.com/ollama/ollama/blob/main/docs/faq.md | Default loopback bind, absence of authentication, `OLLAMA_CONTEXT_LENGTH` (FIND-07) |
| Model Context Protocol | https://modelcontextprotocol.io/specification | `tools/list_changed`, stdio and HTTP+SSE transports (FIND-02) |
| Anthropic API | https://docs.claude.com/en/api/overview | `x-api-key` authentication and endpoint host (FIND-05, FIND-06) |
| GitHub Actions security hardening | https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions | Action pinning by SHA and workflow permissions (FIND-12) |
| Go vulnerability management | https://go.dev/doc/security/vuln/ | `govulncheck` as a merge gate (FIND-11) |
| SQLite | https://www.sqlite.org/wal.html | WAL journal mode and the `-wal`/`-shm` companion files (FIND-24) |
| Aegis project documentation | https://github.com/fiddler110/Aegis/tree/main/docs | `configuration.md`, `permissions.md`, `mcp-trust-boundary.md`, `security_scan.md` |

---

## Report Metadata

| Field | Value |
|-------|-------|
| Source Location | `D:\Development\Aegis` |
| Git Repository | `https://github.com/fiddler110/Aegis.git` |
| Git Branch | `main` |
| Git Commit | `88cea69` (`2026-08-30 10:43:43 -0400`) |
| Model | `claude-opus-5` |
| Machine Name | `Scott-Desktop` |
| Analysis Started | `2026-08-31 00:21:23 UTC` |
| Analysis Completed | `2026-08-31 00:48:37 UTC` |
| Duration | `27m 14s` |
| Output Folder | `D:\Development\Aegis\threat-model-20260831-002123` |
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
