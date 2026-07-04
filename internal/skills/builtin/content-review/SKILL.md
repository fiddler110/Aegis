---
name: content-review
description: Use when asked to review, audit, or critique existing content — code, a diff/PR, a config, or technical documentation — for correctness and quality. Triggers on "review this", "audit this", "comprehensive review", "code review", "review the docs", "critique this", "check this over". Always summarize findings in chat first; only produce a written report document if the user asks for one.
---

# Content Review Skill

A comprehensive review is not a re-read of the target in isolation — it's
checking the target against ground truth (the code it claims to describe,
the tests that should cover it, the conventions the rest of the project
follows) and reporting only what survives that check. This skill covers two
review kinds — **code** and **technical documentation** — plus what to do
with the result.

Read `references/severity-rubric.md` before starting; it defines the four
severity levels used throughout and referenced in the manifest below.

## 1. Scope the review

Before reading anything closely, establish:

- **What is the target?** A specific file, a diff/PR, a whole directory, a
  doc page, or "review my recent changes" (in which case diff against the
  base branch or last commit rather than guessing).
- **Code, docs, or both?** If a change touches both (e.g. a PR that edits a
  function and its doc comment), review them together — a docs finding
  that just restates a code finding is noise; the interesting bugs are
  where they *disagree*.
- **What's out of scope?** If the user names a subsystem or a specific
  concern ("just check the error handling"), don't broaden the review
  yourself, but do mention in the summary if something clearly severe
  outside that scope caught your eye.

## 2. Gather ground truth before judging

Don't review the target against itself. Use your file/search/shell tools to
pull in whatever the target makes claims about or depends on:

- **For code:** the functions/types it calls into, its existing tests (do
  they cover the changed behavior?), how similar code elsewhere in the
  project handles the same problem, and recent git history/blame if intent
  is unclear.
- **For docs:** the actual code, config, or CLI behavior each claim
  describes. A doc claim is a testable assertion — verify it by reading the
  referenced file/flag/function, not by re-reading the doc more carefully.
  Flag anything the doc asserts that the code no longer does (renamed
  function, removed flag, changed default).

Load `references/code-review-checklist.md` and/or
`references/doc-review-checklist.md` depending on scope, and use them as a
checklist of *categories to check*, not a script to read aloud — skip
categories that don't apply rather than forcing a finding into every one.

## 3. Vary your method, don't just re-verify a list

If this project has been reviewed before (check for prior review docs,
roadmap "shipped" markers, or CHANGELOG entries), a checklist re-pass over
the same known-risk areas will mostly reconfirm what's already fixed. The
bugs still standing after N prior reviews tend to be interaction bugs
between two individually-correct pieces, or a fix that was applied in one
place but not a sibling that needed the same treatment. Deliberately look
for both:

- **Seams**: where two features that are each fine on their own combine
  badly (a cache plus a mutation path, a retry plus a non-idempotent
  action, a new field that doesn't get the same trust/validation gate a
  sibling field already has).
- **Unstated assumptions**: comments or docs that say "already handled" or
  "can't happen" — verify rather than trust.

## 4. Report the summary in chat — always

Regardless of what the user does next, respond in the conversation with a
concise, ranked summary. Do not skip this step or wait to be asked. Format:

- One-sentence overall verdict (e.g. "solid, two medium-severity gaps" vs.
  "not ready — one critical issue").
- Findings grouped by severity, most severe first. Per finding: one-line
  summary, `file:line` (or doc section) location, and — only when it's not
  obvious from the summary — a one-line concrete failure scenario ("input X
  causes Y") or, for docs, the specific claim vs. actual behavior mismatch.
- Skip categories/dimensions with nothing worth reporting; don't pad the
  summary with "no issues found in X" for every checklist category.
- If you had to guess at scope or couldn't verify a claim (e.g. no way to
  run the code), say so rather than presenting a guess as a finding.

Keep this tight — a reader should be able to act on it without opening a
document.

## 5. Only produce a document when asked

Do not create a report file unless the user's message requests one (e.g.
"write this up", "make it a report", "can you document these findings",
"give me something I can share"). When they do:

1. If a skill named `html-report` is available, load it and use its
   `template.html` + severity classes — they already map 1:1 to this
   skill's `critical`/`high`/`medium`/`low` rubric, so no translation is
   needed. Validate the result with its `validate_report.py` before
   telling the user it's done.
2. If `html-report` isn't available, write a well-structured Markdown file
   instead: title, one-paragraph scope/method note, findings grouped by
   severity with the same fields as the chat summary, and (for a code
   review of a diff/PR) a short "what's left before this is mergeable"
   punch list at the end.
3. Save it somewhere sensible for the project (ask if genuinely unclear
   where) and tell the user the path.

Never generate the document speculatively "in case they want it" — the
chat summary is the deliverable by default.
