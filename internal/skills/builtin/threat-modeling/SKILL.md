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

This exploration happens **inside the architecture phase (phase 1 of §4.2)**,
not the top-level run — and the bounded-read discipline below is what keeps
that phase's peak context small enough to survive a local model. Never model
an assumed architecture. Before applying any framework:

1. Explore the workspace: list directories, read entry points, config,
   auth/authz code, network-facing handlers, and data-access layers.
   **Read large files in bounded excerpts, not whole.** When a file is big
   (a multi-hundred-line handler, a ~100KB single-file script), don't pull
   it into context in one `read_file` call — page through it with
   `read_file`'s `offset`/`limit`, or run a targeted `grep`/search for the
   entry points, config keys, routes, and data-access calls you actually
   need and read only those regions. On a local model this is not optional:
   one whole-file read of a large script can eat half a turn's token budget,
   and every later turn repays that context, so keep each turn's reads small
   and targeted.
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

## 4. Build the suite as isolated phases

Every run produces the same seven files, in the same directory, regardless
of framework — the file list, naming, and directory convention are in
`references/output-formats.md`; read it now if you haven't. Do not stop
after a partial pass — populate every category/stage/cell the chosen
framework defines, even ones with no findings ("none identified" is a valid,
complete entry; a missing cell is not).

**A threat model that runs as one ever-growing conversation accumulates
every reference file, every workspace read, and every written report's
content in a single context.** On a local model that peak context is what
kills the run: a large prefill can blow the model server's response-header
timeout before a single file is written. The fix is structural — **build the
suite as a sequence of isolated phases, each in its own throwaway context,
passing forward only short structured identifiers, never file content,
because the content is already durably on disk.** What changes from a naive
run is only *where* each file is filled: not in this top-level run's context,
but in a delegated build sub-agent that loads only that phase's inputs and
unloads when it returns.

### 4.1 Top-level setup (this run)

Do only the cheap, decision-bearing setup here, then delegate the heavy work.
Keep this run's own context small — do **not** explore the workspace or read
framework skeletons here; each phase reads what it needs in its own context.

1. Framework is already chosen (§1). Decide the `<target>` slug and the
   `<YYYY-MM-DD-HHMM>` timestamp (local time, 24h, dash-separated — get it
   from the `shell` `date` command, never guess it), and create the directory
   `.aegis/security/threat-model/<framework>-<target>-<YYYY-MM-DD-HHMM>/`.

   The `<target>` slug is **mandatory, never omitted**: use the scoped
   feature/system name when the model covers one (`webui`, `auth-service`),
   or the repo/workspace directory name when it covers the whole project
   (`aegis`) — a reader scanning `.aegis/security/threat-model/` must be able
   to tell what each directory modeled without opening it. The timestamp
   (e.g. `stride-aegis-2026-07-08-1432`) keeps two same-day runs from
   colliding; a date alone isn't enough since a full run plus an update can
   both land on one day.

2. `write_file` a `<!-- PENDING -->` stub for **all seven files** before
   delegating any of them: `0-assessment.md`, `0.1-architecture.md`,
   `1.1-model.mmd`, `1-model.md`, `2-<framework>-analysis.md`,
   `3-findings.md`, `inventory.yaml` — each with just a top-level heading and
   `<!-- PENDING -->`. The phase that fills a file overwrites its stub with
   the exact skeleton, so these stubs need no real structure; they exist so a
   crash leaves a resumable, self-describing directory (§4.2 "Resume").

Never delete or overwrite a *prior dated run directory* — an update is a new
directory, and the old one is the baseline it was diffed against (editing
today's own in-progress directory is, of course, the normal flow).

### 4.2 Delegate the build to isolated phases, in dependency order

Issue **one** `agent` tool call with `mode: "sequential"` and an `agents`
array — one entry per phase, `subagent_type: "build"` for **every** entry
(each phase writes files, so it needs write access; a plan-mode session
cannot run this skill, same as today). The sequential workflow runs each
phase in a fresh, isolated context and threads **only the prior phase's final
text answer** forward into the next phase's prompt — a real context unload,
so phase N never carries phase N-1's reads or writes.

Each phase entry's `prompt` must state explicitly: the output directory path
and framework; which reference file(s) to read (**only its own**); which
file(s) it owns and must fill; and — verbatim — the terse-final-answer
contract below.

**⛔ The terse-final-answer contract — the single most important detail.**
Every phase's final answer is prepended verbatim to the next phase's prompt
(that is how the sequential workflow threads context). A phase that ends by
dumping its file's content, or re-narrating the threats it just wrote,
reinjects exactly the bloat this design exists to remove — one level down,
into every later phase. So end **every** phase prompt with this instruction:

> *Your files on disk are the deliverable. Your final answer must be ONLY a
> terse structured list of the stable identifiers the next phase needs —
> component names and anchors, `DF##` ids, threat IDs with their (component,
> category, prerequisite, tier, severity) — never the file's prose or table
> content. Keep it under ~40 lines.*

The phases, in the dependency order of the file suite:

| # | Phase | Reads (only this, plus prior files from disk) | Owns / fills | Returns (terse identifiers only) |
|---|---|---|---|---|
| 1 | Architecture | `output-formats.md`; the workspace (§2 exploration + §3 evidence rules) | `0.1-architecture.md` | component names + types + anchors; deployment classification; each component's exposure floor; security-infra component names |
| 2 | Model / DFD | `diagram-conventions.md`; `0.1-architecture.md` | `1.1-model.mmd`, `1-model.md` | element names; `DF##` ids with source→target; trust-boundary names |
| 3 | Framework analysis | `skeletons/skeleton-<framework>.md`, the framework reference (`stride.md`/etc.), `companion-techniques.md`; `0.1-architecture.md`, `1-model.md` | `2-<framework>-analysis.md` | every threat ID with (component, category, prerequisite, tier, severity, has-mitigation) |
| 4 | Findings | `output-formats.md` (findings section); `2-<framework>-analysis.md`, `0.1-architecture.md`'s exposure table | `3-findings.md` | `FIND-##` ids with (threat IDs covered, tier, severity) |
| 5 | Assessment + inventory | `output-formats.md` (assessment section), `skeletons/skeleton-inventory.md`; all prior files | `0-assessment.md`, `inventory.yaml` | tier/threat/finding counts; confirmation both written |
| 6 | Review round (§5) | the **complete** suite, fresh from disk | edits in place across all files | seams fixed; final self-check pass/fail |

Phase 3 must **copy the skeleton structure exactly** (same columns, same
order, same fixed value lists) and run its inline `<!-- ⛔ POST-*-CHECK -->`
comments right after writing each table, and **run the technology sweep** in
`companion-techniques.md` — the same rules as before, now inside that phase's
context. Phase 4 runs the Threat Coverage Verification loop. Phase 5 recounts
from the finished files, never carrying a stale mid-analysis number.

Grounding each phase in the prior phase's exact identifiers (threaded as the
sequential workflow's forwarded context) is what keeps names from diverging
across files — phase 2 gets phase 1's verbatim component names, phase 3 gets
phase 2's `DF##` ids, and so on, so no phase can invent a divergent name.
Together with the phase-6 review round re-reading the whole suite from disk,
this replaces the old "only the top-level run writes files" rule
(`references/verification-and-updates.md`, revised for this design).

**Within a phase, still write incrementally, never all-at-once.** The files
on disk are the working state. This matters most in phase 3: `write_file` the
skeleton stub, then `edit_file` **one component/section at a time**, so the
phase's own context never has to hold every section it already wrote — that
is what bounds even a large analysis file's peak context, the failure mode
this whole restructure targets.

**Resume, don't restart.** If the target directory already exists and any
file still contains `<!-- PENDING -->` markers, a previous run was
interrupted. Re-run the same sequential workflow: each phase must first check
whether its own file is already complete and free of `<!-- PENDING -->`, and
if so return its identifiers from a quick re-read without rewriting, so the
run continues from the first unfinished phase in dependency order.

Two cross-framework rules every phase enforces while filling the template:

- **Every threat states its prerequisite** (none / authenticated user /
  internal network / local process / host compromise), and no prerequisite
  may sit below the deployment classification's floor from §2, or the derived
  tier (`output-formats.md`) will be wrong.
- **Risk acceptance is not yours to make.** Never mark a threat "accepted
  risk" on your own authority — an unmitigated threat is an open finding with
  a proposed mitigation, and accepting it is the owning team's decision
  (Trike formalizes exactly this; see its reference).

If the ask also touches attacker realism, backlog integration, or
Agile-native framing, phase 3 should also pull Attack Trees, MITRE ATT&CK
mapping, or Evil User Stories from `references/companion-techniques.md` —
optional add-ons layered on top of the chosen primary framework, not
replacements for it.

**Time budget.** The sequential workflow shares one deadline of roughly
`10 min × (phases + 1)` across all phases — about **70 minutes** of
wall-clock for the six phases above — drawn from a shared pool, not a hard
per-phase cap, so a slow phase (the framework-analysis phase writing a large
file on local hardware) can run well past 10 minutes as long as the whole run
fits the pool. If a target is large enough that one phase would still
dominate, **split that phase** — e.g. give the framework analysis two entries,
one per subsystem — which both lowers that phase's peak context *and raises*
the shared pool (the deadline grows with phase count). If the pool is
exhausted mid-run, the completed phases' files are already on disk; a
follow-up "continue the threat model" run resumes from the pending markers.

## 5. Final review round — consistency, then debate (P12) when enabled

This is **phase 6** of §4.2 — the last entry in the sequential `agents`
array, run in its own fresh context. Because the earlier phases each wrote
their files in an isolated context that has since unloaded, this phase is the
one place the whole suite is seen together. After every file's
`<!-- PENDING -->` markers are gone, re-read the **complete suite from disk**
and review it as a whole — the files were written one phase at a time, each
grounded only in the last phase's forwarded identifiers, so this is where
seams show:

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
severity, and proposed mitigation. This phase is itself a build sub-agent and
has the `agent` tool, so it runs the debate directly and patches the
arbiter's verdict back into `2-<framework>-analysis.md` and `3-findings.md`:
adjust severity/mitigation per a REVISE verdict, drop the entry per a REJECT
verdict, keep it as-is per UPHOLD. (At the normal slash-command invocation
this run is depth 0, phase 6 is depth 1, and its debate roles are depth 2 —
within the depth-3 spawn ceiling.) Skip clear-cut, uncontroversial entries.
Debating here, over the assembled suite, lets the debaters see each threat in
the context of the whole model rather than in isolation.

## 6. Self-check, inventory, and updates

Read `references/verification-and-updates.md` before finishing. It covers
four things: **phased-orchestration governance** (each phase owns specific
files and returns only stable identifiers, and consistency is guaranteed by
threading those identifiers forward plus the phase-6 review round re-reading
the whole suite — the revised rule for §4.2's design), the **inventory
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

The directory in `.aegis/security/threat-model/` (built by the phases in
§4.2) is the deliverable — a chat-only summary is not a complete threat
model, since the whole point is a navigable suite mitigations can be
tracked against. Write this report in the top-level run, after the sequential
workflow returns (its combined terse phase summaries are all you need — the
file content is on disk, not in this run's context). State which framework
was used (and why, if inferred rather than requested) and the deployment
classification up front — both also belong in `0-assessment.md`'s Executive
Summary. The task is done only when no `<!-- PENDING -->` marker remains in
any of the seven files, the §5 review round (phase 6) has run over the
assembled suite, the final self-check passes, and `inventory.yaml` exists and
agrees with the documents. If the run must
stop early anyway (step limit, context pressure), say plainly which files
are complete on disk and that a follow-up "continue the threat model" run
will resume from the pending markers, in the dependency order from §4.2.
