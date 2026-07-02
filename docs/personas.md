# Personas

A persona sets the system prompt and default configuration for a session, tuning the agent's behavior, communication style, and focus area. Personas do not limit which tools are available — they shape *how* the agent approaches problems.

---

## Selecting a Persona

**At launch:**
```bash
aegis --persona security
aegis --persona developer --mode build
aegis --persona report-writer
```

**Inside the TUI:**
```
/persona security          # switch directly
/persona                   # open interactive picker
```

---

## Built-in Personas

| Persona | `--persona` value | Focus |
|---------|-------------------|-------|
| General | `general` | Research, documentation, and coding — the default |
| Security | `security` | Security platform architect: capability research, STRIDE/LINDDUN threat modeling, C4/Mermaid architecture diagrams |
| Platform Architect | `platform-architect` | System design, technology evaluation, capacity planning |
| Security Architect | `security-architect` | Security architecture, threat modeling, design review |
| Security Engineer | `security-engineer` | Security tooling, vulnerability management, automation, incident response |
| AppSec Engineer | `appsec-engineer` | Secure code review, OWASP testing, CI/CD security integration |
| Developer | `developer` | Implementation, debugging, code review, testing |
| Security Researcher | `security-researcher` | Vulnerability research, attack analysis, MITRE ATT&CK mapping |
| Risk Assessor | `risk-assessor` | Risk identification and treatment using NIST RMF, ISO 27005, FAIR |
| Business Analyst | `business-analyst` | Requirements analysis, process mapping, stakeholder communication |
| Data Analyst | `data-analyst` | Data exploration, statistical analysis, visualization, reporting |
| Network Security Architect | `network-security-architect` | Network design, segmentation, zero-trust, threat analysis |
| Report Writer | `report-writer` | Structured reports, technical writing, findings documentation |
| SRE | `sre` | Reliability engineering, SLOs/SLIs, observability, incident management |
| Infrastructure Architect | `infrastructure-architect` | IaC (Terraform/Pulumi), container orchestration, day-2 operations |
| Cloud Architect | `cloud-architect` | Cloud-native design, migration strategies, multi-cloud/hybrid, cost optimization |
| Cloud Security Engineer | `cloud-security-engineer` | Cloud security posture (CIS Benchmarks), IAM, cloud-native security controls |

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

Drop a markdown file into either of these directories:

| Scope | Directory |
|-------|-----------|
| Project | `.aegis/personas/<name>.md` |
| User (global) | `~/.local/share/aegis/personas/<name>.md` |

Project files take precedence over user files on name collision.

The file body becomes the system prompt. YAML frontmatter carries optional overrides:

```markdown
---
name: secure-reviewer
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
| `name` | string | Persona name (used with `--persona` and `/persona`) |
| `description` | string | Short description shown in the picker |
| `model` | string | Model ID override (same provider as global config) |
| `mode` | string | Default permission mode: `plan`, `build`, or `auto` |
| `tools` | list | Informational list of tool names (shown in UI) |
| `rules` | list | Permission rules merged into the session gate |
| `output_guard` | object | Output validation config (see [Configuration](configuration.md)) |

To disable output validation for a persona:
```yaml
output_guard: none
```

---

## Disabling Output Guard per Persona

The output guard is on by default. A custom persona can disable it:

```markdown
---
name: code-generator
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
---
name: appsec-strict
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
---
name: json-architect
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
---
name: latex-reporter
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
