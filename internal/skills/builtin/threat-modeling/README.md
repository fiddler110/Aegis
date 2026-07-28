# Threat-modeling skill — bundled scripts

This directory is the `threat-modeling` built-in skill. `SKILL.md` is the
playbook the model loads on demand; `references/` holds the framework process
docs and the verbatim skeletons; the six `.py` files here are **deterministic
helpers** that codify the mechanical parts of the workflow so the model spends
its judgment only where judgment is actually required.

All six are **Python 3.7+, standard-library only** (no `pyyaml` or other
third-party deps), emit deterministic sorted output, reconfigure stdout to
UTF-8 for Windows `cp1252` safety, and take an `argparse` CLI. They are
`go:embed`-ed with the skill (`internal/skills/builtin.go`) and surfaced in the
skill's generated `<skill_assets>` manifest, so a run can invoke them by path
without any host-side wiring.

## The scripts

| Script | Skill phase | What it does | CLI |
|---|---|---|---|
| `scaffold.py` | Setup (§4.1 step 2), before any filling | Pre-writes all seven report files **from the skeletons** — real structure (headings, table header rows + separators, fixed-value lists, the DFD's `flowchart LR` + three `classDef`s) with a `<!-- PENDING -->` marker per fillable section — so a weak model **fills sections** instead of authoring structure it gets wrong. A freshly-scaffolded suite already passes `lint_dfd.py` and parses cleanly under `verify.py` (only the PENDING markers and empty-content checks fail). Never clobbers a file whose PENDING markers are gone. | `python scaffold.py <run-dir> --framework <name> [--target SLUG] [--date DATE] [--force]` |
| `recon.py` | Phase 1 (Architecture), first action | One deterministic filesystem pass → compact repo digest (git metadata, language histogram, parsed dependency manifests, bind/listen sites, entry points, config/env keys, security-infra signals, external-egress signals, per-file symbols ranked security-relevant-first, and an **evidence-based suggested deployment class**). Replaces reading source raw — ~11KB digest vs megabytes for a ~540-file repo. | `python recon.py [ROOT] [--json PATH] [--full] [--max-files N]` |
| `inventory.py` | Phase 5 (generate), Phase 6 (`--check`) | Parses the finished `2-<framework>-analysis.md` + `3-findings.md` + `0.1-architecture.md` and emits `inventory.yaml` deterministically, **deriving each threat's tier from its prerequisite** so the sidecar can't disagree with the analysis. `--check` regenerates in-memory and diffs vs the on-disk file, exit non-zero on drift. | `python inventory.py <run-dir>` / `python inventory.py <run-dir> --check` |
| `verify.py` | Phase 6 (review round) | Ten mechanical cross-file assertions: no leftover skeleton syntax, component-name consistency, dataflow refs defined, threat↔coverage bijection, finding-id sequence, tier/prerequisite consistency, count agreement, no forbidden coverage statuses, external-AV consistency, and architecture↔analysis deployment-classification agreement. Built on a generic markdown-table parser (survives column reordering). Prints PASS/FAIL per check. | `python verify.py <run-dir> [--quiet]` |
| `lint_dfd.py` | Phase 6 (when the DFD changed) | Six Mermaid-DFD checks: `flowchart LR` direction, three-palette `classDef`s, no stray fence/keyword, balanced `subgraph`/`end`, labeled edges, and `.mmd`↔`.md` equality. Tolerant of `%%` comments and the `%%{init}%%` block. | `python lint_dfd.py <file.mmd \| 1-model.md \| run-dir>` |
| `diff_inventory.py` | Update workflow (§6) | Diffs two `inventory.yaml` sidecars for the Changes Since Baseline section: classifies each threat new/resolved/still-present/changed via id-match then a fingerprint fallback (component + category + title-ish), with per-threat category and tier deltas. Parses both block-style and the one-line flow-mapping YAML `inventory.py` emits. | `python diff_inventory.py <baseline-inventory.yaml> <current-inventory.yaml>` |

## Design line — facts only, never decisions

Every script does the deterministic extraction/checking and the **model owns
every decision**. `scaffold.py` writes empty structure only — every judgment
cell is a `<!-- PENDING -->` the model fills, and it invents no component,
threat, severity, or deployment class. `recon.py` lists only symbols that
actually exist (so it structurally cannot invent the abstract components the
skill warns against) and
labels deployment class a *suggestion*; `inventory.py`/`verify.py` assert
consistency but never judge severity or eligibility; `diff_inventory.py`
matches on stable keys but does not re-model. Anything a script cannot read
deterministically (e.g. a config/flag-driven bind address) it flags for the
model to confirm rather than guessing.

## Development

```bash
# Compile-check all six (no third-party deps to install)
python -m py_compile *.py

# The scripts are go:embed-ed — after editing any .py, rebuild the binary or
# a running daemon keeps the old copy. A test enforces the embed:
go test ./internal/skills/...
```

The skill is embedded via `//go:embed builtin` (recursive; skips `_`-prefixed
dirs like `__pycache__`, which are never committed). Run scripts from a scratch
dir or clean up `__pycache__` before committing.

## Driving the build on a local model

The unattended drive is `aegis chat --skill threat-modeling --mode build --yes`;
for threat-modeling it runs **phased** (each phase in its own fresh context —
`internal/cli/chat_phased.go`) so peak context stays bounded to one phase. The
TUI's `/threat-model` command is the **interactive** counterpart — it seeds the
skill into the normal chat loop and stops at the model's first yield so a present
user reviews and nudges between phases; it does **not** run the phased
drive-to-completion, the PENDING oracle, or the auto verify/quality pass. Use
`chat --skill` for the hands-off build and `/threat-model` when you want to steer.
The
P47.1–P47.5/P47.7/P47.8 stability fixes make the drive converge regardless of
model, so **no model choice is required for correctness**. But there is a real
**throughput/looping tradeoff** in *which* local model you point it at:

- A small **active-parameter MoE** run in a "fast" mode (e.g. `a3b`, ~3B active)
  **loops more** — it spends turns re-auditing files it already filled and
  recomputing STRIDE/coverage counts by hand — so it burns more tokens and wall
  time to reach the same verify-clean suite. On the 2026-07-24
  FirewallRuleAnalyzer run this self-verification looping was the proximate cause
  of the context growth the stability batch then made resumable.
- A **steadier or larger** model — the `-deep` variant of the same MoE, or a
  larger dense model — **converges with less token burn and fewer loops**. The
  `noSelfVerifyInstruction` woven into the content-phase prompts (P47.3) cuts the
  looping for every model, but a steadier model needs the guardrail less.

So for the fastest unattended convergence, prefer a `-deep`/larger drive model
over a small "fast" MoE; the fast MoE still finishes (the code fixes make it
resumable), it just costs more turns. This is guidance only — there is no code
gate on the drive model.

## Next steps / future work

The suite-scripting batch (**P37.1–P37.5**) shipped 2026-07-19; a live dogfood
eval against a real target (`D:\Development\AiGateway`) on 2026-07-20 then shipped
**P37.6** — a fix to `inventory.py`'s deployment-classification parse (it used to
pick the wrong class when the doc documented an overridden recon suggestion) plus
a new `verify.py` check that the architecture and analysis files agree on the
classification. See [`research/releases.md`](../../../../research/releases.md) for
per-item detail.

The same live eval also exercised the skill through the **real Aegis binary on
local models** (qwen3:14b, mythos-sec:24b) and found that **none of them can drive
the phased sub-agent orchestration** — so the skill is being **pivoted to a
non-orchestrated, single-context linear build** as its primary path (the model
works the phases itself and writes all seven files; these scripts + P36.2 pruning
do the context-bounding that phasing was meant to provide). The follow-up live
test (qwen3:14b, 2026-07-20) then showed the linear build *runs* but its output
did not conform — a 14B model skips the skeleton templates and re-authors
structure — which is why `scaffold.py` (**P38.4**) now applies the skeletons
mechanically. That rework and its dependencies are **P38.1–P38.5** in
[`research/roadmap.md`](../../../../research/roadmap.md); they concern the skill's
build strategy and the `aegis chat` driver, not these scripts.

The remaining **script** leads (filed under the roadmap's Tier-3 recon note, not
yet promoted to `### P<n>.<m>` items):

- **`recon.py` depth follow-ups** (each worth its own item when a concrete need
  appears):
  - *Data-flow edge inference* — seed the DFD's `DF##` flows from import graphs /
    client instantiations so phase 2 starts from real edges instead of
    re-deriving them.
  - *Config-default resolution* — parse the actual default bind address from the
    config struct / `config.yaml` so the deployment class is settled
    deterministically in the common case (instead of punting "confirm the
    default" to the model). A specialization the AiGateway eval raised: downgrade
    an `internet-facing` suggestion to `internal-network` when the k8s `Service`
    is `NodePort`/`ClusterIP` with no ingress/TLS — recon already parses the
    compose/k8s files, so it has the signal.
  - *Richer symbol extraction* — functions/methods and route→handler maps,
    optionally via `ctags`/tree-sitter when on PATH, falling back to today's
    regex.
  - *Target-commit in the sidecar* — `inventory.py`'s `git_commit()` runs
    `git -C <run-dir>`, so when the run dir is kept outside the target repo (the
    recommended clean-target setup) the sidecar records no commit for the analyzed
    code; let it take `--target-dir`/`--repo` or read the commit from
    `0-assessment.md`.
- **Two doc-inconsistency cleanups** surfaced while building the scripts (the
  scripts already handle both forms; the docs should settle on one canonical
  form):
  - *Threat-ID form* — `references/skeletons/skeleton-stride.md` uses bare
    sequential `T1`/`T2`; `references/output-formats.md`'s coverage examples use
    composite `T04.S`.
  - *Inventory YAML style* — `skeleton-inventory.md`'s example is block-style
    while directive #13 says one-line and `inventory.py` emits one-line flow
    mappings; the skeleton example should match what the generator produces.
