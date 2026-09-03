# Containers

Compose templates for optional tools Aegis can talk to, but does not manage
the lifecycle of. Each subdirectory is one tool: a `compose.yaml` you bring up
yourself (`docker compose up -d` / `podman compose up -d`), a config file
already templated for use with Aegis, and a README covering setup and the
matching `.aegis/config.yaml` / `~/.config/aegis/config.yaml` stanza.

This is deliberately **not** the same shape as `internal/security`'s scanner
images. Those are opt-in security tooling Aegis builds, pins by image ID, and
runs itself (`aegis security build-image`) — a workload where Aegis owning the
container lifecycle is the point. The tools here (a search backend today,
potentially others later) sit in the main chat loop, `go build` needs none of
this, and self-hosting one is a deliberate operator choice with its own
tradeoffs (see each tool's README) — not something Aegis should silently start
managing. See P71.13 in [research/roadmap.md](../research/roadmap.md) for the
full reasoning.

## Available templates

| Directory | Tool | Used by |
|-----------|------|---------|
| [`searxng/`](searxng/) | [SearXNG](https://docs.searxng.org/) meta-search engine | `search.provider: searxng` |

## Conventions

- `compose.yaml` is runnable as-is for a private/local instance; anything that
  must change before exposing it beyond localhost is called out in the
  README, not silently defaulted safe.
- Config files in each directory (e.g. `searxng/settings.yml`) are already
  shaped for Aegis's needs (the JSON output format Aegis's `web_search` tool
  requires, above all) — you are not expected to write one from scratch.
- Each README ends with the exact `search:` (or equivalent) block to drop
  into Aegis config, and reminds you which config layer needs
  `aegis trust --dir` (see
  [docs/configuration.md](../docs/configuration.md#project-config-and-workspace-trust)).
