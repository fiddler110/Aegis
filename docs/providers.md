# Providers & Models

Aegis supports local LLMs and cloud providers through a normalized adapter interface. The provider configuration determines which model the agent calls and how.

---

## Provider Architecture

Two adapters handle all model communication:

- **`anthropic`** — Anthropic Messages API (`/v1/messages`), SSE streaming
- **`openai`** — OpenAI Chat Completions API (`/v1/chat/completions`), SSE streaming; also handles **all local LLMs** that expose an OpenAI-compatible endpoint

Set `provider.default` to choose which adapter to use.

---

## Local LLMs

Local LLM usage is the primary focus of Aegis. Any server exposing `/v1/chat/completions` works.

### Ollama (recommended for local use)

```yaml
provider:
  default: openai
  base_url: "http://localhost:11434/v1"
  model: "auto"     # picks first available model at startup
  max_tokens: 8192
```

```bash
export OPENAI_API_KEY="ollama"   # local servers ignore the key; must be non-empty
```

Aegis **auto-starts Ollama** if it is installed but not running. You do not need to run `ollama serve` manually.

```bash
ollama pull llama3.2           # general purpose
ollama pull qwen2.5:32b        # better tool use
ollama pull qwen3:14b          # reasoning model
ollama pull deepseek-r1:14b    # reasoning model
```

**Shared-host note:** Ollama serves over plain HTTP by default (no TLS), so daemon↔Ollama traffic is unencrypted. On a single-user machine this is no different from any other localhost loopback traffic. On a **shared multi-user host**, another local account could observe or tamper with that traffic; where Ollama supports it, enable TLS, or prefer a single-user host for sensitive work.

### Tool-calling reliability for local models

Not every locally-runnable model reliably drives Aegis's agent loop. The engine's tool-dispatch loop
depends on the model actually emitting a structured `tool_call` in its response — a model that instead
*describes* the action it would take, in prose, never triggers `edit_file`/`write_file`/etc., and the
turn just ends with nothing done. A live evaluation (`TestLiveWorkflow`, 2026-07-14) drove three local
models through identical run/fix/verify tasks over the real HTTP+SSE seam the TUI/web UI use and found
wide variance:

- **`gpt-oss:20b`** completed the task end-to-end (13 tool calls, ~2m28s) — reliable tool-calling.
- **`qwythos:latest`** (this repo's own template default `provider.model`) correctly diagnosed the
  seeded bug in its response text but never called `edit_file`/`write_file` to actually fix it —
  partial reliability: it reasons well but doesn't consistently close the loop into action.
- **`deepseek-r1:8b`** made **zero tool calls** on an explicit, unambiguous task, answering entirely in
  prose instead. This is a known R1-distill failure mode: the model's reasoning gets dumped as the
  final answer instead of being followed by a structured tool call.

A later pass (2026-07-16) added a fourth, and a different failure shape:

- **`qwen2.5-coder:1.5b`** made **zero tool calls** despite an Ollama manifest that *claims* tool
  support. It printed tool-call-shaped JSON (`{"name": "shell", "arguments": {...}}`) into its prose,
  then **fabricated** the results — inventing a plausible directory listing rather than reporting that
  it hadn't read anything. Worth calling out separately from the `deepseek-r1:8b` case above: manifest
  metadata is not evidence a model can speak the protocol, and a model at this size may confabulate the
  output of the tool it failed to call. Prefer a larger model for anything agentic.

Takeaways:

- Prefer models explicitly instruction-tuned for tool/function calling (`gpt-oss:20b`-class models,
  `qwen2.5:32b`+) for agentic tasks over small reasoning-distilled models. Reasoning-distilled models —
  the `-r1`/`deepseek-r1` family in particular — are prone to answering in prose even when a tool call
  is clearly required; see [Extended Thinking](#extended-thinking) for more on thinking-model quirks
  (a separate concern from tool-calling reliability — a model can support tool calls fine while its
  thinking preamble leaks stray text, or vice versa).
- `aegis doctor` (see [CLI Reference](cli-reference.md)) includes a "tool-calling" check (P28.2) that
  sends a cheap, obviously-actionable smoke-test prompt to the configured local model and **warns**
  (never fails hard — safe for offline/CI use) if it comes back with zero tool calls. Run it after
  switching models to catch this before it costs you a real task.
- Since P53.4 that check reports a **conformance rate**, not a yes/no verdict: it runs the probe
  `provider.tool_call_probe_trials` times (default 5) and reports how many trials actually produced a
  tool call — `3/5 trials made a tool call (60%)` warns, `5/5` passes. "Can this model ever call a
  tool" and "how often does it" are different questions, and only the second predicts whether an
  unattended run survives; a model that complies 60% of the time passes a single-trial probe and then
  fails a long drive in a way that reads like a harness bug. Trials cut off at the probe's token cap
  reach **no verdict** and are excluded from the rate's denominator rather than counted as misses, so
  a slow-thinking reasoning model is never accused by the arithmetic; if *every* trial is truncated
  there is no rate at all, and doctor says so. Because these trials run inline, `aegis doctor`
  announces the trial count before it starts; set `provider.tool_call_probe_trials: 1` for the old
  single-trial check.
- You no longer have to remember to run it (P34.2). The daemon runs that same probe itself at run
  start, for local Ollama-style providers only, and warns before the turn is spent if the model can't
  call tools — once per model (the verdict is cached for the daemon's life) and once per session (a
  tool-incapable model is still fine to converse with). A probe that can't reach a verdict — an
  unreachable server, a timeout — stays silent rather than blaming the model. The probe is not extra
  latency in practice: it runs against the model your turn is about to load anyway, so it shares that
  cold load rather than adding one. The daemon deliberately blocks on **one** trial only — the rest
  of the conformance sample runs in the background and refines the cached rate for later notices — so
  raising `provider.tool_call_probe_trials` never delays your first reply.
- Since P53.5 that verdict is no longer re-paid on every restart. The probe result — and the
  conformance rate behind it — is persisted per model in `<data_dir>/model_caps.json`, alongside the
  other quirk Aegis learns the hard way (a model that 400s the instant the `think` parameter is
  sent). An `aegis doctor` run, which takes the full blocking sample, seeds the same file, so a
  daemon started afterwards reuses it and probes nothing at all.

  Staleness is handled by not trusting the name: an Ollama tag is mutable, so records are keyed to
  the model's **content digest** and a re-pulled model loses its record and gets re-probed. The file
  is a cache and nothing else — deleting it costs one re-probe, never correctness — and an
  unreachable model server invalidates nothing (Aegis cannot tell whether the weights moved, and
  "cannot tell" must not mean "everything is stale").

  You can also skip discovery entirely with `provider.model_capabilities` (see
  [Configuration](configuration.md)): a declaration there outranks anything discovered, which both
  tells Aegis about a model it has never met — so the failing request is never sent even once — and
  gives you a way to override a cached verdict without deleting the file.
- Independently, the engine watches for the `qwen2.5-coder:1.5b` signature above: if a turn's answer
  contains tool-call-shaped JSON naming a real tool, it says so. This
  costs nothing and needs no probe, so it also covers `aegis chat`, which runs its own in-process
  engine and never touches the daemon's run path. Since P59.6 this includes a turn that *did* make a
  real tool call and printed another one as prose alongside it — a partial-protocol reply, reported
  as such rather than as a model that can't call tools. A model quoting JSON in a settled answer
  after tool calls have already succeeded is still left alone: that is quotation, not incapacity.
- If a model **cannot** speak the tool protocol at all, the notice above is as far as Aegis goes on
  its own — but since P53.6 you can opt into a fallback instead of settling for a prose-only session.
  Set `provider.tool_call_shim: on` and the tool schemas move out of the request's tools field and
  into the system prompt, where the model calls a tool by writing:

  ```
  <tool_call>
  {"name": "read_file", "arguments": {"path": "main.go"}}
  </tool_call>
  ```

  which Aegis parses back into a real tool call. This is what recovers the 14-27B class that claims
  tool support in its manifest and can't deliver it.

  Three things to know before turning it on:

  - **It is explicit-only, and off by default.** A shim that quietly turns prose into executable tool
    calls is a security surface, so nothing engages it automatically — not a failed probe, not a low
    conformance rate. Turn it on for a model you have checked (`aegis doctor`).
  - **Parsed calls are not privileged.** They go through the same permission gate, capability check,
    hooks, and workspace confinement as native ones; by the time a call is dispatched the engine
    cannot tell how it arrived, and there is deliberately no separate path for shimmed calls.
  - **The parser declines rather than repairs.** A malformed attempt — fenced-off JSON is tolerated,
    but a truncated object, two objects in one tag, an unknown tool name, or non-object arguments is
    not — executes nothing and earns a corrective naming the reason, bounded to two per run. A parser
    that repairs is a parser that can invent a call the model never made, with real side effects
    behind it. The cost is that a model which can't follow the prompt format either will waste a
    couple of turns before falling back to a prose answer.
  - **A mixed round is declined too** (P59.6). A model that makes one call properly and *prints*
    another in the same reply is the common shape for this size class — "can it produce the protocol"
    and "how often does it" are different questions. Both the parse and the notice used to run only
    on turns with no tool calls at all, so that reply had its printed call silently dropped and the
    model went on to reason about a result that never existed. Now the properly-made calls run, the
    printed one does not, and the model is told so explicitly (bounded by the same two-per-run
    correction budget). Dispatching both would be defensible — parsed calls are unprivileged either
    way — but a turn written half in each dialect is genuinely ambiguous about intent, and running
    both halves would double-execute a model that wrote one call twice.

  Grammar-constrained decoding (Ollama structured outputs, llama.cpp GBNF) is a *different* answer to
  a *different* problem — it helps models that do speak the protocol but malform their arguments —
  and is not part of this.
- If a model diagnoses correctly but doesn't act on it (the `qwythos:latest` pattern above), a more
  directive follow-up prompt ("now call `edit_file` to apply the fix") often unsticks it — and as of
  P28.3, the engine does this automatically: when the first response to a plainly actionable request
  produces zero tool calls, it nudges the model to reconsider and act (one retry by default,
  configurable via `provider.zero_tool_nudge`; negative disables it) before accepting a text-only
  turn as done.

### LM Studio

```yaml
provider:
  default: openai
  base_url: "http://localhost:1234/v1"
  model: "lmstudio-community/Meta-Llama-3-8B-Instruct-GGUF"
  max_tokens: 8192
```

Download from [lmstudio.ai](https://lmstudio.ai). Load a model, then start the local server from the Local Server tab.

### llama.cpp

```yaml
provider:
  default: openai
  base_url: "http://localhost:8080/v1"
  model: "my-model"
  max_tokens: 4096
```

```bash
llama-server -m model.gguf --port 8080
```

### vLLM

```yaml
provider:
  default: openai
  base_url: "http://localhost:8000/v1"
  model: "meta-llama/Llama-3.1-8B-Instruct"
  max_tokens: 8192
```

```bash
pip install vllm
vllm serve meta-llama/Llama-3.1-8B-Instruct
```

### LocalAI

```yaml
provider:
  default: openai
  base_url: "http://localhost:8080/v1"
  model: "llama3"
  max_tokens: 4096
```

```bash
docker run -p 8080:8080 localai/localai
```

### Jan

```yaml
provider:
  default: openai
  base_url: "http://localhost:1337/v1"
  model: "llama3-8b-instruct"
  max_tokens: 8192
```

Download from [jan.ai](https://jan.ai). Enable the API server in Settings → Advanced.

### LiteLLM

```yaml
provider:
  default: openai
  base_url: "http://localhost:4000/v1"
  model: "ollama/llama3.2"
  max_tokens: 8192
```

```bash
pip install litellm
litellm --model ollama/llama3.2
```

### KoboldCpp

```yaml
provider:
  default: openai
  base_url: "http://localhost:5001/v1"
  model: "koboldcpp"
  max_tokens: 4096
```

```bash
koboldcpp model.gguf --port 5001
```

### text-generation-webui

```yaml
provider:
  default: openai
  base_url: "http://localhost:5000/v1"
  model: "my-model"
  max_tokens: 4096
```

Enable the OpenAI-compatible API extension in the webui.

---

## Cloud Providers

### Anthropic (Claude)

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  max_tokens: 16384
```

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

**Available models (check Anthropic docs for current IDs):**
- `claude-opus-4-8` — Most capable; best for complex reasoning
- `claude-sonnet-4-6` — Balanced performance and cost
- `claude-haiku-4-5-20251001` — Fast and cheap; good for `small_model`

**Anthropic-specific features:**
- Extended thinking (`provider.think: true`)
- Prompt caching (automatic; cache hit rate shown in TUI sidebar)
- Vision (image input via `@image:<path>`)
- The `small_model` fallback is particularly useful with Haiku for fast compaction

### OpenAI

```yaml
provider:
  default: openai
  model: "gpt-4o"
  max_tokens: 16384
```

```bash
export OPENAI_API_KEY="sk-..."
```

**Available models:**
- `gpt-4o` — Balanced; good tool use
- `o1`, `o3` — Reasoning models (use `reasoning_effort` config)
- `gpt-4.1` — Latest GPT-4 series

For o1/o3 models:
```yaml
provider:
  default: openai
  model: "o1"
  reasoning_effort: "medium"   # "low", "medium", "high"
```

### Azure OpenAI

```yaml
provider:
  default: openai
  base_url: "https://your-resource.openai.azure.com/openai/deployments/gpt-4o"
  model: "gpt-4o"
  max_tokens: 16384
  headers:
    api-version: "2024-02-01"
```

```bash
export OPENAI_API_KEY="your-azure-api-key"
```

### Groq

```yaml
provider:
  default: openai
  base_url: "https://api.groq.com/openai/v1"
  model: "llama-3.3-70b-versatile"
  max_tokens: 8192
```

```bash
export OPENAI_API_KEY="gsk_..."   # or GROQ_API_KEY
```

### OpenRouter

```yaml
provider:
  default: openai
  base_url: "https://openrouter.ai/api/v1"
  model: "anthropic/claude-opus-4-8"
  max_tokens: 8192
  headers:
    HTTP-Referer: "https://your-site.com"
```

```bash
export OPENAI_API_KEY="sk-or-..."   # or OPENROUTER_API_KEY
```

OpenRouter model IDs are in `vendor/model` format (e.g., `anthropic/claude-opus-4-8`, `meta-llama/llama-3.1-70b-instruct`).

### Vertex AI

```yaml
provider:
  default: openai
  base_url: "https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT/locations/us-central1/endpoints/openapi"
  model: "google/gemini-1.5-pro"
  max_tokens: 8192
```

---

## Model Selection

### Auto-discovery

When `model: "auto"` is set and `base_url` points to Ollama, Aegis probes the server at startup and picks the first available model. This lets you pull a new model and switch to it without editing config.

```yaml
provider:
  model: "auto"
```

### Manual selection

```yaml
provider:
  model: "qwen2.5:32b"
```

### Via environment variable

```bash
export AEGIS_PROVIDER_MODEL="llama3.2"
```

### Via the `/config` wizard

Run `/config` inside the TUI:
1. Select provider
2. Set base URL
3. Pick from discovered local models (or enter any model ID)
4. Set max tokens
5. Set thinking mode

Changes are written to `config.yaml` and take effect on the next restart.

### `aegis models`

```bash
aegis models              # curated model catalog with recommendations
aegis models --local      # also probe localhost for running servers
aegis models --recommend  # detect this machine's hardware and narrow local recommendations
```

`--recommend` (P20.3) detects CPU core count and total system RAM
(`internal/hwinfo`) and narrows the `local` tier of the catalog to the
entries a reasonable rule of thumb says will run without heavy swapping —
see `modelcatalog.RecommendLocal`'s doc comment for the exact RAM
thresholds. This deliberately does **not** attempt GPU/VRAM detection: Ollama
doesn't expose the concurrency/VRAM budget it computes internally over its
own API, so reimplementing that heuristic blind from a fragile,
platform-specific proxy signal (`nvidia-smi`, vendor driver queries, etc.)
was already evaluated and rejected for adaptive sub-agent concurrency
(P17.5) and is rejected here for the same reason. RAM detection itself is
best-effort and platform-specific (`/proc/meminfo` on Linux, `sysctl
hw.memsize` on macOS, the Win32 `GlobalMemoryStatusEx` API on Windows) and
fails soft to "unknown" — on an unsupported platform or in a sandboxed
environment without the usual introspection paths, `--recommend` falls back
to printing the full local catalog unnarrowed rather than erroring.

For each recommended local model not already pulled (cross-checked against
`aegis models --local`'s Ollama probe), `--recommend` prints the exact
`ollama pull <model>` command as a suggestion. It never runs the pull
itself — this is a printed suggestion you run yourself, the same
guarded-suggestion posture as the `security_advise` tool.

The TUI's `/models` picker shows the same hardware-fit information as a
badge on each local-tier entry ("fits your ~16GB RAM" / "wants ~16GB RAM
(you have ~8GB)") whenever RAM detection succeeds.

---

## Extended Thinking

Extended thinking lets the model reason before answering, producing more accurate multi-step results.

**Anthropic Claude:**
```yaml
provider:
  think: true
  max_tokens: 16384   # thinking uses up to half of max_tokens (minimum 1024)
```

Thinking blocks are streamed as dim `✻ thinking` in the TUI. They are preserved in conversation history so multi-step tool use validates correctly.

**Local reasoning models (qwen3, deepseek-r1 via Ollama):**
```yaml
provider:
  think: true
```

The same `think: true` setting toggles the local model's reasoning mode.

**Toggle in the wizard:**
Run `/config` → Step 5 → select Auto/Enabled/Disabled.

---

## Small Model

Configure a fast, cheap secondary model for context compaction and quick operations:

```yaml
provider:
  model: "claude-opus-4-8"
  small_model: "claude-haiku-4-5-20251001"   # used for compaction summaries,
                                             # session titles, and output-guard
                                             # verdict calls
```

If `small_model` is empty, the main model is used for these background calls (more expensive but always available).

**Cloud vs local, for the output guard specifically (P59.5).** On Anthropic or OpenAI, `small_model` is separate remote capacity, so routing guard verdicts to it is a straight cost and latency win. On a single local Ollama server it is the same GPU. The guard fires on *every* final answer plus its corrective retries, and each call naming a model other than the resident one can evict that model and force a full cold reload on the next turn — on a 16GB-VRAM box, every post-guard turn. That churn is what the bounded `keep_alive` default exists to prevent, and it costs far more than the cheaper verdict saves.

So on a local backend Aegis runs guard verdicts on the **session's own model**, ignoring `small_model`, unless you name one explicitly:

```yaml
output_guard:
  enabled: true
  model: "llama3.2:1b"   # only if you have the VRAM to keep both resident
```

The advice about **non-thinking** models still stands wherever you do split them: a thinking model rarely satisfies the guard's strict PASS/FAIL reply contract quickly. If your session model *is* a thinking model and you have no VRAM headroom for a second, prefer leaving the guard off over paying a reload per turn.

---

## Separate Provider for Small Model

The `small_model` runs within the same provider configuration (same adapter, same API key). It cannot use a different provider.

For cost optimization with Anthropic, pairing Opus (main) with Haiku (small) is effective.

---

## AI Gateway Support

Route all model traffic through an internal gateway for audit logging, rate limiting, or policy enforcement:

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  base_url: "https://ai-gateway.internal.example.com"
  headers:
    X-Gateway-Token: "your-gateway-token"
    X-Tenant-ID: "your-tenant"
```

The gateway must proxy:
- **Anthropic:** `POST /v1/messages` (SSE)
- **OpenAI/local:** `POST /v1/chat/completions` (SSE)

---

## Cost Tracking

Token usage is tracked per turn and displayed in the TUI sidebar. The pricing catalog covers:

- Anthropic (Claude families)
- OpenAI (GPT-4o, GPT-4.1, o1, o3 families)
- Google Gemini
- Groq open models
- OpenRouter (vendor/model IDs)

Unknown models have tokens counted but contribute no dollar cost estimate.

**Budget limit:**
```yaml
cost:
  budget_usd: 5.0   # abort run if estimated spend exceeds $5
```

**Per-turn trace:**
```bash
aegis sessions trace <id>
```

Shows input/output/cache tokens and cost per turn, with session totals.

---

## Connection Retry

```yaml
provider:
  max_retries: 4   # retry transient failures (rate limits, connection drops)
```

Set to `0` to disable retries.

---

## Provider Failover

When the primary provider exhausts `max_retries`, Aegis can fail over to an ordered list of backup providers instead of aborting the run. Failover only triggers on a synchronous request failure (before any tokens have streamed) — a run that has already started streaming a response is never interrupted mid-stream to switch providers, so partial output is never lost or replayed.

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  max_retries: 4

  fallback:
    - provider: ollama
      model: "llama3.2"          # falls back to a local model if Anthropic is down
    - provider: openai
      model: "gpt-4o-mini"
      base_url: ""                # optional per-fallback base URL override
```

Each fallback entry gets its own retry budget (`max_retries`) before the chain moves to the next entry. API keys for fallback providers are read from the environment the same way as the primary (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`); a fallback missing its key is skipped with a warning rather than failing the whole chain.

**Trust boundary guard:** failing over *from* a local provider (`ollama`) *to* a cloud provider (`anthropic`, `openai`) is skipped by default — an outage shouldn't silently start sending your conversation to a cloud API. Opt in explicitly:

```yaml
provider:
  allow_cloud_fallback: true
```

Cloud→cloud and any→local failover are never gated behind this flag.

---

## Data Exposure & Redaction

Aegis streams the **full conversation** to whichever provider is configured — including the content of any file a tool has read. That content becomes part of the request body sent to `provider.Stream`, with no filtering step in between.

For a cloud provider (Anthropic, OpenAI, or any OpenAI-compatible cloud endpoint — Azure OpenAI, Groq, OpenRouter, Vertex AI, an AI gateway, etc.) that means everything a read tool surfaces — including secrets a file happens to contain, like an API key or credential left in a config file, `.env`, or log — travels over the network to that third party as part of a normal turn, unless the redaction pass below (on by default) catches and masks it first.

**Local Ollama usage avoids this exposure entirely** — nothing leaves the machine — and is the recommended mitigation when working in a sensitive codebase. See [Local LLMs](#local-llms) above.

When a cloud provider is required, Aegis runs a redaction pass **by default** (P27.3/FIND-05) as a partial mitigation. Disable it only if the cost below is unacceptable and content is otherwise known-safe:

```yaml
security:
  redact_secrets: false   # opt out; on by default
```

By default, the output of every read-capability tool call is run through the same gitleaks-backed secret detection already used to scan a PR title/body before `git_pr` opens a pull request (FIND-13) — any detected secret pattern (AWS keys, tokens, private keys, and gitleaks' other built-in rules) is masked as `[REDACTED:<rule-id>]` before that tool result is appended to the conversation sent to the model/provider.

This is a **best-effort** mitigation, not a guarantee:
- it requires the `gitleaks` binary on `PATH` — if it's missing, content passes through unredacted with no error (fail-open, same posture as the PR-scan check);
- it only catches secret shapes gitleaks' rule set recognizes — a novel or oddly-formatted credential can still slip through;
- it adds latency to every read-capability tool call, since each one shells out to gitleaks.

It never blocks a tool call — a scan failure or a detected secret both still let the (possibly redacted) result reach the model, since a read tool's result must always be available to the agent loop. Treat this as defense-in-depth alongside — not a replacement for — keeping the local-Ollama path available for genuinely sensitive work.

---

## Context Window

Aegis uses the model's context window size to decide when to compact the conversation, with a visible `⚠ context …` notice in the TUI when it happens.

The trigger is **sized against the completion**, not a flat percentage (P59.1). On Ollama `num_ctx` is one budget covering the prompt *and* the generated answer, so a prompt that merely fits is not a prompt that can be answered: the trigger is `window - min(max_tokens, window/2)` less a small margin, floored at half the window and capped at 85%. A generous window with a modest `max_tokens` therefore behaves exactly as it always did (85%), while a 4096-token window facing the default 32768 `max_tokens` compacts at half the window instead of leaving ~600 tokens to answer in.

**One trigger, not two (P66.14).** Until this landed, the engine's per-turn check and the summarizer applied *different* thresholds to the same conversation: the engine used the sized trigger above, while the summarizer used a flat 20%-free rule that never saw `max_tokens`. At a 4096-token window the engine asked for a compaction at 2,048 estimated tokens and the summarizer refused until 3,277 — so summarization finally happened with 819 tokens left for a completion the request had asked 32,768 for, and every turn in between paid a deterministic prune that freed almost nothing. Both gates now read `tokenest.CompactionTrigger`, and the engine passes the number it actually used down to the compactor so the two cannot drift. Above ~133k tokens the shared trigger is the 85% ceiling, which is marginally *later* than the summarizer's old 80% — the engine's gate is the one sized against the completion, so it wins.

**Token-estimate calibration reaches the compat path (P66.14).** The correction Aegis learns from each turn's reported prompt size (P62.4) used to be gated on a telemetry field only the *native* Ollama adapter sets, so it never fired on `provider.default: openai` with an `:11434/v1` base_url — the configuration this page itself describes below. Every session on that setup ran on the uncorrected estimate, which undercounts by 20-33%. The gate is now a positive identification of the backend, so both adapters calibrate.

**Ollama auto-detection.** Ollama's OpenAI-compatible endpoint gives no way to set or read `num_ctx`, and when a prompt exceeds the served context Ollama **silently drops the oldest tokens — the system prompt and task instructions go first**, which is why a long agent task on a local model can "forget" what it was doing and stop without output. To close that gap, when the provider is `ollama` — or an `openai` provider whose `base_url` points at an Ollama server — the daemon queries Ollama's native API for the context actually being served, in order of authority:

1. the loaded model's real allocation (`/api/ps`) — re-checked after each run until known, since the first run is what loads the model;
2. a modelfile-pinned `num_ctx` (`/api/show`);
3. Ollama's server default (4096), capped by the model's training context.

The detected value drives compaction, the engine's proactive per-turn check, and the TUI's context-usage bar; `/status` shows the number and where it came from.

**Mid-run escalation.** A phased skill drive that overflows raises the served `num_ctx` toward the model's maximum rather than aborting (P47.5b). That raise is a runtime *floor* on the adapter — it outranks the per-request window computed before the run — and the engine re-reads it before every turn (P59.7), so a phase that just gained room stops compacting as if it hadn't. Before this, the compaction trigger kept measuring against the pre-escalation number and burned summarizer calls, on a local model, in the middle of the overflow recovery that raised the window. The summarizer's own budget deliberately stays at the sized window: the extra `num_ctx` buys physical headroom against a transient overshoot, and spending it on the recovery rather than the work would defeat the point.

**Raising the served context** happens on the Ollama side, not in Aegis — Ollama sizes its KV cache from available (V)RAM, so this is where "how much memory the system has" actually enters the picture:

```bash
OLLAMA_CONTEXT_LENGTH=32768 ollama serve   # server-wide
# or pin per model in a Modelfile:  PARAMETER num_ctx 32768
```

For agent workloads (threat modeling, deep research, multi-step tool use), 16k–32k is a realistic minimum; at the 4096 default, a handful of file reads fills the window.

**Manual override:**

```yaml
provider:
  context_window: 32768   # set manually in tokens
```

A non-zero value overrides detection, with one exception: if Ollama verifiably serves *less* than the configured value, the served value wins (and a warning is logged) — honoring the larger number would just reintroduce silent truncation. `0` or missing means auto-detect; if detection fails entirely (Ollama unreachable), compaction is skipped for local models until a later run detects successfully.

### Keep-alive and pre-warm

Ollama unloads a model from memory after five idle minutes (its own default `keep_alive`); the next message then pays a full cold reload (tens of seconds on a 16GB machine). Worse, once the model unloads, Ollama's automatic KV-cache reuse is lost — so a multi-turn agentic run whose per-turn cost outlasts that idle window sees the model reload *and* reprocess the whole conversation from scratch each turn (P35.4). Two independent controls address this:

**Keep the model resident.** Set how long Ollama holds the model after each request. Only the native `ollama` adapter (`provider.default: ollama`) sends this — the OpenAI-compat path cannot.

```yaml
provider:
  default: ollama
  keep_alive: "30m"   # a Go duration, or an integer number of seconds
```

`""`/missing (the **default**) substitutes a bounded resident window of **30m** on the native path, so an agentic run stays loaded across the gaps between turns and reuses its KV cache instead of reprocessing every turn — while still unloading once a run goes genuinely idle, so RAM is held only during active work. `"0"` unloads immediately (falling back to Ollama's per-request behavior); `"-1"` pins the model in memory forever — never the default, because a pinned model competes with everything else on a RAM-constrained machine. Set `"-1"` only if you have the memory to spare and want zero reloads.

**Pre-warm (automatic, no config).** When the provider points at Ollama, the TUI fires a background warm-up load the moment you regain focus or start typing a new message — but only if `/api/ps` reports the model is *not* already loaded, so an in-use model is never re-pinged. This overlaps the cold reload with your typing instead of stalling the send, and it does **not** change the residency policy (the warm-up omits `keep_alive`, so the model unloads on the schedule set by `keep_alive` above).

### Response-header timeout

Every provider adapter shares one HTTP client whose transport bounds only the wait for the response *headers* (not the streamed body that follows) at **5 minutes** by default. Ollama withholds the response header until prompt-eval (prefill) finishes, so on a large local context a legitimately-slow prefill can exceed that window — the whole turn then aborts as a transport error (`net/http: timeout awaiting response headers`) before any content streams, even though the model was still working (P35.5).

```yaml
provider:
  response_header_timeout: 900   # seconds; 0 or missing = default (5 minutes)
```

Raise this if you run large local contexts (`context_window` in the tens of thousands of tokens) on a slower box and see the run die mid-turn with that error. `0` or missing keeps the default, which P38.1 raised from 5 minutes to **30 minutes** after the built-in threat-model drive aborted at the 5m ceiling reading a 2845-line file on a local 35B model.

### Stream idle timeout

The header timeout above stops applying the moment the headers arrive, and the streaming client deliberately has **no** overall timeout — one would cap how long a turn may legitimately stream and kill a slow local model mid-answer as a transport error. That left one window unwatched: the gap *between* streamed chunks. An Ollama runner that wedges mid-generation leaves the engine blocked on a read indefinitely, and `cost.max_wall_clock_per_run` cannot help, because it is checked before each model turn and before each tool round — never inside a turn (P59.2).

```yaml
provider:
  stream_idle_timeout: 600   # seconds; 0 or missing = default (10 minutes), negative disables
```

It applies to **all three adapters** — anthropic, openai and the native ollama one. Until P61.1 it reached only the last of those, which mattered more than the name suggests: the openai adapter is also a local path (it is what talks to Ollama's OpenAI-compat `/v1` endpoint), so the backend most likely to wedge was only half covered by a key that reads as global.

The bound **resets on every chunk**, so it measures "the server has sent nothing at all for this long" rather than the length of the response — a model emitting at 7 tok/s never approaches it. Prefill happens before the headers on Ollama, so every gap this sees is an inter-token gap. A trip is surfaced as a transport error, which means the phased drive's existing wait-for-backend/resume-from-disk path (built for a *crashed* server) handles a *wedged* one too, with no separate recovery machinery.

That recovery has a second requirement beyond classifying the error: the drive has to be able to ask the backend "are you back yet?", so the adapter needs a liveness probe. **Both local paths have one** — the native Ollama adapter probes `GET /api/version`, and the openai adapter probes `GET <base_url>/models`, the OpenAI-compatible equivalent (side-effect-free on either: nothing is loaded, unloaded or billed). The openai one matters because that adapter is also what talks to Ollama's `/v1` endpoint; without it a `/v1` backend that died was correctly *classified* and the drive still aborted instead of waiting. Because the question is liveness rather than usability, any answer from the server counts as alive — a `401` from a gateway that wants a key, or a `404` from a backend that routes completions but not `/models`, both prove a server is there; only `502`/`503`/`504`, where a proxy is reporting that the *upstream* model server is gone, count as down. The cloud (anthropic) adapter deliberately has no probe: there is no local server to wait for, and a transient remote outage is already the retry decorator's job.

Two related behaviors close the same gap from the other side:

- `cost.max_wall_clock_per_run`, when set, is now also a deadline on the run's context rather than only a value polled between turns — so it can end a turn it is already inside. The abort is still reported as a wall-clock error, never as an interrupt or a transport failure a caller would try to resume from.
- A stream that ends **without** a completion chunk (`done: true`) is reported as a truncation instead of a finished turn (P59.3). Previously a body closed cleanly mid-generation produced no read error, so the answer surfaced as a complete short response whose stop reason claimed the model chose to end its turn.

### Concurrency against a local backend

Nothing in the harness used to bound how many requests were in flight against one model server. The daemon serves concurrent sessions, swarm spawns sub-agents, and the output guard, compaction and title passes are requests of their own. Against a cloud endpoint that is fine — the provider fans them out across its fleet. Against one Ollama server every one of those requests is a claim on the same GPU (P59.9).

**What this costs was measured, and the measurement corrected the original reasoning.** This section used to say the hazard was correctness: that each request is built believing it owns the full detected `num_ctx` while Ollama splits its KV cache across `OLLAMA_NUM_PARALLEL` slots, silently truncating. On Ollama 0.30.10 (qwen3:14b, `num_ctx` 16384, 16GB card) that does not reproduce — four concurrent ~12k-token requests, each with a passphrase in its *first* tokens, all returned it verbatim with identical `prompt_eval_count` and no failures. Nothing was truncated or evicted. The real cost is latency, and the throughput gain is well short of linear:

| in flight | aggregate | p50 turn latency |
|---|---|---|
| 1 | 11.2 tok/s | 5.7s |
| 2 | 15.6 tok/s (1.40x) | 6.7s |
| 4 | 17.9 tok/s (1.60x) | 9.8s (max 14.3s) |

```yaml
provider:
  max_concurrent_requests: 0   # 0 = auto (local: 1, cloud: unbounded); n = at most n; negative = unbounded
```

The default bounds local backends at 1, because a second in-flight request buys ~40% aggregate throughput and pays ~70% worse turn latency for it — a bad trade when one turn's latency is what you are waiting on, and a reasonable one when you are running a batch of independent sub-agents. "Local" means the native Ollama adapter or any OpenAI-compatible adapter pointed at a loopback `base_url` (LM Studio, llama.cpp, a local proxy). **Raising it is safe**: prefix-cache reuse survives concurrency intact (continuations of a shared conversation held 29–47ms prefills at every depth measured), so you give up latency headroom and nothing else. Set a negative value to opt out entirely.

Two properties worth knowing:

- The slot is held for the **whole life of a stream**, not just until the request is accepted, because that is how long the request occupies the model. A cancelled run releases its slot immediately rather than holding it behind an abandoned reader.
- It sits **inside** the retry decorator, so a retry's backoff sleep does not sit on a slot a queued caller could be using, and it is applied **per backend**, so a local primary with a cloud fallback does not hand the cloud its single-GPU queue depth.

This deliberately does **not** detect VRAM or infer a capacity — a queue depth is a policy you set, not something the harness guesses (the same conclusion P20.3 and P17.5 reached). It also does not replace `swarm.AdaptiveLimiter`, which bounds *spawns* by observing measured speedup: that is the right instrument for "is this host CPU-bound" and the wrong one for a fixed VRAM budget. With the backend no longer thrashing, the limiter is left bounding spawn setup cost, which is what it is good at.
