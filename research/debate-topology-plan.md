# P69 — Heterogeneous debate on one 16GB GPU

Plan and implementation blueprint for running Aegis's `internal/debate`
propose/critique/rebut/arbitrate pipeline with a *different model per role* on a single
RX 7900 GRE (16GB, gfx1100) with 16GB of system RAM.

Status: **design**. No code applied. Diffs below are the proposed change.

---

## 0. Start here: the pipeline exists, and one line blocks the thing you want

Aegis already has the whole pipeline — `internal/debate` (round structure, evidence-grounding
check, concession detection, budget-forced early arbitration, verdict parsing), three personas per
domain, four entry points (`aegis debate`, `/debate`, `POST /debate`, the `agent` tool's
`mode: "debate"`), and tool-using roles so the critic can actually `grep`/`read_file` for evidence.
None of that needs rebuilding.

What blocks per-role models is that every role runner hardcodes the one global model:

| Call site | Line | What it passes |
|---|---|---|
| `internal/server/debate.go` | `116` | `Model: s.cfg.Provider.Model` |
| `internal/cli/debate.go` | `126` | `Model: cfg.Provider.Model` |
| `internal/tool/builtin/agent.go` | `494` | `swarm.SpawnConfig` with `Model` left empty → daemon default |

`debate.RunFunc` is `func(ctx, systemPrompt, userPrompt string)`. The persona *name* the role was
resolved from is consumed inside `debate.Run` (`PersonaSystem`) and never reaches the runner, so the
runner cannot resolve a per-role model even though `Server.personaModel` already exists and already
implements the precedence you want (`config.personas[name].model` → persona file `model:` → global).

**The whole change is: give `RunFunc` the persona name, and resolve the model from it.** Every other
piece — per-model context-window detection (`effectiveContextWindowFor`), per-run `num_ctx` stamping
(`modelAdapter`/`provider.WithNumCtx`), `SpawnConfig.Model`, the `personas:` config override map —
is already built and already used by the session path.

There is one genuinely missing primitive, needed only for Topology 2: **`provider.Request` has no
`KeepAlive` field**, so a role cannot ask Ollama to evict its model afterwards. `keep_alive` is
adapter-level state fixed at `providerfactory.buildOne`. See §4.3.

---

## 1. Hardware & VRAM allocation map

### 1.1 The budget is smaller than 16GB, and the KV cache is bigger than you think

Two corrections to the premise before the tables mean anything.

**Usable VRAM is ~14.8GB, not 16GB.** Windows WDDM reserves a slice, and the desktop compositor
plus any browser holds 0.5–1.2GB. Budget **14.5GB** and treat anything above it as an eviction risk,
not a crash risk — Ollama's failure mode here is silently offloading layers to system RAM, which
`ollama ps` reports as `%GPU < 100` and which costs an order of magnitude of decode speed.

**The 9B does not leave 9GB of headroom at your documented 32k window.** KV cache per token is:

```
bytes_per_token = 2 × n_layers × n_kv_heads × head_dim × bytes_per_element
                  ↑ K and V
```

**Measured 2026-08-17** on `aegis-qwen35-9b:32k` (`Qwen3.5-9B-MTP` UD-Q4_K_XL) via
`vram_topology_probe.py`, replacing the derived figures this section originally carried:

| | reported by `/api/show` |
|---|---|
| layers | 33 |
| KV heads | 4 (16 query heads — GQA 4:1) |
| key/value head dim | 256 / 256 |
| training context | 262,144 |
| weights on disk | 6.57 GiB |

That is `33 × 4 × (256+256) × 2 = 135,168` bytes/token at f16 — **132 KiB per token**:

| window | KV f16 | KV q8_0 | KV q4_0 |
|---|---|---|---|
| 8k | 1.03 GiB | 0.55 GiB | 0.29 GiB |
| 16k | 2.06 GiB | 1.10 GiB | 0.58 GiB |
| 32k | 4.13 GiB | 2.19 GiB | 1.16 GiB |

(The original derived table assumed 36 layers × 8 KV heads × 128 dim = 144 KiB/token. It was ~9%
high, and every conclusion below survives the correction.)

So `aegis-qwen35-9b:32k` as you run it today is `6.57 (weights) + 4.13 (KV at f16) + 0.7 (compute
graph) = 11.4 GiB` **on its own**. Real headroom is ~3.1GB, not 9GB — enough for a 1.5B and nothing
else.

Two incidental findings worth keeping: the model's **training context is 262k**, so a 32k `num_ctx`
is nowhere near the degradation zone docs/local-model-tuning.md warns about — raising the debaters'
window is a VRAM decision here, not a quality one. And measured resident size at low occupancy is
**6.55 GiB at 100% GPU**, i.e. `/api/ps` reports the weights and grows the KV cache as tokens
arrive; it does not reserve the window up front. A probe run at 3% occupancy therefore proves
residency, not that the topology survives a full context — which matters because compaction keeps a
real agent turn at 85–96% of the window by design.

**The enabling move for Topology 1 is KV cache quantization**, not a smaller companion model:

```
OLLAMA_FLASH_ATTENTION=1
OLLAMA_KV_CACHE_TYPE=q8_0
```

q8_0 KV halves the cache for a quality cost that is negligible at these window sizes. It requires
flash attention, which llama.cpp supports on RDNA3/gfx1100 — **but that is the single assumption in
this document most likely to be wrong on your box**, so §5 measures it first. If FA turns out to be
unavailable or slower under ROCm, Topology 1 collapses to "9B at 8k + one 3B", and Topology 2
becomes the only real option.

### 1.2 Topology 1 — Concurrent

Everything resident, no swap, roles can run in parallel.

**Built and measured 2026-08-17.** Both variants exist; these are real numbers, not estimates:

| Slot | Model | Quant | Window | Weights | KV (q8_0) | Compute | Predicted | Measured resident |
|---|---|---|---|---|---|---|---|---|
| Debater A + Critic | `aegis-qwen35-9b:16k` | Q4_K_XL | 16k | 6.57 | 1.10 | 0.7 | 8.37 GiB | **6.02 GiB** |
| Arbiter | `aegis-phi4-reasoning:16k` | Q4_K_M | 16k | 2.89 | 1.06 | 0.35 | 4.35 GiB | **5.08 GiB** |
| | | | | | | | **12.72 GiB** | **11.10 GiB** |

Headroom against 14.5GB: **1.78 GiB predicted**, both models at 100% GPU. Measured resident is lower
than predicted because KV is allocated as tokens arrive — the ~1.6 GiB gap is cache the topology has
not spent yet, not headroom it gained.

> **The `KV (q8_0)` column label is not trustworthy, flagged 2026-08-17 while implementing P69.6.**
> Two things are wrong with the arithmetic in that table, and they happen to cancel.
>
> The `Weights` column is the **on-disk** size from `/api/tags`. P69.5 measured that this overstates
> `aegis-qwen35-9b`'s resident weights by 2.57 GiB — a vision projector that is never resident unless
> an image is sent — so the real figure is **4.00 GiB**, not 6.57. And 4.00 + the **f16** KV at 16k
> (2.06 GiB) is 6.06 GiB against the 6.02 measured, whereas q8_0 would predict 5.10. Read that way the
> debater row was measured at f16, and **q8_0 is unspent headroom rather than headroom already
> counted** — which is the more optimistic reading of the topology, not the less.
>
> But that reading contradicts §1.1's own note that "`/api/ps` reports the weights and grows the KV
> cache as tokens arrive": a probe at 3% occupancy should not show a *full* 16k cache. The 32k reading
> in §1.1 (6.55 GiB) is consistent with the growth model and inconsistent with a full f16 cache
> (4.00 + 4.13 = 8.13), so the two readings cannot both mean what the labels say.
>
> Nothing downstream should be pinned to this table until `research/scripts/vram_topology_probe.py`
> is re-run recording the server's actual `OLLAMA_KV_CACHE_TYPE` alongside each reading, and at a
> filled window rather than at low occupancy. The P69.6 regression test therefore asserts against the
> *hand-fitted* 16000-token figure and the 14.5 GiB budget, both of which are stated rather than
> inferred, and not against this table's measured column.

**The arbiter's `num_ctx` is the load-bearing setting, and it is not optional.** Unpinned,
`phi4-mini-reasoning:3.8b` ships with *no parameters at all*, so Ollama gave it a 32,768-token window
and it allocated **7.02 GiB resident** — more than the 9B debater, for a 3.8B model. Pinning it to
16k dropped that to 5.08 GiB. A 3.8B arbiter is only cheap if you tell it to be. That is the safety margin, and it is the right size — Ollama
sizes its compute graph optimistically and a long tool result can push a prefill past the estimate.

**Three resident models does not fit.** Adding `llama3.2:3b` at 8k (2.0 + 0.47 + 0.3 = 2.8 GiB)
lands you at 14.4 GiB against a 14.5 GiB budget. It will *appear* to work and then offload the first
time the compositor grabs another 300MB. Do not run the three-distinct-model version of Topology 1
on this box.

**The load-bearing consequence:** in Topology 1, Debater A and the Critic share one 9B runner. That
is fine and is not a compromise — see §2.2 for why the critic's seat wants the strongest model
anyway, and why the diversity you're buying belongs in the arbiter seat.

**Two gotchas specific to Ollama:**

1. Ollama keys a resident runner by **model name**, not by weight digest. `aegis-qwen35-9b:32k` and
   `aegis-qwen35-9b:8k` are two runners holding **two copies of the same 6.5GB of weights**. Ship
   exactly one 9B variant. Vary the window by variant only across topologies, never within one.
2. `OLLAMA_MAX_LOADED_MODELS` defaults to 3×GPU-count on some builds; pin it to `2` so Ollama
   refuses a third load instead of evicting your 9B mid-debate.

### 1.3 Topology 2 — Sequential swapping

One model resident at a time; each role's runner evicts before the next loads.

| Phase | Resident | Window | Total |
|---|---|---|---|
| Propose / Rebut | `aegis-qwen35-9b:32k` | 32k | 11.7 GiB |
| Critique | `aegis-qwen35-9b:32k` (same runner, no swap) | 32k | 11.7 GiB |
| Arbitrate | `aegis-phi4:16k` (14B Q4_K_M) | 16k | ~11.5 GiB |

This buys the full 32k window back for the debaters and a **14B** arbiter instead of a 3.8B one.
That is the real argument for Topology 2 — not fitting more models, but fitting *better* ones.

**The cost, on this box specifically, is worse than the usual analysis says**, because you have 16GB
of system RAM and an 11.7GB model. A swapped-out model does not survive in page cache; each swap is
a full re-read from disk. Per debate that is:

- 1 swap-in of the arbiter (~9GB from NVMe at 2–3 GB/s = **3–5s**, much worse on SATA)
- 1 swap-back of the 9B if the same daemon then serves a normal turn (**~3s**)
- plus a full cold prefill of the arbiter's transcript prompt (no prefix cache across a reload)

So ~10–20s of pure overhead per debate, once, at the arbitration boundary. **That is acceptable**
because the schedule below swaps exactly once per debate, not once per role. A naive
A→B→A→B→arbiter schedule would swap 5 times and is what makes sequential topologies feel
unusable — don't build that.

Both aggregate bounds already accommodate it: the agent tool's debate context is
`maxAgentDuration × (2·rounds + 2)`, and `MaxTurnStall` (900s) is per-role.

### 1.4 Which to build

**Build Topology 1 first.** It is a config change plus the §4.1 patch, it has no new failure modes,
and it gets you a genuinely independent arbiter today. Topology 2 needs a new provider primitive
(§4.3) and buys a bigger arbiter — worth doing second, as a config flag over the same code.

---

## 2. Model roles & selection matrix

### 2.1 The correction that matters

**In Aegis, a debate role is not a chat completion — it is a tool-using agent run.** Look at
`debateRoleRunner`: each role gets a full `engine.New` with the tool registry, a permission gate,
and a round-result cap. The critic persona's contract (docs/debate.md) is that a challenge must cite
`file:line`, a `grep`/`read_file` result, or a quoted passage, and `debate.hasEvidence` mechanically
tags anything else `[unsubstantiated]` — which the arbiter is instructed to discard.

A 3B model in the critic seat **cannot hold up its end of that contract.** It will produce
plausible-sounding challenges without driving the tool calls needed to ground them, every round gets
tagged `[unsubstantiated]`, the arbiter correctly ignores all of them, and you have paid 3× the
tokens to `UPHOLD` everything. The failure is silent and looks like "debate says my claim is fine."

Your own measurements already predict this: `qwen3:14b-32k` scores **2.7/12** on `SecurityTriage`
against the 9B's **10.7/12**, and P68.6 records that the 14B "never produces the artifact" — a
reporting-step failure, exactly the class of failure a debate role commits. A 3B is further down
that curve, not further up.

**Therefore: the critic seat takes your strongest tool-capable model. The arbiter seat is where a
different, smaller, or non-tool-capable model belongs** — arbitration reads a transcript that is
already in the prompt and emits three structured lines. It needs no tools at all.

This inverts half your proposed matrix and keeps the other half: Phi-4 as the structural arbiter is
right, and for the right reason. Llama-3.2-3B as Debater B is the part to drop.

### 2.2 The matrix

| Role | Topology 1 | Topology 2 | Why |
|---|---|---|---|
| **Debater A** (proposer/rebutter) | `aegis-qwen35-9b:16k` | `aegis-qwen35-9b:32k` | Your measured best local agent (10.7/12). Owns the claim; must defend with cited evidence, so it needs tools. |
| **Debater B** (critic) | `aegis-qwen35-9b:16k`, `critic` persona, **temperature 0.7** | same | Highest-skill seat: must *find* the flaw and *retrieve* the evidence. Diversity here is worth less than competence, because an uncited challenge is discarded outright. Adversarial pressure comes from the persona and a raised temperature, not from different weights. |
| **Arbiter** | `aegis-phi4-mini:8k` (3.8B Q4_K_M) | `aegis-phi4:16k` (14B Q4_K_M) | The only role that needs no tools — it reads a transcript and emits `VERDICT:`/`CONFIDENCE:`/`REASON:`. This is where a different family genuinely decorrelates error: a Qwen arbiter judging a Qwen debate rubber-stamps its own family's blind spots. Phi's structured-reasoning bias is a good fit for a rubric-shaped output. |

**`gemma4:12b` was measured and rejected for the arbiter seat** (2026-08-17), despite being the
different-family model already on this box. Two independent reasons: its weights are 7.04 GiB, so
`9B + gemma4` is 20.4 GiB against a 14.5 GiB budget under any window — it cannot be a *concurrent*
arbiter at all. And it uses sliding-window attention (1024 tokens) while reporting a null
`head_count_kv`, so its KV cost cannot be computed from `/api/show` at all — the probe returns a
0.40–3.19 GiB range rather than a number. It remains a candidate for the **Topology 2** arbiter
seat, where it is the only resident model and the range no longer has to be budgeted against.

**Do not use `qwen3:14b` in any seat** on this hardware. Two independent measurements in
docs/local-model-tuning.md put it far below the 9B on exactly this workload.

**The one thing to verify before trusting the arbiter seat** (§5, check 4): that the small arbiter
emits parseable `VERDICT:`/`CONFIDENCE:` lines. `debate.parseVerdict` anchors both to line-start; a
model that writes prose around them yields `Outcome: ""`, which surfaces as a verdict with no
outcome and is the most likely way a smaller arbiter fails.

### 2.3 Temperature: the actual source of debate diversity

With one model in both debater seats, the sampling parameters carry the adversarial load — and
Aegis has no per-role sampling knob, so this rides on Modelfile variants. Note that
docs/local-model-tuning.md's `temperature 0.2` is a *reasoned* default for tool fidelity, and its
two attempted A/Bs were void (saturated rubric). Treat the critic's 0.7 the same way: a reasoned
starting point, not a finding.

The critic still needs tool-argument fidelity to retrieve evidence, so 0.7 is the ceiling, not a
suggestion to go higher.

---

## 3. Agentic workflow logic

Your three-step chain maps onto `debate.Run` as it already stands — it is a *loop*, not a
three-step chain, and the difference is the point: the critic sees prior rounds
(`renderRoundsSoFar`), so round 2's challenge is informed by round 1's rebuttal.

```
claim (+ WithFiles reference block)
  │
  ├─ round i ≤ MaxRounds:
  │     budgetExhausted(cfg)? ──yes──► record BudgetStop, break to arbitration
  │     │
  │     CRITIC ── critique(claim, priorRounds) ────────────► [model: critic seat]
  │        │  hasEvidence()  → Round.Evidence  (else tagged [unsubstantiated])
  │        │  isConcession() → Round.Conceded  → break, no rebuttal
  │        ▼
  │     PROPOSER ── rebut(claim, priorRounds, critique) ───► [model: proposer seat]
  │
  └─ ARBITER ── verdict(full transcript) ───────────────────► [model: arbiter seat]
        parseVerdict → {UPHOLD|REVISE|REJECT, high|medium|low}
```

Call count: `2·rounds + 1`, minus one per conceded round. At the default `MaxRounds: 2` that is
**5 model calls** where a normal turn is 1.

**The Topology 2 schedule falls straight out of this shape.** Every proposer and critic call happens
before the single arbiter call, so the swap boundary is the loop exit — one eviction per debate:

```
[9B resident] round 1 critique → round 1 rebuttal → round 2 critique → round 2 rebuttal
                                                                            │
                                                          evict 9B (keep_alive: 0)
                                                                            ▼
[Phi-4 resident] arbitration → verdict
```

This is why the runner must know the role, not just the model: it needs to evict on the *last*
non-arbiter call, which only the loop position tells you. §4.3's `Role` enum carries exactly that.

---

## 4. Implementation

Four phases. P69.1 is the enabling change; P69.2–3 are config and models; P69.4 is Topology 2 only.

### 4.1 P69.1 — `RunFunc` carries the role's persona, so the runner can resolve its model

**`internal/debate/debate.go`**

```go
// Role names which seat a call is for. It reaches RunFunc because the
// caller — not this package — decides what a seat costs: which model
// serves it, which context window that model gets, and (P69.4) whether
// the runner should evict afterwards. This package still owns *what* the
// seat is; it just stops hiding it.
type Role string

const (
	RoleProposer Role = "proposer"
	RoleCritic   Role = "critic"
	RoleArbiter  Role = "arbiter"
)

// Seat is everything a caller needs to execute one role turn. Persona is
// the resolved persona *name* (after Domain defaults and per-role
// overrides), which is the key Server.personaModel and the `personas:`
// config map are both indexed by.
type Seat struct {
	Role    Role
	Persona string
	Last    bool // true on the final call before arbitration (P69.4 swap point)
}

type RunFunc func(ctx context.Context, seat Seat, systemPrompt, userPrompt string) (string, error)
```

`Run` then passes the seat it already computed:

```go
-	critique, err := run(ctx, criticSys, critiquePrompt)
+	critique, err := run(ctx, Seat{Role: RoleCritic, Persona: cfg.CriticPersona}, criticSys, critiquePrompt)
...
-	rebuttal, err := run(ctx, proposerSys, rebuttalPrompt)
+	rebuttal, err := run(ctx, Seat{Role: RoleProposer, Persona: cfg.ProposerPersona,
+		Last: i == cfg.MaxRounds}, proposerSys, rebuttalPrompt)
...
-	verdictText, err := run(ctx, arbiterSys, verdictPrompt)
+	verdictText, err := run(ctx, Seat{Role: RoleArbiter, Persona: cfg.ArbiterPersona}, arbiterSys, verdictPrompt)
```

`Last` is best-effort — a conceded round or a `BudgetStop` exits early, so the swap point is missed
and the arbiter simply loads alongside. That degrades to Topology 1's memory profile for that one
debate, which is why §1.2's budget must hold even under Topology 2. Set the critic's `Last` too when
`i == cfg.MaxRounds` is reached via concession if you want to close that; it is not worth the
complexity in v1.

**`internal/server/debate.go`** — the whole point of the change:

```go
 func (s *Server) debateRoleRunner(tracker *cost.Tracker, workdir string) debate.RunFunc {
 	tools := s.tools.Clone()
-	return func(ctx context.Context, systemPrompt, prompt string) (string, error) {
+	return func(ctx context.Context, seat debate.Seat, systemPrompt, prompt string) (string, error) {
+		// Resolve this seat's model exactly as a session resolves a persona's
+		// (personaModel: config override → persona file → global), then serve it
+		// with *its own* detected window rather than the primary model's. A 3.8B
+		// arbiter handed the 9B's 32k num_ctx allocates a KV cache it will never
+		// fill, out of the same 14.5GB the 9B is holding.
+		p, _ := persona.Get(seat.Persona)
+		model := s.personaModel(p)
+		ctxWin, _ := s.effectiveContextWindowFor(ctx, model)
 		gate, engineHooks := s.buildGate("build", s.approver(), persona.Persona{})
 		eng, err := engine.New(engine.Options{
-			Adapter:         s.adapter,
+			Adapter:         s.modelAdapter(ctxWin),
 			...
-			Model:           s.cfg.Provider.Model,
+			Model:           model,
```

Note `buildGate` keeps `persona.Persona{}` — the seat's persona supplies the *system prompt and
model*, deliberately not the tool gate. Widening a debate role's permissions from a persona file is
a separate decision with a security review attached; don't fold it into this change.

**`internal/cli/debate.go`** — same shape, but `aegis debate` is headless and has no
`effectiveContextWindowFor`. Use `cfg.Personas[name].Model` → `p.Model` → `cfg.Provider.Model` and
leave the window to the model's own pinned `num_ctx` (which is why §4.3 pins it per variant).

**`internal/tool/builtin/agent.go`** — `SpawnConfig.Model` already exists; fill it:

```go
+		Model: a.debateModelFor(seat.Persona),
```

`agent.go` has no `Server`, so it needs the resolver injected — add a
`DebateModelFor func(persona string) string` to the agent tool's options in `enginecfg`, defaulting
to "return empty" (= daemon default) so nothing changes for callers that don't wire it.

**Tests.** `internal/debate/debate_test.go` fakes take the new signature. Add one asserting the
seats arrive in order `critic, proposer, critic, proposer, arbiter` with the right persona names
under both domains — that is the contract the whole feature rests on, and it is cheap to pin.

### 4.2 P69.2 — Modelfiles

One variant per seat. Build them per docs/local-model-tuning.md's procedure; check the template
defect on each (`ollama show --modelfile <m> | grep 'else if.*ToolCalls'`) — the 9B ships Jinja and
is clean, Phi-4-mini needs checking.

```dockerfile
# Modelfile.debater — proposer seat. Fidelity over variety.
FROM qwen3.5:9b
PARAMETER num_ctx 16384          # 32768 under Topology 2
PARAMETER temperature 0.2
PARAMETER top_p 0.8
PARAMETER top_k 20
PARAMETER repeat_penalty 1
PARAMETER num_predict -1
```

```dockerfile
# Modelfile.critic — critic seat. Same weights, adversarial sampling.
# NOTE: a second name = a second resident runner = a second 6.5GB of weights.
# Only build this if you have accepted Topology 2, where nothing else is resident.
# Under Topology 1, point the critic persona at aegis-qwen35-9b:16k and get
# adversarial pressure from the persona prompt alone.
FROM qwen3.5:9b
PARAMETER num_ctx 32768
PARAMETER temperature 0.7
PARAMETER top_p 0.95
PARAMETER top_k 40
PARAMETER repeat_penalty 1
```

```dockerfile
# Modelfile.arbiter — arbiter seat. No tools; needs only structured output.
# As built and measured 2026-08-17.
FROM phi4-mini-reasoning:3.8b

# Pinned so Ollama stops handing this model a 32k window it does not need.
# Unpinned it allocated ~4.6 GiB of KV — as much as the 9B debater — for a
# seat whose entire input is one debate transcript. 16k rather than 8k because
# a *reasoning* arbiter generates its deliberation on top of that transcript.
PARAMETER num_ctx 16384

# Near the model's own recommended sampling, deliberately not crushed toward 0:
# starving a reasoning model's deliberation is how it degenerates into
# repetition, and the engine's loop detector will (correctly) abort that.
# Structured output is enforced by the arbiter persona prompt and parsed
# robustly — parseVerdict takes the *last* VERDICT: line, so a draft verdict
# inside the reasoning trace cannot override the ruling (P69.1).
PARAMETER temperature 0.6
PARAMETER top_p 0.95
PARAMETER repeat_penalty 1
PARAMETER num_predict -1
```

The debater variant derives from the tuned 32k model rather than the stock base, so it inherits the
verified Jinja template, the stops, and the tuned sampling — only the window changes:

```bash
printf 'FROM aegis-qwen35-9b:32k\n\nPARAMETER num_ctx 16384\n' > Modelfile.debater16k
ollama create aegis-qwen35-9b:16k     -f Modelfile.debater16k
ollama create aegis-phi4-reasoning:16k -f Modelfile.arbiter
```

Verified after building: `aegis-qwen35-9b:16k` kept its Jinja template, `temperature 0.2`,
`top_p 0.8`, `top_k 20`, `repeat_penalty 1` and both `<|im_start|>`/`<|im_end|>` stops, and reports
capabilities `[tools, thinking, completion, vision]`.

**The arbiter's `num_ctx` is a real constraint, not a formality.** The arbitration prompt is the
*entire transcript* — 2 rounds × (critique + rebuttal) of tool-using agent output. 8k is tight; if
`ollama ps` shows the arbiter truncating, raise to 12k and take it out of the headroom, or lower
`MaxRounds`.

### 4.3 P69.3 — Config

Because P69.1 routes through `personaModel`, the whole topology is expressed in existing config
surface. No new keys:

```yaml
provider:
  default: ollama
  model: aegis-qwen35-9b:16k        # Debater A / proposer, and the daemon default
  keep_alive: "30m"
  max_concurrent_requests: 1        # local default; see provider/admission.go

personas:
  critic:              {model: aegis-qwen35-9b:16k}
  security-critic:     {model: aegis-qwen35-9b:16k}
  general:             {model: aegis-qwen35-9b:16k}
  security-researcher: {model: aegis-qwen35-9b:16k}
  arbiter:             {model: aegis-phi4-reasoning:16k}
  security-arbiter:    {model: aegis-phi4-reasoning:16k}
```

Only the two arbiter lines are strictly required — the debater seats inherit `provider.model`. They
are listed explicitly because a seat that silently falls back to the global model is the failure
this whole change exists to make visible.

Ollama server environment (systemd drop-in, or the service's env on Windows):

```
OLLAMA_FLASH_ATTENTION=1
OLLAMA_KV_CACHE_TYPE=q8_0
OLLAMA_MAX_LOADED_MODELS=2       # refuse a third load rather than evict mid-debate
OLLAMA_NUM_PARALLEL=1            # one slot; do not split the KV cache
OLLAMA_KEEP_ALIVE=30m
```

`max_concurrent_requests: 1` stays at the local default. The measured curve in
`provider/admission.go` (K=4 buys 1.6× aggregate throughput for 70% worse p50 latency) applies here,
and debate rounds are sequential anyway — there is nothing to overlap except the arbiter, which
runs last.

### 4.4 P69.4 — Per-request `keep_alive` (Topology 2 only)

This is the one new primitive. Mirror `WithNumCtx` exactly — same decorator shape, same
"explicit per-request value wins" rule, same `Unwrap`:

```go
// internal/provider/provider.go
type Request struct {
	...
	// KeepAlive is how long the backend should keep this request's model
	// resident afterwards (Ollama's keep_alive). "0" evicts immediately.
	// Empty means "adapter default" — the P69.4 sequential-debate topology
	// is the only caller that sets it, because it is the only one that knows
	// a *different* model is about to need the whole GPU.
	KeepAlive string
}
```

```go
// internal/provider/keepalive.go — WithKeepAlive(base, v) mirroring numctx.go
```

`internal/provider/ollama/ollama.go:878` currently sets `KeepAlive: a.keepAlive` unconditionally;
change to prefer `req.KeepAlive` when non-empty. Non-Ollama adapters ignore the field, as they
already ignore `NumCtx`.

Then in the runner, gated behind a config flag so Topology 1 is unaffected:

```go
if s.cfg.Debate.SequentialSwap && seat.Last {
    adapter = provider.WithKeepAlive(adapter, "0")   // evict before the arbiter loads
}
```

**Two things to get right.** Eviction must happen *after* the response completes, which
`keep_alive: 0` on the request already guarantees — do **not** implement this as a separate
`POST /api/generate {"keep_alive":0}` call, which races the in-flight request. And the arbiter's
first call after a swap pays a cold prefill; `MaxTurnStall` (900s) covers it, but the telemetry will
show a `load_duration` spike that is expected, not a regression.

---

## 5. Validation, cheapest first

Run these in order. Each one can invalidate the plan below it.

1. **Does flash attention + q8_0 KV work on gfx1100?** Set the env vars, load the 9B, and check
   `ollama ps` reports `100% GPU` with a `SIZE` roughly matching §1.1's q8_0 row. If FA is
   unavailable under your ROCm build, stop and re-plan §1.2 around f16 KV at 8k.
   → `python research/scripts/vram_topology_probe.py --topology 1`

2. **Does the topology actually fit?** Load every model in the topology, run a long prompt through
   each, and assert `100% GPU` on all of them simultaneously. The probe script does this and prints
   the measured-vs-predicted table.
   → `python research/scripts/vram_topology_probe.py --topology 1 --stress`

3. **Do the tuned variants still drive tools?** `aegis doctor` for conformance, plus the
   history-fidelity probe from docs/local-model-tuning.md §"Verifying the result" on any variant
   whose template you haven't checked. The critic seat's raised temperature is the risk here — if
   conformance drops, walk 0.7 back toward 0.4.

4. **Does the small arbiter emit a parseable verdict?** The one failure mode specific to this plan.
   Run 5 debates and check `transcript.Verdict.Outcome != ""` on all 5:
   ```bash
   for i in 1 2 3 4 5; do
     aegis debate "The retry decorator in internal/provider/retry.go backs off on 429s" \
       --domain generic --file internal/provider/retry.go --output-format json | jq -r .verdict
   done
   ```
   Five non-empty `UPHOLD|REVISE|REJECT` values means `parseVerdict` is getting its anchored lines.
   Any empty string means the arbiter model is wrapping the rubric in prose — fix with a lower
   temperature first, a `security-arbiter`-style persona tightening second, a bigger arbiter third.

5. **Is the heterogeneous debate actually better?** The honest version of this question needs the
   graded instrument, not a vibe check. `SecurityTriage` is scored out of 12 and P68.4 already
   records that it saturates against the 9B — so it will not discriminate here either. **Do not
   report a same-family-vs-cross-family arbiter comparison off a saturated rubric**; that is the
   mistake P68.4 exists to record. Widening the rubric's band is a prerequisite, not a detail.

   The cheap interim signal: run the same 10 claims through a same-model arbiter and a Phi arbiter
   and count **verdict disagreements**. Disagreement doesn't prove the Phi arbiter is right, but
   zero disagreement across 10 claims proves the second model is buying you nothing, which is the
   result worth knowing before you spend 3.2GB of VRAM on it.

---

## 6. Risks and things this plan does not claim

- ~~The KV-per-token figure is derived, not measured.~~ **Resolved 2026-08-17** for the 9B: measured
  at 132 KiB/token (33 × 4 × 512), ~9% below the derived estimate. The arbiter rows are still
  estimates until those variants are built.
- **Residency was verified at 3% context occupancy, not at full.** `/api/ps` grows the KV cache as
  tokens arrive, so the probe's "100% GPU" result proves nothing was offloaded at low occupancy. The
  predicted total, not the measured one, is what to budget against. Re-run `--stress` against a
  near-full window before trusting Topology 1 under real agent traffic.
- **q8_0 KV quality cost is asserted as negligible and not measured here.** It is well established
  at these window sizes, but if the critic's evidence citations start drifting (wrong line numbers,
  misquoted passages) this is the first thing to revert.
- **Flash attention on RDNA3 under ROCm is the plan's single point of failure.** §5 check 1 exists
  solely to find out early.
- **A 3.8B arbiter has not been shown to arbitrate well.** §2.2's argument is structural (the role
  needs no tools) and it is sound, but "needs no tools" is not "needs no capability" — weighing a
  cited challenge against a rebuttal is a judgment task. §5 check 4 catches the *format* failure;
  only check 5 catches a *judgment* failure, and check 5 needs a better instrument first.
- **P69.1 changes an exported signature** (`debate.RunFunc`) with three production call sites and
  the test fakes. It is mechanical, but it is not a one-file change.
- **Per-role sampling parameters ride on Modelfile variants**, which under Topology 1 means a second
  copy of the weights in VRAM. That is why §2.2 puts the critic on the *same* variant as the
  proposer and takes its adversarial pressure from the persona prompt. If you later want a
  genuinely different critic temperature under Topology 1, the right fix is a `Request.Options`
  passthrough, not a second Modelfile.
