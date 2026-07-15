# Skeleton: `inventory.yaml`

> **⛔ Use EXACT field names shown below.** Common drift to avoid: `name`
> (wrong → `id`), `type` (wrong → `category`), `severity` (wrong → `status`,
> a different field — see below). The file is YAML, not JSON — no trailing
> commas, no code fence in the actual output file (the fence below is for
> readability of this skeleton only).
>
> Written once per completed run, directly in the run's output directory
> (alongside the other six files — SKILL.md §4, `output-formats.md`). It
> exists for *matching* — a future run diffing itself against this one, or
> an incremental update locating its baseline — not for human reading, so
> keep every entry to one line.

---

```yaml
metadata:
  framework: [FILL: stride / linddun / pasta / trike / vast / nist-800-154]
  target: [FILL: the mandatory target slug from the directory name]
  date: [FILL: YYYY-MM-DD]
  commit: [FILL: short SHA if in a git repo, else omit this key entirely]
  directory: [FILL: the run's directory name, e.g. stride-aegis-2026-07-08-1432]
  deployment_classification: [FILL: internet-facing / internal-network / localhost-service / local-desktop]

components:
  [REPEAT: sorted by id — one entry per component in 0.1-architecture.md's Key Components table]
  - id: [FILL: PascalCase, named after its anchor — e.g. SessionStore]
    anchor: [FILL: the real file/class/manifest this component is named after]
    type: [FILL: process / external_interactor / data_store]

flows:
  [REPEAT: sorted by id — one entry per row in 1-model.md's Data Flow Table]
  - id: [FILL: DF01, DF02, ... matching 1-model.md exactly]
    from: [FILL: component id]
    to: [FILL: component id]

threats:
  [REPEAT: sorted by id — one entry per threat/finding row in 2-<framework>-analysis.md, whatever the framework's own ID prefix is (T##/L##/P##/R##/V##/N##)]
  - id: [FILL: the exact ID used in the document's table, e.g. T1, L3, P2, R4, V5, N1]
    component: [FILL: component id this threat belongs to]
    category: [FILL: framework-specific category, lowercase — e.g. tampering, linkability, denial-of-service]
    tier: [FILL: 1 / 2 / 3 — derived from prerequisite, see output-formats.md]
    prerequisite: [FILL: none / authenticated-user / internal-network / local-process / host-compromise]
    severity: [FILL: critical / high / medium / low]
    cwe: [FILL: e.g. CWE-306, or omit this key entirely if genuinely not applicable]
    owasp: [FILL: e.g. A07:2025, or P2:2021 for LINDDUN, or omit if genuinely not applicable]
    status: [FILL: open / mitigated / mitigated-by-platform]
    finding: [FILL: the FIND-## id in 3-findings.md this threat maps to, or omit if status is mitigated-by-platform]
```

**MANDATORY field name compliance:**
- `target` — not `scope`, not `name` — matches the mandatory slug from the
  directory name (`output-formats.md`).
- `id` (under `components`, `flows`, and `threats`) — not `name`, not
  `component_id`.
- `anchor` — every component has one; no anchor, no entry (SKILL.md §2.3).
- `category` — lowercase, framework-specific vocabulary — not `type` (which
  on a component means process/external_interactor/data_store, a different
  axis entirely).
- `tier` — an integer `1`/`2`/`3`, always derivable from `prerequisite` —
  never write a tier that disagrees with the prerequisite→tier mapping.
- `status` — exactly `open` / `mitigated` / `mitigated-by-platform` — never
  `accepted` (SKILL.md's cross-framework rule: risk acceptance is never the
  model's own call, so there is no `accepted` status this sidecar carries;
  Trike's owner-attributed accept decision is recorded in
  `2-trike-analysis.md` itself, not encoded as a sidecar status value).
- Sort `components`, `flows`, and `threats` by `id` before writing — an
  unsorted list makes future diffs noisier than they need to be.

## Incremental-update extensions

When this run is an update against a baseline (SKILL.md §6 / this file's
"Updating an existing model" section), add to each `threats` entry:

```yaml
    change_status: [FILL: still-present / resolved / changed / new]
```

And at the top level:

```yaml
baseline:
  directory: [FILL: the baseline run's directory name]
  date: [FILL: the baseline's date]
  commit: [FILL: the baseline's commit, if known]
```

<!-- ⛔ POST-WRITE CHECK:
  1. Every `id` under `threats` also appears in 2-<framework>-analysis.md's
     table, and vice versa — no orphaned sidecar entry, no document row
     missing from the sidecar.
  2. Every `component` field under `threats` and `from`/`to` field under
     `flows` references an id that exists in `components`.
  3. Every `finding` field under `threats` references an id that exists in
     3-findings.md.
  4. `metadata.directory` is the exact directory name of this run — a typo
     here breaks the "locate the baseline" step of a future update run.
  5. If this is an incremental run, every threat has a `change_status` and
     `baseline` is populated.
  If any check fails, fix the sidecar now — it is part of the deliverable,
  not an optional extra (SKILL.md §7). -->
