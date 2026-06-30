# Sandbox: dynamic runtime detection & configuration

**Date:** 2026-06-29
**Status:** Approved — proceeding to implementation
**Area:** `internal/sandbox`, `internal/config`, `internal/cli`, `internal/server`, `internal/tui`

## Goal

Make Aegis's sandboxed-execution tier *dynamic*: detect which container
runtimes are available on the host (Docker, Podman, the new Windows **WSL
containers** via `wslc`, and Apple Containers on macOS), let an `auto` backend
pick the best one, and let the user inspect/select the sandbox both from a
dedicated `aegis sandbox` command (and `/sandbox` in the TUI) and from
`config.yaml`.

Out of scope: copy-on-write workspace staging / change review (roadmap P1.3's
"stage changes for review" item). The current live bind-mount model is retained.

## Background

`internal/sandbox` already has a `ContainerBackend` that auto-detects a runtime
(`detectRuntime`) among Docker, Podman, and Apple Containers, plus a `LocalBackend`.
`SandboxConfig` exposes `backend` (`local`|`container`), `runtime`, `image`,
`network`. The server (`server.New`) builds the backend at startup from config.
Configuration today is config-file only; there is no in-app surface and no WSL
support, and detection picks the first working runtime without reporting the
others.

WSL containers (public preview, WSL ≥ 2.9.3, installed via
`wsl --update --pre-release`) expose a Docker-style CLI **`wslc.exe`** (alias
`container.exe`): `wslc run --rm -p ... -v ... <image> <cmd>`. Windows-only.

## Design

### 1. Detection module — `internal/sandbox/detect.go`

```go
type RuntimeInfo struct {
    Runtime   ContainerRuntime
    Available bool
    Version   string // empty when unavailable
    Detail    string // human-readable note (e.g. "daemon not running", install hint)
}

func DetectRuntimes(ctx context.Context) []RuntimeInfo
func DetectBest(ctx context.Context, priority []ContainerRuntime) (ContainerRuntime, bool)
```

- Candidate set is OS-gated:
  - windows: `wslc, docker, podman`
  - darwin: `docker, podman, container`
  - linux: `docker, podman`
- Each probe: `exec.LookPath(bin)` first; if present, run a version/info command
  wrapped in a **~3s context timeout** so a hung/absent daemon can't block startup.
- Probe functions are an **injectable seam** (package-level vars or a struct of
  func fields) so unit tests run without any runtime installed.
- `DetectBest` returns the first `Available` runtime in priority order (caller
  passes config priority or the OS default).

### 2. WSL runtime — `wslc`

- `RuntimeWSL ContainerRuntime = "wslc"`.
- `runtimeAvailable`: Windows-only; LookPath `wslc` (fallback `container`), then
  probe `wslc --version`. Unavailable `Detail` hints `wsl --update --pre-release`.
- Run args reuse the existing OCI builder shape. **Verify at implementation**
  whether `wslc` accepts `--cap-drop=ALL` / `--security-opt=no-new-privileges` /
  `--network none` and the exact host-path mount form; degrade gracefully
  (omit unsupported flags) rather than hard-fail. Document findings in code.

### 3. Config schema — `SandboxConfig`

```yaml
sandbox:
  backend: local      # local (default) | container | auto
  runtime: ""         # force docker|podman|wslc|container (when backend=container)
  priority: []        # optional auto order; empty = OS default
  image: ubuntu:22.04
  network: false
```

- Add `Backend` value `auto` and a new `Priority []string` field.
- **Default remains `local`** — zero behavior change for existing users.

### 4. `auto` selection — `server.New`

- `backend: auto` → `DetectBest(priority|OS default)`; build container backend
  with the winner, else fall back to `LocalBackend` (logged at info).
- `backend: container` → unchanged, but detection now includes `wslc`.
- **Switching model:** config changes take effect on next restart (consistent
  with `/config`). No live hot-swap of the backend mid-session.

### 5. `aegis sandbox` command — `internal/cli/sandbox.go`

Runs without a daemon (probing + config writes are local):

- `aegis sandbox detect` (alias `list`) — table: runtime, available?, version,
  detail, `←` marker on the `auto` pick.
- `aegis sandbox status` — effective config + live detection side by side.
- `aegis sandbox use <local|auto|docker|podman|wslc|container>` — write via new
  `config.PatchGlobalSandbox` (mirrors `PatchGlobalProvider`, reuses
  `spliceSection`); `--project` writes `.aegis/config.yaml`. Prints restart note.
- `aegis sandbox test [--image X] [--network]` — run `uname -a` in the
  selected/auto runtime and print output; clear error on misconfiguration.

### 6. `/sandbox` slash command (TUI)

- `/sandbox` — render the detection table + current selection inline.
- `/sandbox use <runtime>` — write config, toast "saved; restart to apply."
- Registered in `slash.go` help + dispatch. Minimal: informational + `use`.

### 7. Testing

- `detect.go`: table-driven over stubbed probe/LookPath seams — availability,
  `DetectBest` priority, OS-gated candidates, timeout behavior.
- `wslc` run-args: argument construction (mount, network, command).
- `config.PatchGlobalSandbox`: round-trips the `sandbox:` block, preserves
  other sections.
- CLI: `detect`/`status` formatting with injected detection; `use` YAML output.
- No live-daemon / real-Docker dependency anywhere in tests.

## Priority defaults

- windows: `wslc, docker, podman` (WSL containers preferred — the new capability)
- darwin: `docker, podman, container`
- linux: `docker, podman`
