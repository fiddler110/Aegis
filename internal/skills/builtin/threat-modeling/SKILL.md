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
| `scaffold.py` (bundled script) | **Setup (§4.1 step 2), before filling any file** | Pre-writes all seven files from the skeletons — real structure (headings, table headers, fixed-value lists, DFD `flowchart LR` + `classDef`s) with `<!-- PENDING -->` per fillable section, so you fill sections rather than author structure |
| `recon.py` (bundled script, not a reference doc) | **Start of the architecture phase, before any manual exploration (§2 step 1)** | Deterministic one-pass repo digest — run it, read its stdout; replaces reading source files raw |
| `inventory.py` (bundled script) | **Phase 5**, to generate `inventory.yaml` from the finished markdown; **phase 6**, with `--check`, to verify it still agrees | `python inventory.py <run-dir>` writes the sidecar (IDs, derived tiers) deterministically; `--check` regenerates in-memory and diffs vs disk, exit non-zero on drift |
| `verify.py` (bundled script) | **Phase 6** review round, over the assembled suite | `python verify.py <run-dir>` — mechanical cross-file self-check (leftover skeleton syntax, name consistency, dataflow refs, threat↔coverage bijection, finding-id sequence, tier/prerequisite, counts, forbidden coverage statuses, external-AV, deployment-classification agreement between architecture and analysis) |
| `lint_dfd.py` (bundled script) | **Phase 6** review round, whenever the DFD changed | `python lint_dfd.py <run-dir>` — Mermaid DFD linter (LR flowchart, three-palette classDefs, no stray fences/keywords, subgraph balance, labeled edges, `.mmd`↔`.md` equality) |
| `diff_inventory.py` (bundled script) | **Update workflow (§6)**, when refreshing a baseline model | `python diff_inventory.py <baseline-inventory.yaml> <current-inventory.yaml>` — classifies threats new/resolved/still-present/changed for the Changes Since Baseline section |
| `references/stride.md` / `linddun.md` / `pasta.md` / `trike.md` / `vast.md` / `nist-800-154.md` | Framework chosen, before exploring the workspace | That framework's process/stages and category definitions |
| `references/output-formats.md` | Before writing any of the five framework-agnostic files (`0-assessment.md`, `0.1-architecture.md`, `1.1-model.mmd`, `1-model.md`, `3-findings.md`) | Verbatim templates, mandatory fields (tier, CVSS 4.0, CWE, OWASP), the Threat Coverage Verification loop, and per-file post-write checks |
| `references/diagram-conventions.md` | Before writing `1.1-model.mmd` or any diagram in `0.1-architecture.md` | Mermaid shapes, fixed color palette, DFD direction, pre-render checklist |
| `references/skeletons/skeleton-<framework>.md` | **Before writing `2-<framework>-analysis.md` (§4.2), and again before filling each of its sections** | Verbatim document structure for the framework's own analysis file, fixed value lists, inline `<!-- ⛔ POST-*-CHECK -->` self-verification comments |
| `references/companion-techniques.md` | After the framework pass, or if attacker realism/backlog framing was asked for | Technology sweep, Attack Trees, MITRE ATT&CK mapping, Evil User Stories |
| `references/skeletons/skeleton-inventory.md` | Writing `inventory.yaml` (§6) | Exact field names and structure for the sidecar |
| `references/verification-and-updates.md` | Before finishing (§6) | Final self-check, sidecar rules, update workflow, single-context build governance |

## 2. Explore the workspace before modeling

This exploration is the **architecture phase (phase 1 of §4.2)** — and the
bounded-read discipline below is what keeps that phase's peak context small
enough to survive a local model. Never model an assumed architecture. Before
applying any framework:

1. **Run the recon script first — it does the bulk gathering deterministically,
   outside your context.** `recon.py` (Python 3, stdlib only, bundled with this
   skill — its path is in the skill-assets manifest) walks the whole workspace
   in one pass and prints a compact digest: git metadata, languages, dependency
   manifests, bind/listen sites with a suggested deployment classification,
   entry points, config/env keys, security-infrastructure signals, external
   egress signals, and per-file declared symbols ranked security-relevant
   first. Run it against the workspace root —
   `python <path>/recon.py <workspace-root>` — and read its stdout digest
   *instead of* reading dozens of source files yourself. The digest for a
   500-file repo is a few KB; reading those files raw would be megabytes, and
   on a local model that peak context is exactly what kills the run. The
   script's output is deterministic (same repo → same digest), which is what
   makes component names, the exposure classification, and `inventory.yaml`
   ids stable across runs. Everything the digest labels a *suggestion*
   (deployment class, security infra, component candidates) is evidence for
   **you** to confirm or override per the rules below — not a decision the
   script made; verify at the cited file before relying on it.
2. **Then read selectively, only to confirm or fill gaps the digest leaves.**
   The digest points you at the exact files that matter — the listener call
   sites, the auth/secret-handling files, the entry points, the component
   candidates carrying security signals. Open *those* to confirm the finding,
   and **read large files in bounded excerpts, not whole**: page through with
   `read_file`'s `offset`/`limit`, or `grep` for the specific route, config
   key, or data-access call the digest flagged, and read only that region.
   Never re-read the whole tree the script already summarized. On a local
   model this is not optional: one whole-file read of a large handler can eat
   half a turn's token budget, and every later turn repays that context.
3. From what recon reported and you confirmed, identify: **assets** (data, credentials,
   capabilities worth protecting), **trust boundaries** (process/network/
   privilege boundaries the system crosses), **entry points** (where
   untrusted input enters), and **data flows** (how data moves between
   components, including third-party/external dependencies).
4. **Anchor every component to a real code artifact.** The recon digest's
   "component candidates" are already anchored to a file — that is their whole
   point. For each component you keep in the model you must be able to cite the
   specific class, file, or manifest that is its anchor — if you can't cite one,
   the component doesn't exist; delete it rather than modeling an invented
   abstraction (`ConfigurationStore`, `DataLayer`) that no code implements
   (the digest lists only symbols that actually exist, so it structurally
   can't invent these — but you can, so don't). Curate the candidates down to
   real components per §4.2 eligibility, and name each after its anchor, not a
   synonym of it, so a re-run on the same code produces the same names and
   `inventory.yaml`'s ids stay stable across runs.
5. **Classify the deployment, and let it bound severity.** Recon prints a
   *suggested* classification with its evidence (listener call sites, bind
   addresses, Dockerfile/Helm/compose signals) — confirm or override it, don't
   just copy it. The classes are `internet-facing`, `internal-network`,
   `localhost-service` (daemon bound to 127.0.0.1), or `local-desktop` (no
   listener at all). Watch the case recon flags explicitly: a listener whose
   bind address is config/flag-driven rather than a literal — check the config
   default and any bind-to-all / allow-remote flag before settling the class.
   The classification is binding: a localhost-only daemon has no
   "unauthenticated remote attacker" threats — the floor prerequisite for
   every threat is local process access, and severity must reflect that.
   Record it, with evidence, in `0.1-architecture.md`'s Component Exposure
   Table — that table is the single source of truth every later file's
   prerequisite/tier must respect (`output-formats.md`).
6. Only then apply the chosen framework's process against this real map —
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

## 4. Build the suite yourself, one phase at a time

Every run produces the same seven files, in the same directory, regardless
of framework — the file list, naming, and directory convention are in
`references/output-formats.md`; read it now if you haven't. Do not stop
after a partial pass — populate every category/stage/cell the chosen
framework defines, even ones with no findings ("none identified" is a valid,
complete entry; a missing cell is not).

**You build the whole suite yourself, in one context — no sub-agents, no
delegation.** A threat model that runs as one ever-growing conversation would
accumulate every reference file, every workspace read, and every written
report's content in a single context, and on a local model that peak context
is what kills the run: a large prefill can blow the model server's
response-header timeout before a single file is written. Bounding that peak
does **not** require phasing work out to throwaway contexts — four levers
already built for exactly this keep a single-context build inside the window:

- **`recon.py` replaces the megabyte architecture reads with an ~11KB digest**
  (§2 step 1), so the architecture phase never pulls the whole tree into
  context.
- **Context pruning drops spent payloads automatically.** Once a file is
  written, its `write_file`/`edit_file` payload — and one-time skill/reference
  reads — fall out of the running context, so a phase you have finished stops
  costing tokens on every later turn.
- **Incremental writes keep the working set small.** `scaffold.py` writes the
  structured stubs once (§4.1), then you `edit_file` one section at a time
  (§4.2), so you never hold a whole file in context to produce it.
- **The deterministic scripts do the bulk mechanical work outside your
  context.** `inventory.py`, `verify.py`, and `lint_dfd.py` mean you never
  have to hold the entire analysis in context to generate the sidecar or run
  the checks.

Work the phases **in dependency order** (architecture → DFD → framework
analysis → findings → assessment → self-check), letting the prior phase's file
on disk — plus a short running note of its stable identifiers — ground the
next, rather than re-reading everything.

### 4.1 Setup

Do the cheap, decision-bearing setup first, then work the phases in order.

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

2. **Run `scaffold.py` to pre-write all seven files with real structure** —
   don't hand-write bare stubs. `python <path>/scaffold.py <run-dir>
   --framework <name> [--target <slug>] [--date <YYYY-MM-DD>]` writes
   `0-assessment.md`, `0.1-architecture.md`, `1.1-model.mmd`, `1-model.md`,
   `2-<framework>-analysis.md`, `3-findings.md`, and an `inventory.yaml`
   placeholder, each with its **fixed structure already in place** — every
   heading, every table's header row and separator, the DFD's `flowchart LR`
   header and three `classDef`s, the fixed-value reference lists — and a
   `<!-- PENDING -->` marker wherever run-specific content goes. You then
   **fill sections** (`edit_file`, §4.2) rather than authoring the structure:
   the skeletons determine the shape, the script applies it, and you supply
   only the judgment. This is what makes `verify.py`/`lint_dfd.py` check a
   real structure from turn one (a freshly-scaffolded DFD already lints clean)
   and keeps every file resumable and self-describing (§4.2 "Resume"). The
   script never clobbers a file whose `<!-- PENDING -->` markers are already
   gone (i.e. one you've started filling), so re-running it on an in-progress
   directory is safe. Pass `--framework stride-a` for a STRIDE-A run.

Never delete or overwrite a *prior dated run directory* — an update is a new
directory, and the old one is the baseline it was diffed against (editing
today's own in-progress directory is, of course, the normal flow).

### 4.2 Work the phases, in dependency order

Work each phase yourself, in order, reading **only that phase's inputs** and
writing **only that phase's file(s)**. You do not need to carry a phase's full
output forward — its file is on disk, and pruning will drop the write payload
from your running context — so between phases keep only a **short running note
of the stable identifiers** the next phase needs (component names + anchors,
`DF##` ids, threat IDs with their component/category/prerequisite/tier/
severity). That compact note is what keeps names consistent across files
without re-reading each prior file whole; it is cheap to hold, while the bulk
(prose, tables) stays on disk where it belongs.

The phases, in the dependency order of the file suite:

| # | Phase | Reads (only this, plus prior files from disk) | Owns / fills | Note forward (stable identifiers only) |
|---|---|---|---|---|
| 1 | Architecture | runs `recon.py` (§2 step 1) then reads its digest; `output-formats.md`; targeted confirmation reads (§2 steps 2–5 + §3 evidence rules) | `0.1-architecture.md` | component names + types + anchors; deployment classification; each component's exposure floor; security-infra component names |
| 2 | Model / DFD | `diagram-conventions.md`; `0.1-architecture.md` | `1.1-model.mmd`, `1-model.md` | element names; `DF##` ids with source→target; trust-boundary names |
| 3 | Framework analysis | `skeletons/skeleton-<framework>.md`, the framework reference (`stride.md`/etc.), `companion-techniques.md`; `0.1-architecture.md`, `1-model.md` | `2-<framework>-analysis.md` | every threat ID with (component, category, prerequisite, tier, severity, has-mitigation) |
| 4 | Findings | `output-formats.md` (findings section); `2-<framework>-analysis.md`, `0.1-architecture.md`'s exposure table | `3-findings.md` | `FIND-##` ids with (threat IDs covered, tier, severity) |
| 5 | Assessment + inventory | `output-formats.md` (assessment section), `skeletons/skeleton-inventory.md`; all prior files | `0-assessment.md`, then run `python inventory.py <run-dir>` to generate `inventory.yaml` | tier/threat/finding counts; confirmation both written |
| 6 | Review round (§5) | the **complete** suite, fresh from disk; runs `verify.py`, `lint_dfd.py`, `inventory.py --check` | edits in place across all files | seams fixed; all three scripts' pass/fail |

Phase 3 must **copy the skeleton structure exactly** (same columns, same
order, same fixed value lists) and run its inline `<!-- ⛔ POST-*-CHECK -->`
comments right after writing each table, and **run the technology sweep** in
`companion-techniques.md`. Phase 4 runs the Threat Coverage Verification loop.
Phase 5 recounts from the finished files, never carrying a stale mid-analysis
number, and generates `inventory.yaml` by running `python inventory.py
<run-dir>` rather than hand-writing it — the script derives each threat's tier
from its prerequisite and emits stable, sorted, deterministic YAML, so the
sidecar can't drift from the analysis or vary between runs. Phase 6 runs the
three bundled check scripts (below) over the assembled suite before any debate.

Grounding each phase in the prior phase's exact identifiers — the running note
above, cross-checked against the file on disk — is what keeps names from
diverging across files: phase 2 reuses phase 1's verbatim component names,
phase 3 phase 2's `DF##` ids, and so on, so no phase invents a divergent name.
The phase-6 review round re-reading the whole suite from disk is the backstop
that catches any drift the note missed (`references/verification-and-updates.md`).

**Write incrementally, never all-at-once.** The files on disk are the working
state, and — with context pruning dropping each spent write payload — writing
in small pieces is what keeps a single-context build bounded. This matters most
in phase 3: `scaffold.py` already wrote the analysis file's structure (§4.1), so
`edit_file` it **one component/section at a time** — replacing one
`<!-- PENDING -->` marker per edit — rather than regenerating the whole file
with `write_file`. Your context never has to hold every section you already
wrote; that is what bounds even a large analysis file's peak context, the
failure mode this whole approach targets. (Re-authoring the whole file each
pass is exactly the non-convergent behavior scaffolding exists to prevent.)

**Resume, don't restart.** If the target directory already exists and any file
still contains `<!-- PENDING -->` markers, a previous run was interrupted.
Resume from the **first unfinished phase in dependency order**: check each
file for `<!-- PENDING -->` and skip a phase whose file is already complete
(re-read just its identifiers into your note without rewriting), continuing
from the first file that still has pending work.

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

**This is a long, single-context run — drive it to completion, keep it
resumable.** The build is many turns in one session, and there is no shared
agent deadline to split across sub-agents — you are the single context doing
every phase. Two consequences. First, **do not stop mid-build to ask whether
to continue**: a threat model is a long, non-interactive job, so work straight
through until no `<!-- PENDING -->` marker remains, rather than writing a few
files and handing back a partial suite with a question. Second, because you
write incrementally, an interrupted run always leaves complete files on disk
with only `<!-- PENDING -->` work outstanding — so if the run genuinely must
stop (step limit, real context pressure), say which files are complete and a
follow-up "continue the threat model" run resumes from the pending markers in
dependency order.

## 5. Final review round — consistency, then debate (P12) when enabled

This is **phase 6** — the final phase, which you run yourself over the finished
suite. Because the earlier phases were written incrementally and context
pruning has since dropped most of that content from your context, this phase
deliberately re-reads the **complete suite fresh from disk** — it is the one
place the whole model is seen together at once. After every file's
`<!-- PENDING -->` markers are gone, re-read the whole suite and review it as a
whole — the files were written one phase at a time, each grounded only in your
running identifier note, so this is where seams show:

- Component names and threat IDs are consistent across all seven files; no
  file refers to a component another file renamed or dropped.
- Every threat's prerequisite still respects the deployment classification's
  floor from §2, and its derived tier is consistent with its CVSS vector's
  `AV`/`PR` values (a `Local Process` prerequisite cannot carry `AV:N`).
- Every threat in `2-<framework>-analysis.md` appears exactly once in
  `3-findings.md`'s Threat Coverage Verification table.
- No two files contradict each other about the same control or data flow.

Run the three bundled check scripts first — they mechanize this checklist so
the manual read confirms rather than hunts:

- `python verify.py <run-dir>` — cross-file consistency (leftover skeleton
  syntax, component-name consistency, dataflow refs defined, threat↔coverage
  bijection, finding-id sequence, tier/prerequisite consistency, count
  agreement, forbidden coverage statuses, external-AV consistency, and that
  `0.1-architecture.md` and the analysis file agree on the deployment
  classification — a divergence silently skews every derived tier).
- `python lint_dfd.py <run-dir>` — the DFD's Mermaid conventions and
  `.mmd`↔`.md` equality.
- `python inventory.py <run-dir> --check` — that `inventory.yaml` still
  regenerates identically from the current markdown (catches a threat added or
  a tier changed after the sidecar was written).

Each exits non-zero and names the failing check. Fix what any script flags,
then re-run it until clean. Then do the manual read for the judgment calls no
script can make (does a control actually contradict a data flow; is a
prerequisite realistic). Fix what fails by editing the files in place — the
full checklist is `references/verification-and-updates.md`'s "Final
self-check".

Then, if your system prompt's "Debate mode (P12)" section marks threat
modeling enabled, route the **contested entries** — high-severity threats
and any whose severity or mitigation is genuinely arguable — through the
`agent` tool's `mode:"debate"`, with `claim` set to the threat description,
severity, and proposed mitigation. Debate is a separate primitive from the
(now removed) build orchestration — a single `agent` call that returns an
arbiter verdict, not a workflow to drive — so you run it directly and patch
the verdict back into `2-<framework>-analysis.md` and `3-findings.md`: adjust
severity/mitigation per a REVISE verdict, drop the entry per a REJECT verdict,
keep it as-is per UPHOLD. (At the normal slash-command invocation this run is
depth 0 and its debate roles are depth 1 — within the depth-3 spawn ceiling.)
Skip clear-cut, uncontroversial entries.
Debating here, over the assembled suite, lets the debaters see each threat in
the context of the whole model rather than in isolation.

## 6. Self-check, inventory, and updates

Read `references/verification-and-updates.md` before finishing. It covers
four things: **single-context build governance** (each phase owns specific
files and you carry only stable identifiers forward between them, with
consistency guaranteed by that note plus the phase-6 review round re-reading
the whole suite — the rule for §4.2's linear build), the **inventory
sidecar** (`inventory.yaml`, with stable component/threat IDs so a future
run can diff against this one), the **update workflow** for when the ask is
"update/refresh the threat model" or "what changed since last time" (locate
the baseline directory, verify each baseline threat against the current
code, produce a standalone new directory with a Changes Since Baseline
section, rather than modeling from scratch or trusting the old files'
claims — once the new directory's `inventory.yaml` exists, run `python
diff_inventory.py <baseline>/inventory.yaml <new>/inventory.yaml` to classify
threats new/resolved/still-present/changed and drive that section
mechanically), and the **final self-check** (coverage, prerequisite/tier
consistency, anchors, evidence, cross-file agreement — run it and fix what
fails before reporting).

## 7. Report

The directory in `.aegis/security/threat-model/` (built by the phases in
§4.2) is the deliverable — a chat-only summary is not a complete threat
model, since the whole point is a navigable suite mitigations can be
tracked against. Write this report after the phases complete — the file
content is on disk, so your running identifier note plus a re-read of
`0-assessment.md` are all you need to summarize it. State which framework
was used (and why, if inferred rather than requested) and the deployment
classification up front — both also belong in `0-assessment.md`'s Executive
Summary. The task is done only when no `<!-- PENDING -->` marker remains in
any of the seven files, the §5 review round (phase 6) has run over the
assembled suite, the final self-check passes, and `inventory.yaml` exists and
agrees with the documents. If the run must
stop early anyway (step limit, context pressure), say plainly which files
are complete on disk and that a follow-up "continue the threat model" run
will resume from the pending markers, in the dependency order from §4.2.
