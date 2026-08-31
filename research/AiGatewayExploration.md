# AiGateway Integration — Exploration

**Question:** Should AiGateway be baked into Aegis, or kept separate with Aegis's external
calls routed through it?

**Answer:** Keep them separate. Point Aegis at AiGateway through `provider.base_url` +
`provider.headers`. Do not merge the codebases.

**Date:** 2026-08-31 · **Aegis commit:** `88cea69` (working tree) · **AiGateway:** `D:\Development\AiGateway`

This document records the reasoning, the concrete integration shape, and — importantly —
the parts of Aegis's egress surface a gateway integration does *not* cover. It follows the
threat model in [`threat-model-20260831-002123/`](threat-model-20260831-002123/) and
references its findings by ID.

---

## 1. Why not merge

### It breaks Aegis's defining property

`CLAUDE.md` opens with the constraint that makes Aegis what it is:

> `go build` needs no container runtime or Node.js.

AiGateway is Python/FastAPI (`pyproject.toml`, `gateway/main.py`) shipped as Docker and k8s
(`Dockerfile`, `docker-compose.yml`, `k8s/`). Merging means one of two things, and both are bad:

| Option | Cost |
|--------|------|
| Ship a Python runtime dependency with Aegis | Kills the single-binary promise; `go build` now needs a Python toolchain and a venv |
| Reimplement the middleware pipeline in Go | A large rewrite of `audit_log`, `secrets_scanner`, `context_pseudonymizer`, `pii_redactor`, `content_policy`, `cost_tracker`, `rate_limiter`, `token_rate_limiter`, `token_counter`, response cache and upstream manager — which then serves exactly one client |

### It also breaks AiGateway's defining property

AiGateway's README states its scope in the first paragraph:

> Every request from your tools — Claude Code, VS Code Copilot, opencode, scripts, local
> models — flows through the gateway before reaching the AI provider.

It is deliberately client-agnostic and provider-agnostic. Folding it into Aegis makes the
cost ledger, the audit trail and the pseudonym map **per-tool instead of per-network** —
the wrong granularity for the thing it exists to provide. And because Copilot and opencode
would still need a standalone gateway, merging means maintaining both anyway.

### The integration is already a config change, not code

Aegis was built anticipating this deployment. Two pieces of evidence:

- `internal/config/config_provider.go:44` — the comment names the use case directly:

  ```go
  Headers map[string]string `koanf:"headers"` // extra HTTP headers sent with every request (e.g. gateway auth)
  ```

- `internal/providerfactory/factory.go`, `validateBaseURL` doc comment — a non-default host
  gets a startup `WARN` rather than a refusal, because "many legitimate uses (corporate
  gateways, self-hosted OpenAI-compatible proxies) exist and a hard refusal there would be
  a real regression."

---

## 2. What routing through the gateway buys

Mapped against the threat model's findings:

| Finding | Effect of routing through AiGateway |
|---------|-------------------------------------|
| **FIND-05** — outbound provider payloads and tool arguments are never redacted | **Closed for the LLM hop.** `secrets_scanner` → `context_pseudonymizer` → `pii_redactor` is precisely the missing pass. The pseudonymizer is strictly better than redaction here: structurally-similar fakes let the model still reason about the infrastructure, and the mapping is reversed on the response. |
| **FIND-15** — all `cost.*` budgets default to `0` (unlimited) | **Closed, and better placed.** `cost_tracker` + `rate_limiter` + `token_rate_limiter` give a network-wide ceiling across every tool, which is more useful than a per-Aegis-session cap. |
| **FIND-14** / **T17.R** — no durable record of what left the host | **Closed.** `audit_log` writing to the gateway's SQLite store answers "what did we send the vendor, and when" — a question Aegis has no way to answer today. |
| **FIND-06** — `provider.base_url` override redirects credentials and model control | **Dissolved,** if credentials are relocated (see §3.1). Aegis stops holding a real vendor key, so redirecting its base URL no longer exfiltrates one. |

### The best part: credential relocation

The highest-value move is not the redaction pipeline — it is moving the vendor API keys out
of Aegis entirely.

- Real keys live only in AiGateway, referenced by `api_key_env` in its `upstreams:` config.
- Aegis holds only a gateway token, supplied via `provider.headers`.

This turns FIND-06 from a credential-exfiltration finding into a routing preference, and it
shrinks the blast radius of every Tier 3 finding that involves reading Aegis's own state
(FIND-24, FIND-25, FIND-27) — there is no longer a vendor credential in that state to steal.

---

## 3. Integration shape

### 3.1 Recommended configuration

```yaml
# .aegis/config.yaml (or ~/.config/aegis/config.yaml)
provider:
  name: openai                         # NOT anthropic — see §3.3
  base_url: https://gateway.lan:8080/v1
  headers:
    Authorization: "Bearer ${AEGIS_GATEWAY_TOKEN}"
```

With the real vendor key held only on the gateway side:

```yaml
# AiGateway config.yaml
upstreams:
  anthropic:
    base_url: https://api.anthropic.com/v1
    api_key_env: ANTHROPIC_API_KEY
    api_format: anthropic
    models: [claude-opus-5, claude-sonnet-5]
```

### 3.2 Gotcha — plaintext LAN will hard-fail, not warn

`validateBaseURL` (`internal/providerfactory/factory.go`) **refuses outright** when all three
hold: scheme is `http`, host is non-loopback, and a real API key would be attached.
`isRealAPIKey` (`factory.go:210-215`) treats any non-empty key as real, with a single
exception for the literal `ollama`/`ollama` pair:

```go
func isRealAPIKey(name, apiKey string) bool {
	if apiKey == "" {
		return false
	}
	return !(name == "ollama" && apiKey == "ollama")
}
```

So a gateway at `http://10.x.x.x:8080/v1` with `ANTHROPIC_API_KEY` set in Aegis's environment
means Aegis **will not start that provider**. Two ways out, and they compose:

1. Terminate TLS on the gateway (`https://`), which the refusal is designed to steer you toward.
2. Relocate the credential (§3.1) so Aegis holds no real key — then `isRealAPIKey` is false
   and the plaintext path is permitted. Still prefer TLS.

### 3.3 Gotcha — use Aegis's `openai` provider, not `anthropic`

AiGateway's **inbound** surface is OpenAI-shaped only:

| Route | Source |
|-------|--------|
| `POST /v1/chat/completions` | `gateway/main.py:538` |
| `POST /v1/embeddings` | `gateway/main.py:707` |
| `POST /v1/completions` | `gateway/main.py:711` |
| `GET /v1/models` | `gateway/main.py:715` |

There is no `/v1/messages` inbound route. The `/messages` path at `gateway/proxy.py:33` is
the **outbound** path to an Anthropic upstream, selected when `upstream.api_format ==
"anthropic"`; `gateway/anthropic_translate.py` performs the OpenAI→Anthropic translation.

Aegis's native Anthropic adapter (`internal/provider/anthropic/anthropic.go:198`) speaks
Anthropic's Messages API natively and sends `x-api-key`. Pointing it at the gateway would
target a route that does not exist.

**Therefore:** configure Aegis as the `openai` provider against the gateway, and let the
gateway translate to Claude upstream.

**Open question worth testing:** whether extended thinking survives that round trip.
`anthropic_translate.py`'s docstring claims to handle "extended thinking, native tool-use
blocks", but Aegis's engine has specific expectations here — see the `CLAUDE.md` invariant
"A reasoning model's preamble and its answer share one budget", plus
`provider.SuppressesExtendedThinking(Purpose)` and `provider.Request.SuppressThinking`.
Verify with `go test -tags live_eval -count=1 ./internal/eval/... -run TestLiveModelQuality -v`
pointed at the gateway before trusting it for real work.

---

## 4. What the gateway does *not* cover

This is the part that matters most, because the framing "all external calls from Aegis go
through AiGateway" **is not achievable as things stand**. AiGateway is an *LLM API proxy*,
not an *egress proxy*: its inbound routes are the four OpenAI-shaped endpoints above, and it
has no CONNECT or forward-proxy mode.

Three egress paths bypass it entirely:

| Flow | Path | Why it bypasses | Finding left open |
|------|------|-----------------|-------------------|
| **DF14** | `web_fetch` / `web_search` (`internal/tool/builtin/web.go`) | Arbitrary HTTPS to arbitrary hosts; not OpenAI-shaped | **FIND-08** — unreviewed egress channel, the injection-driven exfiltration path, entirely untouched |
| **DF21** | Outbound MCP (`internal/mcp/`) | stdio subprocesses and arbitrary HTTP/SSE | **FIND-05**'s MCP half (T19.I, T09.I1 — secrets in tool arguments) untouched |
| **DF22** | Sandboxed shell with `sandbox.network: true` | Commands reach the network directly from the container | **T11.A** untouched — this is also the path that bypasses `internal/netblock`'s SSRF blocklist |

**Consequence:** the redaction-on-egress and egress-ledger work described in FIND-05 and
FIND-08 still needs to live inside Aegis regardless of whether the gateway is deployed. The
gateway is the right answer for the model hop and no answer at all for the other three.

---

## 5. Decision and follow-on work

**Decision:** keep the two applications separate. Integrate by configuration.

Ranked follow-on work:

1. **Relocate vendor credentials to the gateway** (§3.1). Highest value, lowest effort;
   dissolves FIND-06 and shrinks several Tier 3 findings.
2. **Verify the OpenAI→Anthropic round trip preserves extended thinking** (§3.3) before
   depending on the gateway for Claude work.
3. **Build the in-Aegis egress controls anyway** — outbound redaction and a per-session
   egress ledger (FIND-05, FIND-08). The gateway cannot reach `web_fetch` or MCP.
4. **Optional, larger:** add an HTTP forward-proxy (CONNECT) mode to AiGateway and wire
   `HTTP_PROXY`/`HTTPS_PROXY` into Aegis's `ssrfClient` (`internal/tool/builtin/web.go:30`)
   and the MCP HTTP client (`internal/mcp/http.go`). This is the only version where "all
   external calls go through the gateway" becomes literally true. It is a real feature in
   both repos, not a configuration change.

---

## References

| Source | Location |
|--------|----------|
| Aegis threat model | [`threat-model-20260831-002123/0-assessment.md`](threat-model-20260831-002123/0-assessment.md) |
| Findings referenced here | [`threat-model-20260831-002123/3-findings.md`](threat-model-20260831-002123/3-findings.md) |
| Data flow IDs (DF14, DF21, DF22) | [`threat-model-20260831-002123/1-threatmodel.md`](threat-model-20260831-002123/1-threatmodel.md) |
| Base URL validation | `internal/providerfactory/factory.go` (`validateBaseURL`, `isRealAPIKey`) |
| Gateway auth headers | `internal/config/config_provider.go:44` |
| Anthropic adapter | `internal/provider/anthropic/anthropic.go:198` |
| AiGateway inbound routes | `D:\Development\AiGateway\gateway\main.py:538-715` |
| AiGateway upstream translation | `D:\Development\AiGateway\gateway\proxy.py`, `gateway\anthropic_translate.py` |
| AiGateway upstream config | `D:\Development\AiGateway\config.yaml` |
