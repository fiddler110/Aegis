---
name: deep-research
description: Use when asked to research a topic in depth on the web — survey the state of something, compare options or tools, gather evidence for a decision, or produce a research report with citations. Triggers on "research X", "deep research", "do some research on", "find sources on", "what's the current state of", "literature review", and the /research command.
---

# Deep Research Skill

The failure mode this skill exists to prevent is unguided tool-looping: a
handful of ad-hoc searches, whichever pages happened to load, and a summary
whose claims can't be traced back to any source. Work in structured rounds
instead, with a findings log and citation discipline from the first search.

## 0. Scope the question before searching

Restate the request as one primary question plus 2–5 sub-questions that
together answer it, and say what a complete answer would contain (a
comparison table? a timeline? a recommendation with trade-offs?). If the
request is too vague to decompose, ask the user to narrow it before spending
searches on guesses.

**Before the first search, `write_file` a working file** at
`.aegis/research/<topic-slug>.md` with the scope above and the section 2
skeleton (an empty findings log, an empty audit trail). This is unconditional
— not "for anything beyond a couple of rounds" — because a run that turns out
short cost nothing extra, while a run that turns out long and never got a
file loses everything the moment compaction fires (P71.9: a live run on a
16k-token local model compacted 25 times across 42 tool calls and the log was
never touched past its round-1 placeholders — the audit trail in the final
report was reconstructed from memory and two of five cited URLs were wrong).
The file is the primary record; the conversation is the cache.

Set budgets up front and hold to them:

- **Round cap: 8.** Most questions resolve in 2–4 rounds; 8 is the hard stop.
- **Source target:** roughly 5–12 quality sources for a typical question.
  More sources is not more rigor — corroborated sources are.
- Anything you can answer from the workspace or your own knowledge, mark as
  uncited background rather than spending rounds re-deriving it from the web —
  but never present uncited background as a sourced finding.

## 1. Work in rounds: plan → search → select → read → record

Each round:

1. **Plan** — name the sub-question or gap this round attacks. If you can't
   name one, you're done researching (see stop conditions).
2. **Search** — 1–3 `web_search` calls with *varied* queries: different
   phrasings, more specific terms, a recency qualifier, or a likely
   authoritative site's name. Re-running a near-identical query is a wasted
   round.
3. **Select** — apply the source-quality bar (section 3) to the result
   titles/URLs/snippets *before* fetching. Skip what fails the bar; record
   the skip in the audit trail.
4. **Read** — `web_fetch` the selected URLs. Output is capped; pass a larger
   `max_chars` when a page is load-bearing and the default window cut it off.
5. **Record** — `edit_file` the working file from section 0, appending this
   round's findings-log entries and audit-trail lines, **before your next
   `web_search` or `web_fetch` call**. Not "when convenient," not "at the end
   of the round" if that means after several more tool calls — the write is
   what protects the round's work from a mid-round compaction. A round is not
   complete until this step has actually landed in the file.

**Stop when any of these holds**, and say which one: every sub-question is
answered with corroborated sources; a full round produced nothing material
(saturation — don't grind out the remaining rounds); the round cap is hit; or
the remaining gaps aren't worth the remaining budget, in which case list them
as open questions instead of stretching thin evidence to cover them.

## 2. The findings log and audit trail

Keep one structured record per source that contributed something:

```
- url:      https://…
  title:    …
  type/date: official docs / paper / vendor advisory / blog …, published YYYY-MM
  summary:  1–3 sentences on what this source contributes
  evidence: a short quote or specific data point, not a vibe
  bearing:  which sub-question it addresses; supports / contradicts what
```

Separately, keep an **audit trail of every URL you examined** — including the
ones you rejected — as `url — kept/rejected — one-phrase reason` lines. The
trail is what lets a reader (or a later session) see what the research
covered and what it deliberately passed over, and it prevents re-fetching the
same dead ends when a topic gets revisited.

Both live in the working file created in section 0, appended every round per
section 1 step 5 — never only in conversation, which compaction can and will
destroy exactly when the log is most needed.

## 3. Source-quality bar

- **Prefer primary and authoritative sources:** official documentation,
  standards, peer-reviewed papers, source repositories and changelogs,
  vendor advisories, named-author reporting from outlets with editorial
  review.
- **Corroborate-only sources** — forums, Q&A sites, personal blogs without
  stated author credentials — can point you at claims and search terms but
  are never citable alone; chase the primary source they got it from.
- **Reject outright:** SEO content farms, AI-generated aggregator pages,
  undated/unattributed listicles, and pages that merely restate another
  source (cite the original instead).
- **Load-bearing claims need two independent sources.** Independent means
  separately produced — ten articles rewriting one press release are one
  source.
- **Note publication dates.** For fast-moving topics prefer recent material
  and flag anything old enough that it may no longer hold; for stable topics
  age is fine but say what you checked.

## 4. Citation discipline

- Number sources and cite inline with `[n]` markers as you write — every
  non-obvious claim carries one. A claim you can't attach a marker to is
  either background knowledge (label it as such) or doesn't belong in the
  report.
- A claim resting on a single source is flagged as single-source in the text,
  not silently presented with the same confidence as corroborated findings.
- When sources contradict each other, surface the contradiction with both
  sides cited and say which you find more credible and why — never silently
  pick one.

## 5. The report

Structure the final output as:

1. **Question & scope** — what was asked, the sub-questions, budgets used
   (rounds run, sources fetched vs. examined).
2. **Answer (TL;DR)** — the direct answer up front, a short paragraph.
3. **Findings** — per sub-question, with inline `[n]` citations.
4. **Contradictions & open questions** — what the sources disagree on, and
   what the budget didn't cover.
5. **Sources** — the numbered list: `n. title — url (type, published date)`.
6. **Audit trail** — as an appendix: the examined-URLs list, or for long runs
   a summary count plus a pointer to the working file.

Deliver the report as markdown in the conversation by default. If the user
wants a shareable artifact, the `html-report` or `latex-report` skill (the
`/report` command) can consolidate it into a standalone page or PDF.
