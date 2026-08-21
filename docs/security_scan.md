# Security Features

Aegis includes several security-focused capabilities: static/dependency/secrets scanning (`security_scan`), dynamic web-app testing (`dast_scan`, OWASP ZAP), network/host reconnaissance (`recon_scan`, nmap + Nuclei), a persistent multi-day engagement notebook plus NVD CVE lookups and guarded next-step suggestions (`security_advise`), pluggable sandbox backends for shell execution isolation, and contextual policies that control tool behavior at runtime.

---

## Security Scanning

The `security_scan` tool and `aegis scan` command run available security scanners against your codebase and produce a normalized findings report.

### CLI usage

```bash
aegis scan .                                  # scan current directory
aegis scan ./src                              # scan a specific path
aegis scan --scanner trufflehog               # run only trufflehog, force-enabled for this run
aegis scan --scanner secrets ./src             # run only the "secrets" category (gitleaks + trufflehog)
aegis scan image alpine:3.20                  # scan a container image by reference (see below)
aegis scan sbom .                             # generate a CycloneDX SBOM via syft (see below)
```

Every findings scan (plain path or `--scanner`-filtered) is written to
`.aegis/security/scan.json` under the scanned path — see [Persisted reports](#persisted-reports)
below.

### Picking specific scanners, or letting Aegis pick for you

A plain scan with no `--scanner` filter does three things beyond "run every enabled scanner":

1. **Language auto-detection** — it looks for `go.mod`/`*.go`, `requirements.txt`/`*.py`,
   `Gemfile`/`*.rb`, `package.json`/`*.js`/`*.ts`, and several more languages (Rust, Java, C/C++,
   C#, PHP, Kotlin, Swift — shown for information even though most have no dedicated engine yet;
   opengrep's general multi-language SAST already covers them) under the scanned path and
   auto-enables the matching opt-in language-specific SAST engine (gosec/bandit/brakeman/njsscan)
   for that run — no config change needed, and a pure Rust or Java repo never triggers bandit.
   This never overrides an explicit `security.tools.<name>.enabled` you've already set, in either
   direction.
2. **File-relevance gating** — hadolint (Dockerfile lint) and kubescape (Kubernetes manifest scan)
   are skipped, with a reason ("no Dockerfile found in workspace" / "no Kubernetes manifests found
   in workspace"), when the scanned path has nothing for them to analyze, rather than running a
   binary that would just report zero findings every time. An explicit `--scanner hadolint` (or
   `security.tools.hadolint.enabled: true`) always bypasses this and runs it anyway.
3. Everything already enabled by default or via config still runs, as before.

At a real terminal, a plain `aegis scan` (no `--scanner`, no `--yes`) previews this auto-detected
plan — the languages it found and exactly which scanners would run or be skipped and why — and
asks for confirmation before running anything:

```
$ aegis scan
Detected: go (gosec)

Planned scan:
  ->  opengrep    host
  ->  gosec       host
  --  bandit      skip: opt-in tool, not enabled by default — ...
  --  hadolint    skip: no Dockerfile found in workspace
  --  kubescape   skip: no Kubernetes manifests found in workspace
  ...

Run this plan? [Y/n, or a comma-separated scanner/category list, e.g. "secrets" or "gitleaks,trufflehog"]
```

Press Enter/`y` to run it, `n` to abort, or type a scanner/category list to run something else
instead. Pass `--yes` to skip the prompt and run the plan immediately — the default in
non-interactive contexts (CI, scripts, piped stdin) either way, where the prompt never appears.

Pass `--scanner <name-or-category>` (repeatable, or comma-separated in the TUI) to instead run
**only** specific scanners, force-enabled for this run regardless of config or relevance — no
prompt, since naming scanners explicitly is already a deliberate choice — the same way
`aegis scan image` already runs its own distinct, explicitly-requested scanner set:

```bash
aegis scan --scanner trufflehog          # exact scanner name
aegis scan --scanner secrets             # category alias: gitleaks + trufflehog
aegis scan --scanner gitleaks --scanner trufflehog   # equivalent to --scanner secrets
```

Recognized category aliases: `secrets` (gitleaks, trufflehog), `sast` (opengrep, gosec,
bandit, brakeman, njsscan), `sca`/`deps` (osv-scanner, grype), `iac` (kubescape, hadolint),
`misconfig` (kubescape, hadolint, trivy). An unrecognized name/category is rejected with the full
valid list rather than silently running everything or erroring opaquely.

Run `aegis scan --list` (or `/scan list` in the TUI) to see every valid `--scanner` name and
category alias, each with its category, whether it runs by default, and its live availability
(on PATH / via a container / unavailable and why) — the reference for what you can pass to
`--scanner`/`/scan <selector>` right now on this machine:

```
$ aegis scan --list
SCANNER      CATEGORY                DEFAULT  STATUS
gitleaks     Secrets                 enabled  on PATH
trufflehog   Secrets                 opt-in   trufflehog not installed and no container image configured — ...
...

Category aliases (--scanner <alias> runs every scanner in the group):
  secrets    -> gitleaks, trufflehog
  sast       -> bandit, brakeman, gosec, njsscan, opengrep
  ...
```

### TUI usage

`/scan` runs the same scan directly from inside a session, without spending a model turn — the
daemon runs it against its own workspace and prints the formatted report straight into the
transcript:

```
/scan                          # preview the auto-detected plan for the whole workspace
/scan confirm                  # run the previewed plan
/scan src                      # preview the plan for a workspace-relative subdirectory
/scan src confirm              # run it
/scan trufflehog                # run only trufflehog immediately, force-enabled, no preview
/scan secrets                  # run only the "secrets" category immediately, no preview
/scan gitleaks,trufflehog src   # comma-separated selector list + a path, immediately, no preview
/scan list                     # list every valid scanner name/category, with live availability
/scan image alpine:3.20        # scan a container image reference instead
/scan sbom                     # generate a CycloneDX SBOM instead of a findings report
```

A bare `/scan` (or `/scan <path>`) doesn't run anything on the first call — like
`/security install <tool>`, it shows what it *would* do (detected languages, and each scanner's
planned run/skip with a reason, including the file-relevance gating described above) and asks you
to re-run with a trailing `confirm` — `/scan confirm`, or `/scan <path> confirm` to keep the same
path scope. Naming a scanner/category explicitly is already a deliberate choice, so that skips the
preview and runs immediately, as before.

`/scan`'s first argument is treated as a scanner/category selector only when *every*
comma-separated token in it resolves to a known scanner name or category — otherwise it's treated
as a literal path, so `/scan src` still means "scan the src directory," not "run the (nonexistent)
src scanner."

A scan can take a while (container fallback pulls, multiple scanner binaries), so give it a
minute on a cold run. Use `/security-config` first to enable/install the scanners you want
included — a tool that's off or not installed shows up as "skipped," not scanned.

### Tool usage

The agent can call `security_scan` directly:

```json
{
  "path": "."   // optional: workspace-relative subdirectory; defaults to the whole workspace
}
```

Restrict it to specific scanners or categories:

```json
{
  "scanners": ["trufflehog"]   // or ["secrets"], ["sast"], etc. — force-enabled for this run
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

### Persisted reports

Every findings scan (path, image, or network) persists its report as JSON under
`.aegis/security/` — `scan.json` for a path scan, `image.json` for `aegis scan image`/`/scan
image`, `network.json` for `aegis scan network`/`/scan network`/`recon_scan`, `dast.json` for
`aegis scan dast`/`dast_scan`. Each file is overwritten on every run (the latest result, not a
growing history) — the same posture `.aegis/sbom.cdx.json` already uses for SBOMs. This means a
scan's findings survive past whatever ephemeral output captured them (terminal scrollback, a
model turn) and are diffable/greppable/scriptable afterward.

Threat models produced by the `threat-modeling` skill (`/threat-model`, or the unattended
`aegis chat "…" --skill threat-modeling --mode build --yes`) live in the same family: the skill
writes each run to its own **directory**,
`.aegis/security/threat-model/<framework>-<target>-<YYYY-MM-DD-HHMM>/` — the target slug (scoped
feature name, or the repo name for a whole-project model) and timestamp are both mandatory, so the
directory listing alone says what each run modeled and when, and two same-day runs never collide.
The directory holds the seven-file report suite `scaffold.py` lays down and the phased drive fills:
`0-assessment.md` (executive summary), `0.1-architecture.md`, `1.1-model.mmd` + `1-model.md` (the
DFD), `2-<framework>-analysis.md`, `3-findings.md`, and an `inventory.yaml` sidecar with stable
component/threat IDs. Unlike the scan JSONs these are *not* overwritten — each update is a new dated
directory diffed against the previous one (the sidecar is what makes the "what changed since the last
threat model?" re-run cheap, via `diff_inventory.py`), so the parent directory is the model's
history, not just its latest state.

### Scanners

| Scanner | What it finds | Host binary | Enabled by default? |
|---------|--------------|---------|---|
| **opengrep** | SAST: code patterns, injection, auth issues, insecure APIs | `opengrep` | Yes |
| **gosec** | Go-specific SAST (crypto misuse, SQL injection, hardcoded creds) | `gosec` | No — opt-in |
| **Bandit** | Python-specific SAST | `bandit` | No — opt-in |
| **Brakeman** | Ruby on Rails-specific SAST | `brakeman` | No — opt-in |
| **njsscan** | Node.js-specific SAST (no working Windows host build — see below) | `njsscan` | No — opt-in |
| **Trivy** | Vulnerabilities in dependencies (Go, npm, pip, etc.), IaC misconfig (Terraform/CloudFormation/K8s/Helm/Dockerfile/ARM), secrets | `trivy` | Yes |
| **Gitleaks** | Secrets and credentials accidentally committed | `gitleaks` | Yes |
| **trufflehog** | Secrets and credentials, with optional live verification against the real provider API — see below | `trufflehog` | No — opt-in |
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

### SCA scope: dev dependencies are scanned

The three SCA scanners (trivy, osv-scanner, grype) all scan **development dependencies**, not
just runtime ones. That's worth stating because it isn't trivy's own default: `trivy fs` skips
npm/yarn/gradle dev dependencies unless `--include-dev-deps` is passed, which Aegis passes.

The reasoning is scanner *agreement* rather than maximal findings. osv-scanner includes dev
dependencies with no way to turn that off, so leaving trivy's default alone doesn't spare
anyone the findings — it only means the two scanners disagree about what the scan covers, and
that the ecosystem where they'd otherwise corroborate each other (and dedup, see below) is
covered by one of them alone. On this repo's frontend lockfile, trivy's default catalogs 1 of
140 packages (139 of them are devDependencies); on a lockfile with known-vulnerable dev
dependencies, that default reports 0 findings where osv-scanner reports 8 — including a
CRITICAL.

A dev-dependency CVE is genuinely lower-severity than a runtime one — it doesn't ship to
production. It is not *no* severity: build and test tooling executes on developer machines and
in CI, with repo and often credential access. Judge these findings by that standard rather
than filtering them out sight-unseen; `--severity`-style triage belongs in the report, not in
a scanner's collection scope, because a scanner that never catalogs a package cannot tell you
anything about it later.

### SAST: opengrep, plus language-specific engines opt-in

**opengrep** (a community-governed semgrep fork — no login/telemetry, openly-licensed rules)
is the SAST engine. It runs with **explicitly pinned rule packs** (`p/owasp-top-ten`,
`p/security-audit`) rather than `auto`, for reproducibility and supply-chain hygiene:
`--config auto` needs network egress, nudges toward a platform login, and resolves whatever
the registry currently serves for that pack name rather than a fixed rule set.

Four opt-in, language-targeted engines cover what the multi-language core misses:
**gosec** (Go), **bandit** (Python), **brakeman** (Ruby on Rails), **njsscan** (Node.js).
They're listed in `aegis security status` like every other scanner but resolve to
"opt-in tool, not enabled by default" until turned on:

```yaml
security:
  tools:
    gosec:
      enabled: true    # this is a Go project
```

**njsscan on Windows has no usable host build.** Its engine (libsast) stubs out its own
analysis call on Windows and then crashes on the stub, so the host binary fails with a Python
`AttributeError` on every project, with or without JavaScript in it. This is a property of
njsscan itself, not of the underlying engine, which runs on Windows fine — libsast never asks
whether it's available. Aegis therefore never routes njsscan to the host on Windows: a normal
`method: auto` (the default) resolves it to the **container**, which runs it correctly on the
same machine. Only an explicit `security.tools.njsscan.method: host` fails, and it reports
that reason rather than a traceback. `aegis security install njsscan` on Windows says the
same thing instead of installing a binary that cannot work.

Or interactively: `/security-config` (TUI) or `aegis security config`. The TUI wizard also
installs a tool for you — pick it from the list, choose "Install now (guided)," and confirm the
exact host command it's about to run (the same guided install `aegis security install <tool>`
does from the CLI); the list refreshes afterward so you can see it move from "unavailable" to
"on PATH" without leaving the dialog.

### trufflehog: live secret verification (opt-in)

**trufflehog** runs alongside gitleaks, not instead of it — the same (rule/CVE, location)
finding flagged by both is deduped into one (P11.8), tagged `[also flagged by: ...]`. Its
differentiator is **live verification**: 800+ detectors can call the real provider API
(AWS, GitHub, Slack, etc.) to confirm a found credential is still active, which cuts triage
noise sharply versus pattern/entropy matching alone — a verified AWS key is unambiguously
urgent; an unverified one might be a fixture, a revoked credential, or a false positive.

Verification is a separate, explicit, host-only opt-in — **off by default**, and trufflehog
itself always runs with `--no-verification` unless you turn it on:

```yaml
security:
  tools:
    trufflehog:
      enabled: true    # opt-in, like gitleaks' predecessor pattern-matchers
      verify: true     # opt-in: makes real calls to third-party provider APIs
```

**Read this before enabling `verify`:**

- It sends the actual discovered secret to the credential's own provider (AWS STS, GitHub's
  token-introspection endpoint, etc.) to check whether it's live — this is a real network call
  using real (if compromised) credentials, not a local check. Treat it with the same care as
  any other outbound call using sensitive material.
- It is **host-only**: the scanner-container runner is network-isolated (`--network none`,
  the same hardening every scanner container gets), so `verify: true` forces
  `security.tools.trufflehog.method: host` — `Resolve` refuses container mode outright rather
  than silently dropping verification or punching a network hole through that isolation.
- `/security-config` (TUI) and `aegis security config` show `verify` as its own explicitly
  warning-labelled toggle, separate from the tool's regular enabled/method/image settings.
- A finding trufflehog verified renders with a `[VERIFIED: confirmed active credential]` tag
  in the report (`verification: "verified"` in JSON) — treat this as the strongest possible
  signal to rotate the credential immediately, and don't baseline-suppress a verified finding
  without a very good reason.

**Licensing:** trufflehog is **AGPL-3.0** licensed, unlike gitleaks' MIT license. Aegis only
shells out to a separately-installed trufflehog binary (no code linking), so this isn't a
license-compatibility concern for Aegis itself — but it's worth knowing before you install and
run it, since AGPL's network-use copyleft terms may matter for how *you* distribute or operate
software that bundles it.

### Reachability: is the vulnerable code actually called?

A dependency CVE means the vulnerable *package* is present — not necessarily that your code
ever calls the vulnerable *function*. osv-scanner runs with `--call-analysis=all`, which
does call-graph analysis to tell the difference: for Go it's on by default and is
govulncheck under the hood (the same analysis `go vet`-adjacent tooling uses); Rust and
Java JAR analysis are experimental upstream. No other scanner Aegis integrates (trivy,
grype, opengrep) has an open-source equivalent as of this writing, so their findings carry
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

### Network / Host Reconnaissance (nmap + Nuclei)

DAST tests one already-known web application. `aegis scan network <target> [<target>...]` /
`security_scan` (via the standalone `recon_scan` tool) works the other direction — attack-surface
*mapping*: given a bare host, IP, or CIDR range (no `http(s)://` needed), it discovers what's
actually there. Two scanners run per call:

- **nmap** finds live hosts, open ports, and service/version banners.
- **nuclei** matches ProjectDiscovery's community template library (CVEs, misconfigurations,
  exposed panels, raw network checks) against whatever nmap found alive.

```bash
aegis scan network 192.168.1.0/24                 # baseline: top-100-port scan + safe nuclei templates
aegis scan network 192.168.1.10 db.lan             # multiple targets in one call
```

In-session: `/scan network <target> [target...]` runs the same scan through the daemon
(`POST /security/scan` with `targets`) without spending a model turn — same shared
`security.dast.allowed_targets` gate; a disallowed target is rejected before either scanner runs.

This reaches out to real network hosts — attack-surface mapping against something you don't own
would be reconnaissance for an attack. `recon_scan` shares its target-authorization gate with
`dast_scan` (they're the same policy, `internal/security/target.go`'s `isHostAllowed`, not two
gates to keep in sync) and adds its own multi-host and template-pinning checks — all running
**unconditionally, independent of permission mode**:

1. **Target authorization (`security.dast.allowed_targets`)** — every target's host must be
   loopback/RFC-1918 private (allowed by default — the common "scan my home lab" case needs no
   config) or explicitly declared, exactly like DAST's gate above. **Every target in the list is
   checked individually** — one undeclared host in an otherwise-authorized list fails the whole
   call, not a partial silent skip.
2. **Target count cap** — a single call accepts at most 256 targets; a longer list is rejected
   outright rather than silently truncated.
3. **Active-mode opt-in (`security.dast.allow_active`)** — the same flag DAST's `--mode active/api`
   requires. Baseline (default): nmap runs a top-100-port version-detection scan with no OS
   fingerprinting or scripts, and nuclei excludes `dos`/`fuzz`/`intrusive`-tagged templates. Active:
   nmap adds OS detection, the full port range, and its default-safe NSE script category; nuclei
   runs its full template set.
4. **Nuclei template pinning (`security.tools.nuclei.templates_version`)** — templates are
   executable network-probe logic, so nuclei never runs against an implicit "latest" pull (P7.6's
   posture: a rule/template pack is itself supply-chain attack surface). Set this to a
   `nuclei-templates` release tag (e.g. `"v9.9.0"`); the tag is shallow-cloned once into a local
   cache and reused after that. Missing this config reports nuclei skipped with that exact reason.
5. **Normal execute-tool approval** — `recon_scan`'s capability is `execute`, same as
   `dast_scan`/`security_scan`/shell: blocked outright in `plan` mode, prompts for approval in
   `build` mode.

```yaml
security:
  tools:
    nuclei:
      enabled: true
      templates_version: "v9.9.0"   # a real nuclei-templates release tag
  dast:
    allowed_targets:
      - staging.example.com    # shared with DAST — see above
    allow_active: false
```

Nmap's open-port findings beyond plain "port is open": a small curated table of commonly-risky
services (Telnet, FTP, unauthenticated Redis, an exposed Docker API, SMB, RDP, VNC, Elasticsearch,
etc.) is flagged `MEDIUM`/`HIGH` with a specific remediation rather than left at `INFO` — the
concrete "identify weaknesses" step beyond a bare port list.

**No container fallback for either scanner.** Reaching LAN targets needs real network egress,
which the source-scanning container runner deliberately denies (`--network none`, the same
hardening every other scanner container gets); punching a network hole through that posture for
two more tools isn't done here (same reasoning DAST's ZAP container already documents, and the
same "host-binary only for now" precedent as image scanning below).

**On Windows, prefer a Kali WSL distro over the native install.** Native Windows nmap needs Npcap
installed and running as a service, plus admin rights for `-O`/SYN-style scans — exactly the
"all kinds of errors" territory Windows operators commonly hit. `nmap` and `nuclei` are both
`WSLCapable` (see [WSL fallback](#wsl-fallback-for-tools-with-no-native-windows-build-or-an-unreliable-one)
below): install [WSL](https://learn.microsoft.com/en-us/windows/wsl/install) with a
security-focused distro —
[Kali Linux](https://www.kali.org/docs/wsl/win-kex/) (`wsl --install -d kali-linux`) ships nmap,
nuclei, and a broader red-team toolkit (metasploit, hydra, nikto, sqlmap) already installed — then
point Aegis at it and force the WSL method:

```yaml
security:
  wsl_distro: kali-linux        # target this distro instead of the WSL default
  tools:
    nmap:   { enabled: true, method: wsl }
    nuclei: { enabled: true, method: wsl, templates_version: "v9.9.4" }
```

`aegis security status` reports `wsl` in its METHOD column ("via WSL") once this resolves
correctly. A bare Ubuntu WSL default distro has neither tool installed by default — either
install them there (`apt install nmap`, per nuclei's own install docs) or set `wsl_distro` to a
distro that already has them, like Kali.

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
    opengrep:
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

To install every scanner at once instead of one at a time, run the opt-in action 3 in the platform
build script (`./build-linux.sh 3`, `./build-macos.sh 3`, `.\build-windows.ps1 3`) — it loops this
same `aegis security install <tool> --yes` over every scanner descriptor using the binary the
script just built. It's never included in the scripts' `all` selection since it's a privileged,
host-modifying action across many tools; see [installation.md](installation.md#what-the-build-script-does).

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

### The multiscanner image: one image instead of sixteen

Pinning an image per tool is accurate but tedious, and the section above is why the
container path resolves to "unavailable" out of the box for everything. The alternative is
to build **one local image carrying every scanner**:

```bash
aegis security build-image                   # full profile, auto-detected runtime
aegis security build-image --profile core    # static binaries only, much smaller
```

That builds a Containerfile embedded in the binary (source:
`internal/security/multiscanner/`), reads back the resulting image ID, and writes the
config block for you. Then populate the vulnerability databases once:

```bash
aegis security update-db                  # fills the cache volume
aegis security update-db --skip-java-db   # skips trivy's ~1.4GB Java DB
```

```yaml
security:
  multiscanner:
    enabled: true
    image: "localhost/aegis-multiscanner:v1"
    image_id: "sha256:..."      # written by build-image; re-verified before every run
    runtime: podman             # the image only exists in this runtime's storage
    concurrency: 3              # how many scanners run at once
    tools: [...]                # what the built profile actually carries
```

Resolution precedence is: an explicit `security.tools.<name>.image` wins, then the
multiscanner image (for tools it carries), then nothing. Host binaries still win under
`method: auto`, so building the image doesn't take a scan away from a tool you already
have installed — set `security.default_method: container` if you want the image used
even when a host binary exists.

**Profiles.** `core` carries the statically-linked scanners (trivy, gitleaks, trufflehog,
syft, grype, osv-scanner, kubescape, hadolint, opengrep). `full` (the default) adds the
Python engines (bandit, njsscan), Ruby (brakeman), Go (gosec, plus the Go toolchain it
needs — see below), and the network scanners (nmap, nuclei).

**Size.** The full image is ~1.8GB. The vulnerability databases are deliberately *not* in
it — baked in they were ~3.7GB of a 5.8GB image. They live in a podman/docker volume
(`aegis-scanner-cache`) instead, which is also what makes runtime fetching viable at all:
scanner containers run with `--rm`, so without a persistent cache every scan would
re-download trivy's ~1.2GB database from scratch.

That volume is a single named volume shared by every scan in every project — nothing is
duplicated per-project, so you don't accumulate GBs per repo. Inspect or reclaim it with
`podman system df -v` / `podman volume inspect aegis-scanner-cache`.

**Not included, deliberately.** Each exclusion has a reason, recorded in
`multiscannerExcludedTools` (`internal/security/multiscanner.go`) so `aegis scan` can
explain itself rather than suggesting a rebuild that wouldn't help:

- **zap** — a large Java app with a different mount contract; keeps its own image.
- **dockle** — needs the container **engine socket** to inspect an image through the local
  engine. That's a different privilege axis from network egress: socket access is
  effectively host root. It runs from a host binary (`aegis security install dockle`).

The image scanners (`trivy image`, `grype <ref>`) and the network scanners (nmap, nuclei)
are *not* in this list — they moved to a second image; see
[The netscanner image](#the-netscanner-image-network-on-workspace-never) below.

**gosec: two phases, not an exclusion.** gosec used to be excluded here, and the reason
was real: it's a compile-assisted analyzer that resolves packages via `go list`, needing a
Go toolchain and module downloads, which `--network none` can't provide — and it doesn't
fail when it can't. Measured on this repo (gosec 2.28.0):

| | findings | exit |
|---|---|---|
| host binary | 244 | 0 |
| no Go toolchain | **0** | 0 |
| toolchain, cold module cache | **258** | 0 |
| toolchain, warm module cache | 283 | 0 |

The third row is the one to read twice, and it's why the fail-closed rule below exists.
With no modules to resolve, `go list` leaves packages with type errors, gosec logs
"skipping SSA analysis" to stderr and continues with the AST-only rules — so every
type-aware rule silently stops firing (G115 3→0, G118 5→0, G124 1→0, G702 1→0, G122 5→1,
G703 13→2) while the total still looks perfectly healthy. A zero draws the eye. A confident
258 does not.

What unblocked it is not relaxing `--network none`. It's the same split `update-db`
already uses — *the phase with network does no analysis, and the phase that analyzes has
no network*:

| gosec phase | network | workspace |
|---|---|---|
| 1. warm (`go mod download`) | **yes** | mounted **read-only** |
| 2. analyze (`gosec ./...`) | no (`--network none`) | mounted, as every scanner |

`go mod download` fetches modules but never executes them, and it can't write to the
source tree, so the exposure is materially smaller than a general network-plus-workspace
grant. Modules (and any toolchain `GOTOOLCHAIN=auto` pulls for a repo on a newer Go) land
in the `aegis-scanner-gocache` volume, which phase 2 reads offline.

**A failed warm phase aborts gosec rather than downgrading it.** That's the whole point: a
warm phase that quietly gives up hands gosec a cold cache, and the table above is what a
cold cache produces — not an obvious zero but a confident, wrong-by-25 report with a whole
class of rules missing, which nothing downstream can tell apart from a good run. You'll see
the fetch error instead.

#### Why an image ID rather than a digest pin

A locally-built image has no registry digest — `RepoDigests` stays empty until an image is
pushed or pulled — so the `image@sha256:...` reference every other scanner image must
carry cannot exist for one. Rather than waive that rule, the multiscanner path replaces it
with a stronger check: the image's real ID is read back via `image inspect` and compared
against `image_id` before the first container run of a scan. Rebuild or retag the image
behind Aegis's back and scans fail closed:

```
multiscanner image localhost/aegis-multiscanner:v1 no longer matches the ID recorded in
config (have a1b2c3d4e5f6, expected 1111222233 44) — it was rebuilt or retagged; re-run
`aegis security build-image` to re-pin it
```

That verifies the actual image content, which is what the digest rule was reaching for; a
regex over a reference string never inspects the image at all.

#### One networked run, and only one

Databases have to be fetched from somewhere. The split is:

| | network | workspace mounted |
|---|---|---|
| `aegis security update-db` | **yes** | no |
| gosec phase 1 (`go mod download`) | **yes** | **read-only** |
| every scan | no (`--network none`) | yes |

`update-db` runs the image's `aegis-update-db` script with networking on and *no* source
mounted, writing trivy's and osv-scanner's databases into the cache volume. Scans mount
that volume read-write but keep `--network none`, so the workspace is never mounted into a
container that can reach the network. Re-run `update-db` whenever you want fresher data —
the databases are only as current as its last run.

Two things stay baked into the image, because they're small and pinning them is the point:
the SAST rule packs (~1.9MB) and the nuclei template set. Both would otherwise be fetched
at scan time — a "pinned" registry pack like `p/owasp-top-ten` is still an HTTP fetch from
semgrep.dev, which is exactly why opengrep failed against a network-less container before
they were baked.

Every scanner version in the image is pinned by an `ARG` and its download checked against
the checksum its project publishes. That catches corruption and wrong asset names, not a
compromised upstream — the checksum ships from the same release as the artifact. The
image-ID pin is what actually binds Aegis to a specific image. See
`internal/security/multiscanner/README.md` for how to bump a scanner version.

### The netscanner image: network on, workspace never

Four tools don't fit the image above, and not because of what they *are* — because of what
they need to **see**. `nmap` and `nuclei` scan a host list. `trivy image` and
`grype <ref>` scan a container image by reference. All four need network egress; none of
them needs your source code.

That distinction is the whole design. Network *plus* workspace in one container is an
exfiltration path out of a hostile repo, which is why the multiscanner refuses it. But
these four have nothing to exfiltrate — they never see the workspace. So they get their own
image, run with **network on and no workspace mount, ever**:

```bash
aegis security build-image --netscanner    # ~570MB; no update-db step needed
aegis security verify-image --netscanner   # needs network — that's what's being verified
```

```yaml
security:
  netscanner:
    enabled: true
    image: "localhost/aegis-netscanner:v1"
    image_id: "sha256:..."      # same ID pin, same fail-closed check as the multiscanner
    runtime: podman
    tools: [grype, nmap, nuclei, trivy]
```

The invariant is **structural, not conventional**: the netscanner's runner
(`runNetscannerImage`) takes a target argument and has no directory parameter at all, so
there is nothing a future call site could pass. The multiscanner's runner keeps its
directory parameter and its `--network none`. The two never converge, and a test asserts
both halves.

Both images are built from **one** embedded build context (`--target netscanner` selects
the stage), so they share one fetch script, one set of pinned tool versions, and one source
fingerprint — `aegis security build-image` can't drift from `--netscanner`.

**What this replaces.** Before it, `aegis scan --image` and the `recon_scan` tool reported
"container fallback ... is not yet supported — install this tool natively instead", even on
Windows where nmap and nuclei were *already sitting inside the multiscanner image the
operator had built* and reachable only by provisioning a Kali WSL distro. Both now resolve
to the container first, falling back to a host binary (and saying so) when it isn't
available.

**No `update-db` for this one.** It has network, so trivy and grype refresh their own
databases into a separate volume (`aegis-netscanner-cache`). Deliberately separate: pointing
both images at one volume would put a networked writer inside the cache every offline scan
reads.

**Capabilities.** Runs are `--cap-drop=ALL --security-opt=no-new-privileges` like every
other scanner run, with exactly one exception: nmap gets `--cap-add=NET_RAW`, without which
it silently degrades to a TCP connect scan and refuses OS detection outright.

**Still not included.** `zap` keeps its own official image and its `/zap/wrk` mount
contract — it already needs no host install, so folding a large Java app in here would buy
nothing. `dockle` needs the container engine socket, which is effectively host root rather
than merely egress; whether Aegis should ever mount a container socket is a decision to
make on its own, not a side effect of building this image, so dockle stays host-only and
says exactly that.

**Verification.** `verify-image --netscanner` probes each tool's version, then scans
`debian:11-slim` with trivy and grype, requiring **at least 20** findings (both report ~190).
Choosing that image was not obvious and the measurements are recorded in code: a tiny EOL
Alpine — the obvious pick — makes **trivy report zero on a working scanner**, because Alpine's
security data is per-branch and trivy stops reporting once a branch leaves support
(`alpine:3.14` → trivy 0, `alpine:3.10` → trivy 1, `debian:11-slim` → trivy 190). A floor of
1 against a canary that swings between 0 and 1 would fail a correct image; a floor of 20
against ~190 catches a partial database without being brittle. nmap and nuclei get a version
probe only, reported as skipped with the reason: canarying them would mean standing up a
deliberately vulnerable service to attack.

#### Parallelism

Each container-method scanner is a container, so scans run several at once —
`security.multiscanner.concurrency` (default 3) bounds how many. Set it to `1` for
strictly sequential runs. The report is identical at any concurrency: results are folded
in plan order rather than completion order, so `Scanners run:` never reorders. Live
progress events do interleave now, so a scanner's "start" is no longer immediately
followed by its own "done".

### WSL fallback for tools with no native Windows build (or an unreliable one)

Four built-in scanners are `WSLCapable` — Aegis can run them inside the **Windows Subsystem for
Linux** instead of reporting them unavailable, or instead of a native Windows install that's
known to be unreliable in practice:

- **opengrep** and **kubescape** ship no Windows build at all — only `darwin`/`linux` install
  commands.
- **nmap** and **nuclei** *do* have a native Windows install (`winget`/`scoop`), but nmap in
  particular needs Npcap installed and running as a service plus admin rights for `-O`/SYN-style
  scans — the most common source of the failures that send Windows operators looking for an
  alternative in the first place.

On Windows, if a Linux distro is registered under WSL (`wsl -l -q` lists at least one), Aegis
uses it as a fallback:

- `aegis security install opengrep` (and the `/security-config` "Install now" action) detects
  there's no native Windows entry, checks for a WSL distro, and — if present — runs the
  tool's own Linux install command inside it: the guided-install command you're shown and
  asked to confirm becomes `wsl -- bash -lc '<the same linux install script>'`. (nmap/nuclei
  already have a native Windows entry, so this guided-install path doesn't apply to them —
  install them inside your chosen WSL distro yourself, e.g. `apt install nmap` on Kali/Ubuntu.)
- `aegis security status`/`aegis scan` resolve a `WSLCapable` tool to a third method,
  `MethodWSL` ("via WSL" in `status`'s output), whenever the binary is found inside WSL but not
  on the Windows host — checked after the host binary and container fallback in `auto` mode, so
  a native install or configured container image always takes priority. Set
  `security.tools.<name>.method: wsl` to force it even when a (flaky) native binary is present —
  this is the recommended override for nmap on Windows.
- Execution shares the host filesystem directly (`/mnt/<drive>/...`, via `wslpath`) rather
  than a bind mount — no container runtime involved.

**Targeting a specific distro (`security.wsl_distro`).** By default, Aegis runs these tools
against whatever distro `wsl --set-default` currently points at. Set `security.wsl_distro` to
name a different registered distro instead — the recommended setup on Windows is a distro
purpose-built for security tooling:

```yaml
security:
  wsl_distro: kali-linux   # wsl --install -d kali-linux
```

This applies to every `WSLCapable` tool at once (nmap, nuclei, opengrep, kubescape) — there's no
per-tool distro override. If you split tooling across distros (e.g. Kali for nmap/nuclei, a bare
Ubuntu default for opengrep/kubescape), install every `WSLCapable` tool you use into whichever
single distro `wsl_distro` names, so none of them silently regress to unavailable.

This WSL path is deliberately scoped to just these four tools (`ScannerDescriptor.WSLCapable`):
every other built-in scanner already has a working native Windows install path (even when that
path's own package manager has unrelated problems — see
[installation.md](installation.md#windows-scoop-installs-failing) for the `Get-FileHash`/scoop
issue), so there's no WSL execution branch wired for them, and offering one would silently
misroute execution back to a host binary that doesn't exist. Explicitly forcing WSL for a tool
that doesn't support it (`security.tools.<name>.method: wsl`) reports a clear `MethodNone`
reason instead. If a Linux install script doesn't add its own binary to the WSL distro's
`PATH` (kubescape's doesn't; opengrep's does) `aegis security status` will keep reporting it
unavailable until you add it yourself (`export PATH=$PATH:~/.kubescape/bin` in `~/.bashrc`,
per that installer's own instructions) — the same "installed but not on PATH" situation a
native `go install`/`pipx` install can also leave you in.

### Output format

Findings are normalized across all scanners:

```
Severity: HIGH
Location: internal/server/server.go:142
Rule:     CWE-307 (Brute Force)
Message:  Missing rate limiting on authentication endpoint
Source:   opengrep

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
  — opengrep/gosec/bandit/brakeman/njsscan/ZAP all tag one when they
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

### Engagement Notebook, CVE Lookup & Guarded Suggestions (`security_advise`)

`security_scan`/`dast_scan`/`recon_scan` find things; `security_advise` (P13.4) is bookkeeping and
research support for a multi-day engagement built around what they find. Unlike everything above,
its notebook is scoped to an operator-chosen **engagement name**, not the chat session — notes
persist across sessions and daemon restarts under the daemon's per-user data directory, one
append-only JSONL file per engagement (`internal/security/notebook.go`).

- `action: "note"` appends a timestamped, optionally-tagged note (e.g. `tags: ["recon"]`).
- `action: "list"` (alias `"log"`) returns every note for an engagement, oldest first.
- `action: "cve_lookup"` queries the NVD REST API (`https://services.nvd.nist.gov/rest/json/cves/2.0`)
  by `cve_id` or free-text `keyword`. The unauthenticated public API is rate-limited to roughly
  5 requests/30s; a 403/429 response is surfaced as a clear error naming the limit and how to raise
  it (set `NVD_API_KEY` in the environment — no config field, per this codebase's secrets-from-
  environment-only convention), never a retry loop or a hang.
- `action: "suggest"` returns plain-text next-step suggestions from simple, explainable rules over
  the notebook's own content — e.g. "no recon_scan logged yet" or "a CVE is mentioned but nothing
  documents it." This is **guarded**: it only ever returns text. It never calls `recon_scan`,
  `dast_scan`, or anything else itself, and it is not a second LLM call — every rule is a direct,
  inspectable keyword check, so the model or operator reading the output can see exactly why each
  suggestion fired and stays fully in the loop on whether to act on it.
- `action: "status"` returns a short digest (note count, date range, and how many notes reference
  recon/dast/security_scan/findings/CVE lookups). This is a tool-action fallback rather than a field
  on `/status` (`api.StatusInfo`, `internal/server/server.go`): that endpoint is daemon-global with
  no existing per-entity-keyed field, so a per-engagement digest is one `security_advise` call away
  instead.

Capability is `network` (the CVE lookup's real risk surface — a fixed public host, not
model-supplied, so none of `web_fetch`'s SSRF dialer applies here); the notebook actions are local
file reads/appends, in the same low-risk vein as the always-available `remember` tool. `red-team`,
the security architect persona (`security`), and `security-critic` all carry `security_advise` in
their advisory `Tools` list; `security-arbiter` does not, since an arbiter round explicitly
introduces no new claims or investigation of its own.

---

## Sandboxed Execution

Shell commands (`shell` tool) can run inside containers instead of directly on the host, providing process isolation, filesystem isolation, and optional network isolation.

### Backends

| Backend | Description |
|---------|-------------|
| `local` | Run directly on the host (default) |
| `docker` | Docker containers |
| `podman` | Podman containers (rootless) |
| `wslc` | WSL containers (Windows). Last in the Windows auto-detect order — no hardening flags, no persistent-container surface, and it cannot build the scanner images; pick it explicitly if it's all you have |
| `container` | Apple Containers (macOS) |
| `os` | OS-level isolation without a container runtime: macOS `sandbox-exec` (seatbelt) or Linux `bwrap`. Reads are confined to the workspace plus a toolchain allowlist (P27.18/FIND-19) — see the read-exposure caveat below, this is still a materially weaker guarantee than `container`. |
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
- **Windows:** podman → docker → wslc
- **macOS:** docker → podman → container (Apple Containers)
- **Linux:** docker → podman

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
| Filesystem **read** isolation | Yes — host filesystem is not accessible at all | **Partial (P27.18/FIND-19) — confined to an allowlist, not the whole host.** Seatbelt denies `file-read*` outside the workspace plus a built-in toolchain allowlist (system dirs, `~/go`, `~/.cargo`, `~/.npm`, etc. — see `sandbox.os_extra_read_paths`); bwrap only `--ro-bind`s that same allowlist instead of the whole host root. Credential dirs like `~/.ssh`, `~/.aws`, or `~/.config` are never on the allowlist, so they're unreadable from inside the sandbox even though they exist on the host. Still weaker than `container` (which never mounts the host filesystem at all): a toolchain dir that happens to also hold a stray credential file would still be readable, and the allowlist is necessarily broad enough to cover common toolchains. On macOS the seatbelt allowlist additionally grants read on the root directory `/` and on the `/tmp`, `/etc`, `/var` symlinks (P51.1) — without these, path resolution fails and no command runs at all. These are *literal* entries, not subpaths: they expose the root directory's entry names and the link targets, not the contents of the directories they name |
| Network isolation | Optional (`network: false` blocks container egress) | Optional (`network: false` denies network the same way) |
| Root access on host | No | No |

**Read this before treating `os` as equivalent to `container`:** "sandboxed" is not one property — confining writes, confining reads, and confining network are three independent guarantees. The `os` backend gives you all three, but the read guarantee is an allowlist bounded by what common toolchains need (system dirs plus per-language package-manager caches under `$HOME`), not a hard "nothing outside the workspace" boundary the way writes are. It is a real and useful mitigation, but do not rely on it to protect secrets or credentials readable by the host user if a toolchain cache directory could plausibly also hold one — use `container` for that, or avoid running genuinely untrusted code under `os` mode at all.

**Path validation:** The sandbox backend validates that shell commands can't escape the workspace mount point (this governs the write-confinement above; it is independent of the `os` backend's read allowlist).

**SSRF protection:** The `web_fetch` and `web_search` tools independently reject private IP addresses (10.x, 172.16–31.x, 192.168.x, 127.x, ::1) regardless of sandbox backend.

**Secret env stripping (P7.2):** The `local` and `os` backends run on the host and would otherwise inherit the daemon's full environment, including `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` — a prompt-injected instruction that gets the agent to run `shell` could read them back out, then use `web_fetch` to exfiltrate them to a public host. Both backends always strip those two names before exec; add more via `sandbox.strip_env` (e.g. MCP tokens loaded from `.aegis/.env`). The `container` backend is unaffected — `docker run`/`podman run` never pass host env into the container in the first place.

### Docker/Podman socket privilege equivalence

> **Read this before treating the `docker`/`podman`/`auto` sandbox backend as a privilege boundary on its own.** This is inherent to how Docker and Podman work, not an Aegis bug, and it is not something a container's runtime flags can close.

By the well-documented design of those engines, access to the Docker daemon's socket is **equivalent to local root** on the host, and access to a **rootful** Podman socket is equivalent to the invoking user's full host privileges. This is not specific to Aegis — it applies to any process that can reach the socket, because both engines let a caller with socket access mount the host filesystem, run privileged containers, or otherwise act as root (Docker) or as that user (rootful Podman). Rootless Podman avoids this specific escalation path by running the engine (and everything it launches) as the invoking user with no daemon socket to compromise.

Concretely: **any component that can reach the container backend Aegis is configured to use inherits that privilege level.** If the daemon process, a compromised MCP server, or a sub-agent can talk to the same Docker/Podman socket Aegis uses, it has the same root-or-invoking-user privilege the engine itself has — independent of whatever flags Aegis passes to `docker run`/`podman run`.

**What Aegis already does today** (verified against `internal/sandbox/docker.go`'s `ociRunArgs`, current as of P24.10): every container Aegis spawns via the `docker` or `podman` runtime unconditionally gets `--cap-drop=ALL` and `--security-opt=no-new-privileges`, dropping all Linux capabilities and preventing privilege escalation inside the container. This meaningfully reduces what a compromised process *inside* a spawned container can do.

**What Aegis does not, and cannot, do:** those flags hardens the container's contents; they do nothing about the privilege equivalence of *socket access itself*. That's a property of the Docker/Podman architecture, not of the container Aegis asks them to run — no combination of `docker run` flags closes it. If your threat model includes "something on this host other than Aegis could reach the configured socket," the mitigation is not a run flag but a different engine configuration:

- **Prefer rootless Podman** for the sandbox backend where feasible (`sandbox.backend: container`, `sandbox.runtime: podman`, with Podman installed and running rootless — this is Podman's default mode for a non-root user, and it means there is no root-owned daemon socket to compromise in the first place).
- **Or run a user-namespace-remapped ("userns-remap") Docker daemon** if you're committed to Docker specifically — this maps container UID 0 to an unprivileged host UID, so even full container compromise doesn't yield host root. Rootless Docker (`dockerd-rootless`) is a further step in the same direction as rootless Podman.
- Restrict OS-level access to the socket/group (`docker` group membership, or the Podman socket's file permissions) to only the accounts that need it — the daemon process itself already has this access once `sandbox.backend` is configured to use it, so this is about *other* processes on the same host, not about Aegis.
- **Or put a socket-proxy in front of the Docker socket** (e.g. [`docker-socket-proxy`](https://github.com/linuxserver/docker-socket-proxy) / [`docker-socket-proxy` (Tecnativa)](https://github.com/Tecnativa/docker-socket-proxy)) if you're committed to a rootful Docker daemon and can't move to rootless Podman — point `sandbox.docker_host`-equivalent socket access at the proxy instead of the raw daemon socket, and restrict the proxy to only the container-create/start/stop endpoints Aegis actually needs (`CONTAINERS=1`, `POST=1`), denying the broader Docker API surface (image builds, volume/network management, host-info endpoints) a full socket grants. This narrows what a component that reaches the socket can do without requiring a rootless engine at all — useful when the host's Docker install is managed by something else and switching to rootless Podman isn't an option. Aegis does not ship or manage this proxy itself; it's an external component you place between the socket and anything that talks to it.

Aegis does not auto-detect or enforce rootless-vs-rootful at startup: there is no reliable, version-stable cross-platform signal available from the client side to distinguish a rootless from a rootful Docker/Podman install without a fragile `docker/podman info` parse that would vary across engine versions and isn't verified here. Instead, the daemon logs a one-time informational notice at `INFO` level when it selects the `docker` or `podman` runtime (`sandbox: <runtime> socket access is privilege-equivalent to local root ...`), pointing back at this section — it is not a detection of your specific configuration, just a standing reminder that the property applies whenever these two runtimes are in use.

### When to use

- **Any time the agent runs genuinely untrusted code** (executing downloaded scripts, building user-provided packages) — use `container`. The `os` backend's read confinement is an allowlist sized for legitimate toolchains, not a hard boundary (see above), so a malicious script could still find and exfiltrate a secret stashed inside an allowlisted toolchain directory.
- **Preventing accidental damage from trusted-but-fallible agent output** (writes outside the workspace) without wanting the overhead of a container runtime — `os` is a reasonable, lighter-weight choice here.
- **Enforcing network egress restrictions** — `network: false` prevents `curl`, `wget`, etc. from reaching the internet even if the permission rules allow `shell`
- **CI/CD pipelines** — run `aegis chat --yes` with a container sandbox for safe automated runs

### Startup warning

**Local sandbox, execute-capable tools (P27.14/FIND-04):** whenever the effective backend is `local` and permission mode is not `plan` (i.e. shell/execute tool calls are reachable at all), the daemon logs a persistent `WARN` at startup recommending `os` or `container` instead — the local backend gives no fs/network/process isolation; an approval prompt (build mode) or auto-approval (auto mode) is the only compensating control once a command is approved. `aegis doctor`'s `sandbox` check surfaces the same recommendation. `sandbox.backend` defaults to `os` for exactly this reason — both the built-in default (no config file at all) and `aegis --first-init`'s generated config agree on it — and only falls back to `local` (with this same warning) if no OS sandbox mechanism is available on the host.

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

The daemon writes a single, global JSONL audit trail shared by every session — there is no per-session audit file.

**Location:** `<data_dir>/audit.jsonl` (`~/.config/aegis/audit.jsonl` on macOS/Linux, `%AppData%\aegis\audit.jsonl` on Windows, by default; see `internal/config/config.go`'s `defaultDataDir()`).

Each line is a JSON object, and the fields present depend on the record's `phase`:

- **`pre`** — logged for every tool call before it runs: `time`, `phase`, `tool`, `input` (the raw tool input, replaced with a `"[N bytes, truncated]"` string if larger than 1024 bytes).
- **`post`** — logged for every tool call after it runs: `time`, `phase`, `tool`, `is_error`.
- **`policy_decision`** — logged when a contextual security policy (`egress_then_write`, `network_allowlist`), a text-based permission rule, or the persona-tool advisory gate makes a decision: `time`, `phase`, `tool`, `cap` (the tool's capability), `rule`, `decision`, `reason`.
- **`subagent_stop`** — logged when a spawned sub-agent finishes: `time`, `phase`, `agent_id`, `status`, `summary`, `is_error`.

```json
{"time":"2026-07-02T10:00:00Z","phase":"pre","tool":"shell","input":{"command":"npm install malicious-package"}}
{"time":"2026-07-02T10:00:01Z","phase":"policy_decision","tool":"shell","cap":"execute","rule":"deny bash(npm install*)","decision":"deny","reason":"denied by rule"}
```

**Decision values** (`policy_decision` records only): `allow`, `deny`, `ask`.

The audit trail is append-only and never rewritten. It persists even when sessions are deleted, since it is not tied to any single session. See docs/permissions.md#audit-trail for the canonical description.

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

## Client<->Daemon Transport

All traffic between a CLI client (`aegis`, `aegis ui`, `aegis sessions`, `aegis acp`,
`aegis mcp-serve`, ...) and the daemon is pinned-cert TLS by default (`server.tls.enabled: true`,
on since P27.5/FIND-13): the daemon auto-generates a self-signed certificate on first start and
every in-repo client pins it explicitly (no `InsecureSkipVerify`, no public CA involved), so this
needs no operator setup. The daemon also refuses to bind a non-loopback address unless
`server.allow_remote: true` is set (FIND-08), keeping this traffic off the network by default.

Before P27.5, this traffic was plain HTTP over loopback, protected only by the bearer token
(`daemon.token`) — on a shared multi-user host, another local account with packet-capture privilege
(raw socket access) could observe the loopback interface, including the bearer token and full
conversation content (FIND-32, CVSS 3.3, Tier 3/defense-in-depth given the loopback-only default).
Set `server.tls.enabled: false` (or `AEGIS_SERVER_TLS_ENABLED=false`) to opt back out, e.g. in a
container/CI environment where the extra handshake overhead isn't worth it. See `server.tls.*` in
[configuration.md](configuration.md#full-config-reference) for the full option set, the
auto-generated cert/key file locations, and what this protects against — notably, it does **not**
protect against Host/OS-level compromise of the same account, which can already read
`daemon.token` (and, with TLS enabled, `daemon.key`) directly off disk.

---

## Multi-Agent Debate (P12)

A security claim — a scan finding, a threat/mitigation pair, a "this is a false positive" call — is
easy to accept on a single unchallenged pass, especially from a small local model. Debate mode runs it
through adversarial review instead: a critic challenges the claim (grounded in cited evidence — a
`security_scan` result, a `grep`/`read_file` lookup, a specific `file:line` — or an explicit concession
if it finds no flaw), the proposer rebuts, this repeats for up to `max_rounds` (default 2), then an
arbiter issues a final UPHOLD/REVISE/REJECT verdict. A critique with no cited evidence is tagged
`[unsubstantiated]` in the transcript the arbiter sees and discounted, rather than treated as a real
rebuttal — this is what keeps one model arguing with itself across personas from just performing
disagreement.

```bash
aegis debate "This XSS finding is a false positive because the output is HTML-escaped downstream"
```

```
/debate <claim>
```

Debate isn't limited to security claims — `--domain generic` swaps in non-security roles for
debating documents, plans, or decisions. Full guide with worked examples: [debate.md](debate.md).
Mechanism, config toggles, and CLI/TUI/HTTP entry points: see
[multi-agent.md#debate-p12](multi-agent.md#debate-p12). Two existing security workflows can opt into
routing through a debate round automatically — `security.debate.threat_model` for the security-architect
persona's threat modeling, and `security.debate.triage` for the security-audit skill's baseline-
suppression triage — both default off.

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
| `security-critic` / `security-arbiter` | Debate roles (P12) — critic/arbiter, used by default in `mode:"debate"` |

```bash
aegis --persona appsec-engineer --mode plan
```

---

## Recommended Security Configuration

`aegis harden [--project] [--yes]` sets the `sandbox`/`security`/`cost` portions of this below
(sandbox backend → `auto`, `egress_then_write` → `true`, and any unset cost cap → a conservative
default) in one step, without hand-editing YAML — see [cli-reference.md](cli-reference.md#aegis-harden).
It leaves `permission.rules` and `security.network_allowlist` for you to fill in, since those are
project-specific.

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
