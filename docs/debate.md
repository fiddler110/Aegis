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

This runs for up to `max_rounds` (default 2) critique/rebuttal exchanges, then always produces a
verdict — including when a round is skipped for budget reasons or a role call errors, since a
partial transcript arbitrated as-is beats no verdict at all.

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
