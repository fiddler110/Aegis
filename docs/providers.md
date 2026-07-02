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
aegis models          # curated model catalog with recommendations
aegis models --local  # also probe localhost for running servers
```

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
  small_model: "claude-haiku-4-5-20251001"   # used for compaction summaries
```

If `small_model` is empty, the main model is used for compaction (more expensive but always available).

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

## Context Window

Aegis uses the model's context window size to determine when to run compaction (at 85% fill). For local models that don't report their context size:

```yaml
provider:
  context_window: 32768   # set manually in tokens
```

`0` or missing means auto-detect; if detection fails, compaction is skipped for local models.
