# Skeleton: `<document-stem>.inventory.yaml`

> **⛔ Use EXACT field names shown below.** Common drift to avoid:
> `name` (wrong → `id`), `type` (wrong → `category`), `severity` (wrong →
> `status`, which is a different field — see below). The file is YAML, not
> JSON — no trailing commas, no code fence in the actual output file (the
> fence below is for readability of this skeleton only).
>
> Written alongside the finished document in
> `.aegis/security/threat-model/`, once per completed model (§6 of
> SKILL.md and `references/verification-and-updates.md`). It exists for
> *matching* — a future run diffing itself against this one — not for
> human reading, so keep every entry to one line.

---

```yaml
metadata:
  framework: [FILL: stride / linddun / pasta / trike / vast / nist-800-154]
  target: [FILL: the mandatory target slug from the document's filename]
  date: [FILL: YYYY-MM-DD]
  commit: [FILL: short SHA if in a git repo, else omit this key entirely]
  document: [FILL: the sibling document's exact filename, e.g. stride-aegis-2026-07-08-1432.md]
  deployment_classification: [FILL: internet-facing / internal-network / localhost-service / local-desktop]

components:
  [REPEAT: sorted by id — one entry per component in the document]
  - id: [FILL: PascalCase, named after its anchor — e.g. SessionStore]
    anchor: [FILL: the real file/class/manifest this component is named after]

threats:
  [REPEAT: sorted by id — one entry per threat/finding row in the document, whatever the framework's own ID prefix is (T##/P##/R##/V##/AV##)]
  - id: [FILL: the exact ID used in the document's table, e.g. T1, P3, R2, V4, AV5]
    component: [FILL: component id this threat belongs to]
    category: [FILL: framework-specific category, lowercase — e.g. tampering, linkability, denial-of-service]
    severity: [FILL: critical / high / medium / low — omit for frameworks that use Risk/Priority instead of Severity (Trike, PASTA); use that value here instead]
    prerequisite: [FILL: none / authenticated-user / internal-network / local-process / host-compromise]
    status: [FILL: open / mitigated]
```

**MANDATORY field name compliance:**
- `target` — not `scope`, not `name` — matches the mandatory filename slug
  from SKILL.md §4.
- `id` (under `components` and `threats`) — not `name`, not `component_id`.
- `anchor` — every component has one; no anchor, no entry (SKILL.md §2.3).
- `category` — lowercase, framework-specific vocabulary — not `type`.
- `status` — exactly `open` or `mitigated` — never `accepted` (SKILL.md
  §4's cross-framework rule: risk acceptance is never the model's own
  call, so there is no `accepted` status this sidecar can carry).
- Sort both `components` and `threats` by `id` before writing — an
  unsorted list makes future diffs noisier than they need to be.

<!-- ⛔ POST-WRITE CHECK:
  1. Every `id` under `threats` also appears in the finished document's
     table, and vice versa — no orphaned sidecar entry, no document row
     missing from the sidecar.
  2. Every `component.id` referenced by a threat's `component` field
     exists in the `components` list.
  3. `metadata.document` is the exact filename of the sibling `.md` file
     in the same directory — a typo here breaks the "locate the baseline"
     step of a future update run.
  If any check fails, fix the sidecar now — it is part of the deliverable,
  not an optional extra (SKILL.md §7). -->
