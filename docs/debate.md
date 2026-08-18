# Multi-Agent Debate

Debate runs a claim through adversarial review instead of accepting it on a single unchallenged
pass: a **critic** hunts for the weakest part of the claim (grounded in cited evidence, or an
explicit concession if it finds no flaw), the **proposer** rebuts, this repeats for a bounded
number of rounds, and an **arbiter** synthesizes the transcript into a final verdict. It's the same
mechanism whether the claim is a security finding, a design decision, or a paragraph in an
implementation plan — what changes is which personas play the three roles and whether you point
them at source material to ground the debate in.

Use it whenever you'd otherwise trust a single model pass on something that matters: "is this
finding a false positive," "does this migration plan actually handle the rollback case," "is this
threat fully mitigated," "does section 3 of this design doc contradict section 1."

---

## The three roles

| Role | Job |
|------|-----|
| **Proposer** | Authored (or is standing behind) the claim; rebuts each challenge unless it concedes the point |
| **Critic** | Tries to find one specific, concrete flaw — a factual error, an unsupported assumption, a missing case, an internal inconsistency. Must ground each challenge in cited evidence (`grep`/`read_file`/`web_fetch`/`security_scan` output, a `file:line`, a quoted passage) or explicitly `CONCEDE`. An uncited challenge is tagged `[unsubstantiated]` and discarded by the arbiter — it can't move the verdict |
| **Arbiter** | Reads the full transcript and issues a final `VERDICT: UPHOLD \| REVISE \| REJECT` with a `CONFIDENCE: high \| medium \| low` |

This runs for up to `max_rounds` (default 2, hard-capped at 10 regardless of what a caller requests)
critique/rebuttal exchanges, then always produces a verdict — including when a round is skipped for
budget reasons or a role call errors, since a partial transcript arbitrated as-is beats no verdict
at all.

---

## Domains: security vs. generic

`domain` selects the default persona trio. It only matters if you don't override individual role
personas yourself.

| Domain | Proposer | Critic | Arbiter | Use for |
|--------|----------|--------|---------|---------|
| `security` (default) | `security-researcher` | `security-critic` | `security-arbiter` | Vulnerability findings, threat/mitigation pairs, security design assertions |
| `generic` | `general` | `critic` | `arbiter` | Documents, implementation plans, design decisions, anything non-security |

Individual `--proposer`/`--critic`/`--arbiter` overrides (or the equivalent request fields) always
win regardless of `domain` — `domain` just picks sensible defaults so you don't have to name all
three every time.

---

## Four ways to run a debate

All four call the same `internal/debate` engine and produce the same transcript format. Pick
whichever fits where you already are.

### CLI — `aegis debate`

Headless, no daemon required (same one-shot construction as `aegis chat`):

```bash
aegis debate "The rate limiter fully mitigates the credential-stuffing risk on /login"
```

```bash
aegis debate "The plan's phased rollout in migration-plan.md correctly handles rollback" \
  --domain generic --file migration-plan.md
```

Flags: `--domain`, `--file` (repeatable), `--proposer`/`--critic`/`--arbiter`, `--max-rounds`,
`--output-format text|json`. Full flag reference: [cli-reference.md](cli-reference.md#aegis-debate).

### TUI — `/debate`

Runs directly against the daemon's configured model, no conversational turn needed first:

```
/debate The plan's phased rollout in migration-plan.md correctly handles rollback --domain generic --file migration-plan.md
```

Same flags as the CLI. `/debate --help`-style detail is in `/help debate`.

### HTTP — `POST /debate`

For scripting or a custom client against a running daemon:

```bash
curl -s http://localhost:PORT/debate \
  -H 'Content-Type: application/json' \
  -d '{
    "claim": "The plan'\''s phased rollout in migration-plan.md correctly handles rollback",
    "domain": "generic",
    "files": ["migration-plan.md"]
  }' | jq
```

Response:

```json
{
  "report": "=== Claim ===\n...\n=== Verdict ===\nREVISE (confidence: medium)\n...",
  "verdict": "REVISE",
  "confidence": "medium"
}
```

### From inside a conversation — the `agent` tool

The model can trigger a debate mid-conversation without you invoking anything directly — useful
when it has just produced a claim (a finding, a recommendation) and should check its own work, or
when you ask it to "debate whether X" in plain language:

```json
{
  "mode": "debate",
  "claim": "Section 3's caching strategy contradicts the consistency guarantee promised in section 1.",
  "domain": "generic",
  "files": ["docs/design.md"],
  "max_rounds": 2
}
```

Full mode reference: [multi-agent.md#debate-p12](multi-agent.md#debate-p12).

---

## Grounding a debate in real documents

This is the main tool for the "check my implementation plan for accuracy" use case: pass `files`
(CLI `--file`, repeatable; HTTP/tool `files: [...]`) and the claim is extended with a reference-
material block instructing every role to read those files with their own `read_file` tool before
proposing, critiquing, or rebutting — so the critic is checking the claim against what the
documents actually say, not against a paraphrase or its memory of them.

Worked example — you've written a three-document rollout plan and want the riskiest part checked
before you commit to it:

```bash
aegis debate \
  "The database migration plan (docs/migration-plan.md) is compatible with the zero-downtime \
   deployment strategy described in docs/deploy-strategy.md — no step requires a maintenance window" \
  --domain generic \
  --file docs/migration-plan.md \
  --file docs/deploy-strategy.md \
  --max-rounds 3
```

The critic will read both files, look for a step in the migration plan that conflicts with the
deployment strategy's zero-downtime constraint (e.g. a blocking schema lock, a column drop that
breaks the old binary during a rolling deploy), and either cite the specific line it found or
concede. The arbiter's `REVISE` or `REJECT` verdict tells you whether to trust the plan as written
or go fix the specific gap it names — a `REASON:` line in the verdict text points at which round
drove the decision.

You can point `files` at any mix of markdown docs, code, or config — anything the roles' `read_file`
tool can open. This works the same way in security-domain debates (pointing the critic at the
actual scanned file instead of trusting the finding's own summary of it).

---

## Overriding roles individually

Any role persona can be swapped independently of `domain` — useful when you want, say, a
security-flavored critic scrutinizing a claim about a generally-written design doc, or you've
written a custom persona file for a specialized reviewer:

```bash
aegis debate "This design doc's threat model covers the new webhook endpoint" \
  --domain generic --file docs/design.md \
  --critic security-critic
```

`aegis persona list` shows every persona (built-in or custom `.md` file) available for any role.

---

## Running each role on a different model

Each role runs on whatever model its persona resolves to — a `personas.<name>.model` config
override first, then the persona file's own `model:` frontmatter, then `provider.model`. This is the
same precedence the session path has always used for a persona; before P69.1 a debate ignored it and
ran all three roles on `provider.model`.

```yaml
provider:
  model: aegis-qwen35-9b:16k     # debaters, and the daemon default

personas:
  security-arbiter: {model: aegis-phi4-mini:8k}
  arbiter:          {model: aegis-phi4-mini:8k}
```

Each role is served with **its own** context window (detected per model), not the primary model's —
a small arbiter handed a 9B's 32k `num_ctx` would allocate a KV cache it never fills out of the same
VRAM the debater is holding.

**Which seat to vary.** The arbiter is the seat worth changing, and the critic is the seat to leave
alone:

- The **critic** must ground each challenge in evidence it retrieves itself (`grep`/`read_file`), and
  `hasEvidence` tags an uncited challenge `[unsubstantiated]` for the arbiter to discard. A model
  that can't reliably drive tool calls fills every round with challenges that are then correctly
  ignored — the debate returns `UPHOLD` for a reason that has nothing to do with the claim. Give
  this seat the strongest tool-capable model you have.
- The **arbiter** calls no tools at all: it reads a transcript already in its prompt and emits
  `VERDICT:`/`CONFIDENCE:`/`REASON:`. That makes it the one seat a smaller or differently-trained
  model can take, and the seat where a different model actually decorrelates error rather than just
  costing VRAM — an arbiter from the debaters' own family shares their blind spots.

If you swap in a smaller arbiter, check that verdicts still parse: `parseVerdict` anchors
`VERDICT:`/`CONFIDENCE:` to line-start, so a model that wraps them in prose yields an empty
`verdict` field. Run a few claims with `--output-format json` and confirm the field is non-empty.

### Fitting the seats in one GPU

Per-seat models mean two or three models are resident **at the same time**, and until you state a
memory budget each of them is sized as if it owned the card. Set one:

```yaml
provider:
  vram_budget_gb: 14.5     # what Ollama may hold, across every model at once
```

That figure is stated, never detected — Aegis performs no GPU/VRAM introspection on any platform —
and it is not the card's capacity: subtract the driver reserve and whatever your desktop already
holds. ~14.5 of a 16 GB card is the measured figure on the machine this was calibrated against.

With it set, every debate — CLI, TUI, HTTP, and the `agent` tool — plans its seats as one resident
set before the first role runs:

- Windows are split by equal **token count**, clamped at each model's training maximum. Two seats
  reading the same transcript need comparable room to hold it.
- Seats sharing a model are planned **once**. Ollama holds one runner per model *name*, so a shared
  model means one copy of the weights and one KV cache; counting it twice would refuse sets that fit.
- The plan is installed for the debate's duration and the solo windows are restored afterwards. A
  session turn that lands mid-debate is served the planned (smaller) window rather than flipping the
  runner back — that thrash is the thing being avoided, and the only visible effect is that the turn
  compacts a little earlier.
- A window is never *raised* by a plan. Shrinking is the only direction that buys anything.
- If no assignment fits, the debate is **refused with a reason** before a single model turn is spent.

Preview any of this without spending a debate:

```console
$ aegis models --fit-debate
```

Every path reads the same key and runs the same planner, so `aegis debate` (headless, no daemon) and
the daemon cannot disagree about the machine they are both running on. See
[cli-reference.md](cli-reference.md#--fit-set----fit-debate-planning-a-resident-set-p696) for the
output, and [research/debate-topology-plan.md](../research/debate-topology-plan.md) for the measured
worked example, with a harness at `research/scripts/vram_topology_probe.py`.

---

## Cost and rounds

Each round is at least 2 model calls (critique + rebuttal, or just the critique if the critic
concedes), plus one more for arbitration — a 2-round debate is up to 5 calls where a normal turn is
1. Debate checks the run's cost tracker before starting each round and, once within 90% of your
configured `cost.budget_usd`/`cost.max_tokens_per_run`, skips straight to arbitration on whatever
transcript it has rather than starting a round that would hit the budget mid-critique with nothing
to show for it. Tune `--max-rounds` down for cheap/fast sanity checks, up for claims worth
scrutinizing harder.

---

## Reading the verdict

```
=== Claim ===
<the claim, with any reference-material block appended>

--- Round 1 critique [evidence cited] ---
<critic's challenge, with the file:line or quote it grounded it in>
--- Round 1 rebuttal ---
<proposer's response>

=== Verdict ===
REVISE (confidence: medium)
VERDICT: REVISE
CONFIDENCE: medium
REASON: <one to three sentences naming which round drove the decision>
```

`[unsubstantiated]` on a round means the critic's challenge cited no retrievable evidence — the
arbiter is instructed to treat it as noise, not a real rebuttal, so it shouldn't by itself flip an
`UPHOLD`. If you see a verdict that seems to ignore a round, check that round's evidence tag first.

`--output-format json` (CLI) gives you `{"report", "verdict", "confidence", "cost_usd", "turns"}`
for scripting instead of parsing the text report.

---

## Automatic debate in existing workflows

Two security workflows can opt into routing a claim through debate automatically before finalizing
their output (both default off, since debate multiplies model calls per item):

```yaml
security:
  debate:
    threat_model: false   # security-architect: debate each threat/mitigation before writing it down
    triage: false          # security-audit skill: debate a borderline/disputed finding before suppressing it
```

These always use the security domain trio (they're inherently security claims) — there is no
generic-workflow equivalent yet; use one of the four direct entry points above for document/plan
review.

---

## Writing a good claim

The critic can only attack what you actually assert. Vague claims get vague (and less useful)
debates:

- Weak: `"This plan is good"`
- Better: `"The rollout plan's step 4 (dropping the legacy `user_id` column) is safe to run before
  step 6 (deploying the new binary that stops reading it)"`

A claim with a specific, falsifiable assertion — naming the file, section, or step it's about —
gives the critic something concrete to check against the source material instead of something to
agree or disagree with in the abstract.
