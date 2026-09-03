---
name: deep-research
description: Use when asked to research a topic in depth on the web — survey the state of something, compare options or tools, gather evidence for a decision, or produce a research report with citations. Triggers on "research X", "deep research", "do some research on", "find sources on", "what's the current state of", "literature review", and the /research command.
run_dir: .aegis/research/*
phases:
  - name: research
    setup: true
    files: ["findings.md"]
    require_pattern: 'url:\s*https?://'
    require_count: 1
    require_hint: "at least one findings-log entry with a real `url: https://…` line — a scope/audit-trail-only file with no source recorded is not research"
    prompt: |
      Research task: {task}

      This is a fresh context for one research round — the findings file on
      disk is the memory the previous round left you, not this conversation.
      Read {skill_dir}/SKILL.md sections 0-4 if you have not already: they
      define the findings-log format, the source-quality bar and citation
      discipline this file has to follow.

      If `{run_dir}/findings.md` does not exist yet, this is round 1: pick a
      short topic slug and treat `.aegis/research/<slug>/` as {run_dir} for
      every round from now on (it does not exist until you create it).
      Restate the task as one primary question plus 2-5 sub-questions, then
      write {run_dir}/findings.md with that scope, an empty findings log, an
      empty audit trail, and a `<!-- PENDING: findings -->` line at the end
      of the findings section.

      If it already exists, read it, then run exactly one more round: plan
      the sub-question or gap this round attacks, search (1-3 web_search
      calls with varied phrasings), select against the source-quality bar,
      read the selected URLs (web_fetch), and edit_file to append this
      round's findings-log entries and audit-trail lines. {budget} — sized to
      this run's actual context window, not the fixed numbers SKILL.md
      section 0 describes for a reader opening the file directly. Count the
      audit trail's rounds against the cap stated here before starting
      another.

      Remove the `<!-- PENDING: findings -->` line (do not replace it with
      anything) only when one of the stop conditions holds: every
      sub-question is answered with corroborated sources; a full round
      produced nothing material; the round cap is hit; or the remaining gaps
      are not worth the remaining budget. In the last two cases, record the
      open questions in the file first. Do not remove the marker in the same
      round you create the file — round 1 scopes and searches, it does not
      finish the research.
  - name: synthesize
    files: ["report.md"]
    require_pattern: '(?m)^\d+\.\s.*https?://'
    require_count: 1
    require_hint: "at least one numbered Sources line carrying a real https:// URL, in the `n. title — url (type, published date)` shape section 5 specifies — a Sources list that names publications by title alone (\"1. Postman Blog: ...\") without the url is not verifiable and does not satisfy this phase"
    prompt: |
      Write the final research report for: {task}

      If `{run_dir}/report.md` does not exist yet, first write it with just
      one line, `<!-- PENDING: report -->`, so an interrupted report is never
      mistaken for a finished one — then keep going in the same turn if you
      can.

      Read {run_dir}/findings.md in full — every source, the audit trail,
      and any open questions the research phase recorded. Then write
      {run_dir}/report.md following {skill_dir}/SKILL.md section 5's
      structure (question & scope, TL;DR, findings with inline `[n]`
      citations, contradictions & open questions, numbered sources, an
      audit-trail summary), citing only what findings.md actually supports —
      an unsupported claim goes under open questions, not into a citation.
      One additional web_search/web_fetch is fine if a single claim
      genuinely needs a source no round covered, but this phase's job is to
      write the report, not run another research round.

      Remove the `<!-- PENDING: report -->` line only once the report is
      actually complete — a partial report with the marker still removed
      would be indistinguishable from a finished one.
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

Set budgets up front and hold to them. **The numbers below are the cloud-scale
defaults** (roughly what a ~128k-context run gets); every research-round
prompt states this run's *actual* round cap and source target, computed from
the model's resolved context window (P71.11) — a small local model gets a
proportionally smaller budget it can actually complete, rather than planning
against numbers it has no room to execute. Follow the number in the prompt,
not the one below, whenever they differ:

- **Round cap: 8** (cloud-scale default; a 16k window gets 4). Most questions
  resolve in 2–4 rounds regardless of the cap.
- **Source target:** roughly 5–12 quality sources for a typical question at
  cloud scale (3–4 at a 16k window). More sources is not more rigor —
  corroborated sources are.
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
   titles/URLs/snippets *before* fetching — and to each result's `published:`
   date when the backend supplied one. Skip what fails the bar; record the
   skip in the audit trail.
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
- **Note publication dates when the search result carries one.** `web_search`
  results include a `published: <date>` line only when the active backend
  supplies one — Brave (`search.provider: brave`) and, when the underlying
  engine reports it, a configured SearXNG instance. The zero-config
  DuckDuckGo/Marginalia fallback carries no such signal; for those, a fetched
  page's own dateline (visible after `web_fetch`) is the earliest point a date
  is available. For fast-moving topics prefer recent material and flag
  anything old enough that it may no longer hold; for stable topics age is
  fine but say what you checked.

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
