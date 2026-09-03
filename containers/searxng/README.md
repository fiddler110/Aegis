# SearXNG for Aegis

A template to self-host [SearXNG](https://docs.searxng.org/) — a meta-search
engine that queries other search engines and returns aggregated results — as
the backend for Aegis's `web_search` tool. This is optional; the zero-config
default (DuckDuckGo scraping) needs nothing.

## Why you might want this over Tavily/Brave

`search.provider: searxng` already existed in Aegis before this template did
— what didn't exist was a ready-to-run compose file. Reasons to pick it over
a keyed API (Tavily, Brave):

- No API key, no account, no per-query cost or rate plan.
- Results stay on infrastructure you control — nothing about your queries
  goes to a third-party search-API vendor.
- You already run other self-hosted infrastructure and would rather add one
  more container than manage another API key.

## Read this before you self-host it

A self-hosted SearXNG **does not remove the challenge-page/rate-limit
failure mode** the zero-config DuckDuckGo scrape hits (Aegis's `web_search`
already handles that case on its own — falling back past a throttled
DuckDuckGo to Marginalia and reporting the block clearly rather than
pretending the web is empty). SearXNG proxies out to the *same* upstream
engines (Google, Bing, Brave, DDG, …) a direct scrape already hits — it moves
where that risk lives, into a container you now operate, rather than
eliminating it. And a datacenter/CI host's IP is generally **more** likely to
get blocked by those upstreams than a residential one, which is the opposite
of what you might expect from "now it's my own infrastructure." If you're
running this on a cloud VM rather than a home connection, keep that in mind.

`settings.yml` in this directory disables Google and Bing by default and
enables Mojeek, Startpage, Brave and DuckDuckGo instead — the engines least
likely to instant-block a non-residential IP. Adjust the `engines:` list to
taste; see [SearXNG's engine list](https://docs.searxng.org/admin/engines/index.html).

## Setup

1. **Generate a secret key** and put it in `settings.yml`:

   ```bash
   openssl rand -hex 32
   ```

   Replace `REPLACE_ME_WITH_YOUR_OWN_SECRET_...` in `settings.yml`'s
   `server.secret_key` with the output. Don't skip this — it signs SearXNG's
   session cookies.

2. **Bring it up:**

   ```bash
   docker compose up -d
   # or: podman compose up -d
   ```

3. **Verify JSON output works** (Aegis's `web_search` needs it — SearXNG
   disables the JSON format by default upstream, which `settings.yml` here
   already re-enables, but confirm it landed):

   ```bash
   curl "http://localhost:8080/search?q=test&format=json"
   ```

   A JSON body with a `results` array means you're set. An HTML page or a 403
   means the `format=json` override in `settings.yml` isn't being read —
   check the volume mount picked up your edited file.

4. **Point Aegis at it.** In `.aegis/config.yaml` (project) or
   `~/.config/aegis/config.yaml` (user):

   ```yaml
   search:
     provider: searxng
     base_url: "http://localhost:8080"
   ```

   `search` is trust-gated in project config (see
   [docs/configuration.md](../../docs/configuration.md#project-config-and-workspace-trust)) —
   run `aegis trust --dir` in the project if you set it there instead of the
   user config layer.

## Exposing it beyond localhost

This template assumes SearXNG is reachable only from the machine or LAN
running Aegis. If you expose it more broadly:

- Turn `server.limiter: true` back on in `settings.yml` and add a
  Redis/Valkey service to `compose.yaml` — the limiter is a no-op without a
  backing store, which is why this template ships it off rather than
  half-enabled. See
  [SearXNG's limiter docs](https://docs.searxng.org/admin/searx.limiter.html).
- Put it behind TLS (a reverse proxy) rather than serving plain HTTP.
- Consider `server.public_instance: false` stays as-is — this isn't meant to
  be a public instance for others to query.

## Uninstalling

```bash
docker compose down -v
```

`-v` also removes SearXNG's local state volume, if one has accumulated.
