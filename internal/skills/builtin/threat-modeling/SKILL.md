---
name: threat-modeling
description: Use when asked to threat model a system, application, feature, or data flow — identify assets, trust boundaries, threats, and mitigations using a structured framework (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) — or to update an existing threat model against the current code. Triggers on "threat model", "threat modeling", "STRIDE analysis", "privacy threat model", "risk-centric threat model", "update/refresh the threat model", "what changed since the last threat model", "what could go wrong with this design security-wise".
---

# Threat Modeling Skill

Six named frameworks solve different problems, and picking the wrong one
produces a technically-complete document that answers a question nobody
asked (a STRIDE pass on a system whose actual risk is regulatory PII
exposure, or a PASTA business-impact exercise for a five-person team that
just needed a per-endpoint threat/mitigation list). This skill's job is to
get the framework choice right *before* any modeling starts, then keep the
model grounded in the real system rather than an assumed one.

## 1. Pick the framework

If the user already named one ("do a STRIDE analysis", "LINDDUN pass on
this"), use it — don't second-guess an explicit choice. If updating an
existing model (see §6), keep the baseline's framework unless the user
overrides it. Otherwise, ask one clarifying question before doing anything
else, using this table to frame the options:

| Framework | Focus | Best use case |
|---|---|---|
| STRIDE | 6-category threat taxonomy per DFD element | General-purpose default |
| LINDDUN | 7-category privacy threats | PII/regulated privacy contexts |
| PASTA | Risk-centric, 7-stage, includes attack simulation | Enterprise, business-impact traceability |
| Trike | Requirements/access-control model, explicit risk acceptance | Governance-heavy, auditable risk decisions |
| VAST | Scales across many teams, Agile/DevSecOps cadence | Large orgs, many services |
| NIST 800-154 | Data-centric: flow/storage/exposure of sensitive data | Compliance, data-protection-anchored assessments |

If the conversation gives a strong signal (the system's central concern is
personal data → lean LINDDUN or NIST 800-154; the ask mentions audit trails,
sign-off, or risk acceptance → lean Trike; many services/teams and a backlog
workflow → lean VAST) name your inferred pick and ask for a one-word
confirm/override rather than a fully open question. If there's truly no
signal and no way to ask (non-interactive run), default to **STRIDE** — it's
the general-purpose default in the table above — and say so explicitly in
the output so the reader knows a default was chosen, not derived.

Once chosen, read the matching reference file before doing anything else:
`references/stride.md`, `references/linddun.md`, `references/pasta.md`,
`references/trike.md`, `references/vast.md`, or
`references/nist-800-154.md`. Each one has the framework's process steps and
an output template — follow it rather than improvising a structure, since
these keep the model's output actually aligned to the named framework
instead of a generic threat list wearing the framework's label.

## 2. Explore the workspace before modeling

Never model an assumed architecture. Before applying any framework:

1. Explore the workspace: list directories, read entry points, config,
   auth/authz code, network-facing handlers, and data-access layers.
2. From what you actually found, identify: **assets** (data, credentials,
   capabilities worth protecting), **trust boundaries** (process/network/
   privilege boundaries the system crosses), **entry points** (where
   untrusted input enters), and **data flows** (how data moves between
   components, including third-party/external dependencies).
3. **Anchor every component to a real code artifact.** For each component
   in the model you must be able to cite the specific class, file, or
   manifest that is its anchor — if you can't cite one, the component
   doesn't exist; delete it rather than modeling an invented abstraction
   (`ConfigurationStore`, `DataLayer`) that no code implements. Name
   components after their anchors, not synonyms of them, so a re-run on the
   same code produces the same names.
4. **Classify the deployment, and let it bound severity.** From the same
   evidence (listeners and bind addresses, ingress/proxy config, ports
   mappings, service manifests) classify the system: `internet-facing`,
   `internal-network`, `localhost-service` (daemon bound to 127.0.0.1), or
   `local-desktop` (no listener at all). The classification is binding: a
   localhost-only daemon has no "unauthenticated remote attacker" threats —
   the floor prerequisite for every threat is local process access, and
   severity must reflect that. State the classification in the document so
   the reader can check the floor was applied.
5. Only then apply the chosen framework's process against this real map —
   not a generic web-app shape.

## 3. Evidence rules — verify before flagging

Never flag a security gap without confirming it exists; many platforms have
secure defaults, and a confident finding about a gap that isn't there costs
the whole document its credibility. Before writing any "missing X" threat:

- **Inventory the security infrastructure first.** Auth middleware, TLS
  termination, secret managers, permission gates, sandboxing, service-mesh
  mTLS, policy engines — if such a component exists, its protection is
  likely active unless explicitly disabled.
- **Distinguish three configuration states:** *explicitly disabled*
  (`enabled: false` — flag it), *not configured* (check the platform's
  default before assuming insecure; Kubernetes RBAC and service-mesh mTLS
  default on, Redis auth and Docker non-root default off), and *implicitly
  secure* (document as an existing control, not a gap).
- **Cite evidence per threat.** Every threat entry names the file, config,
  or code path that makes it real — for a "missing security" claim, that
  means evidence the default is actually insecure, not just the absence of
  a setting. A threat you cannot evidence goes in a "needs verification"
  note, not the threat table.

## 4. Apply the framework, then write the document

Follow the loaded reference file's process and output template exactly. Do
not stop after a partial pass — populate every category/stage/cell the
framework defines, even ones with no findings ("none identified" is a valid,
complete entry; a missing cell is not).

Write the completed document via `write_file` to
**`.aegis/security/threat-model/`** — the same directory family the other
persisted security reports use — named
`<framework>[-<scope>]-<YYYY-MM-DD>.md` (e.g. `stride-2026-07-08.md`, or
`stride-webui-2026-07-08.md` when the model covers one feature rather than
the whole project). Never delete or overwrite a prior report in that
directory — an update is a new dated file, and the old one is the baseline
it was diffed against.

Three cross-framework rules while filling the template:

- **Every threat states its prerequisite** (what access the attacker
  already needs: none / authenticated user / internal network / local
  process / host compromise), and no prerequisite may sit below the
  deployment classification's floor from §2.
- **Risk acceptance is not yours to make.** Never mark a threat "accepted
  risk" on your own authority — an unmitigated threat is an open finding
  with a proposed mitigation, and accepting it is the owning team's
  decision (Trike formalizes exactly this; see its reference).
- **Run the technology sweep** in `references/companion-techniques.md`
  after the framework pass — it catches cross-cutting gaps (database auth
  defaults, containers running as root, secrets reaching external LLMs)
  that a per-element pass tends to miss.

If the ask also touches attacker realism, backlog integration, or
Agile-native framing, check `references/companion-techniques.md` for Attack
Trees, MITRE ATT&CK mapping, and Evil User Stories — optional add-ons layered
on top of the chosen primary framework, not replacements for it.

## 5. Route disputed findings through debate (P12), when enabled

If your system prompt's "Debate mode (P12)" section marks threat modeling
enabled, route each identified threat/mitigation pair through the `agent`
tool's `mode:"debate"` before writing it into the document — call with
`claim` set to the threat description, severity, and proposed mitigation.
Reflect the arbiter's verdict in the final entry: adjust severity/mitigation
per a REVISE verdict, drop the entry per a REJECT verdict, write it as-is per
UPHOLD. Skip this for a clear-cut, uncontroversial entry; it exists for
threats where the severity or mitigation is genuinely arguable.

## 6. Self-check, inventory, and updates

Read `references/verification-and-updates.md` before finishing. It covers
three things: the **final self-check** (coverage, prerequisite floors,
anchors, evidence — run it and fix what fails before reporting), the
**inventory sidecar** (a small YAML file written next to the document with
stable component/threat IDs, so a future run can diff against this one),
and the **update workflow** for when the ask is "update/refresh the threat
model" or "what changed since last time" — verify each baseline threat
against the current code and produce a standalone updated document with a
changes-since section, rather than modeling from scratch or trusting the
old document's claims.

## 7. Report

Write the full document to `.aegis/security/threat-model/` per §4 — a
chat-only summary is not a complete threat model, since the whole point is
a document mitigations can be tracked against. State which framework was
used (and why, if inferred rather than requested) and the deployment
classification at the top of the document. Do
not stop after an outline; every category/stage the framework defines needs
a populated entry, the self-check needs to pass, and the inventory sidecar
needs to exist before the task is done.
