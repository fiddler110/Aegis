# Aegis Capability Roadmap

**Last updated:** 2026-07-06

This document tracks only **open** work — what's next. For shipped-feature history and full design
rationale behind completed items, see [releases.md](releases.md).

---

## Status

Open items: **P13** (P13.2 trufflehog, P13.3 terminal enhancements, P13.4 nebula-inspired
engagement tooling, P13.6 threat-modeling skill, P13.7 LaTeX report skill), **P9.4** (per-task model
routing), **P6.1** (mid-turn state persistence), **P6.5** (desktop/IDE surface beyond ACP).

Everything else — P2–P5, P7, P8, P9.1/P9.2/P9.5, the 2026-07-03 architecture/security review's
15-item punch list, P10, P11, P12, P13.1/P13.5/P13.8, P14 (all of P14.1–P14.10), and the TQ TUI
track — is shipped. See [releases.md](releases.md) for what each shipped and why.

**Nothing is currently in progress.** P13's five remaining sub-items are researched and scoped
(2026-07-05) but not started. P9.4 and P6.1/P6.5 are real but have no concrete trigger — don't
build them speculatively; check with the user first.

---

## Open Work — P13 (Security & Capability Enhancements)

Researched 2026-07-05 (five items via background review of a named external project/methodology,
two via direct codebase audit). P13.1, P13.5, and P13.8 shipped — see
[releases.md](releases.md#shipped--p13-items-security--capability-enhancements). The five below are
scoped proposals, not started.

### P13.2 — Trufflehog secret-scanning enhancement

Researched: trufflehog's differentiator over gitleaks is **live verification** (800+ detectors
call the real provider API — AWS/GitHub/etc. — to confirm a found credential is still active),
cutting triage noise sharply. It has no SARIF output (needs a hand-written parser, same as
gitleaks today) and is AGPL-3.0 licensed (vs. gitleaks' MIT — no code-linking concern given Aegis
only shells out to a separately-installed binary, but worth disclosing to operators).

Recommendation: add **alongside** gitleaks, opt-in (`DefaultEnabled:false`, matching the P11.3
gosec/bandit/etc. precedent), deduped against gitleaks via the existing P11.8
`DedupFindings`/`SeenBy` machinery — not a replacement. Verification mode makes real calls to
third-party services using the actual discovered secret (rate-limit/alerting/misuse-resembling
risk) and needs a DAST-style hard gate, not a normal scanner toggle.

- **P13.2.1** — Add `trufflehogScanner` (opt-in) to `internal/security/scanners.go`/`descriptors`,
  filesystem mode, hand-written JSON parser, default `--no-verification`. (M)
- **P13.2.2** — `security.tools.trufflehog.verify` config bool (default false); when true, require
  `MethodHost` only — verification is incompatible with the `--network none` container-hardening
  posture, same host-only carve-out precedent as image scanning — and have `Resolve` explicitly
  refuse container mode with `verify:true`. (S)
- **P13.2.3** — Extend `Finding` with an optional verified/unverified/unknown tri-state; surface it
  in `Format()` and the security-audit skill's triage loop so verified findings are harder to
  baseline-suppress. (S)
- **P13.2.4** — Document AGPL-3.0 licensing in `docs/security.md`'s tool table. (S)
- **P13.2.5** — Guided-install descriptor entries (brew/curl-script/go install/docker) per the
  P11.10 pattern. (S)

TUI surface requirement (see the cross-cutting note below): the `verify` opt-in must appear as an
explicit, warning-labelled toggle in `/security-config`; the verified/unverified tri-state
(P13.2.3) must render in the `/scan` output, not only the CLI.

Priority: Medium, Effort: M overall.

### P13.3 — Terminal enhancements (Microsoft Intelligent Terminal review)

Researched: Intelligent Terminal is Microsoft's experimental ACP-native fork of Windows Terminal
(a terminal *emulator*, not a standalone tool) — docked Agent Pane, agent status bar,
command-failure diagnosis with fix suggestions, a context-injecting command palette, and session
management. Most of its UX ideas are already shipped in Aegis's TQ track (docked terminal pane,
sidebar status, session picker/resume, theming, streaming). Aegis's embedded terminal pane already
shells out via plain `os/exec` (no pty library), so it's already fully cross-platform — that
constrains what's easy to add without introducing OS-specific pty code.

Genuinely new, worth adding:

- **P13.3.1** — Shell-aware error assist: on a non-zero exit from the `shell` tool or the embedded
  terminal pane, offer an inline "diagnose this?" affordance that pipes stderr+exit code to the
  model on request. Cross-platform (exit-code capture only). (S/M)
- **P13.3.2** — `@shell`/`@last` context token: extend the existing `@file`/`@image:`
  attachment-token parser to inject the last N lines of embedded-terminal output into the next
  prompt. (S) — TUI-surface work; build under the same command-surface conventions P14 established.
- **P13.3.3** — ACP `terminal/*` capability passthrough: `internal/acp` implements
  session/prompt/permission methods but not the optional ACP terminal capability
  (`terminal/create`, `/output`, `/wait_for_exit`, `/kill`, `/release`). Implementing it lets an
  ACP host (Zed, a future Intelligent-Terminal-as-client) supply its own pty for agent shell calls
  — live visibility/Ctrl+C control on the host side — falling back to Aegis's native exec path
  when the host doesn't advertise the capability. The one item requiring real ACP protocol work;
  everything else here is TUI-only. (M/L)
- **P13.3.4** — Background-task attention indicator: extend the existing sidebar agent-count
  display to flag a failed background sub-agent/cron job. (S)
- **P13.3.5** — Configurable keybinding remap: `internal/tui/keymap.go` is fully hardcoded; add a
  `tui.keybindings` config section. Trivial cross-platform (bubbles/key is already OS-agnostic). (S)
  — TUI-surface work, same note as P13.3.2.

Priority: Low-Medium, Effort: S-M per item, no single blocker.

### P13.4 — Nebula (berylliumsec/nebula) AI-pentesting review

Researched and identified: github.com/berylliumsec/nebula (~500 stars, PyPI `nebula-ai`) —
confirmed as the correct LLM-driven pentesting project (not the unrelated Slack/Defined-Networking
VPN mesh tool of the same name). Its OSS core is an *advisory copilot*, not an autonomous attack
engine: `!`-prefixed LLM queries against on-screen terminal output, AI-assisted categorized
note-taking, real-time next-step suggestions, and ingestion of external recon-tool output — no
exploit chaining, no autonomous target execution, no report generation in the free tier. A paid
"Nebula Pro" tier claims an undocumented "autonomous mode"; public docs give no technical detail,
and this should **not** be a model for anything Aegis adopts.

Genuinely new pattern worth taking: a persistent, session-spanning "engagement notebook" (distinct
from a single scan `Report`) and an advisory flow that ingests arbitrary external tool output
(nmap, nikto, gobuster — none of which Aegis wraps) and reasons about next steps.

- **P13.4.1** — Security engagement notebook: persistent structured notes/findings ledger spanning
  a multi-day review (extends `internal/memory`). (M)
- **P13.4.2** — `security_advise` tool: ingest pasted external recon-tool output, return
  AI-suggested next steps, map into the `Finding` model where possible. (M)
- **P13.4.3** — CVE/exploit-context lookup tool: scoped lookup feeding real citations to the P12
  debate proposer/critic roles instead of relying on model recall. (S)
- **P13.4.4** — Engagement status digest: summary of recent scan/session activity (finding counts,
  deltas, open items). (S) — fold into the existing `/status` command's output rather than adding a
  separate one.
- **P13.4.5** — Guarded "suggest next action" layer on top of `RunDAST`: proposes manual next-test
  steps post-scan, never auto-executed, reusing the exact `allow_active`/`allowed_targets` gate —
  explicitly excludes autonomous exploit chaining. (M)

TUI surface requirement: P13.4.1 (notebook) and P13.4.4 (status digest) each need a slash surface
(`/notebook`, folded into `/status`).

Priority: Low (interesting, not urgent), Effort: M overall. P13.4.5 must not adopt Nebula Pro's
undocumented autonomous-mode pattern.

### P13.6 — Aegis threat-modeling persona/skill

Researched the six named frameworks (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) plus the
"top 12" blog's other entries. Worth adding as lightweight companions: **Attack Trees** (visual
attacker-goal/path decomposition), **MITRE ATT&CK mapping** (already gestured at in the
`security-researcher` persona), and **Evil User Stories** (Agile-native, pairs with VAST). Not
worth their own artifacts: OCTAVE (org-level, not app-level), Kill Chain Analysis (SOC/IR-oriented,
not model-generation), Hybrid Threat Modeling (worth a one-paragraph note only — "frameworks can be
combined").

| Framework | Focus | Best use case |
|---|---|---|
| STRIDE | 6-category threat taxonomy per DFD element | General-purpose default |
| LINDDUN | 7-category privacy threats | PII/regulated privacy contexts |
| PASTA | Risk-centric, 7-stage, includes attack simulation | Enterprise, business-impact traceability |
| Trike | Requirements/access-control model, explicit risk acceptance | Governance-heavy, auditable risk decisions |
| VAST | Scales across many teams, Agile/DevSecOps cadence | Large orgs, many services |
| NIST 800-154 | Data-centric: flow/storage/exposure of sensitive data | Compliance, data-protection-anchored assessments |

Design recommendation: **one skill bundle, not a new persona, not one skill per framework loaded ad
hoc.** A single `threat-modeling` skill (mirroring `content-review`'s `references/*.md` bundling
pattern) whose `SKILL.md` handles "clarify or infer which framework, then load only that
framework's reference file" — this is inherently a single-entry-point behavior (the user often
won't already know which framework they want), and keeps token cost to name+description until one
framework is actually chosen. The existing `security-architect` persona's "Workflow for threat
modeling" section should name this skill instead of hardcoding STRIDE/LINDDUN.

- **P13.6.1** — New builtin skill `internal/skills/builtin/threat-modeling/SKILL.md`:
  clarifying-question/framework-selection logic, shared workflow (explore workspace for
  assets/trust boundaries/data flows → apply chosen framework → write document → optional P12
  debate-mode routing per finding), pointer to per-framework reference files. (M)
- **P13.6.2–P13.6.7** — One `references/<framework>.md` per framework (stride, linddun, pasta,
  trike, vast, nist-800-154): checklist/process steps + output template each. (S each)
- **P13.6.8** — `references/companion-techniques.md`: Attack Trees, MITRE ATT&CK mapping, Evil User
  Stories as optional add-ons, plus the Hybrid Threat Modeling note. (S)
- **P13.6.9** — Update `securityArchitectSystem` (`internal/persona/persona.go`) to name the skill
  instead of hardcoding STRIDE/LINDDUN, preserving the existing P12 debate-mode routing hook. (S)
- **P13.6.10** — Update `docs/personas.md`/skills docs. (S)

TUI surface requirement: add a `/threat-model` slash command as the discoverable entry point that
loads the skill and asks the framework-selection clarifying question directly, instead of relying
on the model to notice trigger phrases in free text.

Priority: Medium, Effort: M overall (mostly content-writing, not code).

### P13.7 — LaTeX report writing: consolidation skill

Audited against the current codebase: `latex_build`/`latex_new_document` tools already exist
(`internal/tool/builtin/latex.go`) with a built-in "report" document-class style (fancy headers,
code-listing styling, bibliography support), and the `report-writer` persona already references
them. The original framing of this item ("incorporate LaTeX use") no longer matches the codebase —
the capability exists.

The real gap: no skill walks through the specific ask — consolidating a large number of existing
markdown research/planning docs into one coherent LaTeX report — the way `html-report` bundles a
template + validator + steps for its narrower single-report case.

- **P13.7.1** — New builtin skill `internal/skills/builtin/latex-report/SKILL.md` (mirrors
  `html-report`'s pattern): steps for gathering/reading the source markdown docs, synthesizing a
  section outline, calling `latex_new_document(style="report")`, filling sections from the source
  material, `latex_build`, and reporting the output PDF path. Skill (progressive disclosure), not
  always-loaded — triggered on phrases like "consolidate these into a report", "write this up as a
  LaTeX report". (M)

TUI surface requirement: add a `/report [latex] <sources…>` slash entry point that kicks off the
consolidation skill, rather than depending on trigger-phrase detection.

Priority: Low, Effort: M.

### P13 cross-cutting requirement — every new capability must ship its in-session TUI surface

A recurring failure mode found during the 2026-07-05 review and worth keeping in mind for every
item above: a capability ships as a *tool* (model-callable) and a *CLI subcommand*, but never gets
an in-session `/slash` command — so a user driving the TUI can't reach it, and it feels absent.
Each open P13 item above already carries its own TUI-surface requirement inline; the general rule
going forward is that no P13 capability is "done" until it's reachable from the TUI and covered by
the P14.1/P14.10 command-surface sync test (`TestBuiltinCommandsCoverDispatchTable`,
`TestCommandDefsWellFormed`). This is a requirement addition, not new scope.

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 are shipped. P9.3 (telemetry export) and P9.6 (bulk session/memory
export-import) were dropped 2026-07-05 — not wanted. Remaining:

### P9.4 — No per-task/complexity model routing

P5.9 only reroutes on failure. Nothing picks a cheaper model for simple turns and reserves an
expensive one for hard turns (cf. Aider). Plausible cheap win given cost tracking already exists,
but no evidence of demand. Priority: **Low**, Effort: **M**.

**Not blocking** — real but no concrete trigger, don't build speculatively.

---

## Open Work — P6 (Long-Horizon / Exploratory)

P6.3 (MCP server mode) shipped 2026-07-05.

### P6.1 — Mid-turn state persistence _(was P4.1)_

Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during
streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit
if crash-during-long-turn becomes a reported pain point.

### P6.5 — Desktop / IDE surface beyond ACP

ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to
the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Only worth it if user demand
materializes — the TUI is the product.

**Neither P6.1 nor P6.5 is blocking.** P6.1 has no reported pain point; P6.5 is speculative. Don't
build either without a concrete trigger — check with the user first. (P6.2, A2A protocol
integration, was evaluated and declined 2026-07-05 — no consumer, not wanted.)

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
