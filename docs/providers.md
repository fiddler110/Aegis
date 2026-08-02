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
  contains tool-call-shaped JSON naming a real tool but made no actual tool call, it says so. This
  costs nothing and needs no probe, so it also covers `aegis chat`, which runs its own in-process
  engine and never touches the daemon's run path.
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

If `small_model` is empty, the main model is used for these background calls (more expensive but always available). For local/Ollama setups, set `small_model` to a fast **non-thinking** model before enabling the output guard — a thinking model rarely satisfies the guard's strict PASS/FAIL reply contract quickly.

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

Aegis uses the model's context window size to decide when to compact the conversation (proactively at 85% fill, with a visible `⚠ context …` notice in the TUI when it happens).

**Ollama auto-detection.** Ollama's OpenAI-compatible endpoint gives no way to set or read `num_ctx`, and when a prompt exceeds the served context Ollama **silently drops the oldest tokens — the system prompt and task instructions go first**, which is why a long agent task on a local model can "forget" what it was doing and stop without output. To close that gap, when the provider is `ollama` — or an `openai` provider whose `base_url` points at an Ollama server — the daemon queries Ollama's native API for the context actually being served, in order of authority:

1. the loaded model's real allocation (`/api/ps`) — re-checked after each run until known, since the first run is what loads the model;
2. a modelfile-pinned `num_ctx` (`/api/show`);
3. Ollama's server default (4096), capped by the model's training context.

The detected value drives compaction, the engine's proactive per-turn check, and the TUI's context-usage bar; `/status` shows the number and where it came from.

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

Raise this if you run large local contexts (`context_window` in the tens of thousands of tokens) on a slower box and see the run die mid-turn with that error. `0` or missing keeps the previous hardcoded 5-minute behavior — no change unless you opt in.
