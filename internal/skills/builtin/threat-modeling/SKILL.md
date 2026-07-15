---
name: threat-modeling
description: Use when asked to threat model a system, application, feature, or data flow — identify assets, trust boundaries, threats, and mitigations using a structured framework (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) — or to update an existing threat model against the current code. Produces a suite of report files (architecture, DFD, framework analysis, findings with CVSS/CWE/OWASP, executive assessment) in a timestamped directory. Triggers on "threat model", "threat modeling", "STRIDE analysis", "privacy threat model", "risk-centric threat model", "update/refresh the threat model", "what changed since the last threat model", "what could go wrong with this design security-wise".
---

# Threat Modeling Skill

Six named frameworks solve different problems, and picking the wrong one
produces a technically-complete document that answers a question nobody
asked (a STRIDE pass on a system whose actual risk is regulatory PII
exposure, or a PASTA business-impact exercise for a five-person team that
just needed a per-endpoint threat/mitigation list). This skill's job is to
get the framework choice right *before* any modeling starts, keep the model
grounded in the real system rather than an assumed one, and deliver it as a
suite of files a reader can navigate — not one document they have to
scroll through to find the one finding that matters to them.

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
`references/nist-800-154.md`. Each one has the framework's process steps
and points onward to its **skeleton** — the verbatim, fill-in-the-blanks
template for its `2-<framework>-analysis.md` file, under
`references/skeletons/`. §4 below covers when to load which reference and
how the whole seven-file suite fits together; the point for now is: the
skeletons, not this prose, fix the actual document structure, so don't
improvise one from a process description alone.

| Reference file | Read when | Contains |
|---|---|---|
| `references/stride.md` / `linddun.md` / `pasta.md` / `trike.md` / `vast.md` / `nist-800-154.md` | Framework chosen, before exploring the workspace | That framework's process/stages and category definitions |
| `references/output-formats.md` | Before writing any of the five framework-agnostic files (`0-assessment.md`, `0.1-architecture.md`, `1.1-model.mmd`, `1-model.md`, `3-findings.md`) | Verbatim templates, mandatory fields (tier, CVSS 4.0, CWE, OWASP), the Threat Coverage Verification loop, and per-file post-write checks |
| `references/diagram-conventions.md` | Before writing `1.1-model.mmd` or any diagram in `0.1-architecture.md` | Mermaid shapes, fixed color palette, DFD direction, pre-render checklist |
| `references/skeletons/skeleton-<framework>.md` | **Before writing `2-<framework>-analysis.md` (§4.2), and again before filling each of its sections** | Verbatim document structure for the framework's own analysis file, fixed value lists, inline `<!-- ⛔ POST-*-CHECK -->` self-verification comments |
| `references/companion-techniques.md` | After the framework pass, or if attacker realism/backlog framing was asked for | Technology sweep, Attack Trees, MITRE ATT&CK mapping, Evil User Stories |
| `references/skeletons/skeleton-inventory.md` | Writing `inventory.yaml` (§6) | Exact field names and structure for the sidecar |
| `references/verification-and-updates.md` | Before finishing (§6) | Final self-check, sidecar rules, update workflow, sub-agent governance |

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
   same code produces the same names, and so `inventory.yaml`'s ids stay
   stable across runs.
4. **Classify the deployment, and let it bound severity.** From the same
   evidence (listeners and bind addresses, ingress/proxy config, ports
   mappings, service manifests) classify the system: `internet-facing`,
   `internal-network`, `localhost-service` (daemon bound to 127.0.0.1), or
   `local-desktop` (no listener at all). The classification is binding: a
   localhost-only daemon has no "unauthenticated remote attacker" threats —
   the floor prerequisite for every threat is local process access, and
   severity must reflect that. Record it, with evidence, in
   `0.1-architecture.md`'s Component Exposure Table — that table is the
   single source of truth every later file's prerequisite/tier must respect
   (`output-formats.md`).
5. Only then apply the chosen framework's process against this real map —
   not a generic web-app shape.

## 3. Evidence rules — verify before flagging

Never flag a security gap without confirming it exists; many platforms have
secure defaults, and a confident finding about a gap that isn't there costs
the whole suite its credibility. Before writing any "missing X" threat:

- **Inventory the security infrastructure first.** Auth middleware, TLS
  termination, secret managers, permission gates, sandboxing, service-mesh
  mTLS, policy engines — if such a component exists, its protection is
  likely active unless explicitly disabled. This inventory lands directly
  in `0.1-architecture.md`'s Security Infrastructure Inventory table.
- **Distinguish three configuration states:** *explicitly disabled*
  (`enabled: false` — flag it), *not configured* (check the platform's
  default before assuming insecure; Kubernetes RBAC and service-mesh mTLS
  default on, Redis auth and Docker non-root default off), and *implicitly
  secure* (document as an existing control, not a gap).
- **Cite evidence per threat.** Every threat entry names the file, config,
  or code path that makes it real — for a "missing security" claim, that
  means evidence the default is actually insecure, not just the absence of
  a setting. A threat you cannot evidence goes in `0-assessment.md`'s
  "Needs Verification" table, not the threat table.

## 4. Build the seven-file suite, writing as you go

Every run produces the same seven files, in the same directory, regardless
of framework — the file list, naming, and directory convention are in
`references/output-formats.md`; read it now if you haven't. Do not stop
after a partial pass — populate every category/stage/cell the chosen
framework defines, even ones with no findings ("none identified" is a valid,
complete entry; a missing cell is not).

**Write incrementally, never all-at-once at the end.** A threat model is a
long task; holding the whole analysis in conversation until one final batch
of writes means an interrupted run — context exhaustion on a local model, a
step limit, a crash — loses everything. Instead:

### 4.1 Skeleton stubs for all seven files

Create the directory
`.aegis/security/threat-model/<framework>-<target>-<YYYY-MM-DD-HHMM>/` and
`write_file` a stub for **all seven files** before filling any of them:
`0-assessment.md`, `0.1-architecture.md`, `1.1-model.mmd`, `1-model.md`,
`2-<framework>-analysis.md`, `3-findings.md`, `inventory.yaml` — each with
its top-level headings from `output-formats.md` (or
`skeletons/skeleton-<framework>.md` for the framework-analysis file) and
`<!-- PENDING -->` under every section.

The `<target>` slug is **mandatory, never omitted**: use the scoped
feature/system name when the model covers one (`webui`, `auth-service`), or
the repo/workspace directory name when it covers the whole project
(`aegis`) — a reader scanning `.aegis/security/threat-model/` must be able
to tell what each directory modeled without opening it. The
`<YYYY-MM-DD-HHMM>` timestamp (local time, 24h, dash-separated — e.g.
`stride-aegis-2026-07-08-1432`) is what keeps two same-day runs from
colliding; a date alone isn't enough since a full run plus an update can
both land on one day.

### 4.2 Fill in dependency order

Files depend on each other — fill them in this order, replacing that file's
`<!-- PENDING -->` markers with real content section by section as each
section's analysis completes, per that file's template in
`output-formats.md` (or the framework skeleton for step 3 below):

1. **`0.1-architecture.md`** first — everything downstream cites its
   component names, anchors, and Component Exposure Table.
2. **`1.1-model.mmd` then `1-model.md`** — the diagram and its tables reuse
   `0.1-architecture.md`'s exact component names (`diagram-conventions.md`
   for the Mermaid conventions).
3. **`2-<framework>-analysis.md`** — read
   `references/skeletons/skeleton-<framework>.md` first, and copy its
   structure exactly, same columns, same order, same fixed value lists
   (prerequisite, tier, severity, deployment classification). Run the
   skeleton's inline `<!-- ⛔ POST-*-CHECK -->` comments right after writing
   each table — they travel with the copied content and are invisible once
   rendered, but skipping them is how column drift and missing cells
   happen.
4. **`3-findings.md`** — every threat with a non-empty mitigation column in
   step 3 becomes a finding here, with tier, CVSS 4.0, CWE, and OWASP per
   `output-formats.md`'s mandatory-fields table (and its per-framework
   applicability notes — not every framework's threats map to CVSS/CWE the
   same way). Run the Threat Coverage Verification loop before moving on.
5. **`0-assessment.md`** last — its counts and links depend on every file
   above being complete.
6. **`inventory.yaml`** — the sidecar, per
   `references/skeletons/skeleton-inventory.md`.

Do not batch several finished sections in memory — the files on disk are
the working state, and once a section is written you no longer need to keep
its details in the conversation (this is what lets a long run survive
context compaction).

**Resume, don't restart.** If the target directory already exists and any
file still contains `<!-- PENDING -->` markers, a previous run was
interrupted: re-read what's there, keep every completed section, and
continue from the first pending one, in the dependency order above. Only
the final §5 review round re-examines completed sections.

Never delete or overwrite a *prior dated run directory* — an update is a new
directory, and the old one is the baseline it was diffed against (editing
today's own in-progress directory is, of course, the normal flow above).

Three cross-framework rules while filling the template:

- **Every threat states its prerequisite** (what access the attacker
  already needs: none / authenticated user / internal network / local
  process / host compromise), and no prerequisite may sit below the
  deployment classification's floor from §2, or the derived tier
  (`output-formats.md`) will be wrong.
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

## 5. Final review round — consistency, then debate (P12) when enabled

After every file's `<!-- PENDING -->` markers are gone, re-read the
**complete suite from disk** and review it as a whole — the files were
written one at a time, each depending on the last, so this is where seams
show:

- Component names and threat IDs are consistent across all seven files; no
  file refers to a component another file renamed or dropped.
- Every threat's prerequisite still respects the deployment classification's
  floor from §2, and its derived tier is consistent with its CVSS vector's
  `AV`/`PR` values (a `Local Process` prerequisite cannot carry `AV:N`).
- Every threat in `2-<framework>-analysis.md` appears exactly once in
  `3-findings.md`'s Threat Coverage Verification table.
- No two files contradict each other about the same control or data flow.

Fix what fails by editing the files in place — the full checklist is
`references/verification-and-updates.md`'s "Final self-check".

Then, if your system prompt's "Debate mode (P12)" section marks threat
modeling enabled, route the **contested entries** — high-severity threats
and any whose severity or mitigation is genuinely arguable — through the
`agent` tool's `mode:"debate"`, with `claim` set to the threat description,
severity, and proposed mitigation. Patch the arbiter's verdict back into
`2-<framework>-analysis.md` and `3-findings.md`: adjust severity/mitigation
per a REVISE verdict, drop the entry per a REJECT verdict, keep it as-is per
UPHOLD. Skip clear-cut, uncontroversial entries. Debating at the end, over
the assembled suite, both keeps a long run from stalling mid-analysis and
lets the debaters see each threat in the context of the whole model rather
than in isolation.

## 6. Self-check, inventory, and updates

Read `references/verification-and-updates.md` before finishing. It covers
four things: **sub-agent governance** (if any exploration or verification
was delegated to the `agent` tool, only the top-level run writes report
files — a sub-agent is a narrow, read-only helper, never an independent
producer of `0.1-architecture.md` or any other suite file), the **inventory
sidecar** (`inventory.yaml`, with stable component/threat IDs so a future
run can diff against this one), the **update workflow** for when the ask is
"update/refresh the threat model" or "what changed since last time" (locate
the baseline directory, verify each baseline threat against the current
code, produce a standalone new directory with a Changes Since Baseline
section, rather than modeling from scratch or trusting the old files'
claims), and the **final self-check** (coverage, prerequisite/tier
consistency, anchors, evidence, cross-file agreement — run it and fix what
fails before reporting).

## 7. Report

The directory in `.aegis/security/threat-model/` (built incrementally per
§4) is the deliverable — a chat-only summary is not a complete threat
model, since the whole point is a navigable suite mitigations can be
tracked against. State which framework was used (and why, if inferred
rather than requested) and the deployment classification up front — both
also belong in `0-assessment.md`'s Executive Summary. The task is done only
when no `<!-- PENDING -->` marker remains in any of the seven files, the §5
review round has run over the assembled suite, the final self-check passes,
and `inventory.yaml` exists and agrees with the documents. If the run must
stop early anyway (step limit, context pressure), say plainly which files
are complete on disk and that a follow-up "continue the threat model" run
will resume from the pending markers, in the dependency order from §4.2.
