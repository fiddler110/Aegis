# Personas

A persona sets the system prompt and default configuration for a session, tuning the agent's behavior, communication style, and focus area. Most built-in personas also declare a `Tools` list that shapes *which tools it's expected to reach for* — but this is advisory, never enforced as a hard restriction (see [Tool Guidance](#tool-guidance-advisory-not-enforced) below).

---

## Selecting a Persona

**At launch:**
```bash
aegis --persona security
aegis --persona developer --mode build
aegis --persona report-writer
```

Without `--persona`, a new session uses the project's or user's configured `default_persona` if one is set (see [Default Persona](#default-persona)), falling back to `general`.

**Inside the TUI:**
```
/persona security          # switch directly
/persona                   # open interactive picker
```

Switching persona mid-session applies the **full profile**, not just the system prompt: the persona's model, permission rules, and output-guard overrides take effect on the next message, and its default permission mode is applied unless you've set one explicitly (subject to the trust guard below — a custom persona can't silently escalate past the configured default). The TUI reports a mode change when one happens.

**From the CLI:**
```bash
aegis persona list                 # all personas, custom/default markers shown
aegis persona show security        # profile: source, model, mode, rules, guard, tools, prompt
aegis persona show my-helper       # for custom personas, prints the file path to edit
aegis persona new incident-responder   # scaffold a new custom persona (see below)
aegis persona use developer        # set this project's default persona (see below)
```

---

## Default Persona

Set which persona a project starts with, so you don't need `--persona` on every launch:

```bash
aegis persona use developer              # writes default_persona to .aegis/config.yaml
aegis persona use security --global      # writes to the user-global config instead
```

Precedence: an explicit `--persona` flag always wins; otherwise the project's `default_persona` is used; otherwise the user-global one; otherwise `general`. `aegis persona list`/`show` mark the currently-configured default. You can also set it directly in either config file:

```yaml
default_persona: developer
```

---

## Built-in Personas

| Persona | `--persona` value | Focus |
|---------|-------------------|-------|
| General | `general` | Research, documentation, and coding — the default |
| Critic | `critic` | Debate role (P12, generic domain): adversarially hunts for the weakest part of a non-security claim (a document, plan, or decision), grounded in cited evidence, or concedes |
| Arbiter | `arbiter` | Debate role (P12, generic domain): synthesizes a generic-domain debate transcript into a structured UPHOLD/REVISE/REJECT verdict |
| Security | `security` | Security platform architect: capability research, STRIDE/LINDDUN threat modeling, C4/Mermaid architecture diagrams |
| Platform Architect | `platform-architect` | Full architecture lifecycle: system & security design, threat modeling, solution evaluation & PoCs, process development, automation, roadmap planning, documentation & reporting |
| Security Architect | `security-architect` | Security architecture, threat modeling (via the `threat-modeling` skill: STRIDE/LINDDUN/PASTA/Trike/VAST/NIST 800-154), design review |
| Security Engineer | `security-engineer` | Security tooling, vulnerability management, automation, incident response |
| AppSec Engineer | `appsec-engineer` | Secure code review, OWASP testing, CI/CD security integration |
| Developer | `developer` | Implementation, debugging, code review, testing |
| Security Researcher | `security-researcher` | Vulnerability research, attack analysis, MITRE ATT&CK mapping |
| Red Team | `red-team` | Authorized attack-surface mapping (`recon_scan`: nmap + nuclei), network/host vulnerability scanning, exploitation validation under an explicit scope |
| Risk Assessor | `risk-assessor` | Risk identification and treatment using NIST RMF, ISO 27005, FAIR |
| Business Analyst | `business-analyst` | Requirements analysis, process mapping, stakeholder communication |
| Data Analyst | `data-analyst` | Data exploration, statistical analysis, visualization, reporting |
| Network Security Architect | `network-security-architect` | Network design, segmentation, zero-trust, threat analysis |
| Report Writer | `report-writer` | Structured reports, technical writing, findings documentation |
| SRE | `sre` | Reliability engineering, SLOs/SLIs, observability, incident management |
| Infrastructure Architect | `infrastructure-architect` | IaC (Terraform/Pulumi), container orchestration, day-2 operations |
| Cloud Architect | `cloud-architect` | Cloud-native design, migration strategies, multi-cloud/hybrid, cost optimization |
| Cloud Security Engineer | `cloud-security-engineer` | Cloud security posture (CIS Benchmarks), IAM, cloud-native security controls |
| Security Critic | `security-critic` | Debate role (P12): adversarially hunts for the weakest part of a claim, grounded in cited evidence, or concedes |
| Security Arbiter | `security-arbiter` | Debate role (P12): synthesizes a debate transcript into a structured UPHOLD/REVISE/REJECT verdict |

`critic`/`arbiter` and `security-critic`/`security-arbiter` are debate roles (P12) used by the
`agent` tool's `mode:"debate"` — `security-critic`/`security-arbiter` by default, or `critic`/
`arbiter` when `domain: "generic"` (see [debate.md](debate.md) and
[multi-agent.md](multi-agent.md#debate-p12)). They're rarely picked directly with `--persona`, but
are addressable the same way any other persona is (`aegis persona show critic`), and any of the four
can be substituted for either role in either domain via `critic_persona`/`arbiter_persona`.

---

## Per-Persona Model Overrides

Pin a built-in persona to a specific model in `config.yaml`:

```yaml
personas:
  security-architect: { model: claude-opus-4-8 }   # use a stronger model
  developer:          { model: "" }                 # blank = global provider.model
  report-writer:      { model: claude-opus-4-8 }
  sre:                { model: "" }
```

Model resolution order (first non-empty wins):
1. `config.yaml` → `personas[name].model`
2. Custom persona file frontmatter → `model`
3. Global `provider.model`

Model overrides are model-ID only — they do not switch providers.

---

## Custom Personas

Scaffold one with the CLI (recommended):

```bash
aegis persona new incident-responder                     # project: .aegis/personas/
aegis persona new incident-responder --global            # user:    <data-dir>/personas/
aegis persona new triage --description "Bug triage lead"
```

The generated file contains a commented frontmatter template — edit the prompt, delete the options you don't need, and switch to it with `/persona <name>`.

Or drop a markdown file into either of these directories yourself:

| Scope | Directory |
|-------|-----------|
| Project | `.aegis/personas/<name>.md` |
| User (global) | `~/.local/share/aegis/personas/<name>.md` |

Project files take precedence over user files on name collision. The persona's name is the **filename stem** (`secure-reviewer.md` → `secure-reviewer`); file personas shadow built-ins of the same name.

**Hot reload:** the daemon rescans these directories whenever personas are listed, switched, or a message is sent — adding, editing, or deleting a persona file takes effect immediately, no restart. One nuance: a session's system prompt is captured when the session is created or the persona is switched, so after editing a persona's *prompt*, run `/persona <name>` again in existing sessions to re-apply it (model, rules, and guard changes apply on the next message automatically).

The file body becomes the system prompt. YAML frontmatter carries optional overrides:

```markdown
---
description: Strict secure code reviewer with remediation focus
model: claude-opus-4-8       # pin to this model (same provider)
mode: build                  # default permission mode for sessions using this persona
tools: [read_file, grep, shell]  # informational — used for display; tool filtering not yet enforced
rules:                       # permission rules merged into the session gate
  - "deny write(*)"
  - "allow shell(git diff*)"
  - "allow shell(git log*)"
output_guard:                # validate this persona's answers
  mode: llm
  rubric: "Every finding must cite a file:line and a CWE ID."
  max_retries: 2
---

You are a strict secure code reviewer specializing in Go web services.

Your approach:
1. Use `grep` and `read_file` to understand the codebase before making claims
2. Every finding must include the exact file path and line number
3. Every finding must reference a CWE ID or OWASP category
4. Provide a concrete remediation for each finding, not just the problem

Focus areas: injection vulnerabilities, authentication/authorization flaws,
sensitive data exposure, insecure deserialization, SSRF.
```

**Frontmatter fields:**

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Short description shown in the picker |
| `model` | string | Model ID override (same provider as global config) |
| `mode` | string | Default permission mode: `plan`, `build`, or `auto` (see trust note below) |
| `tools` | list | Advisory tool list — see [Tool Guidance](#tool-guidance-advisory-not-enforced) |
| `rules` | list | Permission rules merged into the session gate |
| `output_guard` | object | Output validation config (see [Configuration](configuration.md)) |

To disable output validation for a persona:
```yaml
output_guard: none
```

---

## Tool Guidance (advisory, not enforced)

A persona can declare `tools:` to describe which tools fit its role. This is guidance, not a restriction — nothing about it is a security boundary, and it never hard-blocks a tool call. When the model calls a tool outside the persona's declared list:

- It's logged as a warning on the daemon.
- The same approval flow used for permission decisions (e.g. shell execution in build mode) is consulted: in `auto` mode, or wherever the session's approver auto-approves, the call is silently allowed ("warn and allow"); in an interactive TUI session, you get a confirmation prompt ("ask to confirm") before the call proceeds.
- Approving once (allow-always) is remembered for the rest of the session, same as any other approval — you won't be re-prompted for that tool.
- If you decline, that specific call is blocked; the underlying capability rules (mode, text-based rules, contextual policies) are otherwise completely unaffected by a persona's tool list — they remain the real security boundary. A tool the persona doesn't list, once approved, is still subject to those rules exactly as if it had been listed.

A persona that omits `tools:` entirely (like the built-in `general` persona) has no restriction of any kind — no warnings, no prompts.

Every built-in persona except `general` declares a curated `Tools` list matching its role (e.g. `data-analyst` doesn't list `git_commit`; `risk-assessor` doesn't list `security_scan`). Calling an off-list tool from a built-in persona still just warns/asks — it's a nudge, not a wall.

**Trust note on `mode` (P7.5):** a persona file — including one from a third-party bundle (`aegis bundle install <git-url>`) — is less trusted than a built-in persona. Its `mode:` is only honored implicitly (i.e. when a session is created without an explicit `--mode`/`mode` request) if it's no more permissive than the daemon's configured default (`permission.mode` in config.yaml). A loaded persona declaring `mode: auto` while the configured default is `plan`/`build` has that request ignored and logged as a warning, instead of silently granting unattended shell execution. Pass `--mode auto` explicitly (or configure `permission.mode: auto`) if you actually want that persona's elevated mode.

---

## Disabling Output Guard per Persona

The output guard is on by default. A custom persona can disable it:

```markdown
<!-- code-generator.md -->
---
description: Generates boilerplate code quickly
output_guard: none
---
You generate code quickly without extensive explanation...
```

Or set a custom rubric:

```markdown
---
output_guard:
  mode: schema
  rubric: '{"required": ["files", "summary"]}'
---
```

---

## Examples

### Security-focused reviewer

```markdown
<!-- appsec-strict.md -->
---
description: OWASP-focused application security reviewer
model: claude-opus-4-8
mode: plan
rules:
  - "deny write(*)"
output_guard:
  mode: llm
  rubric: "Each finding cites OWASP Top 10 category, file:line, and remediation."
  max_retries: 2
---
You are an application security engineer focused on OWASP Top 10.

Always:
- Read the actual source files before commenting
- Cite exact file paths and line numbers
- Reference OWASP Top 10 or CWE for each finding
- Suggest specific code fixes, not general advice
```

### Structured output generator

```markdown
<!-- json-architect.md -->
---
description: Outputs structured architecture assessments as JSON
output_guard:
  mode: schema
  rubric: '{"required": ["summary", "risks", "recommendations"]}'
---
You are an architecture reviewer. Always respond with valid JSON containing:
- "summary": string
- "risks": array of {severity, description, mitigation}
- "recommendations": array of {priority, action, rationale}
```

### LaTeX report writer

```markdown
<!-- latex-reporter.md -->
---
description: Produces LaTeX reports for security assessments
mode: build
tools: [read_file, glob, grep, latex_build, latex_new_document, edit_file]
output_guard:
  mode: llm
  rubric: "The response confirms the PDF was compiled successfully and gives the output path."
---
You produce professional PDF reports using LaTeX.

Workflow:
1. Use latex_new_document to scaffold a report template
2. Fill sections with content from the conversation
3. Compile with latex_build (2 passes for cross-references)
4. Report the output PDF path

Always use the "report" style for security assessments.
```
