---
name: threat-modeling
description: Use when asked to threat model a system, application, feature, or data flow — identify assets, trust boundaries, threats, and mitigations using a structured framework (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154). Triggers on "threat model", "threat modeling", "STRIDE analysis", "privacy threat model", "risk-centric threat model", "what could go wrong with this design security-wise".
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
this"), use it — don't second-guess an explicit choice. Otherwise, ask one
clarifying question before doing anything else, using this table to frame
the options:

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
3. Only then apply the chosen framework's process against this real map —
   not a generic web-app shape.

## 3. Apply the framework, then write the document

Follow the loaded reference file's process and output template exactly. Do
not stop after a partial pass — populate every category/stage/cell the
framework defines, even ones with no findings ("none identified" is a valid,
complete entry; a missing cell is not). Write the completed document to a
file via `write_file`.

If the ask also touches attacker realism, backlog integration, or
Agile-native framing, check `references/companion-techniques.md` for Attack
Trees, MITRE ATT&CK mapping, and Evil User Stories — optional add-ons layered
on top of the chosen primary framework, not replacements for it.

## 4. Route disputed findings through debate (P12), when enabled

If your system prompt's "Debate mode (P12)" section marks threat modeling
enabled, route each identified threat/mitigation pair through the `agent`
tool's `mode:"debate"` before writing it into the document — call with
`claim` set to the threat description, severity, and proposed mitigation.
Reflect the arbiter's verdict in the final entry: adjust severity/mitigation
per a REVISE verdict, drop the entry per a REJECT verdict, write it as-is per
UPHOLD. Skip this for a clear-cut, uncontroversial entry; it exists for
threats where the severity or mitigation is genuinely arguable.

## 5. Report

Write the full document to a file — a chat-only summary is not a complete
threat model, since the whole point is a document mitigations can be tracked
against. State which framework was used (and why, if inferred rather than
requested) at the top of the document. Do not stop after an outline; every
category/stage the framework defines needs a populated entry before the task
is done.
