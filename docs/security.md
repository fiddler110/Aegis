# Security Features

Aegis includes several security-focused capabilities: a security scanning tool, pluggable sandbox backends for shell execution isolation, and contextual policies that control tool behavior at runtime.

---

## Security Scanning

The `security_scan` tool and `aegis scan` command run available security scanners against your codebase and produce a normalized findings report.

### CLI usage

```bash
aegis scan .                      # scan current directory
aegis scan ./src                  # scan a specific path
```

### Tool usage

The agent can call `security_scan` directly:

```json
{
  "path": ".",
  "tools": ["semgrep", "trivy", "gitleaks"]   // optional: run a subset
}
```

### Scanners

| Scanner | What it finds | Requires |
|---------|--------------|---------|
| **Semgrep** | SAST: code patterns, injection, auth issues, insecure APIs | `semgrep` in PATH |
| **Trivy** | Vulnerabilities in dependencies (Go, npm, pip, etc.) and containers | `trivy` in PATH |
| **Gitleaks** | Secrets and credentials accidentally committed | `gitleaks` in PATH |

Aegis runs whichever scanners are installed and skips the rest with a note.

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

| Property | Container sandbox |
|----------|------------------|
| Process isolation | Yes |
| Filesystem isolation | Yes — workspace is mounted read-write; host is not accessible |
| Network isolation | Optional (`network: false` blocks container egress) |
| Root access on host | No |

**Path validation:** The sandbox backend validates that shell commands can't escape the workspace mount point.

**SSRF protection:** The `web_fetch` and `web_search` tools independently reject private IP addresses (10.x, 172.16–31.x, 192.168.x, 127.x, ::1) regardless of sandbox backend.

### When to use

- **Any time the agent runs untrusted code** (executing downloaded scripts, building user-provided packages)
- **Enforcing network egress restrictions** — `network: false` prevents `curl`, `wget`, etc. from reaching the internet even if the permission rules allow `shell`
- **CI/CD pipelines** — run `aegis chat --yes` with a container sandbox for safe automated runs

### Startup warning

When `auto` mode runs with the local sandbox and the shell tool is present alongside network policies (`network_allowlist` or `egress_then_write`), the daemon logs a startup warning:

```
WARN: network_allowlist is set but sandbox.backend=local — the shell tool can still
reach the network directly. Set sandbox.backend=container with network=false for
enforced egress isolation.
```

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

**What counts as network:** Tool calls with the `network` capability — `web_fetch`, `web_search`, MCP server calls. Does not affect `shell` (even if shell runs curl — that's why container sandboxing matters).

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
