# Aegis Release History

This is the shipped-feature changelog and historical design record for Aegis — every completed
roadmap item, why it was built, what it touched, and how it was tested. For what's currently open
or next, see [roadmap.md](roadmap.md).

**This file is now an index.** The full record was split across six part files once it passed 1.5MB —
each is a plain continuation of the same newest-first (Parts 1-3) or chronological (Parts 4-6) record,
just in a smaller file. Follow a link below to the file that has the section you want; every anchor
that used to point into `releases.md` directly now points at the specific part file instead.

---

## Parts, newest first

- **[releases/releases-01.md](releases/releases-01.md)** — `## Latest changes`, newest entries: everything from the
  2026-09-01 P81 batch closures back through **P74.16** (2026-08-21, context-overflow clip-and-retry).
- **[releases/releases-02.md](releases/releases-02.md)** — `## Latest changes` continued: **P74.15** (2026-08-21) back
  through **P66.10**/the template-that-ate-the-tool-calls sitting (2026-08-17).
- **[releases/releases-03.md](releases/releases-03.md)** — `## Latest changes` continued: **P66.9** and the rest of the
  P66 review batch's individual fix narrative, back to the start of that record.
- **[releases/releases-04.md](releases/releases-04.md)** — `## Migrated from roadmap.md` sections: batch origins,
  refutation records, closed-batch status notes (2026-08-06); the P52.x full-stack review batch,
  P51.1, P50.x; and `## P55.x` container-only scanning (through 2026-08-02).
- **[releases/releases-05.md](releases/releases-05.md)** — the P29 docs-vs-implementation drift batch and the
  pre-P55 record between it and the P23 batch below.
- **[releases/releases-06.md](releases/releases-06.md)** — `## Shipped — P23 items` through `## P14`, and
  Appendices A-D (the pre-2026-08 record, oldest material in this history).

## Finding something specific

- **A `releases.md#some-anchor` link from elsewhere in the repo** (roadmap.md, docs/) that hasn't been
  updated yet: grep the six part files above for the anchor's heading text — GitHub-style anchors are
  heading text lowercased with punctuation stripped and spaces turned to hyphens, so the heading is
  easy to spot even without recomputing the slug.
- **A specific `P<n>.<m>` item**: `grep -rn "P<n>\.<m>" research/releases/releases-0*.md` — every part file uses
  the same `### P<n>.<m> — ...` or descriptive-title-plus-`(P<n>.<m>)` heading convention as the
  original single file did.
- **What shipped on a given date**: each part is internally ordered the same way the original section
  was (newest-first for Parts 1-3, the batch's own document order for Parts 4-6) — scan a part's
  headings rather than the whole file.
