# Skills

A skill is a reusable, on-demand playbook: a markdown file of step-by-step instructions the agent can load mid-conversation when a task matches it. Skills use **progressive disclosure** — at session start only a one-line `name — description` is injected into the system prompt for each skill; the full body loads only when the model calls the `skill` tool with that name. This keeps dormant skills nearly free (a few tokens each) no matter how many you author.

---

## Minimal skill

A skill is just a markdown file with YAML frontmatter:

```markdown
<!-- .aegis/skills/deploy-staging.md -->
---
description: Deploy the application to the staging environment. Use when asked to "deploy", "push to staging", or "ship this".
---

1. Run `go build -o bin/app ./cmd/app`
2. Copy the binary to staging: `scp bin/app deploy@staging:/opt/app/`
3. Restart the service: `ssh deploy@staging 'sudo systemctl restart app'`
4. Tail logs to confirm it came up clean: `ssh deploy@staging 'journalctl -u app -f --lines=50'`
```

Drop it in `.aegis/skills/` (project) or `~/.aegis/skills/` (user, global) and it's live immediately — no restart. The **filename stem** (`deploy-staging`) is the skill's name unless overridden by a frontmatter `name:`.

Write the `description` the way you'd brief someone deciding whether to open the file: what the skill does, and the phrases/situations that should trigger it. The model sees only this line before deciding to load the skill, so vague descriptions ("deployment stuff") get skipped; specific ones ("Use when asked to deploy, push to staging, or ship this") get matched.

**Frontmatter fields** (anything else is ignored):

| Field | Required | Description |
|-------|----------|--------------|
| `description` | Recommended | Shown in `<skills_available>`; drives progressive disclosure. Omit it and the skill is eager-injected in full on every turn instead (legacy behavior — avoid for anything non-trivial). |
| `name` | No | Overrides the filename/directory stem as the skill's name. |
| `phases` | No | Declares a **phased drive** plan — see below. |
| `run_dir` | No | Where the phase globs live, when they aren't relative to the workspace root. |

---

## Phased skills (long unattended builds)

A skill that produces a multi-file deliverable — a threat model, a report suite,
a documentation set — hits a wall on a local model when it's built in one
conversation: context grows with every file read, and past roughly the model's
window the run stalls or starts truncating tool calls. Declaring a `phases:`
plan opts the skill into the **phased drive**, which runs each phase in its own
fresh context so peak context stays bounded to one phase's work:

```markdown
---
description: Produce the API documentation suite. Use when asked to document the API.
run_dir: docs/api            # optional; omit to use the workspace root
phases:
  - name: outline
    setup: true              # runs before run_dir exists; scaffolds the files
    files: ["outline.md"]
    prompt: Explore {cwd}, then scaffold {files} under {run_dir} with one `<!-- PENDING: <section> -->` marker per section.
  - name: endpoints
    files: ["endpoints-*.md"]
    prompt: Fill every PENDING marker in {files} with real, evidence-grounded content.
---
```

| Phase field | Meaning |
|---|---|
| `name` | Label used in logs and progress notices |
| `files` | Globs (relative to `run_dir`) the phase must clear of `<!-- PENDING` markers before it counts as complete. Required — a phase with no files has no completion oracle |
| `setup` | Marks the first phase, which runs before `run_dir` exists. At most one |
| `prompt` | Seeds the phase's fresh context. Placeholders: `{task}`, `{run_dir}`, `{skill_dir}`, `{cwd}`, `{phase}`, `{files}`. Optional — a phase without one gets a generated prompt from its name and files |

The **`<!-- PENDING: … -->` marker is the completion oracle**: the setup phase
stubs each section with one, later phases replace them with real content, and a
file with no markers left is done. This is also what makes a drive resumable —
an interrupted build re-run picks up at the first file still carrying a marker,
costing nothing for the phases already finished.

`run_dir` may be a glob (`.aegis/reports/*`), which resolves to its
most-recently-modified matching directory — the pattern for a skill whose setup
phase scaffolds a fresh dated output directory per run. Before anything matches,
the drive treats the plan as unscaffolded and runs the setup phase.

Start a drive from any client: `/drive <skill> <task…>` in the TUI, the **Drive**
button beside the web UI composer, `aegis chat --skill <name>` from the CLI, or
`POST /sessions/{id}/drive` over the HTTP API (see
[sessions.md](sessions.md#phased-drives)). A skill with no phase plan is refused
rather than silently run as one growing conversation — that failure is exactly
what the phased drive exists to avoid. `threat-modeling` ships with a built-in,
hand-tuned plan and needs no frontmatter.

---

## Bundled skill (companion scripts, templates, references)

A skill can be a directory instead of a single file, bundling a `SKILL.md` manifest with sibling assets — templates, reference docs, validation scripts:

```
.aegis/skills/html-report/
├── SKILL.md
├── template.html
└── validate_report.py
```

The directory name is the skill's name (`html-report`), same precedence rules as a flat file. When this skill is loaded, its content gets a `<skill_assets>` block appended automatically, listing every sibling file (recursively) so the model knows to read them with its own file tools rather than fabricating their contents:

```
<skill_assets dir=".aegis/skills/html-report">
Read these with your file tools before proceeding; do not fabricate their contents.
- template.html
- validate_report.py
</skill_assets>
```

You don't write this block yourself — it's generated at load time from whatever files sit next to `SKILL.md`.

### Worked example

This mirrors the built-in `html-report` skill (`internal/skills/builtin/html-report/`): a report-writing skill that ships a real HTML template and a Python validator instead of describing the format in prose each time.

`SKILL.md`:
```markdown
---
name: html-report
description: Use when asked to produce a polished, shareable report as a standalone HTML file instead of plain markdown. Triggers on "write this up as a report", "html report", "shareable page".
---

# HTML Report Skill

1. Read `template.html` in this skill's directory (see `<skill_assets>` below)
   — a complete, valid report with dummy content in every section.
2. Copy it and replace the dummy content with the real report. Keep the
   `<style>` block and theme overrides intact.
3. Write the finished file to disk with your normal file-write tool.
4. Run the validator before telling the user you're done:
   `python validate_report.py path/to/report.html`
   Fix and re-run until it exits 0.
5. Tell the user the output path.
```

`template.html` — a real, complete HTML document the model copies and edits, not a description of one.

`validate_report.py` — a stdlib-only Python script that checks structural invariants (self-contained, has both light/dark theme overrides, no unclosed tags) and exits non-zero with a specific message when something's wrong.

The pattern generalizes: whenever a skill's output has to satisfy invariants that are easy to describe but easy to get subtly wrong (a required CSS override, a required JSON field, a required section), bundle a script that checks the invariant mechanically instead of just writing the invariant down as prose the model might skim past.

---

## Precedence & name collisions

Skills are discovered from three places, first match per name wins:

1. `.aegis/skills/` — project-local
2. `~/.aegis/skills/` — user-global
3. Embedded built-ins (see below) — only the ones explicitly enabled

A project skill shadows a same-named user skill, which shadows a same-named built-in. This lets a project override a built-in's behavior (or a user's personal default) just by naming a file the same thing.

---

## Built-in skills

Aegis ships several skills embedded in the binary (`content-review`, `html-report`, `security-audit`, `architecture-diagram`, `debug-investigation`, `redteam-engagement`, `threat-modeling`, `latex-report`, `deep-research`, `structured-build`, `documentation-as-code`, `document-codebase`). Unlike a project/user skill file, nobody chose to author these for this project, so they stay **dormant by default** — zero system-prompt cost — until named explicitly:

```bash
aegis skills list                           # every built-in + on/off status
aegis skills enable security-audit          # project config (.aegis/config.yaml)
aegis skills enable security-audit --global # user-global config instead
aegis skills disable security-audit
```

Or from the TUI: `/skills` (list), `/skills enable <name> [global]`, `/skills disable <name> [global]`. This is the config-driven route: it writes `.aegis/config.yaml` (or the user-global one) and takes effect on the next daemon restart.

A project or user skill file with the same name always takes precedence over a built-in of that name — enabling a built-in never overrides something you authored yourself.

### On-demand activation

The four TUI commands that invoke a specific built-in skill directly — `/threat-model`, `/report`, `/research`, `/review` — activate that skill for the current session automatically, right when you run the command. No config edit or restart needed, and the skill stays dormant (no system-prompt cost) for every session that never asks for it; a fresh session starts with everything dormant again. This is what makes those commands work out of the box on a freshly cloned checkout even with an empty `builtin_enabled` list. `aegis skills enable`/`/skills enable` is still the right tool when you want a skill available every turn of every session without a dedicated command (e.g. `security-audit`, `architecture-diagram`).

---

## Creating skills from a session

The agent can save a skill mid-conversation with the `save_skill` tool — handy after working out a repeatable procedure together instead of writing the file by hand:

```json
{"name": "deploy-staging", "content": "1. Run `go build`...\n2. scp to staging...\n3. Restart service..."}
```

This always writes to the **project** skills directory (`.aegis/skills/<name>.md`) with exactly the content given — there's no `description` or `scope` parameter, so add a frontmatter block yourself afterward if you want the skill to participate in progressive disclosure rather than being eager-injected. `/remember` is the TUI entry point that can trigger this (describe a skill in chat and ask the agent to save it).

---

## Skills vs. memory

Use **memory** (`remember` tool, `.aegis/memory.md`) for static facts: endpoints, conventions, constraints — things worth recalling, not doing. Use **skills** for multi-step procedures: a deploy flow, a review checklist, a test strategy — things worth *replaying*. See [Memory & Knowledge](memory-and-knowledge.md) for the memory side of this.
