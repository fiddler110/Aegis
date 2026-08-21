# Configuration Reference

Aegis uses a layered configuration system. Values are resolved in this order (highest wins):

```
environment variables
  > project config (.aegis/config.yaml)
    > global config (~/.config/aegis/config.yaml)
      > built-in defaults
```

`.aegis/.env` is not a config layer. It supplies *secrets* to the process
environment (trusted workspaces only) and cannot set `AEGIS_*` — see
[The `.aegis/.env` File](#the-aegisenv-file).

**API keys are always read from environment variables** — never from config files.

---

## Config File Locations

| File | Purpose |
|------|---------|
| `~/.config/aegis/config.yaml` (macOS/Linux) | Global config — applies to all projects |
| `%AppData%\aegis\config.yaml` (Windows) | Global config on Windows |
| `.aegis/config.yaml` | Project-level overrides (safe to commit) |
| `.aegis/.env` | Local secrets — add to `.gitignore`; read only in a trusted workspace |

Generate files with:

```bash
aegis --first-init   # global config with full template
aegis --init         # project config (.aegis/config.yaml)
```

Both abort if the target file already exists. To regenerate an existing config
from the latest template — e.g. after upgrading Aegis and picking up new
template sections — add `--overwrite`; the old file is backed up first
(`config.yaml.bak-<unix-timestamp>`) before being replaced:

```bash
aegis --first-init --overwrite
aegis --init --overwrite
```

This fully replaces the file, discarding any customizations — use `aegis
config update` instead if you want to merge in new fields while keeping your
existing edits (see below).

---

## Environment Variables

Any config key can be overridden with an environment variable by converting the YAML path to uppercase with underscores, prefixed with `AEGIS_`:

| Variable | Config key | Example |
|----------|-----------|---------|
| `AEGIS_PROVIDER_DEFAULT` | `provider.default` | `anthropic` |
| `AEGIS_PROVIDER_MODEL` | `provider.model` | `claude-opus-4-8` |
| `AEGIS_PROVIDER_BASE_URL` | `provider.base_url` | `http://localhost:11434/v1` |
| `AEGIS_PROVIDER_MAX_TOKENS` | `provider.max_tokens` | `16384` |
| `AEGIS_PERMISSION_MODE` | `permission.mode` | `plan` |
| `AEGIS_COST_BUDGET_USD` | `cost.budget_usd` | `5.0` |
| `AEGIS_LOG_LEVEL` | `log_level` | `debug` |
| `AEGIS_SERVER_ADDR` | `server.addr` | `127.0.0.1:4127` |
| `AEGIS_SERVER_MAX_CONCURRENT_RUNS` | `server.max_concurrent_runs` | `10` |
| `AEGIS_SERVER_MAX_RUN_DURATION_SEC` | `server.max_run_duration_sec` | `1800` |
| `AEGIS_SERVER_SSE_BUFFER_SIZE` | `server.sse_buffer_size` | `256` |
| `AEGIS_SERVER_TLS_ENABLED` | `server.tls.enabled` | `true` |
| `AEGIS_REPOMAP_MAX_BYTES` | `repomap.max_bytes` | `24000` |
| `AEGIS_REPOMAP_MAX_SYMBOLS_PER_FILE` | `repomap.max_symbols_per_file` | `6` |

API keys use their native names (not the `AEGIS_` prefix):

| Variable | Provider |
|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic Claude |
| `OPENAI_API_KEY` | OpenAI or any local LLM |
| `GROQ_API_KEY` | Groq |
| `OPENROUTER_API_KEY` | OpenRouter |

Groq and OpenRouter aren't distinct `provider.default` values — both are reached with
`provider.default: openai` plus a `base_url` pointing at their OpenAI-compatible endpoint (see
docs/providers.md). `ProviderAPIKey` checks `OPENAI_API_KEY` first, then falls back to
`GROQ_API_KEY`, then `OPENROUTER_API_KEY` — so either the shared `OPENAI_API_KEY` or the
provider-specific var works.

---

## Full Config Reference

```yaml
# ── Provider ──────────────────────────────────────────────────────────────────
provider:
  # "anthropic" or "openai"
  # Use "openai" for ALL local LLMs and OpenAI-compatible cloud providers.
  default: openai

  # Model ID. "auto" or "" picks the first available Ollama model at startup.
  # Set any explicit ID: "llama3.2", "claude-opus-4-8", "gpt-4o", etc.
  model: "auto"

  # Required for local LLMs and API gateways. Leave empty for direct cloud calls.
  # Ollama:    http://localhost:11434/v1
  # LM Studio: http://localhost:1234/v1
  # llama.cpp: http://localhost:8080/v1
  # vLLM:      http://localhost:8000/v1
  # LiteLLM:   http://localhost:4000/v1
  base_url: "http://localhost:11434/v1"

  # Optional fast model for titles, context compaction, and output-guard
  # verdict calls. Falls back to `model` if empty.
  small_model: ""

  # P9.4: opt-in per-turn model routing for user-facing agent turns. When
  # true AND small_model is set, each turn is classified by a purely local
  # heuristic (message length, code fences, multi-step lists, sentence
  # count, and whether the session already made tool calls this turn) as
  # "simple" or "complex". Simple turns run on small_model; everything else
  # — including any turn where the classifier is unsure — stays on `model`.
  # No effect unless small_model is also set, and never overrides an
  # explicit per-session /model pin (P14.7), which always wins. Off by
  # default; existing setups see no behavior change.
  task_routing: false

  # Maximum tokens in the model's response. Capped by the model's own limits.
  #
  # On Ollama this shares ONE budget with the prompt: num_ctx covers both, so a
  # max_tokens at or above context_window is a request for more output than the
  # whole conversation is allowed to occupy. Aegis reconciles the pair at two
  # points (P59.1) — proactive compaction reserves room for the completion
  # rather than triggering at a flat 85%, and the native adapter clamps the
  # per-request num_predict to the headroom the prompt actually leaves — but
  # neither makes a badly-sized pair a good idea. `aegis doctor` reports it
  # under "generation budget". Cloud providers bill max_tokens against a
  # separate output allowance, where a large value is correct.
  max_tokens: 8192

  # Retry count for transient failures (connection errors, rate limits).
  # 0 disables retries.
  max_retries: 4

  # Sampling controls. Both are unset by default, which leaves the backend to
  # decide — and for a local model that usually means Ollama's default of 0.8,
  # since most Modelfiles pin no temperature. That is a poor fit for agentic
  # work: a tool-dispatching run is a decision process, not a creative one, and
  # two runs of the same prompt visibly diverge. In a measured pair of
  # threat-model runs (identical prompt, model and repo) one opened by writing
  # a file and the other by running an unprompted web search.
  #
  # Set temperature: 0 when you want a run to behave the same way twice, and
  # add a seed to pin the sampler's RNG on top of it. A seed alone does not
  # give determinism — it fixes *which* of many paths is taken, not that there
  # is only one — so set both or neither.
  #
  # temperature: 0
  # seed: 42

  # Maximum agent-loop iterations per run. 0 uses the built-in default (40).
  max_iterations: 0

  # Abort a run when the same turn is produced N times in a row.
  # 0 uses the built-in default (5).
  loop_threshold: 0

  # Bounds the corrective-nudge retries fired when a turn that plainly reads
  # as actionable produces zero tool calls (a model dumping its reasoning as
  # prose instead of calling a tool). 0 uses the built-in default (1 retry);
  # negative disables the nudge entirely.
  zero_tool_nudge: 0

  # Extra HTTP headers on every request to the provider. Useful for gateway auth.
  headers:
    X-Gateway-Token: "your-token"
    X-Tenant-ID: "your-tenant"

  # Extended thinking.
  # null/~ = provider default (Anthropic disables by default; local varies)
  # true   = enable (Anthropic extended thinking; local reasoning models)
  # false  = disable explicitly
  think: ~

  # OpenAI o1/o3 reasoning effort: "low", "medium", or "high".
  # Only applies to o-series models; ignored otherwise.
  reasoning_effort: ""

  # Model context window in tokens. 0 = auto-detect: for Ollama (or an openai
  # provider whose base_url points at an Ollama server) the daemon queries the
  # native Ollama API for the context actually being served — the loaded
  # model's allocation, a modelfile num_ctx, or Ollama's 4096 default — and
  # uses it for compaction thresholds and the TUI usage bar; if Ollama is
  # unreachable at startup, detection retries after each run. A non-zero value
  # overrides detection, except that a *verified smaller* served window wins
  # (Ollama silently truncates prompts beyond it, so trusting a larger
  # configured value breaks long runs). /status shows the value in use.
  context_window: 0

  # How much memory, in GiB, Aegis may assume the model server can hold across
  # EVERY concurrently resident model (P69.6). 0 = unset, which means no
  # resident-set planning at all: without it, each model is sized as if it owned
  # the card, which is wrong the moment a debate puts a second model beside it.
  #
  # This is a figure you state, never one Aegis detects — no GPU/VRAM
  # introspection is attempted on any platform (P17.5). It is also not the card's
  # capacity: subtract the driver reserve and whatever your desktop compositor
  # and browser hold. ~14.5 of a 16 GB card is the measured figure on the machine
  # this was calibrated against.
  #
  # With it set, a debate plans its seats as one resident set, installs the
  # resulting windows for the debate's duration, and restores them afterwards —
  # or refuses before spending a turn when no assignment fits. Preview any plan
  # with `aegis models --fit-debate`. Only meaningful for a local Ollama backend.
  vram_budget_gb: 0

  # Let the daemon solve context_window from vram_budget_gb instead of serving
  # the number above (P72.1). Off by default, and the fit runs *without* it
  # whenever context_window is unset — this flag answers the other case: a
  # context_window you set on purpose (a debate topology pin, a figure worked out
  # by hand) is never replaced silently, so replacing it is a separate yes.
  #
  # What it does at startup: load the default model (and small_model, planned as
  # one co-resident set) so their weights can be measured, solve for the largest
  # windows that fit the budget, reload at those windows, and check Ollama's own
  # size/size_vram split to confirm nothing spilled. A model a session later
  # switches to with /model joins the set and the whole set is re-planned after
  # that turn. Nothing is written back to config.yaml: the fitted window is
  # effective for the daemon's lifetime and reported by /status as the source
  # "fit:vram-budget". `aegis models --fit` prints the same arithmetic without
  # applying it, and `--fit --write` is still the only thing that edits config.
  #
  # Only meaningful for the native ollama adapter; the /v1 compat path cannot
  # send num_ctx, so a fitted window would be a number the server never receives.
  autofit_context: false

  # The element type Ollama stores K and V in — its OLLAMA_KV_CACHE_TYPE.
  # "f16" (default), "q8_0" (roughly half the cache) or "q4_0" (roughly a
  # quarter). Ollama does not report this over its API, so it is a declaration
  # you keep in sync with the server: get it wrong and every fitted window is
  # planned against the wrong number of bytes. A wrong declaration is caught
  # empirically — Ollama's own size/size_vram split shows the model spilled to
  # system RAM — rather than silently believed.
  kv_cache_type: f16

  # How long a streamed request waits for the response headers (not the
  # streamed body that follows), in seconds. 0 = default (5 minutes) — the
  # previously-hardcoded value, unchanged unless you opt in. Ollama withholds
  # the response header until prompt-eval (prefill) finishes, so a large local
  # context can legitimately need longer than the default; raise this rather
  # than lower context_window if a run dies mid-turn with
  # "timeout awaiting response headers" (P35.5). See docs/providers.md#response-header-timeout.
  response_header_timeout: 0

  # How long a streamed response may go with NO chunk at all before the request
  # is abandoned, in seconds (P59.2). response_header_timeout above stops
  # applying the moment the headers arrive and the streaming client has no
  # overall timeout (one would cap a legitimately long turn), so before this key
  # a model runner that wedged mid-generation left the turn blocked forever —
  # and cost.max_wall_clock_per_run could not help, since it is checked between
  # turns, never inside one. The bound resets on every chunk, so a slow but
  # progressing model is never cut off. A trip is reported as a transport
  # error, which routes into the same wait-and-resume path a crashed server
  # takes. 0 = default (10 minutes); negative disables the bound entirely.
  stream_idle_timeout: 0

  # How many requests may be in flight against the backend at once (P59.9);
  # the rest queue inside the adapter until a slot frees. This is admission
  # control, not a rate limit: the slot is held for the whole life of a
  # stream, because that is how long the request occupies the model.
  #
  #   0 (unset) — auto: a LOCAL backend gets 1, a cloud backend is unbounded
  #   n > 0     — at most n concurrent requests, whatever the backend
  #   negative  — explicitly unbounded, local included
  #
  # "Local" means the native ollama adapter, or any OpenAI-compatible adapter
  # pointed at a loopback base_url (LM Studio, llama.cpp, a local proxy).
  # Those are bounded by default because a local server is ONE GPU: every
  # concurrent request is built believing it owns the full detected num_ctx,
  # while Ollama splits its KV cache across OLLAMA_NUM_PARALLEL slots and
  # evicts models to fit. Two concurrent requests do not get two GPUs — they
  # get one GPU serving two smaller windows, which is the truncation
  # (context) and eviction (stall) this bound exists to prevent. Queueing is
  # honest there; oversubscribing is not. Cloud endpoints fan out across a
  # fleet, so bounding them by default would only slow multi-agent work down.
  #
  # It applies at the adapter layer, so every caller in the daemon passes
  # through it — sessions, in-process swarm agents, the phased drive, the
  # output guard, compaction. It does NOT bound a separate process (the
  # subprocess swarm worker builds its own adapter).
  #
  # Aegis deliberately does not detect VRAM or infer a capacity here. This is
  # a policy you set; raise it if your host genuinely has room (a big card,
  # OLLAMA_NUM_PARALLEL tuned to match).
  max_concurrent_requests: 0

  # Ordered (provider, model) pairs tried in sequence after the primary
  # adapter exhausts max_retries (P5.9). Empty = no failover.
  fallback: []
  #   - provider: ollama
  #     model: "llama3.2"
  #   - provider: openai
  #     model: "gpt-4o-mini"
  #     base_url: ""      # optional per-fallback API base override

  # Required to fail over FROM a local provider (ollama) TO a cloud provider
  # (anthropic, openai) — guards against a local-only session silently
  # sending data off the machine on an outage. Cloud-to-cloud and any-to-local
  # failover are always allowed and never need this flag.
  allow_cloud_fallback: false

  # Selects the system-prompt/tool-exposure shape (P25.6). "auto" (default)
  # infers from base_url: loopback/localhost gets the "local" profile
  # (trimmed prompt, web_search/web_fetch/security_scan/git_pr deferred, repo
  # map capped) tuned for small local models; any other base_url gets the
  # unchanged "default" profile. Set "local" or "default" to force the choice
  # regardless of base_url.
  prompt_profile: auto

  # How many times the tool-calling smoke probe runs when measuring a local
  # model's conformance *rate* rather than a yes/no verdict (P53.4). Local
  # (Ollama-style) providers only. `aegis doctor` runs the whole sample inline
  # (and announces the trial count before it starts); the daemon blocks on only
  # the first trial and refines the rest in the background, so raising this
  # never adds first-message latency. 1 = the single-trial check, unchanged.
  tool_call_probe_trials: 5

  # Pre-declared per-model capabilities (P53.5), keyed by model name. Aegis
  # normally *discovers* these — it finds out a model rejects `think` by
  # sending one and taking the 400, and finds out whether it can call tools by
  # probing — and caches what it learns in <data_dir>/model_caps.json so a
  # restart doesn't re-pay the discovery. That cache is keyed on the model's
  # content digest, so re-pulling a tag invalidates its record automatically.
  #
  # A declaration here outranks anything discovered. Use it to tell Aegis about
  # a model it has never met (so the failing request is never sent even once),
  # or to override a cached verdict without deleting the file. Unset fields
  # declare nothing and leave discovery in charge.
  model_capabilities: {}
  #   "mythos-sec:24b":
  #     think: false        # never send the think parameter to this model
  #   "some-model:latest":
  #     tool_calling: ok    # or "unsupported"

  # Per-model overrides for the harness repair behaviors (P74.17), keyed by
  # model id like model_capabilities above. Every model starts from the
  # provider-level default prompt_profile already picks — prose-tool-call
  # salvage and argument-shape repair both on for the "local" profile, both
  # off for "default" — and a named entry here corrects individual fields on
  # top of that default rather than replacing it, so naming a model to flip
  # one field leaves the other at the default instead of resetting to false.
  model_harness: {}
  #   "qwen2.5-coder:1.5b":
  #     argument_shape_repair: true    # turn it on even under the cloud default
  #   "gpt-oss:20b":
  #     prose_tool_call_salvage: false # this model's calls are already structured

  # Non-native tool-calling fallback (P53.6). "off" (default) | "on".
  #
  # For models that cannot speak the provider's tool protocol at all — the
  # class that emits `{"name": ..., "arguments": ...}` into its prose and then
  # fabricates the results it never fetched. With the shim on, the tool
  # schemas are serialized into the system prompt instead of the request's
  # tools field, and the model calls a tool by writing:
  #
  #     <tool_call>
  #     {"name": "read_file", "arguments": {"path": "main.go"}}
  #     </tool_call>
  #
  # which Aegis parses back into a real tool call. Parsed calls go through the
  # same permission gate, capability check and workspace confinement as native
  # ones — the shim changes how a call arrives, never what it may do.
  #
  # Explicit-only, and off by default, on purpose: a shim that quietly starts
  # turning prose into executable tool calls is a security surface. Turn it on
  # only for a model you know needs it (`aegis doctor` reports the verdict).
  # The parser is strict — a malformed attempt is refused and corrected, never
  # repaired into a call the model didn't make — so expect a couple of wasted
  # turns with a model that also can't follow the prompt format.
  tool_call_shim: "off"


# ── Permission ────────────────────────────────────────────────────────────────
permission:
  # "plan"  — read-only; no file writes, no shell execution
  # "build" — file edits allowed; shell/execute prompts for approval (default)
  # "auto"  — all capabilities without prompting (trusted sandboxes only)
  mode: build

  # Skip approval prompts for shell/execute calls even in build mode.
  auto_approve_exec: false

  # auto_approve_exec: true combined with an unsandboxed *local* backend means
  # every model-issued shell command runs on the host with no approval and no
  # isolation — the daemon refuses to start with that combination unless this
  # is explicitly set to true. The default sandbox.backend is "os" (P4.7
  # OS-level isolation, no container runtime needed), which is not this
  # combination on a host that can serve it; it falls back to local — and so
  # can trigger this refusal — wherever it can't (every current Windows host,
  # any macOS/Linux box missing seatbelt/bwrap). Configure a real sandbox
  # (sandbox.backend: container, or confirm "os" is actually active via
  # `aegis sandbox status`) instead of setting this unless the daemon itself
  # is already running inside an isolated environment (e.g. a CI container).
  allow_unsandboxed_auto_exec: false

  # Fine-grained allow/deny rules evaluated before the mode gate.
  # Syntax: "allow <tool>(<pattern>)" or "deny <tool>(<pattern>)"
  # <tool>: tool name, capability alias (bash, write, read, network), or *
  # <pattern>: glob matched against the call's primary input
  # deny takes precedence over allow.
  rules:
    - "allow bash(npm test*)"      # auto-approve npm test without prompting
    - "allow bash(git status)"
    - "deny write(/etc/*)"         # never write under /etc, even in auto mode
    - "deny shell(rm -rf /*)"


# ── Cost guard ────────────────────────────────────────────────────────────────
cost:
  # 0 = unlimited. Set e.g. 5.0 to abort runs that exceed $5 of estimated spend
  # within a single turn. Pricing covers Anthropic, OpenAI, Gemini, Groq,
  # OpenRouter families. Unknown models have tokens counted but no dollar cost.
  #
  # NOTE: budget_usd (and session_cap_usd/daily_cap_usd below) are silent
  # no-ops for local/Ollama models and any model absent from the pricing
  # catalog — their usage is estimated or unpriced, so it contributes $0
  # regardless of how much was actually used. Use max_tokens_per_run /
  # session_token_cap / daily_token_cap (P10.5) as the primary guardrail for a
  # local-first setup; the dollar caps remain a convenience for cloud models.
  budget_usd: 0.0

  # 0 = unlimited. Aborts a run once its cumulative token count (input +
  # output + cache, across every turn) reaches this amount. Always
  # enforceable — token counts are present even when usage was estimated or
  # the model is unpriced, unlike budget_usd.
  #
  # Read this as a *context/billing* budget, not a work budget. It sums the
  # whole prompt on every turn, which is precisely what a priced provider
  # bills you for — but Ollama's prompt_eval_count is also the full prompt
  # each turn rather than a per-turn delta, so on a local model the sum grows
  # with conversation length as well as with work done (a 20-turn run on an 8k
  # window reports ~160k tokens while the model may have generated a few
  # thousand). If what you mean is "don't do more than this much work", use
  # max_generated_tokens_per_run below.
  max_tokens_per_run: 0

  # 0 = unlimited (the default). Aborts a run once the model has *generated*
  # this many tokens — output only, with no input or cache tokens folded in
  # (P59.4). This is the work budget; max_tokens_per_run above is the context
  # budget. They are separate keys rather than one key that changes meaning by
  # provider class, so neither number is ever ambiguous about what it counted.
  #
  # Most useful on a local/Ollama setup, where budget_usd is a silent no-op and
  # max_tokens_per_run answers a question you weren't asking. Note that spawned
  # sub-agents inherit this cap whole rather than a divided share (unlike
  # budget_usd/max_tokens_per_run, which are split across teammates), so N
  # teammates can each generate up to this much.
  max_generated_tokens_per_run: 0

  # 0 = unlimited (the default). Seconds of wall-clock time a single run may
  # take before it aborts, checked before each model turn and before each tool
  # round. This is the only budget that bounds *time*: budget_usd is a no-op
  # for local models, max_tokens_per_run defaults to 0, and
  # provider.max_iterations (40) is a step count — on a model running at
  # ~7 tok/s that is potentially hours before anything trips.
  #
  # Off by default on purpose: a wall-clock cap cannot tell a stalled run from
  # a slow one that is making real progress, so a default would eventually kill
  # legitimate long work. Set it when you actually want "spend at most N
  # minutes on this" — most useful for unattended runs (spawned sub-agents,
  # scripted `aegis chat`) where nobody is watching to interrupt.
  #
  # The bound is per *run*, not per session or per task. In the phased
  # threat-model drive that means per phase turn, so it will not guillotine a
  # long build partway through — but note that a run aborted this way is fatal
  # to the drive, unlike a context overflow or a tool-failure stall, which
  # reset and resume. That is deliberate: resuming past a time bound you set
  # would defeat setting it.
  max_wall_clock_per_run: 0

  # Seconds of complete silence — no streamed token, no tool call starting or
  # finishing — before the current turn is declared hung and the run aborts
  # with a named error. 0 disables it. Default 900 (15 minutes).
  #
  # This is the one time bound that ships ON, and the reason is that it does not
  # require a judgement call. `max_wall_clock_per_run` above cannot tell a
  # stalled run from a slow one, so it must stay opt-in; there is no legitimate
  # work that produces literally nothing for fifteen minutes, so this one can
  # carry a default.
  #
  # 15 minutes is a backstop, not a competitor: it deliberately sits above every
  # narrower *per-call* timeout (provider.stream_idle_timeout at 10 minutes, the
  # shell tool's 600s per-call ceiling, cron's 10-minute job bound, every
  # per-call bound in the tool package) so those report their own precise
  # failure first. It is also the only bound that covers the tool-execution half
  # of a turn at all. Like the wall-clock abort, a stall is fatal to the phased
  # drive rather than a resumable reset — a fresh context would be handed
  # straight back to whatever is wedged.
  #
  # Two *aggregate* bounds are deliberately larger: an `agent` workflow batch
  # and a `debate` run may span up to 40 and 80 minutes respectively. They are
  # safe because each is decomposed into per-teammate waits capped at 10 minutes
  # that report progress in between, so the aggregate can never be reached
  # without activity the detector sees. A queued request waiting on
  # provider.max_concurrent_requests reports itself alive the same way. Before
  # that (P66.8) a healthy multi-agent round was aborted here as "the turn is
  # hung, not slow" — the one diagnosis a drive cannot recover from.
  max_turn_stall: 900

  # 0 = unlimited. Refuses to start a new turn once a session's cumulative
  # (persisted) cost reaches this amount.
  session_cap_usd: 0.0

  # 0 = unlimited. Refuses to start a new turn once total spend across all
  # sessions for the current UTC day reaches this amount.
  daily_cap_usd: 0.0

  # 0 = unlimited. Token-denominated counterparts to session_cap_usd/
  # daily_cap_usd — refuse a new turn once cumulative tokens (session or
  # cross-session daily) reach this amount. Recommended for local models,
  # where the dollar caps above never fire.
  session_token_cap: 0
  daily_token_cap: 0

  # Fraction (0-1) of session_cap_usd/daily_cap_usd/session_token_cap/
  # daily_token_cap at which a "cost_alert" warning event is surfaced to the
  # client instead of a hard stop. Only applies to whichever cap above is
  # non-zero.
  alert_threshold: 0.8


# ── Daemon ────────────────────────────────────────────────────────────────────
server:
  # The daemon's listen address. Defaults to loopback. Separately, browser
  # requests carrying a non-loopback Origin header are always rejected
  # regardless of this setting (DNS-rebinding protection).
  addr: "127.0.0.1:4127"

  # Must be set to bind `addr` to a non-loopback address (e.g. "0.0.0.0:4127"
  # to reach the daemon from another machine). The API is protected only by a
  # bearer token with no rate limiting, so the daemon refuses to start on a
  # non-loopback address until this is explicitly acknowledged (FIND-08).
  allow_remote: false

  # Bounds which directories a client may request as a session's working
  # directory (P25.1) once allow_remote is set: the resolved path must be
  # the daemon's own default workspace (or nested under it) or nested under
  # one of these entries, else session creation is rejected. Ignored on the
  # default loopback-only bind, where a client is already as trusted as a
  # local shell user. Empty by default (no extra directories allowed beyond
  # the daemon's own workspace).
  session_workdir_allowlist: []

  # Transport encryption for client<->daemon traffic (FIND-32/P24.18). On by
  # default since P27.5/FIND-13: without it, client<->daemon HTTP is
  # plaintext, including the bearer token and full conversation content —
  # fine against off-host attackers given the loopback-only default above,
  # but observable by another local account on a shared host with
  # packet-capture privilege. Set enabled: false (or AEGIS_SERVER_TLS_ENABLED
  # =false) to opt back out, e.g. in a container/CI environment where the
  # extra cert/handshake overhead isn't worth it and the host isn't shared.
  #
  # With no cert_file/key_file set, the daemon generates a self-signed
  # ECDSA P-256 certificate on first start and persists it as
  # <data_dir>/daemon.crt and daemon.key (reused across restarts, same
  # convention as daemon.token). Every CLI client (`aegis`, `aegis ui`, `aegis
  # sessions`, `aegis acp`, `aegis mcp-serve`, ...) reads daemon.crt and pins
  # it explicitly — this is certificate pinning to a file that never leaves
  # the machine, not verification against a public CA or hostname, so no
  # system trust store is involved. A browser opening the web UI (`aegis ui`)
  # has no such pinning and will show a self-signed-certificate warning; this
  # is expected, and the CLI prints a one-line notice to that effect when TLS
  # is enabled.
  #
  # This protects against another local account observing loopback traffic.
  # It does not protect against Host/OS-level compromise of the same
  # account, which can already read daemon.token — and, with TLS enabled,
  # daemon.key — directly off disk.
  tls:
    enabled: true
    cert_file: ""  # optional operator-supplied cert; auto-generated if empty
    key_file: ""   # optional operator-supplied key; auto-generated if empty

  # Defaults to 10 (P27.12/FIND-14). Caps how many message-turn runs may be
  # actively executing across ALL sessions at once. A request past the cap is
  # rejected immediately (429) rather than queued. The per-session
  # serialization the daemon already does (at most one active run per
  # session) doesn't bound total concurrency across sessions — this exists to
  # bound a lower-trust caller that can create many sessions, e.g. `aegis
  # mcp-serve`. Only top-level HTTP-driven runs count against it; in-process
  # sub-agents spawned by the `agent`/swarm tool don't. 0 = unlimited.
  max_concurrent_runs: 10

  # Defaults to 1800/30 minutes (P27.12/FIND-14). Aborts a single run once it
  # has been active this many seconds, the same clean way an interrupted
  # request is handled. cost.max_tokens_per_run/budget_usd are the primary
  # spend guardrails; this is a coarser wall-clock backstop for a run that
  # never trips those (e.g. a local model stuck in a near-zero-cost tool-call
  # loop). 0 = unlimited.
  max_run_duration_sec: 1800

  # Per-connection cap on queued-but-not-yet-flushed SSE events. If a
  # consumer (TUI, web UI, or an mcp-serve client) reads slower than the
  # engine produces events and the queue fills, the oldest queued event is
  # dropped to make room for the newest rather than growing memory without
  # bound. The run itself keeps executing and persisting to the session store
  # regardless of how far behind the SSE stream falls.
  sse_buffer_size: 256


# ── Logging ───────────────────────────────────────────────────────────────────
# debug | info | warn | error
log_level: info


# ── TUI ───────────────────────────────────────────────────────────────────────
tui:
  # D&D-themed flavor phrases in the status bar while the agent is busy.
  # Phrases are bucketed to match what's actually happening: wizardly/
  # intellectual phrases while the model is reasoning, and separate
  # investigation/crafting/combat/travel/party banks while a tool call is
  # in flight (read/write/execute/network/spawn respectively).
  humor_mode: true

  # Color scheme: "dark" (default), "light", an embedded builtin (catppuccin,
  # dracula, gruvbox, tokyonight), or a custom name loaded from
  # .aegis/themes/<name>.json (project) or ~/.aegis/themes/<name>.json
  # (user). Applied to the sidebar, status bar, glamour markdown rendering,
  # and the ANSI-16 remap used for shell tool output.
  theme: dark

  # Attention system (P16.1): fires on stream-end, approval-pending, and
  # error, but only while the terminal is known to be unfocused (via
  # tea.FocusMsg/BlurMsg — not every terminal/multiplexer reports focus).
  # "off": nothing. "bell": terminal BEL. "desktop": OSC 9/777 desktop
  # notification. "both" (default): bell + desktop. Also settable live with
  # /notify <mode>, for that session only.
  notifications: both

  # Inline image thumbnails (P16.9): render a small half-block ANSI preview
  # in the transcript when an image is attached, instead of only sending it
  # to the model. "auto" (default): the kitty graphics protocol when the
  # terminal *answers* P67.9's startup capability query saying it speaks it,
  # else a half-block preview when the detected color profile supports at
  # least 256 colors, else nothing (dumb terminals, NO_COLOR). "off": never
  # render, text-only notice as before. "halfblock": force the half-block
  # tier whatever the terminal answered. "kitty" (P40.4): force the graphics
  # protocol — only needed for a terminal that supports it but answers no
  # queries; note its placement in the render loop is still not verified
  # against real terminals, so expect rough edges.
  #
  # The capability probe itself (kitty graphics, synchronized output, true
  # color — one DA1-terminated batch at startup, never per frame) has its own
  # escape hatch, the AEGIS_TERM_CAPS environment variable:
  #   AEGIS_TERM_CAPS=off                   do not ask; report nothing supported
  #   AEGIS_TERM_CAPS=kitty,sync,truecolor  force this exact set, do not ask
  # `aegis doctor` prints what the terminal answered, and falls back to the
  # old TERM-based "plausible" wording only when its output is not a terminal.
  image_rendering: auto

  # Keybinding remap (P13.3.5): override the key sequence(s) for a named
  # action. Action names are the lowercased internal/tui keyMap field names
  # (send, steer, newline, thinking, complete, help, palette, cancel,
  # interrupt, clear, editor, cyclemode, histup, histdown, teammates,
  # sessions, terminal, sidebartoggle, pasteimage, diagnose). Values are one
  # or more bubbles/key sequences (e.g. "ctrl+x", "alt+t", "f2"); the first
  # is shown in help text. Unlisted actions keep their default. Run with an
  # unknown action name and Aegis exits with an error naming the typo.
  keybindings:
    terminal: ["ctrl+x"]
    diagnose: ["ctrl+g"]

  # Mouse capture (P74.19): "on" (default) or "off". Capturing the mouse is
  # what makes Aegis's own drag-to-copy selection possible, but it also stops
  # the terminal emulator from offering its own click-drag select — the only
  # thing that reliably works for a tmux/kitty copy-mode workflow. "off"
  # releases capture while keeping the alternate-screen dashboard (so resize
  # re-wrap still works, unlike /scrollback, which releases both); the cost
  # is no mouse-wheel scroll and no click-to-focus. Read once at startup —
  # unlike /scrollback there's no in-session toggle. Most SSH users don't
  # need this: the clipboard already goes over OSC 52.
  mouse: on

  # Reduced motion (P74.10): disable the continuous "working" animations —
  # the status-line shimmer sweep, the streaming caret's blink, the cycling
  # thinking phrase, and a pending tool card's shimmer frame — freezing each
  # at its last frame instead of advancing every tick. Off by default. Also
  # an accessibility setting (the shimmer is a moving-luminance sweep, the
  # class of animation vestibular sensitivity reacts to) and a CPU one (skips
  # the per-tick transcript re-render).
  reduced_motion: false


# ── Diagrams ──────────────────────────────────────────────────────────────────
diagram:
  # Kroki API endpoint for diagram rendering.
  # Use a self-hosted Kroki instance for air-gapped environments.
  kroki_url: "https://kroki.io"


# ── Multi-agent ───────────────────────────────────────────────────────────────
swarm:
  # "in_process"  — sub-agents run as goroutines in the same process (default)
  # "subprocess"  — sub-agents run as isolated processes (more isolation, slower)
  backend: in_process


# ── Lifecycle hooks ───────────────────────────────────────────────────────────
# Shell commands that fire on agent lifecycle events.
# Aegis passes a JSON event object to the command's stdin.
# Exit 0 = allow. Exit 2 = veto (stderr message shown to the model).
# Other non-zero exits are logged as warnings but do not block execution.
hooks:
  # Fires before a tool call. Exit 2 vetoes the call.
  pre_tool_use: ""           # e.g. "aegis-hook-lint"
  # Fires after a tool call completes.
  post_tool_use: ""
  # Fires once when the session starts.
  session_start: ""
  # Fires when the agent finishes (success or error).
  stop: ""
  # Fires when a sub-agent finishes.
  subagent_stop: ""


# ── Web search ────────────────────────────────────────────────────────────────
# Provider for the web_search tool. DuckDuckGo scraping is the zero-config default.
search:
  # brave | tavily | searxng | duckduckgo (default)
  provider: duckduckgo

  # API key for brave or tavily. Supports $VAR expansion from environment / .aegis/.env.
  api_key: ""

  # Required when provider=searxng. Base URL of your self-hosted SearxNG instance.
  base_url: ""

  # Opts web_fetch/web_search output into the heuristic prompt-injection
  # scan, mirroring the per-server mcp[].scan_output toggle (FIND-04). On by
  # default since P27.13/FIND-12 — a best-effort heuristic (invisible
  # characters, base64-encoded payloads) that only adds a visible warning on
  # a hit, never blocks or mutates content. The untrusted-content provenance
  # marker is always applied to fetched/searched content regardless of this
  # setting — see docs/mcp-trust-boundary.md.
  scan_output: true


# ── Repository map ────────────────────────────────────────────────────────────
# Sizes the structural repo overview (files, top-level symbols, import edges)
# injected into the system prompt as <repo_map>, built by `aegis index` and
# refreshed automatically when the cache goes stale.
repomap:
  # Cap on the rendered map, in bytes. The 8000 default is roughly 2000 tokens
  # (~4 chars/token) — a figure calibrated when a small context window was the
  # constraint, and it is why this is a config key rather than a constant: on a
  # 128k-context model, 8000 bytes is 1.5% of the window spent on the single
  # cheapest orientation the agent gets, and doubling or quadrupling it usually
  # buys more than the same tokens spent re-reading files. Raise it on a
  # large-context model; lower it if you are squeezing a 4k/8k local model.
  max_bytes: 8000

  # Cap on how many symbols any one file contributes. This decides what
  # max_bytes buys: breadth across files or depth within a few. Measured on this
  # repo the untruncated render is ~58x the default byte budget, so uncapped the
  # budget is exhausted by the first handful of files — 10 of 672 reached the
  # model, and which 10 was decided by the alphabet. At 3 symbols each, 37 files
  # fit instead (measured, same repo). Files whose list is cut carry an explicit
  # "+N more" marker, so a shortened list never reads as "this file has no other
  # symbols".
  #
  # A negative value means uncapped — render every symbol found and let max_bytes
  # do the truncating. Sensible only with a generous max_bytes.
  max_symbols_per_file: 3


# ── Context compaction ────────────────────────────────────────────────────────
# Compaction keeps a conversation inside the model's context window by
# summarizing older turns; recent turns are always preserved verbatim. It is
# automatic and needs no configuration — this block exists only to override an
# auto-detected default.
compaction:
  # Whether the deterministic prune pre-pass is gated on headroom instead of
  # running on every compaction call.
  #
  # Unset (the default) auto-detects: on a local backend the gate is ON, because
  # llama.cpp/Ollama cache the KV of a request's longest common prefix and the
  # pre-pass rewrites the *middle* of the conversation, discarding every cached
  # token after that point. Against a cloud provider there is no such cache and
  # the gate is off. Set it explicitly to override or to A/B it.
  #
  # Measured 2026-08-08 (qwen3:14b, 24,576-token window, same fixture both arms,
  # two runs), the gate is worth keeping:
  #
  #     gate on:   1m16s / 1m27s wall,  ~54,287ms total prefill
  #     gate off:  2m7s  / 2m7s  wall,  ~98,481ms total prefill
  #
  # Past the compaction trigger, an ungated pre-pass runs on EVERY turn and
  # rewrites the middle for a yield too small to drop back under the trigger —
  # ~9s of prefill per turn, repeatedly. Gated, the conversation stays
  # append-only at ~2.5s and pays that cost three times.
  #
  # An earlier measurement said the opposite (3m19s vs 1m32s) and it is worth
  # knowing why, because the same trap applies to any timing comparison here: the
  # compaction trigger was running on a token estimate that undercounted the real
  # prompt by 20-33%, so compaction fired late enough that BOTH arms were already
  # in the regime where Ollama silently context-shifts. There the prefix cache is
  # gone regardless and the gate can only cost. Fixing the estimate moved the
  # operating point, and the verdict with it.
  #
  # Still unmeasured: above a 200,000-token window the gate uses a fixed 40k
  # buffer rather than a ratio. See internal/eval's
  # TestLiveWorkflowCompactionPrefixCacheGate, which runs one workload twice with
  # only this value changed.
  # preserve_prefix_cache: false

  # How long a conversation may sit idle before its stale, re-fetchable tool
  # results are cleared on the next turn (P67.6). Default: 20m. "off" disables.
  #
  # This is a SECOND, orthogonal trigger. Everything above tunes compaction on
  # context *pressure* — how close the conversation is to the window — which is
  # the right trigger for running out of room and the wrong one for a different
  # problem: a session resumed after a long gap re-sends a prefix the backend has
  # already evicted, paying full prefill on stale tool results it is going to
  # summarize away later anyway. This one triggers on cache *temperature*. The
  # observation behind it is a scheduling one: when the cache is already cold,
  # clearing old tool results is free, because the usual reason not to rewrite the
  # middle of a conversation (you invalidate the cache) has already happened.
  #
  # Only tool results are touched, only the re-fetchable kinds (reads, searches,
  # shell, and read-only git/web), and never the model's own text. Errors are
  # kept. Each cleared result becomes a fixed sentinel telling the model the
  # content is gone and to re-run the tool if it still needs it. It fires at most
  # once per run, and never for an analysis-only caller — the output guard's
  # second pass, a title generation, a capability probe — which must be able to
  # inspect a conversation without mutating it.
  #
  # 20 minutes sits clear of every cache TTL Aegis ships against rather than
  # splitting the difference: Ollama unloads an idle model after 5 minutes by
  # default, and Anthropic's prompt cache expires after 5 minutes (1 hour on the
  # extended tier). Below ~5 minutes the pass would throw away context that is
  # still cached. It is a default, not a finding — no live measurement has been
  # taken at any value, which is why it is a knob.
  # cold_cache_after: 20m

  # How many of the most recent clearable tool results the pass leaves verbatim.
  # Default: 3 — enough that the model still has the last read, the last search
  # and the last command it ran. Floored at 1 no matter what is configured:
  # clearing every result leaves the model with no working context at all.
  # cold_cache_keep: 3


# ── External host commands ────────────────────────────────────────────────────
# Overrides how Aegis locates optional host binaries. Every one has a working
# fallback, so this block is entirely optional — see docs/host-tools.md for what
# each tool is worth and the measurements behind that.
#
# A value may be a bare name (resolved on PATH), a path (used as-is, verified
# executable), or a disable keyword ("off"/"false"/"no"/"none"/"disabled"/"0")
# that forces Aegis's built-in fallback even when the binary is installed.
#
# Aegis execs binaries directly rather than through a shell, so a shell alias is
# never visible to it — if your binary is not on PATH under its usual name, give
# the real path here. `aegis doctor` prints what each key resolved to.
#
# A configured binary that cannot be found is a hard failure, not a silent
# fallback: naming a specific binary and quietly getting another defeats the
# point of setting the key. An unset key that is simply not installed is only a
# warning.
#
# This key redirects binaries Aegis execs, so it is frozen from untrusted
# project config by the workspace-trust gate (see "Project Config and Workspace
# Trust" below) — the grep tool is a *read* capability and is allowed silently
# in plan mode, so a cloned repo pointing `ripgrep:` at its own binary would be
# unprompted host execution. One extra rule applies even after `aegis trust`:
# a project config value naming a *relative* path (`./tools/rg`) is dropped
# with a warning, because it resolves against the workspace and so names a
# binary the repository ships. Absolute paths and bare PATH names are fine.
commands:
  ripgrep: rg        # grep/glob tools; markedly faster than the built-in walker
  git: git           # git tool, commit/diff/log, checkpoints
  gh: gh             # git_pr tool
  mmdc: mmdc         # local Mermaid rendering (else remote Kroki)
  plantuml: plantuml # local PlantUML rendering (else remote Kroki)


# ── Out-of-band notifications ─────────────────────────────────────────────────
# Alert when a detached session finishes, errors, or needs input — and when a
# cron job that opted in (cron_create's `notify: true`) fires. These are the
# channels; what gets sent over them is decided per session/per job.
notify:
  # Desktop notification via osascript (macOS), notify-send (Linux), or
  # PowerShell toast (Windows). Enabled by default.
  desktop: true

  # POST the event JSON to this URL (optional). Leave empty to disable.
  # The payload carries session_id or job_id, title, status
  # (completed/error/needs_input/blocked), message, and — for a cron fire —
  # the run's full captured output in `output`.
  webhook: ""


# ── Semantic recall ───────────────────────────────────────────────────────────
# Optional embedding layer over the project knowledge base and long-term
# entity memory (P5.8). Disabled by default — both stay FTS5/BM25-only, which
# needs no extra service running. When enabled, search results are the
# reciprocal-rank fusion of the BM25 ranking and a cosine-similarity ranking
# over embeddings computed via a local Ollama model.
embeddings:
  enabled: false

  # Only "ollama" is supported today.
  provider: ollama

  # Any Ollama embedding model, e.g. "nomic-embed-text", "mxbai-embed-large".
  model: "nomic-embed-text"

  # Ollama server base URL.
  base_url: "http://localhost:11434"


# ── Shell sandbox ─────────────────────────────────────────────────────────────
sandbox:
  # "local"     — run directly on the host; no isolation at all (P27.14/FIND-04:
  #               approval prompts and env-var stripping are the only
  #               compensating controls — a shell tool call can still read/
  #               write/exfiltrate anything the daemon's own user account can).
  # "os"        — OS-level isolation: seatbelt on macOS, bwrap/Landlock on Linux; no
  #               container needed. `aegis --first-init`'s generated config defaults
  #               new installs to this (falls back to "local" with a startup WARN
  #               if unavailable, e.g. bubblewrap not installed on Linux, or on
  #               Windows where neither mechanism exists).
  #               Confines WRITES, network (if configured below), and READS (P27.18/FIND-19) to
  #               the workspace plus a built-in toolchain allowlist (system dirs, ~/go, ~/.cargo,
  #               ~/.npm, etc. — see os_extra_read_paths below); anything not on that allowlist,
  #               including credential dirs like ~/.ssh or ~/.aws, is simply unreadable from
  #               inside the sandbox. Still weaker than "container", which never mounts the host
  #               filesystem at all — a toolchain dir that happens to also hold a stray credential
  #               file would still be readable. See docs/security_scan.md before relying on this
  #               for genuinely untrusted code.
  # "container" — run inside a container (requires runtime)
  # "auto"      — detect available runtimes and pick the best one
  #
  # A container runtime name (docker, podman, wsl/wslc, apple) is also
  # accepted here directly and is rewritten to backend: container + the
  # matching runtime below (P25.2) — convenient, but the pair below is the
  # canonical spelling. Any other value fails the daemon at startup with an
  # error naming the offending value, rather than silently running
  # unsandboxed (which is what happened before P25.2).
  #
  # NOTE: shown here as "os" because that's what `aegis --first-init` writes
  # into a fresh global config. If this key is absent entirely (no config
  # file, or a config file that doesn't set it), the built-in default is
  # "local", not "os".
  backend: os

  # Force a specific runtime when backend=container or backend=auto:
  # docker | podman | wslc | container (Apple Containers, macOS)
  # Leave empty to let auto-detection choose.
  runtime: ""

  # Override the auto-detection priority order.
  # Default (OS-specific): on Windows [podman, docker, wslc]; on macOS
  # [docker, podman, container]; elsewhere [docker, podman].
  #
  # wslc is last on Windows rather than first: its Docker-shaped CLI carries
  # neither the hardening flags (--cap-drop/--security-opt/--read-only) nor the
  # persistent-container detach/exec surface docker and podman do, and it
  # cannot build the scanner images at all. It stays reachable for a machine
  # that has only Windows Containers.
  priority: []

  # Container image to use when backend=container or backend=auto selects a container.
  image: "ubuntu:22.04"

  # Allow network access inside containers. false = network-isolated (safer).
  network: false

  # Per-container resource caps (P60.1). The hardening flags Aegis applies
  # (--cap-drop=ALL, --security-opt=no-new-privileges) cover the privilege
  # axis; these cover the resource one. Without them a model-driven `go
  # build`, `npm ci` or test run inside the sandbox can consume the whole
  # host — and on a machine that is also running the model server, that means
  # the OOM killer choosing between the model and the daemon rather than a
  # single failed command. `--rm` per command bounds how long a runaway
  # lasts, never its peak, and the peak is what binds.
  #
  # Values are in the container runtime's own vocabulary and are passed
  # through verbatim ("4G", "512M", "1.5"). Empty (or 0 for pids_limit)
  # removes that cap; raise them for a heavy toolchain.
  #
  # Enforcement is per-runtime, because a flag a runtime's CLI doesn't accept
  # is not a weaker limit — it's a container that refuses to start:
  #   docker/podman  — memory, cpus, pids_limit
  #   container      — memory, cpus (Apple's CLI has no --pids-limit)
  #   wslc           — none; its resource surface is unverified, the same
  #                    reason it gets no hardening flags. The daemon logs a
  #                    WARN at startup when limits are configured but the
  #                    selected runtime cannot enforce them, so an operator
  #                    never infers a bound that isn't there.
  # Only applies to backend: container (and auto when it selects one).
  limits:
    memory: "4G"
    cpus: "2"
    pids_limit: 1024

  # One long-lived container per workspace directory for the daemon's
  # lifetime, with each command run inside it, instead of a fresh
  # `run --rm` per command (P60.2).
  #
  # Why it defaults to true: with a container per command, nothing an agent
  # does survives the call that did it. An installed toolchain, a warmed
  # build cache, a background dev server, a half-applied migration — all
  # discarded the moment the command returns, with the workspace bind-mount
  # as the only channel through which anything is observable. That made the
  # container backend behaviourally WORSE than `local` for multi-step work,
  # not merely slower: an agent could not do `npm install` and then
  # `npm test` without collapsing them into one shell string.
  #
  # What persists and what doesn't: filesystem and process state persist —
  # anything installed, anything written outside the mount, anything left
  # running. Shell state does not: each command is a new process with a new
  # shell, so `cd` and `export` still die with it, exactly as they do
  # between two shell calls on the `local` backend.
  #
  # The container carries the same hardening flags, resource limits and
  # network posture a per-command run would. Only docker and podman are
  # supported (verified `run -d`/`exec`/`rm -f` surface); on wslc and Apple
  # Containers this key has no effect and the daemon says so at startup.
  # Set false for the strictly leak-free per-command posture.
  persistent: true

  # How long a persistent container lives when the daemon never gets to tear
  # it down — SIGKILL, a host that sleeps forever (P60.2). The container is
  # held open by a `sleep` of this length under `--rm`, so expiry removes it
  # with nothing needing to run. A daemon start also reaps containers whose
  # owning process is verifiably gone (matched by label, never touching a
  # container belonging to a live Aegis process). 0 = 4 hours.
  session_ttl_sec: 14400

  # If backend=container/os and the runtime can't be initialized (e.g. Docker
  # daemon down), the default is to log a warning and fall back to running
  # unsandboxed on the host — a silent security downgrade an operator might
  # not notice (P7.4). Set strict=true to make that a hard startup failure
  # instead. The daemon also reports an active fallback via /healthz, and the
  # TUI/CLI print a warning before entering a session when it's set.
  strict: false

  # Commands run by the local/os backends never see ANTHROPIC_API_KEY or
  # OPENAI_API_KEY (P7.2) — a prompt-injected `shell` call can't read the
  # daemon's own provider credentials back out and exfiltrate them via
  # web_fetch. List additional env var names here to also exclude them, e.g.
  # secrets loaded from .aegis/.env for MCP server auth that the shell tool
  # has no legitimate reason to see. Container backend tools never inherit
  # host env at all, so this only applies to backend: local/os/auto(fallback).
  strip_env: []

  # Additional host paths the "os" backend may read from, on top of the
  # workspace and the built-in toolchain allowlist (P27.18/FIND-19). Use this
  # when a project's toolchain lives somewhere non-standard. Entries that
  # don't exist on the host are silently skipped. Only applies to backend: os.
  os_extra_read_paths: []


# ── Contextual security policies ──────────────────────────────────────────────
# Note: these are tool-layer controls, not system-wide egress firewalls.
# The shell tool can still reach the network. For hard enforcement, use
# sandbox.backend=container with network=false.
security:
  # Require approval for write-capability tool calls that happen after any
  # network-capability call in the same session (prevents exfiltrate-then-modify).
  egress_then_write: false

  # Restrict network-capability tool calls to these domains.
  # Empty list = unrestricted.
  network_allowlist: []
    # - "api.github.com"
    # - "registry.npmjs.org"

  # Runs a read-capability tool's output through gitleaks-backed secret
  # detection before it's added to the conversation sent to the model
  # provider; any match is masked as "[REDACTED:<rule>]". Defaults to true —
  # sending file/conversation content carrying an unredacted secret to a
  # cloud provider is a real exposure with no other default control. Needs
  # gitleaks on PATH (fails open, i.e. no redaction, if missing) and adds
  # per-call latency; local Ollama usage never sends content off the machine
  # at all and remains the stronger mitigation. See docs/providers.md "Data
  # Exposure & Redaction".
  redact_secrets: true

  # Security-scanner availability (P11.11): controls `aegis scan`/security_scan.
  # "auto" | "host" (never fall back to a container) | "container" (always
  # prefer it).
  #
  # Under "auto", a tool the multiscanner image carries resolves to the
  # CONTAINER in preference to an available host binary (P55.4): host binaries
  # are unpinned and unconfined, so two machines can silently scan with
  # different rule sets. A refused container falls back to host rather than
  # failing the tool, and says so. Tools the image does not carry resolve
  # container -> host -> WSL as before.
  default_method: auto

  # Per-tool overrides, keyed by scanner name (opengrep, trivy, gitleaks).
  # image must be digest-pinned (image@sha256:...) — Aegis ships no built-in
  # image pin; see docs/security_scan.md for how to obtain and verify one.
  tools: {}
    # trivy:
    #   method: auto
    #   image: "aquasec/trivy@sha256:<digest-you-verified>"
    # opengrep:
    #   enabled: true
    #   method: host
    # gitleaks:
    #   install: prompt   # prompt (default) | always | never

  # One locally-built image carrying every bundled scanner, so container-method
  # scanning needs one image instead of a digest-pinned image per tool. Written
  # by `aegis security build-image` — you don't normally hand-edit this block.
  # Run `aegis security update-db` afterwards to populate the vulnerability
  # databases, which live in a container volume rather than in the image.
  #
  # image_id is a real image ID, not a registry digest: a locally-built image
  # has no digest (RepoDigests is empty until a push/pull), so instead of the
  # pin rule above, Aegis reads the image's actual ID back via `image inspect`
  # and compares it before every container run. Rebuilt or retagged behind
  # Aegis's back = scans fail closed with a specific reason.
  multiscanner:
    enabled: false
    # image: "localhost/aegis-multiscanner:v1"
    # image_id: "sha256:..."   # recorded at build time; re-verified before use
    #
    # The runtime that built the image. Recorded because a locally-built image
    # exists only in the storage of the engine that built it — auto-detection
    # could pick a different one — it returns the first *available* engine in
    # priority order, not the one that built anything — and report a perfectly
    # good docker-built image as missing because podman answered first.
    # runtime: podman
    #
    # How many scanners run at once (each container-method scanner is one
    # container). Applies to host-method runs too. Default 3; set 1 for
    # strictly sequential runs. The report is identical at any value.
    # concurrency: 3
    #
    # Which scanners the built image carries; written from the profile that
    # was actually built. Empty assumes the full profile.
    # tools: []

  # The second locally-built image (P55.7), written by
  # `aegis security build-image --netscanner`. Pinned exactly as the
  # multiscanner is, and for the same reason.
  #
  # What separates them is mount posture, not tool category: nmap, nuclei, and
  # image-reference scanning with trivy/grype each analyze a REMOTE target, so
  # every tool here needs network egress and none needs your workspace. This
  # image therefore runs with network ON and no workspace mounted, ever —
  # enforced structurally (its runner takes no directory argument), while the
  # multiscanner keeps --network none with the workspace mounted. The two
  # resolve through separate resolvers so "container" never means two postures
  # at one call site.
  #
  # It needs no `update-db`: having network, it refreshes its own databases
  # into a separate volume.
  netscanner:
    enabled: false
    # image: "localhost/aegis-netscanner:v1"
    # image_id: "sha256:..."   # recorded at build time; re-verified before use
    # runtime: podman          # the engine that built it — see multiscanner above

  # Both images come out of one embedded build context, so both pins carry the
  # same source fingerprint and editing either one's stages moves it for both:
  # rebuilding one leaves the other reported as drifted. `aegis security status`
  # shows drift; `build-image` warns about the sibling it did not just build.
  #
  # Neither image carries dockle (it needs the container engine socket —
  # effectively host root) or zap (its own official image and mount contract).
  # Those two are host-only by design.

  # Names a specific registered WSL distro (e.g. "kali-linux") to target for
  # every WSL-capable scanner (nmap, nuclei, opengrep, kubescape), instead of
  # whatever `wsl --set-default` currently points at. Empty (default) uses
  # WSL's own default-distro selection. Windows-only; a security-tooling
  # distro like Kali is recommended for red-team/recon work — see
  # docs/security_scan.md.
  wsl_distro: ""

  # Hard authorization gate for the dast_scan tool (P11.7), enforced inside
  # the tool itself regardless of permission mode — an active scanner
  # pointed at an arbitrary host is an abuse primitive. Loopback and
  # RFC-1918 private addresses are always allowed with no config needed.
  # Sourced from user/global config only: a project .aegis/config.yaml can
  # never widen this, even once trusted.
  dast:
    # Exact hostnames, ".suffix" subdomain wildcards, or CIDR ranges
    # authorized for scanning, in addition to the loopback/RFC-1918
    # default-allow. Hostnames are matched literally, never DNS-resolved.
    allowed_targets: []
      # - "staging.example.com"
      # - ".internal.example.com"
      # - "10.0.0.0/8"

    # Gates active/api scan modes (real attack payloads, not just passive
    # observation) behind an explicit opt-in, on top of the per-call
    # approval prompt every dast_scan call already gets from its execute
    # capability. Default false.
    allow_active: false

  # Opt-in P12 debate integration (P12.5): both default false. See
  # multi-agent.md#debate-p12.
  debate:
    threat_model: false   # security-architect: debate each threat/mitigation before writing it down
    triage: false           # security-audit skill: debate a borderline/disputed finding before suppressing it


# ── Workspace roots ───────────────────────────────────────────────────────────
workspace:
  # Directories outside the session's own workdir that workspace-confined
  # tools (read_file, ls, write_file, edit_file, multi_edit, yaml_validate,
  # render_diagram, security_scan, latex_build, ...) may resolve paths into
  # (P52.13).
  #
  # This exists for the cross-repo shape a single root makes inexpressible:
  # read research artifacts out of repo A, write the formal document into
  # repo B. Starting Aegis from their common parent also works, but widens
  # confinement far past what the task needs and inflates the repo map.
  #
  # Two locks stand in front of every entry:
  #   1. Like permission.*/sandbox.*/hooks, this key is frozen from an
  #      untrusted project config — a cloned repo cannot nominate "/" as a
  #      root just by being checked out. Run `aegis trust` in the project.
  #   2. Each root needs its OWN decision: `aegis trust --dir <path>`. An
  #      additional root does not inherit the workspace's trust. Entries that
  #      are untrusted, missing, or already inside the workdir are dropped
  #      with a warning in the daemon log rather than failing startup.
  #
  # Roots are read-only unless you say otherwise, which is what makes them
  # cheap to grant. Relative paths resolve against the session workdir.
  additional_roots: []
  #  - path: ../research-repo    # readable, not writable
  #  - path: /srv/shared/docs
  #    writable: true            # opt in to writes

# ── Git tools ─────────────────────────────────────────────────────────────────
git:
  # Pre-commit test gate (P46.2). When set, the git_commit tool runs this
  # command in the workspace before every commit; a non-zero exit aborts the
  # commit and returns the command's output to the model. Makes "tests pass
  # before every commit" a mechanical gate rather than unenforced prose.
  # Empty (default) = git_commit stays a straight passthrough.
  #
  # It executes an arbitrary host command, so — like hooks and plugins — it is
  # frozen from untrusted project config by the workspace-trust gate: a cloned
  # repo's .aegis/config.yaml cannot introduce or change it until you run
  # `aegis trust`.
  pre_commit_test_command: ""    # e.g. "go test ./..." or "npm test"
  pre_commit_test_timeout_sec: 0  # 0 = default (600s)

# ── Output validation ─────────────────────────────────────────────────────────
output_guard:
  # enabled: true means every final answer is checked before being shown.
  # Toggle per-session with /guard on|off inside the TUI.
  enabled: true

  # "llm"    — second model call checks the answer against the rubric.
  # "schema" — the answer must be valid JSON containing the required keys
  #
  # In schema mode the corrective retry after a failure is sent with decoding
  # constrained to the required shape (P59.8): the required keys are compiled
  # into a JSON Schema and passed as Ollama's `format`, so the retry cannot
  # produce a non-conforming object rather than being asked nicely and checked
  # again. Only the retry is constrained — the first turn is where the model
  # does the actual work, and tools are suppressed on the constrained turn
  # since a grammar and a tool schema pull it in two directions. Backends that
  # can't constrain decoding (OpenAI, Anthropic) ignore it and fall back to
  # today's ask-and-check behavior; the schema check itself runs either way and
  # is what actually decides.
  mode: llm

  # Which model runs the verdict. Empty (the default) resolves per backend
  # (P59.5):
  #   cloud — provider.small_model when set, a fast non-thinking judge being
  #           both cheaper and better at the strict PASS/FAIL contract;
  #   local — the session's own model. The guard fires on every final answer,
  #           and on one Ollama server naming a second model evicts the
  #           resident one and pays a full cold reload on the next turn, which
  #           costs far more than the cheaper verdict saves.
  # Set it explicitly to override either default — e.g. a small local judge on
  # a box with the VRAM to keep both models resident.
  model: ""

  # Rubric for llm mode. Empty = built-in rubric (fully addresses request, no
  # unfinished work like TODOs/stubs, grounded in tool output). Clearly-marked
  # example/placeholder values in documentation — an illustrative IP address,
  # a <your-api-key>-style token — are acceptable under the built-in rubric,
  # since the real value often depends on the reader's own environment and
  # was never supplied to the model.
  rubric: ""

  # Number of corrective retry attempts when the guard fails.
  # Guards fail open: any validator error yields a pass.
  max_retries: 1


# ── Default persona ────────────────────────────────────────────────────────────
# Persona a new session starts with when --persona isn't passed. Set this in
# .aegis/config.yaml to give a project its own default focus (`aegis persona use
# <name>` writes this for you). An explicit --persona always overrides it.
default_persona: ""   # e.g. "developer"; empty falls back to "general"

# ── Per-persona model overrides ───────────────────────────────────────────────
# Pin specific built-in personas to a different model within the same provider.
# Model resolution: config personas[name].model > persona file model > global model.
personas:
  security-architect: { model: claude-opus-4-8 }
  developer: { model: "" }   # blank = use global provider.model


# ── Built-in skills ───────────────────────────────────────────────────────────
# Skills embedded in the Aegis binary (content-review, html-report,
# security-audit, architecture-diagram, debug-investigation,
# redteam-engagement, threat-modeling, latex-report, deep-research,
# structured-build, documentation-as-code, document-codebase — see
# `aegis skills list`). Empty by
# default: they stay dormant (no system-prompt cost) until named here, via
# `aegis skills enable <name>`, or
# the /skills TUI command. Project/user skill files (.aegis/skills/,
# ~/.aegis/skills/) are unaffected — those are always active.
skills:
  builtin_enabled: []   # e.g. ["security-audit", "architecture-diagram"]


# ── Optional tool families ────────────────────────────────────────────────────
# Deferred tool families the *local prompt profile* omits, additive and empty by
# default. The default profile registers every family and ignores this key.
#
# Under the local profile (provider.prompt_profile: local, or a local backend),
# three families are dropped: "team" (team_send/team_inbox/team_task_*, swarm
# coordination), "cron" (cron_create/list/toggle/delete/history, background
# scheduling) and "entity" (entity_remember/entity_recall, long-term memory).
# Between them they were thirteen of the twenty-six tools listed in
# <deferred_tools> and ~570 tokens of every turn's prompt — advertised on a
# profile tuned for small local models doing file-scoped work, which do not
# reach for them.
#
# None of the three is unusable on a local model, which is why this is a knob
# rather than a deletion: a local model driving a swarm is a real setup, just
# not the one the profile is tuned for. Name a family here to put it back.
# An unrecognized name is ignored rather than failing startup.
tools:
  families: []   # e.g. ["team"] or ["cron", "entity"]


# ── LSP servers ───────────────────────────────────────────────────────────────
# Language servers give the agent IDE-level code intelligence (diagnostics,
# references). Multiple servers can be listed; each handles its file extensions.
lsp:
  - name: gopls
    command: gopls
    args: []
    extensions: [".go"]
  - name: typescript-language-server
    command: typescript-language-server
    args: ["--stdio"]
    extensions: [".ts", ".tsx", ".js", ".jsx"]
  - name: pyright
    command: pyright-langserver
    args: ["--stdio"]
    extensions: [".py"]
  - name: my-custom-lsp
    command: /opt/tools/my-custom-lsp
    args: []
    extensions: [".foo"]
    trust: true   # required for commands outside the built-in allowlist

# LSP servers start eagerly at daemon boot, with no interactive prompt
# available — a malicious project config could otherwise point `command` at
# an arbitrary binary and get it executed on first launch. `command` is
# checked by basename (path/extension stripped) against a small built-in
# allowlist of common LSP servers (gopls, typescript-language-server,
# pyright*, rust-analyzer, clangd, etc.); anything not on that list is
# refused unless the entry sets `trust: true`.


# ── MCP servers ───────────────────────────────────────────────────────────────
# External tools via the Model Context Protocol (stdio or HTTP/SSE transport).
# The agent sees MCP tools alongside built-in tools with no distinction.
#
# `capability` sets the tool.Capability ("read", "write", "network", "execute",
# or "spawn") the permission gate uses for every tool this server exposes.
# It defaults to "execute" — the most restrictive class — for any server that
# doesn't declare one, since Aegis has no way to know what an MCP server's
# tools actually do; an unlabeled or untrusted server must not silently get
# the always-allowed "network" capability. Use `tool_capabilities` to override
# per remote tool name when a server exposes a known mix (e.g. a read-only
# `search` tool alongside a `write_file` tool).
#
# `scan_output` (default true, P27.13/FIND-12) opts a server's
# tool/resource/prompt output into a heuristic prompt-injection scan before
# it reaches the model — a best-effort check (invisible characters,
# base64-encoded payloads) that only adds a visible warning on a hit, never
# blocks or mutates content, so it's safe to leave on even for well-vetted
# servers. Every server's output is always wrapped with a provenance marker
# regardless of this flag. Set `scan_output: false` per server to opt out
# (e.g. a high-volume trusted server where the extra scan pass isn't worth
# it). See docs/mcp-trust-boundary.md.
#
# `scan_arguments` (default false) is the outbound mirror (FIND-12):
# tool-call arguments are model-constructed and may carry anything the model
# has read into context, and they're forwarded verbatim to the target
# server — an exfiltration channel if the server is untrusted. When enabled,
# arguments bound for this server are checked against a small set of
# credential-shaped patterns (API keys, PEM private keys, AWS keys, bearer
# tokens, ...) and a hit logs a Warn naming the server, tool, and pattern
# class. Flag-only: the call is never blocked or altered. See
# docs/mcp-trust-boundary.md.
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
    tool_capabilities:
      read_file: read
      list_directory: read
      write_file: write

  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_..."
    capability: network   # trusted, well-known server; all its tools call out to GitHub's API

  # HTTP/SSE transport: set command to empty string and provide a URL.
  - name: my-http-server
    command: ""
    auth: "$MY_MCP_TOKEN"   # $VAR references expanded from environment / .aegis/.env
    # capability omitted → defaults to "execute", so calls hit the Ask gate in build mode
    # scan_output omitted → defaults to true; not fully vetted, so leave it on
    scan_arguments: true     # not fully vetted — warn if credential-shaped data heads its way


# ── MCP server mode ────────────────────────────────────────────────────────────
# `aegis mcp-serve` (P6.3) — the reverse direction of the `mcp:` block above:
# this daemon acts as an MCP server itself, so other MCP-speaking harnesses
# (Claude Code, Codex, editors) can drive Aegis sessions as tools. See the CLI
# reference for the tools it exposes (aegis_prompt, aegis_new_session,
# aegis_list_sessions).
mcp_server:
  # New sessions default to plan mode (read-only) — a lower-trust posture than
  # the local TUI/CLI's own "build" default, since the caller here is an
  # external harness. Override per-call with the `mode` tool argument, or here
  # to change the server-wide default.
  default_mode: plan
  # An MCP tools/call is a synchronous request/response with no human in the
  # loop to ask, so a tool call needing approval (e.g. a session explicitly
  # requested in build/auto mode) is denied by default. Set true only for a
  # fully trusted caller/sandbox — equivalent to permission.auto_approve_exec
  # but scoped to sessions created through mcp-serve.
  auto_approve: false


# ── Process plugins ───────────────────────────────────────────────────────────
# Register external commands as tools. Aegis pipes tool input as JSON to stdin
# and captures stdout as the result.
plugins:
  - name: check_types
    description: "Run TypeScript type checking"
    command: npx
    args: ["tsc", "--noEmit", "--pretty"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}}}'
    capability: read     # read | write | execute | network
    timeout_sec: 60

  - name: my-linter
    description: "Run custom linting on a file"
    command: my-lint
    args: ["--json"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}'
    capability: read
    timeout_sec: 30


# ── Session cleanup ───────────────────────────────────────────────────────────
cleanup:
  # Auto-delete non-archived sessions older than N days (since last update).
  # 0 disables automatic pruning.
  session_ttl_days: 0

  # How often the pruner runs, in hours.
  interval_hours: 24


# ── Data directory ────────────────────────────────────────────────────────────
# Where Aegis stores its databases, logs, and user-scoped files.
# Leave empty for the OS default:
#   macOS/Linux: ~/.config/aegis/
#   Windows:     %AppData%\aegis\
data_dir: ""
```

---

## Project Config and Workspace Trust

`.aegis/config.yaml` is committed to the repository, so checking out a repo is
enough to hand Aegis its settings. Most settings are preferences and apply
straight away. The rest — anything that can execute a host binary, open a
network channel, widen the file-access boundary, or relax the permission gate —
is **frozen to your own user/global values until you run `aegis trust` in that
directory**. `aegis trust` shows exactly which settings the project would
change before you accept them, and `aegis doctor` reports a frozen workspace.

**A project may set these without trust** — they grant no capability:

`log_level` · `default_persona` · `personas` · `skills` · `repomap` ·
`compaction` · `output_guard` · `tui` · `swarm` · `tools` · `cost` ·
and, under `provider:`, the model and tuning knobs — `model`, `small_model`,
`max_tokens`, `max_retries`, `max_iterations`, `loop_threshold`,
`zero_tool_nudge`, `temperature`, `seed`, `think`, `reasoning_effort`,
`context_window`, `keep_alive`, `response_header_timeout`,
`stream_idle_timeout`, `task_routing`, `tool_call_probe_trials`,
`model_capabilities`.

**Everything else needs `aegis trust`**, including `permission`, `sandbox`,
`mcp`, `mcp_server`, `hooks`, `plugins`, `lsp`, `commands`, `server`,
`security`, `git`, `workspace`, `notify`, `search`, `embeddings`, `diagram`,
`cleanup`, and the rest of `provider:` (`default`, `base_url`, `headers`,
`fallback`, `vram_budget_gb`, `kv_cache_type`, `autofit_context`). The last
three are frozen on a different ground from the rest: they describe the
*operator's machine*, not the work, and a cloned repo declaring how much VRAM the
model server may hold oversizes every window on hardware it has never seen —
`autofit_context` with it, since it is the permission to act on that figure. The
list is deliberately the complement of the one above rather than an enumeration
of its own: a config key added to Aegis in a later release
is frozen until somebody classifies it, so the boundary cannot quietly develop
a hole (P66.5/SEC-02).

**Two settings are never taken from project config, trusted or not**, because
their effect reaches past the workspace you are trusting:

- `data_dir` — it resolves the audit trail, the session database and the
  tool-result spill directory. Set it globally or in the environment.
- `security.dast.allowed_targets` — the authorization list for an *active*
  scanner (P27.9).

An attempt at either is reverted and logged.

### A grant covers content, not just a path

A trust decision is recorded against a **fingerprint of the security-relevant
settings in that directory's `.aegis/config.yaml`** — exactly the keys listed
under "everything else needs `aegis trust`" above, plus any key a future release
adds without classifying it. If those settings change afterwards, the grant no
longer applies: the workspace goes back to frozen, `aegis trust`, `aegis doctor`
and the startup warning report it as *stale* rather than untrusted, and re-running
`aegis trust` shows you the current diff and re-grants. A `git pull` that adds a
`hooks:` block, flips `security.*` or introduces a `commands:` override therefore
prompts you again instead of being silently inherited (P66.25/SEC-07).

What does **not** move a fingerprint:

- edits to keys a project may set without trust (`log_level`, `provider.model`,
  `cost`, …) — a digest that fired on every config edit would train you to
  re-accept without reading;
- changes to your *own* `~/.config/aegis/config.yaml` or `AEGIS_*` environment
  variables — only project-controlled content is fingerprinted;
- reordering a YAML block without changing any value.

**Grants recorded before this release carry no fingerprint and are treated as
stale**, so each already-trusted directory prompts once. That is deliberate:
those grants were made against content nobody recorded, so "it still matches"
is not something Aegis can check, and adopting whatever is on disk today would
bless anything that arrived in the meantime.

**Known gap: `.aegis/.env` is not fingerprinted.** Trust is resolved *before*
any project-controlled file is read (P66.1/SEC-01) precisely so that `.env` is
never parsed on the strength of a decision that has not been made yet; covering
it in the fingerprint would mean parsing project content ahead of that decision
and reintroducing the ordering that gate exists to prevent. The two cannot both
be had, and this is the smaller hole: `.env` is read **only in an already
trusted workspace**, and it may not set `AEGIS_*` at all (those keys are dropped
and logged, trusted or not), so it is a secrets file rather than a config layer
and cannot move an Aegis setting the way an unfingerprinted `hooks:` or
`commands:` block can. What it *can* still do in a workspace you previously
trusted is set ordinary environment variables that child processes read
(`GIT_SSH_COMMAND`, `NODE_OPTIONS`, `PATH`, …), and no re-prompt will fire for
that. Treat `.aegis/.env` as content you re-review yourself, and use
`aegis trust --revoke` on a repository you no longer want reading it.

---

## The `.aegis/.env` File

Place secrets that must not appear in version-controlled YAML into `.aegis/.env`:

```ini
# .aegis/.env — add this file to .gitignore
MY_MCP_TOKEN=secret-bearer-token-here
SOME_INTERNAL_API_KEY=another-secret
```

- Values are loaded before config parsing, so they can be referenced as `$VAR` in supported YAML fields (currently `mcp[].auth`)
- Real environment variables always override `.env` values
- **The file is read only in a trusted workspace.** It sets variables into the
  Aegis process and every child it spawns (`LD_PRELOAD`, `GIT_SSH_COMMAND`,
  `NODE_OPTIONS`, `PATH`, …), which is host execution handed to whoever wrote
  the repository — so it is gated on the same `aegis trust` decision as
  `.aegis/config.yaml`. In an untrusted directory it is not read at all, and
  nothing warns about it beyond the usual untrusted-workspace notice. Run
  `aegis trust` in your own projects. (P66.1/SEC-01)
- **A later edit to this file does not re-prompt.** Workspace trust is pinned to
  a fingerprint of `.aegis/config.yaml`'s security-relevant keys, and `.env` is
  deliberately outside that fingerprint — see "A grant covers content, not just
  a path" above for why that ordering is the smaller of the two available holes,
  and what the residual risk is. (P66.25/SEC-07)
- **`AEGIS_*` keys are ignored here, trusted or not**, and logged when dropped.
  This file is for *secrets*; settings belong in `.aegis/config.yaml`, where
  they are reviewable and diffable and the trust gate can show you what
  changed. `AEGIS_*` is the highest-precedence config layer, so honoring it
  from `.env` would have been a way to configure Aegis that no review step ever
  displays. Export the variable in your shell if you want a process-level
  override.
- The file is never written by Aegis — manage it manually
- On Windows, after Aegis successfully reads this file it best-effort applies
  the same owner-only ACL hardening described below for `sessions.db` and
  the daemon auth token (a no-op on POSIX, where the file's own mode bits
  already restrict it). A failure to tighten the ACL of this pre-existing
  file only logs a warning — it never blocks startup.

---

## Local Data Store Permissions (Windows ACL Hardening)

A POSIX file mode bit (e.g. `0o600`) already restricts a file to its owner,
but has no effect on Windows: a new file there inherits its parent
directory's ACL, which commonly grants access to more than just the current
user. On Windows, Aegis applies an explicit, non-inherited, owner-only DACL
(`internal/fsguard`) to every local file that can hold secrets or
conversation history, so another local account on a shared machine can't
read them just because it can read the containing folder:

- The daemon auth token (`<data_dir>/auth`)
- The SQLite session database (`<data_dir>/sessions.db`) and its WAL-mode
  sidecar files (`sessions.db-wal`, `sessions.db-shm`) — this also covers
  checkpoint snapshots, which are stored in the same database
- `.aegis/.env`, best-effort, after Aegis successfully reads it (see above)

This is a defense-in-depth control for Host/OS-level access (FIND-29); it
does not protect against a compromised process running as the same user
account. On POSIX platforms all of the above is a no-op — the existing mode
bits are already sufficient.

---

## Common Recipes

### Pin to a specific Ollama model

```yaml
provider:
  default: openai
  base_url: "http://localhost:11434/v1"
  model: "qwen2.5:32b"
```

### Use Claude for one project

In `.aegis/config.yaml`:
```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  max_tokens: 16384
```

And set `ANTHROPIC_API_KEY` in your environment.

`provider.default` decides which vendor (and which of your API keys) a run
spends, so it is one of the trust-gated keys: run `aegis trust` in the project
once, or set it globally. `model` and `max_tokens` apply either way — see
[Project Config and Workspace Trust](#project-config-and-workspace-trust).

### Restrict shell commands in a project

```yaml
permission:
  mode: build
  rules:
    - "allow bash(npm *)"
    - "allow bash(go *)"
    - "deny bash(*)"    # deny everything else
```

### Enable output validation with a custom rubric

```yaml
output_guard:
  enabled: true
  mode: llm
  rubric: "Every security finding must cite a CVE number or CWE ID."
  max_retries: 2
```

### Run all shell commands in Docker

```yaml
sandbox:
  backend: container
  runtime: docker
  image: "node:20-alpine"
  network: false
```

### Restrict outbound network to specific domains

```yaml
security:
  network_allowlist:
    - "api.github.com"
    - "registry.npmjs.org"
    - "pypi.org"
```

### Set a cost budget for a session

```yaml
cost:
  budget_usd: 2.0   # abort the run if estimated spend exceeds $2
```

### Cap total session/daily spend

```yaml
cost:
  session_cap_usd: 10.0   # refuse new turns once a session has spent $10
  daily_cap_usd: 25.0     # refuse new turns once all sessions spend $25 in a UTC day
  alert_threshold: 0.8    # warn at 80% of whichever cap above is set
```

### Set a token budget (recommended for local/Ollama models)

Dollar caps never fire for local models — their usage is estimated and
contributes $0 regardless of actual volume. Token caps are always
enforceable:

```yaml
cost:
  max_tokens_per_run: 200000   # abort a single run past 200k cumulative tokens
  session_token_cap: 1000000   # refuse new turns once a session hits 1M tokens
  daily_token_cap: 5000000     # refuse new turns once all sessions hit 5M tokens in a UTC day
```

**Which token cap you want depends on the question you are asking (P59.4).**
`max_tokens_per_run` counts input + output + cache, summed over every turn — and
on Ollama the input half is the *entire prompt, re-counted each turn*, not a
delta. That is the right number when you are billed on it, and a surprising one
when you are not: it grows roughly with the square of conversation length, so a
cap chosen to mean "do at most this much work" trips far earlier than intended,
and the abort reports a figure that has little to do with how much the model
actually produced.

For a work budget, cap generated output instead:

```yaml
cost:
  max_generated_tokens_per_run: 40000   # abort once the model has written 40k tokens
```

Both can be set at once; whichever is reached first ends the run, and each abort
names the key that fired. On a local backend, `max_generated_tokens_per_run` is
usually the one that means what you meant.

### Bound a run by time (unattended runs on slow local hardware)

Token caps bound volume, not duration — a run can sit well under a token cap
and still take hours on a model generating a handful of tokens per second. When
the real constraint is "don't spend more than N minutes on this", set a
wall-clock bound:

```yaml
cost:
  max_wall_clock_per_run: 900   # abort a run that has been going 15 minutes
```

Leave it at 0 for interactive work, where you can interrupt a run yourself and
where a slow-but-progressing turn is not a failure. It earns its keep on
unattended surfaces — spawned sub-agents and scripted `aegis chat` — which have
no human watching and, unlike cron jobs (bounded separately at 10 minutes),
nothing else bounding their duration.

### Catch a hung turn (on by default)

A wall-clock bound is the wrong instrument for a *hang*. A legitimate phased
drive runs for hours, so any value large enough to be safe for it is far too
large to notice a turn that stopped dead — and every other guard in the engine
is progress-shaped (the no-progress nudge counts turns, the loop detector
compares tool calls, the tool-failure breaker counts failed rounds), so all of
them need turns to keep completing. A turn that never returns advances no
counter and looks exactly like a slow one.

`cost.max_turn_stall` measures silence instead of duration: no provider stream
event and no tool call starting or finishing. It is on by default at 900
seconds and needs no tuning for ordinary use. Raise it if you run a tool that
legitimately blocks for more than fifteen minutes with no output; set it to 0
to turn the check off:

```yaml
cost:
  max_turn_stall: 0     # opt out entirely
```

The abort is reported as a distinct error ("no model output and no tool activity
for …"), never as an interrupt or a backend transport failure, so an automated
caller does not mistake a hang for something worth retrying.

### Route threat-model entries or borderline scan findings through a debate round (P12.5)

Both default off — a debate round is 3+ model calls instead of 1, so this is a deliberate opt-in, not a
default-on behavior change:

```yaml
security:
  debate:
    threat_model: true   # security-architect debates each threat/mitigation before writing it down
    triage: true           # security-audit skill debates a borderline/disputed finding before suppressing it
```

See [multi-agent.md](multi-agent.md#debate-p12) for the debate mechanism itself (`/debate`, `aegis
debate`, the `agent` tool's `mode:"debate"`).

### Configure per-persona models

```yaml
personas:
  security-architect: { model: claude-opus-4-8 }
  developer:          { model: gpt-4o }
  report-writer:      { model: claude-opus-4-8 }
```

### Set this project's default persona

```yaml
default_persona: developer
```

Or from the CLI: `aegis persona use developer` (add `--global` to set the user-wide default instead).

### Bound daemon resources when exposing sessions to another harness (P21.5 / P27.12)

`aegis mcp-serve` lets another MCP-speaking harness create sessions and drive
runs through this daemon. `server.max_concurrent_runs` (default 10) and
`server.max_run_duration_sec` (default 1800/30 minutes) already give every
daemon a global concurrency ceiling and a wall-clock run timeout out of the
box, so a misbehaving or hostile caller can't fan out unbounded sessions/runs
and exhaust the host. Tighten or loosen them for your own deployment:

```yaml
server:
  max_concurrent_runs: 4     # refuse (429) runs past this many active at once
  max_run_duration_sec: 900  # abort any single run past 15 minutes
```

### Enable a built-in skill for this project

```yaml
skills:
  builtin_enabled: ["security-audit", "architecture-diagram"]
```

Or from the CLI: `aegis skills enable security-audit` (add `--global` for the user-wide default instead), or `/skills enable security-audit` in the TUI.

### Get the team/cron/entity tools back on a local model

```yaml
tools:
  families: ["team"]   # or ["team", "cron", "entity"]
```

The local prompt profile omits those three families to keep `<deferred_tools>` small (see *Optional tool families* above). Name the ones you actually use; the default profile is unaffected either way.

### Spend more of a large context window on the repo map

```yaml
repomap:
  max_bytes: 32000            # ~8k tokens: 6% of a 128k window
  max_symbols_per_file: 8     # deeper per file, since there is room for both
```

The defaults (8000 / 3) are sized for a small window. Re-index after changing them — `aegis index` writes the cache the prompt renders from — or just start a session, which rebuilds a stale cache on its own.

### Configure lifecycle hooks

```yaml
hooks:
  pre_tool_use: "/usr/local/bin/aegis-lint-hook"   # lint before file writes
  post_tool_use: "jq . >> /var/log/aegis-audit.jsonl"
```

### Configure pluggable web search

The zero-config DuckDuckGo scrape (the default) throttles hard after roughly
two searches in quick succession — fine for an occasional lookup, not for a
research-shaped workload issuing several searches per round. When DuckDuckGo
is throttled, `web_search` falls back to a second unkeyed scrape (Marginalia)
before giving up, but that ladder is still a degrade-honestly measure, not a
fix. Configuring any of the three providers below makes it the **primary**
backend — `web_search` tries it first and only falls through to the
DuckDuckGo/Marginalia scrape ladder if that call itself errors.

**If you already run a SearXNG instance, point at it — it's the best option
you can have.** No key, no per-query cost, no external rate limit, and
results stay on infrastructure you control:

```yaml
search:
  provider: searxng
  base_url: "http://your-searxng-instance:8080"
  # api_key: "..."   # only if your instance requires auth
```

If you don't already run one, Tavily is the lowest-friction keyed option —
no self-hosting, no card:

```yaml
search:
  provider: tavily
  api_key: "$TAVILY_API_KEY"   # set TAVILY_API_KEY in environment or .aegis/.env
```

Tavily's free tier (1,000 credits/month, no card required as of 2026-08) is
the lowest-friction zero-infrastructure option. Brave's API is also supported
(`provider: brave`), but as of February 2026 it no longer has a no-card free
tier — it bills per query past a small monthly credit that requires public
attribution to keep; it's worth using only if you're already paying for it.

### Use an AI gateway

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  base_url: "https://ai-gateway.internal.example.com"
  headers:
    X-Gateway-Token: "your-token"
    X-Tenant-ID: "tenant-id"
```

`base_url` and `headers` send every prompt and tool result to the named host,
so both are trust-gated in project config — a gateway is normally a global
setting anyway. See
[Project Config and Workspace Trust](#project-config-and-workspace-trust).

The gateway must proxy the provider's native paths:
- Anthropic: `POST /v1/messages`
- OpenAI/local: `POST /v1/chat/completions`
