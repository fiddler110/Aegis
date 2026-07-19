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

An update run uses the **same phased orchestration** as a fresh run
(SKILL.md §4.2), with the baseline threaded in: the top-level run locates the
baseline (step 1) and reads its `inventory.yaml`, then passes the baseline's
component and threat IDs into phase 1's prompt so those IDs are reused rather
than reinvented (the ID-stability rules above). Phase 3 verifies each
baseline threat against the current code and assigns new threats the next
free IDs; phase 5 adds the `## Changes Since Baseline` section and writes the
`baseline` block and per-threat `change_status`. The terse-identifier
contract between phases is unchanged — the baseline IDs are exactly the kind
of stable identifier that is meant to cross a phase boundary.

## Phased-orchestration governance

SKILL.md §4.2 builds the suite as a sequence of isolated build sub-agents —
one `agent` call with `mode:"sequential"`, one phase per file (or file
group), each running in a fresh context that unloads when it returns. This is
a deliberate change from an earlier design in which only the top-level run
wrote files: peak context per request, not who holds the pen, is what kills a
local-model run, and phasing bounds that peak by never carrying one phase's
reads or writes into the next. The rules that keep distributed writes
consistent are therefore different — and stricter about *what crosses phase
boundaries* — not about *who writes*:

- **Report files are written by the phase that owns them, and only that
  phase.** Phase 1 owns `0.1-architecture.md`, phase 2 the model files, phase
  3 `2-<framework>-analysis.md`, and so on (SKILL.md §4.2's table). A phase
  never writes a file another phase owns; the phase-6 review round is the
  only step that edits across all files, and it only reconciles seams, it
  does not author new content.
- **Only stable identifiers cross a phase boundary — never file content.**
  Each phase's final answer is the *sole* thing threaded into the next
  phase's context, so it must be a terse list of the identifiers the next
  phase needs (component names + anchors, `DF##` ids, threat IDs with their
  category/prerequisite/tier/severity) — never the file's prose or tables,
  which are already on disk. A verbose final answer reintroduces the same
  context bloat one phase down; this is the contract SKILL.md §4.2 marks as
  its single most important detail.
- **Handing the exact prior identifiers forward is what enforces
  consistency.** Because phase N+1 is grounded in phase N's verbatim
  names/ids, no phase can invent a divergent component name or threat id.
  What that grounding cannot catch — a name a later phase still drifts on, a
  `DF##` referenced but never defined, a count that disagrees — is caught by
  the phase-6 review round re-reading the complete suite fresh from disk and
  fixing it in place (SKILL.md §5). That review round is mandatory precisely
  because writes are now distributed across contexts that never saw each
  other.
- **Any *additional* delegation is narrow and read-only.** Delegating extra
  work beyond the six phases (searching for auth-related code across a large
  tree, for instance) is fine, but keep it narrow and read-only —
  "return a table of every function that handles credentials", never
  "analyze this codebase and write the threat model". The same narrowing
  discipline the `debate` step in SKILL.md §5 applies to the arbiter call
  extends to any such helper.

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
