---
description: Get a structured, one-shot report of repo state (uncommitted work, recent commits) plus every open item in research/roadmap.md with priority/effort and a suggested next item. Use whenever the user says "continue the roadmap", "keep working through roadmap items", "what's next", or similar — instead of separately running git status/diff/log and manually grepping/reading roadmap.md and releases.md.
---

# Roadmap Status Skill

Run this single command instead of the ad-hoc sequence of `git status`, `git diff --stat`,
`git log`, and manually grepping/reading `research/roadmap.md` / `research/releases.md`:

```bash
bash scripts/roadmap-status.sh
```

It prints, in one shot:

1. **Repo state** — `git status --short`, a diffstat if anything is uncommitted, last 5 commits.
2. **Roadmap Status summary** — the `## Status` section of `research/roadmap.md` verbatim.
3. **Every open `### P<n>.<m>` item** — its Priority/Effort line, flagged
   `[NOT BLOCKING -- confirm with user before starting]` when the roadmap marks it
   speculative / no-concrete-trigger / not-blocking (checked both within the item's own section
   and against shared closing notes that cover multiple items, e.g. "Neither P6.1 nor P6.5 is
   blocking...").
4. **Suggested next item** — the first non-flagged item in document order (roadmap.md already
   lists items in priority order within each track).

## Workflow

1. Run the script.
2. If there's uncommitted work, don't just build on top of it blind — check what it is (`git
   diff`), confirm it's complete (build/test pass), and ask the user whether to commit it before
   starting something new. It may be a finished-but-uncommitted prior task, not a
   work-in-progress.
3. Take the suggested next item unless the user directs otherwise.
4. If the only remaining items are all `[NOT BLOCKING]`, do not start one speculatively — ask the
   user which one they want, per roadmap.md's own instruction.
5. After shipping an item: **delete** it from `research/roadmap.md` and write the entry in
   `research/releases.md` (with rationale), matching this repo's existing convention — check recent
   commits/releases.md entries for the expected shape before writing the new one. Roadmap.md holds
   only open work; leaving a shipped write-up there is the drift the 2026-08-01 and 2026-08-06
   cleanups both had to undo. A line or two of *forward-looking* residue is fine and belongs in the
   tier header (what the fix corrected about the filed item, what it unblocked); the write-up itself
   does not.

If `research/roadmap.md`'s structure ever changes (new heading levels, a track that isn't titled
`## Open Work — ...`), the script's parsing may silently miss items — spot-check its output
against a fresh read of the file rather than trusting it blindly the first time it's used after a
roadmap restructure.
