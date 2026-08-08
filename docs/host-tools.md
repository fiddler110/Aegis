# Host tools

Some Aegis features run faster, or only run at all, when an external binary is
present on the host. None of them is required — every one has a working
fallback — but the difference is large enough for search that it is worth
installing deliberately rather than by accident.

`aegis doctor` reports every one of them: whether it resolved, where it
resolved from, what Aegis uses it for, and how to install it.

```
PASS command: ripgrep    /usr/bin/rg — backs the grep tool; 2.5-9x faster than the built-in walker on a mid-size repo
WARN command: mmdc       not found on PATH (mmdc); diagrams render via the remote Kroki service instead (needs network)
     -> npm install -g @mermaid-js/mermaid-cli (npm), or set commands.mmdc to its path
```

## Configuring where a binary lives

The `commands:` block overrides how each tool is located. It belongs in
`~/.config/aegis/config.yaml` for a machine-wide choice, or `.aegis/config.yaml`
for one repo.

```yaml
commands:
  ripgrep: rg                       # bare name — resolved on PATH (the default)
  git: /opt/homebrew/bin/git        # absolute path — used as-is
  gh: off                           # disabled — always use Aegis's fallback
```

A value may be:

| Form | Meaning |
|---|---|
| a bare name (`rg`, `rg-14`) | looked up on `PATH` |
| a path (`/opt/bin/rg`, `C:\tools\rg.exe`) | used directly, verified executable |
| `off` / `false` / `no` / `none` / `disabled` / `0` | never use it; take the built-in fallback |

Unset means "look for the tool's usual name on PATH", which is what happens
today with no config at all.

**Shell aliases do not work here.** Aegis executes binaries directly rather than
through a shell, so an `alias rg=...` in your `.bashrc` or PowerShell profile is
never visible to it. If your binary is not on `PATH` under the name in the table
below, give `commands:` its real path — that is exactly what the key is for.

A configured binary that cannot be found is a **failure**, not a silent
fallback: you named a specific binary, and quietly using something else would
defeat the point. A tool you simply never installed is a warning, since the
fallback is fine.

## The tools

| Key | Binary | Used for | Without it |
|---|---|---|---|
| `ripgrep` | `rg` | the `grep` and `glob` tools | a pure-Go directory walk — correct, but slower on large trees |
| `git` | `git` | the `git` tool, commit/diff/log, checkpoints | git-backed tools error; nothing else is affected |
| `gh` | `gh` | the `git_pr` tool | PR tools unavailable; plain git still works |
| `mmdc` | `mmdc` | local Mermaid rendering | diagrams render via the remote Kroki service (needs network) |
| `plantuml` | `plantuml` | local PlantUML rendering | diagrams render via the remote Kroki service (needs network) |

Container runtimes (`docker`, `podman`, …) and the security scanners are **not**
in this list. They have their own resolution in `internal/sandbox` and
`internal/security`, with rules — OS-specific auto-detect order, container-vs-host
method selection — that this simpler mechanism has no business overriding. See
[installation.md](installation.md) and [security_scan.md](security_scan.md).

## Why ripgrep is the one that matters

Measured on this repository (4.4k files, 270MB) and on a synthetic 40k-file
tree, comparing the ripgrep backend against the built-in Go walker. Each figure
is the median of three runs on Windows; process-spawn overhead is roughly 35ms
and is included.

| Operation | 4.4k files | 40k files |
|---|---|---|
| `grep`, common term (hits the 500 cap) | **96ms** vs 452ms walk | 46ms vs 43ms walk |
| `grep`, rare term | **70ms** vs 295ms walk | — |
| `grep`, no matches | — | **879ms** vs 3326ms walk |
| `glob **/*.go` | 39ms vs **17ms** walk | **79ms** vs 461ms walk |

The crossover for `glob` sits somewhere around 10-15k files: below it the Go
walker wins because ripgrep's process spawn dominates, above it ripgrep wins by
roughly 6x. ripgrep is preferred for both operations because the penalty on a
small repo is ~20ms — noise next to a model round-trip — while the win on a
large one is seconds.

The capped-`grep` row is the interesting one. ripgrep has no *global* match
limit (`--max-count` is per file), so a common pattern used to make it walk the
entire tree to produce output that was then thrown away at 500 matches — 964ms
on the 40k tree, against 43ms for the Go walker, which stops early. Aegis now
streams ripgrep's output and cancels the process the moment the cap is reached,
which brings that case to 46ms without giving up the large win on queries that
are not capped.

Two behaviours are worth knowing about:

- **Both backends return the same answer.** ripgrep honours `.gitignore` by
  default and the Go walker does not, so the same query used to return different
  results depending on whether ripgrep happened to be installed. Aegis now runs
  ripgrep with `--no-ignore-vcs` and applies its own exclusion list explicitly,
  so the two agree; a regression test asserts it.
- **A capped result set says so.** `grep` returns at most 500 matches and `glob`
  at most 1000. When the cap bites, the output ends with a notice saying the set
  is partial and unordered — which subset you get depends on traversal order,
  and ripgrep's parallel walk does not visit files in the same order as the Go
  walker. Forcing `--sort path` would make it deterministic but costs about 4x
  (220ms → 900ms measured), more than ripgrep's entire advantage.

## Provisioning a host

The commands below install everything in the table. Only `ripgrep` and `git` are
worth treating as near-mandatory; the rest are situational.

```bash
# Debian / Ubuntu
sudo apt-get install -y ripgrep git gh

# Fedora / RHEL
sudo dnf install -y ripgrep git gh

# Arch
sudo pacman -S --noconfirm ripgrep git github-cli

# macOS
brew install ripgrep git gh

# Windows (scoop)
scoop install ripgrep git gh

# Windows (winget)
winget install BurntSushi.ripgrep.MSVC Git.Git GitHub.cli

# Optional, any platform — local diagram rendering instead of remote Kroki
npm install -g @mermaid-js/mermaid-cli
```

`aegis doctor` picks the manager actually present on the machine when it prints
an install hint, so running it on a fresh host tells you the right command for
that host.
