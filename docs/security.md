# Security Features

Aegis includes several security-focused capabilities: a security scanning tool, pluggable sandbox backends for shell execution isolation, and contextual policies that control tool behavior at runtime.

---

## Security Scanning

The `security_scan` tool and `aegis scan` command run available security scanners against your codebase and produce a normalized findings report.

### CLI usage

```bash
aegis scan .                      # scan current directory
aegis scan ./src                  # scan a specific path
aegis scan image alpine:3.20      # scan a container image by reference (see below)
aegis scan sbom .                 # generate a CycloneDX SBOM via syft (see below)
```

### Tool usage

The agent can call `security_scan` directly:

```json
{
  "path": "."   // optional: workspace-relative subdirectory; defaults to the whole workspace
}
```

Or scan a built container image instead of the workspace:

```json
{
  "image": "alpine:3.20"   // container image reference; mutually exclusive with path
}
```

Or generate an SBOM instead of scanning for findings:

```json
{
  "sbom": true   // generates via syft, persists to .aegis/sbom.cdx.json; mutually exclusive with image
}
```

### Scanners

| Scanner | What it finds | Host binary | Enabled by default? |
|---------|--------------|---------|---|
| **opengrep** | SAST: code patterns, injection, auth issues, insecure APIs — default engine | `opengrep` | Yes |
| **Semgrep** | Same as opengrep; selectable alternative engine | `semgrep` | No — opt-in |
| **gosec** | Go-specific SAST (crypto misuse, SQL injection, hardcoded creds) | `gosec` | No — opt-in |
| **Bandit** | Python-specific SAST | `bandit` | No — opt-in |
| **Brakeman** | Ruby on Rails-specific SAST | `brakeman` | No — opt-in |
| **njsscan** | Node.js-specific SAST | `njsscan` | No — opt-in |
| **Trivy** | Vulnerabilities in dependencies (Go, npm, pip, etc.), IaC misconfig (Terraform/CloudFormation/K8s/Helm/Dockerfile/ARM), secrets | `trivy` | Yes |
| **Gitleaks** | Secrets and credentials accidentally committed | `gitleaks` | Yes |
| **Kubescape** | Kubernetes manifest/Helm chart misconfigurations, mapped to NSA/MITRE/CIS framework controls with real severity | `kubescape` | Yes |
| **Hadolint** | Dockerfile lint (any `Dockerfile`/`Dockerfile.*`/`*.dockerfile` found in the scanned path) | `hadolint` | Yes |
| **osv-scanner** | Dependency CVEs across lockfiles/manifests, backed by OSV.dev; also the only reachability-aware scanner (below) | `osv-scanner` | Yes |
| **Grype** (directory mode) | Dependency CVEs, preferring a syft-generated SBOM over its own cataloger | `grype` (+ `syft`, optional) | Yes |

Grype's directory scan generates a CycloneDX SBOM via `syft` first and scans that
(`grype sbom:<file>`) rather than cataloging the directory itself — this ties the CVE match
to a standalone artifact persisted at `.aegis/sbom.cdx.json`, reusable by other tooling. If
`syft` isn't installed (or the SBOM-first run fails for any reason), grype falls back to
scanning the directory directly (`grype dir:<path>`) so a missing/broken syft install never
blackholes the SCA control. Use `aegis scan sbom` to generate the SBOM standalone, without
running grype at all.

### SAST: opengrep by default, semgrep and language-specific engines opt-in

**opengrep** (a community-governed semgrep fork — no login/telemetry, openly-licensed rules)
is the default SAST engine. **Semgrep** is a selectable alternative: same rule syntax, but
`--config auto` needs network egress and nudges toward a platform login, and its registry's
rule-update velocity for brand-new CVE patterns is faster than opengrep's — so keep it
available for teams that want that trade-off. Both run with **explicitly pinned rule packs**
(`p/owasp-top-ten`, `p/security-audit`) rather than `auto`, for reproducibility and
supply-chain hygiene.

Four opt-in, language-targeted engines cover what the multi-language core misses:
**gosec** (Go), **bandit** (Python), **brakeman** (Ruby on Rails), **njsscan** (Node.js).
They're listed in `aegis security status` like every other scanner but resolve to
"opt-in tool, not enabled by default" until turned on:

```yaml
security:
  tools:
    semgrep:
      enabled: true    # use semgrep instead of / alongside opengrep
    gosec:
      enabled: true    # this is a Go project
```

Or interactively: `/security-config` (TUI) or `aegis security config`.

### Reachability: is the vulnerable code actually called?

A dependency CVE means the vulnerable *package* is present — not necessarily that your code
ever calls the vulnerable *function*. osv-scanner runs with `--call-analysis=all`, which
does call-graph analysis to tell the difference: for Go it's on by default and is
govulncheck under the hood (the same analysis `go vet`-adjacent tooling uses); Rust and
Java JAR analysis are experimental upstream. No other scanner Aegis integrates (trivy,
grype, semgrep) has an open-source equivalent as of this writing, so their findings carry
no reachability signal — this is deliberately not guessed or inferred from anywhere else.

Each finding's `reachability` is one of:

- **`reachable`** — a real call path from your code to the vulnerable symbol was found. Treat as the priority fix.
- **`unreachable`** — the package is a dependency, but the flagged code is never invoked. Lower urgency; still worth a version bump when convenient, since a future code change could start calling it.
- *(absent)* — not analyzed (every non-osv-scanner finding, and any osv-scanner ecosystem without call-analysis support, e.g. npm/PyPI today). Treat exactly as before: severity alone, no reachability claim either way.

`aegis scan`/`security_scan`'s report sorts by severity first, then reachability as a
tiebreak — a `reachable` HIGH surfaces above an `unreachable` HIGH — but never hides or
demotes an unreachable finding out of severity order; it's still a real CVE in your
dependency tree.

### Container image scanning

`aegis scan image <ref>` / `security_scan {"image": "..."}` run a separate set of
scanners that analyze a **built image** rather than a source directory:

| Scanner | What it finds | Host binary |
|---------|--------------|---------|
| **Trivy** (`image` mode) | OS + application layer CVEs | `trivy` |
| **Grype** | Layer CVEs (Anchore vulnerability DB) | `grype` |
| **Dockle** | CIS Docker Benchmark / image best-practice violations | `dockle` |

**Host-binary only for now:** image scanning needs to pull/inspect the target image, which
means network egress — but the container-fallback runner these scanners would otherwise use
is deliberately network-isolated (`--network none`, same hardening as every other scanner
container, see below). Rather than punch a network hole through that posture, image
scanning simply doesn't offer a container fallback yet: a tool that would resolve to
`MethodContainer` is reported skipped with an explicit reason instead. Install
trivy/grype/dockle natively to use image scanning.

### Dynamic Application Security Testing (DAST)

Every scanner above is static — it reads source/config/an image without running anything.
`aegis scan dast <target-url>` / `security_scan` (via the standalone `dast_scan` tool) runs
**OWASP ZAP** against a *running* application instead: it crawls the live app and (in
`active`/`api` mode) sends real attack payloads to find exploitable vulnerabilities (XSS,
injection, auth issues, missing security headers) that static analysis structurally can't see.

```bash
aegis scan dast http://localhost:3000                                    # baseline: passive spider + passive scan
aegis scan dast http://localhost:3000 --mode active                      # + real attack payloads
aegis scan dast http://localhost:3000 --mode api --api-definition <url>  # OpenAPI-defined endpoints + active scan
```

**This is the highest-risk scanner in Aegis, and it is gated accordingly — not just by
approval.** Pointing an active scanner at a host you don't own is an attack, and an agent
that can point ZAP at an arbitrary URL is an abuse primitive. Every `dast_scan` call passes
through checks that run **unconditionally, independent of permission mode** (`plan`/`build`/
`auto` — even `auto` mode, which otherwise allows execute-capability tools outright):

1. **Target authorization (`security.dast.allowed_targets`)** — the target's host must be
   loopback/RFC-1918 private (allowed by default; the common "scan my locally running app"
   case needs no config) or explicitly declared. Hostnames are matched as literal strings,
   never DNS-resolved, so a declared target's identity can't be silently changed by whatever
   it happens to resolve to at scan time (ZAP does its own resolution inside the container,
   outside this check's visibility):
   ```yaml
   security:
     dast:
       allowed_targets:
         - staging.example.com      # exact hostname
         - .internal.example.com    # leading dot = subdomain wildcard
         - 203.0.113.0/24           # CIDR range
   ```
2. **Active-mode opt-in (`security.dast.allow_active`)** — `active`/`api` modes send real
   attack traffic and are refused entirely unless `allow_active: true` is set, *in addition
   to* the normal execute-tool approval prompt every `dast_scan` call gets. `baseline` mode
   (passive: spider + passive scan, no attack payloads) needs no such flag.
3. **Normal execute-tool approval** — `dast_scan`'s capability is `execute` (not `network`),
   so it's blocked outright in `plan` mode and prompts for approval in `build` mode like any
   other execute-capability tool, the same as `security_scan`/shell.

Container-only, like every ZAP install path: `security.tools.zap.image` must be set to a
**digest-pinned** `ghcr.io/zaproxy/zaproxy` reference (same two-command `docker pull`/
`docker inspect` recipe as every other scanner — see below) — there is no host-binary mode.
Unlike every other scanner container, the DAST container run does **not** use `--network
none`: reaching the target is the entire point, so this is the one documented exception to
the network-isolation hardening every other scanner container gets.

`aegis scan dast` runs ZAP's **Automation Framework** (a YAML plan Aegis generates per call)
rather than the packaged `zap-baseline.py`/`zap-full-scan.py`/`zap-api-scan.py` scripts,
because only the Automation Framework's `report` job can emit **SARIF** — the format every
other finding in this codebase already normalizes through. `api` mode requires
`api_definition` (an OpenAPI spec URL); GraphQL/SOAP API definitions aren't wired up yet.

**v1 scope:** you supply an already-running target URL reachable from the container (e.g. an
app running on the host, or one it can reach on your Docker network). Composing "build the
target + scan it, with no external exposure" on one ephemeral network is real follow-up work,
not done here.

### Scanner availability (host binary vs container fallback)

By default each scanner runs its host binary if it's on `PATH`. When it isn't, Aegis
can fall back to running the scanner's own container image instead of silently
skipping it — but only once you configure a **digest-pinned** image for that tool.
Aegis ships no built-in image pin: a scanner container image is itself supply-chain
attack surface, and a digest baked into the binary would inevitably go stale. Pin one
yourself once you've verified it:

```bash
docker pull aquasec/trivy:0.56.2
docker inspect --format='{{index .RepoDigests 0}}' aquasec/trivy:0.56.2
# -> aquasec/trivy@sha256:<digest>
```

```yaml
security:
  default_method: auto        # "auto" (host, else container) | "host" | "container"
  tools:
    trivy:
      method: auto
      image: "aquasec/trivy@sha256:<digest-you-verified>"
    semgrep:
      enabled: true
      method: host             # never fall back to a container for this one
    gitleaks:
      install: prompt           # prompt (default) | always | never — see `aegis security install`
```

Inspect what will actually happen before you run a scan:

```bash
aegis security status              # host / container / unavailable + why, per scanner
aegis security config              # print the resolved security.tools configuration
aegis security install trivy       # guided host install: shows the exact command, asks to confirm
aegis security install trivy --yes # skip the confirmation prompt (scripted use)
```

Or configure it interactively in the TUI instead of hand-editing YAML:

```
/security-config          # edit the project's .aegis/config.yaml
/security-config global   # edit ~/.config/aegis/config.yaml instead
```

This opens a form: pick a tool to toggle enabled/method/install policy/image,
or "Save & exit" to write it out. Changes take effect on the next restart.

`aegis scan`/`security_scan` report which method actually ran each tool ("Scanners run:
trivy (container), gitleaks (host)"), and any skipped tool's reason (disabled,
no binary + no image configured, no container runtime available, ...) — never a
silent skip.

A configured `security.tools.<name>.image` is **validated**, not just documented, as a
digest pin (P11.9): a floating tag (`aquasec/trivy:latest`) or a bare image name resolves
to `MethodNone` with a reason pointing back at this section's `docker pull`/`docker inspect`
recipe, rather than silently running an image that can be repointed at any time by whoever
controls the registry.

### Output format

Findings are normalized across all scanners:

```
Severity: HIGH
Location: internal/server/server.go:142
Rule:     CWE-307 (Brute Force)
Message:  Missing rate limiting on authentication endpoint
Source:   semgrep

Severity: MEDIUM
Location: go.sum
Rule:     CVE-2024-12345
Message:  golang.org/x/crypto@v0.17.0 has a known vulnerability
Source:   trivy

Severity: HIGH
Location: .env.example:3
Rule:     generic-api-key
Message:  Potential API key in example file
Source:   gitleaks
```

### Dedup, ASVS mapping, and the suppression baseline

Every scan report goes through three post-processing passes before it's
returned (P11.8):

- **Dedup** — the same CVE/rule flagged at the same location by more than
  one tool (a common SCA case: osv-scanner, grype, and trivy all matching
  the same vulnerable dependency) collapses into a single finding, keeping
  the highest-severity copy and tagging it `[also flagged by: <tools>]` so
  nothing is silently hidden, just not repeated three times.
- **ASVS mapping** — findings with a recognizable CWE (from SARIF rule tags
  — semgrep/opengrep/gosec/bandit/brakeman/njsscan/ZAP all tag one when they
  know it) get a best-effort OWASP ASVS 4.0.3 chapter label (e.g. `asvs: V5.3
  Output Encoding and Injection Prevention`), plus a small tool-based
  fallback for gitleaks/kubescape/hadolint/trivy-misconfig. Left blank when
  there's no confident mapping (in particular, known-vulnerable-dependency
  findings have no dedicated ASVS chapter and are never guessed at) — a
  wrong standards claim is worse than none.
- **Suppression baseline** — an optional `.aegis/security-baseline.yaml`
  lets an operator accept a specific finding as known risk, with a mandatory
  expiry:

  ```yaml
  suppressions:
    - rule_id: "CVE-2024-12345"      # or any scanner rule ID
      location: "go.sum"             # optional — omit to suppress this rule everywhere
      reason: "no fix available upstream; mitigated by network policy"
      expires: "2026-12-31"          # required — YYYY-MM-DD
      added_by: "you"                # optional, for your own records
  ```

  A matched, unexpired entry moves its finding out of the report's main list
  into a `Suppressed` count (`aegis scan` prints "Suppressed by baseline:
  N"). An **expired** entry (past `expires`) stops suppressing — the finding
  comes back into view with a note that the acceptance lapsed. An **invalid**
  entry (missing `rule_id`/`reason`, or an unparseable `expires`) is never
  applied at all, also with a note — a malformed baseline fails safe rather
  than silently hiding something. `aegis security baseline [path]` shows
  every entry's current status (active/expired/invalid) without needing to
  run a full scan; the file itself is hand-edited, same as
  `security.tools`/`security.default_method` in `.aegis/config.yaml`.

### Regression testing (P11.9)

`internal/security/regression_test.go` (`TestScanRegressionAcrossRecordedOutputs`) runs
the full aggregation pipeline — parse, dedup, ASVS tagging, baseline suppression, sort —
over **recorded** scanner-output fixtures under `internal/security/testdata/` and compares
the result to a checked-in golden file. No scanner binary, container runtime, or network
access is needed to run it, so it exercises the same code path as a live `aegis scan`
entirely in CI. See `internal/security/testdata/README.md` for what each fixture
represents (including the one honest gap: the ZAP/DAST fixture is a representative
synthetic SARIF report, not a live capture, since generating a real one needs Docker and a
running target — the README documents the exact `aegis scan dast` invocation against
Juice Shop/VAmPI to replace it once that's available). Regenerate deliberately after a
fixture or normalization change, reviewing the diff first:

```bash
AEGIS_EVAL_UPDATE=1 go test ./internal/security/... -run TestScanRegressionAcrossRecordedOutputs
```

### Combining with personas

Security-focused personas are tuned to work with scanning results:

```bash
aegis --persona appsec-engineer
# Then: "run a security scan and give me a prioritized remediation plan"
```

---

## Sandboxed Execution

Shell commands (`shell` tool) can run inside containers instead of directly on the host, providing process isolation, filesystem isolation, and optional network isolation.

### Backends

| Backend | Description |
|---------|-------------|
| `local` | Run directly on the host (default) |
| `docker` | Docker containers |
| `podman` | Podman containers (rootless) |
| `wslc` | WSL containers (Windows; preferred on Windows when available) |
| `container` | Apple Containers (macOS) |
| `os` | OS-level isolation without a container runtime: macOS `sandbox-exec` (seatbelt) or Linux `bwrap`. See the read-exposure caveat below — this is a materially weaker guarantee than `container`. |
| `auto` | Auto-detect: probe available runtimes, pick best; fall back to local |

### Configuration

```yaml
sandbox:
  backend: auto              # auto-detect or pick a specific backend
  runtime: ""                # force when backend=container: docker | podman | wslc | container
  priority: []               # override auto-detection order
  image: "ubuntu:22.04"      # container image to use
  network: false             # allow network inside containers
```

**Auto-detection priority** (OS-specific defaults):
- **Windows:** wslc → docker → podman
- **macOS/Linux:** docker → podman

Override with `priority: [podman, docker]`.

### CLI management

```bash
aegis sandbox detect          # probe all runtimes; show availability and auto pick
aegis sandbox use auto        # set backend=auto (writes config)
aegis sandbox use docker      # set backend=container, runtime=docker
aegis sandbox use local       # revert to no sandboxing
aegis sandbox test            # run uname -a in configured sandbox to verify
```

**TUI:**
```
/sandbox                  show current backend and detected runtimes
/sandbox use docker       switch to Docker sandbox
/sandbox use local        switch back to local
```

### Security properties

| Property | Container sandbox | OS sandbox (`os`) |
|----------|------------------|--------------------|
| Process isolation | Yes | No — commands run as a child of the daemon, not in a separate namespace/VM |
| Filesystem write isolation | Yes — workspace is mounted read-write; host is not accessible | Yes — writes outside the workspace (plus temp dirs) are denied |
| Filesystem **read** isolation | Yes — host filesystem is not accessible at all | **No — the entire host filesystem is readable.** Seatbelt's profile is `(allow default)` with only `file-write*` denied outside the workspace; bwrap's is `--ro-bind / /`, read-only-mounting the whole host root. A compromised shell command can still read `~/.ssh`, cloud credential files, or any other host file and exfiltrate it (if network isn't separately denied via `network: false`) |
| Network isolation | Optional (`network: false` blocks container egress) | Optional (`network: false` denies network the same way) |
| Root access on host | No | No |

**Read this before treating `os` as equivalent to `container`:** "sandboxed" is not one property — confining writes, confining reads, and confining network are three independent guarantees, and the `os` backend only ever gives you the first (and third, if configured). It is a real and useful mitigation (it stops the agent from writing outside the workspace, which is the majority of accidental-damage scenarios), but do not rely on it to protect secrets or credentials readable by the host user — use `container` for that, or avoid running genuinely untrusted code under `os` mode at all.

**Path validation:** The sandbox backend validates that shell commands can't escape the workspace mount point (this governs the write-confinement above; it does not add read confinement to the `os` backend).

**SSRF protection:** The `web_fetch` and `web_search` tools independently reject private IP addresses (10.x, 172.16–31.x, 192.168.x, 127.x, ::1) regardless of sandbox backend.

**Secret env stripping (P7.2):** The `local` and `os` backends run on the host and would otherwise inherit the daemon's full environment, including `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` — a prompt-injected instruction that gets the agent to run `shell` could read them back out, then use `web_fetch` to exfiltrate them to a public host. Both backends always strip those two names before exec; add more via `sandbox.strip_env` (e.g. MCP tokens loaded from `.aegis/.env`). The `container` backend is unaffected — `docker run`/`podman run` never pass host env into the container in the first place.

### When to use

- **Any time the agent runs genuinely untrusted code** (executing downloaded scripts, building user-provided packages) — use `container`. The `os` backend does not confine reads (see above), so a malicious script can still read and exfiltrate host secrets from under it.
- **Preventing accidental damage from trusted-but-fallible agent output** (writes outside the workspace) without wanting the overhead of a container runtime — `os` is a reasonable, lighter-weight choice here.
- **Enforcing network egress restrictions** — `network: false` prevents `curl`, `wget`, etc. from reaching the internet even if the permission rules allow `shell`
- **CI/CD pipelines** — run `aegis chat --yes` with a container sandbox for safe automated runs

### Startup warning

When `auto` mode runs with the local sandbox and the shell tool is present alongside network policies (`network_allowlist` or `egress_then_write`), the daemon logs a startup warning:

```
WARN: network_allowlist is set but sandbox.backend=local — the shell tool can still
reach the network directly. Set sandbox.backend=container with network=false for
enforced egress isolation.
```

### Fallback-to-local warning

If `sandbox.backend` is `container` or `os` but the runtime can't be initialized (e.g. the Docker daemon isn't running), the daemon falls back to the unsandboxed local backend rather than refusing to start. This is a silent security downgrade if you don't notice it, so:

- It's always logged at `WARN`.
- It's reported by `/healthz` (`sandbox_fallback`/`sandbox_fallback_reason`), and the TUI/CLI print a warning banner before entering a session when it's active.
- Set `sandbox.strict: true` to turn this into a hard startup failure instead of a silent downgrade — use this when you depend on sandboxing being active (e.g. CI running untrusted code).

---

## Contextual Security Policies

Two runtime policies control how tool capabilities interact:

### `egress_then_write`

Require explicit approval for write-capability tool calls that occur after any network-capability call in the same session.

```yaml
security:
  egress_then_write: true
```

**Why:** Prevents the agent from fetching external content and then persisting it locally without your knowledge — a pattern that appears in prompt injection attacks (malicious content in a fetched page instructs the agent to write files or exfiltrate data).

**Behavior:** Even in `auto` mode, if the agent fetches a URL and then tries to write a file, a permission prompt appears. The agent can still proceed after your approval.

**What counts as network:** Tool calls with the `network` capability — `web_fetch`, `web_search`, and any MCP server tool whose config declares `capability: network` (MCP tools default to `execute`, the most restrictive class, unless the server config says otherwise — see [configuration.md](configuration.md)). Does not affect `shell` (even if shell runs curl — that's why container sandboxing matters).

### `network_allowlist`

Restrict outbound network-capability tool calls to specific domains:

```yaml
security:
  network_allowlist:
    - "api.github.com"
    - "registry.npmjs.org"
    - "pkg.go.dev"
    - "docs.anthropic.com"
```

An empty list (`[]`) means unrestricted.

**What's checked:** The URL's hostname is matched against the allowlist. Subdomains are matched exactly — `api.github.com` does not match `github.com`.

**Scope:** Applies to `web_fetch` and `web_search`. Does not restrict `shell`.

---

## Audit Trail

Every tool call and permission decision is recorded to a JSONL audit trail:

**Location:** `~/.local/share/aegis/audit/<session-id>.jsonl`

**Entry format:**
```json
{
  "ts": "2026-07-02T10:00:00Z",
  "session_id": "abc12345",
  "tool": "shell",
  "capability": "execute",
  "input": {"command": "npm install malicious-package"},
  "decision": "ask_denied",
  "rule": null
}
```

**Decision values:**
| Value | Meaning |
|-------|---------|
| `allow` | Allowed by mode or rule, no prompt |
| `deny` | Denied by rule or mode, no prompt |
| `ask_approved` | Prompted; user approved |
| `ask_denied` | Prompted; user denied |

The audit trail is append-only and persists when sessions are deleted. Keep it for compliance and incident review.

---

## Permission Rules as Security Controls

Fine-grained permission rules in `config.yaml` are a practical first layer of defense:

```yaml
permission:
  mode: build
  rules:
    # Prevent destructive operations
    - "deny shell(rm -rf *)"
    - "deny shell(*--force*)"
    - "deny shell(*truncate*)"

    # Block network access from shell
    - "deny shell(*curl*)"
    - "deny shell(*wget*)"
    - "deny shell(*nc *)"
    - "deny shell(*ncat *)"

    # Protect sensitive files
    - "deny write(/etc/*)"
    - "deny write(~/.ssh/*)"
    - "deny write(~/.aws/*)"
    - "deny write(~/.gnupg/*)"
    - "deny write(/usr/*)"
    - "deny write(/var/*)"

    # Block reads of secrets
    - "deny read(~/.ssh/*)"
    - "deny read(~/.aws/*)"
```

See [Permission System](permissions.md) for full rule syntax.

---

## Security-Focused Personas

Several built-in personas are tuned for security work:

| Persona | Best for |
|---------|---------|
| `security` | Security platform architecture, threat modeling, capability research |
| `security-architect` | Security architecture, design review, threat modeling |
| `security-engineer` | Security tooling, vuln management, automation, incident response |
| `appsec-engineer` | OWASP testing, secure code review, CI/CD security integration |
| `security-researcher` | Vulnerability research, attack analysis, MITRE ATT&CK |
| `cloud-security-engineer` | Cloud posture, IAM, CIS Benchmarks, cloud-native security |
| `network-security-architect` | Network segmentation, zero-trust, threat analysis |
| `risk-assessor` | NIST RMF, ISO 27005, FAIR risk assessments |

```bash
aegis --persona appsec-engineer --mode plan
```

---

## Recommended Security Configuration

For a hardened local development setup:

```yaml
permission:
  mode: build
  rules:
    - "allow bash(go test ./...)"
    - "allow bash(go build ./...)"
    - "allow bash(git status)"
    - "allow bash(git diff*)"
    - "allow bash(git log*)"
    - "deny shell(rm -rf *)"
    - "deny shell(*curl*)"
    - "deny shell(*wget*)"
    - "deny write(/etc/*)"
    - "deny write(~/.ssh/*)"
    - "deny write(~/.aws/*)"

security:
  egress_then_write: true
  network_allowlist:
    - "pkg.go.dev"
    - "api.github.com"

sandbox:
  backend: auto       # use container if available
  network: false      # no egress from containers

cost:
  budget_usd: 10.0    # abort runaway sessions
```

For CI/CD (unattended, fully sandboxed):

```yaml
permission:
  mode: auto

sandbox:
  backend: container
  runtime: docker
  image: "ubuntu:22.04"
  network: false

security:
  egress_then_write: true
  network_allowlist:
    - "registry.npmjs.org"
    - "pkg.go.dev"

cost:
  budget_usd: 5.0
```

```bash
aegis chat "run the test suite and fix any failures" --yes
```
