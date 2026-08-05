---
name: document-codebase
description: Use when writing or updating documentation that lives in the repository — a README, ARCHITECTURE or design doc, module/package overview, API reference, onboarding guide, CONTRIBUTING, or runbook kept as a repo file. Covers both authoring a doc that does not exist and revising one that does. Triggers on "document this codebase", "write a README", "update the docs", "document this package/module/API", "write an architecture doc", "onboarding guide", "explain how this works in a doc".
---

# Document Codebase Skill

This skill is for documentation that **lives in the repo and is maintained
alongside the code**. It is not the same job as producing a report:

- A **report** is a deliverable about a moment in time, written once for a
  reader who is not in the repo. Use `html-report` or `latex-report`.
- A **branded organizational document** built through a Documentation-as-Code
  toolchain — use `documentation-as-code`.
- **This skill** produces a file a maintainer will edit next month, next to
  the code it describes.

That difference drives everything below. A report can be verbose and
self-contained because nothing will contradict it later. Repo documentation
is judged by whether it is still true in six months.

## 0. The failure modes this skill exists to prevent

Documentation generated from a codebase fails in four predictable ways.
Every rule below traces back to one of them:

1. **Restating the code.** "The `Run` function runs the loop." A reader who
   wanted that could have read the signature. Documentation earns its place
   by carrying what the code *cannot* say: why this design, what was rejected,
   what breaks if you change it, what the invariants are.
2. **Documenting what you inferred, not what you read.** A plausible,
   confidently-worded description of behavior the code does not have is worse
   than no documentation, because it is trusted and it is wrong.
3. **Writing docs that are stale on arrival.** Duplicating exact signatures,
   field lists, flag tables, and line numbers into prose guarantees drift —
   the code changes, the doc does not, and the doc becomes a liability.
4. **Bulldozing existing documentation.** Rewriting a human-authored file
   wholesale to "improve" it destroys hard-won context — the caveat someone
   added after an outage, the deliberately terse section, the house voice.

## 1. Settle the audience and the document type first

Ask what the document is *for* if the request does not make it obvious. These
are different documents and they do not merge well:

| Type | Reader | Answers |
|---|---|---|
| `README.md` | Someone who just arrived | What is this, why would I use it, how do I run it |
| `ARCHITECTURE.md` / design doc | A maintainer changing it | How is it structured, why this way, what are the seams and invariants |
| Package/module doc | Someone working in that subtree | What lives here, what is the entry point, how does it relate to neighbours |
| API reference | A caller | What can I call, with what, what comes back, what errors |
| Onboarding / how-to guide | A new contributor | How do I set up, make a change, test it, ship it |
| `CONTRIBUTING.md` | An outside contributor | What is expected of a change, conventions, review process |

If the ask is "document this codebase" with no further shape, propose a
specific set (usually a README plus one architecture doc) and confirm before
writing. Producing six files nobody asked for is its own failure.

**Check what already exists before writing anything.** A repo with a README,
a `docs/` tree, and an `AGENTS.md`/`CLAUDE.md` has conventions — placement,
heading depth, voice, how much detail. Match them rather than importing a
generic template. If a doc on this topic already exists, you are in §5
(updating), not §3 (authoring).

## 2. Ground every claim in the code

Read before you write. Not a sample — the actual entry points, the actual
types, the actual call paths you intend to describe. `repo_map`, `glob`, and
`grep` find the surface; `read_file` is what earns the right to describe it.

- **Never describe behavior you have not read.** If you are inferring from a
  name, either go read it or do not write the sentence.
- **Run the commands you document.** A build, test, or install command that
  appears in a README will be copy-pasted by the next reader. If you cannot
  run it (no toolchain, needs credentials, would mutate something), say so in
  your report to the user and mark it as unverified rather than presenting it
  with the same confidence as a command you ran.
- **Cite while drafting, prune before delivering.** Track `file:line` for each
  non-obvious claim as you work. Keep pointers to *stable* anchors in the
  final text — package and file paths, exported symbol names, directory
  structure. Strip line numbers: they are correct for exactly one commit.
- **Say when you are unsure.** "Ownership of X is unclear from the code" is
  useful and honest. A confident fabrication is not.

## 3. Write it to survive

- **Explain the why, link to the what.** Prose carries rationale, trade-offs,
  invariants, and the shape of the system. Point at the code for exhaustive
  detail instead of copying it. The test: if this paragraph would need editing
  after a pure rename, it is carrying detail that belongs in the code.
- **Prefer generated or checked detail over hand-copied detail.** If a flag
  table or an API surface must appear verbatim, note where it came from so
  the next person can regenerate it.
- **Lead with orientation.** The first screen answers what this is and why it
  exists. Do not open with installation instructions for a thing the reader
  cannot yet identify.
- **Keep the register plain.** No marketing voice, no "simply"/"just"/
  "obviously" — they only ever tell a stuck reader that they should not be
  stuck. Short sentences. Concrete nouns.
- **Show the real thing.** Examples come from the actual API with real names,
  not `foo`/`bar` placeholders. An example that would not compile is a bug
  report waiting to happen.
- **Diagram when structure is the point.** A component or flow diagram earns
  its place where prose would need three paragraphs of "A calls B which calls
  C". Use the `architecture-diagram` skill for the notation choice, and keep
  the source (Mermaid) in the doc so it stays editable.

## 4. Write incrementally, one section per edit

Draft the outline first and get agreement on it if the document is
substantial. Then fill it **one section per `edit_file` call**.

This is not a style preference. A whole-file write of a long document is slow,
loses the structure you just agreed, and — on a local model — risks
truncating mid-call into a malformed tool call that costs several turns to
recover. Incremental section fills also keep the document readable at every
intermediate point, so an interrupted run leaves something usable.

## 5. Updating an existing document

Default to **surgical edits, not replacement.**

- Read the whole document first. Understand why it says what it says.
- Change what is wrong or missing. Leave correct prose alone even when you
  would have phrased it differently — a diff full of cosmetic rewording hides
  the substantive change and burns reviewer attention.
- Preserve deliberate content: caveats, warnings, historical notes, "do not
  do X because Y". If something looks redundant, it is more likely load-
  bearing than accidental — ask rather than delete.
- Match the existing voice and structure, including heading style and depth.
- When the document has genuinely diverged from the code, say so explicitly
  in your report rather than quietly correcting it — the drift itself is
  information the maintainer wants.

Wholesale rewrites are justified when the user asks for one, or when the
document is so stale it is actively misleading. Say which applies and why.

## 6. Verify before delivering

Check your own output:

- Every command in the doc: run it, or flag it as unverified.
- Every path, package, and symbol name: confirm it exists and is spelled
  right. A broken path in a README is the most common and most annoying defect.
- Every internal link and anchor: confirm the target exists.
- Every code example: confirm it matches the current API.
- Re-read for §0's failure modes — particularly restatement. If a paragraph
  says nothing the signature does not, cut it.

## 7. Report

State plainly:

- which files you wrote or changed, and where
- what you verified by running versus what you could only read
- anything you could not confirm, or that looked wrong in the code but was
  out of scope to fix
- for an update: what you deliberately left alone

Do not paste the finished document back into chat — the file is the
deliverable. Summarize its structure and the decisions you made instead.
