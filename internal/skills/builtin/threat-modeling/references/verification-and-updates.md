# Verification, Inventory, and Updates

Framework-agnostic finishing steps: the inventory sidecar that makes a run
re-runnable and diffable, the workflow for updating an existing run, and the
final self-check across the whole seven-file suite. All three apply
whichever framework was used.

## Inventory sidecar

`inventory.yaml`, written directly in the run's output directory alongside
the other six files. The exact field names, structure, and post-write
self-check live in `references/skeletons/skeleton-inventory.md` — copy that
skeleton verbatim rather than improvising the shape here.

Rules that make the diff work:

- **IDs come from anchors.** A component's `id` derives from its real
  class/file/manifest name — two runs on the same code must produce the
  same IDs. Never rename a component between runs; if a prior inventory
  exists, reuse its IDs for components that still exist.
- **Every component has an `anchor`** — the artifact from SKILL.md §2's
  anchor rule. No anchor, no entry.
- **Threat IDs are stable within a lineage.** When updating, a still-present
  threat keeps its baseline ID; new threats take the next free numbers.
  Never reuse a resolved threat's ID for a new threat.
- **Tier is always derived from prerequisite**, never assigned independently
  — see the mapping in `output-formats.md`'s `3-findings.md` section. A
  sidecar entry whose `tier` disagrees with its `prerequisite` is a defect
  in the sidecar, not a legitimate exception.

## Updating an existing model

When the ask is "update/refresh the threat model" or "what changed since
last time":

1. **Locate the baseline directory** — the one the user named, or the most
   recent `.aegis/security/threat-model/<framework>-<target>-*/` directory
   matching the target slug (sort by the directory name's timestamp, not
   directory listing order). If none exists, say so and fall back to a
   fresh full run.
2. **Keep the baseline's framework** unless the user explicitly asks for a
   different one (a framework switch is a new run, not an update — say that
   if it comes up).
3. **Verify each baseline threat against the current code.** Do not trust
   the old files' prose — re-check each threat's evidence at its anchor.
   Classify each as **still-present** (evidence still holds), **resolved**
   (the mitigation landed or the code path is gone — cite what changed), or
   **changed** (still real but severity/prerequisite/mitigation needs
   revision).
4. **Discover what's new.** Re-run the SKILL.md §2 exploration for
   components, entry points, and flows the baseline doesn't cover. If the
   baseline's `inventory.yaml` recorded a commit, `git diff --stat <commit>`
   and `git log <commit>..` (via the `git` tool) are the cheap way to focus
   this pass.
5. **Produce a standalone new run directory** — complete on its own, not a
   patch to the baseline's files — following the exact same seven-file
   structure, with one addition: `0-assessment.md` gets a
   `## Changes Since Baseline` section right after Executive Summary,
   listing counts and per-item summaries of new / resolved / still-present /
   changed threats and findings. Write a fresh `inventory.yaml` per the
   ID-stability rules above, with the `baseline` block and each threat's
   `change_status` populated (`skeleton-inventory.md`'s incremental
   extension).
6. **Never delete or modify the baseline directory.** An update is a new
   timestamped directory; the old one remains the artifact this run was
   diffed against.

An update run uses the **same linear build** as a fresh run (SKILL.md §4.2),
with the baseline threaded in: locate the baseline (step 1) and read its
`inventory.yaml`, then carry the baseline's component and threat IDs into your
running identifier note before phase 1 so those IDs are reused rather than
reinvented (the ID-stability rules above). Phase 3 verifies each baseline
threat against the current code and assigns new threats the next free IDs;
phase 5 adds the `## Changes Since Baseline` section and writes the `baseline`
block and per-threat `change_status`. The baseline IDs are exactly the kind of
stable identifier the running note is meant to carry across a phase boundary.

## Single-context build governance

SKILL.md §4.2 builds the suite yourself, in one context, phase by phase in
dependency order — no sub-agents, no delegation. Peak context per request, not
who holds the pen, is what kills a local-model run, and the bound comes from
four levers (recon's digest, context pruning of spent write/read payloads,
incremental writes, and the deterministic scripts — SKILL.md §4) rather than
from phasing work out to throwaway contexts. The rules that keep a build whose
writes span many turns consistent are:

- **Each phase owns and writes specific files, in dependency order.** Phase 1
  owns `0.1-architecture.md`, phase 2 the model files, phase 3
  `2-<framework>-analysis.md`, and so on (SKILL.md §4.2's table). Fill a
  phase's file before moving on; the phase-6 review round is the only step
  that edits across all files, and it only reconciles seams, it does not
  author new content.
- **Carry only stable identifiers forward between phases — never file
  content.** Keep a short running note of the identifiers the next phase needs
  (component names + anchors, `DF##` ids, threat IDs with their
  category/prerequisite/tier/severity); the prose and tables stay on disk,
  where the earlier file already holds them and where pruning has dropped the
  write payload from context. Re-reading a whole prior file back into context
  to continue reintroduces exactly the bloat this design removes — read from
  disk only to confirm a specific identifier, not to reload the file.
- **Grounding each phase in the prior identifiers is what enforces
  consistency.** Because phase N+1 works from phase N's verbatim names/ids in
  the note, no phase invents a divergent component name or threat id. What
  that cannot catch — a name a later phase still drifts on, a `DF##`
  referenced but never defined, a count that disagrees — is caught by the
  phase-6 review round re-reading the complete suite fresh from disk and
  fixing it in place (SKILL.md §5). That review round is mandatory precisely
  because the files were written across many turns, most of which pruning has
  since dropped from context.

## Final self-check

Run this across the whole seven-file suite before reporting; fix failures
rather than noting them:

**Per-file (catches what a single-file pass would have caught immediately —
should already be clean if each file's own "Post-write checks" in
`output-formats.md` ran at write time):**
- [ ] Every component/element has coverage in `2-<framework>-analysis.md` —
      every applicable category populated, or an explicit "none identified
      — <why>" entry, never a silently missing cell.
- [ ] Every open threat has a proposed mitigation or is flagged as an open
      finding — no threat dropped between enumeration and the findings file.
- [ ] No threat's prerequisite sits below its component's floor in the
      Component Exposure Table, and tier/severity/CVSS AV are mutually
      consistent (`output-formats.md`'s tier-derivation table).
- [ ] Every component cites its anchor artifact; anything unanchorable was
      deleted, not kept.
- [ ] Every threat cites evidence (file/config/code path) per SKILL.md §3 —
      unevidenced suspicions live in "Needs Verification" in
      `0-assessment.md`, not the threat table.
- [ ] No "accepted risk" written on the model's own authority anywhere in
      the suite (Trike's owner-attributed exception aside).
- [ ] No stray skeleton syntax remains — grep every file for `[FILL`,
      `[REPEAT`, `[END-REPEAT` before reporting done; each hit is a
      placeholder that was never actually filled in.
- [ ] No section was *emptied* instead of filled — a heading standing over
      blank space, or a table reduced to its header + separator, is a
      placeholder too even though the `<!-- PENDING -->` marker is gone.
      `verify.py`'s `section-bodies-nonempty` check reports these by
      `file:line` from `scaffold.py`'s `.scaffold-manifest.json`.
- [ ] No section was *filled with nothing*: an Evidence cell that names a
      file but pins nothing inside it, a `TBD`/`N/A`/`see code` in a
      Mitigation or Residual-risk cell, a table whose every row reads "none
      identified", or a narrative section shorter than a sentence.
      `verify.py`'s `evidence-cells-cited`, `no-placeholder-cells`,
      `none-identified-fraction` and `prose-sections-substantive` checks
      report each by `file:line`. One or two "none identified" rows is a
      *correct*, complete entry and never fails — only a table that is
      nothing else does.
- [ ] Every top-level directory from recon.py's digest appears exactly once
      in `0.1-architecture.md`'s Coverage Ledger, as `Covered — <component>`
      or `Excluded — <reason>` — including recon's auto-excluded directories
      (SKILL.md §2 step 6).

**Cross-file (this is what the whole-suite review actually exists to
catch — see SKILL.md §5):**
- [ ] Component names are identical, verbatim, across
      `0.1-architecture.md`, `1-model.md`'s Element Table, and
      `2-<framework>-analysis.md`'s section headings.
- [ ] Every `DF##` referenced in `2-<framework>-analysis.md` exists in
      `1-model.md`'s Data Flow Table.
- [ ] Every threat in `2-<framework>-analysis.md` appears exactly once in
      `3-findings.md`'s Threat Coverage Verification table.
- [ ] `0-assessment.md`'s counts (Executive Summary, Action Summary) match
      the actual totals in `2-<framework>-analysis.md` and `3-findings.md`.
- [ ] `inventory.yaml`'s components/threats agree with the documents (same
      IDs, same statuses, same tiers).
