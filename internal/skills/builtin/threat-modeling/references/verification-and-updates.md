# Verification, Inventory, and Updates

Framework-agnostic finishing steps: the inventory sidecar that makes a
model re-runnable, the workflow for updating an existing model, and the
final self-check. All three apply whichever framework was used.

## Inventory sidecar

Alongside the finished document in `.aegis/security/threat-model/`, write
`<document-stem>.inventory.yaml` — a compact, machine-readable index of
the model. It exists for *matching*
(a future run diffing itself against this one), not for reading, so keep
every entry to one line:

```yaml
metadata:
  framework: stride            # the framework actually used
  date: 2026-07-08
  commit: 825bb98              # short SHA if in a git repo, else omit
  document: stride-2026-07-08.md   # sibling file in .aegis/security/threat-model/
  deployment_classification: localhost-service
components:
  - id: SessionStore           # PascalCase, named after its anchor
    anchor: internal/session/store.go
threats:
  - id: T1
    component: SessionStore
    category: tampering        # framework-specific category, lowercase
    severity: medium
    prerequisite: local-process-access
    status: open               # open | mitigated
```

Rules that make the diff work:

- **IDs come from anchors.** A component's `id` derives from its real
  class/file/manifest name — two runs on the same code must produce the
  same IDs. Never rename a component between runs; if a prior inventory
  exists, reuse its IDs for components that still exist.
- **Every component has an `anchor`** — the artifact from SKILL.md §2's
  anchor rule. No anchor, no entry.
- **Threat IDs are stable within a document lineage.** When updating, a
  still-present threat keeps its baseline ID; new threats take the next
  free numbers. Never reuse a resolved threat's ID for a new threat.

## Updating an existing model

When the ask is "update/refresh the threat model" or "what changed since
last time":

1. **Locate the baseline** — the document/inventory the user named, or
   the most recent inventory sidecar in `.aegis/security/threat-model/`
   (matching the scope slug, if the ask is scoped to one feature). If
   none exists, say so and fall back to a fresh full analysis.
2. **Keep the baseline's framework** unless the user explicitly asks for
   a different one (a framework switch is a new model, not an update —
   say that if it comes up).
3. **Verify each baseline threat against the current code.** Do not trust
   the old document's prose — re-check each threat's evidence at its
   anchor. Classify each as **still-present** (evidence still holds),
   **resolved** (the mitigation landed or the code path is gone — cite
   what changed), or **changed** (still real but severity/prerequisite/
   mitigation needs revision).
4. **Discover what's new.** Re-run the SKILL.md §2 exploration for
   components, entry points, and flows the baseline doesn't cover. If the
   baseline recorded a commit, `git diff --stat <commit>` and
   `git log <commit>..` are the cheap way to focus this pass.
5. **Produce a standalone updated document** — complete on its own, not a
   patch — with a `## Changes since <date/commit>` section up front:
   counts and per-item lists of new / resolved / still-present / changed
   threats. Write a fresh inventory sidecar per the ID-stability rules
   above.

## Final self-check

Run this before reporting; fix failures rather than noting them:

- [ ] Every component/element in the diagram has coverage in the analysis
      — every applicable category populated, or an explicit
      "none identified — <why>" entry, never a silently missing cell.
- [ ] Every open threat has a proposed mitigation or is flagged as an
      open finding — no threat dropped between enumeration and the final
      table.
- [ ] No threat's prerequisite sits below the deployment classification's
      floor, and each severity is consistent with its prerequisite (a
      threat requiring host compromise is rarely critical).
- [ ] Every component cites its anchor artifact; anything unanchorable
      was deleted, not kept.
- [ ] Every threat cites evidence (file/config/code path) per SKILL.md
      §3 — unevidenced suspicions live in a "needs verification" note.
- [ ] No "accepted risk" written on the model's own authority.
- [ ] Summary counts match the actual table rows.
- [ ] The inventory sidecar exists and agrees with the document
      (same components, same threat IDs and statuses).
