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

### Scanners

| Scanner | What it finds | Host binary |
|---------|--------------|---------|
| **Semgrep** | SAST: code patterns, injection, auth issues, insecure APIs | `semgrep` |
| **Trivy** | Vulnerabilities in dependencies (Go, npm, pip, etc.), IaC misconfig (Terraform/CloudFormation/K8s/Helm/Dockerfile/ARM), secrets | `trivy` |
| **Gitleaks** | Secrets and credentials accidentally committed | `gitleaks` |
| **Kubescape** | Kubernetes manifest/Helm chart misconfigurations, mapped to NSA/MITRE/CIS framework controls with real severity | `kubescape` |
| **Hadolint** | Dockerfile lint (any `Dockerfile`/`Dockerfile.*`/`*.dockerfile` found in the scanned path) | `hadolint` |

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
