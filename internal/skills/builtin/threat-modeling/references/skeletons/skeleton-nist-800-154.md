# Skeleton: NIST SP 800-154 threat model document

> Copy the structure below **verbatim** as the initial skeleton
> (`write_file`, §4.1 of SKILL.md): every heading, in this order (one per
> step, plus Summary), each body replaced with `<!-- PENDING -->`. Then,
> step by step (§4.2), replace one `<!-- PENDING -->` at a time with the
> filled content shown beneath that step's heading here. Step 3 depends on
> Step 2's rows existing, and Step 4 depends on both — do not fill out of
> order.
>
> **⛔ Fixed values — do not invent alternatives:**
> - State (Step 2): exactly one of `at rest`, `in transit`, `in use`.
> - Verified implemented? (Step 3): exactly `yes` or `no` — never
>   "partially" or "should be" (a partial control is `no` plus a note in
>   the Control cell explaining the gap).
> - Attack vector ID prefix: `AV` (`AV1`, `AV2`, …).

---

## Skeleton (initial, before any analysis)

```markdown
# NIST 800-154 Data-Centric Threat Model — [FILL: dataset name]

> Framework: NIST SP 800-154 — [FILL: why chosen, or "default — no stronger signal"]
> Deployment classification: [FILL: one of internet-facing/internal-network/localhost-service/local-desktop]
> Date: [FILL: YYYY-MM-DD]

## Step 1 — System and data characterization

<!-- PENDING -->

## Step 2 — Attack vectors per location

<!-- PENDING -->

## Step 3 — Controls per attack vector

<!-- PENDING -->

## Step 4 — Gap analysis

<!-- PENDING -->

## Summary

<!-- PENDING -->
```

## Fill-in shape per section

### Step 1 — System and data characterization

Prose: name the specific sensitive dataset in scope (not "all data" —
the actual regulated/sensitive type this assessment is about), then list
every location it exists at-rest, in-transit, and in-use, each grounded in
the real code/config/schema. This location list is what Step 2's table
enumerates against — an attack vector in Step 2 with no matching location
here is out of scope creep; add the location here first.

### Step 2 — Attack vectors per location

```markdown
| ID | Location | State | Attack vector |
|----|----------|-------|-----------------|
[REPEAT: one row per (location, state, plausible attack vector) from Step 1]
| [FILL: AV##] | [FILL: location from Step 1] | [FILL: at rest / in transit / in use] | [FILL: plausible vector for this specific state — see the per-state list in references/nist-800-154.md] |
[END-REPEAT]
```

**⛔ Columns are exactly these four, in this order.** Not every attack
vector applies everywhere the data exists — select the ones plausible for
each specific `(location, state)` pair rather than repeating a fixed list
at every row.

<!-- ⛔ POST-TABLE CHECK: every location named in Step 1 appears at least
once in this table (a location with zero rows means no attack vector was
even considered for it — that's a coverage gap, not a clean bill of
health). -->

### Step 3 — Controls per attack vector

```markdown
| Attack vector (ID) | Control | Verified implemented? |
|----------------------|---------|--------------------------|
[REPEAT: one row per attack vector from Step 2]
| [FILL: AV##] | [FILL: encryption/access control/DLP/tokenization/audit logging/contractual limit — the specific control] | [FILL: yes or no — cite the code/config checked] |
[END-REPEAT]
```

**⛔ `Verified implemented?` is `yes` only when the control was actually
checked against real code/config** (not assumed present because it
"should" be there) — cite what was checked in the same cell or the row
directly above it.

<!-- ⛔ POST-TABLE CHECK: every `AV##` from Step 2 appears exactly once in
this table — a vector with no corresponding control row here is silently
dropped, not "obviously fine". -->

### Step 4 — Gap analysis

Every `(location, attack vector)` pair from Step 2 whose Step 3 row says
`Verified implemented?: no` is an open finding — list them explicitly here
(don't just leave them implied by the table above). Note what change
(new data location, new third-party integration) would require
re-running this analysis.

```markdown
| Attack vector (ID) | Location | Gap | Severity |
|----------------------|----------|-----|----------|
[REPEAT: one row per Step-3 "no" — omit this table entirely and write "No open gaps — every attack vector has a verified control." if none exist]
| [FILL: AV##] | [FILL] | [FILL: what's missing] | [FILL: Critical/High/Medium/Low] |
[END-REPEAT]
```

<!-- ⛔ POST-TABLE CHECK: the count of rows here equals the count of `no`
values in Step 3 — every unverified control surfaces as a gap, none
silently dropped. -->

### Summary

Count of locations covered, controls verified vs. missing, and what
change would trigger re-analysis (per Step 4's note).

<!-- ⛔ POST-SECTION CHECK: the Summary's "controls missing" count matches
the Gap analysis table's row count. -->
